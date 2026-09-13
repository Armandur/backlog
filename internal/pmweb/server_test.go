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
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/service"
	"github.com/mazen160/backlog/internal/timeutil"
)

type fakeAgent struct {
	svar   string
	fel    error
	prompt *string
	anrop  *int
	vanta  bool
}

func (f fakeAgent) Namn() string { return "fake-modell" }
func (f fakeAgent) Fraga(ctx context.Context, prompt string) (string, error) {
	if f.prompt != nil {
		*f.prompt = prompt
	}
	if f.anrop != nil {
		(*f.anrop)++
	}
	if f.vanta {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.svar, f.fel
}

func (f fakeAgent) Kor(ctx context.Context, in pm.KorInput) (pm.Resultat, error) {
	if f.prompt != nil {
		*f.prompt = in.Brief
	}
	if f.anrop != nil {
		(*f.anrop)++
	}
	if in.VidHandelse != nil {
		in.VidHandelse(pm.Handelse{Tid: timeutil.Now(), Sort: "text", Text: "tänker"})
	}
	if f.vanta {
		<-ctx.Done()
		return pm.Resultat{ExitKod: 1}, ctx.Err()
	}
	return pm.Resultat{Utdata: f.svar}, f.fel
}

func korForslagsanrop(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if strings.TrimSpace(konfigWorkDir()) == "" || konfigWorkDir() == "." {
		gammal := konfigWorkDir
		dir := t.TempDir()
		konfigWorkDir = func() string { return dir }
		t.Cleanup(func() { konfigWorkDir = gammal })
	}
	start := httptest.NewRecorder()
	srv.ServeHTTP(start, req)
	if start.Code != http.StatusAccepted {
		return start
	}
	var svar struct {
		Korning pm.Korning `json:"korning"`
	}
	if err := json.NewDecoder(start.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/forslag/"+svar.Korning.ID, nil))
		if w.Code != http.StatusAccepted {
			return w
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("förslaget blev inte klart")
	return start
}

func vantaPaAgentsvar(t *testing.T, db *sql.DB) pm.Inlagg {
	t.Helper()
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poster, err := pm.NewSamtalStore(db).List(t.Context(), projectID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(poster) > 1 && poster[len(poster)-1].Actor.Kind == models.ActorKindAI {
			return poster[len(poster)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agentsvaret kom inte")
	return pm.Inlagg{}
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

func TestFragaViaRoutenStartarKorningOchSpararAiInlagg(t *testing.T) {
	// Frågan skapar en körning med logg, så provet behöver ett eget workspace.
	medKonfigDir(t, t.TempDir())
	srv, db := testServer(t)
	kropp := bytes.NewBufferString(`{"text":"vilka tasks är öppna?","actor":"human:rasmus","fraga":true}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal", kropp))
	if w.Code != http.StatusAccepted {
		t.Fatalf("fråga gav %d: %s", w.Code, w.Body.String())
	}
	var startad pm.StartadFraga
	if err := json.NewDecoder(w.Body).Decode(&startad); err != nil {
		t.Fatal(err)
	}
	if startad.Korning == nil || startad.Korning.ID == "" || startad.Inlagg == nil {
		t.Fatalf("svaret saknar körning eller inlägg: %+v", startad)
	}
	post := vantaPaAgentsvar(t, db)
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
		"/api/projects/demo/filer",
		"/api/projects/demo/git-andringar",
		"/api/projects/demo/git-diff",
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
	body := w.Body.String()
	for _, innehall := range []string{`data-v="oversikt"`, `id="v-oversikt"`, `pm-oversikt.js`, `data-v="kunskap"`, `data-v="filer"`, `id="fillista"`, `id="filtext"`, `pm-filer.js`, `id="docs"`, `id="minne"`, `id="kommentarsdrawer"`, `id="forloppruta"`, `id="forslagsdrawer"`, `pm-forslag.js`, `id="testserverKnapp"`, `id="testserverLank"`, `id="testserverBadge"`, `id="testserverLoggruta"`, `id="testserverLogg"`} {
		if !strings.Contains(body, innehall) {
			t.Fatalf("PM-vyn saknar %s", innehall)
		}
	}
	skript := []string{"/pm-static/pm.js", "/pm-static/pm-konfig.js", "/pm-static/pm-strom.js", "/pm-static/pm-forslag.js"}
	for i := 1; i < len(skript); i++ {
		if strings.Index(body, skript[i-1]) >= strings.Index(body, skript[i]) {
			t.Fatalf("skripten laddas i fel ordning: %v", skript)
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
	srv.MedUtdelare(func(taskID, agent, modell, anstrangning string) string {
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

func TestProvaKonfigGerFemDelresultatUtanKorningEllerTask(t *testing.T) {
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
	var svar konfigProvSvar
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if len(svar.Delresultat) != 5 || !strings.Contains(svar.Svar, "prov fungerar") {
		t.Fatalf("oväntat provsvar: %+v", svar)
	}
	if svar.Delresultat[0].Status != "ok" || svar.Delresultat[2].Status != "overhoppad" || svar.Delresultat[4].Status != "overhoppad" {
		t.Fatalf("oväntade delresultat: %+v", svar.Delresultat)
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

func TestProvaKonfigForklararNarKommandotSaknas(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, _ := testServer(t)
	konfig := pm.Konfig{
		DefaultAgent: "saknas",
		Agenter: map[string]pm.AgentKonfig{
			"saknas": {
				Kommando: "ett-kommando-som-inte-finns", Args: []string{"{brief}"},
				Brief: "arg", Svar: "stdout", TimeoutSekunder: 15,
			},
		},
	}
	if err := pm.SkrivKonfig(dir, konfig); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/konfig/prova",
		bytes.NewBufferString(`{"agent":"saknas"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("prov gav %d: %s", w.Code, w.Body.String())
	}
	var svar konfigProvSvar
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	kommando := svar.Delresultat[0]
	if kommando.Status != "fel" || !strings.Contains(kommando.Meddelande, "finns inte eller kunde inte starta") {
		t.Fatalf("kommandofelet är inte begripligt: %+v", kommando)
	}
}

func TestByggKonfigProvSvarKontrollerarMCPActorOchSvarsfil(t *testing.T) {
	svarsfil := filepath.Join(t.TempDir(), "svar.txt")
	if err := os.WriteFile(svarsfil, []byte("Konfigurationen fungerar."), 0o600); err != nil {
		t.Fatal(err)
	}
	markor := "MCP-kontroll test"
	kommentarer := []*models.Comment{{
		Body: markor, Actor: models.Actor{Kind: models.ActorKindAI, Name: "claude"},
	}}
	svar := byggKonfigProvSvar("claude", pm.AgentKonfig{Svar: "fil", MCP: true},
		pm.Resultat{ExitKod: 0, Utdata: "Konfigurationen fungerar."}, nil, svarsfil, markor, kommentarer)

	if len(svar.Delresultat) != 5 {
		t.Fatalf("fick %d delresultat: %+v", len(svar.Delresultat), svar.Delresultat)
	}
	for _, del := range svar.Delresultat {
		if del.Status != "ok" {
			t.Fatalf("delresultatet %s misslyckades: %+v", del.Namn, del)
		}
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
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
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
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
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
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/konfig/foresla",
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

func TestStromSagerAvbrottAvenUtanTidigareHandelser(t *testing.T) {
	srv, db := testServer(t)
	dir := t.TempDir()
	logg := filepath.Join(dir, "k.log")
	var projektID string
	if err := db.QueryRow(`SELECT id FROM projects LIMIT 1`).Scan(&projektID); err != nil {
		t.Fatal(err)
	}
	taskID := ids.New()
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, task_seq, title, status, type, priority, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, taskID, projektID, 3, "Prov", "doing", "task", 3, nu, nu); err != nil {
		t.Fatal(err)
	}
	// Köad körning med död process, utan händelsefil.
	id := ids.New()
	if _, err := db.Exec(`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, logg_sokvag, skapad_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, projektID, taskID, "TASK-3", "claude", "", pm.StatusKoad, dir, 999999, logg, nu); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/korningar/"+id+"/strom", nil))
	kropp := w.Body.String()
	if !strings.Contains(kropp, "avbröts") {
		t.Fatalf("en övergiven köad körning sa inte att den avbröts: %s", kropp)
	}
	if strings.Contains(kropp, "strömmar inte") {
		t.Fatalf("beskedet blev missvisande: %s", kropp)
	}
}

func testserverLoggdata(t *testing.T, kropp io.Reader) []string {
	t.Helper()
	var rader []string
	skanner := bufio.NewScanner(kropp)
	for skanner.Scan() {
		rad := strings.TrimPrefix(skanner.Text(), "data: ")
		if rad == skanner.Text() {
			continue
		}
		var text string
		if err := json.Unmarshal([]byte(rad), &text); err != nil {
			t.Fatalf("ogiltig loggdata %q: %v", rad, err)
		}
		rader = append(rader, text)
	}
	if err := skanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rader
}

func TestTestserverloggSpelarUppSistaRaderna(t *testing.T) {
	srv, _ := testServer(t)
	dir := t.TempDir()
	medKonfigDir(t, dir)
	logg := filepath.Join(dir, "loggar", "testserver-demo.log")
	if err := os.MkdirAll(filepath.Dir(logg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logg, []byte("ett\ntvå\ntre\n<b>fyra</b>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/testserver/logg?tail=2", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("strömmen gav %d och %q", w.Code, w.Header().Get("Content-Type"))
	}
	fick := testserverLoggdata(t, w.Body)
	vantat := []string{"tre", "<b>fyra</b>", "Testservern har stoppats."}
	if !slices.Equal(fick, vantat) {
		t.Fatalf("loggen blev %#v, väntade %#v", fick, vantat)
	}
}

func TestTestserverloggFortsatterMedNyRadOchStoppbesked(t *testing.T) {
	srv, db := testServer(t)
	dir := t.TempDir()
	logg := filepath.Join(dir, "testserver.log")
	if err := os.WriteFile(logg, []byte("redan skriven\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	process := exec.Command("sleep", "300")
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
		_, _ = process.Process.Wait()
	})
	if _, err := db.Exec(`INSERT INTO pm_testservrar(alias,pid,port,startad_at,logg_sokvag) VALUES(?,?,?,?,?)`,
		"demo", process.Process.Pid, 18123, timeutil.Now(), logg); err != nil {
		t.Fatal(err)
	}
	webbserver := httptest.NewServer(srv)
	t.Cleanup(webbserver.Close)

	ctx, avbryt := context.WithCancel(t.Context())
	defer avbryt()
	begaran, err := http.NewRequestWithContext(ctx, http.MethodGet, webbserver.URL+"/api/projects/demo/testserver/logg", nil)
	if err != nil {
		t.Fatal(err)
	}
	svar, err := webbserver.Client().Do(begaran)
	if err != nil {
		t.Fatal(err)
	}
	defer svar.Body.Close()
	skanner := bufio.NewScanner(svar.Body)
	if !skanner.Scan() || !strings.Contains(skanner.Text(), "redan skriven") {
		t.Fatalf("första raden saknas: %q", skanner.Text())
	}

	fil, err := os.OpenFile(logg, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fil.WriteString("ny rad\n"); err != nil {
		fil.Close()
		t.Fatal(err)
	}
	if err := fil.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM pm_testservrar WHERE alias='demo'`); err != nil {
		t.Fatal(err)
	}

	klart := make(chan []string, 1)
	go func() {
		var rader []string
		for skanner.Scan() {
			if !strings.HasPrefix(skanner.Text(), "data: ") {
				continue
			}
			var text string
			if json.Unmarshal([]byte(strings.TrimPrefix(skanner.Text(), "data: ")), &text) == nil {
				rader = append(rader, text)
			}
		}
		klart <- rader
	}()
	select {
	case rader := <-klart:
		if !slices.Equal(rader, []string{"ny rad", "Testservern har stoppats."}) {
			t.Fatalf("liveflödet blev %#v", rader)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("liveflödet stängdes inte efter stopp")
	}
}

func TestTestserverloggUtanFilGerBesked(t *testing.T) {
	srv, _ := testServer(t)
	medKonfigDir(t, t.TempDir())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/testserver/logg", nil))
	fick := testserverLoggdata(t, w.Body)
	vantat := []string{"Testserverns logg finns inte ännu.", "Testservern kör inte."}
	if !slices.Equal(fick, vantat) {
		t.Fatalf("beskedet blev %#v, väntade %#v", fick, vantat)
	}
}

func TestDelaUtSkickarModellOchAnstrangning(t *testing.T) {
	srv, db := testServer(t)
	projektID := ""
	if err := db.QueryRow(`SELECT id FROM projects LIMIT 1`).Scan(&projektID); err != nil {
		t.Fatal(err)
	}
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, task_seq, title, status, type, priority, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, ids.New(), projektID, 44, "Prov", "todo", "task", 3, nu, nu); err != nil {
		t.Fatal(err)
	}
	sett := make(chan [2]string, 1)
	srv.MedUtdelare(func(taskID, agent, modell, anstrangning string) string {
		sett <- [2]string{modell, anstrangning}
		return "startad"
	})
	kropp := bytes.NewBufferString(`{"task":"TASK-44","modell":" opus ","anstrangning":"hog"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut", kropp))
	if w.Code != http.StatusAccepted {
		t.Fatalf("utdelningen gav %d: %s", w.Code, w.Body.String())
	}
	got := <-sett
	if got[0] != "opus" || got[1] != "hog" {
		t.Fatalf("utdelaren fick %q", got)
	}
}

func TestForeslaNyTaskGerKomplettForslagUtanSkrivning(t *testing.T) {
	srv, db := testServer(t)
	var prompt string
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{
		svar:   `{"titel":"Kort agenttitel","beskrivning":"## Kontext\nBakgrund.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa.","typ":"improvement","prioritet":2}`,
		prompt: &prompt,
	})

	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
		strings.NewReader(`{"text":"Bygg om taskflödet så agenten föreslår alla fält först."}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("förslaget gav %d: %s", w.Code, w.Body.String())
	}
	var forslag nyttTaskForslag
	if err := json.NewDecoder(w.Body).Decode(&forslag); err != nil {
		t.Fatal(err)
	}
	if forslag.Titel != "Kort agenttitel" || forslag.Typ != models.TaskTypeImprovement || forslag.Prioritet != 2 {
		t.Fatalf("oväntat förslag: %+v", forslag)
	}
	if !strings.Contains(forslag.Beskrivning, "## Acceptanskriterier") {
		t.Fatalf("beskrivningen saknar innehåll: %q", forslag.Beskrivning)
	}
	for _, taskTyp := range models.AllTaskTypes() {
		if !strings.Contains(prompt, string(taskTyp)) {
			t.Fatalf("prompten saknar typen %q", taskTyp)
		}
	}
	for _, rubrik := range []string{"Kontext", "Acceptanskriterier", "Verifiering"} {
		if !strings.Contains(prompt, rubrik) {
			t.Fatalf("prompten saknar rubriken %q", rubrik)
		}
	}
	verifieraIngaTasks(t, db)
}

func TestForeslaNyTaskVisarAgentfelOchOgiltigJSONUtanSkrivning(t *testing.T) {
	testfall := []struct {
		namn  string
		agent fakeAgent
		text  string
	}{
		{"agentfel", fakeAgent{fel: errors.New("verktyget startade inte")}, "agenten kunde inte skapa förslaget"},
		{"ogiltig JSON", fakeAgent{svar: "Här är mitt förslag."}, "agenten svarade inte med giltig JSON"},
	}
	for _, testfall := range testfall {
		t.Run(testfall.namn, func(t *testing.T) {
			srv, db := testServer(t)
			srv.register = pm.NewAgentRegister()
			srv.register.Registrera(testfall.agent)
			w := httptest.NewRecorder()
			w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
				strings.NewReader(`{"text":"Det här är en tillräckligt lång text."}`)))
			if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), testfall.text) {
				t.Fatalf("%s gav %d: %s", testfall.namn, w.Code, w.Body.String())
			}
			verifieraIngaTasks(t, db)
		})
	}
}

func TestForeslaNyTaskGerTimeoutUtanSkrivning(t *testing.T) {
	gammalTimeout := taskForslagTimeout
	taskForslagTimeout = time.Millisecond
	t.Cleanup(func() { taskForslagTimeout = gammalTimeout })
	srv, db := testServer(t)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{vanta: true})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
		strings.NewReader(`{"text":"Det här är en tillräckligt lång text."}`)))
	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), "agenten hann inte skapa ett förslag") {
		t.Fatalf("timeout gav %d: %s", w.Code, w.Body.String())
	}
	verifieraIngaTasks(t, db)
}

