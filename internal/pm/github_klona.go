package pm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// KlonaRepo klonar ett tillåtet repo och kontrollerar dess origin.
// Städningen tar bara bort kataloger som kloningen fick använda.
func (k *GitHubKlient) KlonaRepo(ctx context.Context, repo, sokvag string) (stada func() error, err error) {
	tillatet, skal := GitHubRepoTillatet(k.konfig, repo, false)
	if !tillatet {
		return nil, fmt.Errorf("GitHub-vakten nekade kloningen: %s", skal)
	}
	normaliserat, err := NormaliseraGitHubRepo(repo)
	if err != nil {
		return nil, fmt.Errorf("GitHub-vakten nekade kloningen: %s", err)
	}

	stada, err = forberedKloningsmal(sokvag)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil {
			return
		}
		if stadfel := stada(); stadfel != nil {
			err = errors.New("PM kunde inte städa projektmappen efter den misslyckade kloningen")
		}
		stada = nil
	}()

	namn := strings.TrimPrefix(normaliserat, "github.com/")
	if _, err = k.kor(ctx, []string{"repo", "clone", namn, sokvag, "--", "--origin", "origin"}, true); err != nil {
		return stada, fmt.Errorf("PM kunde inte klona GitHub-repot: %w", err)
	}

	var origin string
	origin, err = k.hamtaOrigin(ctx, sokvag)
	if err != nil {
		return stada, err
	}
	if origin != normaliserat {
		return stada, errors.New("det klonade repots origin pekar inte på det begärda GitHub-repot")
	}
	return stada, nil
}

func (k *GitHubKlient) hamtaOrigin(ctx context.Context, sokvag string) (string, error) {
	utdata, korfel := k.korare(ctx, "git", []string{"-C", sokvag, "remote", "get-url", "origin"}, []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=",
	})
	token := k.hamtaToken("GH_TOKEN")
	if korfel != nil {
		sakertFel := doljGitHubToken(korfel.Error(), token)
		return "", fmt.Errorf("PM kunde inte kontrollera repots origin: %s", sakertFel)
	}
	sakerOrigin := strings.TrimSpace(doljGitHubToken(string(utdata), token))
	normaliserat, err := NormaliseraGitHubRepo(sakerOrigin)
	if err != nil {
		return "", errors.New("det klonade repots origin har ett ogiltigt GitHub-repo")
	}
	return normaliserat, nil
}

func forberedKloningsmal(sokvag string) (func() error, error) {
	info, err := os.Lstat(sokvag)
	if err == nil {
		if !info.IsDir() {
			return nil, errors.New("sökvägen finns redan och är inte en katalog")
		}
		poster, lasfel := os.ReadDir(sokvag)
		if lasfel != nil {
			return nil, errors.New("PM kunde inte läsa projektmappen")
		}
		if len(poster) > 0 {
			namn := make([]string, 0, len(poster))
			for _, post := range poster {
				namn = append(namn, post.Name())
			}
			return nil, fmt.Errorf("katalogen innehåller redan: %s", strings.Join(namn, ", "))
		}
		mode := info.Mode().Perm()
		return func() error {
			if err := os.RemoveAll(sokvag); err != nil {
				return err
			}
			return os.Mkdir(sokvag, mode)
		}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("PM kunde inte kontrollera projektmappen")
	}

	rot := forstaSaknadeKloningskatalog(sokvag)
	if err := os.MkdirAll(filepath.Dir(sokvag), 0o755); err != nil {
		return nil, errors.New("PM kunde inte förbereda projektmappen")
	}
	return func() error { return os.RemoveAll(rot) }, nil
}

func forstaSaknadeKloningskatalog(sokvag string) string {
	rot := sokvag
	for aktuell := filepath.Dir(sokvag); aktuell != filepath.Dir(aktuell); aktuell = filepath.Dir(aktuell) {
		if _, err := os.Stat(aktuell); err == nil {
			break
		}
		rot = aktuell
	}
	return rot
}
