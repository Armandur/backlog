package pm

import (
	"fmt"
	"net/url"
	"strings"
)

// GitHubKonfig begränsar vilka GitHub-repon PM får läsa och skriva.
type GitHubKonfig struct {
	TillatnaRepon []string `toml:"tillatna_repon" json:"tillatna_repon"`
	SparradeRepon []string `toml:"sparrade_repon" json:"sparrade_repon"`
	Skrivlage     bool     `toml:"skrivlage" json:"skrivlage"`
}

// NormaliseraGitHubRepo ger ett repo identiteten github.com/ägare/repo.
func NormaliseraGitHubRepo(repo string) (string, error) {
	varde := strings.TrimSpace(repo)
	if varde == "" {
		return "", fmt.Errorf("repo saknas")
	}
	if strings.HasPrefix(strings.ToLower(varde), "git@github.com:") {
		varde = "ssh://git@github.com/" + varde[len("git@github.com:"):]
	} else if !strings.Contains(varde, "://") {
		delar := strings.Split(strings.Trim(varde, "/"), "/")
		if len(delar) == 2 && !strings.ContainsAny(delar[0], ".:@") {
			varde = "https://github.com/" + varde
		} else {
			varde = "https://" + varde
		}
	}

	u, err := url.Parse(varde)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("repo %q har ett ogiltigt format", repo)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ssh", "git":
	default:
		return "", fmt.Errorf("repo %q har ett ogiltigt format", repo)
	}
	if !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" {
		return "", fmt.Errorf("repo %q ligger inte på github.com", repo)
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.User != nil && u.User.String() != "git") {
		return "", fmt.Errorf("repo %q har ett ogiltigt format", repo)
	}
	delar := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(delar) != 2 || delar[0] == "" || delar[1] == "" {
		return "", fmt.Errorf("repo %q måste ha formatet ägare/repo", repo)
	}
	namn := strings.TrimSuffix(strings.ToLower(delar[1]), ".git")
	if namn == "" {
		return "", fmt.Errorf("repo %q saknar reponamn", repo)
	}
	return "github.com/" + strings.ToLower(delar[0]) + "/" + namn, nil
}

// GitHubRepoTillatet avgör om PM får använda repot och förklarar beslutet.
func GitHubRepoTillatet(k GitHubKonfig, repo string, skrivning bool) (bool, string) {
	normaliserat, err := NormaliseraGitHubRepo(repo)
	if err != nil {
		return false, err.Error()
	}
	if err := k.validera(); err != nil {
		return false, err.Error()
	}

	if finnsGitHubRepo(k.SparradeRepon, normaliserat) {
		return false, fmt.Sprintf("repot %s finns i GitHub-spärrlistan", normaliserat)
	}
	if !finnsGitHubRepo(k.TillatnaRepon, normaliserat) {
		return false, fmt.Sprintf("repot %s finns inte i GitHubs tillåtelselista", normaliserat)
	}
	if skrivning && !k.Skrivlage {
		return false, fmt.Sprintf("skrivläget är avstängt för repot %s", normaliserat)
	}
	if skrivning {
		return true, fmt.Sprintf("repot %s är tillåtet för skrivning", normaliserat)
	}
	return true, fmt.Sprintf("repot %s är tillåtet för läsning", normaliserat)
}

func finnsGitHubRepo(repon []string, sokt string) bool {
	for _, repo := range repon {
		normaliserat, err := NormaliseraGitHubRepo(repo)
		if err == nil && normaliserat == sokt {
			return true
		}
	}
	return false
}

func (k GitHubKonfig) validera() error {
	for _, lista := range []struct {
		namn  string
		repon []string
	}{
		{namn: "tillåtelselistan", repon: k.TillatnaRepon},
		{namn: "spärrlistan", repon: k.SparradeRepon},
	} {
		for _, repo := range lista.repon {
			if _, err := NormaliseraGitHubRepo(repo); err != nil {
				return fmt.Errorf("GitHubs %s innehåller ett ogiltigt repo: %w", lista.namn, err)
			}
		}
	}
	return nil
}
