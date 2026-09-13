package pmweb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mazen160/backlog/internal/service"
)

const (
	maxFilsoktraffar = 100
	filsokTidsgrans  = 2 * time.Second
	// En minifierad fil kan ha hela innehållet på en rad. Träfftexten kapas
	// därför, annars skickar en sökning megabyte till webbläsaren.
	maxFilsokradlangd = 300
)

type filsoktraff struct {
	Fil  string `json:"fil"`
	Rad  int    `json:"rad"`
	Text string `json:"text"`
}

type filsoksvar struct {
	Fraga     string        `json:"fraga"`
	Traffar   []filsoktraff `json:"traffar"`
	Begransad bool          `json:"begransad"`
}

func (s *Server) sokFiler(w http.ResponseWriter, r *http.Request) {
	fraga := r.URL.Query().Get("q")
	if strings.TrimSpace(fraga) == "" {
		svaraFel(w, errors.New("ange texten som PM ska söka efter"), http.StatusBadRequest)
		return
	}
	projekt, err := service.NewProjectService(s.db).GetByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	repo, relativ, err := sakerReposokvag(projekt.RepoPath, r.URL.Query().Get("path"))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	rot, err := os.OpenRoot(repo)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte öppna projektets repo"), http.StatusInternalServerError)
		return
	}
	defer rot.Close()

	ctx, avbryt := context.WithTimeout(r.Context(), filsokTidsgrans)
	defer avbryt()
	traffar, begransad, err := sokTextfiler(ctx, rot, relativ, fraga)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte söka i projektets filer"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, filsoksvar{
		Fraga: fraga, Traffar: traffar, Begransad: begransad,
	})
}

func sokTextfiler(ctx context.Context, rot *os.Root, start, fraga string) ([]filsoktraff, bool, error) {
	traffar := make([]filsoktraff, 0)
	soktext := strings.ToLower(fraga)
	begransad := false
	err := fs.WalkDir(rot.FS(), start, func(sokvag string, post fs.DirEntry, gangfel error) error {
		if err := ctx.Err(); err != nil {
			begransad = true
			return err
		}
		if gangfel != nil {
			if sokvag == start {
				return gangfel
			}
			return nil
		}
		if post.IsDir() && post.Name() == ".git" {
			return fs.SkipDir
		}
		if post.IsDir() || post.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := post.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxFilstorlek {
			return nil
		}
		fil, err := rot.Open(sokvag)
		if err != nil {
			return nil
		}
		data, lasfel := io.ReadAll(io.LimitReader(fil, maxFilstorlek+1))
		fil.Close()
		if lasfel != nil || int64(len(data)) > maxFilstorlek || !utf8.Valid(data) {
			return nil
		}
		prov := data[:min(len(data), 8192)]
		if arBinarFil(prov) {
			return nil
		}
		for radnummer, rad := range bytes.Split(data, []byte("\n")) {
			if err := ctx.Err(); err != nil {
				begransad = true
				return err
			}
			rad = bytes.TrimSuffix(rad, []byte("\r"))
			if strings.Contains(strings.ToLower(string(rad)), soktext) {
				traffar = append(traffar, filsoktraff{
					Fil: webbSokvag(filepath.Clean(sokvag)), Rad: radnummer + 1, Text: kapaTraffrad(string(rad)),
				})
				if len(traffar) == maxFilsoktraffar {
					begransad = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return traffar, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return traffar, begransad, nil
}

// kapaTraffrad klipper långa rader på teckengräns, aldrig mitt i ett tecken.
func kapaTraffrad(rad string) string {
	if utf8.RuneCountInString(rad) <= maxFilsokradlangd {
		return rad
	}
	tecken := []rune(rad)
	return string(tecken[:maxFilsokradlangd]) + "..."
}
