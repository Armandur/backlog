package pmweb

import (
	"fmt"
	"net/http"
	"os"

	"github.com/mazen160/backlog/internal/pm"
)

type testserverSvar struct {
	Alias        string `json:"alias"`
	PID          int    `json:"pid"`
	Port         int    `json:"port"`
	StartadAt    int64  `json:"startad_at"`
	Logg         string `json:"logg_sokvag"`
	Lever        bool   `json:"lever"`
	Status       string `json:"status"`
	Exitkod      *int   `json:"exitkod,omitempty"`
	Konfigurerad bool   `json:"konfigurerad"`
	Lank         string `json:"lank,omitempty"`
}

func (s *Server) testserverStore() (*pm.TestserverStore, pm.Konfig, error) {
	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err != nil {
		return nil, pm.Konfig{}, err
	}
	return pm.NewTestserverStore(s.db, konfig, konfigWorkDir()), konfig, nil
}

func (s *Server) hamtaTestserver(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), alias); err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	store, konfig, err := s.testserverStore()
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	server, err := store.Status(r.Context(), alias)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, testserverTillSvar(alias, server, konfig))
}

func (s *Server) startaTestserver(w http.ResponseWriter, r *http.Request) {
	store, konfig, err := s.testserverStore()
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	server, err := store.Starta(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusCreated, testserverTillSvar(server.Alias, server, konfig))
}

func (s *Server) stoppaTestserver(w http.ResponseWriter, r *http.Request) {
	store, konfig, err := s.testserverStore()
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	server, err := store.Stoppa(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusOK, testserverTillSvar(server.Alias, server, konfig))
}

func testserverTillSvar(alias string, server *pm.Testserver, konfig pm.Konfig) testserverSvar {
	svar := testserverSvar{Alias: alias}
	_, svar.Konfigurerad = konfig.Testserver[alias]
	if server == nil {
		return svar
	}
	svar.PID = server.PID
	svar.Port = server.Port
	svar.StartadAt = server.StartadAt
	svar.Logg = server.Logg
	svar.Lever = server.Lever
	svar.Status = server.Status
	svar.Exitkod = server.Exitkod
	if server.Lever {
		vard, err := os.Hostname()
		if err == nil && vard != "" {
			svar.Lank = fmt.Sprintf("http://%s:%d/", vard, server.Port)
		}
	}
	return svar
}
