package pmweb

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/service"
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

func TestKommentarerKanLasasViaTaskRef(t *testing.T) {
	srv, db := testServer(t)
	projekt, err := service.NewProjectService(db).GetByAlias(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(
		context.Background(), models.CreateTaskInput{
			ProjectID: projekt.ID, Title: "Rapporttask", Type: models.TaskType("task"),
			Priority: 3, Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	aktor := models.Actor{Kind: models.ActorKindAI, Name: "testagent"}
	if _, err := service.NewCommentService(db).Create(context.Background(), models.CreateCommentInput{
		TaskID: task.ID, Body: "Rapport med <tagg>.", Actor: aktor,
	}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	url := fmt.Sprintf("/api/tasks/TASK-%d/kommentarer", task.Seq)
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("kommentarer gav %d: %s", w.Code, w.Body.String())
	}
	var svar struct {
		Kommentarer []models.Comment `json:"kommentarer"`
	}
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if len(svar.Kommentarer) != 1 || svar.Kommentarer[0].Body != "Rapport med <tagg>." {
		t.Fatalf("fel kommentarer: %+v", svar.Kommentarer)
	}
	if svar.Kommentarer[0].Actor != aktor || svar.Kommentarer[0].CreatedAt == 0 {
		t.Fatalf("aktör eller tid saknas: %+v", svar.Kommentarer[0])
	}
}

func TestKunskapsrutterVisarDocsOchMinne(t *testing.T) {
	srv, db := testServer(t)
	projekt, err := service.NewProjectService(db).GetByAlias(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	aktor := models.Actor{Kind: models.ActorKindAI, Name: "testagent"}
	doc, err := service.NewDocService(db).Create(context.Background(), "demo", models.CreateDocInput{
		Title: "Utredning", Body: "# Rubrik\nOformaterad text", Actor: aktor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewMemoryService(db).Add(context.Background(), models.CreateMemoryInput{
		ProjectID: projekt.ID, Body: "Ett bestående beslut", Tags: "beslut", Actor: aktor,
	}); err != nil {
		t.Fatal(err)
	}

	tester := []struct {
		namn, url, innehall string
	}{
		{"docs", "/api/projects/demo/docs", `"title":"Utredning"`},
		{"doc", "/api/docs/" + doc.ID, `"body":"# Rubrik\nOformaterad text"`},
		{"minne", "/api/projects/demo/minne", `"body":"Ett bestående beslut"`},
	}
	for _, test := range tester {
		t.Run(test.namn, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.url, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), test.innehall) {
				t.Fatalf("GET %s gav %d: %s", test.url, w.Code, w.Body.String())
			}
		})
	}
}

func TestLasrutterGerSvensk404(t *testing.T) {
	srv, _ := testServer(t)
	tester := []struct {
		namn, url, text string
	}{
		{"task", "/api/tasks/TASK-9999/kommentarer", "tasken"},
		{"docs", "/api/projects/finns-inte/docs", "projektet"},
		{"doc", "/api/docs/finns-inte", "dokumentet"},
		{"minne", "/api/projects/finns-inte/minne", "projektet"},
	}
	for _, test := range tester {
		t.Run(test.namn, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.url, nil))
			if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), test.text) {
				t.Fatalf("GET %s gav %d: %s", test.url, w.Code, w.Body.String())
			}
		})
	}
}

func TestLasrutterAvvisarSkrivmetoder(t *testing.T) {
	srv, _ := testServer(t)
	for _, url := range []string{
		"/api/tasks/TASK-1/kommentarer",
		"/api/projects/demo/docs",
		"/api/docs/ett-id",
		"/api/projects/demo/minne",
	} {
		for _, metod := range []string{http.MethodPost, http.MethodPut} {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(metod, url, strings.NewReader(`{}`)))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s gav %d, väntade 405", metod, url, w.Code)
			}
		}
	}
}

func TestPMVyInnehallerKunskapOchKommentarspanel(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pm/demo", nil))
	for _, innehall := range []string{`data-v="kunskap"`, `id="docs"`, `id="minne"`, `id="kommentarsdrawer"`, `id="forloppruta"`} {
		if !strings.Contains(w.Body.String(), innehall) {
			t.Fatalf("PM-vyn saknar %s", innehall)
		}
	}
}

