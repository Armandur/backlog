package pmweb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/timeutil"
)

type fakeAgent struct{ svar string }

func (f fakeAgent) Namn() string { return "fake-modell" }
func (f fakeAgent) Fraga(ctx context.Context, prompt string) (string, error) {
	return f.svar, nil
}

func testServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	db, err := repo.Open(filepath.Join(t.TempDir(), "backlog.db"))
	if err != nil {
		t.Fatalf("öppna db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.Run(db); err != nil {
		t.Fatalf("migrering: %v", err)
	}
	if err := pm.Migrate(db); err != nil {
		t.Fatalf("pm-migrering: %v", err)
	}
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO projects(id, alias, name, created_at, updated_at) VALUES(?,?,?,?,?)`,
		ids.New(), "demo", "Demo", nu, nu); err != nil {
		t.Fatalf("skapa projekt: %v", err)
	}
	reg := pm.NewAgentRegister()
	reg.Registrera(fakeAgent{svar: "TASK-1 är öppen."})
	return New(db, models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, reg), db
}

// Routetestet går genom muxen på den literala sökvägen, inte förbi den.
func TestPostOchGetPaSamtalsrouten(t *testing.T) {
	srv, _ := testServer(t)

	kropp := bytes.NewBufferString(`{"text":"hej","actor":"human:rasmus"}`)
	post := httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal", kropp)
	post.Header.Set("Content-Type", "application/json")
	pw := httptest.NewRecorder()
	srv.ServeHTTP(pw, post)
	if pw.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", pw.Code, pw.Body.String())
	}

	gw := httptest.NewRecorder()
	srv.ServeHTTP(gw, httptest.NewRequest(http.MethodGet, "/api/projects/demo/samtal", nil))
	if gw.Code != http.StatusOK {
		t.Fatalf("GET gav %d: %s", gw.Code, gw.Body.String())
	}
	var svar struct {
		Samtal []pm.Inlagg `json:"samtal"`
	}
	if err := json.NewDecoder(gw.Body).Decode(&svar); err != nil {
		t.Fatalf("kunde inte läsa svaret: %v", err)
	}
	if len(svar.Samtal) != 1 || svar.Samtal[0].Text != "hej" {
		t.Fatalf("väntade inlägget i listan, fick %+v", svar.Samtal)
	}
	if svar.Samtal[0].Actor.Name != "rasmus" || svar.Samtal[0].CreatedAt == 0 {
		t.Fatalf("aktör eller tid saknas: %+v", svar.Samtal[0])
	}
}

func TestSamtalsroutenGer404ForOkantProjekt(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/finns-inte/samtal", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("väntade 404, fick %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "finns inte i PM-workspacet") {
		t.Fatalf("väntade svenskt fel, fick %s", w.Body.String())
	}
}

func TestSamtalsroutenAvvisarTomTextOchFelMetod(t *testing.T) {
	srv, _ := testServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal", bytes.NewBufferString(`{"text":"  "}`))
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("tom text gav %d", w.Code)
	}

	dw := httptest.NewRecorder()
	srv.ServeHTTP(dw, httptest.NewRequest(http.MethodDelete, "/api/projects/demo/samtal", nil))
	if dw.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE gav %d, väntade 405", dw.Code)
	}
}

func TestFragaViaRoutenSparasSomAiInlagg(t *testing.T) {
	srv, _ := testServer(t)
	kropp := bytes.NewBufferString(`{"text":"vilka tasks är öppna?","actor":"human:rasmus","fraga":true}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal", kropp))
	if w.Code != http.StatusCreated {
		t.Fatalf("fråga gav %d: %s", w.Code, w.Body.String())
	}
	var post pm.Inlagg
	if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
		t.Fatal(err)
	}
	if post.Actor.Kind != models.ActorKindAI || post.Actor.Name != "fake-modell" {
		t.Fatalf("svaret sparades inte som ai-inlägg: %+v", post.Actor)
	}
}

// Tråd-vyn ska svara på djuplänken, och upstreams API får inte tappas bort.
func TestTradVyOchUpstreamFinnsKvar(t *testing.T) {
	srv, _ := testServer(t)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pm/demo", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pm.js") {
		t.Fatalf("tråd-vyn gav %d: %s", w.Code, w.Body.String())
	}

	cw := httptest.NewRecorder()
	srv.ServeHTTP(cw, httptest.NewRequest(http.MethodGet, "/pm-static/pm.js", nil))
	if cw.Code != http.StatusOK {
		t.Fatalf("statisk fil gav %d", cw.Code)
	}

	uw := httptest.NewRecorder()
	srv.ServeHTTP(uw, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if uw.Code != http.StatusOK || !strings.Contains(uw.Body.String(), "demo") {
		t.Fatalf("upstreams projekt-API gav %d: %s", uw.Code, uw.Body.String())
	}
}

