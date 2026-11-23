package profile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/config"
)

func TestRegisterInitRechecksPathBeforePublication(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target := filepath.Join(home, "target")
	if err := CheckInitPath("default", target); err != nil {
		t.Fatal(err)
	}

	if err := Add("default", "~/earlier"); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(config.GlobalDir(), "config.toml")
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterInit("default", target, true); err == nil || !strings.Contains(err.Error(), "already points to") {
		t.Fatalf("expected profile collision at publication, got %v", err)
	}
	after, err := os.ReadFile(registry)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("registry changed at publication: %s (%v)", after, err)
	}
	isDefault, err := RegisterInit("default", filepath.Join(home, "earlier"), true)
	if err != nil || !isDefault {
		t.Fatalf("same expanded path should activate the profile: default=%v, err=%v", isDefault, err)
	}
	profiles, active, err := List()
	if err != nil || active != "default" || profiles["default"].Path != filepath.Clean("~/earlier") {
		t.Fatalf("same path registration changed the mapping: %v (%q, %v)", profiles, active, err)
	}
}
