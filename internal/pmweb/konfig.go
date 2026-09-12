package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	// Hemligheterna lämnar aldrig servern. Konfigvyn ser bara vilka
	// miljövariabler som är satta.
	svaraJSON(w, http.StatusOK, konfigSvar{
		Konfig: konfig.Maskera(),
		Sokvag: filepath.Join(workspace, pm.KonfigFil),
		Saknas: konfig.Kalla == "",
	})
}

type skrivKonfigBody struct {
	pm.Konfig
	RaderadeTestservrar []string `json:"raderade_testservrar"`
}

func (s *Server) skrivKonfig(w http.ResponseWriter, r *http.Request) {
	var body skrivKonfigBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa konfigurationen"), http.StatusBadRequest)
		return
	}
	konfig := body.Konfig
	konfig.Kalla = ""
	workspace := konfigWorkDir()
	sparad, err := pm.LasKonfig(workspace)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	projektalias, err := s.projektalias(r.Context())
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	raderade := make(map[string]bool, len(body.RaderadeTestservrar))
	for _, alias := range body.RaderadeTestservrar {
		raderade[alias] = true
		delete(konfig.Testserver, alias)
	}
	for alias := range konfig.Testserver {
		_, fanns := sparad.Testserver[alias]
		if !projektalias[alias] && !fanns {
			svaraFel(w, fmt.Errorf("testservern pekar på projektet %q som inte finns", alias), http.StatusBadRequest)
			return
		}
	}
	if konfig.Testserver == nil {
		konfig.Testserver = map[string]pm.TestserverKonfig{}
	}
	for alias, server := range sparad.Testserver {
		if projektalias[alias] || raderade[alias] {
			continue
		}
		if _, finns := konfig.Testserver[alias]; !finns {
			konfig.Testserver[alias] = server
		}
	}
	// Ett maskerat värde betyder att användaren lämnade hemligheten orörd.
	konfig = konfig.AterstallMaskerat(sparad)
	if err := konfig.Validera(); err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	harOvergivna := false
	for alias := range konfig.Testserver {
		if !projektalias[alias] {
			harOvergivna = true
			break
		}
	}
	if harOvergivna {
		err = pm.SkrivKonfigMedOvergivna(workspace, konfig)
	} else {
		err = pm.SkrivKonfig(workspace, konfig)
	}
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	sokvag := filepath.Join(workspace, pm.KonfigFil)
	konfig.Kalla = sokvag
	svaraJSON(w, http.StatusOK, konfigSvar{Konfig: konfig.Maskera(), Sokvag: sokvag})
}

func (s *Server) projektalias(ctx context.Context) (map[string]bool, error) {
	rader, err := s.db.QueryContext(ctx, `SELECT alias FROM projects WHERE archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("kunde inte läsa projekt för konfigurationen: %w", err)
	}
	defer rader.Close()
	alias := map[string]bool{}
	for rader.Next() {
		var namn string
		if err := rader.Scan(&namn); err != nil {
			return nil, fmt.Errorf("kunde inte läsa projektalias: %w", err)
		}
		alias[namn] = true
	}
	if err := rader.Err(); err != nil {
		return nil, fmt.Errorf("kunde inte läsa projektalias: %w", err)
	}
	return alias, nil
}

type provaKonfigBody struct {
	Agent string `json:"agent"`
}

type foreslaAgentBody struct {
	Alias       string `json:"alias"`
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
	var projectID string
	var err error
	if strings.TrimSpace(body.Alias) == "" {
		err = s.db.QueryRowContext(r.Context(), `SELECT id FROM projects WHERE archived_at IS NULL ORDER BY created_at LIMIT 1`).Scan(&projectID)
	} else {
		projectID, err = pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), strings.TrimSpace(body.Alias))
	}
	if err != nil {
		svaraFel(w, errors.New("välj ett projekt innan du hämtar förslaget"), http.StatusBadRequest)
		return
	}
	korning, err := s.startaForslag(r.Context(), projectID, body.Agent, "förslag till agentkonfiguration",
		byggForslagsprompt(body.Beskrivning), func(_ context.Context, svar string, _ pm.Agent) (any, error) {
			var forslag agentForslag
			if err := json.Unmarshal([]byte(svar), &forslag); err != nil {
				return nil, errors.New("agenten svarade inte med ett giltigt agentblock")
			}
			forslag.Namn = strings.TrimSpace(forslag.Namn)
			if forslag.Namn == "" {
				return nil, errors.New("agentens förslag saknar ett namn")
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
				return nil, fmt.Errorf("agentens förslag går inte att använda: %w", err)
			}
			return forslag, nil
		})
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusAccepted, map[string]any{"korning": korning})
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
	agent, finns := konfig.Agenter[body.Agent]
	if !finns {
		svaraFel(w, fmt.Errorf("agenten %q finns inte", body.Agent), http.StatusBadRequest)
		return
	}
	svar, err := korKonfigprov(r.Context(), body.Agent, agent)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, svar)
}

type forslagBearbetare func(context.Context, string, pm.Agent) (any, error)

// startaForslag skapar en kort körning utan task och svarar innan agenten är klar.
