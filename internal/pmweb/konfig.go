package pmweb

import (
	"context"
	"encoding/json"
	"errors"
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
