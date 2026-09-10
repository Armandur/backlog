package pm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/cli"
)

const defaultDir = "/home/rasmus/.backlog/default"

func TestCheckAvvisarSaknadProfil(t *testing.T) {
	err := Check(Selection{}, defaultDir)
	if err == nil {
		t.Fatal("tomt val skulle använda vardagsprofilen, väntade fel")
	}
	if !strings.Contains(err.Error(), "--profile pm") {
		t.Fatalf("felet ska peka ut lösningen, fick: %v", err)
	}
}

func TestCheckAvvisarDefaultProfil(t *testing.T) {
	if err := Check(Selection{Profile: "default"}, defaultDir); err == nil {
		t.Fatal("profilen default skulle passera spärren")
	}
}

func TestCheckSlapperIgenomEgenProfil(t *testing.T) {
	if err := Check(Selection{Profile: "pm"}, defaultDir); err != nil {
		t.Fatalf("profilen pm ska passera, fick: %v", err)
	}
}

func TestCheckAvvisarDbSomPekarInIVardagsprofilen(t *testing.T) {
	cases := []Selection{
		{Profile: "pm", DB: filepath.Join(defaultDir, "backlog.db")},
		{Profile: "pm", EnvDB: filepath.Join(defaultDir, "backlog.db")},
		{Profile: "pm", EnvDB: defaultDir},
	}
	for _, sel := range cases {
		if err := Check(sel, defaultDir); err == nil {
			t.Fatalf("valet %+v pekar på vardagsdatabasen men passerade", sel)
		}
	}
}

func TestCheckSlapperIgenomAnnanDbSokvag(t *testing.T) {
	sel := Selection{Profile: "pm", DB: "/home/rasmus/.config/backlog/pm/backlog.db"}
	if err := Check(sel, defaultDir); err != nil {
		t.Fatalf("egen db-sökväg ska passera, fick: %v", err)
	}
}

// BACKLOG_DB går före --profile i resolveDB, så spärren måste titta på sökvägen.
func TestCheckAvvisarEnvDbAvenUtanProfil(t *testing.T) {
	if err := Check(Selection{EnvDB: filepath.Join(defaultDir, "backlog.db")}, defaultDir); err == nil {
		t.Fatal("BACKLOG_DB mot vardagsdatabasen passerade spärren")
	}
}

// Vägen genom cobra: rotens flaggor och init:s lokala --profile som skuggar dem.
func TestGuardGenomCobra(t *testing.T) {
	cli.SetGuard(Guard)
	t.Cleanup(func() { cli.SetGuard(nil) })

	cases := []struct {
		namn    string
		args    []string
		vantFel bool
	}{
		{"utan profil", []string{"project", "list"}, true},
		{"profil default", []string{"--profile", "default", "project", "list"}, true},
		{"profilkommando utan profil", []string{"profile", "list"}, true},
		{"init utan profil", []string{"init"}, true},
		{"init med default", []string{"init", "--profile", "default"}, true},
		{"version slipper spärren", []string{"version"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.namn, func(t *testing.T) {
			root := cli.NewRoot("backlog-pm")
			root.SetArgs(tc.args)
			root.SetOut(&strings.Builder{})
			root.SetErr(&strings.Builder{})
			err := root.Execute()
			if tc.vantFel {
				if err == nil {
					t.Fatalf("%v passerade spärren", tc.args)
				}
				if !strings.HasPrefix(err.Error(), "backlog-pm") {
					t.Fatalf("väntade spärrfel på svenska, fick: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v skulle inte spärras, fick: %v", tc.args, err)
			}
		})
	}
}

// init --profile pm får inte spärras: den lokala flaggan ska läsas, inte rotens.
func TestGuardLaserInitsEgnaProfilflagga(t *testing.T) {
	root := cli.NewRoot("backlog-pm")
	initCmd, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("hittade inte init: %v", err)
	}
	if err := initCmd.Flags().Set("profile", "pm"); err != nil {
		t.Fatalf("kunde inte sätta flaggan: %v", err)
	}
	if err := Guard(initCmd); err != nil {
		t.Fatalf("init --profile pm ska passera, fick: %v", err)
	}
}

// init --path öppnar databasen i katalogen och kan med --reset radera den.
func TestCheckAvvisarPathInIVardagsprofilen(t *testing.T) {
	if err := Check(Selection{Profile: "pm", Path: defaultDir}, defaultDir); err == nil {
		t.Fatal("--path mot vardagsprofilens katalog passerade spärren")
	}
	if err := Check(Selection{Profile: "pm", Path: "/home/rasmus/.config/backlog/pm"}, defaultDir); err != nil {
		t.Fatalf("egen --path ska passera, fick: %v", err)
	}
}

func TestGuardSparrarInitMotVardagskatalogen(t *testing.T) {
	root := cli.NewRoot("backlog-pm")
	initCmd, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("hittade inte init: %v", err)
	}
	if err := initCmd.Flags().Set("profile", "pm"); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.Flags().Set("path", filepath.Join(homeDir(t), ".backlog", "default")); err != nil {
		t.Fatal(err)
	}
	if err := Guard(initCmd); err == nil {
		t.Fatal("init --path mot vardagskatalogen passerade spärren")
	}
}

