package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI runs the root command with args and returns what it printed.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	root := newRootCmd()
	root.SetArgs(args)
	runErr := root.Execute()
	w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

// TestDocCommandsAcceptPrintedShortID runs the CLI the way an agent does:
// doc add prints an 8-character ID, and show and append must accept it.
func TestDocCommandsAcceptPrintedShortID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("BACKLOG_DB", "")
	db := filepath.Join(dir, "backlog.db")
	base := []string{"--db", db, "--as", "human:test"}

	if _, err := runCLI(t, append(base, "project", "add", "Proj", "--alias", "proj")...); err != nil {
		t.Fatalf("project add: %v", err)
	}
	out, err := runCLI(t, append(base, "doc", "add", "--project", "proj", "--title", "Runbook", "--content", "first")...)
	if err != nil {
		t.Fatalf("doc add: %v", err)
	}
	_, after, found := strings.Cut(out, "(ID: ")
	if !found {
		t.Fatalf("doc add printed no ID: %q", out)
	}
	short := strings.TrimSuffix(strings.TrimSpace(after), ")")
	if len(short) != 8 {
		t.Fatalf("expected the 8-character ID that agents see, got %q", short)
	}

	if _, err := runCLI(t, append(base, "doc", "append", short, "--content", "second")...); err != nil {
		t.Fatalf("doc append %s: %v", short, err)
	}
	out, err = runCLI(t, append(base, "doc", "show", short, "--json")...)
	if err != nil {
		t.Fatalf("doc show %s: %v", short, err)
	}
	var d struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		CurrentVersion int    `json:"current_version"`
		Version        struct {
			Body string `json:"body"`
		} `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("doc show --json: %v\n%s", err, out)
	}
	if !strings.HasPrefix(d.ID, short) || len(d.ID) != 26 {
		t.Errorf("doc show returned id %q, want the full ULID starting with %s", d.ID, short)
	}
	if d.Title != "Runbook" || d.CurrentVersion != 2 || d.Version.Body != "first\nsecond" {
		t.Errorf("unexpected doc after append: %+v", d)
	}
}