func TestSkapaTaskRoutenLaggerTaskenIRattProjekt(t *testing.T) {
	srv, db := testServer(t)
	w := httptest.NewRecorder()
	kropp := bytes.NewBufferString(`{"titel":"Ny task","beskrivning":"Från PM-webben"}`)
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/tasks", kropp))
	if w.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
	}
	var svar struct {
		Ref   string `json:"ref"`
		Titel string `json:"titel"`
	}
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if svar.Ref == "" || svar.Titel != "Ny task" {
		t.Fatalf("oväntat svar: %+v", svar)
	}
	var alias, beskrivning, typ string
	var prioritet int
	if err := db.QueryRow(`SELECT p.alias, t.description, t.type, t.priority
		FROM tasks t JOIN projects p ON p.id=t.project_id WHERE t.title=?`, "Ny task").
		Scan(&alias, &beskrivning, &typ, &prioritet); err != nil {
		t.Fatal(err)
	}
	if alias != "demo" || beskrivning != "Från PM-webben" || typ != "task" || prioritet != 3 {
		t.Fatalf("tasken fick fel projekt eller standardvärden: %q %q %q P%d", alias, beskrivning, typ, prioritet)
	}
}

func TestSkapaTaskRoutenAvvisarTomTitel(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/tasks",
		bytes.NewBufferString(`{"titel":"  "}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("tom titel gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ange taskens titel") {
		t.Fatalf("väntade svenskt fel, fick %s", w.Body.String())
	}
}

func TestSkapaTaskRoutenGer404ForOkantProjekt(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/finns-inte/tasks",
		bytes.NewBufferString(`{"titel":"Ny task"}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("okänt projekt gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "finns inte i PM-workspacet") {
		t.Fatalf("väntade svenskt fel, fick %s", w.Body.String())
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
	// Agentvalet valideras mot konfigurationen, samma källa som körningen
	// använder, så testet pekar konfigen på en egen fil.
	dir := t.TempDir()
	medKonfigDir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, pm.KonfigFil), []byte(`
default_agent = "fake-modell"

[agenter.fake-modell]
kommando = "/bin/sh"
args = ["-c", "printf %s {brief}"]
brief = "arg"
svar = "stdout"
`), 0o644); err != nil {
		t.Fatal(err)
	}
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

func medKonfigDir(t *testing.T, dir string) {
	t.Helper()
	gammal := konfigWorkDir
	konfigWorkDir = func() string { return dir }
	t.Cleanup(func() { konfigWorkDir = gammal })
}

func TestGetOchPutPaKonfigrouten(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, _ := testServer(t)

	gw := httptest.NewRecorder()
	srv.ServeHTTP(gw, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	if gw.Code != http.StatusOK {
		t.Fatalf("GET gav %d: %s", gw.Code, gw.Body.String())
	}
	var fore struct {
		Agenter map[string]pm.AgentKonfig `json:"agenter"`
		Sokvag  string                    `json:"sokvag"`
		Saknas  bool                      `json:"saknas"`
	}
	if err := json.NewDecoder(gw.Body).Decode(&fore); err != nil {
		t.Fatal(err)
	}
	if !fore.Saknas || fore.Sokvag != filepath.Join(dir, pm.KonfigFil) || len(fore.Agenter) == 0 {
		t.Fatalf("GET gav fel metadata eller defaulter: %+v", fore)
	}

	kropp := bytes.NewBufferString(`{
		"default_agent":"test",
		"agenter":{"test":{"kommando":"/bin/sh","args":["-c","printf %s {brief}"],"brief":"arg","svar":"stdout","timeout_sekunder":15}},
		"regler":[{"namn":"allt","agent":"test"}],
		"krok":{}
	}`)
	pw := httptest.NewRecorder()
	srv.ServeHTTP(pw, httptest.NewRequest(http.MethodPut, "/api/konfig", kropp))
	if pw.Code != http.StatusOK {
		t.Fatalf("PUT gav %d: %s", pw.Code, pw.Body.String())
	}
	k, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatalf("kunde inte läsa den skrivna filen: %v", err)
	}
	if k.DefaultAgent != "test" || k.Agenter["test"].TimeoutSekunder != 15 {
		t.Fatalf("PUT sparade fel konfiguration: %+v", k)
	}

	gw = httptest.NewRecorder()
	srv.ServeHTTP(gw, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	var efter struct {
		DefaultAgent string `json:"default_agent"`
		Saknas       bool   `json:"saknas"`
	}
	if err := json.NewDecoder(gw.Body).Decode(&efter); err != nil {
		t.Fatal(err)
	}
	if efter.Saknas || efter.DefaultAgent != "test" {
		t.Fatalf("GET läste inte den nya filen: %+v", efter)
	}
}

func TestPutKonfigAvvisarOgiltigUtanAttAndraFilen(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, _ := testServer(t)
	ursprung := pm.Konfig{
		DefaultAgent: "test",
		Agenter: map[string]pm.AgentKonfig{
			"test": {Kommando: "/bin/sh", Args: []string{"-c", "printf %s {brief}"}, Brief: "arg", Svar: "stdout", TimeoutSekunder: 15},
		},
	}
	if err := pm.SkrivKonfig(dir, ursprung); err != nil {
		t.Fatal(err)
	}
	sokvag := filepath.Join(dir, pm.KonfigFil)
	fore, err := os.ReadFile(sokvag)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/konfig", bytes.NewBufferString(`{
		"default_agent":"trasig",
		"agenter":{"trasig":{"args":["{brief}"],"brief":"arg","svar":"stdout","timeout_sekunder":15}},
		"regler":[],"krok":{}
	}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ogiltig PUT gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `agenten \"trasig\" saknar kommando i konfigurationen`) {
		t.Fatalf("PUT gav inte Valideras felmeddelande: %s", w.Body.String())
	}
	efter, err := os.ReadFile(sokvag)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fore, efter) {
		t.Fatal("den ogiltiga PUT-begäran ändrade konfigurationsfilen")
	}
}

