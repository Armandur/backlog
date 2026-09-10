// Package pm innehåller PM-lagret ovanpå backlog: spärren som håller
// backlog-pm borta från vardagsdatabasen, och senare PM-kommandon.
package pm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/profile"
)

// DefaultProfileName är namnet på vardagsprofilen som backlog-pm aldrig får köra mot.
const DefaultProfileName = "default"

// Selection är det databasval en körning gör: profilflaggan, --db, BACKLOG_DB
// och init:s --path.
type Selection struct {
	Profile string
	DB      string
	EnvDB   string
	Path    string
}

// Check avgör om ett databasval är tillåtet för backlog-pm.
// defaultDir är vardagsprofilens workspace-katalog, tom om ingen finns.
func Check(sel Selection, defaultDir string) error {
	// init --path öppnar och kan med --reset radera databasen i katalogen.
	if sel.Path != "" && defaultDir != "" && withinDir(sel.Path, defaultDir) {
		return fmt.Errorf("backlog-pm vägrar skriva i vardagsprofilens katalog: --path pekar in i %q (%s)", DefaultProfileName, defaultDir)
	}

	if path := firstNonEmpty(sel.DB, sel.EnvDB); path != "" {
		if defaultDir != "" && withinDir(path, defaultDir) {
			return fmt.Errorf("backlog-pm vägrar köra mot vardagsdatabasen: %s pekar in i profilen %q (%s). Peka om till PM-workspacet", sourceName(sel), DefaultProfileName, defaultDir)
		}
		return nil
	}

	switch sel.Profile {
	case "":
		return fmt.Errorf("backlog-pm kräver en egen profil: ange --profile pm. Utan flagga skulle vardagsprofilen %q användas", DefaultProfileName)
	case DefaultProfileName:
		return fmt.Errorf("backlog-pm får inte köras mot profilen %q - det är vardagsdatabasen. Ange --profile pm", DefaultProfileName)
	}
	return nil
}

// Guard är cobra-adaptern som backlog-pm hänger in i CLI:t.
func Guard(cmd *cobra.Command) error {
	defaultDir, err := profile.Resolve("")
	if err != nil {
		// Ingen läsbar vardagsprofil: fortsätt med tom katalog, flaggkontrollen räcker.
		defaultDir = ""
	}
	return Check(selectionFromCmd(cmd), defaultDir)
}

func selectionFromCmd(cmd *cobra.Command) Selection {
	return Selection{
		Profile: flagValue(cmd, "profile"),
		DB:      flagValue(cmd, "db"),
		EnvDB:   os.Getenv("BACKLOG_DB"),
		Path:    flagValue(cmd, "path"),
	}
}

// flagValue läser den flagga kommandot faktiskt ser: ett lokalt --profile
// (som på init) skuggar rotens persistenta flagga.
func flagValue(cmd *cobra.Command, name string) string {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

func sourceName(sel Selection) string {
	if sel.DB != "" {
		return "--db"
	}
	return "BACKLOG_DB"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// withinDir är sant om path är dir eller ligger under dir. Symlänkar löses
// upp först, annars smiter en länk till vardagskatalogen förbi spärren.
func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(resolvePath(dir), resolvePath(path))
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

// resolvePath ger en absolut sökväg med symlänkar upplösta. Finns inte
// sökvägen än löses närmaste befintliga förälder upp i stället.
func resolvePath(p string) string {
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		return expandHome(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(real, filepath.Base(abs))
	}
	return abs
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
