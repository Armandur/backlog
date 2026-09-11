package pm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mazen160/backlog/internal/timeutil"
)

// Handelse beskriver ett steg i en pågående agentkörning.
type Handelse struct {
	Tid  int64  `json:"tid"`
	Sort string `json:"sort"`
	Text string `json:"text"`
}

type handelseSkrivare struct {
	mu      sync.Mutex
	fil     *os.File
	buffert *bufio.Writer
	kodare  *json.Encoder
	fel     error
	stangd  bool
}

func nyHandelseSkrivare(logg string) (*handelseSkrivare, error) {
	fil, err := os.Create(handelseSokvag(logg))
	if err != nil {
		return nil, err
	}
	buffert := bufio.NewWriter(fil)
	return &handelseSkrivare{fil: fil, buffert: buffert, kodare: json.NewEncoder(buffert)}, nil
}

func handelseSokvag(logg string) string {
	andelse := filepath.Ext(logg)
	return strings.TrimSuffix(logg, andelse) + ".handelser.jsonl"
}

func (s *handelseSkrivare) Skriv(handelse Handelse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fel != nil || s.stangd {
		return
	}
	if err := s.kodare.Encode(handelse); err != nil {
		s.fel = err
		return
	}
	if err := s.buffert.Flush(); err != nil {
		s.fel = err
	}
}

func (s *handelseSkrivare) Stang() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stangd {
		return s.fel
	}
	s.stangd = true
	if err := s.buffert.Flush(); err != nil && s.fel == nil {
		s.fel = err
	}
	if err := s.fil.Close(); err != nil && s.fel == nil {
		s.fel = err
	}
	return s.fel
}

type claudeRad struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	Content json.RawMessage `json:"content"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
}

type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// tolkaClaudeRad omvandlar en stream-json-rad till noll eller flera händelser.
// Ett assistentmeddelande kan bära både text och verktygsanrop i samma rad, och
// då ska båda synas i vyn.
func tolkaClaudeRad(rad []byte) []Handelse {
	var post claudeRad
	if err := json.Unmarshal(rad, &post); err != nil {
		return []Handelse{nyHandelse("fel", "kunde inte tolka agentutdata som JSON")}
	}

	switch post.Type {
	case "assistant":
		innehall := post.Content
		if len(post.Message) > 0 {
			var meddelande struct {
				Content json.RawMessage `json:"content"`
			}
			if json.Unmarshal(post.Message, &meddelande) == nil && len(meddelande.Content) > 0 {
				innehall = meddelande.Content
			}
		}
		if len(innehall) == 0 && strings.TrimSpace(post.Text) != "" {
			return []Handelse{nyHandelse("text", post.Text)}
		}
		return tolkaClaudeInnehall(innehall)
	case "tool_use":
		return []Handelse{claudeVerktyg(post.Name, post.Input)}
	default:
		return nil
	}
}

func tolkaClaudeInnehall(ratt json.RawMessage) []Handelse {
	if len(ratt) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(ratt, &text) == nil {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []Handelse{nyHandelse("text", text)}
	}
	var block []claudeBlock
	if json.Unmarshal(ratt, &block) != nil {
		return nil
	}
	var ut []Handelse
	for _, del := range block {
		switch del.Type {
		case "text":
			if strings.TrimSpace(del.Text) != "" {
				ut = append(ut, nyHandelse("text", del.Text))
			}
		case "tool_use":
			ut = append(ut, claudeVerktyg(del.Name, del.Input))
		}
	}
	return ut
}

func claudeVerktyg(namn string, ratt json.RawMessage) Handelse {
	var indata map[string]json.RawMessage
	_ = json.Unmarshal(ratt, &indata)
	if sokvag := forstaText(indata, "file_path", "path", "notebook_path"); sokvag != "" {
		verb := "hanterade"
		switch strings.ToLower(namn) {
		case "read", "ls":
			verb = "läste"
		case "write", "edit", "multiedit", "notebookedit":
			verb = "ändrade"
		case "glob", "grep":
			verb = "sökte i"
		}
		return nyHandelse("fil", verb+" "+sokvag)
	}
	if kommando := forstaText(indata, "command", "cmd"); kommando != "" {
		return nyHandelse("kommando", "körde "+kommando)
	}
	if strings.TrimSpace(namn) == "" {
		namn = "okänt verktyg"
	}
	return nyHandelse("verktyg", "anropade "+namn)
}

func forstaText(indata map[string]json.RawMessage, nycklar ...string) string {
	for _, nyckel := range nycklar {
		var varde string
		if json.Unmarshal(indata[nyckel], &varde) == nil && strings.TrimSpace(varde) != "" {
			return varde
		}
	}
	return ""
}

func nyHandelse(sort, text string) Handelse {
	return Handelse{Tid: timeutil.Now(), Sort: sort, Text: kortaHandelsetext(text)}
}

func kortaHandelsetext(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	tecken := []rune(text)
	if len(tecken) <= 200 {
		return text
	}
	langd := 200
	for {
		dolda := len(tecken) - langd
		slut := fmt.Sprintf("... %d tecken till", dolda)
		nyLangd := 200 - len([]rune(slut))
		if nyLangd < 0 {
			nyLangd = 0
		}
		if nyLangd == langd {
			return string(tecken[:langd]) + slut
		}
		langd = nyLangd
	}
}
