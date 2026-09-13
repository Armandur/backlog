package pmweb

import (
	"os/exec"
	"strings"
	"testing"
)

// Tasklistans filter och sortering bor i JavaScript. Ett riktigt projekt har
// hundratals tasks, så urvalet behöver en vakt.
func TestTasklistansFilterOchSortering(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node saknas, hoppar över provet för tasklistan")
	}
	ut, err := exec.Command(node, "static/pm-tasklista.test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("tasklistprovet föll: %v\n%s", err, ut)
	}
	if !strings.Contains(string(ut), "gick igenom") {
		t.Fatalf("oväntat svar från tasklistprovet:\n%s", ut)
	}
}