func TestOversiktsroutenGerSektionerna(t *testing.T) {
	srv, db := testServer(t)
	nu := timeutil.Now()
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	taskID := ids.New()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, title, description, type, status, priority, task_seq, created_at, updated_at)
	                      VALUES(?,?,?,?,'task','todo',1,42,?,?)`, taskID, projectID, "Öppna P1-tasken", "text", nu, nu); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/oversikt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("oversikt gav %d: %s", w.Code, w.Body.String())
	}
	var o Oversikt
	if err := json.NewDecoder(w.Body).Decode(&o); err != nil {
		t.Fatalf("kunde inte läsa svaret: %v", err)
	}
	if o.Projekt == nil || o.Projekt.Alias != "demo" {
		t.Fatalf("projektet saknas i svaret: %+v", o.Projekt)
	}
	if len(o.Tasks) != 1 || o.Tasks[0].Ref != "TASK-42" {
		t.Fatalf("tasklistan är fel: %+v", o.Tasks)
	}
	// En öppen P1-task utan körning ska ligga under "väntar på mig".
	if len(o.Vantar) != 1 || o.Vantar[0].Sort != "beslut" {
		t.Fatalf("väntar-listan är fel: %+v", o.Vantar)
	}

	nw := httptest.NewRecorder()
	srv.ServeHTTP(nw, httptest.NewRequest(http.MethodGet, "/api/projects/finns-inte/oversikt", nil))
	if nw.Code != http.StatusNotFound {
		t.Fatalf("okänt projekt gav %d", nw.Code)
	}
}

// Ett obesvarat agentinlägg i tråden ska visas som en fråga.
func TestOversiktVisarObesvaradAgentfraga(t *testing.T) {
	srv, db := testServer(t)
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.NewSamtalStore(db).Add(context.Background(), projectID, "",
		models.Actor{Kind: models.ActorKindAI, Name: "fake-modell"}, "Ska jag hoppa över containern?"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/oversikt", nil))
	var o Oversikt
	if err := json.NewDecoder(w.Body).Decode(&o); err != nil {
		t.Fatal(err)
	}
	hittad := false
	for _, v := range o.Vantar {
		if v.Sort == "fraga" && strings.Contains(v.Text, "hoppa över containern") {
			hittad = true
		}
	}
	if !hittad {
		t.Fatalf("den obesvarade frågan saknas: %+v", o.Vantar)
	}
}

func TestDelaUtRoutenStartarKorningen(t *testing.T) {
	srv, db := testServer(t)
	nu := timeutil.Now()
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, title, description, type, status, priority, task_seq, created_at, updated_at)
	                      VALUES(?,?,?,?,'task','todo',3,43,?,?)`, ids.New(), projectID, "Task att dela ut", "text", nu, nu); err != nil {
		t.Fatal(err)
	}

	startade := make(chan string, 1)
	srv.MedUtdelare(func(taskID, agent string) string {
		startade <- agent
		return "startad"
	})

	kropp := bytes.NewBufferString(`{"task":"TASK-43","agent":"fake-modell"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut", kropp))
	if w.Code != http.StatusAccepted {
		t.Fatalf("dela-ut gav %d: %s", w.Code, w.Body.String())
	}
	select {
	case agent := <-startade:
		if agent != "fake-modell" {
			t.Fatalf("fel agent skickades vidare: %q", agent)
		}
	default:
		t.Fatal("utdelaren anropades aldrig")
	}

	// Okänd agent ska avvisas innan något startas.
	aw := httptest.NewRecorder()
	srv.ServeHTTP(aw, httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut",
		bytes.NewBufferString(`{"task":"TASK-43","agent":"finns-inte"}`)))
	if aw.Code != http.StatusBadRequest {
		t.Fatalf("okänd agent gav %d", aw.Code)
	}
	// Okänd task ska ge 404.
	tw := httptest.NewRecorder()
	srv.ServeHTTP(tw, httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut",
		bytes.NewBufferString(`{"task":"TASK-9999"}`)))
	if tw.Code != http.StatusNotFound {
		t.Fatalf("okänd task gav %d", tw.Code)
	}
}

func TestDelaUtUtanUtdelareGer503(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut",
		bytes.NewBufferString(`{"task":"TASK-1"}`)))
	if w.Code != http.StatusNotFound && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("utan utdelare väntade 404 eller 503, fick %d", w.Code)
	}
}