func TestForeslaNyTaskAvvisarKortTextUtanAgentanrop(t *testing.T) {
	for _, testfall := range []struct {
		namn string
		text string
		fel  string
	}{
		{"tom", "   ", "ange texten"},
		{"kort", "nio teck", "minst tio tecken"},
	} {
		t.Run(testfall.namn, func(t *testing.T) {
			srv, db := testServer(t)
			anrop := 0
			srv.register = pm.NewAgentRegister()
			srv.register.Registrera(fakeAgent{anrop: &anrop})
			kropp, err := json.Marshal(map[string]string{"text": testfall.text})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task", bytes.NewReader(kropp)))
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), testfall.fel) {
				t.Fatalf("%s text gav %d: %s", testfall.namn, w.Code, w.Body.String())
			}
			if anrop != 0 {
				t.Fatalf("kort text anropade agenten %d gånger", anrop)
			}
			verifieraIngaTasks(t, db)
		})
	}
}

func TestForeslaNyTaskKortArLangSvenskTitelPaTecken(t *testing.T) {
	srv, db := testServer(t)
	svar, err := json.Marshal(nyttTaskForslag{
		Titel: strings.Repeat("å", 300), Beskrivning: "## Kontext\nText", Typ: models.TaskTypeTask, Prioritet: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: string(svar)})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
		strings.NewReader(`{"text":"Det här är en tillräckligt lång text."}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("lång titel gav %d: %s", w.Code, w.Body.String())
	}
	var forslag nyttTaskForslag
	if err := json.NewDecoder(w.Body).Decode(&forslag); err != nil {
		t.Fatal(err)
	}
	// Tasktjänsten mäter i tecken, så kortningen ska göra det också.
	if len([]rune(forslag.Titel)) > 255 || !utf8.ValidString(forslag.Titel) {
		t.Fatalf("titeln blev %d tecken och giltig UTF-8=%t", len([]rune(forslag.Titel)), utf8.ValidString(forslag.Titel))
	}
	verifieraIngaTasks(t, db)
}

func TestForeslaNyTaskGerSvensk404ForOkantProjekt(t *testing.T) {
	srv, db := testServer(t)
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, "/api/projects/finns-inte/foresla-task",
		strings.NewReader(`{"text":"Det här är en tillräckligt lång text."}`)))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "finns inte i PM-workspacet") {
		t.Fatalf("okänt projekt gav %d: %s", w.Code, w.Body.String())
	}
	verifieraIngaTasks(t, db)
}

func verifieraIngaTasks(t *testing.T, db *sql.DB) {
	t.Helper()
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 0 {
		t.Fatalf("förslagsanropet skapade %d tasks", antal)
	}
}

func skapaForslagsTask(t *testing.T, db *sql.DB) *models.Task {
	t.Helper()
	projekt, err := service.NewProjectService(db).GetByAlias(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(
		t.Context(), models.CreateTaskInput{
			ProjectID: projekt.ID, Title: "Kort titel", Description: "Kort beskrivning",
			Type: models.TaskTypeTask, Priority: 3, Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestForeslaTaskKlassningGerUtkastUtanSkrivning(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	skapaKlassningsKorning(t, db, task, "gpt-5", "")
	var prompt string
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{
		svar:   `{"typ":"feature","prioritet":2,"modell":"gpt-5","anstrangning":""}`,
		prompt: &prompt,
	})

	w := httptest.NewRecorder()
	url := fmt.Sprintf("/api/tasks/TASK-%d/foresla", task.Seq)
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("klassningen gav %d: %s", w.Code, w.Body.String())
	}
	var utkast taskUtkast
	if err := json.NewDecoder(w.Body).Decode(&utkast); err != nil {
		t.Fatal(err)
	}
	if utkast.Typ != models.TaskTypeFeature || utkast.Prioritet != 2 || utkast.Modell != "gpt-5" || utkast.Anstrangning != "" {
		t.Fatalf("oväntat utkast: %+v", utkast)
	}
	if !strings.Contains(prompt, "gpt-5") {
		t.Fatalf("prompten saknar den historiska modellen: %s", prompt)
	}
	if strings.Contains(prompt, "anstrangning") {
		t.Fatalf("prompten erbjuder ansträngning utan konfigurationsstöd: %s", prompt)
	}
	for _, taskTyp := range models.AllTaskTypes() {
		if !strings.Contains(prompt, string(taskTyp)) {
			t.Fatalf("prompten saknar typen %q: %s", taskTyp, prompt)
		}
	}
	oandrad, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Get(
		t.Context(), task.ID, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if oandrad.Type != models.TaskTypeTask || oandrad.Priority != 3 || oandrad.Title != "Kort titel" ||
		oandrad.Description != "Kort beskrivning" {
		t.Fatalf("förslagsanropet ändrade tasken: %+v", oandrad)
	}
}

func skapaKlassningsKorning(t *testing.T, db *sql.DB, task *models.Task, modell, anstrangning string) {
	t.Helper()
	korning := &pm.Korning{
		ProjectID: task.ProjectID, TaskID: task.ID, TaskRef: fmt.Sprintf("TASK-%d", task.Seq),
		Agent: "testagent", Status: pm.StatusKlar, Modell: modell, Anstrangning: anstrangning,
	}
	if err := pm.NewKorningStore(db).Skapa(t.Context(), korning); err != nil {
		t.Fatal(err)
	}
}

func TestKlassningsvardenKommerFranKonfigOchHistorik(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	konfig := pm.Konfig{
		DefaultAgent: "testagent",
		Agenter: map[string]pm.AgentKonfig{
			"testagent": {
				Kommando: "/bin/true", Brief: "stdin", Svar: "stdout",
				Modell: "konfig-modell", Anstrangning: "high",
			},
		},
	}
	if err := pm.SkrivKonfig(dir, konfig); err != nil {
		t.Fatal(err)
	}
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	skapaKlassningsKorning(t, db, task, "historisk-modell", "xhigh")

	varden, err := srv.hamtaKlassningsvarden(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(varden.Modeller, ",") != "historisk-modell,konfig-modell" {
		t.Fatalf("oväntade modeller: %v", varden.Modeller)
	}
	if strings.Join(varden.Anstrangningar, ",") != "high,xhigh" {
		t.Fatalf("oväntade ansträngningar: %v", varden.Anstrangningar)
	}
	prompt := byggTaskForslagsprompt(forslagKlassning, task, varden)
	for _, varde := range []string{"historisk-modell", "konfig-modell", "high", "xhigh", "anstrangning"} {
		if !strings.Contains(prompt, varde) {
			t.Fatalf("prompten saknar %q: %s", varde, prompt)
		}
	}
}

func TestForeslaTaskAvvisarOkandaKlassningsvarden(t *testing.T) {
	for namn, testfall := range map[string]struct {
		svar string
		fel  string
	}{
		"modell":       {`{"typ":"bug","prioritet":2,"modell":"påhittad","anstrangning":""}`, "okänt modellvärde"},
		"ansträngning": {`{"typ":"bug","prioritet":2,"modell":"gpt-5","anstrangning":"extrem"}`, "okänt ansträngningsvärde"},
	} {
		t.Run(namn, func(t *testing.T) {
			srv, db := testServer(t)
			task := skapaForslagsTask(t, db)
			skapaKlassningsKorning(t, db, task, "gpt-5", "")
			srv.register = pm.NewAgentRegister()
			srv.register.Registrera(fakeAgent{svar: testfall.svar})
			w := httptest.NewRecorder()
			w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
				bytes.NewBufferString(`{"sort":"klassning"}`)))
			if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), testfall.fel) {
				t.Fatalf("okänt %s gav %d: %s", namn, w.Code, w.Body.String())
			}
		})
	}
}

func TestForeslaTaskMedTommaKlassningsvardenKräverTommaFalt(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	var prompt string
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{
		svar: `{"typ":"task","prioritet":3,"modell":"","anstrangning":""}`, prompt: &prompt,
	})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
		bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("tomma värden gav %d: %s", w.Code, w.Body.String())
	}
	var utkast taskUtkast
	if err := json.NewDecoder(w.Body).Decode(&utkast); err != nil {
		t.Fatal(err)
	}
	if utkast.Modell != "" || utkast.Anstrangning != "" {
		t.Fatalf("väntade tomma fält, fick %+v", utkast)
	}
	if !strings.Contains(prompt, "Inga kända modeller finns") || !strings.Contains(prompt, "Lämna modell som en tom sträng") {
		t.Fatalf("prompten förklarar inte tom modellista: %s", prompt)
	}
	if strings.Contains(prompt, "anstrangning") {
		t.Fatalf("prompten nämner ansträngning utan stöd: %s", prompt)
	}
}

func TestForeslaTaskBerikningOchPatchSpararGranskadeFalt(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: `{
		"titel":"Tydligare titel",
		"beskrivning":"## Kontext\nBakgrund.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."
	}`})

	fw := httptest.NewRecorder()
	url := fmt.Sprintf("/api/tasks/TASK-%d/foresla", task.Seq)
	fw = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{"sort":"berikning"}`)))
	if fw.Code != http.StatusOK {
		t.Fatalf("berikningen gav %d: %s", fw.Code, fw.Body.String())
	}
	var utkast taskUtkast
	if err := json.NewDecoder(fw.Body).Decode(&utkast); err != nil {
		t.Fatal(err)
	}
	if utkast.Titel != "Tydligare titel" || !strings.Contains(utkast.Beskrivning, "## Acceptanskriterier") {
		t.Fatalf("oväntat berikningsutkast: %+v", utkast)
	}

	pw := httptest.NewRecorder()
	patch := `{"titel":"Granskad titel","beskrivning":"Granskad beskrivning","typ":"improvement","prioritet":4}`
	srv.ServeHTTP(pw, httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/TASK-%d", task.Seq), strings.NewReader(patch)))
	if pw.Code != http.StatusOK {
		t.Fatalf("PATCH gav %d: %s", pw.Code, pw.Body.String())
	}
	sparad, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Get(
		t.Context(), task.ID, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if sparad.Title != "Granskad titel" || sparad.Description != "Granskad beskrivning" ||
		sparad.Type != models.TaskTypeImprovement || sparad.Priority != 4 {
		t.Fatalf("PATCH sparade fel värden: %+v", sparad)
	}
}

func TestForeslaTaskAvvisarSvarSomInteArJSON(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: "Jag föreslår en funktion."})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
		bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "giltig JSON") {
		t.Fatalf("ogiltigt agentsvar gav %d: %s", w.Code, w.Body.String())
	}
}

