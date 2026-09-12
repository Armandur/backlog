package pm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func briefUnderlag(t *testing.T, alias string) (*TestserverStore, TaskFakta) {
	t.Helper()
	db := testDB(t)
	repo := t.TempDir()
	projectID := projektMedRepo(t, db, alias, repo)
	fakta := TaskFakta{
		Ref: "TASK-1", Titel: "Testa", Beskrivning: "Kontrollera funktionen.",
		Typ: "task", ProjectID: projectID, RepoPath: repo,
	}
	workspace := filepath.Dir(databasSokvag(t, db))
	konfig := StandardKonfig()
	konfig.Testserver = map[string]TestserverKonfig{
		alias: {Kommando: "sh", Args: []string{"-c", "sleep 30", "{port}"}},
	}
	return NewTestserverStore(db, konfig, workspace), fakta
}

func databasSokvag(t *testing.T, db interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}) string {
	t.Helper()
	var sekvens int
	var namn, sokvag string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&sekvens, &namn, &sokvag); err != nil {
		t.Fatalf("läs databassökväg: %v", err)
	}
	return sokvag
}

func TestByggBriefBeskriverSaknadTestserverkonfiguration(t *testing.T) {
	store, fakta := briefUnderlag(t, "demo")
	brief, err := ByggBrief(context.Background(), store.db, fakta)
	if err != nil {
		t.Fatalf("bygg brief: %v", err)
	}
	for _, vantat := range []string{
		"## Testserver",
		"PM äger testservern men saknar konfiguration för demo",
		"Välj ingen port",
		"Föreslå ett lämpligt startkommando i rapporten",
		"Gissa inte och starta ingen egen bakgrundsprocess",
	} {
		if !strings.Contains(brief, vantat) {
			t.Fatalf("briefen saknar %q:\n%s", vantat, brief)
		}
	}
}

func TestByggBriefBeskriverKonfigureradOchKorandeTestserver(t *testing.T) {
	store, fakta := briefUnderlag(t, "demo")
	if err := SkrivKonfig(store.workspace, store.konfig); err != nil {
		t.Fatalf("skriv konfiguration: %v", err)
	}

	brief, err := ByggBrief(context.Background(), store.db, fakta)
	if err != nil {
		t.Fatalf("bygg brief: %v", err)
	}
	for _, vantat := range []string{
		"PM äger testservern",
		"Välj ingen port",
		"testserver_start",
		"backlog-pm testserver start demo",
		"starta ingen egen bakgrundsprocess",
	} {
		if !strings.Contains(brief, vantat) {
			t.Fatalf("briefen saknar %q:\n%s", vantat, brief)
		}
	}

	server, err := store.Starta(context.Background(), "demo")
	if err != nil {
		t.Fatalf("starta testserver: %v", err)
	}
	t.Cleanup(func() {
		if server.Lever {
			_, _ = store.Stoppa(context.Background(), "demo")
		}
	})
	brief, err = ByggBrief(context.Background(), store.db, fakta)
	if err != nil {
		t.Fatalf("bygg brief med körande server: %v", err)
	}
	vard, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	adress := fmt.Sprintf("http://%s:%d/", vard, server.Port)
	if !strings.Contains(brief, adress) {
		t.Fatalf("briefen saknar adressen %q:\n%s", adress, brief)
	}
}

func TestTestserverMCPVerktygenAnroparStore(t *testing.T) {
	store, _ := briefUnderlag(t, "demo")
	extension := TestserverMCP(func() (*TestserverStore, error) { return store, nil })
	if len(extension.Tools) != 3 {
		t.Fatalf("väntade tre verktyg, fick %d", len(extension.Tools))
	}
	args := map[string]interface{}{"project": "demo"}

	if _, err := extension.Handlers["testserver_start"](context.Background(), args); err != nil {
		t.Fatalf("testserver_start: %v", err)
	}
	t.Cleanup(func() { _, _ = store.Stoppa(context.Background(), "demo") })
	if _, err := extension.Handlers["testserver_start"](context.Background(), args); err == nil || !strings.Contains(err.Error(), "kör redan") {
		t.Fatalf("dubbelstart ska ge svenskt fel, fick %v", err)
	}
	if _, err := extension.Handlers["testserver_status"](context.Background(), args); err != nil {
		t.Fatalf("testserver_status: %v", err)
	}
	if _, err := extension.Handlers["testserver_stop"](context.Background(), args); err != nil {
		t.Fatalf("testserver_stop: %v", err)
	}
}

func TestTestserverMCPAvvisarSaknadKonfiguration(t *testing.T) {
	store, _ := briefUnderlag(t, "demo")
	store.konfig.Testserver = nil
	extension := TestserverMCP(func() (*TestserverStore, error) { return store, nil })
	_, err := extension.Handlers["testserver_start"](
		context.Background(), map[string]interface{}{"project": "demo"},
	)
	if err == nil || !strings.Contains(err.Error(), "saknar konfiguration för testserver") {
		t.Fatalf("väntade begripligt konfigurationsfel, fick %v", err)
	}
}
