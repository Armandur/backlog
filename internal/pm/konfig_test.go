package pm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skrivKonfig(t *testing.T, innehall string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KonfigFil), []byte(innehall), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLasKonfigUtanFilGerStandard(t *testing.T) {
	k, err := LasKonfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if k.DefaultAgent != "claude" {
		t.Fatalf("default-agenten skulle vara claude, är %q", k.DefaultAgent)
	}
	if _, finns := k.Agenter["codex"]; !finns {
		t.Fatal("codex saknas i standardkonfigurationen")
	}
	if k.Agenter["codex"].Stdin != "devnull" {
		t.Fatal("codex måste köras med stängd stdin")
	}
}

// En tredje agent ska gå att lägga till utan kodändring.
func TestTredjeAgentFranKonfig(t *testing.T) {
	dir := skrivKonfig(t, `
default_agent = "test"

[agenter.test]
kommando = "/bin/sh"
args = ["-c", "echo {brief}"]
brief = "arg"
svar = "stdout"

[[regler]]
namn = "allt till test"
agent = "test"
`)
	k, err := LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := FranKonfig(k)
	agent, err := reg.Hamta("")
	if err != nil || agent.Namn() != "test" {
		t.Fatalf("förvalet skulle vara test-agenten, fick %v %v", agent, err)
	}
	if _, err := reg.HamtaKorare("test"); err != nil {
		t.Fatalf("konfigagenten ska kunna köra tasks: %v", err)
	}
	val, err := ValjAgent(k, TaskFakta{Typ: "task"}, "")
	if err != nil || val.Agent != "test" {
		t.Fatalf("den villkorslösa regeln skulle välja test, fick %+v %v", val, err)
	}
}

func TestKonfigValideringFangarFel(t *testing.T) {
	fall := map[string]string{
		"agent utan kommando":             "[agenter.x]\nargs = [\"{brief}\"]\n",
		"okänt brief-läge":                "[agenter.x]\nkommando = \"sh\"\nbrief = \"telepati\"\nargs = [\"{brief}\"]\n",
		"svar från fil utan platshållare": "[agenter.x]\nkommando = \"sh\"\nsvar = \"fil\"\nargs = [\"{brief}\"]\n",
		"regel mot okänd agent":           "[agenter.x]\nkommando = \"sh\"\nargs = [\"{brief}\"]\n\n[[regler]]\nagent = \"saknas\"\n",
	}
	for namn, innehall := range fall {
		if _, err := LasKonfig(skrivKonfig(t, innehall)); err == nil {
			t.Fatalf("%s godtogs", namn)
		}
	}
}

func TestTrasigKonfigGerTydligtFel(t *testing.T) {
	_, err := LasKonfig(skrivKonfig(t, "det här är inte toml ["))
	if err == nil || !strings.Contains(err.Error(), "trasig") {
		t.Fatalf("väntade tydligt fel om trasig konfig, fick %v", err)
	}
}
