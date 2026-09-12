package pm

import (
	"strings"
	"testing"
)

func TestNormaliseraGitHubRepo(t *testing.T) {
	fall := []string{
		"Armandur/Backlog",
		"github.com/Armandur/Backlog.git",
		"https://GitHub.com/Armandur/Backlog/",
		"git@github.com:Armandur/Backlog.git",
		"ssh://git@github.com/Armandur/Backlog.git",
	}
	for _, repo := range fall {
		t.Run(repo, func(t *testing.T) {
			fick, err := NormaliseraGitHubRepo(repo)
			if err != nil {
				t.Fatal(err)
			}
			if fick != "github.com/armandur/backlog" {
				t.Fatalf("normaliseringen gav %q", fick)
			}
		})
	}
}

func TestNormaliseraGitHubRepoAvvisarOgiltigaVarden(t *testing.T) {
	fall := []string{
		"",
		"https://gitlab.com/armandur/backlog",
		"github.com/armandur",
		"github.com/armandur/backlog/issues",
		"https://github.com/armandur/backlog?tab=readme",
		"file://github.com/armandur/backlog",
	}
	for _, repo := range fall {
		if _, err := NormaliseraGitHubRepo(repo); err == nil {
			t.Fatalf("NormaliseraGitHubRepo godtog det ogiltiga repot %q", repo)
		}
	}
}

func TestGitHubRepoTillatetNekarSomStandard(t *testing.T) {
	k := StandardKonfig().GitHub
	if tillatet, skal := GitHubRepoTillatet(k, "armandur/backlog", false); tillatet || !strings.Contains(skal, "tillåtelselista") {
		t.Fatalf("vakten skulle neka ett okänt repo med ett tydligt skäl: %t %q", tillatet, skal)
	}
	if k.Skrivlage || len(k.TillatnaRepon) != 0 {
		t.Fatalf("GitHub-standarden ska neka skrivning och sakna tillåtna repon: %+v", k)
	}
}

func TestGitHubRepoTillatetLaterSparrlistanVinna(t *testing.T) {
	// Samma repo står i båda listorna. Spärren ska vinna, oavsett hur
	// adressen är skriven.
	k := GitHubKonfig{
		TillatnaRepon: []string{"Armandur/produktion"},
		SparradeRepon: []string{"github.com/armandur/produktion"},
		Skrivlage:     true,
	}
	tillatet, skal := GitHubRepoTillatet(k, "git@github.com:ARMANDUR/produktion.git", false)
	if tillatet || !strings.Contains(skal, "spärrlistan") {
		t.Fatalf("vakten släppte igenom ett spärrat repo: %t %q", tillatet, skal)
	}
}

func TestGitHubRepoTillatetSkiljerLasningFranSkrivning(t *testing.T) {
	k := GitHubKonfig{TillatnaRepon: []string{"github.com/Armandur/backlog.git"}}
	if tillatet, skal := GitHubRepoTillatet(k, "armandur/BACKLOG", false); !tillatet {
		t.Fatalf("vakten skulle tillåta läsningen: %q", skal)
	}
	if tillatet, skal := GitHubRepoTillatet(k, "armandur/backlog", true); tillatet || !strings.Contains(skal, "skrivläget") {
		t.Fatalf("vakten skulle neka skrivningen: %t %q", tillatet, skal)
	}
	k.Skrivlage = true
	if tillatet, skal := GitHubRepoTillatet(k, "armandur/backlog", true); !tillatet {
		t.Fatalf("vakten skulle tillåta skrivningen: %q", skal)
	}
}

func TestLasKonfigLaserOchValiderarGitHub(t *testing.T) {
	dir := skrivKonfig(t, `
[github]
tillatna_repon = ["Armandur/backlog"]
sparrade_repon = ["github.com/annan/produktion"]
skrivlage = true
`)
	k, err := LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !k.GitHub.Skrivlage || len(k.GitHub.TillatnaRepon) != 1 || len(k.GitHub.SparradeRepon) != 1 {
		t.Fatalf("LasKonfig läste GitHub-konfigurationen fel: %+v", k.GitHub)
	}

	dir = skrivKonfig(t, "[github]\ntillatna_repon = [\"gitlab.com/armandur/backlog\"]\n")
	if _, err := LasKonfig(dir); err == nil || !strings.Contains(err.Error(), "tillåtelselistan") {
		t.Fatalf("LasKonfig skulle avvisa ett ogiltigt repo i konfigurationen: %v", err)
	}
}

func TestGitHubVaktenNekarTrasigKonfiguration(t *testing.T) {
	k := GitHubKonfig{
		TillatnaRepon: []string{"armandur/backlog"},
		SparradeRepon: []string{"inte ett repo"},
	}
	if tillatet, skal := GitHubRepoTillatet(k, "armandur/backlog", false); tillatet || !strings.Contains(skal, "spärrlistan") {
		t.Fatalf("vakten skulle neka en trasig konfiguration: %t %q", tillatet, skal)
	}
}
