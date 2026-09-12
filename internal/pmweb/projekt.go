package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

var projektBasDir = standardProjektBasDir

func standardProjektBasDir() (string, error) {
	hemkatalog, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("PM hittar inte din hemkatalog")
	}
	return filepath.Join(hemkatalog, "workspace"), nil
}

type skapaProjektBody struct {
	Alias       string `json:"alias"`
	Namn        string `json:"namn"`
	Beskrivning string `json:"beskrivning"`
	Lage        string `json:"lage"`
	Sokvag      string `json:"sokvag"`
	// Startkommando är valfritt och blir projektets testserver.
	Startkommando string `json:"startkommando"`
}

func (s *Server) skapaProjekt(w http.ResponseWriter, r *http.Request) {
	var body skapaProjektBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa projektuppgifterna"), http.StatusBadRequest)
		return
	}
	body.Alias = strings.TrimSpace(body.Alias)
	body.Namn = strings.TrimSpace(body.Namn)
	body.Beskrivning = strings.TrimSpace(body.Beskrivning)
	body.Lage = strings.TrimSpace(body.Lage)

	sokvag, err := sakerProjektSokvag(body.Sokvag)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}

	var stada func() error
	switch body.Lage {
	case "nytt":
		stada, err = sattUppProjekt(r.Context(), sokvag, body.Namn)
	case "befintligt":
		err = kontrolleraBefintligtRepo(sokvag)
	default:
		err = errors.New("välj om PM ska skapa ett nytt projekt eller koppla ett befintligt repo")
	}
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}

	projekt, err := service.NewProjectService(s.db).Create(r.Context(), models.CreateProjectInput{
		Alias: body.Alias, Name: body.Namn, Description: body.Beskrivning, RepoPath: sokvag, Actor: s.aktor,
	})
	if err != nil {
		if stada != nil {
			if stadfel := stada(); stadfel != nil {
				svaraFel(w, errors.New("PM kunde inte återställa arbetsmappen efter felet"), http.StatusInternalServerError)
				return
			}
		}
		meddelande, kod := begripligtProjektfel(err, body.Alias)
		svaraFel(w, errors.New(meddelande), kod)
		return
	}

	svar := map[string]any{
		"projekt": projekt,
		"lank":    "/pm/" + projekt.Alias,
	}
	// Konfigurationen skrivs efter projektet, annars känner den inte igen
	// aliaset. Ett fel här får inte kasta bort projektet som redan finns.
	if varning := sparaStartkommando(projekt.Alias, sokvag, body.Startkommando); varning != "" {
		svar["varning"] = varning
	}
	svaraJSON(w, http.StatusCreated, svar)
}

// sparaStartkommando lägger projektets testserver i pm.toml. Returnerar ett
// besked till användaren när kommandot inte gick att spara.
func sparaStartkommando(alias, repo, kommando string) string {
	delar := strings.Fields(kommando)
	if len(delar) == 0 {
		return ""
	}
	workspace := konfigWorkDir()
	konfig, err := pm.LasKonfig(workspace)
	if err != nil {
		return "Projektet är klart, men startkommandot kunde inte sparas: konfigurationen går inte att läsa. Lägg in det i pm.toml."
	}
	if konfig.Testserver == nil {
		konfig.Testserver = map[string]pm.TestserverKonfig{}
	}
	konfig.Testserver[alias] = pm.TestserverKonfig{
		Kommando: delar[0], Args: delar[1:], CWD: repo, Halsa: "/",
	}
	if err := pm.SkrivKonfig(workspace, konfig); err != nil {
		return "Projektet är klart, men startkommandot kunde inte sparas. Lägg in det under Konfig."
	}
	return ""
}

func sakerProjektSokvag(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("ange projektets sökväg")
	}
	for _, del := range strings.Split(filepath.ToSlash(in), "/") {
		if del == ".." {
			return "", errors.New("sökvägen får inte innehålla ..")
		}
	}

	bas, err := projektBasDir()
	if err != nil {
		return "", err
	}
	bas, err = filepath.Abs(filepath.Clean(bas))
	if err != nil {
		return "", errors.New("PM kunde inte läsa basmappen")
	}
	if upplost, evalfel := filepath.EvalSymlinks(bas); evalfel == nil {
		bas = upplost
	}

	sokvag := filepath.Clean(in)
	if !filepath.IsAbs(sokvag) {
		sokvag = filepath.Join(bas, sokvag)
	}
	sokvag, err = filepath.Abs(sokvag)
	if err != nil {
		return "", errors.New("PM kunde inte läsa sökvägen")
	}
	relativ, err := filepath.Rel(bas, sokvag)
	if err != nil || relativ == "." || relativ == ".." || strings.HasPrefix(relativ, ".."+string(filepath.Separator)) {
		return "", errors.New("sökvägen måste ligga under ~/workspace")
	}
	if err := kontrolleraSymlankar(bas, relativ); err != nil {
		return "", err
	}
	return sokvag, nil
}

