package pm

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// schemaKey är PM-lagrets egen versionsnyckel i schema_meta. Upstreams
// migreringar räknar på schema_version och får inte kollidera med denna.
const schemaKey = "pm_schema_version"

// Migrate kör PM-migreringarna. Bara backlog-pm anropar den, så vardagsdatabasen
// får aldrig PM-tabellerna.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("pm-migrering: schema_meta: %w", err)
	}
	var version int
	if err := db.QueryRow(`SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM schema_meta WHERE key=?), 0)`, schemaKey).Scan(&version); err != nil {
		return fmt.Errorf("pm-migrering: läs version: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("pm-migrering: läs katalog: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for i, name := range names {
		ver := i + 1
		if ver <= version {
			continue
		}
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("pm-migrering: läs %s: %w", name, err)
		}
		if err := execMigration(db, string(content), ver); err != nil {
			return fmt.Errorf("pm-migrering: kör %s: %w", name, err)
		}
	}
	return nil
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
	if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_meta(key,value) VALUES(?,?)`, schemaKey, fmt.Sprintf("%d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
