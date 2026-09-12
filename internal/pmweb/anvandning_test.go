package pmweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
)

type namngivenAgent string

func (a namngivenAgent) Namn() string { return string(a) }
func (a namngivenAgent) Fraga(context.Context, string) (string, error) {
	return "", nil
}

func TestAnvandningsruttVisarSparatOchSaknatLageEfterOmstart(t *testing.T) {
	_, db := testServer(t)
	if err := pm.NewAnvandningStore(db).Spara(t.Context(), pm.Anvandning{
		Agent: "claude", Status: "allowed", Kvottyp: "five_hour", AvlastAt: 99,
		FemTimmar: &pm.Kvotfonster{Andel: 0.44, NollstallsAt: 1789141800},
		SjuDagar:  &pm.Kvotfonster{Andel: 0.63, NollstallsAt: 1789480800},
	}); err != nil {
		t.Fatal(err)
	}
	register := pm.NewAgentRegister()
	register.Registrera(namngivenAgent("claude"))
	register.Registrera(namngivenAgent("codex"))

	gammal := konfigWorkDir
	konfigWorkDir = func() string { return t.TempDir() }
	t.Cleanup(func() { konfigWorkDir = gammal })
	srv := New(db, models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, register)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/anvandning", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET gav %d: %s", w.Code, w.Body.String())
	}
	var svar struct {
		Anvandning []pm.Anvandning `json:"anvandning"`
	}
	if err := json.NewDecoder(w.Body).Decode(&svar); err != nil {
		t.Fatal(err)
	}
	if len(svar.Anvandning) != 2 {
		t.Fatalf("fick %d agenter: %+v", len(svar.Anvandning), svar.Anvandning)
	}
	if svar.Anvandning[0].Agent != "claude" || svar.Anvandning[0].Saknas {
		t.Fatalf("Claude saknar sparat läge: %+v", svar.Anvandning[0])
	}
	if svar.Anvandning[0].FemTimmar.Andel != 0.44 || svar.Anvandning[0].SjuDagar.Andel != 0.63 {
		t.Fatalf("Claude har fel andelar: %+v", svar.Anvandning[0])
	}
	if svar.Anvandning[1].Agent != "codex" || !svar.Anvandning[1].Saknas {
		t.Fatalf("Codex skulle sakna uppgift: %+v", svar.Anvandning[1])
	}
}
