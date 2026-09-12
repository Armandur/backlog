package pm

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	basmigrate "github.com/mazen160/backlog/internal/migrate"
	_ "modernc.org/sqlite"
)

func TestLasMigreringarAnvanderFilernasVersioner(t *testing.T) {
	filer := fstest.MapFS{
		"migrations/0002_andra.sql":  {Data: []byte("SELECT 2")},
		"migrations/0001_forsta.sql": {Data: []byte("SELECT 1")},
	}

	migreringar, err := lasMigreringar(filer, "migrations")
	if err != nil {
		t.Fatalf("läs migreringar: %v", err)
	}
	if len(migreringar) != 2 {
		t.Fatalf("väntade två migreringar, fick %d", len(migreringar))
	}
	if migreringar[0].version != 1 || migreringar[1].version != 2 {
		t.Fatalf("fel versioner: %+v", migreringar)
	}
}

func TestLasMigreringarAvvisarTrasigKedja(t *testing.T) {
	testfall := []struct {
		namn  string
		filer fstest.MapFS
		fel   string
	}{
		{
			namn:  "lucka",
			filer: migrationsfiler("0001_forsta.sql", "0003_tredje.sql"),
			fel:   "lucka",
		},
		{
			namn:  "dublett",
			filer: migrationsfiler("0001_forsta.sql", "0001_annan.sql"),
			fel:   "dubbel",
		},
		{
			namn:  "ogiltigt namn",
			filer: migrationsfiler("forsta.sql"),
			fel:   "ogiltigt migrationsfilnamn",
		},
		{
			namn:  "version noll",
			filer: migrationsfiler("0000_noll.sql"),
			fel:   "ogiltig migrationsversion",
		},
	}

	for _, test := range testfall {
		t.Run(test.namn, func(t *testing.T) {
			_, err := lasMigreringar(test.filer, "migrations")
			if err == nil || !strings.Contains(err.Error(), test.fel) {
				t.Fatalf("väntade fel med %q, fick %v", test.fel, err)
			}
		})
	}
}

func TestUpstreamAntaletKommerFranUpstreamSjalv(t *testing.T) {
	// Talet får inte stå som en siffra i PM, då blir det fel när forken
	// hämtar nya migreringar från upstream.
	antal, err := basmigrate.Antal()
	if err != nil {
		t.Fatal(err)
	}
	if antal < 1 {
		t.Fatalf("upstream rapporterade %d migreringar", antal)
	}
}
func TestMigrateAvvisarNyareSchemaversionUtanSkrivning(t *testing.T) {
	pmMigreringar, err := lasMigreringar(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("läs PM-migreringar: %v", err)
	}
	testfall := []struct {
		namn    string
		nyckel  string
		version int
	}{
		{namn: "upstream", nyckel: upstreamSchemaKey, version: upstreamAntalForProv(t) + 1},
		{namn: "PM", nyckel: schemaKey, version: len(pmMigreringar) + 1},
	}

	for _, test := range testfall {
		t.Run(test.namn, func(t *testing.T) {
			db := migrationsdatabas(t)
			if _, err := db.Exec(`CREATE TABLE schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
				t.Fatalf("skapa schema_meta: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO schema_meta(key, value) VALUES(?, ?)`, test.nyckel, test.version); err != nil {
				t.Fatalf("sätt version: %v", err)
			}

			err := Migrate(db)
			if err == nil {
				t.Fatal("väntade att en nyare databas avvisades")
			}
			if !strings.Contains(err.Error(), "Installera en nyare PM-version") || !strings.Contains(err.Error(), "återställ en kompatibel backup") {
				t.Fatalf("otydligt besked: %v", err)
			}
			var version int
			if err := db.QueryRow(`SELECT value FROM schema_meta WHERE key=?`, test.nyckel).Scan(&version); err != nil {
				t.Fatalf("läs kvarvarande version: %v", err)
			}
			if version != test.version {
				t.Fatalf("versionen ändrades från %d till %d", test.version, version)
			}
			var pmTabeller int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE 'pm_%'`).Scan(&pmTabeller); err != nil {
				t.Fatalf("räkna PM-tabeller: %v", err)
			}
			if pmTabeller != 0 {
				t.Fatalf("migreringen skrev %d PM-tabeller före spärren", pmTabeller)
			}
		})
	}
}

func migrationsfiler(namn ...string) fstest.MapFS {
	filer := make(fstest.MapFS, len(namn))
	for _, filnamn := range namn {
		filer["migrations/"+filnamn] = &fstest.MapFile{Data: []byte("SELECT 1")}
	}
	return filer
}

func migrationsdatabas(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "backlog.db"))
	if err != nil {
		t.Fatalf("öppna databas: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func upstreamAntalForProv(t *testing.T) int {
	t.Helper()
	antal, err := basmigrate.Antal()
	if err != nil {
		t.Fatal(err)
	}
	return antal
}
