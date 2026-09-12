package pmweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mazen160/backlog/internal/service"
)

const maxGitUtdata = 1 << 20

type gitAndring struct {
	Sokvag string `json:"sokvag"`
	Status string `json:"status"`
	Typ    string `json:"typ"`
}

type gitAndringssvar struct {
	Andringar []gitAndring `json:"andringar"`
}

type gitDiffsvar struct {
	Sokvag string `json:"sokvag"`
	Diff   string `json:"diff"`
}

func (s *Server) hamtaGitAndringar(w http.ResponseWriter, r *http.Request) {
	repo, err := s.gitRepo(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusUnprocessableEntity)
		return
	}
	utdata, forStor, err := korGitBegransat(r.Context(), repo, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if forStor {
		svaraFel(w, errors.New("listan över ändringar överskrider gränsen 1 MiB"), http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa Git-status för projektet"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, gitAndringssvar{Andringar: tolkaGitStatus(utdata)})
}

func (s *Server) hamtaGitDiff(w http.ResponseWriter, r *http.Request) {
	repo, err := s.gitRepo(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusUnprocessableEntity)
		return
	}
	_, relativ, err := sakerReposokvag(repo, r.URL.Query().Get("path"))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	if relativ == "." {
		svaraFel(w, errors.New("välj en fil för att visa dess diff"), http.StatusBadRequest)
		return
	}

	argument := []string{"diff", "HEAD", "--no-ext-diff", "--no-textconv", "--", relativ}
	spårad := exec.CommandContext(r.Context(), "git", "--no-optional-locks", "-C", repo, "ls-files", "--error-unmatch", "--", relativ).Run() == nil
	if !spårad {
		rot, oppningsfel := os.OpenRoot(repo)
		if oppningsfel != nil {
			svaraFel(w, errors.New("PM kunde inte öppna projektets repo"), http.StatusInternalServerError)
			return
		}
		info, statfel := rot.Stat(relativ)
		rot.Close()
		if statfel != nil || !info.Mode().IsRegular() {
			svaraFel(w, errors.New("den valda filen finns inte i arbetsträdet eller Git"), http.StatusNotFound)
			return
		}
		argument = []string{"diff", "--no-index", "--no-ext-diff", "--no-textconv", "--", os.DevNull, relativ}
	}

	utdata, forStor, diffFel := korGitBegransat(r.Context(), repo, argument...)
	if forStor {
		svaraFel(w, errors.New("diffen överskrider gränsen 1 MiB"), http.StatusRequestEntityTooLarge)
		return
	}
	var exitFel *exec.ExitError
	forvantadNoIndex := !spårad && errors.As(diffFel, &exitFel) && exitFel.ExitCode() == 1
	if diffFel != nil && !forvantadNoIndex {
		svaraFel(w, errors.New("PM kunde inte läsa filens diff mot HEAD"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, gitDiffsvar{Sokvag: webbSokvag(relativ), Diff: string(utdata)})
}

func (s *Server) gitRepo(ctx context.Context, alias string) (string, error) {
	projekt, err := service.NewProjectService(s.db).GetByAlias(ctx, alias)
	if err != nil {
		return "", errors.New("projektet finns inte")
	}
	repo, _, err := sakerReposokvag(projekt.RepoPath, "")
	if err != nil {
		return "", err
	}
	kommando := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", repo, "rev-parse", "--show-toplevel")
	kommando.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	utdata, err := kommando.Output()
	if err != nil {
		return "", errors.New("projektmappen är inte ett Git-repo")
	}
	gitrot, err := filepath.EvalSymlinks(strings.TrimSpace(string(utdata)))
	if err != nil || filepath.Clean(gitrot) != filepath.Clean(repo) {
		return "", errors.New("projektmappen är inte roten i ett Git-repo")
	}
	return repo, nil
}

func tolkaGitStatus(utdata []byte) []gitAndring {
	poster := bytes.Split(utdata, []byte{0})
	andringar := make([]gitAndring, 0, len(poster))
	for i := 0; i < len(poster); i++ {
		post := poster[i]
		if len(post) < 4 {
			continue
		}
		status := string(post[:2])
		andringar = append(andringar, gitAndring{
			Sokvag: filepath.ToSlash(string(post[3:])), Status: status, Typ: gitStatustyp(status),
		})
		if status[0] == 'R' || status[0] == 'C' {
			i++
		}
	}
	return andringar
}

func gitStatustyp(status string) string {
	switch {
	case status == "??":
		return "Ny"
	case strings.Contains(status, "D"):
		return "Borttagen"
	case strings.Contains(status, "R"):
		return "Namnändrad"
	case strings.Contains(status, "A"):
		return "Tillagd"
	default:
		return "Ändrad"
	}
}

type begransadGitBuffer struct {
	buffer  bytes.Buffer
	forStor bool
}

func (b *begransadGitBuffer) Write(data []byte) (int, error) {
	antal := len(data)
	plats := maxGitUtdata + 1 - b.buffer.Len()
	if plats > 0 {
		if plats > antal {
			plats = antal
		}
		_, _ = b.buffer.Write(data[:plats])
	}
	if b.buffer.Len() > maxGitUtdata || antal > plats {
		b.forStor = true
	}
	return antal, nil
}

func korGitBegransat(ctx context.Context, repo string, argument ...string) ([]byte, bool, error) {
	args := append([]string{"--no-optional-locks", "-C", repo}, argument...)
	kommando := exec.CommandContext(ctx, "git", args...)
	kommando.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var utdata begransadGitBuffer
	var felutdata bytes.Buffer
	kommando.Stdout = &utdata
	kommando.Stderr = &felutdata
	err := kommando.Run()
	if err != nil && felutdata.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(felutdata.String()))
	}
	return utdata.buffer.Bytes(), utdata.forStor, err
}
