package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

const (
	// startfilMax är hur mycket PM läser ur varje fil. En hel bundle ryms inte
	// i en prompt, och starten står alltid i början av filen.
	startfilMax = 4000
	// rotfilerMax håller listan läsbar för agenten.
	rotfilerMax = 80
)

// startfiler är filerna som avgör hur ett projekt startas.
var startfiler = []string{
	"package.json", "Makefile", "makefile", "go.mod", "docker-compose.yml",
	"docker-compose.yaml", "Procfile", "pyproject.toml", "requirements.txt",
	"index.html", "README.md",
}

type testserverForslagBody struct {
	Agent string `json:"agent"`
}

type testserverForslag struct {
	Kommando   string   `json:"kommando"`
	Args       []string `json:"args"`
	CWD        string   `json:"cwd"`
	Halsa      string   `json:"halsa"`
	Port       int      `json:"port"`
	Forklaring string   `json:"forklaring"`
}

// foreslaTestserver låter en agent läsa projektets rot och svara med hur
// projektet startas. PM sparar ingenting, användaren granskar förslaget först.
func (s *Server) foreslaTestserver(w http.ResponseWriter, r *http.Request) {
	var body testserverForslagBody
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			svaraFel(w, errors.New("kunde inte läsa vilken agent du vill fråga"), http.StatusBadRequest)
			return
		}
	}
	alias := r.PathValue("alias")
	projekt, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias)
	if err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte", alias), http.StatusNotFound)
		return
	}
	if strings.TrimSpace(projekt.RepoPath) == "" {
		svaraFel(w, fmt.Errorf("projektet %q saknar arbetskatalog, så PM kan inte läsa koden", alias), http.StatusBadRequest)
		return
	}
	underlag, err := lasStartunderlag(projekt.RepoPath)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}

	agent, err := s.aktuelltRegister().Hamta(strings.TrimSpace(body.Agent))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	ctx, avbryt := context.WithTimeout(r.Context(), provTimeout)
	defer avbryt()
	svar, err := agent.Fraga(ctx, byggTestserverprompt(projekt.RepoPath, underlag))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			svaraFel(w, errors.New("agenten hann inte svara"), http.StatusGatewayTimeout)
			return
		}
		svaraFel(w, fmt.Errorf("agenten kunde inte svara: %w", err), http.StatusBadGateway)
		return
	}
	forslag, err := tolkaTestserverforslag(svar, projekt.RepoPath)
	if err != nil {
		svaraFel(w, err, http.StatusBadGateway)
		return
	}
	svaraJSON(w, http.StatusOK, forslag)
}

// lasStartunderlag listar rotens filer och läser början av dem som avgör
// starten. PM läser aldrig utanför repot och aldrig genom en symbolisk länk.
func lasStartunderlag(repo string) (string, error) {
	rot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", fmt.Errorf("PM hittar inte projektets arbetskatalog %q", repo)
	}
	poster, err := os.ReadDir(rot)
	if err != nil {
		return "", fmt.Errorf("PM kunde inte läsa projektets arbetskatalog %q", repo)
	}
	var namn []string
	for _, post := range poster {
		if strings.HasPrefix(post.Name(), ".") {
			continue
		}
		etikett := post.Name()
		if post.IsDir() {
			etikett += "/"
		}
		namn = append(namn, etikett)
	}
	sort.Strings(namn)
	if len(namn) > rotfilerMax {
		namn = namn[:rotfilerMax]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Filer i projektets rot:\n%s\n", strings.Join(namn, "\n"))
	for _, fil := range startfiler {
		text, ok := lasStartfil(rot, fil)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", fil, text)
	}
	return b.String(), nil
}

// lasStartfil läser början av en fil i repots rot. Den vägrar följa en
// symbolisk länk, så en länk i repot inte kan peka ut resten av maskinen.
func lasStartfil(rot, namn string) (string, bool) {
	sokvag := filepath.Join(rot, namn)
	info, err := os.Lstat(sokvag)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	data, err := os.ReadFile(sokvag)
	if err != nil {
		return "", false
	}
	if len(data) > startfilMax {
		return string(data[:startfilMax]) + "\n[...avkortad]", true
	}
	return string(data), true
}

func byggTestserverprompt(repo, underlag string) string {
	return fmt.Sprintf(`Du ska säga hur projektet nedan startas lokalt för test.
Svara endast med ett JSON-objekt. Använd inga kodstaket och ingen förklarande text runt.
Objektet ska ha fälten kommando, args, cwd, halsa, port och forklaring.

kommando är programmet som startar servern, till exempel npm, go eller python3.
args är en lista med ett argument per post.
Tar servern en port: skriv {port} där porten ska stå i args, och sätt port till 0.
Går porten inte att styra: sätt port till den port servern lyssnar på.
cwd är arbetskatalogen, normalt %s.
halsa är sökvägen PM hämtar för att se att servern svarar, till exempel /.
forklaring är en mening på svenska om varför du valde så.

Är projektet bara statiska filer utan byggsteg är python3 -m http.server {port} rätt svar.

Projektets arbetskatalog: %s

%s`, repo, repo, underlag)
}

func tolkaTestserverforslag(svar, repo string) (*testserverForslag, error) {
	var forslag testserverForslag
	if err := json.Unmarshal([]byte(strings.TrimSpace(svar)), &forslag); err != nil {
		return nil, errors.New("agenten svarade inte med ett giltigt förslag")
	}
	forslag.Kommando = strings.TrimSpace(forslag.Kommando)
	if forslag.Kommando == "" {
		return nil, errors.New("agentens förslag saknar ett kommando")
	}
	if forslag.Args == nil {
		forslag.Args = []string{}
	}
	if strings.TrimSpace(forslag.CWD) == "" {
		forslag.CWD = repo
	}
	if strings.TrimSpace(forslag.Halsa) == "" {
		forslag.Halsa = "/"
	}
	// Samma krav som konfigurationen ställer, så förslaget går att spara.
	konfig := pm.TestserverKonfig{
		Kommando: forslag.Kommando, Args: forslag.Args,
		CWD: forslag.CWD, Halsa: forslag.Halsa, Port: forslag.Port,
	}
	if err := (pm.Konfig{Testserver: map[string]pm.TestserverKonfig{"prov": konfig}}).Validera(); err != nil {
		return nil, fmt.Errorf("agentens förslag går inte att använda: %w", err)
	}
	return &forslag, nil
}
