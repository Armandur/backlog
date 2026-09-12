package pm

import (
	"context"
	"database/sql"
	basmigrate "github.com/mazen160/backlog/internal/migrate"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/timeutil"
)

// TestDB ger en PM-databas: upstreams schema plus PM-migreringarna.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := repo.Open(filepath.Join(t.TempDir(), "backlog.db"))
	if err != nil {
		t.Fatalf("öppna db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.Run(db); err != nil {
		t.Fatalf("upstream-migrering: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("pm-migrering: %v", err)
	}
	return db
}

func testProjekt(t *testing.T, db *sql.DB, alias string) string {
	t.Helper()
	id := ids.New()
	nu := timeutil.Now()
	_, err := db.Exec(`INSERT INTO projects(id, alias, name, created_at, updated_at) VALUES(?,?,?,?,?)`,
		id, alias, alias, nu, nu)
	if err != nil {
		t.Fatalf("skapa projekt: %v", err)
	}
	return id
}

func TestMigrateSkaparSamtalstabellenOchEgenVersion(t *testing.T) {
	db := testDB(t)
	var namn string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='pm_samtal'`).Scan(&namn); err != nil {
		t.Fatalf("pm_samtal saknas: %v", err)
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM schema_meta WHERE key='pm_schema_version'`).Scan(&v); err != nil {
		t.Fatalf("pm_schema_version saknas: %v", err)
	}
	// Versionen följer antalet PM-migreringar.
	if v == "0" {
		t.Fatalf("pm_schema_version kördes inte, fick %q", v)
	}
	// Upstreams version får inte ha rubbats av PM-migreringen. Talen kan vara
	// lika när kedjorna råkar vara lika långa, så provet jämför mot upstreams
	// egen räkning i stället för mot PM:s tal.
	var upstream int
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM schema_meta WHERE key='schema_version'`).Scan(&upstream); err != nil {
		t.Fatalf("schema_version saknas: %v", err)
	}
	upstreamAntal, err := basmigrate.Antal()
	if err != nil {
		t.Fatal(err)
	}
	if upstream != upstreamAntal {
		t.Fatalf("upstreams version är %d, men kedjan har %d migreringar", upstream, upstreamAntal)
	}
}

func TestMigrateArIdempotent(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("andra körningen: %v", err)
	}
}

func TestAddOchListGerInlaggIOrdning(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	store := NewSamtalStore(db)
	ctx := context.Background()

	for _, text := range []string{"ett", "två", "tre"} {
		if _, err := store.Add(ctx, pid, "", models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, text); err != nil {
			t.Fatalf("add %s: %v", text, err)
		}
	}
	poster, err := store.List(ctx, pid, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(poster) != 3 || poster[0].Text != "ett" || poster[2].Text != "tre" {
		t.Fatalf("väntade ett, två, tre i ordning, fick %+v", poster)
	}
	if poster[0].Actor.Kind != models.ActorKindHuman || poster[0].Actor.Name != "rasmus" {
		t.Fatalf("aktören följde inte med: %+v", poster[0].Actor)
	}
	if poster[0].CreatedAt == 0 {
		t.Fatal("tidsstämpeln saknas")
	}

	senaste, err := store.List(ctx, pid, 2)
	if err != nil {
		t.Fatalf("list med limit: %v", err)
	}
	if len(senaste) != 2 || senaste[0].Text != "två" || senaste[1].Text != "tre" {
		t.Fatalf("--limit 2 ska ge de två senaste i stigande ordning, fick %+v", senaste)
	}
}

func TestAddAvvisarTomTextOchOkandAktor(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	store := NewSamtalStore(db)
	ctx := context.Background()

	if _, err := store.Add(ctx, pid, "", models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, "   "); err == nil {
		t.Fatal("tom text sparades")
	}
	if _, err := store.Add(ctx, pid, "", models.Actor{Kind: "robot", Name: "x"}, "hej"); err == nil {
		t.Fatal("okänd aktörstyp sparades")
	}
}

func TestProjectIDByAliasGerSvensktFel(t *testing.T) {
	db := testDB(t)
	store := NewSamtalStore(db)
	_, err := store.ProjectIDByAlias(context.Background(), "finns-inte")
	if err == nil || !strings.Contains(err.Error(), "finns inte i PM-workspacet") {
		t.Fatalf("väntade svenskt fel om okänt projekt, fick: %v", err)
	}
}

func TestParseActor(t *testing.T) {
	a, err := ParseActor("ai:claude-opus-5")
	if err != nil || a.Kind != models.ActorKindAI || a.Name != "claude-opus-5" {
		t.Fatalf("ai-aktör tolkades fel: %+v %v", a, err)
	}
	for _, dalig := range []string{"", "rasmus", "robot:x", "human:"} {
		if _, err := ParseActor(dalig); err == nil {
			t.Fatalf("aktören %q godtogs", dalig)
		}
	}
}
