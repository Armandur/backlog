package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/pm"
)

var konfigWorkDir = cli.WorkDir

// provTimeout håller provknappen kort, oavsett agentens egen timeout.
const provTimeout = 120 * time.Second

type konfigSvar struct {
	pm.Konfig
	Sokvag string `json:"sokvag"`
	Saknas bool   `json:"saknas"`
}

func (s *Server) hamtaKonfig(w http.ResponseWriter, _ *http.Request) {
	workspace := konfigWorkDir()
	konfig, err := pm.LasKonfig(workspace)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, konfigSvar{
		Konfig: konfig,
		Sokvag: filepath.Join(workspace, pm.KonfigFil),
		Saknas: konfig.Kalla == "",
	})
}

func (s *Server) skrivKonfig(w http.ResponseWriter, r *http.Request) {
	var konfig pm.Konfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&konfig); err != nil {
		svaraFel(w, errors.New("kunde inte läsa konfigurationen"), http.StatusBadRequest)
		return
	}
	konfig.Kalla = ""
	if err := konfig.Validera(); err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	workspace := konfigWorkDir()
	if err := pm.SkrivKonfig(workspace, konfig); err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	konfig.Kalla = filepath.Join(workspace, pm.KonfigFil)
	svaraJSON(w, http.StatusOK, konfigSvar{Konfig: konfig, Sokvag: konfig.Kalla})
}

type provaKonfigBody struct {
	Agent string `json:"agent"`
}

type foreslaAgentBody struct {
	Beskrivning string `json:"beskrivning"`
	Agent       string `json:"agent"`
}

type agentForslag struct {
	Namn string `json:"namn"`
	pm.AgentKonfig
}

func (s *Server) foreslaAgent(w http.ResponseWriter, r *http.Request) {
	var body foreslaAgentBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa beskrivningen av verktyget"), http.StatusBadRequest)
		return
	}
	body.Beskrivning = strings.TrimSpace(body.Beskrivning)
	if body.Beskrivning == "" {
		svaraFel(w, errors.New("beskriv verktyget som agenten ska konfigurera"), http.StatusBadRequest)
		return
	}
	if s.register == nil {
		svaraFel(w, errors.New("ingen agent finns för att skapa ett förslag"), http.StatusServiceUnavailable)
		return
	}
	agent, err := s.register.Hamta(strings.TrimSpace(body.Agent))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	ctx, avbryt := context.WithTimeout(r.Context(), provTimeout)
	defer avbryt()
	svar, err := agent.Fraga(ctx, byggForslagsprompt(body.Beskrivning))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			svaraFel(w, errors.New("agenten hann inte skapa ett förslag"), http.StatusGatewayTimeout)
			return
		}
		svaraFel(w, fmt.Errorf("agenten kunde inte skapa förslaget: %w", err), http.StatusBadGateway)
		return
	}
	var forslag agentForslag
	if err := json.Unmarshal([]byte(strings.TrimSpace(svar)), &forslag); err != nil {
		svaraFel(w, errors.New("agenten svarade inte med ett giltigt agentblock"), http.StatusBadGateway)
		return
	}
	forslag.Namn = strings.TrimSpace(forslag.Namn)
	if forslag.Namn == "" {
		svaraFel(w, errors.New("agentens förslag saknar ett namn"), http.StatusBadGateway)
		return
	}
	if forslag.Args == nil {
		forslag.Args = []string{}
	}
	if forslag.Miljo == nil {
		forslag.Miljo = map[string]string{}
	}
	konfig := pm.Konfig{
		DefaultAgent: forslag.Namn,
		Agenter:      map[string]pm.AgentKonfig{forslag.Namn: forslag.AgentKonfig},
	}
	if err := konfig.Validera(); err != nil {
		svaraFel(w, fmt.Errorf("agentens förslag går inte att använda: %w", err), http.StatusBadGateway)
		return
	}
	svaraJSON(w, http.StatusOK, forslag)
}

func byggForslagsprompt(beskrivning string) string {
	return fmt.Sprintf(`Du hjälper användaren att konfigurera ett agentverktyg i backlog-pm.
Svara endast med ett JSON-objekt. Använd inga kodstaket eller förklaringar.
Objektet ska ha fälten namn, kommando, args, brief, svar, stdin, timeout_sekunder, miljo och mcp.
args ska vara en lista med ett kommandoargument per post.
brief ska vara arg eller stdin. Lägg {brief} i args när brief är arg.
svar ska vara stdout eller fil. Lägg {svarsfil} i args när svar är fil.
miljo ska vara ett objekt med miljövariabler. mcp ska vara true eller false.

Verktygets beskrivning:
%s
`, strings.TrimSpace(beskrivning))
}

func (s *Server) provaKonfig(w http.ResponseWriter, r *http.Request) {
	var body provaKonfigBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa vilken agent du vill prova"), http.StatusBadRequest)
		return
	}
	body.Agent = strings.TrimSpace(body.Agent)
	if body.Agent == "" {
		svaraFel(w, errors.New("ange vilken agent som ska provas"), http.StatusBadRequest)
		return
	}

	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	korare, err := pm.FranKonfig(konfig).HamtaKorare(body.Agent)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	// Provet körs i en tom temporärkatalog, inte i serverns arbetskatalog -
	// en agent med skrivrättigheter ska inte kunna röra ett riktigt repo.
	repo, err := os.MkdirTemp("", "backlog-pm-prov-*")
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(repo)

	ctx, avbryt := context.WithTimeout(r.Context(), provTimeout)
	defer avbryt()
	resultat, korfel := korare.Kor(ctx, pm.KorInput{
		Brief:    "Svara kort med texten: Konfigurationen fungerar.",
		Repo:     repo,
		Profil:   profilNamn(),
		PMBinar:  pm.PMBinar(),
		Svarsfil: filepath.Join(repo, "svar.txt"),
	})
	svar := map[string]any{
		"agent":    body.Agent,
		"exitkod":  resultat.ExitKod,
		"svar":     strings.TrimSpace(resultat.Utdata),
		"lyckades": korfel == nil && resultat.ExitKod == 0,
	}
	if korfel != nil {
		svar["error"] = korfel.Error()
		svaraJSON(w, http.StatusBadGateway, svar)
		return
	}
	svaraJSON(w, http.StatusOK, svar)
}
