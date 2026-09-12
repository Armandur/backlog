// Package pmweb lägger PM-vyerna ovanpå upstreams webbserver, utan att röra
// internal/web/server.go.
package pmweb

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
type Utdelarfunktion func(taskID, agent, modell, anstrangning string) string

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
	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "kunde inte läsa testserverkonfigurationen vid städning: %v\n", err)
	} else if _, err := pm.NewTestserverStore(db, konfig, konfigWorkDir()).StadaDoda(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "kunde inte städa döda testservrar: %v\n", err)
	}
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
	s.mux.HandleFunc("POST /api/projekt/{alias}/arkivera", s.arkiveraProjekt)
	s.mux.HandleFunc("POST /api/projekt/{alias}/aterstall", s.aterstallProjekt)
	s.mux.HandleFunc("DELETE /api/projekt/{alias}", s.taBortProjekt)
	s.mux.HandleFunc("POST /api/projekt/{alias}/foresla-testserver", s.foreslaTestserver)
	s.mux.HandleFunc("POST /api/foresla-testserver", s.foreslaTestserverForSokvag)
	s.mux.HandleFunc("GET /api/projects/{alias}/samtal", s.hamtaSamtal)
	s.mux.HandleFunc("POST /api/projects/{alias}/samtal", s.skrivSamtal)
	s.mux.HandleFunc("POST /api/projects/{alias}/samtal/{id}/kvittera", s.kvitteraSamtal)
	s.mux.HandleFunc("POST /api/projects/{alias}/samtal/{id}/minne", s.sparaSamtalsminne)
	// Utan metodmönster skulle en DELETE falla vidare till upstream och ge 404.
	s.mux.HandleFunc("/api/projects/{alias}/samtal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, POST")
		svaraFel(w, fmt.Errorf("metoden %s stöds inte på samtalsrouten", r.Method), http.StatusMethodNotAllowed)
	})
	s.mux.HandleFunc("GET /api/projects/{alias}/oversikt", s.hamtaOversikt)
	s.mux.HandleFunc("GET /api/projects/{alias}/testserver", s.hamtaTestserver)
	s.mux.HandleFunc("GET /api/projects/{alias}/testserver/logg", s.strommaTestserverlogg)
	s.mux.HandleFunc("POST /api/projects/{alias}/testserver/start", s.startaTestserver)
	s.mux.HandleFunc("POST /api/projects/{alias}/testserver/stop", s.stoppaTestserver)
	s.mux.HandleFunc("GET /api/tasks/{id}/kommentarer", s.hamtaKommentarer)
	s.mux.HandleFunc("GET /api/projects/{alias}/docs", s.listaDocs)
	s.mux.HandleFunc("GET /api/docs/{id}", s.hamtaDoc)
	s.mux.HandleFunc("GET /api/projects/{alias}/minne", s.listaMinne)
	s.mux.HandleFunc("GET /api/projects/{alias}/filer", s.hamtaFiler)
	s.mux.HandleFunc("GET /api/projects/{alias}/git-andringar", s.hamtaGitAndringar)
	s.mux.HandleFunc("GET /api/projects/{alias}/git-diff", s.hamtaGitDiff)
	endastLasning := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET")
		svaraFel(w, fmt.Errorf("metoden %s stöds inte på läsrouten", r.Method), http.StatusMethodNotAllowed)
	}
	s.mux.HandleFunc("/api/tasks/{id}/kommentarer", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/docs", endastLasning)
	s.mux.HandleFunc("/api/docs/{id}", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/minne", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/filer", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/git-andringar", endastLasning)
	s.mux.HandleFunc("/api/projects/{alias}/git-diff", endastLasning)
	s.mux.HandleFunc("POST /api/projects/{alias}/foresla-task", s.foreslaNyTask)
	s.mux.HandleFunc("POST /api/projects/{alias}/tasks", s.skapaTask)
	s.mux.HandleFunc("POST /api/tasks/{id}/foresla", s.foreslaTask)
	s.mux.HandleFunc("PATCH /api/tasks/{id}", s.uppdateraTask)
	s.mux.HandleFunc("DELETE /api/tasks/{id}", s.taBortTask)
	s.mux.HandleFunc("GET /api/tasks/{id}/detaljer", s.hamtaTaskdetaljer)
	s.mux.HandleFunc("POST /api/tasks/{id}/etiketter", s.laggEtikett)
	s.mux.HandleFunc("DELETE /api/tasks/{id}/etiketter/{namn}", s.taBortEtikett)
	s.mux.HandleFunc("POST /api/tasks/{id}/plan", s.sparaPlan)
	s.mux.HandleFunc("POST /api/projects/{alias}/dela-ut", s.delaUt)
	s.mux.HandleFunc("GET /api/projects/{alias}/korningar", s.hamtaKorningar)
	s.mux.HandleFunc("GET /api/korningar/{id}/strom", s.strommaKorning)
	s.mux.HandleFunc("GET /api/korningar/{id}/handelser", s.hamtaKorningshandelser)
	s.mux.HandleFunc("GET /api/korningar/{id}", s.hamtaKorning)
	s.mux.HandleFunc("GET /api/agenter", s.hamtaAgenter)
	s.mux.HandleFunc("GET /api/anvandning", s.hamtaAnvandning)
	s.mux.HandleFunc("GET /api/konfig", s.hamtaKonfig)
	s.mux.HandleFunc("PUT /api/konfig", s.skrivKonfig)
	s.mux.HandleFunc("POST /api/konfig/foresla", s.foreslaAgent)
	s.mux.HandleFunc("GET /api/forslag/{id}", s.hamtaForslag)
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

func (s *Server) hamtaOversikt(w http.ResponseWriter, r *http.Request) {
	o, err := byggOversikt(r.Context(), s.db, r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	svaraJSON(w, http.StatusOK, o)
}

type utdelBody struct {
	Task         string `json:"task"`
	Agent        string `json:"agent"`
	Modell       string `json:"modell"`
	Anstrangning string `json:"anstrangning"`
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

	klart := s.utdelare(taskID, body.Agent, strings.TrimSpace(body.Modell), strings.TrimSpace(body.Anstrangning))
	svaraJSON(w, http.StatusAccepted, map[string]any{
		"startad": true, "task": body.Task, "agent": body.Agent,
		"modell": strings.TrimSpace(body.Modell), "kvitto": klart,
	})
}

var synligaKorningssorter = []string{pm.KorningSortTask, pm.KorningSortFraga}

func (s *Server) hamtaKorningar(w http.ResponseWriter, r *http.Request) {
	projectID, err := pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), r.PathValue("alias"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			svaraFel(w, errors.New("limit måste vara ett heltal mellan 1 och 100"), http.StatusBadRequest)
			return
		}
	}
	var innan int64
	if v := r.URL.Query().Get("innan"); v != "" {
		innan, err = strconv.ParseInt(v, 10, 64)
		if err != nil || innan < 1 {
			svaraFel(w, errors.New("innan måste vara en giltig tidscursor"), http.StatusBadRequest)
			return
		}
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "alla" && status != "pagaende" && status != pm.StatusKlar && status != pm.StatusFel {
		svaraFel(w, errors.New("status måste vara alla, pågående, klar eller fel"), http.StatusBadRequest)
		return
	}
	filterstatus := status
	if filterstatus == "" || filterstatus == "alla" {
		filterstatus = "avslutade"
	}
	queryLimit := limit + 1
	if status == "pagaende" {
		queryLimit = 0
	}
	store := pm.NewKorningStore(s.db)
	korningar, err := store.ListaFiltrerad(r.Context(), pm.KorningFilter{
		ProjectID: projectID, Status: filterstatus, Sorter: synligaKorningssorter, Innan: innan, Limit: queryLimit,
	})
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	fler := queryLimit > 0 && len(korningar) > limit
	if fler {
		korningar = korningar[:limit]
	}
	if status == "" && innan == 0 {
		pagaende, listfel := store.ListaFiltrerad(r.Context(), pm.KorningFilter{
			ProjectID: projectID, Status: "pagaende", Sorter: synligaKorningssorter,
		})
		if listfel != nil {
			svaraFel(w, listfel, http.StatusInternalServerError)
			return
		}
		historikplatser := max(0, limit-len(pagaende))
		if len(korningar) > historikplatser {
			fler = true
			korningar = korningar[:historikplatser]
		}
		korningar = append(pagaende, korningar...)
	}
	nastaInnan := ""
	if fler && len(korningar) > 0 {
		nastaInnan = strconv.FormatInt(korningar[len(korningar)-1].SkapadAt, 10)
	}
	svaraJSON(w, http.StatusOK, map[string]any{
		"korningar": korningar, "fler": fler, "nasta_innan": nastaInnan,
	})
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

type sparatForslag struct {
	Resultat json.RawMessage `json:"resultat,omitempty"`
	Fel      string          `json:"fel,omitempty"`
}

func forslagResultatSokvag(id string) string {
	return filepath.Join(pm.LoggKatalog(konfigWorkDir()), id+".forslag.json")
}

func (s *Server) hamtaKlassningsvarden(ctx context.Context) (klassningsvarden, error) {
	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err != nil {
		return klassningsvarden{}, err
	}
	modeller := map[string]struct{}{}
	anstrangningar := map[string]struct{}{}
	for _, agent := range konfig.Agenter {
		laggTillKlassningsvarde(modeller, agent.Modell)
		laggTillKlassningsvarde(anstrangningar, agent.Anstrangning)
	}
	// Bara de senaste körningarna behövs. Utan gräns läses hela tabellen vid
	// varje klassning, och den växer.
	korningar, err := pm.NewKorningStore(s.db).Lista(ctx, "", 200)
	if err != nil {
		return klassningsvarden{}, err
	}
	for _, korning := range korningar {
		laggTillKlassningsvarde(modeller, korning.Modell)
		if len(anstrangningar) > 0 {
			laggTillKlassningsvarde(anstrangningar, korning.Anstrangning)
		}
	}
	return klassningsvarden{
		Modeller:       nycklar(modeller),
		Anstrangningar: nycklar(anstrangningar),
	}, nil
}

func laggTillKlassningsvarde(varden map[string]struct{}, varde string) {
	if varde = strings.TrimSpace(varde); varde != "" {
		varden[varde] = struct{}{}
	}
}

func nycklar(varden map[string]struct{}) []string {
	resultat := make([]string, 0, len(varden))
	for varde := range varden {
		resultat = append(resultat, varde)
	}
	sort.Strings(resultat)
	return resultat
}

func valideraKlassningsvarde(namn, varde string, tillatna []string) error {
	if varde == "" {
		return nil
	}
	for _, tillatet := range tillatna {
		if varde == tillatet {
			return nil
		}
	}
	return fmt.Errorf("agentens förslag innehåller ett okänt %s %q", namn, varde)
}