func TestPMKommandotFinnsIBinarensRot(t *testing.T) {
	root := cli.NewRoot("backlog-pm", NewPMCmd())
	if _, _, err := root.Find([]string{"pm", "info"}); err != nil {
		t.Fatalf("pm info saknas i backlog-pm: %v", err)
	}
	if _, _, err := cli.NewRoot("backlog").Find([]string{"pm"}); err == nil {
		t.Fatal("pm-kommandot ska inte finnas i vanliga backlog")
	}
}

func TestInfoBeskriverValdWorkspace(t *testing.T) {
	text, err := Info(Selection{Profile: "pm", DB: "/tmp/finns-inte/backlog.db"})
	if err != nil {
		t.Fatal(err)
	}
	for _, vantat := range []string{"profil:   pm", "/tmp/finns-inte/backlog.db", "saknas", "spärr:    aktiv"} {
		if !strings.Contains(text, vantat) {
			t.Fatalf("info saknar %q, fick:\n%s", vantat, text)
		}
	}
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("ingen hemkatalog")
	}
	return home
}

// En symlänk till vardagskatalogen får inte ta sig förbi spärren.
func TestCheckAvvisarSymlankTillVardagskatalogen(t *testing.T) {
	riktig := t.TempDir()
	lank := filepath.Join(t.TempDir(), "genvag")
	if err := os.Symlink(riktig, lank); err != nil {
		t.Skipf("kan inte skapa symlänk: %v", err)
	}

	cases := []Selection{
		{Profile: "pm", Path: lank},
		{Profile: "pm", Path: filepath.Join(lank, "under")},
		{Profile: "pm", DB: filepath.Join(lank, "backlog.db")},
		{Profile: "pm", EnvDB: filepath.Join(lank, "backlog.db")},
	}
	for _, sel := range cases {
		if err := Check(sel, riktig); err == nil {
			t.Fatalf("symlänksvalet %+v passerade spärren", sel)
		}
	}
}

// Flera nivåer av ännu icke-existerande kataloger under en symlänk får inte
// smita förbi: init skapar dem och hamnar då inuti vardagskatalogen.
func TestCheckAvvisarDjupOskapadSokvagUnderSymlank(t *testing.T) {
	riktig := t.TempDir()
	lank := filepath.Join(t.TempDir(), "genvag")
	if err := os.Symlink(riktig, lank); err != nil {
		t.Skipf("kan inte skapa symlänk: %v", err)
	}

	for _, djup := range []string{"nested/deep", "a/b/c/d", "nested/deep/backlog.db"} {
		sel := Selection{Profile: "pm", Path: filepath.Join(lank, djup)}
		if err := Check(sel, riktig); err == nil {
			t.Fatalf("--path %s under symlänk passerade spärren", djup)
		}
	}
	if err := Check(Selection{Profile: "pm", Path: filepath.Join(t.TempDir(), "a/b/c")}, riktig); err != nil {
		t.Fatalf("egen oskapad sökväg ska passera, fick: %v", err)
	}
}
