package pm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTolkaCodexStromFranInspeladeRader(t *testing.T) {
	rader := []string{
		`{"type":"thread.started","thread_id":"019c"}`,
		`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"go test ./internal/...","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"go test ./internal/...","aggregated_output":"ok","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"file_change","changes":[{"path":"internal/pm/handelse.go","kind":"update"},{"path":"internal/pm/ny.go","kind":"add"}],"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Tolken är klar."}}`,
		`det här är inte JSON`,
		`{"type":"turn.completed","usage":{"input_tokens":12}}`,
		`{"type":"framtida.typ","message":{"okänt":true}}`,
	}

	var handelser []Handelse
	for _, rad := range rader {
		handelser = append(handelser, tolkaCodexRad([]byte(rad))...)
	}
	vill := []struct {
		sort string
		text string
	}{
		{sort: "kommando", text: "körde go test ./internal/..."},
		{sort: "fil", text: "ändrade internal/pm/handelse.go"},
		{sort: "fil", text: "skapade internal/pm/ny.go"},
		{sort: "text", text: "Tolken är klar."},
		{sort: "fel", text: "kunde inte tolka agentutdata som JSON"},
	}
	if len(handelser) != len(vill) {
		t.Fatalf("fick %d händelser, vill ha %d: %+v", len(handelser), len(vill), handelser)
	}
	for i, vantad := range vill {
		if handelser[i].Sort != vantad.sort || handelser[i].Text != vantad.text {
			t.Fatalf("händelse %d blev %+v, vill ha sort %q och text %q", i, handelser[i], vantad.sort, vantad.text)
		}
		if handelser[i].Tid == 0 {
			t.Fatalf("händelse %d saknar tid", i)
		}
	}
}

func TestTolkaCodexRadRapporterarFel(t *testing.T) {
	fall := []struct {
		rad  string
		text string
	}{
		{
			rad:  `{"type":"item.completed","item":{"type":"command_execution","command":"false","exit_code":1,"status":"failed"}}`,
			text: "kommandot misslyckades med exitkod 1",
		},
		{rad: `{"type":"error","message":"strömmen bröts"}`, text: "strömmen bröts"},
		{rad: `{"type":"turn.failed","error":{"message":"modellen svarade inte"}}`, text: "modellen svarade inte"},
	}
	for _, testfall := range fall {
		handelser := tolkaCodexRad([]byte(testfall.rad))
		if len(handelser) != 1 || handelser[0].Sort != "fel" || handelser[0].Text != testfall.text {
			t.Fatalf("fick %+v, vill ha ett fel med texten %q", handelser, testfall.text)
		}
	}
}

func TestCodexJsonLaggerTillFlaggaOchTolkarStrom(t *testing.T) {
	skript := `if [ "$1" != "--json" ]
then
	exit 9
fi
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"Codex svarar."}}'`
	agent := NewKommandoAgent("codex-test", AgentKonfig{
		Kommando: "/bin/sh",
		Args:     []string{"-c", skript, "codex-test"},
		Brief:    "stdin",
		Svar:     "stdout",
		Strom:    "codex-json",
	})
	var handelser []Handelse
	res, err := agent.Kor(t.Context(), KorInput{VidHandelse: func(handelse Handelse) {
		handelser = append(handelser, handelse)
	}})
	if err != nil || res.ExitKod != 0 {
		t.Fatalf("agentkörningen misslyckades: %+v, %v", res, err)
	}
	if len(handelser) != 1 || handelser[0].Sort != "text" || handelser[0].Text != "Codex svarar." {
		t.Fatalf("fel händelser: %+v", handelser)
	}
}

