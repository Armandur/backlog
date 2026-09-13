package pmweb

import (
	"net/http"

	"github.com/mazen160/backlog/internal/pm"
)

func (s *Server) hamtaAnvandning(w http.ResponseWriter, r *http.Request) {
	store := pm.NewAnvandningStore(s.db)
	agenter := s.aktuelltRegister().Namn()
	// Konfigurationen säger vilka agenter som alls kan rapportera kvot. Utan
	// den skillnaden ser codex ut som en agent PM bara inte hunnit läsa av.
	konfig, konfigfel := pm.LasKonfig(konfigWorkDir())
	lagen := make([]pm.Anvandning, 0, len(agenter))
	for _, agent := range agenter {
		lage, err := store.Hamta(r.Context(), agent)
		if err != nil {
			svaraFel(w, err, http.StatusInternalServerError)
			return
		}
		if konfigfel == nil {
			lage.Rapporterar = pm.KvotstromFinns(konfig.Agenter[agent].Strom)
		}
		lagen = append(lagen, lage)
	}
	svaraJSON(w, http.StatusOK, map[string]any{"anvandning": lagen})
}
