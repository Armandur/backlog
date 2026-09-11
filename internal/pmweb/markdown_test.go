package pmweb

import (
	"os/exec"
	"strings"
	"testing"
)

// Markdown-renderaren tar text som agenter skriver, så saneringen behöver en
// grind. Provet ligger i JavaScript, för det är där renderaren bor.
func TestMarkdownrenderarenSlapperInteIgenomHTML(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node saknas, hoppar över provet för markdown-renderaren")
	}
	ut, err := exec.Command(node, "static/pm-markdown.test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("markdown-provet föll: %v\n%s", err, ut)
	}
	if !strings.Contains(string(ut), "gick igenom") {
		t.Fatalf("oväntat svar från markdown-provet:\n%s", ut)
	}
}