func TestTolkaClaudeRad(t *testing.T) {
	fall := []struct {
		namn  string
		rad   string
		finns bool
		sort  string
		text  string
	}{
		{
			namn:  "text och verktyg i samma rad ger två händelser",
			rad:   `{"type":"assistant","message":{"content":[{"type":"text","text":"Jag öppnar filen."},{"type":"tool_use","name":"Read","input":{"file_path":"app/main.go"}}]}}`,
			finns: true,
			sort:  "text",
			text:  "Jag öppnar filen.",
		},
		{
			namn:  "assistenttext",
			rad:   `{"type":"assistant","message":{"content":[{"type":"text","text":"Jag kontrollerar filen."}]}}`,
			finns: true,
			sort:  "text",
			text:  "Jag kontrollerar filen.",
		},
		{
			namn:  "filsökväg",
			rad:   `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"app/main.go"}}]}}`,
			finns: true,
			sort:  "fil",
			text:  "läste app/main.go",
		},
		{
			namn:  "kommando",
			rad:   `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
			finns: true,
			sort:  "kommando",
			text:  "körde go test ./...",
		},
		{
			namn:  "inte JSON",
			rad:   `det här är inte JSON`,
			finns: true,
			sort:  "fel",
			text:  "kunde inte tolka agentutdata som JSON",
		},
		{
			namn:  "okänd typ",
			rad:   `{"type":"system","subtype":"init"}`,
			finns: false,
		},
	}

	for _, testfall := range fall {
		t.Run(testfall.namn, func(t *testing.T) {
			handelser := tolkaClaudeRad([]byte(testfall.rad))
			if (len(handelser) > 0) != testfall.finns {
				t.Fatalf("fick %d händelser, ville ha minst en: %t", len(handelser), testfall.finns)
			}
			if !testfall.finns {
				return
			}
			handelse := handelser[0]
			if handelse.Sort != testfall.sort || handelse.Text != testfall.text {
				t.Fatalf("fick %+v, vill ha sort %q och text %q", handelse, testfall.sort, testfall.text)
			}
			if handelse.Tid == 0 {
				t.Fatal("händelsen saknar tid")
			}
		})
	}
}

func TestTolkaClaudeRadKortArStoraIndata(t *testing.T) {
	kommando := "printf " + strings.Repeat("x", 1000)
	rad := `{"type":"tool_use","name":"Bash","input":{"command":"` + kommando + `"}}`
	handelser := tolkaClaudeRad([]byte(rad))
	if len(handelser) == 0 {
		t.Fatal("verktygsanropet gav ingen händelse")
	}
	handelse := handelser[0]
	if len([]rune(handelse.Text)) > 200 {
		t.Fatalf("texten är %d tecken, högst 200 tillåts", len([]rune(handelse.Text)))
	}
	if !strings.Contains(handelse.Text, "tecken till") {
		t.Fatalf("texten saknar besked om dolt innehåll: %q", handelse.Text)
	}
	if strings.Contains(handelse.Text, kommando) {
		t.Fatal("hela den stora indatan hamnade i händelsen")
	}
}

func TestTolkaClaudeRadGerBadeTextOchVerktyg(t *testing.T) {
	rad := `{"type":"assistant","message":{"content":[` +
		`{"type":"text","text":"Jag öppnar filen."},` +
		`{"type":"tool_use","name":"Read","input":{"file_path":"app/main.go"}}]}}`
	handelser := tolkaClaudeRad([]byte(rad))
	if len(handelser) != 2 {
		t.Fatalf("fick %d händelser, vill ha 2: %+v", len(handelser), handelser)
	}
	if handelser[0].Sort != "text" || handelser[1].Sort != "fil" {
		t.Fatalf("fel sorter: %+v", handelser)
	}
	if handelser[1].Text != "läste app/main.go" {
		t.Fatalf("fel text på verktygshändelsen: %q", handelser[1].Text)
	}
}

// En agent utan strömning ska inte lämna en tom händelsefil efter sig.
func TestIngenHandelsefilUtanStrom(t *testing.T) {
	dir := t.TempDir()
	logg := filepath.Join(dir, "k.log")
	skrivare, err := nyHandelseSkrivare(logg)
	if err != nil {
		t.Fatal(err)
	}
	agent := NewKommandoAgent("tyst", AgentKonfig{Kommando: "/bin/echo", Args: []string{"hej"}, Brief: "stdin", Svar: "stdout"})
	if _, err := agent.Kor(t.Context(), KorInput{Brief: "x", Logg: logg, VidHandelse: skrivare.Skriv}); err != nil {
		t.Fatal(err)
	}
	if err := skrivare.Stang(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(handelseSokvag(logg)); !os.IsNotExist(err) {
		t.Fatalf("händelsefilen skapades trots att agenten inte strömmar: %v", err)
	}
}

func TestSvarUrClaudeStromGerSvaretInteStrommen(t *testing.T) {
	strom := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Jag tittar på filen."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}}]}}`,
		`{"type":"result","is_error":false,"result":"Klart. Tavlan har tre kolumner."}`,
	}, "\n")
	svar := SvarUrClaudeStrom(strom)
	if svar != "Klart. Tavlan har tre kolumner." {
		t.Fatalf("fick %q", svar)
	}
	if strings.Contains(svar, "tool_use") || strings.Contains(svar, "\"type\"") {
		t.Fatalf("svaret bär råa strömrader: %q", svar)
	}
}

func TestSvarUrClaudeStromFallerTillbakaPaTexten(t *testing.T) {
	strom := `{"type":"assistant","message":{"content":[{"type":"text","text":"Enda svaret."}]}}`
	if svar := SvarUrClaudeStrom(strom); svar != "Enda svaret." {
		t.Fatalf("fick %q", svar)
	}
}

func TestModellUrClaudeStrom(t *testing.T) {
	strom := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-fable-5-1","cwd":"/tmp"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hej"}]}}`,
	}, "\n")
	if modell := ModellUrClaudeStrom(strom); modell != "claude-fable-5-1" {
		t.Fatalf("fick %q", modell)
	}
	if modell := ModellUrClaudeStrom(`{"type":"assistant","message":{"content":[]}}`); modell != "" {
		t.Fatalf("en ström utan modell gav %q", modell)
	}
}
