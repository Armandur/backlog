package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/repo"
)

type webTask struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	DependsOn []struct {
		Seq int `json:"seq"`
	} `json:"depends_on"`
	Blocks []struct {
		Seq int `json:"seq"`
	} `json:"blocks"`
}

func do(t *testing.T, srv *Server, method, path, body string, out interface{}) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code, rec.Body.String()
}

// TestDependencyRoutes calls the task routes the web UI uses: create with
// depends_on, ready filter, PATCH add and remove, and the cycle refusal.
func TestDependencyRoutes(t *testing.T) {
	db, err := repo.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.Run(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, alias, name, created_at, updated_at) VALUES('p1','proj','Proj',1,1)`); err != nil {
		t.Fatal(err)
	}
	srv := New(db, models.Actor{Kind: models.ActorKindHuman, Name: "test"})

	var a, b, c webTask
	do(t, srv, "POST", "/api/tasks", `{"project":"proj","title":"A"}`, &a)
	code, body := do(t, srv, "POST", "/api/tasks", `{"project":"proj","title":"B","depends_on":["TASK-1"]}`, &b)
	if code != 200 || len(b.DependsOn) != 1 || b.DependsOn[0].Seq != 1 {
		t.Fatalf("create B: %d %s", code, body)
	}
	do(t, srv, "POST", "/api/tasks", `{"project":"proj","title":"C"}`, &c)

	var list struct {
		Tasks []webTask `json:"tasks"`
	}
	do(t, srv, "GET", "/api/tasks?project=proj&ready=true", "", &list)
	seqs := map[int]bool{}
	for _, task := range list.Tasks {
		seqs[task.Seq] = true
	}
	if !seqs[1] || seqs[2] || !seqs[3] {
		t.Fatalf("ready = %v, want TASK-1 and TASK-3 only", seqs)
	}
	if code, _ := do(t, srv, "GET", "/api/tasks?ready=1&status=doing", "", nil); code != 400 {
		t.Fatalf("ready with status doing = %d, want 400", code)
	}

	// PATCH by ULID, as the UI does.
	var upd webTask
	code, body = do(t, srv, "PATCH", "/api/tasks/"+c.ID, `{"add_depends_on":["TASK-2"]}`, &upd)
	if code != 200 || len(upd.DependsOn) != 1 || upd.DependsOn[0].Seq != 2 {
		t.Fatalf("add C -> B: %d %s", code, body)
	}
	code, body = do(t, srv, "PATCH", "/api/tasks/"+a.ID, `{"add_depends_on":["TASK-3"]}`, nil)
	var refusal struct {
		Error string `json:"error"`
	}
	json.Unmarshal([]byte(body), &refusal) //nolint:errcheck
	if code != 400 || !strings.Contains(refusal.Error, "TASK-1 -> TASK-3 -> TASK-2 -> TASK-1") {
		t.Fatalf("cycle: %d %s, want 400 with the chain", code, body)
	}

	var shown webTask
	do(t, srv, "GET", "/api/tasks/"+b.ID, "", &shown)
	if len(shown.Blocks) != 1 || shown.Blocks[0].Seq != 3 {
		t.Fatalf("B blocks = %+v, want TASK-3", shown.Blocks)
	}

	code, body = do(t, srv, "PATCH", "/api/tasks/"+c.ID, `{"remove_depends_on":["TASK-2"]}`, &upd)
	if code != 200 || len(upd.DependsOn) != 0 {
		t.Fatalf("remove: %d %s", code, body)
	}
}

// TestDependencyChangeLogsOnce checks that a PATCH that only adds a
// dependency logs dependency_added and no extra "updated" entry.
func TestDependencyChangeLogsOnce(t *testing.T) {
	db, err := repo.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.Run(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, alias, name, created_at, updated_at) VALUES('p1','proj','Proj',1,1)`); err != nil {
		t.Fatal(err)
	}
	srv := New(db, models.Actor{Kind: models.ActorKindHuman, Name: "test"})
	var a, b webTask
	do(t, srv, "POST", "/api/tasks", `{"project":"proj","title":"A"}`, &a)
	do(t, srv, "POST", "/api/tasks", `{"project":"proj","title":"B"}`, &b)
	if code, body := do(t, srv, "PATCH", "/api/tasks/"+b.ID, `{"add_depends_on":["TASK-1"]}`, nil); code != 200 {
		t.Fatalf("add: %d %s", code, body)
	}
	count := func(action string) int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM activity_log WHERE entity_id=? AND action=?`, b.ID, action).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	added, updated := count("dependency_added"), count("updated")
	if added != 1 || updated != 0 {
		t.Fatalf("activity: dependency_added=%d updated=%d, want 1 and 0", added, updated)
	}
}
