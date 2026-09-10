package pm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TaskFakta är det regelmotorn dömer på.
type TaskFakta struct {
	ID          string
	Ref         string
	Titel       string
	Beskrivning string
	Typ         string
	Etiketter   []string
	ProjectID   string
	RepoPath    string
	Status      string
}

// AgentVal är utfallet av regelmotorn. Motiveringen följer med till körningen.
type AgentVal struct {
	Agent      string
	Motivering string
}

// ValjAgent följer första matchande regel. Overstyrning vinner alltid, och
// utan träff används default-agenten.
func ValjAgent(k Konfig, fakta TaskFakta, overstyrning string) (AgentVal, error) {
	if overstyrning != "" {
		if _, finns := k.Agenter[overstyrning]; !finns {
			return AgentVal{}, fmt.Errorf("agenten %q finns inte i konfigurationen", overstyrning)
		}
		return AgentVal{Agent: overstyrning, Motivering: fmt.Sprintf("överstyrd med --agent %s", overstyrning)}, nil
	}
	for i, r := range k.Regler {
		if skal, traff := matchar(r, fakta); traff {
			namn := r.Namn
			if namn == "" {
				namn = fmt.Sprintf("regel %d", i+1)
			}
			return AgentVal{Agent: r.Agent, Motivering: fmt.Sprintf("regeln %q matchade (%s)", namn, skal)}, nil
		}
	}
	if k.DefaultAgent == "" {
		return AgentVal{}, fmt.Errorf("ingen regel matchade och default_agent saknas i konfigurationen")
	}
	kalla := "inbyggd standardkonfiguration"
	if k.Kalla != "" {
		kalla = k.Kalla
	}
	return AgentVal{Agent: k.DefaultAgent, Motivering: fmt.Sprintf("ingen regel matchade, default-agent %s (%s)", k.DefaultAgent, kalla)}, nil
}

// matchar kräver att varje angivet villkor stämmer, och hoppar över tomma villkor.
func matchar(r Regel, f TaskFakta) (string, bool) {
	var skal []string

	if len(r.Typ) > 0 {
		if !innehaller(r.Typ, f.Typ) {
			return "", false
		}
		skal = append(skal, "typ="+f.Typ)
	}
	if len(r.Etiketter) > 0 {
		traffad := ""
		for _, e := range r.Etiketter {
			if innehaller(f.Etiketter, e) {
				traffad = e
				break
			}
		}
		if traffad == "" {
			return "", false
		}
		skal = append(skal, "etikett="+traffad)
	}
	if len(r.Nyckelord) > 0 {
		text := strings.ToLower(f.Titel + " " + f.Beskrivning)
		traffat := ""
		for _, n := range r.Nyckelord {
			if n != "" && strings.Contains(text, strings.ToLower(n)) {
				traffat = n
				break
			}
		}
		if traffat == "" {
			return "", false
		}
		skal = append(skal, "nyckelord="+traffat)
	}
	if len(skal) == 0 {
		// Regel utan villkor fångar allt.
		skal = append(skal, "villkorslös")
	}
	return strings.Join(skal, ", "), true
}

func innehaller(lista []string, varde string) bool {
	for _, v := range lista {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(varde)) {
			return true
		}
	}
	return false
}

// HamtaTaskFakta läser det regelmotorn behöver ur PM-databasen.
func HamtaTaskFakta(ctx context.Context, db *sql.DB, taskID string) (TaskFakta, error) {
	f := TaskFakta{ID: taskID}
	var seq sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT t.title, t.description, t.type, t.status, t.task_seq, t.project_id, COALESCE(p.repo_path,'')
		 FROM tasks t JOIN projects p ON p.id = t.project_id WHERE t.id = ?`, taskID).
		Scan(&f.Titel, &f.Beskrivning, &f.Typ, &f.Status, &seq, &f.ProjectID, &f.RepoPath)
	if err == sql.ErrNoRows {
		return f, fmt.Errorf("tasken finns inte i PM-workspacet")
	}
	if err != nil {
		return f, err
	}
	if seq.Valid {
		f.Ref = fmt.Sprintf("TASK-%d", seq.Int64)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT l.name FROM labels l JOIN task_labels tl ON tl.label_id = l.id WHERE tl.task_id = ?`, taskID)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var namn string
		if err := rows.Scan(&namn); err != nil {
			return f, err
		}
		f.Etiketter = append(f.Etiketter, namn)
	}
	return f, rows.Err()
}