func TestProvaKonfigKorAgentUtanKorningEllerTask(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, db := testServer(t)
	konfig := pm.Konfig{
		DefaultAgent: "test",
		Agenter: map[string]pm.AgentKonfig{
			"test": {
				Kommando: "/bin/echo", Args: []string{"prov fungerar", "{brief}"},
				Brief: "arg", Svar: "stdout", TimeoutSekunder: 15,
			},
		},
	}
	if err := pm.SkrivKonfig(dir, konfig); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/konfig/prova",
		bytes.NewBufferString(`{"agent":"test"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("prov gav %d: %s", w.Code, w.Body.String())
	}
	var svar struct {
		Exitkod int    `json:"exitkod"`
		Svar    string `json:"svar"`
	}
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if svar.Exitkod != 0 || !strings.Contains(svar.Svar, "prov fungerar") {
		t.Fatalf("oväntat provsvar: %+v", svar)
	}
	var korningar, tasks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_korningar`).Scan(&korningar); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if korningar != 0 || tasks != 0 {
		t.Fatalf("provet rörde PM-data: %d körningar, %d tasks", korningar, tasks)
	}
}

func TestForeslaAgentGerUtkastUtanAttAndraKonfig(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	sokvag := filepath.Join(dir, pm.KonfigFil)
	fore := []byte("behåll den här filen\n")
	if err := os.WriteFile(sokvag, fore, 0o600); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: `{
		"namn":"verktyg","kommando":"verktyg","args":["run","{brief}"],
		"brief":"arg","svar":"stdout","stdin":"devnull",
		"timeout_sekunder":60,"miljo":{"LAGE":"test"},"mcp":false
	}`})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
		bytes.NewBufferString(`{"beskrivning":"Kör verktyg med run och briefen"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("förslag gav %d: %s", w.Code, w.Body.String())
	}
	var forslag struct {
		Namn string `json:"namn"`
		pm.AgentKonfig
	}
	if err := json.NewDecoder(w.Body).Decode(&forslag); err != nil {
		t.Fatal(err)
	}
	if forslag.Namn != "verktyg" || forslag.Kommando != "verktyg" || forslag.Args[1] != "{brief}" {
		t.Fatalf("oväntat förslag: %+v", forslag)
	}
	efter, err := os.ReadFile(sokvag)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fore, efter) {
		t.Fatal("förslagsanropet ändrade pm.toml")
	}
}

func TestForeslaAgentAvvisarSvarSomInteArJSON(t *testing.T) {
	srv, _ := testServer(t)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: "kör verktyget med --help"})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
		bytes.NewBufferString(`{"beskrivning":"Ett eget verktyg"}`)))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("ogiltigt agentsvar gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "agenten svarade inte med ett giltigt agentblock") {
		t.Fatalf("felmeddelandet hjälper inte användaren: %s", w.Body.String())
	}
}

func medProjektBas(t *testing.T, bas string) {
	t.Helper()
	gammal := projektBasDir
	projektBasDir = func() (string, error) { return bas, nil }
	t.Cleanup(func() { projektBasDir = gammal })
}

func postProjekt(t *testing.T, srv *Server, body skapaProjektBody) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projekt", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	return w
}

func TestSkapaNyttProjektViaRouten(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	srv, db := testServer(t)

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "nytt-projekt", Namn: "Nytt projekt", Beskrivning: "Ett prov", Lage: "nytt", Sokvag: "nytt-projekt",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
	}

	repoSokvag := filepath.Join(bas, "nytt-projekt")
	readme, err := os.ReadFile(filepath.Join(repoSokvag, "README.md"))
	if err != nil || string(readme) != "# Nytt projekt\n" {
		t.Fatalf("README.md är fel: %q, %v", readme, err)
	}
	gitInfo, err := os.Stat(filepath.Join(repoSokvag, ".git"))
	if err != nil || !gitInfo.IsDir() {
		t.Fatalf("Git-repot saknas: %v", err)
	}
	antal, err := exec.Command("git", "-C", repoSokvag, "rev-list", "--count", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(antal)) != "1" {
		t.Fatalf("väntade en commit, fick %q: %v", antal, err)
	}
	var sparadSokvag string
	if err := db.QueryRow(`SELECT repo_path FROM projects WHERE alias='nytt-projekt'`).Scan(&sparadSokvag); err != nil {
		t.Fatal(err)
	}
	if sparadSokvag != repoSokvag || !strings.Contains(w.Body.String(), `"lank":"/pm/nytt-projekt"`) {
		t.Fatalf("projektet fick fel sökväg eller länk: %q, %s", sparadSokvag, w.Body.String())
	}
}

func TestRegistreraBefintligtRepoViaRouten(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	repoSokvag := filepath.Join(bas, "befintligt")
	if err := os.Mkdir(repoSokvag, 0o755); err != nil {
		t.Fatal(err)
	}
	if utdata, err := exec.Command("git", "-C", repoSokvag, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", utdata, err)
	}
	markor := filepath.Join(repoSokvag, "behall.txt")
	if err := os.WriteFile(markor, []byte("behåll"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, db := testServer(t)

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "befintligt", Namn: "Befintligt", Lage: "befintligt", Sokvag: repoSokvag,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
	}
	if innehall, err := os.ReadFile(markor); err != nil || string(innehall) != "behåll" {
		t.Fatalf("PM ändrade det befintliga repot: %q, %v", innehall, err)
	}
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE alias='befintligt' AND repo_path=?`, repoSokvag).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 1 {
		t.Fatalf("projektet registrerades inte: %d", antal)
	}
}

