package pm

import (
	"database/sql"
	"fmt"
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

func skrivProjektDB(t *testing.T, dir string, alias ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE projects (alias TEXT, archived_at INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, namn := range alias {
		if _, err := db.Exec("INSERT INTO projects(alias) VALUES(?)", namn); err != nil {
			t.Fatal(err)
		}
	}
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
	if len(k.Testserver) != 0 {
		t.Fatal("standardkonfigurationen ska sakna testservrar")
	}
	if k.Portar.Fran != 8100 || k.Portar.Till != 8199 {
		t.Fatalf("standardintervallet ska vara 8100 till 8199: %+v", k.Portar)
	}
}

func TestLasKonfigLaserPortintervall(t *testing.T) {
	dir := skrivKonfig(t, `
[portar]
fran = 9200
till = 9299
`)
	k, err := LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if k.Portar.Fran != 9200 || k.Portar.Till != 9299 {
		t.Fatalf("konfigurationen gav fel portintervall: %+v", k.Portar)
	}
}

func TestKonfigAvvisarOgiltigtPortintervall(t *testing.T) {
	dir := skrivKonfig(t, `
[portar]
fran = 9000
till = 8000
`)
	_, err := LasKonfig(dir)
	if err == nil || !strings.Contains(err.Error(), "portintervallet") {
		t.Fatalf("PM godtog ett ogiltigt portintervall: %v", err)
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

func TestLasKonfigLaserTestserver(t *testing.T) {
	cwd := t.TempDir()
	dir := skrivKonfig(t, fmt.Sprintf(`
[testserver.demo]
kommando = "go"
args = ["run", ".", "--port", "{port}"]
cwd = %q
port = 8123
halsa = "/health"

[testserver.demo.miljo]
APP_ENV = "test"
`, cwd))
	skrivProjektDB(t, dir, "demo")

	k, err := LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	server := k.Testserver["demo"]
	if server.Kommando != "go" || server.CWD != cwd || server.Port != 8123 || server.Halsa != "/health" {
		t.Fatalf("testservern lästes fel: %+v", server)
	}
	if len(server.Args) != 4 || server.Miljo["APP_ENV"] != "test" {
		t.Fatalf("testserverns args eller miljö lästes fel: %+v", server)
	}
}

func TestTestserverValideringFangarFel(t *testing.T) {
	saknadCWD := filepath.Join(t.TempDir(), "katalog-som-saknas")
	fall := []struct {
		namn    string
		server  TestserverKonfig
		alias   string
		feltext string
	}{
		{namn: "kommando saknas", alias: "demo", server: TestserverKonfig{Args: []string{"{port}"}}, feltext: "saknar kommando"},
		{namn: "port saknas", alias: "demo", server: TestserverKonfig{Kommando: "go"}, feltext: "saknar både fast port och {port}"},
		{namn: "cwd saknas", alias: "demo", server: TestserverKonfig{Kommando: "go", Args: []string{"{port}"}, CWD: saknadCWD}, feltext: saknadCWD},
		{namn: "alias saknas", alias: "okant", server: TestserverKonfig{Kommando: "go", Args: []string{"{port}"}}, feltext: "projektet \"okant\" som inte finns"},
	}
	for _, fall := range fall {
		t.Run(fall.namn, func(t *testing.T) {
			k := StandardKonfig()
			k.Testserver = map[string]TestserverKonfig{fall.alias: fall.server}
			err := k.Validera("demo")
			if err == nil || !strings.Contains(err.Error(), fall.feltext) {
				t.Fatalf("väntade fel med %q, fick %v", fall.feltext, err)
			}
		})
	}
}

// Skrivningen avvisar ett alias som inte finns, men läsningen släpper igenom
// det. Annars går ett kvarglömt block för ett raderat projekt inte att ta bort
// i konfigvyn, för vyn kan då inte ens läsa konfigurationen.
func TestSkrivKonfigAvvisarOkantProjektaliasMenLasKonfigSlapperIgenom(t *testing.T) {
	dir := skrivKonfig(t, `
[testserver.okant]
kommando = "go"
args = ["--port", "{port}"]
`)
	skrivProjektDB(t, dir, "demo")

	k, err := LasKonfig(dir)
	if err != nil {
		t.Fatalf("läsningen spärrades av ett kvarglömt block: %v", err)
	}
	if _, finns := k.Testserver["okant"]; !finns {
		t.Fatal("blocket försvann vid läsningen, då går det inte att ta bort i vyn")
	}

	if err := SkrivKonfig(dir, k); err == nil || !strings.Contains(err.Error(), "okant") {
		t.Fatalf("väntade fel om okänt projektalias vid skrivning, fick %v", err)
	}
}

func TestTestserverRundtripp(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	skrivProjektDB(t, dir, "demo")
	fore := StandardKonfig()
	fore.Testserver = map[string]TestserverKonfig{
		"demo": {
			Kommando: "npm", Args: []string{"run", "dev", "--", "--port={port}"},
			CWD: cwd, Halsa: "/status", Miljo: map[string]string{"NODE_ENV": "test"},
		},
	}
	if err := SkrivKonfig(dir, fore); err != nil {
		t.Fatal(err)
	}
	efter, err := LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	server := efter.Testserver["demo"]
	if server.Kommando != "npm" || server.CWD != cwd || server.Halsa != "/status" {
		t.Fatalf("rundtrippen ändrade testservern: %+v", server)
	}
	if strings.Join(server.Args, "|") != "run|dev|--|--port={port}" || server.Miljo["NODE_ENV"] != "test" {
		t.Fatalf("rundtrippen ändrade args eller miljö: %+v", server)
	}
	data, err := os.ReadFile(filepath.Join(dir, KonfigFil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[testserver.demo]") {
		t.Fatalf("pm.toml saknar testserversektionen: %s", data)
	}
}

func TestAterstallMaskeratTarBortOkandMaskering(t *testing.T) {
	// En nyckel som bara finns som maskering har inget sparat värde. Den får
	// inte bli kvar, för då sparar PM maskeringen som om den vore hemligheten.
	inkommande := Konfig{Agenter: map[string]AgentKonfig{
		"claude": {Miljo: map[string]string{"NY": MaskeratVarde, "GAMMAL": MaskeratVarde, "EGEN": "eget"}},
	}}
	sparad := Konfig{Agenter: map[string]AgentKonfig{
		"claude": {Miljo: map[string]string{"GAMMAL": "hemlig"}},
	}}

	ut := inkommande.AterstallMaskerat(sparad)
	miljo := ut.Agenter["claude"].Miljo
	if miljo["GAMMAL"] != "hemlig" {
		t.Fatalf("den sparade hemligheten kom inte tillbaka: %+v", miljo)
	}
	if miljo["EGEN"] != "eget" {
		t.Fatalf("ett eget värde försvann: %+v", miljo)
	}
	if _, finns := miljo["NY"]; finns {
		t.Fatalf("en maskering utan sparat värde blev kvar: %+v", miljo)
	}
}

func TestMaskeraRorInteOriginalet(t *testing.T) {
	konfig := Konfig{
		Agenter: map[string]AgentKonfig{"claude": {Miljo: map[string]string{"NYCKEL": "hemlig"}}},
		Krok:    Krok{Miljo: map[string]string{"KROK": "hemlig"}},
	}

	maskerad := konfig.Maskera()
	if maskerad.Agenter["claude"].Miljo["NYCKEL"] != MaskeratVarde {
		t.Fatalf("värdet maskerades inte: %+v", maskerad.Agenter["claude"].Miljo)
	}
	if konfig.Agenter["claude"].Miljo["NYCKEL"] != "hemlig" {
		t.Fatalf("maskeringen ändrade originalet: %+v", konfig.Agenter["claude"].Miljo)
	}
	if konfig.Krok.Miljo["KROK"] != "hemlig" {
		t.Fatalf("maskeringen ändrade krokens original: %+v", konfig.Krok.Miljo)
	}
}
