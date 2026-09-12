package pm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ByggBrief skriver briefen agenten får: taskens beskrivning, projektets
// memory och de senaste samtalsinläggen.
func ByggBrief(ctx context.Context, db *sql.DB, fakta TaskFakta) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Du utför en task åt backlog-pm. Arbeta i repot %s och avsluta med en kort rapport.\n\n", fakta.RepoPath)
	fmt.Fprintf(&b, "# %s: %s\n", fakta.Ref, fakta.Titel)
	if len(fakta.Etiketter) > 0 {
		fmt.Fprintf(&b, "Etiketter: %s\n", strings.Join(fakta.Etiketter, ", "))
	}
	fmt.Fprintf(&b, "Typ: %s\n\n## Beskrivning\n%s\n", fakta.Typ, strings.TrimSpace(fakta.Beskrivning))

	testserver, err := byggTestserverBrief(ctx, db, fakta.ProjectID)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Testserver\n")
	b.WriteString(testserver)

	minnen, err := projektminne(ctx, db, fakta.ProjectID, StandardGranser.Memory)
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

	poster, err := NewSamtalStore(db).List(ctx, fakta.ProjectID, StandardGranser.Inlagg)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Senaste inläggen i projektsamtalet\n")
	if len(poster) == 0 {
		b.WriteString("(tomt)\n")
	}
	for _, p := range poster {
		fmt.Fprintf(&b, "[%s] %s:%s: %s\n", Tidstext(p.CreatedAt), p.Actor.Kind, p.Actor.Name, p.Text)
	}
	return b.String(), nil
}

func byggTestserverBrief(ctx context.Context, db *sql.DB, projectID string) (string, error) {
	var alias string
	if err := db.QueryRowContext(ctx, `SELECT alias FROM projects WHERE id=?`, projectID).Scan(&alias); err != nil {
		return "", fmt.Errorf("kunde inte läsa projektalias för briefen: %w", err)
	}
	workspace, err := databasWorkspace(ctx, db)
	if err != nil {
		return "", err
	}
	konfig, err := LasKonfig(workspace)
	if err != nil {
		return "", err
	}
	if _, finns := konfig.Testserver[alias]; !finns {
		return fmt.Sprintf(
			"PM äger testservern men saknar konfiguration för %s.\nVälj ingen port.\nFöreslå ett lämpligt startkommando i rapporten. Gissa inte och starta ingen egen bakgrundsprocess.\n",
			alias,
		), nil
	}

	var b strings.Builder
	b.WriteString("PM äger testservern. Välj ingen port och starta ingen egen bakgrundsprocess.\n")
	fmt.Fprintf(&b, "Starta med MCP-verktyget `testserver_start` eller kommandot `backlog-pm testserver start %s`.\n", alias)
	server, err := NewTestserverStore(db, konfig, workspace).Hamta(ctx, alias)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil && server.Lever {
		vard, vardfel := os.Hostname()
		if vardfel != nil {
			return "", fmt.Errorf("kunde inte läsa värdnamnet för testservern: %w", vardfel)
		}
		fmt.Fprintf(&b, "Servern kör på http://%s:%d/.\n", vard, server.Port)
	}
	return b.String(), nil
}

func databasWorkspace(ctx context.Context, db *sql.DB) (string, error) {
	var sekvens int
	var namn, sokvag string
	if err := db.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&sekvens, &namn, &sokvag); err != nil {
		return "", fmt.Errorf("kunde inte hitta PM-workspacet: %w", err)
	}
	if sokvag == "" {
		return "", fmt.Errorf("kunde inte hitta PM-workspacet för databasen")
	}
	return filepath.Dir(sokvag), nil
}
