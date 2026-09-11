// Package pmweb lägger PM-vyerna ovanpå upstreams webbserver, utan att röra
// internal/web/server.go.
package pmweb

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
	"github.com/mazen160/backlog/internal/web"
)

//go:embed static
var staticFiles embed.FS

// Server hanterar PM-rutterna och skickar resten vidare till upstream.
// Utdelarfunktion startar en körning i bakgrunden och ger ett kvitto att
// visa i UI:t. Servern äger inte utdelningen, web-kommandot kopplar in den.
type Utdelarfunktion func(taskID, agent string) string

type Server struct {
	db       *sql.DB
	aktor    models.Actor
	register *pm.AgentRegister
	utdelare Utdelarfunktion
	mux      *http.ServeMux
}

func New(db *sql.DB, aktor models.Actor, register *pm.AgentRegister) *Server {
	s := &Server{db: db, aktor: aktor, register: register, mux: http.NewServeMux()}
	s.rutter(web.New(db, aktor))
	return s
}

// MedUtdelare kopplar in utdelningen. Utan den svarar dela-ut med 503.
func (s *Server) MedUtdelare(f Utdelarfunktion) *Server {
	s.utdelare = f
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) rutter(upstream http.Handler) {
	s.mux.HandleFunc("POST /api/projekt", s.skapaProjekt)
	s.mux.HandleFunc("GET /api/projects/{alias}/samtal", s.hamtaSamtal)
	s.mux.HandleFunc("POST /api/projects/{alias}/samtal", s.skrivSamtal)
	// Utan metodmönster skulle en DELETE falla vidare till upstream och ge 404.
	s.mux.HandleFunc("/api/projects/{alias}/samtal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, POST")
		svaraFel(w, fmt.Errorf("metoden %s stöds inte på samtalsrouten", r.Method), http.StatusMethodNotAllowed)
	})
	s.mux.HandleFunc("GET /api/projects/{alias}/oversikt", s.hamtaOversikt)
	s.mux.HandleFunc("GET /api/tasks/{id}/kommentarer", s.hamtaKommentarer)
	s.mux.HandleFunc("GET /api/projects/{alias}/docs", s.listaDocs)
	s.mux.HandleFunc("GET /api/docs/{id}", s.hamtaDoc)
	s.mux.HandleFunc("GET /api/projects/{alias}/minne", s.listaMinne)
	endastLasning := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET")
		svaraFel(w, fmt.Errorf("metoden %s stöds inte på läsrouten", r.Method), http.StatusMethodNotAllowed)
	}
	s.mux.HandleFunc("/api/tasks/{id}/kommentarer", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/docs", endastLasning)
	s.mux.HandleFunc("/api/docs/{id}", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/minne", endastLasning)
	s.mux.HandleFunc("POST /api/projects/{alias}/tasks", s.skapaTask)
	s.mux.HandleFunc("POST /api/projects/{alias}/dela-ut", s.delaUt)
	s.mux.HandleFunc("GET /api/projects/{alias}/korningar", s.hamtaKorningar)
	s.mux.HandleFunc("GET /api/korningar/{id}/strom", s.strommaKorning)
	s.mux.HandleFunc("GET /api/korningar/{id}", s.hamtaKorning)
	s.mux.HandleFunc("GET /api/agenter", s.hamtaAgenter)
	s.mux.HandleFunc("GET /api/konfig", s.hamtaKonfig)
	s.mux.HandleFunc("PUT /api/konfig", s.skrivKonfig)
	s.mux.HandleFunc("POST /api/konfig/foresla", s.foreslaAgent)
	s.mux.HandleFunc("POST /api/konfig/prova", s.provaKonfig)
	s.mux.HandleFunc("GET /pm/{alias}", s.tradVy)
	s.mux.HandleFunc("GET /pm/", s.tradVy)

	statiskt, _ := fs.Sub(staticFiles, "static")
	s.mux.Handle("GET /pm-static/", http.StripPrefix("/pm-static/", http.FileServer(http.FS(statiskt))))

	// Allt annat är upstreams webb-UI och API.
	s.mux.Handle("/", upstream)
}

func (s *Server) tradVy(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/pm.html")
	if err != nil {
		http.Error(w, "sidan saknas", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

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
		svar, err := pm.Fraga(r.Context(), s.db, s.aktuelltRegister(), pm.FragaInput{
			Alias: alias, ProjectID: projectID, Fraga: body.Text, Agent: body.Agent, Fragare: aktor,
		})
		if err != nil {
			svaraFel(w, err, http.StatusBadGateway)
			return
		}
		svaraJSON(w, http.StatusCreated, svar)
		return
	}

	post, err := store.Add(r.Context(), projectID, body.TaskID, aktor, body.Text)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusCreated, post)
}

func (s *Server) hamtaOversikt(w http.ResponseWriter, r *http.Request) {
	o, err := byggOversikt(r.Context(), s.db, r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	svaraJSON(w, http.StatusOK, o)
}

type utdelBody struct {
	Task  string `json:"task"`
	Agent string `json:"agent"`
}

// delaUt startar körningen i bakgrunden och svarar direkt. Vyn följer
// statusen genom att polla översikten.
func (s *Server) delaUt(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), alias); err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	var body utdelBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa utdelningen"), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Task) == "" {
		svaraFel(w, errors.New("ange vilken task som ska delas ut"), http.StatusBadRequest)
		return
	}
	if s.utdelare == nil {
		svaraFel(w, errors.New("utdelaren är inte konfigurerad i den här servern"), http.StatusServiceUnavailable)
		return
	}

	tasks := service.NewTaskService(s.db, service.NewPlanService(s.db), service.NewLabelService(s.db))
	taskID, err := tasks.ResolveRef(r.Context(), body.Task)
	if err != nil {
		svaraFel(w, fmt.Errorf("hittade inte tasken %q", body.Task), http.StatusNotFound)
		return
	}
	if body.Agent != "" {
		if _, err := s.aktuelltRegister().Hamta(body.Agent); err != nil {
			svaraFel(w, err, http.StatusBadRequest)
			return
		}
	}

	klart := s.utdelare(taskID, body.Agent)
	svaraJSON(w, http.StatusAccepted, map[string]any{"startad": true, "task": body.Task, "agent": body.Agent, "kvitto": klart})
}

