package cli

import (
	"strings"
	"testing"
)

type descTask struct {
	Description string `json:"description"`
}

func taskDescription(t *testing.T, base []string) string {
	t.Helper()
	var task descTask
	runJSON(t, base, &task, "task", "show", "TASK-1")
	return task.Description
}

// TestTaskUpdateRefusesEmptyDescription guards against an agent emptying a
// description by mistake: update -d "$(script)" got an empty string when the
// script failed, and the old text was lost (friction TASK-3849).
func TestTaskUpdateRefusesEmptyDescription(t *testing.T) {
	base := cliEnv(t)
	if _, err := runCLI(t, append(base, "task", "add", "--title", "A", "--project", "proj", "-d", "original")...); err != nil {
		t.Fatalf("task add: %v", err)
	}

	for _, value := range []string{"", "   ", "\n\t"} {
		_, err := runCLI(t, append(base, "task", "update", "TASK-1", "-d", value)...)
		if err == nil || !strings.Contains(err.Error(), "--clear-description") {
			t.Fatalf("update -d %q: err = %v, want refusal naming --clear-description", value, err)
		}
		if got := taskDescription(t, base); got != "original" {
			t.Fatalf("after refused update -d %q: description = %q, want unchanged", value, got)
		}
	}

	// A refused description must not let the other flags through either.
	if _, err := runCLI(t, append(base, "task", "update", "TASK-1", "-d", "", "--title", "B")...); err == nil {
		t.Fatal("update -d \"\" --title B: want error")
	}
	var task struct {
		Title string `json:"title"`
	}
	runJSON(t, base, &task, "task", "show", "TASK-1")
	if task.Title != "A" {
		t.Fatalf("title = %q after refused update, want A", task.Title)
	}

	// A real description still updates.
	if _, err := runCLI(t, append(base, "task", "update", "TASK-1", "-d", "ny text")...); err != nil {
		t.Fatalf("update -d text: %v", err)
	}
	if got := taskDescription(t, base); got != "ny text" {
		t.Fatalf("description = %q, want \"ny text\"", got)
	}
}

func TestTaskUpdateClearDescription(t *testing.T) {
	base := cliEnv(t)
	if _, err := runCLI(t, append(base, "task", "add", "--title", "A", "--project", "proj", "-d", "original")...); err != nil {
		t.Fatalf("task add: %v", err)
	}

	if _, err := runCLI(t, append(base, "task", "update", "TASK-1", "--clear-description", "-d", "x")...); err == nil {
		t.Fatal("--clear-description together with -d: want error")
	}
	if got := taskDescription(t, base); got != "original" {
		t.Fatalf("after refused combination: description = %q, want unchanged", got)
	}

	if _, err := runCLI(t, append(base, "task", "update", "TASK-1", "--clear-description")...); err != nil {
		t.Fatalf("update --clear-description: %v", err)
	}
	if got := taskDescription(t, base); got != "" {
		t.Fatalf("description = %q after --clear-description, want empty", got)
	}
}
