package pmweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/pm"
)

type svarsfoljdAgent struct {
	svar     []string
	prompter []string
}

func (a *svarsfoljdAgent) Namn() string { return "svarsfoljd" }

func (a *svarsfoljdAgent) Fraga(_ context.Context, prompt string) (string, error) {
	a.prompter = append(a.prompter, prompt)
	index := len(a.prompter) - 1
	if index >= len(a.svar) {
		return "", errors.New("oväntat agentanrop")
	}
	return a.svar[index], nil
}

func skapaTestlinter(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	markor := filepath.Join(dir, "stdin-sokvag")
	skript := filepath.Join(dir, "linter.py")
	innehall := fmt.Sprintf(`import json
import os
import sys

text = sys.stdin.read()
with open(%q, "w", encoding="utf-8") as fil:
    fil.write(os.readlink("/proc/self/fd/0"))
hog = "Första" in text
print(json.dumps({
    "ord": 20,
    "totalt": 2 if hog else 0,
    "totalt_per100o": 12.5 if hog else 1.25,
    "overtradelser": {"passiv": 2 if hog else 0},
    "exempel_forbjudna": [],
    "exempel_marknadsord": []
}))
`, markor)
	if err := os.WriteFile(skript, []byte(innehall), 0o600); err != nil {
		t.Fatal(err)
	}
	return skript, markor
}

func konfigureraTestlinter(t *testing.T, sokvag string, aktiv bool) {
	t.Helper()
	medKonfigDir(t, t.TempDir())
	gammal := taskForslagKonfig
	taskForslagKonfig = func() (pm.Konfig, error) {
		konfig := pm.StandardKonfig()
		konfig.System = pm.SystemKonfig{
			KlarsprakAktiv: aktiv, KlarsprakSokvag: sokvag, KlarsprakMaxPer100: 3,
		}
		return konfig, nil
	}
	t.Cleanup(func() { taskForslagKonfig = gammal })
}

func korBerikning(t *testing.T, agent *svarsfoljdAgent) taskUtkast {
	t.Helper()
	srv, db := testServer(t)
	task := skapaForslagsTask(t, db)
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(agent)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/tasks/TASK-%d/foresla", task.Seq),
		bytes.NewBufferString(`{"sort":"berikning"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("berikningen gav %d: %s", w.Code, w.Body.String())
	}
	var utkast taskUtkast
	if err := json.NewDecoder(w.Body).Decode(&utkast); err != nil {
		t.Fatal(err)
	}
	return utkast
}

func TestKlarsprakUnderGransenGarRaktIgenom(t *testing.T) {
	skript, _ := skapaTestlinter(t)
	konfigureraTestlinter(t, skript, true)
	agent := &svarsfoljdAgent{svar: []string{
		`{"titel":"Tydlig titel","beskrivning":"## Kontext\nTydlig text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
	}}

	utkast := korBerikning(t, agent)
	if len(agent.prompter) != 1 {
		t.Fatalf("text under gränsen gav %d agentanrop", len(agent.prompter))
	}
	if utkast.KlarsprakPoang == nil || *utkast.KlarsprakPoang != 1.25 {
		t.Fatalf("oväntad klarspråkspoäng: %+v", utkast.KlarsprakPoang)
	}
}

func TestKlarsprakOverGransenSkrivsOmEnGang(t *testing.T) {
	skript, markor := skapaTestlinter(t)
	konfigureraTestlinter(t, skript, true)
	agent := &svarsfoljdAgent{svar: []string{
		`{"titel":"Första titel","beskrivning":"## Kontext\nFörsta text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
		`{"titel":"Andra titel","beskrivning":"## Kontext\nAndra text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
	}}

	utkast := korBerikning(t, agent)
	if len(agent.prompter) != 2 || !strings.Contains(agent.prompter[1], "passiv: 2") {
		t.Fatalf("omskrivningsanropet saknas eller saknar fynd: %#v", agent.prompter)
	}
	if utkast.Titel != "Andra titel" || !utkast.KlarsprakOmskriven {
		t.Fatalf("PM använde inte omskrivningen: %+v", utkast)
	}
	if utkast.KlarsprakPoang == nil || *utkast.KlarsprakPoang != 1.25 {
		t.Fatalf("PM visade inte andra svarets poäng: %+v", utkast.KlarsprakPoang)
	}
	tempfil, err := os.ReadFile(markor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(tempfil))); !os.IsNotExist(err) {
		t.Fatalf("den temporära linterfilen finns kvar: %v", err)
	}
}