func (s *Server) hamtaKorningar(w http.ResponseWriter, r *http.Request) {
	projectID, err := pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	korningar, err := pm.NewKorningStore(s.db).Lista(r.Context(), projectID, 0)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"korningar": korningar})
}

func (s *Server) hamtaAgenter(w http.ResponseWriter, r *http.Request) {
	reg := s.aktuelltRegister()
	svaraJSON(w, http.StatusOK, map[string]any{"agenter": reg.Namn(), "forval": reg.Forval()})
}

// aktuelltRegister läser konfigurationen per anrop, så en agent som lagts till
// i konfigvyn går att använda utan omstart. Saknas filen, eller går den inte
// att läsa, gäller registret från starten. Annars skulle de inbyggda
// defaulterna ta över tyst.
func (s *Server) aktuelltRegister() *pm.AgentRegister {
	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err == nil && konfig.Kalla != "" {
		return pm.FranKonfig(konfig)
	}
	if s.register != nil {
		return s.register
	}
	if err != nil {
		return pm.NewAgentRegister()
	}
	return pm.FranKonfig(konfig)
}

func (s *Server) hamtaKorning(w http.ResponseWriter, r *http.Request) {
	k, err := pm.NewKorningStore(s.db).Hamta(r.Context(), r.PathValue("id"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	svar := map[string]any{"korning": k}
	if r.URL.Query().Get("logg") == "1" {
		svar["logg"] = pm.LasLogg(k.Logg)
	}
	svaraJSON(w, http.StatusOK, svar)
}

func svaraJSON(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(kod)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func svaraFel(w http.ResponseWriter, err error, kod int) {
	svaraJSON(w, kod, map[string]string{"error": err.Error()})
}
