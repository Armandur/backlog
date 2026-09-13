package pm

import (
	"errors"
	"strings"
	"testing"
)

func TestGitHubKommandotHarStatusgren(t *testing.T) {
	kommando, _, err := NewGitHubCmd().Find([]string{"status"})
	if err != nil {
		t.Fatalf("github status saknas: %v", err)
	}
	if kommando.Name() != "status" {
		t.Fatalf("hittade fel kommando: %s", kommando.Name())
	}
}

func TestGitHubStatusVisarDiagnostikUtanHemlighet(t *testing.T) {
	const token = "status-hemlighet"
	t.Setenv("GH_TOKEN", token)
	konfig := GitHubKonfig{
		TillatnaRepon: []string{"zeta/prov", "alfa/prov"},
		Skrivlage:     true,
	}
	utdata := GitHubStatus(konfig, "gh version 2.45.0\nmer", nil)
	for _, vantat := range []string{
		"gh-version: gh version 2.45.0",
		"inloggning: GH_TOKEN",
		"tillåtna repon: alfa/prov, zeta/prov",
		"skrivläge: på",
	} {
		if !strings.Contains(utdata, vantat) {
			t.Fatalf("status saknar %q, fick:\n%s", vantat, utdata)
		}
	}
	if strings.Contains(utdata, token) {
		t.Fatalf("status visade token: %s", utdata)
	}

	utdata = GitHubStatus(konfig, "", errors.New("fel "+token))
	if strings.Contains(utdata, token) || !strings.Contains(utdata, "[hemlighet dold]") {
		t.Fatalf("status maskerades inte: %s", utdata)
	}
}
