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

	"github.com/mazen160/backlog/internal/pm"
)

// Ett förslag är en kort agentkörning utan task. Den lever i pm_korningar med
// sorten forslag, så den befintliga strömmen kan visa arbetet medan det pågår.

func (s *Server) startaForslag(ctx context.Context, projectID, agentnamn, motivering, prompt string,
	bearbeta forslagBearbetare,
) (*pm.Korning, error) {
	agent, err := s.aktuelltRegister().Hamta(strings.TrimSpace(agentnamn))
	if err != nil {
		return nil, err
	}
	korare, ok := agent.(pm.Korare)
	if !ok {
		return nil, fmt.Errorf("agenten %q kan inte köra förslag", agent.Namn())
	}
	var repoPath string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(repo_path,'') FROM projects WHERE id=?`, projectID).Scan(&repoPath); err != nil {
		return nil, fmt.Errorf("läs projektets repo: %w", err)
	}
	loggkatalog := pm.LoggKatalog(konfigWorkDir())
	if err := os.MkdirAll(loggkatalog, 0o755); err != nil {
		return nil, fmt.Errorf("skapa körningens loggkatalog: %w", err)
	}
	korning := &pm.Korning{
		Sort: pm.KorningSortForslag, ProjectID: projectID, Agent: agent.Namn(),
		Motivering: motivering, Status: pm.StatusKoad, RepoPath: repoPath,
	}
	store := pm.NewKorningStore(s.db)
	if err := store.Skapa(ctx, korning); err != nil {
		return nil, err
	}
	korning.Logg = filepath.Join(loggkatalog, korning.ID+".log")
	if err := store.SattLogg(ctx, korning.ID, korning.Logg); err != nil {
		return nil, err
	}
	if err := store.SattStatus(ctx, korning.ID, pm.StatusKor); err != nil {
		return nil, err
	}
	korning.Status = pm.StatusKor
	go s.korForslag(context.WithoutCancel(ctx), korare, agent, korning, prompt, bearbeta)
	return korning, nil
}

func (s *Server) korForslag(ctx context.Context, korare pm.Korare, agent pm.Agent, korning *pm.Korning,
	prompt string, bearbeta forslagBearbetare,
) {
	ctx, avbryt := context.WithTimeout(ctx, taskForslagTimeout)
	defer avbryt()
	handelsefil, err := os.OpenFile(pm.HandelseSokvag(korning.Logg), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.avslutaForslag(ctx, korning, fmt.Errorf("kunde inte skapa händelseloggen: %w", err), nil)
		return
	}
	skriv := func(h pm.Handelse) { _ = json.NewEncoder(handelsefil).Encode(h) }
	starttext := "Agenten skapar förslaget."
	if kommandoagent, ok := agent.(*pm.KommandoAgent); ok && kommandoagent.Konfig().Strom == "" {
		starttext = "Agenten arbetar utan löpande utdata."
	}
	skriv(pm.Handelse{Tid: time.Now().UnixNano(), Sort: "text", Text: starttext})
	res, korfel := korare.Kor(ctx, pm.KorInput{
		Brief: prompt, Repo: korning.RepoPath, Logg: korning.Logg,
		Profil: os.Getenv("BACKLOG_PROFILE"), PMBinar: pm.PMBinar(),
		Svarsfil:    filepath.Join(pm.LoggKatalog(konfigWorkDir()), korning.ID+".svar.txt"),
		VidHandelse: skriv,
	})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		korfel = errors.New("agenten hann inte skapa ett förslag")
	} else if korfel != nil {
		korfel = fmt.Errorf("agenten kunde inte skapa förslaget: %w", korfel)
	} else if res.ExitKod != 0 {
		korfel = fmt.Errorf("agenten %s avslutade med exitkod %d", agent.Namn(), res.ExitKod)
	}
	var resultat any
	if korfel == nil {
		resultat, korfel = bearbeta(ctx, strings.TrimSpace(res.Utdata), agent)
	}
	avslutskontext := context.WithoutCancel(ctx)
	_ = pm.NewKorningStore(s.db).SattTokens(avslutskontext, korning.ID, res.Tokens)
	_ = handelsefil.Close()
	s.avslutaForslag(avslutskontext, korning, korfel, resultat)
}

func (s *Server) avslutaForslag(ctx context.Context, korning *pm.Korning, korfel error, resultat any) {
	status, exitkod := pm.StatusKlar, 0
	sparat := sparatForslag{}
	if korfel != nil {
		status, exitkod, sparat.Fel = pm.StatusFel, 1, korfel.Error()
		fil, err := os.OpenFile(pm.HandelseSokvag(korning.Logg), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_ = json.NewEncoder(fil).Encode(pm.Handelse{Tid: time.Now().UnixNano(), Sort: "fel", Text: sparat.Fel})
			_ = fil.Close()
		}
	} else {
		sparat.Resultat, korfel = json.Marshal(resultat)
		if korfel != nil {
			status, exitkod, sparat.Fel = pm.StatusFel, 1, "PM kunde inte spara förslaget."
		}
	}
	data, err := json.Marshal(sparat)
	if err == nil {
		err = os.WriteFile(forslagResultatSokvag(korning.ID), data, 0o600)
	}
	if err != nil {
		status, exitkod = pm.StatusFel, 1
	}
	_ = pm.NewKorningStore(s.db).Avsluta(ctx, korning.ID, status, exitkod, korning.Logg)
}

func (s *Server) hamtaForslag(w http.ResponseWriter, r *http.Request) {
	korning, err := pm.NewKorningStore(s.db).Hamta(r.Context(), r.PathValue("id"))
	if err != nil || korning.Sort != pm.KorningSortForslag {
		svaraFel(w, errors.New("förslaget finns inte"), http.StatusNotFound)
		return
	}
	if korning.Status == pm.StatusKoad || korning.Status == pm.StatusKor {
		svaraJSON(w, http.StatusAccepted, map[string]bool{"klar": false})
		return
	}
	data, err := os.ReadFile(forslagResultatSokvag(korning.ID))
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa förslaget"), http.StatusInternalServerError)
		return
	}
	var sparat sparatForslag
	if json.Unmarshal(data, &sparat) != nil {
		svaraFel(w, errors.New("PM kunde inte läsa förslaget"), http.StatusInternalServerError)
		return
	}
	if sparat.Fel != "" {
		kod := http.StatusBadGateway
		if strings.Contains(sparat.Fel, "hann inte skapa ett förslag") {
			kod = http.StatusGatewayTimeout
		}
		svaraFel(w, errors.New(sparat.Fel), kod)
		return
	}
	svaraJSON(w, http.StatusOK, sparat.Resultat)
}
