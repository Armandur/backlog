package pmweb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mazen160/backlog/internal/service"
)

const (
	maxFilstorlek      int64 = 1 << 20
	maxMediastorlek    int64 = 100 << 20
	filSakerhetspolicy       = "default-src 'none'; sandbox"
)

type filpost struct {
	Namn    string `json:"namn"`
	Sokvag  string `json:"sokvag"`
	Katalog bool   `json:"katalog"`
	Symlank bool   `json:"symlank,omitempty"`
	Storlek int64  `json:"storlek,omitempty"`
	Andrad  string `json:"andrad"`
}

type filsvar struct {
	Sokvag   string    `json:"sokvag"`
	Foralder string    `json:"foralder,omitempty"`
	Katalog  bool      `json:"katalog"`
	Poster   []filpost `json:"poster,omitempty"`
	Namn     string    `json:"namn,omitempty"`
	Storlek  int64     `json:"storlek,omitempty"`
	Andrad   string    `json:"andrad,omitempty"`
	Medietyp string    `json:"medietyp,omitempty"`
	Innehall string    `json:"innehall,omitempty"`
}

func (s *Server) hamtaFiler(w http.ResponseWriter, r *http.Request) {
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

	info, err := rot.Stat(relativ)
	if err != nil {
		kod := http.StatusInternalServerError
		if errors.Is(err, fs.ErrNotExist) {
			kod = http.StatusNotFound
		}
		svaraFel(w, errors.New("PM kunde inte läsa den valda sökvägen"), kod)
		return
	}
	if info.IsDir() {
		s.listaFiler(w, rot, relativ)
		return
	}
	s.lasFil(w, rot, relativ, info)
}

func sakerReposokvag(repo, begard string) (string, string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", "", errors.New("projektet saknar en repo-sökväg")
	}
	for _, del := range strings.FieldsFunc(begard, func(r rune) bool { return r == '/' || r == '\\' }) {
		if del == ".." {
			return "", "", errors.New("sökvägen får inte innehålla ..")
		}
		if del == ".git" {
			return "", "", errors.New(".git-katalogen visas inte i filbläddraren")
		}
	}
	if filepath.IsAbs(begard) {
		return "", "", errors.New("sökvägen måste ligga under projektets repo")
	}

	bas, err := filepath.Abs(filepath.Clean(repo))
	if err != nil {
		return "", "", errors.New("PM kunde inte läsa projektets repo-sökväg")
	}
	bas, err = filepath.EvalSymlinks(bas)
	if err != nil {
		return "", "", errors.New("PM kunde inte hitta projektets repo")
	}
	relativ := filepath.Clean(begard)
	if relativ == "" {
		relativ = "."
	}
	mål := filepath.Join(bas, relativ)
	relativ, err = filepath.Rel(bas, mål)
	if err != nil || relativ == ".." || strings.HasPrefix(relativ, ".."+string(filepath.Separator)) {
		return "", "", errors.New("sökvägen måste ligga under projektets repo")
	}
	if err := kontrolleraSymlankar(bas, relativ); err != nil {
		return "", "", err
	}
	return bas, relativ, nil
}

func (s *Server) listaFiler(w http.ResponseWriter, rot *os.Root, relativ string) {
	katalog, err := rot.Open(relativ)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte öppna katalogen"), http.StatusInternalServerError)
		return
	}
	defer katalog.Close()
	poster, err := katalog.ReadDir(-1)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa katalogen"), http.StatusInternalServerError)
		return
	}
	svar := filsvar{Sokvag: webbSokvag(relativ), Katalog: true, Poster: make([]filpost, 0, len(poster))}
	if relativ != "." {
		foralder := filepath.Dir(relativ)
		if foralder == "." {
			foralder = ""
		}
		svar.Foralder = webbSokvag(foralder)
	}
	for _, post := range poster {
		if post.Name() == ".git" {
			continue
		}
		info, infofel := post.Info()
		if infofel != nil {
			continue
		}
		svar.Poster = append(svar.Poster, filpost{
			Namn: post.Name(), Sokvag: webbSokvag(filepath.Join(relativ, post.Name())),
			Katalog: post.IsDir(), Symlank: post.Type()&os.ModeSymlink != 0, Storlek: info.Size(),
			Andrad: info.ModTime().Format(time.RFC3339),
		})
	}
	sort.Slice(svar.Poster, func(i, j int) bool {
		if svar.Poster[i].Katalog != svar.Poster[j].Katalog {
			return svar.Poster[i].Katalog
		}
		return strings.ToLower(svar.Poster[i].Namn) < strings.ToLower(svar.Poster[j].Namn)
	})
	svaraJSON(w, http.StatusOK, svar)
}

