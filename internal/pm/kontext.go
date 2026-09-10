package pm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// KontextGranser styr hur mycket projektkontext en fråga får med sig.
type KontextGranser struct {
	Inlagg int
	Tasks  int
	Memory int
}

var StandardGranser = KontextGranser{Inlagg: 20, Tasks: 20, Memory: 10}

// ByggKontext samlar det agenten behöver: projektets namn, de senaste
// inläggen, öppna tasks och projektminnet.
func ByggKontext(ctx context.Context, db *sql.DB, alias, projectID string, g KontextGranser) (string, error) {
	if g.Inlagg == 0 {
		g = StandardGranser
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Projekt: %s\n\n", alias)

	tasks, err := oppnaTasks(ctx, db, projectID, g.Tasks)
	if err != nil {
		return "", err
	}
	b.WriteString("## Öppna tasks\n")
	if len(tasks) == 0 {
		b.WriteString("(inga)\n")
	}
	for _, t := range tasks {
		b.WriteString(t + "\n")
	}

	minnen, err := projektminne(ctx, db, projectID, g.Memory)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Projektminne\n")
	if len(minnen) == 0 {
		b.WriteString("(tomt)\n")
	}
	for _, m := range minnen {
		b.WriteString("- " + m + "\n")
	}

	store := NewSamtalStore(db)
	poster, err := store.List(ctx, projectID, g.Inlagg)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Senaste inläggen i samtalet\n")
	if len(poster) == 0 {
		b.WriteString("(tomt)\n")
	}
	for _, p := range poster {
		fmt.Fprintf(&b, "[%s] %s:%s: %s\n", Tidstext(p.CreatedAt), p.Actor.Kind, p.Actor.Name, p.Text)
	}
	return b.String(), nil
}

// ByggPrompt sätter ihop kontexten med frågan.
func ByggPrompt(kontext, fraga string) string {
	return fmt.Sprintf(`Du svarar i projektsamtalet för ett projekt i backlog-pm.
Svara kort och på svenska. Underlaget nedan är projektets aktuella läge.

%s
## Fråga
%s
`, kontext, strings.TrimSpace(fraga))
}

func oppnaTasks(ctx context.Context, db *sql.DB, projectID string, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(task_seq,0), title, status, priority FROM tasks
		 WHERE project_id = ? AND status != 'done' AND archived_at IS NULL
		 ORDER BY priority ASC, task_seq ASC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("läs öppna tasks: %w", err)
	}
	defer rows.Close()
	var ut []string
	for rows.Next() {
		var seq, prio int
		var titel, status string
		if err := rows.Scan(&seq, &titel, &status, &prio); err != nil {
			return nil, err
		}
		ut = append(ut, fmt.Sprintf("- TASK-%d (P%d, %s): %s", seq, prio, status, titel))
	}
	return ut, rows.Err()
}

func projektminne(ctx context.Context, db *sql.DB, projectID string, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT body FROM project_memory WHERE project_id = ? ORDER BY created_at DESC LIMIT ?`,
		projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("läs projektminne: %w", err)
	}
	defer rows.Close()
	var ut []string
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		ut = append(ut, strings.TrimSpace(body))
	}
	return ut, rows.Err()
}

// Tidstext formaterar en tidsstämpel (nanosekunder) för läsning i terminalen.
func Tidstext(ts int64) string {
	return time.Unix(0, ts).Format("2006-01-02 15:04")
}
