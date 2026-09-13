package pmweb

import (
	"os/exec"
	"strings"
	"testing"
)

// Uppläsningen bor i JavaScript och beror på webbläsarens talstöd. Provet
// vaktar knapparnas villkor, stoppanropet och beskedet om saknad svensk röst.
func TestUpplasningenStyrKnapparOchRost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node saknas, hoppar över provet för uppläsningen")
	}
	ut, err := exec.Command(node, "static/pm-tal.test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("talprovet föll: %v\n%s", err, ut)
	}
	if !strings.Contains(string(ut), "gick igenom") {
		t.Fatalf("oväntat svar från talprovet:\n%s", ut)
	}
}
