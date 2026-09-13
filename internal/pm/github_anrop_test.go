package pm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func testGitHubKlient(konfig GitHubKonfig, korare GitHubKorare) *GitHubKlient {
	klient := NewGitHubKlient(konfig)
	klient.korare = korare
	klient.hittaGH = func(string) (string, error) { return "/prov/gh", nil }
	klient.hamtaToken = func(string) string { return "provtoken" }
	return klient
}

func TestGitHubAnropNekarRepoUtanforTillatelselistanForeStart(t *testing.T) {
	startad := false
	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"armandur/backlog"}},
		func(context.Context, string, []string, []string) ([]byte, error) {
			startad = true
			return nil, nil
		},
	)
	klient.hittaGH = func(string) (string, error) {
		t.Fatal("vakten skulle köra innan PM letar efter gh")
		return "", nil
	}

	_, err := klient.VisaRepo(context.Background(), "annan/hemligt")
	if err == nil || !strings.Contains(err.Error(), "tillåtelselista") {
		t.Fatalf("väntade vaktens skäl, fick: %v", err)
	}
	if startad {
		t.Fatal("PM startade gh för ett nekat repo")
	}
}

func TestGitHubAnropNekarSparratRepoForeStart(t *testing.T) {
	startad := false
	klient := testGitHubKlient(
		GitHubKonfig{
			TillatnaRepon: []string{"armandur/produktion"},
			SparradeRepon: []string{"armandur/produktion"},
		},
		func(context.Context, string, []string, []string) ([]byte, error) {
			startad = true
			return nil, nil
		},
	)

	_, err := klient.ListaIssues(context.Background(), "armandur/produktion")
	if err == nil || !strings.Contains(err.Error(), "spärrlistan") {
		t.Fatalf("väntade spärrlistans skäl, fick: %v", err)
	}
	if startad {
		t.Fatal("PM startade gh för ett spärrat repo")
	}
}

func TestGitHubAnropDoljerTokenOchRensarMiljon(t *testing.T) {
	t.Setenv("HEMLIG_VARIABEL", "får-inte-följa-med")
	const token = "mycket-hemlig-token"
	anrop := 0
	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"armandur/backlog"}},
		func(_ context.Context, program string, args, miljo []string) ([]byte, error) {
			anrop++
			if program != "/prov/gh" {
				t.Fatalf("fel program: %q", program)
			}
			if strings.Contains(strings.Join(miljo, "\n"), "HEMLIG_VARIABEL") {
				t.Fatalf("processmiljön ärvde en främmande variabel: %v", miljo)
			}
			if !innehallerMiljovarde(miljo, "GH_TOKEN", token) {
				t.Fatalf("GH_TOKEN saknas i gh-processens miljö: %v", miljo)
			}
			konfigDir := miljovarde(miljo, "GH_CONFIG_DIR")
			if konfigDir == "" {
				t.Fatalf("GH_CONFIG_DIR saknas: %v", miljo)
			}
			if _, err := os.Stat(konfigDir); err != nil {
				t.Fatalf("GH_CONFIG_DIR finns inte under anropet: %v", err)
			}
			if len(args) == 0 {
				t.Fatal("gh-anropet saknar argument")
			}
			if anrop == 1 {
				return []byte("svar " + token), nil
			}
			return []byte("utdata " + token), fmt.Errorf("fel med %s", token)
		},
	)
	klient.hamtaToken = func(string) string { return token }

	utdata, err := klient.ListaPullRequests(context.Background(), "armandur/backlog")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(utdata, token) || !strings.Contains(utdata, "[hemlighet dold]") {
		t.Fatalf("token maskerades inte i utdata: %q", utdata)
	}

	_, err = klient.VisaRepo(context.Background(), "armandur/backlog")
	if err == nil {
		t.Fatal("väntade fel från den fejkade köraren")
	}
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[hemlighet dold]") {
		t.Fatalf("token maskerades inte i felet: %v", err)
	}
}

func TestGitHubAnropGerBegripligtFelNarGHSaknas(t *testing.T) {
	startad := false
	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"armandur/backlog"}},
		func(context.Context, string, []string, []string) ([]byte, error) {
			startad = true
			return nil, nil
		},
	)
	klient.hittaGH = func(string) (string, error) { return "", errors.New("saknas") }

	_, err := klient.VisaRepo(context.Background(), "armandur/backlog")
	if err == nil || !strings.Contains(err.Error(), "hittar inte GitHub CLI (gh)") {
		t.Fatalf("väntade begripligt besked om gh, fick: %v", err)
	}
	if startad {
		t.Fatal("köraren startade trots att gh saknas")
	}
}

func innehallerMiljovarde(miljo []string, namn, varde string) bool {
	return miljovarde(miljo, namn) == varde
}

func miljovarde(miljo []string, namn string) string {
	prefix := namn + "="
	for _, post := range miljo {
		if strings.HasPrefix(post, prefix) {
			return strings.TrimPrefix(post, prefix)
		}
	}
	return ""
}
