package pmweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilsokroutenHittarTextOchHopparOverOlämpligaFiler(t *testing.T) {
	srv, db := testServer(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fras := `"citat"-flagga`
	filer := map[string][]byte{
		"README.md":         []byte("Första raden\nAnvänd " + fras + " som text\n"),
		"docs/guide.txt":    []byte("Här finns också " + fras + "\n"),
		".git/config":       []byte("hemlig " + fras + "\n"),
		"program.bin":       append([]byte("binär "+fras), 0),
		"alldeles-for-stor": append([]byte("stor "+fras+"\n"), make([]byte, maxFilstorlek)...),
	}
	for namn, innehall := range filer {
		if err := os.WriteFile(filepath.Join(repo, namn), innehall, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	utanfor := filepath.Join(t.TempDir(), "utanfor.txt")
	if err := os.WriteFile(utanfor, []byte(fras), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(utanfor, filepath.Join(repo, "lank.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	adress := "/api/projects/demo/filsok?q=" + url.QueryEscape(fras)
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, adress, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sökningen gav %d: %s", w.Code, w.Body.String())
	}
	var svar filsoksvar
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if svar.Fraga != fras || svar.Begransad || len(svar.Traffar) != 2 {
		t.Fatalf("fel söksvar: %+v", svar)
	}
	hittade := map[string]filsoktraff{}
	for _, traff := range svar.Traffar {
		hittade[traff.Fil] = traff
	}
	if hittade["README.md"].Rad != 2 || hittade["README.md"].Text != "Använd "+fras+" som text" {
		t.Fatalf("README-träffen är fel: %+v", hittade["README.md"])
	}
	if hittade["docs/guide.txt"].Rad != 1 {
		t.Fatalf("träffen i underkatalogen är fel: %+v", hittade["docs/guide.txt"])
	}
}

func TestFilsokroutenAnvanderFilvynsSokvagssparr(t *testing.T) {
	srv, db := testServer(t)
	repo := t.TempDir()
	utanfor := filepath.Join(t.TempDir(), "hemlig.txt")
	if err := os.WriteFile(utanfor, []byte("nålen"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(utanfor, filepath.Join(repo, "lank.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}

	fall := []struct {
		sokvag string
		text   string
	}{
		{"../hemlig.txt", "får inte innehålla .."},
		{utanfor, "måste ligga under projektets repo"},
		{"lank.txt", "symbolisk länk"},
		{".git/config", ".git-katalogen visas inte"},
	}
	for _, test := range fall {
		t.Run(test.text, func(t *testing.T) {
			w := httptest.NewRecorder()
			adress := "/api/projects/demo/filsok?q=nålen&path=" + url.QueryEscape(test.sokvag)
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, adress, nil))
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), test.text) {
				t.Fatalf("sökvägen %q gav %d: %s", test.sokvag, w.Code, w.Body.String())
			}
		})
	}
}

func TestFilsokroutenBegransarAntaletTraffar(t *testing.T) {
	srv, db := testServer(t)
	repoForProjekt(t, db, map[string]string{
		"många.txt": strings.Repeat("en träff\n", maxFilsoktraffar+1),
	})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/filsok?q=träff", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sökningen gav %d: %s", w.Code, w.Body.String())
	}
	var svar filsoksvar
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if len(svar.Traffar) != maxFilsoktraffar || !svar.Begransad {
		t.Fatalf("träffgränsen användes inte: %+v", svar)
	}
}

func TestFilsokroutenKravPaSoktext(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/filsok?q=++", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "ange texten") {
		t.Fatalf("tom sökning gav %d: %s", w.Code, w.Body.String())
	}
}
