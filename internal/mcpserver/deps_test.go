package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/repo"
)

// call sends one tools/call over the stdio protocol and returns the decoded
// task JSON from the text content, or the JSON-RPC error message.
func call(t *testing.T, srv *server, name string, args map[string]interface{}, out interface{}) string {
	t.Helper()
	req, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": name, "arguments": args},
	})
	var buf bytes.Buffer
	srv.r = bufio.NewReader(strings.NewReader(string(req) + "\n"))
	srv.w = &buf
	srv.run()
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("%s: bad response %q: %v", name, buf.String(), err)
	}
	if resp.Error != nil {
		return resp.Error.Message
	}
	if out != nil {
		if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), out); err != nil {
			t.Fatalf("%s: decode %q: %v", name, resp.Result.Content[0].Text, err)
		}
	}
	return ""
}

type mcpTask struct {
	Seq       int `json:"seq"`
	DependsOn []struct {
		Seq int `json:"seq"`
	} `json:"depends_on"`
	Blocks []struct {
		Seq int `json:"seq"`
	} `json:"blocks"`
}

// TestDependencyToolsOverMCP drives the dependency arguments through
// tools/call: create with depends_on, ready list, cycle refusal and removal.
func TestDependencyToolsOverMCP(t *testing.T) {
	db, err := repo.Open(filepath.Join(t.TempDir(), "mcp.db"))
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
	srv := &server{db: db, actor: models.Actor{Kind: models.ActorKindAI, Name: "test"}}

	var a, b mcpTask
	if msg := call(t, srv, "task_create", map[string]interface{}{"project": "proj", "title": "A"}, &a); msg != "" {
		t.Fatal(msg)
	}
	if msg := call(t, srv, "task_create", map[string]interface{}{"project": "proj", "title": "B", "depends_on": []string{"TASK-1"}}, &b); msg != "" {
		t.Fatal(msg)
	}
	if len(b.DependsOn) != 1 || b.DependsOn[0].Seq != 1 {
		t.Fatalf("B depends_on = %+v, want TASK-1", b.DependsOn)
	}

	var list struct {
		Tasks []mcpTask `json:"tasks"`
	}
	call(t, srv, "task_list", map[string]interface{}{"project": "proj", "ready": true}, &list)
	if len(list.Tasks) != 1 || list.Tasks[0].Seq != 1 {
		t.Fatalf("ready = %+v, want only TASK-1", list.Tasks)
	}
	if msg := call(t, srv, "task_list", map[string]interface{}{"ready": true, "status": "done"}, nil); !strings.Contains(msg, "todo") {
		t.Fatalf("ready with status done: %q, want refusal", msg)
	}

	msg := call(t, srv, "task_update", map[string]interface{}{"id": "TASK-1", "depends_on": []string{"TASK-2"}}, nil)
	if !strings.Contains(msg, "TASK-1 -> TASK-2 -> TASK-1") {
		t.Fatalf("cycle: %q, want the chain", msg)
	}

	var show mcpTask
	call(t, srv, "task_show", map[string]interface{}{"id": "TASK-1"}, &show)
	if len(show.Blocks) != 1 || show.Blocks[0].Seq != 2 {
		t.Fatalf("A blocks = %+v, want TASK-2", show.Blocks)
	}

	var upd mcpTask
	if msg := call(t, srv, "task_update", map[string]interface{}{"id": "TASK-2", "remove_depends_on": "TASK-1"}, &upd); msg != "" {
		t.Fatal(msg)
	}
	if len(upd.DependsOn) != 0 {
		t.Fatalf("after removal depends_on = %+v", upd.DependsOn)
	}

	// The schema tells clients about the new arguments.
	schema, _ := json.Marshal(tools())
	for _, want := range []string{`"depends_on"`, `"remove_depends_on"`, `"ready"`} {
		if !bytes.Contains(schema, []byte(want)) {
			t.Fatalf("tools/list lacks %s", want)
		}
	}
}