func TestForeslaTaskAvvisarOgiltigPrioritet(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: `{"typ":"bug","prioritet":8,"modell":"","anstrangning":""}`})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
		bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "prioriteten måste vara P1-P5") {
		t.Fatalf("ogiltig prioritet gav %d: %s", w.Code, w.Body.String())
	}
}

func TestForeslaTaskAvvisarTomBerikning(t *testing.T) {
	for namn, testfall := range map[string]struct {
		svar string
		text string
	}{
		"titel":       {`{"titel":" ","beskrivning":"Text"}`, "ange taskens titel"},
		"beskrivning": {`{"titel":"Titel","beskrivning":" "}`, "saknar en beskrivning"},
	} {
		t.Run(namn, func(t *testing.T) {
			srv, db := testServer(t)
			task := skapaForslagsTask(t, db)
			srv.register = pm.NewAgentRegister()
			srv.register.Registrera(fakeAgent{svar: testfall.svar})
			w := httptest.NewRecorder()
			w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
				bytes.NewBufferString(`{"sort":"berikning"}`)))
			if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), testfall.text) {
				t.Fatalf("tom %s gav %d: %s", namn, w.Code, w.Body.String())
			}
		})
	}
}

func TestTaskForslagsrutterGerSvensk404(t *testing.T) {
	srv, _ := testServer(t)
	for _, testfall := range []struct {
		metod string
		url   string
		kropp string
	}{
		{http.MethodPost, "/api/tasks/TASK-9999/foresla", `{"sort":"klassning"}`},
		{http.MethodPatch, "/api/tasks/TASK-9999", `{"prioritet":2}`},
	} {
		w := httptest.NewRecorder()
		w = korForslagsanrop(t, srv, httptest.NewRequest(testfall.metod, testfall.url, strings.NewReader(testfall.kropp)))
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "tasken finns inte") {
			t.Fatalf("%s %s gav %d: %s", testfall.metod, testfall.url, w.Code, w.Body.String())
		}
	}
}

