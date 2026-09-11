package pmweb

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/mazen160/backlog/internal/service"
)

func (s *Server) hamtaKommentarer(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("id")
	tasks := service.NewTaskService(s.db, service.NewPlanService(s.db), service.NewLabelService(s.db))
	task, err := tasks.Get(r.Context(), ref, false, false)
	if err != nil {
		svaraFel(w, fmt.Errorf("tasken %q finns inte i PM-workspacet", ref), http.StatusNotFound)
		return
	}

	kommentarer, err := service.NewCommentService(s.db).ListForTask(r.Context(), task.ID)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa taskens kommentarer"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"kommentarer": kommentarer})
}

func (s *Server) listaDocs(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias); err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte i PM-workspacet", alias), http.StatusNotFound)
		return
	}

	docs, err := service.NewDocService(s.db).List(r.Context(), alias)
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa projektets docs"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"docs": docs})
}

func (s *Server) hamtaDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := service.NewDocService(s.db).Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		svaraFel(w, fmt.Errorf("dokumentet %q finns inte i PM-workspacet", id), http.StatusNotFound)
		return
	}
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa dokumentet"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func (s *Server) listaMinne(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias); err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte i PM-workspacet", alias), http.StatusNotFound)
		return
	}

	minne, err := service.NewMemoryService(s.db).List(r.Context(), alias, "")
	if err != nil {
		svaraFel(w, errors.New("PM kunde inte läsa projektminnet"), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"minne": minne})
}