func TestKlarsprakOmskrivningOverGransenVisasEfterEttForsok(t *testing.T) {
	skript, _ := skapaTestlinter(t)
	konfigureraTestlinter(t, skript, true)
	agent := &svarsfoljdAgent{svar: []string{
		`{"titel":"Första titel","beskrivning":"## Kontext\nFörsta text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
		`{"titel":"Första omskrivningen","beskrivning":"## Kontext\nFörsta text igen.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
	}}

	utkast := korBerikning(t, agent)
	if len(agent.prompter) != 2 {
		t.Fatalf("PM gjorde %d agentanrop i stället för två", len(agent.prompter))
	}
	if utkast.Titel != "Första omskrivningen" || utkast.KlarsprakPoang == nil || *utkast.KlarsprakPoang != 12.5 {
		t.Fatalf("PM visade inte den fortfarande dåliga omskrivningen: %+v", utkast)
	}
}

func TestLinterfelDoljerInteForslaget(t *testing.T) {
	skript := filepath.Join(t.TempDir(), "trasig-linter.py")
	if err := os.WriteFile(skript, []byte(`print("inte json")`), 0o600); err != nil {
		t.Fatal(err)
	}
	konfigureraTestlinter(t, skript, true)
	agent := &svarsfoljdAgent{svar: []string{
		`{"titel":"Tydlig titel","beskrivning":"## Kontext\nText.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
	}}

	utkast := korBerikning(t, agent)
	if utkast.Titel != "Tydlig titel" || len(agent.prompter) != 1 || utkast.KlarsprakPoang != nil {
		t.Fatalf("linterfelet ändrade förslaget: %+v, anrop=%d", utkast, len(agent.prompter))
	}
}

func TestSaknadEllerAvstangdLinterHopparOver(t *testing.T) {
	for _, testfall := range []struct {
		namn   string
		sokvag string
		aktiv  bool
	}{
		{"saknad", filepath.Join(t.TempDir(), "saknas.py"), true},
		{"avstängd", filepath.Join(t.TempDir(), "finns-kvar.py"), false},
	} {
		t.Run(testfall.namn, func(t *testing.T) {
			konfigureraTestlinter(t, testfall.sokvag, testfall.aktiv)
			agent := &svarsfoljdAgent{svar: []string{
				`{"titel":"Första titel","beskrivning":"## Kontext\nText.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa."}`,
			}}
			utkast := korBerikning(t, agent)
			if len(agent.prompter) != 1 || utkast.KlarsprakPoang != nil {
				t.Fatalf("lintningen hoppades inte över: anrop=%d, poäng=%v", len(agent.prompter), utkast.KlarsprakPoang)
			}
		})
	}
}

func TestForeslaNyTaskLintarAgentensText(t *testing.T) {
	skript, _ := skapaTestlinter(t)
	konfigureraTestlinter(t, skript, true)
	srv, _ := testServer(t)
	agent := &svarsfoljdAgent{svar: []string{
		`{"titel":"Första titel","beskrivning":"## Kontext\nFörsta text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa.","typ":"task","prioritet":3}`,
		`{"titel":"Andra titel","beskrivning":"## Kontext\nAndra text.\n\n## Acceptanskriterier\n- Klart.\n\n## Verifiering\n- Testa.","typ":"task","prioritet":3}`,
	}}
	srv.register = pm.NewAgentRegister()
	srv.register.Registrera(agent)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/demo/foresla-task",
		strings.NewReader(`{"text":"Skapa en tydlig task från detta råmaterial."}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("förslaget gav %d: %s", w.Code, w.Body.String())
	}
	var forslag nyttTaskForslag
	if err := json.NewDecoder(w.Body).Decode(&forslag); err != nil {
		t.Fatal(err)
	}
	if len(agent.prompter) != 2 || forslag.Titel != "Andra titel" || !forslag.KlarsprakOmskriven {
		t.Fatalf("autoläget använde inte omskrivningen: anrop=%d, förslag=%+v", len(agent.prompter), forslag)
	}
}