func TestForeslaTaskVisarAgentfel(t *testing.T) {
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{fel: errors.New("verktyget startade inte")})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
		bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "agenten kunde inte skapa förslaget") {
		t.Fatalf("agentfelet gav %d: %s", w.Code, w.Body.String())
	}
}

func TestForeslaTaskGerBegripligtTimeoutfel(t *testing.T) {
	gammalTimeout := taskForslagTimeout
	taskForslagTimeout = time.Millisecond
	t.Cleanup(func() { taskForslagTimeout = gammalTimeout })
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{vanta: true})
	w := httptest.NewRecorder()
	w = korForslagsanrop(t, srv, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%s/foresla", task.ID),
		bytes.NewBufferString(`{"sort":"klassning"}`)))
	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), "hann inte skapa ett förslag") {
		t.Fatalf("timeout gav %d: %s", w.Code, w.Body.String())
	}
}

// Ett kvarglömt testserverblock för ett raderat projekt får inte spärra
// konfigvyn. Går vyn inte att läsa går blocket inte att ta bort heller.
func TestKonfigVisasAvenMedTestserverForRaderatProjekt(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	konfig := "[agenter.claude]\n  kommando = \"claude\"\n  args = [\"{brief}\"]\n" +
		"\n[testserver.raderat]\n  kommando = \"python3\"\n  args = [\"{port}\"]\n"
	if err := os.WriteFile(filepath.Join(dir, pm.KonfigFil), []byte(konfig), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("konfigvyn gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "raderat") {
		t.Fatalf("blocket kom inte med, då går det inte att ta bort: %s", w.Body.String())
	}
}

func TestSparaOrelateradAndringBevararOvergivenTestserver(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	ursprung := `default_agent = "claude"

[agenter.claude]
kommando = "claude"
args = ["{brief}"]

[agenter.codex]
kommando = "codex"
args = ["{brief}"]

[testserver.raderat]
kommando = "python3"
args = ["-m", "http.server", "{port}"]

[testserver.raderat.miljo]
SERVER_TOKEN = "hemlig-server-token"
`
	if err := os.WriteFile(filepath.Join(dir, pm.KonfigFil), []byte(ursprung), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)

	hamta := httptest.NewRecorder()
	srv.ServeHTTP(hamta, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	if hamta.Code != http.StatusOK {
		t.Fatalf("GET gav %d: %s", hamta.Code, hamta.Body.String())
	}
	var utkast map[string]any
	if err := json.NewDecoder(hamta.Body).Decode(&utkast); err != nil {
		t.Fatal(err)
	}
	utkast["default_agent"] = "codex"
	delete(utkast, "testserver")
	data, err := json.Marshal(utkast)
	if err != nil {
		t.Fatal(err)
	}

	spara := httptest.NewRecorder()
	srv.ServeHTTP(spara, httptest.NewRequest(http.MethodPut, "/api/konfig", bytes.NewReader(data)))
	if spara.Code != http.StatusOK {
		t.Fatalf("PUT gav %d: %s", spara.Code, spara.Body.String())
	}
	if strings.Contains(spara.Body.String(), "hemlig-server-token") {
		t.Fatalf("PUT-svaret innehåller testserverns hemlighet: %s", spara.Body.String())
	}
	efter, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	server, finns := efter.Testserver["raderat"]
	if !finns || server.Miljo["SERVER_TOKEN"] != "hemlig-server-token" {
		t.Fatalf("den orelaterade ändringen tappade testserverblocket: %+v", efter.Testserver)
	}
	if efter.DefaultAgent != "codex" {
		t.Fatalf("den orelaterade ändringen sparades inte: %q", efter.DefaultAgent)
	}

	var bort map[string]any
	if err := json.NewDecoder(spara.Body).Decode(&bort); err != nil {
		t.Fatal(err)
	}
	bort["testserver"] = map[string]any{}
	bort["raderade_testservrar"] = []string{"raderat"}
	data, err = json.Marshal(bort)
	if err != nil {
		t.Fatal(err)
	}
	radera := httptest.NewRecorder()
	srv.ServeHTTP(radera, httptest.NewRequest(http.MethodPut, "/api/konfig", bytes.NewReader(data)))
	if radera.Code != http.StatusOK {
		t.Fatalf("aktiv borttagning gav %d: %s", radera.Code, radera.Body.String())
	}
	efter, err = pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, finns := efter.Testserver["raderat"]; finns {
		t.Fatalf("aktiv borttagning lämnade blocket: %+v", efter.Testserver)
	}
}

func TestTestserverrutterStartarVisarOchStoppar(t *testing.T) {
	srv, db := testServer(t)
	dir := t.TempDir()
	gammal := konfigWorkDir
	konfigWorkDir = func() string { return dir }
	t.Cleanup(func() { konfigWorkDir = gammal })
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, dir); err != nil {
		t.Fatal(err)
	}
	skript := filepath.Join(dir, "webbserver.sh")
	if err := os.WriteFile(skript, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	konfig := fmt.Sprintf("[portar]\nfran = 18200\ntill = 18299\n\n[testserver.demo]\nkommando = %q\nargs = [\"{port}\"]\ncwd = %q\n", skript, dir)
	if err := os.WriteFile(filepath.Join(dir, pm.KonfigFil), []byte(konfig), 0o600); err != nil {
		t.Fatal(err)
	}

	start := httptest.NewRecorder()
	srv.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/projects/demo/testserver/start", nil))
	if start.Code != http.StatusCreated {
		t.Fatalf("start gav %d: %s", start.Code, start.Body.String())
	}
	var server struct {
		PID    int    `json:"pid"`
		Port   int    `json:"port"`
		Lever  bool   `json:"lever"`
		Status string `json:"status"`
		Lank   string `json:"lank"`
	}
	if err := json.NewDecoder(start.Body).Decode(&server); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopp := httptest.NewRecorder()
		srv.ServeHTTP(stopp, httptest.NewRequest(http.MethodPost, "/api/projects/demo/testserver/stop", nil))
	})
	vard, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if !server.Lever || server.Status != pm.TestserverStartar ||
		server.PID <= 0 || server.Lank != fmt.Sprintf("http://%s:%d/", vard, server.Port) {
		t.Fatalf("fel startsvar: %+v", server)
	}

	status := httptest.NewRecorder()
	srv.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/projects/demo/testserver", nil))
	if status.Code != http.StatusOK ||
		!strings.Contains(status.Body.String(), `"status":"startar"`) {
		t.Fatalf("status gav %d: %s", status.Code, status.Body.String())
	}
	stopp := httptest.NewRecorder()
	srv.ServeHTTP(stopp, httptest.NewRequest(http.MethodPost, "/api/projects/demo/testserver/stop", nil))
	if stopp.Code != http.StatusOK ||
		!strings.Contains(stopp.Body.String(), `"status":"nere"`) {
		t.Fatalf("stopp gav %d: %s", stopp.Code, stopp.Body.String())
	}
}

