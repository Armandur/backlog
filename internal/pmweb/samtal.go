package pmweb

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

func (s *Server) hamtaSamtal(w http.ResponseWriter, r *http.Request) {
	store := pm.NewSamtalStore(s.db)
	projectID, err := store.ProjectIDByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 0 {
			svaraFel(w, errors.New("limit måste vara ett positivt heltal"), http.StatusBadRequest)
			return
		}
	}
	poster, err := store.List(r.Context(), projectID, limit)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"samtal": poster})
}

type inlaggBody struct {
	Text   string `json:"text"`
	Actor  string `json:"actor"`
	TaskID string `json:"task_id"`
	Fraga  bool   `json:"fraga"`
	Agent  string `json:"agent"`
}

func (s *Server) skrivSamtal(w http.ResponseWriter, r *http.Request) {
	store := pm.NewSamtalStore(s.db)
	alias := r.PathValue("alias")
	projectID, err := store.ProjectIDByAlias(r.Context(), alias)
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	var body inlaggBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa inlägget"), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		svaraFel(w, errors.New("inlägget saknar text"), http.StatusBadRequest)
		return
	}

	aktor := s.aktor
	if body.Actor != "" {
		aktor, err = pm.ParseActor(body.Actor)
		if err != nil {
			svaraFel(w, err, http.StatusBadRequest)
			return
		}
	}

	if body.Fraga {
		startad, err := pm.StartaFraga(r.Context(), s.db, s.aktuelltRegister(), pm.FragaInput{
			Alias: alias, ProjectID: projectID, Fraga: body.Text, Agent: body.Agent, Fragare: aktor,
			WorkspaceDir: konfigWorkDir(), Profil: os.Getenv("BACKLOG_PROFILE"), PMBinar: pm.PMBinar(),
		})
		if err != nil {
			svaraFel(w, err, http.StatusBadGateway)
			return
		}
		svaraJSON(w, http.StatusAccepted, startad)
		return
	}

	post, err := store.Add(r.Context(), projectID, body.TaskID, aktor, body.Text)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusCreated, post)
}

func (s *Server) kvitteraSamtal(w http.ResponseWriter, r *http.Request) {
	store := pm.NewSamtalStore(s.db)
	projectID, err := store.ProjectIDByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	if err := store.Kvittera(r.Context(), projectID, r.PathValue("id")); err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]bool{"kvitterad": true})
}

type minnesforslagBody struct {
	Text string `json:"text"`
}

func (s *Server) sparaSamtalsminne(w http.ResponseWriter, r *http.Request) {
	store := pm.NewSamtalStore(s.db)
	projectID, err := store.ProjectIDByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	var body minnesforslagBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa minnesförslaget"), http.StatusBadRequest)
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		svaraFel(w, errors.New("minnesförslaget saknar text"), http.StatusBadRequest)
		return
	}
	post, err := store.Minnesforslag(r.Context(), projectID, r.PathValue("id"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	minne, err := service.NewMemoryService(s.db).Add(r.Context(), models.CreateMemoryInput{
		ProjectID: projectID, Body: body.Text, Actor: post.Actor,
	})
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte spara minnesposten"), http.StatusInternalServerError)
		return
	}
	if err := store.MarkeraMinnesforslagSparat(r.Context(), projectID, post.ID); err != nil {
		svaraFel(w, errors.New("PM sparade minnesposten men kunde inte stänga förslaget"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusCreated, minne)
}

type skapaTaskBody struct {
	Titel       string          `json:"titel"`
	Beskrivning string          `json:"beskrivning"`
	Typ         models.TaskType `json:"typ"`
	Prioritet   int             `json:"prioritet"`
}
