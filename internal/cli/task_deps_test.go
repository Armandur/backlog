package cli

import (
	"strings"
	"testing"
)

type depTask struct {
	Seq       int    `json:"seq"`
	Status    string `json:"status"`
	DependsOn []struct {
		Seq    int    `json:"seq"`
		Status string `json:"status"`
	} `json:"depends_on"`
	Blocks []struct {
		Seq    int    `json:"seq"`
		Status string `json:"status"`
	} `json:"blocks"`
}

func readySeqs(t *testing.T, base []string) []int {
	t.Helper()
	var list struct {
		Tasks []depTask `json:"tasks"`
	}
	runJSON(t, base, &list, "task", "list", "--project", "proj", "--ready")
	var seqs []int
	for _, task := range list.Tasks {
		if task.DependsOn == nil {
			t.Fatalf("TASK-%d: depends_on missing from list JSON", task.Seq)
		}
		seqs = append(seqs, task.Seq)
	}
	return seqs
}

func hasSeq(seqs []int, n int) bool {
	for _, s := range seqs {
		if s == n {
			return true
		}
	}
	return false
}

// TestTaskDependenciesThroughCLI runs the dependency commands the way an
// agent does. B depends on A, so B must be missing from --ready until A is
// done, and A must not be allowed to depend on B.
func TestTaskDependenciesThroughCLI(t *testing.T) {
	base := cliEnv(t)
	for _, title := range []string{"A", "B", "C"} {
		if _, err := runCLI(t, append(base, "task", "add", "--title", title, "--project", "proj")...); err != nil {
			t.Fatalf("task add %s: %v", title, err)
		}
	}

	// update --depends-on stores the dependency.
	if _, err := runCLI(t, append(base, "task", "update", "TASK-2", "--depends-on", "TASK-1")...); err != nil {
		t.Fatalf("update --depends-on: %v", err)
	}
	// add --depends-on works at creation.
	if _, err := runCLI(t, append(base, "task", "add", "--title", "D", "--project", "proj", "--depends-on", "TASK-2")...); err != nil {
		t.Fatalf("add --depends-on: %v", err)
	}

	var b depTask
	runJSON(t, base, &b, "task", "show", "TASK-2")
	if len(b.DependsOn) != 1 || b.DependsOn[0].Seq != 1 || b.DependsOn[0].Status != "todo" {
		t.Fatalf("TASK-2 depends_on = %+v, want TASK-1 todo", b.DependsOn)
	}
	if len(b.Blocks) != 1 || b.Blocks[0].Seq != 4 {
		t.Fatalf("TASK-2 blocks = %+v, want TASK-4", b.Blocks)
	}
	var a depTask
	runJSON(t, base, &a, "task", "show", "TASK-1")
	if len(a.Blocks) != 1 || a.Blocks[0].Seq != 2 {
		t.Fatalf("TASK-1 blocks = %+v, want TASK-2", a.Blocks)
	}

	// Text output names both directions with status.
	text, err := runCLI(t, append(base, "task", "show", "TASK-2")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Depends on:") || !strings.Contains(text, "TASK-1 [") ||
		!strings.Contains(text, "Blocks:") || !strings.Contains(text, "TASK-4 [") {
		t.Fatalf("task show text lacks dependency lines:\n%s", text)
	}

	// Before A is done: A and C are ready, B and D are not.
	seqs := readySeqs(t, base)
	if !hasSeq(seqs, 1) || !hasSeq(seqs, 3) || hasSeq(seqs, 2) || hasSeq(seqs, 4) {
		t.Fatalf("ready before A done = %v, want [1 3] only", seqs)
	}

	// A cycle is refused with a readable message: A -> B -> A, and A -> D -> B -> A.
	for _, dep := range []string{"TASK-2", "TASK-4"} {
		_, err := runCLI(t, append(base, "task", "update", "TASK-1", "--depends-on", dep)...)
		if err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("TASK-1 --depends-on %s: err = %v, want cycle error", dep, err)
		}
	}
	_, err = runCLI(t, append(base, "task", "update", "TASK-1", "--depends-on", "TASK-1")...)
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("self-dependency: err = %v, want refusal", err)
	}

	// After A is done: B becomes ready, A drops out (no longer todo), D still waits on B.
	if _, err := runCLI(t, append(base, "task", "move", "TASK-1", "--status", "done")...); err != nil {
		t.Fatal(err)
	}
	seqs = readySeqs(t, base)
	if hasSeq(seqs, 1) || !hasSeq(seqs, 2) || !hasSeq(seqs, 3) || hasSeq(seqs, 4) {
		t.Fatalf("ready after A done = %v, want [2 3] only", seqs)
	}

	// --no-depends-on removes the dependency, and a second removal is refused.
	if _, err := runCLI(t, append(base, "task", "update", "TASK-4", "--no-depends-on", "TASK-2")...); err != nil {
		t.Fatalf("update --no-depends-on: %v", err)
	}
	if seqs = readySeqs(t, base); !hasSeq(seqs, 4) {
		t.Fatalf("ready after removing D's dependency = %v, want TASK-4 included", seqs)
	}
	_, err = runCLI(t, append(base, "task", "update", "TASK-4", "--no-depends-on", "TASK-2")...)
	if err == nil || !strings.Contains(err.Error(), "does not depend on") {
		t.Fatalf("second removal: err = %v, want refusal", err)
	}

	// --ready only makes sense for todo.
	_, err = runCLI(t, append(base, "task", "list", "--ready", "--status", "doing")...)
	if err == nil {
		t.Fatal("--ready --status doing was accepted")
	}
}

// TestTaskEditAliasThroughCLI: task edit är alias för task update och tar
// samma beroendeflaggor.
func TestTaskEditAliasThroughCLI(t *testing.T) {
	base := cliEnv(t)
	for _, title := range []string{"A", "B"} {
		if _, err := runCLI(t, append(base, "task", "add", "--title", title, "--project", "proj")...); err != nil {
			t.Fatalf("task add %s: %v", title, err)
		}
	}
	if _, err := runCLI(t, append(base, "task", "edit", "TASK-2", "--depends-on", "TASK-1")...); err != nil {
		t.Fatalf("task edit --depends-on: %v", err)
	}
	var b depTask
	runJSON(t, base, &b, "task", "show", "TASK-2")
	if len(b.DependsOn) != 1 || b.DependsOn[0].Seq != 1 {
		t.Fatalf("TASK-2 depends_on = %+v, want TASK-1", b.DependsOn)
	}
	if _, err := runCLI(t, append(base, "task", "edit", "TASK-2", "--no-depends-on", "TASK-1")...); err != nil {
		t.Fatalf("task edit --no-depends-on: %v", err)
	}
	runJSON(t, base, &b, "task", "show", "TASK-2")
	if len(b.DependsOn) != 0 {
		t.Fatalf("TASK-2 depends_on = %+v, want empty", b.DependsOn)
	}
}
