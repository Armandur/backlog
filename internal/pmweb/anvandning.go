package pmweb

import (
	"net/http"

	"github.com/mazen160/backlog/internal/pm"
)

func (s *Server) hamtaAnvandning(w http.ResponseWriter, r *http.Request) {
	store := pm.NewAnvandningStore(s.db)
	agenter := s.aktuelltRegister().Namn()
	lagen := make([]pm.Anvandning, 0, len(agenter))
	for _, agent := range agenter {
		lage, err := store.Hamta(r.Context(), agent)
		if err != nil {
			svaraFel(w, err, http.StatusInternalServerError)
			return
		}
		lagen = append(lagen, lage)
	}
	svaraJSON(w, http.StatusOK, map[string]any{"anvandning": lagen})
}