func (s *Server) lasFil(w http.ResponseWriter, rot *os.Root, relativ string, info fs.FileInfo) {
	if !info.Mode().IsRegular() {
		svaraFel(w, errors.New("filbläddraren kan bara läsa vanliga filer"), http.StatusBadRequest)
		return
	}
	fil, err := rot.Open(relativ)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte öppna filen"), http.StatusInternalServerError)
		return
	}
	defer fil.Close()
	prov := make([]byte, min(info.Size(), 8192))
	antal, lasfel := io.ReadFull(fil, prov)
	if lasfel != nil && !errors.Is(lasfel, io.EOF) && !errors.Is(lasfel, io.ErrUnexpectedEOF) {
		svaraFel(w, errors.New("PM kunde inte läsa filen"), http.StatusInternalServerError)
		return
	}
	prov = prov[:antal]
	medietyp, _ := identifieraMedia(prov)
	if medietyp != "" {
		if info.Size() > maxMediastorlek {
			svaraFel(w, errors.New("filen är "+formateraByte(info.Size())+" och överskrider gränsen 100 MiB"), http.StatusRequestEntityTooLarge)
			return
		}
		svaraJSON(w, http.StatusOK, filsvar{
			Sokvag: webbSokvag(relativ), Namn: filepath.Base(relativ), Storlek: info.Size(),
			Andrad: info.ModTime().Format(time.RFC3339), Medietyp: medietyp,
		})
		return
	}
	if info.Size() > maxFilstorlek {
		svaraFel(w, errors.New("filen är "+formateraByte(info.Size())+" och överskrider gränsen 1 MiB"), http.StatusRequestEntityTooLarge)
		return
	}
	if arBinarFil(prov) {
		svaraFel(w, errors.New("filen är binär. PM kan inte visa den som text"), http.StatusUnsupportedMediaType)
		return
	}
	data, err := io.ReadAll(io.LimitReader(io.MultiReader(bytes.NewReader(prov), fil), maxFilstorlek+1))
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa filen"), http.StatusInternalServerError)
		return
	}
	if int64(len(data)) > maxFilstorlek {
		svaraFel(w, errors.New("filen växte under läsningen och överskrider gränsen 1 MiB"), http.StatusRequestEntityTooLarge)
		return
	}
	if !utf8.Valid(data) {
		svaraFel(w, errors.New("filen är binär. PM kan inte visa den som text"), http.StatusUnsupportedMediaType)
		return
	}
	svaraJSON(w, http.StatusOK, filsvar{
		Sokvag: webbSokvag(relativ), Namn: filepath.Base(relativ), Storlek: info.Size(),
		Andrad: info.ModTime().Format(time.RFC3339), Innehall: string(data),
	})
}

func (s *Server) hamtaFilinnehall(w http.ResponseWriter, r *http.Request) {
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
	info, err := rot.Stat(relativ)
	if err != nil || !info.Mode().IsRegular() {
		svaraFel(w, errors.New("PM kunde inte läsa den valda filen"), http.StatusNotFound)
		return
	}
	if info.Size() > maxMediastorlek {
		svaraFel(w, errors.New("filen är "+formateraByte(info.Size())+" och överskrider gränsen 100 MiB"), http.StatusRequestEntityTooLarge)
		return
	}
	fil, err := rot.Open(relativ)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte öppna filen"), http.StatusInternalServerError)
		return
	}
	defer fil.Close()
	prov := make([]byte, min(info.Size(), 512))
	antal, lasfel := io.ReadFull(fil, prov)
	if lasfel != nil && !errors.Is(lasfel, io.EOF) && !errors.Is(lasfel, io.ErrUnexpectedEOF) {
		svaraFel(w, errors.New("PM kunde inte läsa filen"), http.StatusInternalServerError)
		return
	}
	prov = prov[:antal]
	medietyp, innehallstyp := identifieraMedia(prov)
	nedladdning := r.URL.Query().Get("download") == "1"
	if !nedladdning && medietyp == "" {
		svaraFel(w, errors.New("PM visar bara bilder och video via medierouten"), http.StatusUnsupportedMediaType)
		return
	}
	if nedladdning {
		innehallstyp = http.DetectContentType(prov)
	}
	if _, err := fil.Seek(0, io.SeekStart); err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa filen"), http.StatusInternalServerError)
		return
	}
	disposition := "inline"
	if nedladdning {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", innehallstyp)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filepath.Base(relativ)}))
	w.Header().Set("Content-Security-Policy", filSakerhetspolicy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	http.ServeContent(w, r, filepath.Base(relativ), info.ModTime(), fil)
}

func identifieraMedia(prov []byte) (string, string) {
	innehallstyp := http.DetectContentType(prov)
	switch innehallstyp {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return "bild", innehallstyp
	case "video/mp4", "video/webm", "video/ogg":
		return "video", innehallstyp
	default:
		return "", ""
	}
}

func arBinarFil(prov []byte) bool {
	if bytes.IndexByte(prov, 0) >= 0 {
		return true
	}
	typ := http.DetectContentType(prov)
	return !strings.HasPrefix(typ, "text/") && typ != "application/json"
}

func webbSokvag(sokvag string) string {
	if sokvag == "." {
		return ""
	}
	return filepath.ToSlash(sokvag)
}

func formateraByte(storlek int64) string {
	return fmt.Sprintf("%d byte", storlek)
}
