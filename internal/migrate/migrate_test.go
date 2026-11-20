package migrate

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDependencyMigrationKeepsExistingData builds a database at schema
// version 8, fills it with rows, runs the migrations and checks that the
// rows are untouched and that task_dependencies exists afterwards.
func TestDependencyMigrationKeepsExistingData(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v8.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := ensureMeta(db); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) < 9 || names[8] != "0009_task_dependencies.sql" {
		t.Fatalf("expected 0009_task_dependencies.sql as the ninth migration, got %v", names)
	}
	for i, name := range names[:8] {
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := execMigration(db, string(content), i+1); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	seed := []string{
		`INSERT INTO projects(id,alias,name,created_at,updated_at) VALUES('p1','proj','Proj',1,1)`,
		`INSERT INTO tasks(id,project_id,title,status,created_at,updated_at,task_seq) VALUES('t1','p1','First','todo',1,1,1)`,
		`INSERT INTO tasks(id,project_id,title,status,created_at,updated_at,task_seq) VALUES('t2','p1','Second','done',1,1,2)`,
		`INSERT INTO labels(id,project_id,name) VALUES('l1','p1','bug')`,
		`INSERT INTO task_labels(task_id,label_id) VALUES('t1','l1')`,
		`INSERT INTO comments(id,task_id,body,created_at) VALUES('c1','t1','hello',1)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	before := snapshot(t, db)

	if err := Run(db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	v, err := getVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != len(names) {
		t.Fatalf("schema_version = %d, want %d", v, len(names))
	}
	if after := snapshot(t, db); after != before {
		t.Fatalf("rows changed by migration:\nbefore %s\nafter  %s", before, after)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_dependencies`).Scan(&n); err != nil {
		t.Fatalf("task_dependencies missing after migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task_dependencies(task_id,depends_on_id,created_at) VALUES('t1','t2',1)`); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task_dependencies(task_id,depends_on_id,created_at) VALUES('t1','t1',1)`); err == nil {
		t.Fatal("self-dependency was accepted, want CHECK failure")
	}
}

func snapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var s string
	err := db.QueryRow(`SELECT
		(SELECT group_concat(id||':'||alias||':'||name, ',') FROM projects) || '|' ||
		(SELECT group_concat(id||':'||title||':'||status||':'||task_seq, ',') FROM (SELECT * FROM tasks ORDER BY id)) || '|' ||
		(SELECT group_concat(id||':'||name, ',') FROM labels) || '|' ||
		(SELECT group_concat(task_id||':'||label_id, ',') FROM task_labels) || '|' ||
		(SELECT group_concat(id||':'||body, ',') FROM comments)`).Scan(&s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
