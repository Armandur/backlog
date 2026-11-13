package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/models"
)

func TestWriteMemoryMarkdown(t *testing.T) {
	p := &models.Project{Alias: "api", Name: "API"}
	long := strings.Repeat("x", 300)
	entries := []*models.Memory{
		{ID: "01NEW", Body: "  " + long + "\n", Tags: "decision,arch",
			Actor: models.Actor{Kind: models.ActorKindAI, Name: "claude"}, CreatedAt: time.Date(2026, 11, 13, 17, 5, 0, 0, time.UTC).UnixNano()},
		{ID: "01OLD", Body: "untagged",
			Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, CreatedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC).UnixNano()},
	}
	var b strings.Builder
	writeMemoryMarkdown(&b, p, entries, time.UTC)
	out := b.String()

	for _, want := range []string{
		"# Memory: API (api)\n",
		"2 entries, newest first.",
		"## 2026-11-13 17:05 · decision,arch\n\n" + long + "\n",
		"_ai:claude · 01NEW_",
		"## 2026-01-02 03:04\n\nuntagged\n",
		"_human:rasmus · 01OLD_",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export missing %q\n---\n%s", want, out)
		}
	}
	if strings.Index(out, "01NEW") > strings.Index(out, "01OLD") {
		t.Error("entries must keep the order they were given in")
	}

	b.Reset()
	writeMemoryMarkdown(&b, p, nil, time.UTC)
	if !strings.Contains(b.String(), "No memory entries.") {
		t.Errorf("empty project must say so, got %q", b.String())
	}
}
