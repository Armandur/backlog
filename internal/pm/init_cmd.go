package pm

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/config"
	"github.com/mazen160/backlog/internal/profile"
)

//go:embed mallar/pm.toml
var konfigmall []byte

// NewInitCmd bygger vidare på backlogs init: PM skriver också en kommenterad
// pm.toml och berättar vad användaren gör härnäst.
func NewInitCmd() *cobra.Command {
	cmd := cli.NewInitCmd()
	cmd.Short = "Skapa PM:s workspace och en kommenterad konfigurationsfil"
	cmd.Long = "Skapar workspace-katalogen, registrerar profilen och skriver pm.toml med kommenterade exempel."
	standardProfil(cmd)

	upstream := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if err := skyddaRegistreradProfil(c); err != nil {
			return err
		}
		if err := upstream(c, args); err != nil {
			return err
		}
		workspace, err := initWorkspace(c)
		if err != nil {
			return err
		}
		skrevKonfig, err := skrivKonfigmall(workspace)
		if err != nil {
			return err
		}
		profil := c.Flags().Lookup("profile").Value.String()
		fmt.Fprint(c.OutOrStdout(), nastaSteg(workspace, profil, skrevKonfig))
		return nil
	}
	return cmd
}

// standardProfil ger init samma standardprofil som resten av backlog-pm, så
// att kommandot fungerar utan flaggor. Flaggan räknas fortfarande som osatt,
// vilket spärren mot vardagsdatabasen läser.
func standardProfil(cmd *cobra.Command) {
	flagga := cmd.Flags().Lookup("profile")
	if flagga == nil || flagga.DefValue == "" {
		return
	}
	_ = flagga.Value.Set(flagga.DefValue)
}

// skyddaRegistreradProfil hindrar att en ny körning pekar om en profil som
// redan har en databas. Annars ser det ut som att allt arbete är borta.
func skyddaRegistreradProfil(cmd *cobra.Command) error {
	profil := cmd.Flags().Lookup("profile").Value.String()
	if profil == "" {
		return nil
	}
	registrerad, err := profile.Resolve(profil)
	if err != nil || registrerad == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(registrerad, "backlog.db")); err != nil {
		return nil
	}
	onskad, err := initWorkspace(cmd)
	if err != nil {
		return err
	}
	if onskad == registrerad {
		return nil
	}
	return fmt.Errorf("profilen %q finns redan och använder %s. Vill du fortsätta där kör du om med --path %s. Vill du börja om på nytt väljer du ett annat namn med --profile", profil, registrerad, registrerad)
}

// initWorkspace räknar ut katalogen init skrev till. Databasen är inte öppen
// under init, så katalogen kommer från flaggorna och inte från cli.WorkDir.
func initWorkspace(cmd *cobra.Command) (string, error) {
	if sokvag := cmd.Flags().Lookup("path").Value.String(); sokvag != "" {
		return filepath.Abs(sokvag)
	}
	profil := cmd.Flags().Lookup("profile").Value.String()
	if profil == "" {
		profil = "default"
	}
	return config.DefaultWorkspaceDir(profil), nil
}

// skrivKonfigmall lägger mallen på plats. En befintlig pm.toml lämnas orörd.
func skrivKonfigmall(workspace string) (bool, error) {
	sokvag := filepath.Join(workspace, KonfigFil)
	if _, err := os.Stat(sokvag); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("kunde inte läsa %s: %w", sokvag, err)
	}
	if err := os.WriteFile(sokvag, konfigmall, 0o600); err != nil {
		return false, fmt.Errorf("kunde inte skriva %s: %w", sokvag, err)
	}
	return true, nil
}

func nastaSteg(workspace, profil string, skrevKonfig bool) string {
	konfig := filepath.Join(workspace, KonfigFil)
	rad := fmt.Sprintf("Konfigurationen finns redan: %s\n", konfig)
	if skrevKonfig {
		rad = fmt.Sprintf("Konfigurationen ligger i %s. Den är kommenterad, så du kan läsa den.\n", konfig)
	}
	// En egen profil måste följa med varje kommando, annars öppnar PM en annan databas.
	web := "backlog-pm web"
	if profil != "" && profil != DefaultPMProfil {
		web += " --profile " + profil
	}
	return rad + fmt.Sprintf(`
Så här kommer du vidare:
  1. Starta webben:      %s
  2. Öppna adressen som webben skriver ut.
  3. Välj Nytt projekt och fyll i var koden ligger.
`, web)
}