func TestWebbstartStadarDodTestserverrad(t *testing.T) {
	_, db := testServer(t)
	if _, err := db.Exec(`INSERT INTO pm_testservrar(alias,pid,port,startad_at,logg_sokvag) VALUES(?,?,?,?,?)`,
		"demo", 99999999, 18234, timeutil.Now(), "/tmp/finns-inte.log"); err != nil {
		t.Fatal(err)
	}
	New(db, models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, pm.NewAgentRegister())
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_testservrar WHERE alias='demo'`).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 0 {
		t.Fatalf("webbstarten lämnade %d död testserverrad", antal)
	}
}

// testServerIKatalog lägger databasen i samma katalog som konfigurationen,
// precis som i skarp drift. Då kontrolleras testserverblocken mot riktiga
// projektalias när konfigurationen sparas.
func testServerIKatalog(t *testing.T, dir string) *Server {
	t.Helper()
	db, err := repo.Open(filepath.Join(dir, "backlog.db"))
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
	medKonfigDir(t, dir)
	return New(db, models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, pm.NewAgentRegister())
}

func TestSkapaProjektSpararStartkommandoSomTestserver(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	dir := t.TempDir()
	srv := testServerIKatalog(t, dir)

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "webbshop", Namn: "Webbshop", Lage: "nytt", Sokvag: "webbshop",
		Startkommando: "npm run dev -- --port {port}",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "varning") {
		t.Fatalf("projektet gav en varning: %s", w.Body.String())
	}

	konfig, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatalf("konfigurationen går inte att läsa: %v", err)
	}
	server, finns := konfig.Testserver["webbshop"]
	if !finns {
		t.Fatalf("testserverblocket saknas: %+v", konfig.Testserver)
	}
	if server.Kommando != "npm" {
		t.Fatalf("fel kommando: %+v", server)
	}
	if strings.Join(server.Args, " ") != "run dev -- --port {port}" {
		t.Fatalf("fel argument: %+v", server.Args)
	}
	if server.CWD != filepath.Join(bas, "webbshop") {
		t.Fatalf("fel arbetskatalog: %+v", server.CWD)
	}
}

func TestSkapaProjektUtanStartkommandoLamnarKonfigenTom(t *testing.T) {
	bas := t.TempDir()
	medProjektBas(t, bas)
	dir := t.TempDir()
	srv := testServerIKatalog(t, dir)

	w := postProjekt(t, srv, skapaProjektBody{
		Alias: "tomt", Namn: "Tomt", Lage: "nytt", Sokvag: "tomt",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST gav %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, pm.KonfigFil)); !os.IsNotExist(err) {
		t.Fatalf("PM skrev en konfigurationsfil i onödan: %v", err)
	}
}

// hemligKonfig skriver en pm.toml med hemligheter i alla tre miljöfälten.
func hemligKonfig(t *testing.T, dir string) {
	t.Helper()
	konfig := pm.StandardKonfig()
	agent := konfig.Agenter["claude"]
	agent.Miljo = map[string]string{"API_NYCKEL": "hemlig-nyckel"}
	konfig.Agenter["claude"] = agent
	konfig.Krok.Miljo = map[string]string{"KROK_TOKEN": "hemlig-krok"}
	if err := pm.SkrivKonfig(dir, konfig); err != nil {
		t.Fatalf("kunde inte skriva konfigurationen: %v", err)
	}
}

func TestKonfigruttenSkickarIngaHemligheter(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	hemligKonfig(t, dir)
	srv, _ := testServer(t)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET gav %d: %s", w.Code, w.Body.String())
	}
	kropp := w.Body.String()
	if strings.Contains(kropp, "hemlig-nyckel") || strings.Contains(kropp, "hemlig-krok") {
		t.Fatalf("svaret innehåller hemligheter: %s", kropp)
	}
	if !strings.Contains(kropp, "API_NYCKEL") || !strings.Contains(kropp, pm.MaskeratVarde) {
		t.Fatalf("svaret visar inte vilka variabler som är satta: %s", kropp)
	}
}

func TestSparaKonfigBevararMaskeradHemlighet(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	hemligKonfig(t, dir)
	srv, _ := testServer(t)

	hamta := httptest.NewRecorder()
	srv.ServeHTTP(hamta, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	kropp := hamta.Body.Bytes()

	spara := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/konfig", bytes.NewReader(kropp))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(spara, req)
	if spara.Code != http.StatusOK {
		t.Fatalf("PUT gav %d: %s", spara.Code, spara.Body.String())
	}
	if strings.Contains(spara.Body.String(), "hemlig-nyckel") {
		t.Fatalf("svaret på sparningen innehåller hemligheten: %s", spara.Body.String())
	}

	efter, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if efter.Agenter["claude"].Miljo["API_NYCKEL"] != "hemlig-nyckel" {
		t.Fatalf("hemligheten försvann ur pm.toml: %+v", efter.Agenter["claude"].Miljo)
	}
	if efter.Krok.Miljo["KROK_TOKEN"] != "hemlig-krok" {
		t.Fatalf("krokens hemlighet försvann: %+v", efter.Krok.Miljo)
	}
}

func TestSparaKonfigSkriverNyttMiljovarde(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	hemligKonfig(t, dir)
	srv, _ := testServer(t)

	konfig, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := konfig.Agenter["claude"]
	agent.Miljo = map[string]string{"API_NYCKEL": "ny-nyckel"}
	konfig.Agenter["claude"] = agent
	data, err := json.Marshal(konfig)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/konfig", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT gav %d: %s", w.Code, w.Body.String())
	}

	efter, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if efter.Agenter["claude"].Miljo["API_NYCKEL"] != "ny-nyckel" {
		t.Fatalf("det nya värdet sparades inte: %+v", efter.Agenter["claude"].Miljo)
	}
}

func TestArkiveraAterstallOchTaBortProjektViaRutterna(t *testing.T) {
	dir := t.TempDir()
	srv := testServerIKatalog(t, dir)
	bas := t.TempDir()
	medProjektBas(t, bas)

	if w := postProjekt(t, srv, skapaProjektBody{
		Alias: "bortprojekt", Namn: "Bort", Lage: "nytt", Sokvag: "bortprojekt",
		Startkommando: "npm run dev -- --port {port}",
	}); w.Code != http.StatusCreated {
		t.Fatalf("kunde inte skapa projektet: %s", w.Body.String())
	}

	// Ett projekt som inte är arkiverat får inte gå att ta bort.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/projekt/bortprojekt", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("borttagning utan arkivering gav %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projekt/bortprojekt/arkivera", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("arkiveringen gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "archived_at") {
		t.Fatalf("svaret säger inte att projektet är arkiverat: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projekt/bortprojekt/aterstall", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("återställningen gav %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projekt/bortprojekt/arkivera", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("andra arkiveringen gav %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/projekt/bortprojekt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("borttagningen gav %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "varning") {
		t.Fatalf("borttagningen varnade: %s", w.Body.String())
	}

	lista := httptest.NewRecorder()
	srv.ServeHTTP(lista, httptest.NewRequest(http.MethodGet, "/api/projects?include_archived=true", nil))
	if strings.Contains(lista.Body.String(), "bortprojekt") {
		t.Fatalf("projektet finns kvar i listan: %s", lista.Body.String())
	}

	konfig, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, finns := konfig.Testserver["bortprojekt"]; finns {
		t.Fatalf("testserverblocket finns kvar: %+v", konfig.Testserver)
	}
	// Koden på disken ska stå kvar, PM städar bara sitt eget.
	if _, err := os.Stat(filepath.Join(bas, "bortprojekt", "README.md")); err != nil {
		t.Fatalf("PM rörde koden på disken: %v", err)
	}
}

func TestTaBortOkantProjektGer404(t *testing.T) {
	dir := t.TempDir()
	srv := testServerIKatalog(t, dir)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/projekt/finns-inte", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("okänt projekt gav %d: %s", w.Code, w.Body.String())
	}
}

func TestArkiveringBevararAndraTestserverblock(t *testing.T) {
	dir := t.TempDir()
	srv := testServerIKatalog(t, dir)
	bas := t.TempDir()
	medProjektBas(t, bas)

	for _, alias := range []string{"kvar", "bort"} {
		if w := postProjekt(t, srv, skapaProjektBody{
			Alias: alias, Namn: alias, Lage: "nytt", Sokvag: alias,
			Startkommando: "npm run dev -- --port {port}",
		}); w.Code != http.StatusCreated {
			t.Fatalf("kunde inte skapa %s: %s", alias, w.Body.String())
		}
	}

	for _, vag := range []string{"/api/projekt/bort/arkivera"} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, vag, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s gav %d", vag, w.Code)
		}
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/projekt/bort", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("borttagningen gav %d: %s", w.Code, w.Body.String())
	}

	konfig, err := pm.LasKonfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, finns := konfig.Testserver["kvar"]; !finns {
		t.Fatalf("det andra projektets block försvann: %+v", konfig.Testserver)
	}
}

// taskForRedigering lägger en task i demo-projektet och ger dess id och ref.
func taskForRedigering(t *testing.T, db *sql.DB, seq int, titel string) (string, string) {
	t.Helper()
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	nu := timeutil.Now()
	taskID := ids.New()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, title, description, type, status, priority, task_seq, created_at, updated_at)
	                      VALUES(?,?,?,?,'task','todo',3,?,?,?)`, taskID, projectID, titel, "text", seq, nu, nu); err != nil {
		t.Fatal(err)
	}
	return taskID, fmt.Sprintf("TASK-%d", seq)
}

