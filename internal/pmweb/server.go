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
	"github.com/mazen160/backlog/internal/web"
)

//go:embed static
var staticFiles embed.FS

// Server hanterar PM-rutterna och skickar resten vidare till upstream.
type Server struct {
	db       *sql.DB
	aktor    models.Actor
	register *pm.AgentRegister
	mux      *http.ServeMux
}

func New(db *sql.DB, aktor models.Actor, register *pm.AgentRegister) *Server {
	s := &Server{db: db, aktor: aktor, register: register, mux: http.NewServeMux()}
	s.rutter(web.New(db, aktor))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) rutter(upstream http.Handler) {
	s.mux.HandleFunc("GET /api/projects/{alias}/samtal", s.hamtaSamtal)
	s.mux.HandleFunc("POST /api/projects/{alias}/samtal", s.skrivSamtal)
	// Utan metodmönster skulle en DELETE falla vidare till upstream och ge 404.
	s.mux.HandleFunc("/api/projects/{alias}/samtal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, POST")
		svaraFel(w, fmt.Errorf("metoden %s stöds inte på samtalsrouten", r.Method), http.StatusMethodNotAllowed)
	})
	s.mux.HandleFunc("GET /api/projects/{alias}/korningar", s.hamtaKorningar)
	s.mux.HandleFunc("GET /api/korningar/{id}", s.hamtaKorning)
	s.mux.HandleFunc("GET /pm/{alias}", s.tradVy)
	s.mux.HandleFunc("GET /pm/", s.tradVy)

	statiskt, _ := fs.Sub(staticFiles, "static")
	s.mux.Handle("GET /pm-static/", http.StripPrefix("/pm-static/", http.FileServer(http.FS(statiskt))))

	// Allt annat är upstreams webb-UI och API.
	s.mux.Handle("/", upstream)
}

func (s *Server) tradVy(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/samtal.html")
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
		svar, err := pm.Fraga(r.Context(), s.db, s.register, pm.FragaInput{
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
