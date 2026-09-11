package pm

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMcpConfigFilBarAgentensAktor(t *testing.T) {
	fil, stad, err := mcpConfigFil("/usr/bin/backlog-pm", "pm", "ai:claude")
	if err != nil {
		t.Fatal(err)
	}
	defer stad()
	data, err := os.ReadFile(fil)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("konfigen är inte giltig JSON: %v", err)
	}
	args := cfg.MCPServers["backlog-pm"].Args
	if !slices.Contains(args, "--as") {
		t.Fatalf("konfigen saknar --as, agenten skriver då i användarens namn: %v", args)
	}
	i := slices.Index(args, "--as")
	if i+1 >= len(args) || args[i+1] != "ai:claude" {
		t.Fatalf("fel aktör i konfigen: %v", args)
	}
}

func TestHandelsefilSkrivsRadForRad(t *testing.T) {
	logg := filepath.Join(t.TempDir(), "korning.log")
	skrivare, err := nyHandelseSkrivare(logg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skrivare.Stang() })

	forsta := `{"type":"assistant","message":{"content":[{"type":"text","text":"första raden"}]}}`
	andra := `{"type":"assistant","message":{"content":[{"type":"text","text":"andra raden"}]}}`
	kommando := "printf '%s\\n' '" + forsta + "'\nsleep 1\nprintf '%s\\n' '" + andra + "'"
	agent := NewKommandoAgent("test", AgentKonfig{
		Kommando: "/bin/sh",
		Args:     []string{"-c", kommando},
		Brief:    "stdin",
		Svar:     "stdout",
		Strom:    "claude-json",
	})

	type resultat struct {
		res Resultat
		fel error
	}
	klart := make(chan resultat, 1)
	go func() {
		res, fel := agent.Kor(t.Context(), KorInput{Logg: logg, VidHandelse: skrivare.Skriv})
		klart <- resultat{res: res, fel: fel}
	}()

	handelsefil := handelseSokvag(logg)
	var underKorning []byte
	slut := time.Now().Add(2 * time.Second)
	for time.Now().Before(slut) {
		underKorning, _ = os.ReadFile(handelsefil)
		if bytes.Contains(underKorning, []byte("första raden")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !bytes.Contains(underKorning, []byte("första raden")) {
		t.Fatal("händelseloggen visar inte den första raden under körningen")
	}
	if bytes.Contains(underKorning, []byte("andra raden")) {
		t.Fatal("andra händelsen kom före skalets paus")
	}

	utfall := <-klart
	if utfall.fel != nil || utfall.res.ExitKod != 0 {
		t.Fatalf("agentkörningen misslyckades: %+v, %v", utfall.res, utfall.fel)
	}
	if err := skrivare.Stang(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(handelsefil)
	if err != nil {
		t.Fatal(err)
	}
	rader := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(rader) != 2 {
		t.Fatalf("händelseloggen har %d rader, vill ha 2: %s", len(rader), data)
	}
	for _, rad := range rader {
		var handelse Handelse
		if err := json.Unmarshal([]byte(rad), &handelse); err != nil {
			t.Fatalf("ogiltig JSONL-rad %q: %v", rad, err)
		}
	}
}

func TestKonfigGodtarBadaStromformaten(t *testing.T) {
	for _, format := range []string{"claude-json", "codex-json"} {
		k := Konfig{DefaultAgent: "test", Agenter: map[string]AgentKonfig{
			"test": {Kommando: "true", Args: []string{"{brief}"}, Brief: "arg", Svar: "stdout", Strom: format},
		}}
		if err := k.Validera(); err != nil {
			t.Fatalf("Validera avvisade strömformatet %q: %v", format, err)
		}
	}
}

func TestKorBevararUtdataUtanRadbrytning(t *testing.T) {
	agent := NewKommandoAgent("test", AgentKonfig{
		Kommando: "/bin/sh",
		Args:     []string{"-c", "printf exakt"},
		Brief:    "stdin",
		Svar:     "stdout",
	})
	res, err := agent.Kor(t.Context(), KorInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Utdata != "exakt" {
		t.Fatalf("utdata blev %q, vill ha exakt innehåll", res.Utdata)
	}
}
