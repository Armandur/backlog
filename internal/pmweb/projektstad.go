package pmweb

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

// arkiveraProjekt lägger undan ett projekt utan att röra dess data. Ett
// arkiverat projekt går att återställa eller ta bort helt.
func (s *Server) arkiveraProjekt(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.PathValue("alias"))
	if !s.projektFinns(w, r, alias) {
		return
	}
	projekt, err := service.NewProjectService(s.db).Archive(r.Context(), alias, s.aktor)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"projekt": projekt})
}

// aterstallProjekt tar tillbaka ett arkiverat projekt.
func (s *Server) aterstallProjekt(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.PathValue("alias"))
	if !s.projektFinns(w, r, alias) {
		return
	}
	projekt, err := service.NewProjectService(s.db).Unarchive(r.Context(), alias, s.aktor)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"projekt": projekt})
}

// taBortProjekt raderar ett arkiverat projekt med allt PM sparat om det.
// Koden på disken rörs aldrig.
func (s *Server) taBortProjekt(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.PathValue("alias"))
	projekt, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias)
	if err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte", alias), http.StatusNotFound)
		return
	}
	if projekt.ArchivedAt == nil {
		svaraFel(w, fmt.Errorf("arkivera projektet %q innan du tar bort det", alias), http.StatusBadRequest)
		return
	}
	// Testservern stoppas först. Annars lever processen vidare utan ägare.
	if err := stoppaTestserverForProjekt(r.Context(), s, alias); err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	if err := service.NewProjectService(s.db).Delete(r.Context(), alias, s.aktor); err != nil {
		svaraFel(w, fmt.Errorf("kunde inte ta bort projektet %q: %w", alias, err), http.StatusInternalServerError)
		return
	}
	varning := taBortTestserverblock(alias)
	svar := map[string]any{"alias": alias}
	if varning != "" {
		svar["varning"] = varning
	}
	svaraJSON(w, http.StatusOK, svar)
}

// stoppaTestserverForProjekt stoppar projektets testserver och släpper porten.
// En server som inte kör är inget fel.
func stoppaTestserverForProjekt(ctx context.Context, s *Server, alias string) error {
	store, _, err := s.testserverStore()
	if err != nil {
		return err
	}
	if _, err := store.Stoppa(ctx, alias); err != nil && !strings.Contains(err.Error(), "kör inte") {
		return fmt.Errorf("kunde inte stoppa testservern för %q: %w", alias, err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pm_portar WHERE projekt=?`, alias); err != nil {
		return fmt.Errorf("kunde inte släppa porten för %q: %w", alias, err)
	}
	return nil
}

// taBortTestserverblock städar pm.toml. Ett fel här får inte stoppa
// borttagningen, projektet är redan borta. Användaren får ett besked i stället.
func taBortTestserverblock(alias string) string {
	workspace := konfigWorkDir()
	konfig, err := pm.LasKonfig(workspace)
	if err != nil {
		return "Projektet är borta, men konfigurationen går inte att läsa. Ta bort testserverblocket i pm.toml."
	}
	if _, finns := konfig.Testserver[alias]; !finns {
		return ""
	}
	delete(konfig.Testserver, alias)
	// Andra övergivna block ska stå kvar, så skrivningen får inte kräva att
	// varje block pekar på ett projekt som finns.
	if err := pm.SkrivKonfigMedOvergivna(workspace, konfig); err != nil {
		return "Projektet är borta, men testserverblocket finns kvar. Ta bort det under Konfig."
	}
	return ""
}

// projektFinns svarar 404 när aliaset inte finns, så felet blir begripligt.
func (s *Server) projektFinns(w http.ResponseWriter, r *http.Request, alias string) bool {
	if _, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias); err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte", alias), http.StatusNotFound)
		return false
	}
	return true
}