func TestRedigeraTaskViaRutten(t *testing.T) {
	srv, db := testServer(t)
	_, ref := taskForRedigering(t, db, 70, "Gammal titel")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+ref,
		bytes.NewBufferString(`{"titel":"Ny titel","beskrivning":"Ny text","typ":"bug","prioritet":1,"status":"doing"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH gav %d: %s", w.Code, w.Body.String())
	}
	var svar struct {
		Titel     string `json:"titel"`
		Typ       string `json:"typ"`
		Prioritet int    `json:"prioritet"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &svar); err != nil {
		t.Fatal(err)
	}
	if svar.Titel != "Ny titel" || svar.Typ != "bug" || svar.Prioritet != 1 || svar.Status != "doing" {
		t.Fatalf("fälten sparades inte: %+v", svar)
	}
}

func TestRedigeraTaskMedForLangTitelGerBegripligtFel(t *testing.T) {
	srv, db := testServer(t)
	_, ref := taskForRedigering(t, db, 71, "Titel")

	lang, _ := json.Marshal(map[string]string{"titel": strings.Repeat("å", 256)})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+ref, bytes.NewReader(lang))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("för lång titel gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "255 tecken") {
		t.Fatalf("felet hjälper inte användaren: %s", w.Body.String())
	}
}

func TestTaBortTaskViaRutten(t *testing.T) {
	srv, db := testServer(t)
	_, ref := taskForRedigering(t, db, 72, "Task att ta bort")

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+ref, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE gav %d: %s", w.Code, w.Body.String())
	}
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE task_seq=72`).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 0 {
		t.Fatal("tasken finns kvar")
	}
}

func TestTaBortTaskMedKoadKorningAvvisas(t *testing.T) {
	srv, db := testServer(t)
	taskID, ref := taskForRedigering(t, db, 73, "Task med körning")
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	// En köad körning väntar på sin tur efter TASK-1818 och måste också blockera.
	if _, err := db.Exec(`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, status, skapad_at)
	                      VALUES(?,?,?,?,'claude',?,?)`,
		ids.New(), projectID, taskID, ref, pm.StatusKoad, timeutil.Now()); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+ref, nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("borttagning under körning gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "väntar på tur") {
		t.Fatalf("felet säger inte vad som pågår: %s", w.Body.String())
	}
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE task_seq=73`).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 1 {
		t.Fatal("tasken togs bort trots körningen")
	}
}

func TestTaBortTaskMedPagaendeKorningAvvisas(t *testing.T) {
	srv, db := testServer(t)
	taskID, ref := taskForRedigering(t, db, 76, "Task under arbete")
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, status, skapad_at)
	                      VALUES(?,?,?,?,'claude',?,?)`,
		ids.New(), projectID, taskID, ref, pm.StatusKor, timeutil.Now()); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+ref, nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("borttagning under pågående körning gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "pågår") {
		t.Fatalf("felet säger inte att körningen pågår: %s", w.Body.String())
	}
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE task_seq=76`).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 1 {
		t.Fatal("tasken togs bort trots den pågående körningen")
	}
}

func TestEtiketterViaRutterna(t *testing.T) {
	srv, db := testServer(t)
	_, ref := taskForRedigering(t, db, 74, "Task med etiketter")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+ref+"/etiketter", bytes.NewBufferString(`{"namn":"brådskande"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("etiketten gick inte att lägga till: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "brådskande") {
		t.Fatalf("svaret saknar etiketten: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+ref+"/etiketter/br%C3%A5dskande", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("etiketten gick inte att ta bort: %d %s", w.Code, w.Body.String())
	}
	var kvar struct {
		Etiketter []struct {
			Name string `json:"name"`
		} `json:"etiketter"`
		Valbara []struct {
			Name string `json:"name"`
		} `json:"valbara_etiketter"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &kvar); err != nil {
		t.Fatal(err)
	}
	if len(kvar.Etiketter) != 0 {
		t.Fatalf("etiketten sitter kvar på tasken: %+v", kvar.Etiketter)
	}
	// Etiketten själv ska finnas kvar i projektet, andra tasks kan använda den.
	if len(kvar.Valbara) != 1 || kvar.Valbara[0].Name != "brådskande" {
		t.Fatalf("projektets etikett försvann: %+v", kvar.Valbara)
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+ref+"/etiketter/finns-inte", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("okänd etikett gav %d: %s", w.Code, w.Body.String())
	}
}

func TestPlanOchDetaljerViaRutterna(t *testing.T) {
	srv, db := testServer(t)
	_, ref := taskForRedigering(t, db, 75, "Task med plan")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+ref+"/plan",
		bytes.NewBufferString(`{"titel":"Genomförande","innehall":"## Steg\n\n1. Först\n2. Sedan"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("planen sparades inte: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/"+ref+"/detaljer", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("detaljerna gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Genomförande") {
		t.Fatalf("planen syns inte i detaljerna: %s", w.Body.String())
	}
}

// fakeAgentMedSvar svarar med en fast text, så förslagsprovet inte kör en agent.
type fakeAgentMedSvar struct{ svar string }

func (f fakeAgentMedSvar) Namn() string { return "fake" }
func (f fakeAgentMedSvar) Fraga(context.Context, string) (string, error) {
	return f.svar, nil
}

func serverMedAgentsvar(t *testing.T, svar string) (*Server, *sql.DB) {
	t.Helper()
	_, db := testServer(t)
	reg := pm.NewAgentRegister()
	reg.Registrera(fakeAgentMedSvar{svar: svar})
	return New(db, models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, reg), db
}

func repoForProjekt(t *testing.T, db *sql.DB, filer map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	for namn, innehall := range filer {
		if err := os.WriteFile(filepath.Join(repo, namn), []byte(innehall), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestForeslaTestserverFyllerEttUtkast(t *testing.T) {
	srv, db := serverMedAgentsvar(t, `{"kommando":"python3","args":["-m","http.server","{port}"],"cwd":"","halsa":"/","port":0,"forklaring":"Bara statiska filer."}`)
	repo := repoForProjekt(t, db, map[string]string{"index.html": "<h1>Hej</h1>"})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projekt/demo/foresla-testserver", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("förslaget gav %d: %s", w.Code, w.Body.String())
	}
	var f testserverForslag
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Kommando != "python3" || strings.Join(f.Args, " ") != "-m http.server {port}" {
		t.Fatalf("fel förslag: %+v", f)
	}
	// Tom arbetskatalog fylls med projektets repo, annars går blocket inte att spara.
	if f.CWD != repo {
		t.Fatalf("arbetskatalogen fylldes inte i: %+v", f)
	}

	// Förslaget får inte skrivas till pm.toml.
	if _, err := os.Stat(filepath.Join(konfigWorkDir(), pm.KonfigFil)); err == nil {
		t.Fatal("PM sparade förslaget utan att användaren godkände det")
	}
}

func TestForeslaTestserverAvvisarOanvandbartSvar(t *testing.T) {
	srv, db := serverMedAgentsvar(t, `{"kommando":"npm","args":["run","dev"],"halsa":"/","port":0}`)
	repoForProjekt(t, db, map[string]string{"package.json": `{"scripts":{"dev":"vite"}}`})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projekt/demo/foresla-testserver", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	// Varken fast port eller {port} i args, alltså går blocket inte att starta.
	if w.Code != http.StatusBadGateway {
		t.Fatalf("ett oanvändbart förslag gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "port") {
		t.Fatalf("felet säger inte vad som saknas: %s", w.Body.String())
	}
}

func TestForeslaTestserverUtanRepoSagerTill(t *testing.T) {
	srv, db := serverMedAgentsvar(t, "{}")
	if _, err := db.Exec(`UPDATE projects SET repo_path='' WHERE alias='demo'`); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projekt/demo/foresla-testserver", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("projekt utan repo gav %d: %s", w.Code, w.Body.String())
	}
}

func TestFilroutenListarOchLaserText(t *testing.T) {
	srv, db := testServer(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	innehall := "# Demo\n<script>alert('nej')</script>\n"
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(innehall), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/filer", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("rotlistan gav %d: %s", w.Code, w.Body.String())
	}
	var lista filsvar
	if err := json.NewDecoder(w.Body).Decode(&lista); err != nil {
		t.Fatal(err)
	}
	if !lista.Katalog || len(lista.Poster) != 2 {
		t.Fatalf("rotlistan är fel: %+v", lista)
	}
	if lista.Poster[0].Namn != "docs" || !lista.Poster[0].Katalog {
		t.Fatalf("mappar ska ligga först: %+v", lista.Poster)
	}
	for _, post := range lista.Poster {
		if post.Namn == ".git" {
			t.Fatal("filbläddraren visade .git")
		}
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/filer?path=README.md", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("textfilen gav %d: %s", w.Code, w.Body.String())
	}
	var fil filsvar
	if err := json.NewDecoder(w.Body).Decode(&fil); err != nil {
		t.Fatal(err)
	}
	if fil.Innehall != innehall || fil.Namn != "README.md" {
		t.Fatalf("filinnehållet är fel: %+v", fil)
	}
}

func TestFilroutenAvvisarOsakraSokvagar(t *testing.T) {
	srv, db := testServer(t)
	repo := t.TempDir()
	utanfor := filepath.Join(t.TempDir(), "hemlig.txt")
	if err := os.WriteFile(utanfor, []byte("hemligt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(utanfor, filepath.Join(repo, "lank.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}

	fall := []struct {
		sokvag string
		text   string
	}{
		{"../hemlig.txt", "får inte innehålla .."},
		{utanfor, "måste ligga under projektets repo"},
		{"lank.txt", "symbolisk länk"},
		{".git/config", ".git-katalogen visas inte"},
	}
	for _, test := range fall {
		t.Run(test.text, func(t *testing.T) {
			w := httptest.NewRecorder()
			adress := "/api/projects/demo/filer?path=" + test.sokvag
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, adress, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%q gav %d: %s", test.sokvag, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), test.text) {
				t.Fatalf("%q saknas i svaret: %s", test.text, w.Body.String())
			}
		})
	}
}

func TestFilroutenAvvisarStorOchBinarFil(t *testing.T) {
	srv, db := testServer(t)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "stor.txt"), make([]byte, maxFilstorlek+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bild.bin"), []byte{'P', 'N', 'G', 0, 1}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path=? WHERE alias='demo'`, repo); err != nil {
		t.Fatal(err)
	}

	fall := []struct {
		fil  string
		kod  int
		text string
	}{
		{"stor.txt", http.StatusRequestEntityTooLarge, "1048577 byte"},
		{"bild.bin", http.StatusUnsupportedMediaType, "binär"},
	}
	for _, test := range fall {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/filer?path="+test.fil, nil))
		if w.Code != test.kod || !strings.Contains(w.Body.String(), test.text) {
			t.Fatalf("%s gav %d: %s", test.fil, w.Code, w.Body.String())
		}
	}
}

