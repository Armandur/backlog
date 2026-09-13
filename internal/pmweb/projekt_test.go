package pmweb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/pm"
)

type fejkadGitHubKlonare struct {
	konfig  pm.GitHubKonfig
	anropad bool
	repo    string
	sokvag  string
}

func (f *fejkadGitHubKlonare) KlonaRepo(_ context.Context, repo, sokvag string) (func() error, error) {
	f.anropad = true
	f.repo = repo
	f.sokvag = sokvag
	if err := os.MkdirAll(filepath.Join(sokvag, ".git"), 0o755); err != nil {
		return nil, err
	}
	return func() error { return os.RemoveAll(sokvag) }, nil
}

func TestKlonaGitHubProjektViaRoutenMedInjiceradAttrapp(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	konfigDir := t.TempDir()
	medKonfigDir(t, konfigDir)
	if err := os.WriteFile(filepath.Join(konfigDir, pm.KonfigFil), []byte(`[github]
tillatna_repon = ["agare/repo"]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	gammalFabrik := nyGitHubRepoKlonare
	var attrapp *fejkadGitHubKlonare
	nyGitHubRepoKlonare = func(konfig pm.GitHubKonfig) githubRepoKlonare {
		attrapp = &fejkadGitHubKlonare{konfig: konfig}
		return attrapp
	}
	t.Cleanup(func() { nyGitHubRepoKlonare = gammalFabrik })

	srv, db := testServer(t)
	w := postProjekt(t, srv, skapaProjektBody{
		Alias:  "klonat",
		Namn:   "Klonat projekt",
		Lage:   "github",
		Sokvag: "klonat",
		Repo:   "Agare/Repo",
	})
	if w.Code != 201 {
		t.Fatalf("POST /api/projekt gav %d: %s", w.Code, w.Body.String())
	}
	vantadSokvag := filepath.Join(bas, "klonat")
	if attrapp == nil || !attrapp.anropad {
		t.Fatal("routen anropade inte kloningsattrappen")
	}
	if attrapp.repo != "Agare/Repo" || attrapp.sokvag != vantadSokvag {
		t.Fatalf("attrappen fick fel indata: repo=%q sökväg=%q", attrapp.repo, attrapp.sokvag)
	}
	if len(attrapp.konfig.TillatnaRepon) != 1 || attrapp.konfig.TillatnaRepon[0] != "agare/repo" {
		t.Fatalf("routen läste inte GitHub-konfigurationen: %+v", attrapp.konfig)
	}
	var sparadSokvag string
	if err := db.QueryRow(`SELECT repo_path FROM projects WHERE alias='klonat'`).Scan(&sparadSokvag); err != nil {
		t.Fatal(err)
	}
	if sparadSokvag != vantadSokvag || !strings.Contains(w.Body.String(), `"lank":"/pm/klonat"`) {
		t.Fatalf("projektet registrerades fel: sökväg=%q svar=%s", sparadSokvag, w.Body.String())
	}
}