func TestProjektRoutenAvvisarSokvagUtanforBasen(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	srv, _ := testServer(t)
	utanfor := filepath.Join(filepath.Dir(bas), "utanfor")

	for namn, sokvag := range map[string]string{
		"absolut": utanfor,
		"parent":  "../smitare",
	} {
		t.Run(namn, func(t *testing.T) {
			w := postProjekt(t, srv, skapaProjektBody{
				Alias: "smitare-" + namn, Namn: "Smitare", Lage: "nytt", Sokvag: sokvag,
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if _, err := os.Stat(utanfor); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PM skrev utanför basmappen: %v", err)
	}
}

func TestProjektRoutenStadarEfterAliaskrock(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	srv, _ := testServer(t)
	sokvag := filepath.Join(bas, "ska-forsvinna")

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "demo", Namn: "Krock", Lage: "nytt", Sokvag: "ska-forsvinna",
	})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "används redan") {
		t.Fatalf("aliaskrocken gav %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(sokvag); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("projektmappen blev kvar efter aliaskrocken: %v", err)
	}
}

func TestNyttProjektAvvisarKatalogMedInnehall(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	sokvag := filepath.Join(bas, "upptagen")
	if err := os.Mkdir(sokvag, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sokvag, "viktigt.txt"), []byte("behåll"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "upptagen", Namn: "Upptagen", Lage: "nytt", Sokvag: "upptagen",
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "viktigt.txt") {
		t.Fatalf("upptagen katalog gav %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sokvag, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PM startade Git i den upptagna katalogen: %v", err)
	}
}

func TestForeslaAgentAnvanderAgentTillagdEfterStart(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	skript := filepath.Join(dir, "efterstart.sh")
	blocket := `{"namn":"efterstart","kommando":"efterstart","args":["{brief}"],` +
		`"brief":"arg","svar":"stdout","stdin":"devnull","timeout_sekunder":60,"miljo":{},"mcp":false}`
	if err := os.WriteFile(skript, []byte("#!/bin/sh\ncat <<'JSON'\n"+blocket+"\nJSON\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	konfig := "default_agent = \"efterstart\"\n\n[agenter.efterstart]\n  kommando = \"" + skript + "\"\n" +
		"  args = [\"{brief}\"]\n  brief = \"arg\"\n  svar = \"stdout\"\n  stdin = \"\"\n  timeout_sekunder = 60\n  mcp = false\n"
	if err := os.WriteFile(filepath.Join(dir, pm.KonfigFil), []byte(konfig), 0o600); err != nil {
		t.Fatal(err)
	}

	srv, _ := testServer(t)
	// Registret från starten känner inte till agenten. Den ska ändå gå att
	// använda, för konfigurationen läses vid varje anrop.
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: "fel agent svarade"})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
		bytes.NewBufferString(`{"beskrivning":"Ett verktyg","agent":"efterstart"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("agenten som lagts till efter starten gav %d: %s", w.Code, w.Body.String())
	}
	var forslag struct {
		Namn string `json:"namn"`
	}
	if err := json.NewDecoder(w.Body).Decode(&forslag); err != nil {
		t.Fatal(err)
	}
	if forslag.Namn != "efterstart" {
		t.Fatalf("fel agent svarade: %+v", forslag)
	}
}

func TestBegripligtProjektfelFoljerSentinelIntePratetOmFelet(t *testing.T) {
	fall := []struct {
		namn      string
		err       error
		kod       int
		meddeland string
	}{
		{"aliaset upptaget", fmt.Errorf("en helt annan formulering: %w", service.ErrAliasTaken), http.StatusConflict, "aliaset \"krock\" används redan"},
		{"alias saknas", fmt.Errorf("omskrivet: %w", service.ErrAliasRequired), http.StatusBadRequest, "ange ett alias"},
		{"namnet för långt", fmt.Errorf("omskrivet: %w", service.ErrNameTooLong), http.StatusBadRequest, "projektets namn får innehålla högst 255 tecken"},
		{"okänt fel", errors.New("disken är full"), http.StatusInternalServerError, "PM kunde inte registrera projektet"},
	}
	for _, f := range fall {
		t.Run(f.namn, func(t *testing.T) {
			meddelande, kod := begripligtProjektfel(f.err, "krock")
			if kod != f.kod || meddelande != f.meddeland {
				t.Fatalf("fick %d %q, ville ha %d %q", kod, meddelande, f.kod, f.meddeland)
			}
		})
	}
}

func skapaStromKorning(t *testing.T, db *sql.DB, status, logg string) *pm.Korning {
	t.Helper()
	projekt, err := service.NewProjectService(db).GetByAlias(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(
		t.Context(), models.CreateTaskInput{
			ProjectID: projekt.ID, Title: "Strömtest", Type: models.TaskType("task"), Priority: 3,
			Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	korning := &pm.Korning{
		ProjectID: projekt.ID, TaskID: task.ID, TaskRef: fmt.Sprintf("TASK-%d", task.Seq),
		Agent: "testagent", Status: pm.StatusKor, Logg: logg,
	}
	store := pm.NewKorningStore(db)
	if err := store.Skapa(t.Context(), korning); err != nil {
		t.Fatal(err)
	}
	if status == pm.StatusKlar || status == pm.StatusFel {
		exitkod := 0
		if status == pm.StatusFel {
			exitkod = 3
		}
		if err := store.Avsluta(t.Context(), korning.ID, status, exitkod, logg); err != nil {
			t.Fatal(err)
		}
		korning.Status = status
		korning.ExitKod = &exitkod
	}
	return korning
}

func skrivTestHandelser(t *testing.T, sokvag string, handelser ...pm.Handelse) {
	t.Helper()
	fil, err := os.Create(sokvag)
	if err != nil {
		t.Fatal(err)
	}
	kodare := json.NewEncoder(fil)
	for _, handelse := range handelser {
		if err := kodare.Encode(handelse); err != nil {
			fil.Close()
			t.Fatal(err)
		}
	}
	if err := fil.Close(); err != nil {
		t.Fatal(err)
	}
}

func stromData(t *testing.T, kropp io.Reader) []pm.Handelse {
	t.Helper()
	var handelser []pm.Handelse
	skanner := bufio.NewScanner(kropp)
	for skanner.Scan() {
		rad := strings.TrimPrefix(skanner.Text(), "data: ")
		if rad == skanner.Text() {
			continue
		}
		var handelse pm.Handelse
		if err := json.Unmarshal([]byte(rad), &handelse); err != nil {
			t.Fatalf("ogiltig SSE-data %q: %v", rad, err)
		}
		handelser = append(handelser, handelse)
	}
	if err := skanner.Err(); err != nil {
		t.Fatal(err)
	}
	return handelser
}

func TestKorningStromSpelarUppHandelserIOrdning(t *testing.T) {
	srv, db := testServer(t)
	logg := filepath.Join(t.TempDir(), "korning.log")
	korning := skapaStromKorning(t, db, pm.StatusKlar, logg)
	vantar := []pm.Handelse{
		{Tid: 1, Sort: "text", Text: "först"},
		{Tid: 2, Sort: "fil", Text: "sedan"},
		{Tid: 3, Sort: "kommando", Text: "sist"},
	}
	skrivTestHandelser(t, pm.HandelseSokvag(logg), vantar...)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/"+korning.ID+"/strom", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("strömmen gav %d och %q", w.Code, w.Header().Get("Content-Type"))
	}
	fick := stromData(t, w.Body)
	if len(fick) != 4 {
		t.Fatalf("väntade tre händelser och slutbesked, fick %+v", fick)
	}
	for i := range vantar {
		if fick[i] != vantar[i] {
			t.Fatalf("händelse %d blev %+v, väntade %+v", i, fick[i], vantar[i])
		}
	}
	if !strings.Contains(fick[3].Text, "exitkod 0") {
		t.Fatalf("slutbeskedet saknar exitkod: %+v", fick[3])
	}
}

func TestKorningStromFortsatterMedNyRad(t *testing.T) {
	srv, db := testServer(t)
	logg := filepath.Join(t.TempDir(), "korning.log")
	korning := skapaStromKorning(t, db, pm.StatusKor, logg)
	skrivTestHandelser(t, pm.HandelseSokvag(logg), pm.Handelse{Tid: 1, Sort: "text", Text: "redan skriven"})
	testserver := httptest.NewServer(srv)
	t.Cleanup(testserver.Close)

	ctx, avbryt := context.WithCancel(t.Context())
	defer avbryt()
	begaran, err := http.NewRequestWithContext(ctx, http.MethodGet, testserver.URL+"/api/korningar/"+korning.ID+"/strom", nil)
	if err != nil {
		t.Fatal(err)
	}
	svar, err := testserver.Client().Do(begaran)
	if err != nil {
		t.Fatal(err)
	}
	defer svar.Body.Close()
	skanner := bufio.NewScanner(svar.Body)
	if !skanner.Scan() || !strings.Contains(skanner.Text(), `"redan skriven"`) {
		t.Fatalf("första händelsen saknas: %q", skanner.Text())
	}

	fil, err := os.OpenFile(pm.HandelseSokvag(logg), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(fil).Encode(pm.Handelse{Tid: 2, Sort: "fil", Text: "ny rad"}); err != nil {
		fil.Close()
		t.Fatal(err)
	}
	if err := fil.Close(); err != nil {
		t.Fatal(err)
	}

	hittad := make(chan string, 1)
	go func() {
		for skanner.Scan() {
			if strings.HasPrefix(skanner.Text(), "data: ") {
				hittad <- skanner.Text()
				return
			}
		}
		close(hittad)
	}()
	select {
	case rad := <-hittad:
		if !strings.Contains(rad, `"ny rad"`) {
			t.Fatalf("väntade den nya raden, fick %q", rad)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("den nya raden kom inte på samma anslutning")
	}
}

func TestKorningStromUtanHandelsefilGerBeskedOchStanger(t *testing.T) {
	srv, db := testServer(t)
	logg := filepath.Join(t.TempDir(), "korning.log")
	korning := skapaStromKorning(t, db, pm.StatusKlar, logg)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/"+korning.ID+"/strom", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("strömmen gav %d: %s", w.Code, w.Body.String())
	}
	fick := stromData(t, w.Body)
	if len(fick) != 1 || fick[0].Text != "körningen strömmar inte" {
		t.Fatalf("oväntat besked: %+v", fick)
	}
}

func TestKorningStromGerSvensk404ForOkandKorning(t *testing.T) {
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/finns-inte/strom", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "körningen") || !strings.Contains(w.Body.String(), "finns inte") {
		t.Fatalf("väntade svensk 404, fick %d: %s", w.Code, w.Body.String())
	}
}

func TestStromAvslutarOvergivenKorning(t *testing.T) {
	srv, db := testServer(t)
	dir := t.TempDir()
	logg := filepath.Join(dir, "k.log")
	if err := os.WriteFile(pm.HandelseSokvag(logg), []byte(`{"tid":1,"sort":"text","text":"började"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var projektID string
	if err := db.QueryRow(`SELECT id FROM projects LIMIT 1`).Scan(&projektID); err != nil {
		t.Fatal(err)
	}
	taskID := ids.New()
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, task_seq, title, status, type, priority, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, taskID, projektID, 1, "Prov", "doing", "task", 3, nu, nu); err != nil {
		t.Fatal(err)
	}
	// PID 0 hoppas över av städningen, så vi pekar ut en död process.
	id := ids.New()
	if _, err := db.Exec(`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, logg_sokvag, skapad_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, projektID, taskID, "TASK-1", "claude", "", pm.StatusKor, dir, 999999, logg, nu); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/"+id+"/strom", nil))
	kropp := w.Body.String()
	if !strings.Contains(kropp, "började") {
		t.Fatalf("strömmen spelade inte upp händelsen: %s", kropp)
	}
	if !strings.Contains(kropp, "avbröts") {
		t.Fatalf("strömmen sa inte att körningen avbröts: %s", kropp)
	}
}

func TestStromKallarEttVanligtFelForFelInteAvbrott(t *testing.T) {
	srv, db := testServer(t)
	dir := t.TempDir()
	logg := filepath.Join(dir, "k.log")
	if err := os.WriteFile(pm.HandelseSokvag(logg), []byte(`{"tid":1,"sort":"text","text":"började"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var projektID string
	if err := db.QueryRow(`SELECT id FROM projects LIMIT 1`).Scan(&projektID); err != nil {
		t.Fatal(err)
	}
	taskID := ids.New()
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, task_seq, title, status, type, priority, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, taskID, projektID, 2, "Prov", "doing", "task", 3, nu, nu); err != nil {
		t.Fatal(err)
	}
	// Körningen ägs av den levande testprocessen, så städningen rör den inte.
	id := ids.New()
	if _, err := db.Exec(`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, logg_sokvag, skapad_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, projektID, taskID, "TASK-2", "claude", "", pm.StatusKor, dir, os.Getpid(), logg, nu); err != nil {
		t.Fatal(err)
	}
	// Agenten misslyckas på egen hand medan strömmen läser.
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = pm.NewKorningStore(db).Avsluta(context.Background(), id, pm.StatusFel, 137, logg)
	}()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/"+id+"/strom", nil))
	kropp := w.Body.String()
	if !strings.Contains(kropp, "avslutades med fel (exitkod 137)") {
		t.Fatalf("ett vanligt fel beskrevs inte som fel: %s", kropp)
	}
	if strings.Contains(kropp, "avbröts") {
		t.Fatalf("ett vanligt fel beskrevs som avbrott: %s", kropp)
	}
}