func skapaGitRepoForProjekt(t *testing.T, db *sql.DB, filer map[string]string) string {
	t.Helper()
	repo := repoForProjekt(t, db, filer)
	kommandon := [][]string{
		{"init"},
		{"add", "."},
		{"-c", "user.name=PM-prov", "-c", "user.email=pm-prov@localhost", "commit", "-m", "Skapa prov"},
	}
	for _, argument := range kommandon {
		if utdata, err := exec.Command("git", append([]string{"-C", repo}, argument...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %s: %v", argument[0], utdata, err)
		}
	}
	return repo
}

func TestGitAndringarListarOchVisarDiff(t *testing.T) {
	srv, db := testServer(t)
	repo := skapaGitRepoForProjekt(t, db, map[string]string{"README.md": "före\n"})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("efter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ny.txt"), []byte("ny\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/git-andringar", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("Git-status gav %d: %s", w.Code, w.Body.String())
	}
	var lista gitAndringssvar
	if err := json.NewDecoder(w.Body).Decode(&lista); err != nil {
		t.Fatal(err)
	}
	if len(lista.Andringar) != 2 {
		t.Fatalf("Git-status saknar ändringar: %+v", lista.Andringar)
	}
	if lista.Andringar[0].Sokvag != "README.md" || lista.Andringar[0].Typ != "Ändrad" {
		t.Fatalf("den spårade ändringen är fel: %+v", lista.Andringar[0])
	}
	if lista.Andringar[1].Sokvag != "ny.txt" || lista.Andringar[1].Typ != "Ny" {
		t.Fatalf("den nya filen är fel: %+v", lista.Andringar[1])
	}

	for _, test := range []struct {
		fil  string
		text string
	}{
		{"README.md", "+efter"},
		{"ny.txt", "+ny"},
	} {
		w = httptest.NewRecorder()
		adress := "/api/projects/demo/git-diff?path=" + test.fil
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, adress, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), test.text) {
			t.Fatalf("diffen för %s gav %d: %s", test.fil, w.Code, w.Body.String())
		}
	}
}

func TestGitAndringarUtanRepoGerBesked(t *testing.T) {
	srv, db := testServer(t)
	repoForProjekt(t, db, map[string]string{"README.md": "hej\n"})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/git-andringar", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mappen utan Git gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "inte ett Git-repo") {
		t.Fatalf("svaret saknar ett begripligt besked: %s", w.Body.String())
	}
}

func TestGitDiffAvvisarStorDiff(t *testing.T) {
	srv, db := testServer(t)
	repo := skapaGitRepoForProjekt(t, db, map[string]string{"stor.txt": "kort\n"})
	if err := os.WriteFile(filepath.Join(repo, "stor.txt"), []byte(strings.Repeat("x", maxGitUtdata+1)), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/git-diff?path=stor.txt", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("den stora diffen gav %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "gränsen 1 MiB") {
		t.Fatalf("svaret saknar diffgränsen: %s", w.Body.String())
	}
}

func TestGitDiffAvvisarOsakerSokvag(t *testing.T) {
	srv, db := testServer(t)
	repo := skapaGitRepoForProjekt(t, db, map[string]string{"README.md": "hej\n"})
	utanfor := filepath.Join(t.TempDir(), "hemlig.txt")
	if err := os.WriteFile(utanfor, []byte("hemligt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(utanfor, filepath.Join(repo, "lank.txt")); err != nil {
		t.Fatal(err)
	}

	for _, sokvag := range []string{"../hemlig.txt", "lank.txt", ".git/config"} {
		w := httptest.NewRecorder()
		adress := "/api/projects/demo/git-diff?path=" + sokvag
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, adress, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s gav %d: %s", sokvag, w.Code, w.Body.String())
		}
	}
}

func TestForeslaTestserverForSokvagInnanProjektetFinns(t *testing.T) {
	srv, _ := serverMedAgentsvar(t, `{"kommando":"npm","args":["run","dev","--","--port","{port}"],"halsa":"/","port":0,"forklaring":"package.json har ett dev-skript."}`)
	bas := t.TempDir()
	medProjektBas(t, bas)
	repo := filepath.Join(bas, "befintligt")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/foresla-testserver", bytes.NewBufferString(`{"sokvag":"befintligt"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("förslaget gav %d: %s", w.Code, w.Body.String())
	}
	var f testserverForslag
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Kommando != "npm" || f.CWD != repo {
		t.Fatalf("fel förslag: %+v", f)
	}
}

func TestForeslaTestserverForSokvagAvvisarUtanforWorkspace(t *testing.T) {
	srv, _ := serverMedAgentsvar(t, "{}")
	medProjektBas(t, t.TempDir())

	for _, sokvag := range []string{"../hemligt", "/etc"} {
		w := httptest.NewRecorder()
		kropp, _ := json.Marshal(map[string]string{"sokvag": sokvag})
		req := httptest.NewRequest(http.MethodPost, "/api/foresla-testserver", bytes.NewReader(kropp))
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("sökvägen %q gav %d: %s", sokvag, w.Code, w.Body.String())
		}
	}
}

