package pm

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/repo"
)

// initIMapp kör init mot en egen katalog och profil, så testet aldrig rör
// riktiga profiler.
func initIMapp(t *testing.T, profil, katalog string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cli.SetDefaultProfile(DefaultPMProfil)
	cli.SetGuard(Guard)
	cli.SetPostOpen(Migrate)
	t.Cleanup(func() {
		cli.SetDefaultProfile("")
		cli.SetGuard(nil)
		cli.SetPostOpen(nil)
	})

	cmd := cli.NewRoot("backlog-pm", NewInitCmd())
	ut := &bytes.Buffer{}
	cmd.SetOut(ut)
	cmd.SetErr(ut)
	cmd.SetArgs([]string{"init", "--profile", profil, "--path", katalog})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init misslyckades: %v", err)
	}
	return ut.String()
}

func TestInitSkriverKonfigmallOchNastaSteg(t *testing.T) {
	katalog := filepath.Join(t.TempDir(), "ws")
	ut := initIMapp(t, "provprofil", katalog)

	data, err := os.ReadFile(filepath.Join(katalog, KonfigFil))
	if err != nil {
		t.Fatalf("pm.toml saknas: %v", err)
	}
	if !bytes.Contains(data, []byte("# Konfiguration för backlog-pm.")) {
		t.Fatalf("mallen saknar sina kommentarer: %s", data)
	}
	if !strings.Contains(ut, "backlog-pm web --profile provprofil") {
		t.Fatalf("nästa steg saknar profilen: %s", ut)
	}

	konfig, err := LasKonfig(katalog)
	if err != nil {
		t.Fatalf("mallen går inte att läsa: %v", err)
	}
	if konfig.DefaultAgent != "claude" || len(konfig.Agenter) != 2 {
		t.Fatalf("mallen gav fel konfiguration: %+v", konfig)
	}
	if konfig.Portar.Fran != 8100 || konfig.Portar.Till != 8199 {
		t.Fatalf("mallen gav fel portintervall: %+v", konfig.Portar)
	}
	// Exempelblocket är utkommenterat, annars pekar det på ett projekt som
	// inte finns och spärrar första sparningen i konfigvyn.
	if len(konfig.Testserver) != 0 {
		t.Fatalf("mallen har ett aktivt testserverblock: %+v", konfig.Testserver)
	}
}

func TestInitBevararBefintligKonfig(t *testing.T) {
	katalog := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(katalog, 0o755); err != nil {
		t.Fatal(err)
	}
	egen := []byte("default_agent = \"codex\"\n")
	if err := os.WriteFile(filepath.Join(katalog, KonfigFil), egen, 0o600); err != nil {
		t.Fatal(err)
	}

	initIMapp(t, "provprofil", katalog)

	data, err := os.ReadFile(filepath.Join(katalog, KonfigFil))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, egen) {
		t.Fatalf("init skrev över en befintlig pm.toml: %s", data)
	}
}

func TestInitViaCLISkaparPMSchemaINyDatabas(t *testing.T) {
	katalog := filepath.Join(t.TempDir(), "ws")
	initIMapp(t, "provprofil", katalog)
	verifieraPMSchema(t, katalog)
}

func TestInitViaCLIUppgraderarAldreDatabas(t *testing.T) {
	katalog := filepath.Join(t.TempDir(), "ws")
	t.Setenv("HOME", t.TempDir())
	cli.SetPostOpen(nil)
	cmd := cli.NewInitCmd()
	cmd.SetArgs([]string{"--profile", "provprofil", "--path", katalog})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("kunde inte skapa äldre databas: %v", err)
	}

	initIMapp(t, "provprofil", katalog)
	verifieraPMSchema(t, katalog)
}

func verifieraPMSchema(t *testing.T, katalog string) {
	t.Helper()
	db, err := repo.Open(filepath.Join(katalog, "backlog.db"))
	if err != nil {
		t.Fatalf("kunde inte öppna databasen: %v", err)
	}
	defer db.Close()

	tabeller := []string{"pm_samtal", "pm_korningar", "pm_portar", "pm_testservrar", "pm_anvandning"}
	for _, tabell := range tabeller {
		var antal int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, "table", tabell).Scan(&antal)
		if err != nil {
			t.Fatalf("kunde inte kontrollera tabellen %s: %v", tabell, err)
		}
		if antal != 1 {
			t.Errorf("PM-tabellen %s saknas", tabell)
		}
	}

	migreringar, err := lasMigreringar(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("kunde inte läsa PM-migreringarna: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT value FROM schema_meta WHERE key = ?`, schemaKey).Scan(&version); err != nil {
		t.Fatalf("PM:s versionsnyckel saknas: %v", err)
	}
	if version != len(migreringar) {
		t.Errorf("PM:s schemaversion är %d, vill ha %d", version, len(migreringar))
	}
}
