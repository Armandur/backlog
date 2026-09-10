package pm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/profile"
)

// NewPMCmd är backlog-pm:s egna kommandogren. PM-kommandon hängs in här.
func NewPMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pm",
		Short: "PM-kommandon (projektsamtal, agenter, körningar)",
	}
	cmd.AddCommand(infoCmd())
	return cmd
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Visa vilken workspace backlog-pm kör mot",
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := Info(selectionFromCmd(cmd))
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

// Info beskriver vald profil, databasfil och att spärren är aktiv.
func Info(sel Selection) (string, error) {
	dbPath := firstNonEmpty(sel.DB, sel.EnvDB)
	if dbPath == "" {
		dir, err := profile.Resolve(sel.Profile)
		if err != nil {
			return "", err
		}
		dbPath = filepath.Join(dir, "backlog.db")
	}
	abs, err := filepath.Abs(expandHome(dbPath))
	if err != nil {
		return "", err
	}
	finns := "saknas"
	if _, err := os.Stat(abs); err == nil {
		finns = "finns"
	}
	return fmt.Sprintf("profil:   %s\ndatabas:  %s (%s)\nspärr:    aktiv, profilen %q är blockerad\n", sel.Profile, abs, finns, DefaultProfileName), nil
}