func TestKorningarBegransasOchPaginerasMedPagaendeKvar(t *testing.T) {
	srv, db := testServer(t)
	projekt, err := service.NewProjectService(db).GetByAlias(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(
		t.Context(), models.CreateTaskInput{
			ProjectID: projekt.ID, Title: "Många körningar", Type: models.TaskType("task"), Priority: 3,
			Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	bas := timeutil.Now() - 100
	for i := 0; i < 25; i++ {
		status := pm.StatusKlar
		exitkod := 0
		if i%2 == 0 {
			status = pm.StatusFel
			exitkod = 1
		}
		_, err := db.Exec(`INSERT INTO pm_korningar(
			id, project_id, task_id, task_ref, agent, status, exit_kod, skapad_at
		) VALUES(?,?,?,?,?,?,?,?)`,
			ids.New(), projekt.ID, task.ID, "TASK-1", "testagent", status, exitkod, bas+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	pagaendeID := ids.New()
	_, err = db.Exec(`INSERT INTO pm_korningar(
		id, project_id, task_id, task_ref, agent, status, skapad_at
	) VALUES(?,?,?,?,?,?,?)`,
		pagaendeID, projekt.ID, task.ID, "TASK-1", "testagent", pm.StatusKor, bas-100)
	if err != nil {
		t.Fatal(err)
	}

	type korsvar struct {
		Korningar  []pm.Korning `json:"korningar"`
		Fler       bool         `json:"fler"`
		NastaInnan string       `json:"nasta_innan"`
	}
	hamta := func(sokvag string) korsvar {
		t.Helper()
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, sokvag, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s gav %d: %s", sokvag, w.Code, w.Body.String())
		}
		var svar korsvar
		if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
			t.Fatal(err)
		}
		return svar
	}

	forsta := hamta("/api/projects/demo/korningar")
	if len(forsta.Korningar) != 20 {
		t.Fatalf("standardgränsen gav %d körningar, ville ha 20", len(forsta.Korningar))
	}
	if !forsta.Fler || forsta.NastaInnan == "" {
		t.Fatalf("svaret saknar cursor till äldre körningar: %+v", forsta)
	}
	if !slices.ContainsFunc(forsta.Korningar, func(k pm.Korning) bool { return k.ID == pagaendeID }) {
		t.Fatal("den pågående körningen saknas utanför historikgränsen")
	}

	andra := hamta("/api/projects/demo/korningar?status=alla&limit=20&innan=" + forsta.NastaInnan)
	if len(andra.Korningar) != 6 || andra.Fler {
		t.Fatalf("andra sidan blev fel: antal=%d fler=%v", len(andra.Korningar), andra.Fler)
	}
	pagaende := hamta("/api/projects/demo/korningar?status=pagaende")
	if len(pagaende.Korningar) != 1 || pagaende.Korningar[0].ID != pagaendeID {
		t.Fatalf("statusfiltret tappade den pågående körningen: %+v", pagaende.Korningar)
	}
}

func TestForslagBlirDoldKorningMedHandelser(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, db := testServer(t)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: `{"titel":"Titel","beskrivning":"## Kontext\nText","typ":"task","prioritet":3}`})

	start := httptest.NewRecorder()
	srv.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
		strings.NewReader(`{"text":"Det här är en tillräckligt lång text."}`)))
	if start.Code != http.StatusAccepted {
		t.Fatalf("starten gav %d: %s", start.Code, start.Body.String())
	}
	var startat struct {
		Korning pm.Korning `json:"korning"`
	}
	if err := json.NewDecoder(start.Body).Decode(&startat); err != nil {
		t.Fatal(err)
	}
	if startat.Korning.Sort != pm.KorningSortForslag || startat.Korning.TaskID != "" {
		t.Fatalf("förslaget fick fel körning: %+v", startat.Korning)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		korning, err := pm.NewKorningStore(db).Hamta(t.Context(), startat.Korning.ID)
		if err != nil {
			t.Fatal(err)
		}
		if korning.Status == pm.StatusKlar {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	handelser, err := os.ReadFile(pm.HandelseSokvag(startat.Korning.Logg))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(handelser), `"text":"tänker"`) {
		t.Fatalf("händelserna saknar agentens arbete: %s", handelser)
	}
	lista := httptest.NewRecorder()
	srv.ServeHTTP(lista, httptest.NewRequest(http.MethodGet, "/api/projects/demo/korningar", nil))
	if lista.Code != http.StatusOK || strings.Contains(lista.Body.String(), startat.Korning.ID) {
		t.Fatalf("körningslistan visade förslaget: %s", lista.Body.String())
	}
}

func TestSamtalsfragaFarEgenSort(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	srv, _ := testServer(t)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(fakeAgent{svar: "Svar."})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal",
		strings.NewReader(`{"text":"Fråga","fraga":true}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("frågan gav %d: %s", w.Code, w.Body.String())
	}
	var startat pm.StartadFraga
	if err := json.NewDecoder(w.Body).Decode(&startat); err != nil {
		t.Fatal(err)
	}
	if startat.Korning.Sort != pm.KorningSortFraga {
		t.Fatalf("samtalsfrågan fick sorten %q", startat.Korning.Sort)
	}
}

func TestAllaOversiktSamlarAktuelltLageFranFleraProjekt(t *testing.T) {
	srv, db := testServer(t)
	nu := timeutil.Now()
	andraID := ids.New()
	if _, err := db.Exec(`INSERT INTO projects(id,alias,name,repo_path,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		andraID, "andra", "Andra", "/repo/andra", nu, nu); err != nil {
		t.Fatal(err)
	}
	demo, err := service.NewProjectService(db).GetByAlias(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	skapaTask := func(projektID, titel string, prioritet int) *models.Task {
		t.Helper()
		task, skapafel := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(
			t.Context(), models.CreateTaskInput{
				ProjectID: projektID, Title: titel, Type: models.TaskType("task"), Priority: prioritet,
				Actor: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
			},
		)
		if skapafel != nil {
			t.Fatal(skapafel)
		}
		return task
	}
	demoTask := skapaTask(demo.ID, "Pågår i demo", 3)
	andraTask := skapaTask(andraID, "Pågår i andra", 3)
	koTask := skapaTask(demo.ID, "Väntar på repot", 3)
	felTask := skapaTask(andraID, "Misslyckad", 3)
	skapaTask(demo.ID, "Behöver beslut", 1)

	laggKorning := func(task *models.Task, projektID, ref, status, repo string, skapad int64) string {
		t.Helper()
		id := ids.New()
		_, korfel := db.Exec(`INSERT INTO pm_korningar(
			id,project_id,task_id,task_ref,agent,motivering,status,repo_path,skapad_at
		) VALUES(?,?,?,?,?,?,?,?,?)`, id, projektID, task.ID, ref, "testagent", "test", status, repo, skapad)
		if korfel != nil {
			t.Fatal(korfel)
		}
		return id
	}
	laggKorning(demoTask, demo.ID, fmt.Sprintf("TASK-%d", demoTask.Seq), pm.StatusKor, "/repo/demo", nu+1)
	laggKorning(andraTask, andraID, fmt.Sprintf("TASK-%d", andraTask.Seq), pm.StatusKor, "/repo/andra", nu+2)
	laggKorning(koTask, demo.ID, fmt.Sprintf("TASK-%d", koTask.Seq), pm.StatusKoad, "/repo/demo", nu+3)
	felID := laggKorning(felTask, andraID, fmt.Sprintf("TASK-%d", felTask.Seq), pm.StatusFel, "/repo/andra", nu+4)
	if _, err := db.Exec(`UPDATE pm_korningar SET exit_kod=7 WHERE id=?`, felID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		laggKorning(demoTask, demo.ID, fmt.Sprintf("TASK-%d", demoTask.Seq), pm.StatusKlar, "/repo/demo", nu-int64(i)-100)
	}
	if _, err := db.Exec(`INSERT INTO pm_samtal(
		id,project_id,actor_kind,actor_name,text,created_at
	) VALUES(?,?,?,?,?,?)`, ids.New(), andraID, "ai", "testagent", "Vilket alternativ ska jag välja?", nu+5); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/oversikt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("översikten gav %d: %s", w.Code, w.Body.String())
	}
	var svar allaOversikt
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if len(svar.Korningar) != 3 {
		t.Fatalf("översikten ska bara ge tre aktiva körningar, fick %d", len(svar.Korningar))
	}
	projekt := map[string]bool{}
	koorsak := ""
	for _, k := range svar.Korningar {
		projekt[k.Projekt.Alias] = true
		if k.Status == pm.StatusKoad {
			koorsak = k.Koorsak
		}
		if k.Status == pm.StatusKlar || k.Status == pm.StatusFel {
			t.Fatalf("historisk körning läckte in i pollningssvaret: %+v", k)
		}
	}
	if !projekt["demo"] || !projekt["andra"] {
		t.Fatalf("körningarna saknar ett projekt: %+v", projekt)
	}
	if !strings.Contains(koorsak, "Repot används") {
		t.Fatalf("kön saknar konkret orsak: %q", koorsak)
	}
	vantar := map[string]bool{}
	for _, v := range svar.Vantar {
		vantar[v.Sort] = true
	}
	if !vantar["fel"] || !vantar["fraga"] || !vantar["beslut"] {
		t.Fatalf("väntelägen saknas: %+v", svar.Vantar)
	}
	if len(svar.Testservrar) != 2 {
		t.Fatalf("projekt utan testserver saknas: %+v", svar.Testservrar)
	}
}

func TestAllaOversiktFungerarUtanKonfigureradAgent(t *testing.T) {
	srv, _ := testServer(t)
	srv.register = pm.NewAgentRegister()
	for _, sokvag := range []string{"/api/oversikt", "/api/anvandning"} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, sokvag, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s gav %d: %s", sokvag, w.Code, w.Body.String())
		}
	}
}

func TestDelaUtNekarTaskFranAnnatProjekt(t *testing.T) {
	srv, db := testServer(t)
	nu := timeutil.Now()
	annatID := ids.New()
	if _, err := db.Exec(`INSERT INTO projects(id,alias,name,created_at,updated_at) VALUES(?,?,?,?,?)`,
		annatID, "annat", "Annat projekt", nu, nu); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, title, description, type, status, priority, task_seq, created_at, updated_at)
	                      VALUES(?,?,?,?,'task','todo',3,777,?,?)`,
		ids.New(), annatID, "Task i annat projekt", "text", nu, nu); err != nil {
		t.Fatal(err)
	}
	srv.MedUtdelare(func(taskID, agent, modell, anstrangning string) string {
		t.Fatal("utdelaren startades trots att tasken hör till ett annat projekt")
		return ""
	})

	// Aliaset i adressen är demo, men tasken ligger i projektet annat.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/demo/dela-ut", bytes.NewBufferString(`{"task":"TASK-777"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("utdelningen godtog en task från ett annat projekt: %d %s", w.Code, w.Body.String())
	}
}
