package pm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestKlonaRepoAnvanderIsoleratGHLagerOchKontrollerarOrigin(t *testing.T) {
	const token = "hemlig-klontoken"
	sokvag := filepath.Join(t.TempDir(), "klon")
	var ghMiljo, gitMiljo []string

	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"Agare/Repo"}},
		func(_ context.Context, program string, args, miljo []string) ([]byte, error) {
			switch program {
			case "/prov/gh":
				ghMiljo = slices.Clone(miljo)
				vantade := []string{"repo", "clone", "agare/repo", sokvag, "--", "--origin", "origin"}
				if !slices.Equal(args, vantade) {
					t.Fatalf("fel gh-argument: %v", args)
				}
				if strings.Contains(strings.Join(args, " "), token) {
					t.Fatal("token hamnade i gh-kommandots argument")
				}
				if err := os.MkdirAll(filepath.Join(sokvag, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				return nil, nil
			case "git":
				gitMiljo = slices.Clone(miljo)
				vantade := []string{"-C", sokvag, "remote", "get-url", "origin"}
				if !slices.Equal(args, vantade) {
					t.Fatalf("fel git-argument: %v", args)
				}
				return []byte("git@github.com:AGARE/REPO.git\n"), nil
			default:
				t.Fatalf("oväntat program: %q", program)
				return nil, nil
			}
		},
	)
	klient.hamtaToken = func(string) string { return token }

	stada, err := klient.KlonaRepo(context.Background(), "https://github.com/Agare/Repo.git", sokvag)
	if err != nil {
		t.Fatal(err)
	}
	if miljovarde(ghMiljo, "GH_TOKEN") != token || miljovarde(ghMiljo, "GH_CONFIG_DIR") == "" {
		t.Fatalf("gh fick inte den isolerade miljön: %v", ghMiljo)
	}
	if strings.Contains(strings.Join(gitMiljo, "\n"), token) {
		t.Fatalf("git-processen fick GitHub-token: %v", gitMiljo)
	}
	if !slices.Contains(gitMiljo, "GIT_CONFIG_NOSYSTEM=1") || !slices.Contains(gitMiljo, "HOME=") {
		t.Fatalf("git-processen fick inte en rensad miljö: %v", gitMiljo)
	}
	if err := stada(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sokvag); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("städningen tog inte bort kloningen: %v", err)
	}
}

func TestKlonaRepoNekarInnanProjektmappenSkapas(t *testing.T) {
	for namn, testfall := range map[string]struct {
		konfig GitHubKonfig
		repo   string
		besked string
	}{
		"utanför tillåtelselistan": {
			konfig: GitHubKonfig{TillatnaRepon: []string{"agare/tillatet"}},
			repo:   "agare/annat",
			besked: "tillåtelselista",
		},
		"spärrat trots tillåtelse": {
			konfig: GitHubKonfig{
				TillatnaRepon: []string{"agare/repo"},
				SparradeRepon: []string{"github.com/agare/repo"},
			},
			repo:   "Agare/Repo",
			besked: "spärrlistan",
		},
	} {
		t.Run(namn, func(t *testing.T) {
			sokvag := filepath.Join(t.TempDir(), "ska-inte-skapas")
			startad := false
			klient := testGitHubKlient(testfall.konfig,
				func(context.Context, string, []string, []string) ([]byte, error) {
					startad = true
					return nil, nil
				})

			_, err := klient.KlonaRepo(context.Background(), testfall.repo, sokvag)
			if err == nil || !strings.Contains(err.Error(), testfall.besked) {
				t.Fatalf("väntade besked om %s, fick: %v", testfall.besked, err)
			}
			if startad {
				t.Fatal("PM startade ett kommando för ett nekat repo")
			}
			if _, err := os.Stat(sokvag); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("PM skapade mappen före vakten: %v", err)
			}
		})
	}
}

func TestKlonaRepoStadarHalvKloningOchMaskerarToken(t *testing.T) {
	const token = "token-som-inte-far-synas"
	sokvag := filepath.Join(t.TempDir(), "tom")
	if err := os.Mkdir(sokvag, 0o750); err != nil {
		t.Fatal(err)
	}
	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"agare/repo"}},
		func(_ context.Context, program string, _ []string, _ []string) ([]byte, error) {
			if program != "/prov/gh" {
				t.Fatalf("origin ska inte kontrolleras efter kloningsfel: %q", program)
			}
			if err := os.WriteFile(filepath.Join(sokvag, "halv-fil"), []byte("halv"), 0o644); err != nil {
				t.Fatal(err)
			}
			return []byte("utdata med " + token), errors.New("fel med " + token)
		},
	)
	klient.hamtaToken = func(string) string { return token }

	_, err := klient.KlonaRepo(context.Background(), "agare/repo", sokvag)
	if err == nil {
		t.Fatal("väntade kloningsfel")
	}
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[hemlighet dold]") {
		t.Fatalf("token maskerades inte: %v", err)
	}
	poster, lasfel := os.ReadDir(sokvag)
	if lasfel != nil {
		t.Fatalf("den befintliga tomma katalogen återställdes inte: %v", lasfel)
	}
	if len(poster) != 0 {
		t.Fatalf("halva kloningen blev kvar: %v", poster)
	}
}

func TestKlonaRepoStadarNarOriginAvviker(t *testing.T) {
	sokvag := filepath.Join(t.TempDir(), "klon")
	klient := testGitHubKlient(
		GitHubKonfig{TillatnaRepon: []string{"agare/repo"}},
		func(_ context.Context, program string, _ []string, _ []string) ([]byte, error) {
			if program == "/prov/gh" {
				if err := os.MkdirAll(filepath.Join(sokvag, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				return nil, nil
			}
			return []byte("https://github.com/annan/repo.git\n"), nil
		},
	)

	_, err := klient.KlonaRepo(context.Background(), "agare/repo", sokvag)
	if err == nil || !strings.Contains(err.Error(), "origin pekar inte") {
		t.Fatalf("väntade originfel, fick: %v", err)
	}
	if _, err := os.Stat(sokvag); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("kloningen blev kvar efter originfelet: %v", err)
	}
}
