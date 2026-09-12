package pm

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initIMapp kör init mot en egen katalog och profil, så testet aldrig rör
// riktiga profiler.
func initIMapp(t *testing.T, profil, katalog string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cmd := NewInitCmd()
	ut := &bytes.Buffer{}
	cmd.SetOut(ut)
	cmd.SetErr(ut)
	cmd.SetArgs([]string{"--profile", profil, "--path", katalog})
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
