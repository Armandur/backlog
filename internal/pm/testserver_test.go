package pm

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/timeutil"
)

func testserverStore(t *testing.T) (*TestserverStore, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := repo.Open(filepath.Join(dir, "backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.Run(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,alias,name,repo_path,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		ids.New(), "demo", "Demo", dir, timeutil.Now(), timeutil.Now()); err != nil {
		t.Fatal(err)
	}
	skript := filepath.Join(dir, "server.sh")
	if err := os.WriteFile(skript, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	konfig := StandardKonfig()
	konfig.Portar = PortKonfig{Fran: 18100, Till: 18199}
	konfig.Testserver = map[string]TestserverKonfig{
		"demo": {Kommando: skript, Args: []string{"{port}"}, CWD: dir},
	}
	store := NewTestserverStore(db, konfig, dir)
	store.stopptid = 500 * time.Millisecond
	return store, db, dir
}

func TestTestserverStartarSpararOchStopparProcessgrupp(t *testing.T) {
	store, db, dir := testserverStore(t)
	server, err := store.Starta(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if processgruppLever(server.PID) {
			_ = syscall.Kill(-server.PID, syscall.SIGKILL)
		}
	})
	if server.PID <= 0 || server.Port < 18100 || !server.Lever {
		t.Fatalf("felaktig server: %+v", server)
	}
	var pid, port int
	var startad int64
	var logg string
	if err := db.QueryRow(`SELECT pid,port,startad_at,logg_sokvag FROM pm_testservrar WHERE alias='demo'`).Scan(&pid, &port, &startad, &logg); err != nil {
		t.Fatal(err)
	}
	if pid != server.PID || port != server.Port || startad == 0 {
		t.Fatalf("fel databasrad: pid=%d port=%d startad=%d", pid, port, startad)
	}
	if logg != filepath.Join(dir, "loggar", "testserver-demo.log") {
		t.Fatalf("fel logg: %s", logg)
	}
	if _, err := store.Starta(context.Background(), "demo"); err == nil || !strings.Contains(err.Error(), "kör redan") {
		t.Fatalf("dubbelstart gav %v", err)
	}
	stoppad, err := store.Stoppa(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if stoppad.Lever || processgruppLever(server.PID) {
		t.Fatal("processgruppen lever efter stopp")
	}
	if err := db.QueryRow(`SELECT pid FROM pm_testservrar WHERE alias='demo'`).Scan(&pid); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("testserverraden finns kvar: %v", err)
	}
}

func TestTestserverFyllerPortOchMiljo(t *testing.T) {
	store, _, dir := testserverStore(t)
	utfil := filepath.Join(dir, "argument.txt")
	skript := filepath.Join(dir, "skriv.sh")
	if err := os.WriteFile(skript, []byte("#!/bin/sh\nprintf '%s %s' \"$1\" \"$TESTSERVER_VAR\" > \"$2\"\nsleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	k := store.konfig.Testserver["demo"]
	k.Kommando = skript
	k.Args = []string{"port-{port}", utfil}
	k.Miljo = map[string]string{"TESTSERVER_VAR": "miljovarde"}
	store.konfig.Testserver["demo"] = k
	server, err := store.Starta(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.Stoppa(context.Background(), "demo") })
	slut := time.Now().Add(time.Second)
	for time.Now().Before(slut) {
		data, err := os.ReadFile(utfil)
		if err == nil {
			vantat := "port-" + strconv.Itoa(server.Port) + " miljovarde"
			if string(data) != vantat {
				t.Fatalf("fick %q, väntade %q", data, vantat)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("servern skrev inte argument och miljö")
}

func TestTestserverStadarDodRad(t *testing.T) {
	store, db, dir := testserverStore(t)
	if _, err := db.Exec(`INSERT INTO pm_testservrar(alias,pid,port,startad_at,logg_sokvag) VALUES(?,?,?,?,?)`,
		"demo", 99999999, 18123, timeutil.Now(), filepath.Join(dir, "logg")); err != nil {
		t.Fatal(err)
	}
	antal, err := store.StadaDoda(context.Background())
	if err != nil || antal != 1 {
		t.Fatalf("städning gav %d, %v", antal, err)
	}
}

func TestStoppRorInteProcessUtanDatabasrad(t *testing.T) {
	store, _, _ := testserverStore(t)
	kommando := exec.Command("sleep", "300")
	kommando.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := kommando.Start(); err != nil {
		t.Fatal(err)
	}
	pid := kommando.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_, _ = kommando.Process.Wait()
	})
	if _, err := store.Stoppa(context.Background(), "demo"); err == nil {
		t.Fatal("stopp utan databasrad lyckades")
	}
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		t.Fatalf("processen utan databasrad rördes: %v", err)
	}
}
