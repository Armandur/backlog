package pm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