func kontrolleraSymlankar(bas, relativ string) error {
	aktuell := bas
	for _, del := range strings.Split(relativ, string(filepath.Separator)) {
		aktuell = filepath.Join(aktuell, del)
		info, err := os.Lstat(aktuell)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.New("PM kunde inte kontrollera sökvägen")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("sökvägen får inte gå genom en symbolisk länk")
		}
	}
	return nil
}

func sattUppProjekt(ctx context.Context, sokvag, namn string) (func() error, error) {
	stada, err := forberedTomKatalog(sokvag)
	if err != nil {
		return nil, err
	}
	misslyckades := true
	defer func() {
		if misslyckades {
			_ = stada()
		}
	}()

	if err := korGit(ctx, sokvag, "init"); err != nil {
		return nil, errors.New("PM kunde inte starta Git i projektmappen")
	}
	readme := []byte("# " + namn + "\n")
	if err := os.WriteFile(filepath.Join(sokvag, "README.md"), readme, 0o644); err != nil {
		return nil, errors.New("PM kunde inte skriva README.md")
	}
	if err := korGit(ctx, sokvag, "add", "README.md"); err != nil {
		return nil, errors.New("PM kunde inte lägga README.md i Git")
	}
	if err := korGit(ctx, sokvag, "-c", "user.name=backlog-pm", "-c", "user.email=backlog-pm@localhost", "commit", "-m", "Skapa projektet"); err != nil {
		return nil, errors.New("PM kunde inte göra den första Git-committen")
	}
	misslyckades = false
	return stada, nil
}

func forberedTomKatalog(sokvag string) (func() error, error) {
	info, err := os.Stat(sokvag)
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
		return func() error {
			if err := os.RemoveAll(filepath.Join(sokvag, ".git")); err != nil {
				return err
			}
			err := os.Remove(filepath.Join(sokvag, "README.md"))
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("PM kunde inte kontrollera projektmappen")
	}

	rot := forstaSaknadeKatalog(sokvag)
	if err := os.MkdirAll(sokvag, 0o755); err != nil {
		return nil, errors.New("PM kunde inte skapa projektmappen")
	}
	return func() error { return os.RemoveAll(rot) }, nil
}

func forstaSaknadeKatalog(sokvag string) string {
	rot := sokvag
	for aktuell := filepath.Dir(sokvag); aktuell != filepath.Dir(aktuell); aktuell = filepath.Dir(aktuell) {
		if _, err := os.Stat(aktuell); err == nil {
			break
		}
		rot = aktuell
	}
	return rot
}

func korGit(ctx context.Context, repo string, argument ...string) error {
	args := append([]string{"-C", repo}, argument...)
	return exec.CommandContext(ctx, "git", args...).Run()
}

func kontrolleraBefintligtRepo(sokvag string) error {
	info, err := os.Stat(sokvag)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("projektmappen finns inte")
	}
	if err != nil || !info.IsDir() {
		return errors.New("sökvägen måste vara en katalog")
	}
	gitInfo, err := os.Lstat(filepath.Join(sokvag, ".git"))
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("katalogen saknar ett Git-repo")
	}
	if err != nil || !gitInfo.IsDir() {
		return errors.New("katalogens .git måste vara en katalog")
	}
	return nil
}

// begripligtProjektfel översätter service-lagrets fel till svensk text. Den
// frågar med errors.Is, så en omformulering i service-lagret ändrar inte vad
// användaren får se.
func begripligtProjektfel(err error, alias string) (string, int) {
	switch {
	case errors.Is(err, service.ErrAliasTaken):
		return fmt.Sprintf("aliaset %q används redan", alias), http.StatusConflict
	case errors.Is(err, service.ErrAliasRequired):
		return "ange ett alias", http.StatusBadRequest
	case errors.Is(err, service.ErrAliasInvalid):
		return "alias får bara innehålla små bokstäver, siffror och bindestreck", http.StatusBadRequest
	case errors.Is(err, service.ErrAliasTooLong):
		return "alias får innehålla högst 64 tecken", http.StatusBadRequest
	case errors.Is(err, service.ErrNameRequired):
		return "ange projektets namn", http.StatusBadRequest
	case errors.Is(err, service.ErrNameTooLong):
		return "projektets namn får innehålla högst 255 tecken", http.StatusBadRequest
	default:
		return "PM kunde inte registrera projektet", http.StatusInternalServerError
	}
}
