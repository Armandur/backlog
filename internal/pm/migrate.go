package pm

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	basmigrate "github.com/mazen160/backlog/internal/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	// schemaKey är PM-lagrets egen nyckel. Upstream använder en separat nyckel.
	schemaKey         = "pm_schema_version"
	upstreamSchemaKey = "schema_version"
)

var migrationsnamn = regexp.MustCompile(`^([0-9]{4})_[a-z0-9][a-z0-9_]*\.sql$`)

type migrering struct {
	namn     string
	version  int
	innehall string
}

// Migrate kör PM-migreringarna. Bara backlog-pm anropar den, så vardagsdatabasen
// får aldrig PM-tabellerna.
func Migrate(db *sql.DB) error {
	migreringar, err := lasMigreringar(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("pm-migrering: %w", err)
	}

	versioner, err := lasSchemaversioner(db)
	if err != nil {
		return fmt.Errorf("pm-migrering: läs versioner: %w", err)
	}
	// Antalet frågas av upstream i stället för att stå som en siffra här.
	// En hårdkodad siffra blir tyst fel så fort forken hämtar nya migreringar.
	upstreamAntal, err := basmigrate.Antal()
	if err != nil {
		return fmt.Errorf("pm-migrering: läs upstreams migreringar: %w", err)
	}
	if err := kontrolleraMaxversion(upstreamSchemaKey, versioner[upstreamSchemaKey], upstreamAntal); err != nil {
		return err
	}
	if err := kontrolleraMaxversion(schemaKey, versioner[schemaKey], len(migreringar)); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("pm-migrering: schema_meta: %w", err)
	}
	for _, migration := range migreringar {
		if migration.version <= versioner[schemaKey] {
			continue
		}
		if err := execMigration(db, migration.innehall, migration.version); err != nil {
			return fmt.Errorf("pm-migrering: kör %s: %w", migration.namn, err)
		}
	}
	return nil
}

func lasMigreringar(filsystem fs.FS, katalog string) ([]migrering, error) {
	poster, err := fs.ReadDir(filsystem, katalog)
	if err != nil {
		return nil, fmt.Errorf("läs katalog: %w", err)
	}

	migreringar := make([]migrering, 0, len(poster))
	for _, post := range poster {
		if post.IsDir() {
			continue
		}
		traff := migrationsnamn.FindStringSubmatch(post.Name())
		if traff == nil {
			return nil, fmt.Errorf("ogiltigt migrationsfilnamn %q", post.Name())
		}
		version, err := strconv.Atoi(traff[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("ogiltig migrationsversion i %q", post.Name())
		}
		innehall, err := fs.ReadFile(filsystem, katalog+"/"+post.Name())
		if err != nil {
			return nil, fmt.Errorf("läs %s: %w", post.Name(), err)
		}
		migreringar = append(migreringar, migrering{
			namn: post.Name(), version: version, innehall: string(innehall),
		})
	}

	sort.Slice(migreringar, func(i, j int) bool {
		return migreringar[i].version < migreringar[j].version
	})
	for i, migration := range migreringar {
		vantad := i + 1
		if migration.version == vantad {
			continue
		}
		if i > 0 && migration.version == migreringar[i-1].version {
			return nil, fmt.Errorf("dubbel migrationsversion %d", migration.version)
		}
		return nil, fmt.Errorf("lucka i migrationskedjan: väntade version %d, fick %d", vantad, migration.version)
	}
	return migreringar, nil
}

func lasSchemaversioner(db *sql.DB) (map[string]int, error) {
	versioner := map[string]int{upstreamSchemaKey: 0, schemaKey: 0}
	var tabellFinns bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_meta')`).Scan(&tabellFinns); err != nil {
		return nil, err
	}
	if !tabellFinns {
		return versioner, nil
	}

	rader, err := db.Query(`SELECT key, value FROM schema_meta WHERE key IN (?, ?)`, upstreamSchemaKey, schemaKey)
	if err != nil {
		return nil, err
	}
	defer rader.Close()
	for rader.Next() {
		var nyckel, varde string
		if err := rader.Scan(&nyckel, &varde); err != nil {
			return nil, err
		}
		version, err := strconv.Atoi(varde)
		if err != nil || version < 0 {
			return nil, fmt.Errorf("%s har ogiltig version %q", nyckel, varde)
		}
		versioner[nyckel] = version
	}
	return versioner, rader.Err()
}

func kontrolleraMaxversion(nyckel string, lagrad, max int) error {
	if lagrad <= max {
		return nil
	}
	return fmt.Errorf(
		"databasen använder %s %d, men denna PM-version stöder högst %d. Installera en nyare PM-version eller återställ en kompatibel backup",
		nyckel, lagrad, max,
	)
}

func execMigration(db *sql.DB, stmt string, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(stmt); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_meta(key,value) VALUES(?,?)`, schemaKey, strconv.Itoa(version)); err != nil {
		return err
	}
	return tx.Commit()
}
