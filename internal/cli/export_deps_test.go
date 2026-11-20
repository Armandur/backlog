package cli

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDependenciesSurviveExportAndImport makes B depend on A, checks that
// every export format names the dependency, imports the database into a new
// one and expects B to still depend on A there.
func TestDependenciesSurviveExportAndImport(t *testing.T) {
	src := cliEnv(t)
	srcDB := src[1]
	for _, args := range [][]string{
		{"task", "add", "--title", "A", "--project", "proj"},
		{"task", "add", "--title", "B", "--project", "proj", "--depends-on", "TASK-1"},
	} {
		if _, err := runCLI(t, append(src, args...)...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	for format, want := range map[string]string{
		"json": `"depends_on": [`,
		"csv":  "depends_on",
		"md":   "- Depends on: TASK-1",
	} {
		out, err := runCLI(t, append(src, "export", "--format", format, "--project", "proj")...)
		if err != nil {
			t.Fatalf("export %s: %v", format, err)
		}
		if !strings.Contains(out, want) {
			t.Fatalf("export %s lacks %q:\n%s", format, want, out)
		}
		if format != "md" && !strings.Contains(out, "TASK-1") && !strings.Contains(out, `"seq": 1`) {
			t.Fatalf("export %s does not name TASK-1:\n%s", format, out)
		}
	}

	dst := []string{"--db", filepath.Join(t.TempDir(), "dst.db"), "--as", "ai:test"}
	out, err := runCLI(t, append(dst, "import", srcDB)...)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(out, "1 dependencies") {
		t.Fatalf("import summary = %q, want 1 dependencies", out)
	}

	// The import numbers tasks anew, so find A and B by title.
	var list struct {
		Tasks []struct {
			Seq   int    `json:"seq"`
			Title string `json:"title"`
		} `json:"tasks"`
	}
	runJSON(t, dst, &list, "task", "list", "--project", "proj")
	if len(list.Tasks) != 2 {
		t.Fatalf("imported %d tasks, want 2", len(list.Tasks))
	}
	for _, task := range list.Tasks {
		if task.Title == "B" {
			var b struct {
				DependsOn []struct {
					Title string `json:"title"`
				} `json:"depends_on"`
			}
			runJSON(t, dst, &b, "task", "show", "TASK-"+strconv.Itoa(task.Seq))
			if len(b.DependsOn) != 1 || b.DependsOn[0].Title != "A" {
				t.Fatalf("imported B depends_on = %+v, want A", b.DependsOn)
			}
			return
		}
	}
	t.Fatal("task B missing after import")
}

// TestImportFromDatabaseWithoutDependencyTable imports from a database that
// predates migration 0009. The import must still work.
func TestImportFromDatabaseWithoutDependencyTable(t *testing.T) {
	src := cliEnv(t)
	if _, err := runCLI(t, append(src, "task", "add", "--title", "A", "--project", "proj")...); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", src[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE task_dependencies`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	dst := []string{"--db", filepath.Join(t.TempDir(), "dst.db"), "--as", "ai:test"}
	out, err := runCLI(t, append(dst, "import", src[1])...)
	if err != nil {
		t.Fatalf("import from old database: %v", err)
	}
	if !strings.Contains(out, "1 tasks") || !strings.Contains(out, "0 dependencies") {
		t.Fatalf("import summary = %q", out)
	}
}
