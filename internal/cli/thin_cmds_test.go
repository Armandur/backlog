package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// cliEnv gives each test its own database and the base flags for runCLI.
func cliEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("BACKLOG_DB", "")
	base := []string{"--db", filepath.Join(dir, "backlog.db"), "--as", "ai:test"}
	if _, err := runCLI(t, append(base, "project", "add", "Proj", "--alias", "proj")...); err != nil {
		t.Fatalf("project add: %v", err)
	}
	return base
}

func runJSON(t *testing.T, base []string, out interface{}, args ...string) {
	t.Helper()
	raw, err := runCLI(t, append(append(base, args...), "--json")...)
	if err != nil {
		t.Fatalf("%s: %v", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("%s: parse %q: %v", strings.Join(args, " "), raw, err)
	}
}

func TestCommentAddTakesTaskAsFirstArgument(t *testing.T) {
	base := cliEnv(t)
	if _, err := runCLI(t, append(base, "task", "add", "--title", "T1", "--project", "proj")...); err != nil {
		t.Fatalf("task add: %v", err)
	}
	if _, err := runCLI(t, append(base, "comment", "add", "TASK-1", "positional")...); err != nil {
		t.Fatalf("comment add TASK-1 <body>: %v", err)
	}
	if _, err := runCLI(t, append(base, "comment", "add", "flag", "--task", "TASK-1")...); err != nil {
		t.Fatalf("comment add <body> --task: %v", err)
	}

	var list struct {
		Comments []struct{ Body string } `json:"comments"`
	}
	runJSON(t, base, &list, "comment", "list", "--task", "TASK-1")
	var bodies []string
	for _, c := range list.Comments {
		bodies = append(bodies, c.Body)
	}
	if got := strings.Join(bodies, ","); got != "positional,flag" {
		t.Fatalf("comment bodies = %q, want positional,flag", got)
	}

	_, err := runCLI(t, append(base, "comment", "add", "TASK-1", "both", "--task", "TASK-1")...)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("task given twice: want error, got %v", err)
	}
	_, err = runCLI(t, append(base, "comment", "add", "no task")...)
	if err == nil || !strings.Contains(err.Error(), "task is required") {
		t.Fatalf("no task: want error, got %v", err)
	}
}

func TestTaskUnarchive(t *testing.T) {
	base := cliEnv(t)
	if _, err := runCLI(t, append(base, "task", "add", "--title", "T1", "--project", "proj")...); err != nil {
		t.Fatalf("task add: %v", err)
	}
	if _, err := runCLI(t, append(base, "task", "archive", "TASK-1")...); err != nil {
		t.Fatalf("task archive: %v", err)
	}
	var task struct {
		ArchivedAt *int64 `json:"archived_at"`
	}
	runJSON(t, base, &task, "task", "show", "TASK-1")
	if task.ArchivedAt == nil {
		t.Fatal("precondition: task should be archived")
	}

	if _, err := runCLI(t, append(base, "task", "unarchive", "TASK-1")...); err != nil {
		t.Fatalf("task unarchive: %v", err)
	}
	task.ArchivedAt = nil
	runJSON(t, base, &task, "task", "show", "TASK-1")
	if task.ArchivedAt != nil {
		t.Fatalf("task still archived after unarchive: %d", *task.ArchivedAt)
	}
}

func TestMemoryUpdateKeepsIDAndLogsActor(t *testing.T) {
	base := cliEnv(t)
	var added struct {
		ID   string `json:"id"`
		Tags string `json:"tags"`
	}
	runJSON(t, base, &added, "memory", "add", "old fact", "--project", "proj", "--tag", "keep")

	var m struct {
		ID   string `json:"id"`
		Body string `json:"body"`
		Tags string `json:"tags"`
	}
	runJSON(t, base, &m, "memory", "update", added.ID[:8], "new fact")
	if m.ID != added.ID || m.Body != "new fact" || m.Tags != "keep" {
		t.Fatalf("update without --tag: got %+v, want same ID, new body, tags keep", m)
	}
	runJSON(t, base, &m, "memory", "update", added.ID, "newer fact", "--tag", "changed")
	if m.Tags != "changed" {
		t.Fatalf("update with --tag: tags = %q, want changed", m.Tags)
	}

	var entries struct {
		Entries []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"entries"`
	}
	runJSON(t, base, &entries, "memory", "list", "--project", "proj")
	if len(entries.Entries) != 1 || entries.Entries[0].Body != "newer fact" {
		t.Fatalf("memory list after update = %+v, want one entry with the new body", entries.Entries)
	}

	var activity struct {
		Events []struct {
			Action string `json:"action"`
			Actor  struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"actor"`
		} `json:"events"`
	}
	runJSON(t, base, &activity, "activity", "--kind", "memory")
	updates := 0
	for _, e := range activity.Events {
		if e.Action == "updated" {
			updates++
			if e.Actor.Kind != "ai" || e.Actor.Name != "test" {
				t.Fatalf("update logged with actor %+v, want ai:test", e.Actor)
			}
		}
	}
	if updates != 2 {
		t.Fatalf("logged %d memory updates, want 2", updates)
	}
}
