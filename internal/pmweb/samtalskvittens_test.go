package pmweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
)

func TestKvitteringsroutenTommerOversiktenUtanAgentanrop(t *testing.T) {
	srv, db := testServer(t)
	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE alias='demo'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	post, err := pm.NewSamtalStore(db).Add(context.Background(), projectID, "",
		models.Actor{Kind: models.ActorKindAI, Name: "fake-modell"}, "Här är svaret.")
	if err != nil {
		t.Fatal(err)
	}
	anrop := 0
	reg := pm.NewAgentRegister()
	reg.Registrera(fakeAgent{svar: "ska inte användas", anrop: &anrop})
	srv.register = reg

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/projects/demo/samtal/"+post.ID+"/kvittera", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("kvitteringen gav %d: %s", w.Code, w.Body.String())
	}
	if anrop != 0 {
		t.Fatalf("kvitteringen startade agenten %d gånger", anrop)
	}

	o := hamtaTestoversikt(t, srv)
	if len(o.Vantar) != 0 {
		t.Fatalf("översikten är inte tom efter kvitteringen: %+v", o.Vantar)
	}
	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_samtal WHERE project_id=?`, projectID).Scan(&antal); err != nil {
		t.Fatal(err)
	}
	if antal != 1 {
		t.Fatalf("kvitteringen skrev ett nytt inlägg: %d", antal)
	}

	if _, err := pm.NewSamtalStore(db).Add(context.Background(), projectID, "",
		models.Actor{Kind: models.ActorKindAI, Name: "fake-modell"}, "Ett nyare svar."); err != nil {
		t.Fatal(err)
	}
	o = hamtaTestoversikt(t, srv)
	if len(o.Vantar) != 1 || !strings.Contains(o.Vantar[0].Text, "Ett nyare svar") {
		t.Fatalf("det nya agentsvaret syns inte: %+v", o.Vantar)
	}
}

func TestMinnesroutenSpararRedigeratForslagMedAgentaktor(t *testing.T) {
	srv, db := testServer(t)
	reg := pm.NewAgentRegister()
	reg.Registrera(fakeAgent{svar: "Vi behåller SQLite.\n<pm-minne>Behåll SQLite.</pm-minne>"})
	srv.register = reg

	fraga := httptest.NewRecorder()
	srv.ServeHTTP(fraga, httptest.NewRequest(http.MethodPost, "/api/projects/demo/samtal",
		bytes.NewBufferString(`{"text":"Vilken databas?","fraga":true}`)))
	if fraga.Code != http.StatusAccepted {
		t.Fatalf("frågan gav %d: %s", fraga.Code, fraga.Body.String())
	}
	var startad pm.StartadFraga
	if err := json.NewDecoder(fraga.Body).Decode(&startad); err != nil {
		t.Fatal(err)
	}
	svar := vantaPaAgentsvar(t, db)
	if svar.Text != "Vi behåller SQLite." || svar.Minnesforslag != "Behåll SQLite." {
		t.Fatalf("PM delade inte agentsvaret: %+v", svar)
	}

	minne := httptest.NewRecorder()
	srv.ServeHTTP(minne, httptest.NewRequest(http.MethodPost,
		"/api/projects/demo/samtal/"+svar.ID+"/minne",
		bytes.NewBufferString(`{"text":"Projektet använder SQLite."}`)))
	if minne.Code != http.StatusCreated {
		t.Fatalf("minnesrutten gav %d: %s", minne.Code, minne.Body.String())
	}
	var kropp, kind, namn string
	if err := db.QueryRow(`SELECT body, actor_kind, actor_name FROM project_memory ORDER BY created_at DESC LIMIT 1`).
		Scan(&kropp, &kind, &namn); err != nil {
		t.Fatal(err)
	}
	if kropp != "Projektet använder SQLite." || kind != "ai" || namn != "fake-modell" {
		t.Fatalf("minnesposten fick fel innehåll eller aktör: %q %s:%s", kropp, kind, namn)
	}

	poster, err := pm.NewSamtalStore(db).List(context.Background(), svar.ProjectID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if poster[len(poster)-1].Minnesforslag != "" {
		t.Fatalf("det sparade förslaget visas ännu: %+v", poster[len(poster)-1])
	}
}

func hamtaTestoversikt(t *testing.T, srv *Server) Oversikt {
	t.Helper()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/demo/oversikt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("översikten gav %d: %s", w.Code, w.Body.String())
	}
	var o Oversikt
	if err := json.NewDecoder(w.Body).Decode(&o); err != nil {
		t.Fatal(err)
	}
	return o
}
