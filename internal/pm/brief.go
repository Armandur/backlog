package pm

import (
	"context"
	"database/sql"
	"fmt"
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
