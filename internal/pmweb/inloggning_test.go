package pmweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/pm"
)

func testInloggning(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	srv, _ := testServer(t)
	return KravInloggning(srv, Inloggning{Anvandare: "admin", Losenord: "admin"}), srv
}

func TestStartAvTestserverKraverInloggning(t *testing.T) {
	skyddad, _ := testInloggning(t)

	w := httptest.NewRecorder()
	skyddad.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/testserver/start", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("utan inloggning väntade 401, fick %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "backlog-pm") {
		t.Fatalf("svaret ber inte om inloggning: %q", w.Header().Get("WWW-Authenticate"))
	}
	if !strings.Contains(w.Body.String(), "Logga in") && !strings.Contains(w.Body.String(), "logga in") {
		t.Fatalf("beskedet säger inte vad användaren ska göra: %s", w.Body.String())
	}
}

func TestFelLosenordSlapperInteIgenom(t *testing.T) {
	skyddad, _ := testInloggning(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/konfig", nil)
	req.SetBasicAuth("admin", "fel")
	skyddad.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("fel lösenord gav %d: %s", w.Code, w.Body.String())
	}
}

func TestRattInloggningSlapperIgenom(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	skyddad, _ := testInloggning(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/konfig", nil)
	req.SetBasicAuth("admin", "admin")
	skyddad.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rätt inloggning gav %d: %s", w.Code, w.Body.String())
	}
}

func TestInloggningUrKonfigTarMiljonForst(t *testing.T) {
	konfig := pm.Konfig{Webb: pm.WebbKonfig{Anvandare: "rasmus", Losenord: "ur-filen"}}

	logg := InloggningUrKonfig(konfig)
	if logg.Anvandare != "rasmus" || logg.Losenord != "ur-filen" || logg.Standard {
		t.Fatalf("filens värden gällde inte: %+v", logg)
	}

	t.Setenv(LosenordMiljo, "ur-miljon")
	logg = InloggningUrKonfig(konfig)
	if logg.Losenord != "ur-miljon" {
		t.Fatalf("miljövariabeln vann inte: %+v", logg)
	}
}

func TestInloggningUtanKonfigVarnar(t *testing.T) {
	logg := InloggningUrKonfig(pm.Konfig{})
	if logg.Anvandare != StandardAnvandare || logg.Losenord != StandardLosenord || !logg.Standard {
		t.Fatalf("standardinloggningen gäller inte: %+v", logg)
	}
	varning := logg.Varning("/hem/pm.toml")
	if !strings.Contains(varning, "/hem/pm.toml") || !strings.Contains(varning, LosenordMiljo) {
		t.Fatalf("varningen säger inte vad användaren ska göra: %q", varning)
	}
	if InloggningUrKonfig(pm.Konfig{Webb: pm.WebbKonfig{Losenord: "eget"}}).Varning("x") != "" {
		t.Fatal("PM varnar trots ett eget lösenord")
	}
}

func TestLosenordetSkickasInteTillWebblasaren(t *testing.T) {
	dir := t.TempDir()
	medKonfigDir(t, dir)
	konfig := pm.StandardKonfig()
	konfig.Webb = pm.WebbKonfig{Anvandare: "rasmus", Losenord: "hemligt-losen"}
	if err := pm.SkrivKonfig(dir, konfig); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/konfig", nil))
	if strings.Contains(w.Body.String(), "hemligt-losen") {
		t.Fatalf("lösenordet finns i svaret: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), pm.MaskeratVarde) {
		t.Fatalf("lösenordet maskerades inte: %s", w.Body.String())
	}
}
