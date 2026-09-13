package pm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitHubKorare startar ett program direkt med angivna argument och miljö.
// Tester ersätter den för att undvika nätverksanrop.
type GitHubKorare func(context.Context, string, []string, []string) ([]byte, error)

// GitHubKlient kör de GitHub-operationer som PM tillåter.
type GitHubKlient struct {
	konfig     GitHubKonfig
	korare     GitHubKorare
	hittaGH    func(string) (string, error)
	hamtaToken func(string) string
}

// NewGitHubKlient skapar PM:s kontrollerade GitHub-klient.
func NewGitHubKlient(konfig GitHubKonfig) *GitHubKlient {
	return &GitHubKlient{
		konfig:     konfig,
		korare:     korGitHubKommando,
		hittaGH:    exec.LookPath,
		hamtaToken: os.Getenv,
	}
}

// VisaRepo hämtar grunduppgifter om ett tillåtet repo.
func (k *GitHubKlient) VisaRepo(ctx context.Context, repo string) (string, error) {
	return k.korLasning(ctx, repo, func(namn string) []string {
		return []string{"repo", "view", namn, "--json", "nameWithOwner,description,url,defaultBranchRef"}
	})
}

// ListaIssues listar öppna issues i ett tillåtet repo.
func (k *GitHubKlient) ListaIssues(ctx context.Context, repo string) (string, error) {
	return k.korLasning(ctx, repo, func(namn string) []string {
		return []string{"issue", "list", "--repo", namn, "--json", "number,title,state,url"}
	})
}

// ListaPullRequests listar öppna pull requests i ett tillåtet repo.
func (k *GitHubKlient) ListaPullRequests(ctx context.Context, repo string) (string, error) {
	return k.korLasning(ctx, repo, func(namn string) []string {
		return []string{"pr", "list", "--repo", namn, "--json", "number,title,state,isDraft,url"}
	})
}

// Version visar gh-versionen utan att läsa användarens gh-konfiguration.
func (k *GitHubKlient) Version(ctx context.Context) (string, error) {
	return k.kor(ctx, []string{"--version"}, false)
}

func (k *GitHubKlient) korLasning(
	ctx context.Context,
	repo string,
	byggArgs func(string) []string,
) (string, error) {
	tillatet, skal := GitHubRepoTillatet(k.konfig, repo, false)
	if !tillatet {
		return "", fmt.Errorf("GitHub-vakten nekade anropet: %s", skal)
	}
	normaliserat, err := NormaliseraGitHubRepo(repo)
	if err != nil {
		return "", fmt.Errorf("GitHub-vakten nekade anropet: %s", err)
	}
	namn := strings.TrimPrefix(normaliserat, "github.com/")
	return k.kor(ctx, byggArgs(namn), true)
}

func (k *GitHubKlient) kor(ctx context.Context, args []string, kravPaToken bool) (string, error) {
	gh, err := k.hittaGH("gh")
	if err != nil {
		return "", fmt.Errorf("PM hittar inte GitHub CLI (gh). Installera gh och försök igen")
	}
	token := k.hamtaToken("GH_TOKEN")
	if kravPaToken && token == "" {
		return "", fmt.Errorf("PM saknar GitHub-token i GH_TOKEN")
	}

	konfigDir, err := os.MkdirTemp("", "backlog-pm-gh-")
	if err != nil {
		return "", fmt.Errorf("PM kunde inte skapa en isolerad GitHub-miljö: %w", err)
	}
	defer os.RemoveAll(konfigDir)

	miljo := []string{"GH_CONFIG_DIR=" + konfigDir, "GH_PROMPT_DISABLED=1"}
	if token != "" {
		miljo = append(miljo, "GH_TOKEN="+token)
	}
	utdata, korfel := k.korare(ctx, gh, args, miljo)
	sakerUtdata := doljGitHubToken(string(utdata), token)
	if korfel != nil {
		sakertFel := doljGitHubToken(korfel.Error(), token)
		if strings.TrimSpace(sakerUtdata) != "" {
			return "", fmt.Errorf("gh misslyckades: %s: %s", sakertFel, strings.TrimSpace(sakerUtdata))
		}
		return "", fmt.Errorf("gh misslyckades: %s", sakertFel)
	}
	return sakerUtdata, nil
}

func korGitHubKommando(ctx context.Context, program string, args, miljo []string) ([]byte, error) {
	kommando := exec.CommandContext(ctx, program, args...)
	kommando.Env = miljo
	return kommando.CombinedOutput()
}

func doljGitHubToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "[hemlighet dold]")
}
