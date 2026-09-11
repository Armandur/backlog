package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	skill "github.com/mazen160/backlog/skills"
)

func TestRunInstallSkillsWritesCodexSkillDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runInstallSkills([]string{"backlog"}, false, false, true); err != nil {
		t.Fatalf("runInstallSkills: %v", err)
	}

	dest := filepath.Join(home, ".codex", "skills", "backlog", "SKILL.md")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read Codex skill: %v", err)
	}

	got := string(body)
	if !strings.HasPrefix(got, "---\nname: backlog\n") {
		t.Fatalf("Codex skill body is missing the backlog frontmatter, got %.40q", got)
	}
	// Jämför mot den inbäddade källan i stället för mot en fast textrad.
	// Skillens text är översatt och skrivs om, testet ska ändå hålla.
	alla, err := skill.All()
	if err != nil {
		t.Fatalf("skill.All: %v", err)
	}
	var kalla string
	for _, s := range alla {
		if s.Name == "backlog" {
			kalla = s.Body
		}
	}
	if kalla == "" {
		t.Fatal("hittade ingen inbäddad skill som heter backlog")
	}
	if got != kalla {
		t.Fatalf("Codex skill body skiljer sig från den inbäddade skillen")
	}
	if !strings.Contains(kalla, "\ndescription: ") {
		t.Fatal("den inbäddade backlog-skillen saknar en description-rad")
	}

	oldPromptPath := filepath.Join(home, ".codex", "prompts", "backlog.md")
	if _, err := os.Stat(oldPromptPath); !os.IsNotExist(err) {
		t.Fatalf("old Codex prompt path exists or stat failed unexpectedly: %v", err)
	}
}
