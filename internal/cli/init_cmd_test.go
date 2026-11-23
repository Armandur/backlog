package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/config"
	"github.com/mazen160/backlog/internal/profile"
)

func TestResetWorkspaceFilesPreservesDirectoryAndUnmanagedFiles(t *testing.T) {
	dir := t.TempDir()
	managed := []string{
		"backlog.db",
		"backlog.db-shm",
		"backlog.db-wal",
		"backlog.db-journal",
		"backlog.json",
		"backlog.config",
		"config.toml",
	}
	for _, name := range managed {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("managed"), 0644); err != nil {
			t.Fatalf("write managed file %s: %v", name, err)
		}
	}
	unmanaged := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(unmanaged, []byte("keep"), 0644); err != nil {
		t.Fatalf("write unmanaged file: %v", err)
	}

	if err := resetWorkspaceFiles(dir); err != nil {
		t.Fatalf("resetWorkspaceFiles: %v", err)
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("workspace directory was not preserved: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("unmanaged file was not preserved: %v", err)
	}
	for _, name := range managed {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("managed file %s still exists or stat failed with unexpected error: %v", name, err)
		}
	}
}

func initTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if !strings.HasPrefix(config.GlobalDir(), home+string(os.PathSeparator)) {
		t.Fatal("profile registry escaped the test home")
	}
	return home
}

func readInitTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInitRejectsProfileCollision(t *testing.T) {
	home := initTestHome(t)
	original := filepath.Join(home, "first")
	if _, err := runCLI(t, "init", "--path", original); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(config.GlobalDir(), "config.toml")
	before := readInitTestFile(t, registry)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "implicit default"},
		{name: "explicit default", args: []string{"--profile", "default"}},
		{name: "set default", args: []string{"--set-default"}},
		{name: "reset", args: []string{"--reset"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(home, tc.name)
			db := filepath.Join(target, "backlog.db")
			if tc.name == "reset" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(db, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := runCLI(t, append([]string{"init", "--path", target}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), "already points to") {
				t.Fatalf("expected profile collision, got %v", err)
			}
			if got := readInitTestFile(t, registry); !bytes.Equal(got, before) {
				t.Fatalf("registry changed: %s", got)
			}
			if tc.name == "reset" {
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 || string(readInitTestFile(t, db)) != "untouched" {
					t.Fatalf("reset touched the existing target: %v, %v", entries, err)
				}
			} else if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("target must not exist, stat error: %v", err)
			}
		})
	}
}

func TestInitAndResetSameProfilePath(t *testing.T) {
	home := initTestHome(t)
	dir := filepath.Join(home, "workspace")
	for _, args := range [][]string{{"init", "--path", dir}, {"init", "--path", dir}} {
		if _, err := runCLI(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "backlog.json"), []byte("old file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "init", "--path", dir, "--reset"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backlog.json")); !os.IsNotExist(err) {
		t.Fatalf("reset preserved an old managed file: %v", err)
	}
	if actual, err := profile.Resolve("default"); err != nil || actual != dir {
		t.Fatalf("profile path = %q (%v), want %q", actual, err, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "backlog.db")); err != nil {
		t.Fatal(err)
	}
}

func TestInitLeavesRegistryOnLateWorkspaceError(t *testing.T) {
	home := initTestHome(t)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(config.GlobalDir(), "config.toml")
	before := readInitTestFile(t, registry)

	target := filepath.Join(home, "broken")
	if err := os.MkdirAll(filepath.Join(target, "config.toml"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runCLI(t, "init", "--profile", "broken", "--path", target, "--set-default")
	if err == nil || !strings.Contains(err.Error(), "write config") {
		t.Fatalf("expected late workspace configuration error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "backlog.db")); err != nil {
		t.Fatalf("workspace must have reached database initialization: %v", err)
	}
	if got := readInitTestFile(t, registry); !bytes.Equal(got, before) {
		t.Fatalf("late failure changed registry: %s", got)
	}
}

func TestInitConcurrentProfileCollision(t *testing.T) {
	home := initTestHome(t)
	paths := []string{filepath.Join(home, "one"), filepath.Join(home, "two")}
	commands := make([]*exec.Cmd, len(paths))
	outputs := make([]bytes.Buffer, len(paths))
	for i, path := range paths {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInitSubprocess$", "--", path)
		cmd.Env = append(os.Environ(), "BACKLOG_TEST_INIT_CHILD=1")
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[i] = cmd
	}
	winners := 0
	for i, cmd := range commands {
		err := cmd.Wait()
		if err == nil {
			winners++
			actual, err := profile.Resolve("default")
			if err != nil || actual != paths[i] {
				t.Errorf("profile path = %q (%v), want winner %q", actual, err, paths[i])
			}
		} else {
			if !strings.Contains(outputs[i].String(), "already points to") {
				t.Errorf("unexpected failure: %v: %s", err, &outputs[i])
			}
			if _, err := os.Stat(paths[i]); !os.IsNotExist(err) {
				t.Errorf("loser touched its target: %v", err)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful init commands, want one", winners)
	}
}

func TestInitSubprocess(t *testing.T) {
	if os.Getenv("BACKLOG_TEST_INIT_CHILD") != "1" {
		return
	}
	if _, err := runCLI(t, "init", "--path", os.Args[len(os.Args)-1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		t.Fail()
	}
}
