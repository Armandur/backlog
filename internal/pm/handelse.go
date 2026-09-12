package pm

import (
	"bufio"
	"bytes"
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
	sokvag  string
	mu      sync.Mutex
	fil     *os.File
	buffert *bufio.Writer
	kodare  *json.Encoder
	fel     error
	stangd  bool
}

// nyHandelseSkrivare skapar filen först när en händelse faktiskt skrivs. En
// agent utan strömning ska inte lämna en tom fil efter sig.
func nyHandelseSkrivare(logg string) (*handelseSkrivare, error) {
	return &handelseSkrivare{sokvag: handelseSokvag(logg)}, nil
}

func (s *handelseSkrivare) oppna() error {
	if s.fil != nil {
		return nil
	}
	fil, err := os.Create(s.sokvag)
	if err != nil {
		return err
	}
	s.fil = fil
	s.buffert = bufio.NewWriter(fil)
	s.kodare = json.NewEncoder(s.buffert)
	return nil
}

func handelseSokvag(logg string) string {
	andelse := filepath.Ext(logg)
	return strings.TrimSuffix(logg, andelse) + ".handelser.jsonl"
}

// HandelseSokvag ger sökvägen till händelsefilen bredvid en körningslogg.
func HandelseSokvag(logg string) string { return handelseSokvag(logg) }

func (s *handelseSkrivare) Skriv(handelse Handelse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fel != nil || s.stangd {
		return
	}
	if err := s.oppna(); err != nil {
		s.fel = err
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
	if s.fil == nil {
		return s.fel
	}
	if err := s.buffert.Flush(); err != nil && s.fel == nil {
		s.fel = err
	}
	if err := s.fil.Close(); err != nil && s.fel == nil {
		s.fel = err
	}
	return s.fel
}

type codexRad struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	Error   json.RawMessage `json:"error"`
	Item    json.RawMessage `json:"item"`
}

type codexPost struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Message  json.RawMessage `json:"message"`
	Command  string          `json:"command"`
	Changes  []codexAndring  `json:"changes"`
	ExitCode *int            `json:"exit_code"`
}

type codexAndring struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// tolkaCodexRad omvandlar en JSON-rad från codex exec till PM-händelser.
// Okända typer ignoreras så att nya Codex-versioner inte stoppar körningen.
func tolkaCodexRad(rad []byte) []Handelse {
	if len(bytes.TrimSpace(rad)) == 0 {
		return nil
	}
	var post codexRad
	if err := json.Unmarshal(rad, &post); err != nil {
		return []Handelse{nyHandelse("fel", "kunde inte tolka agentutdata som JSON")}
	}

	switch post.Type {
	case "item.started", "item.completed":
		var item codexPost
		if json.Unmarshal(post.Item, &item) != nil {
			return nil
		}
		if post.Type == "item.started" {
			if item.Type == "command_execution" && strings.TrimSpace(item.Command) != "" {
				return []Handelse{nyHandelse("kommando", "körde "+item.Command)}
			}
			return nil
		}
		switch item.Type {
		case "agent_message":
			if strings.TrimSpace(item.Text) != "" {
				return []Handelse{nyHandelse("text", item.Text)}
			}
		case "file_change":
			return codexFilandringar(item.Changes)
		case "command_execution":
			if item.ExitCode != nil && *item.ExitCode != 0 {
				text := fmt.Sprintf("kommandot misslyckades med exitkod %d", *item.ExitCode)
				return []Handelse{nyHandelse("fel", text)}
			}
		case "error":
			text := codexFeltext(item.Message)
			if text == "" {
				text = "Codex rapporterade ett fel"
			}
			return []Handelse{nyHandelse("fel", text)}
		}
	case "error":
		text := codexFeltext(post.Message)
		if text == "" {
			text = "Codex rapporterade ett fel"
		}
		return []Handelse{nyHandelse("fel", text)}
	case "turn.failed":
		text := codexFeltext(post.Error)
		if text == "" {
			text = "Codex-körningen misslyckades"
		}
		return []Handelse{nyHandelse("fel", text)}
	}
	return nil
}

func codexFeltext(ratt json.RawMessage) string {
	var text string
	if json.Unmarshal(ratt, &text) == nil {
		text = strings.TrimSpace(text)
		if json.Valid([]byte(text)) {
			if inbaddad := codexFeltext(json.RawMessage(text)); inbaddad != "" {
				return inbaddad
			}
		}
		return text
	}
	var fel struct {
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(ratt, &fel) == nil {
		if text := codexFeltext(fel.Message); text != "" {
			return text
		}
		return codexFeltext(fel.Error)
	}
	return ""
}

func codexFilandringar(andringar []codexAndring) []Handelse {
	ut := make([]Handelse, 0, len(andringar))
	for _, andring := range andringar {
		sokvag := strings.TrimSpace(andring.Path)
		if sokvag == "" {
			continue
		}
		verb := "ändrade"
		switch andring.Kind {
		case "add":
			verb = "skapade"
		case "delete":
			verb = "tog bort"
		}
		ut = append(ut, nyHandelse("fil", verb+" "+sokvag))
	}
	return ut
}

type claudeRad struct {
	Type    string          `json:"type"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
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
	if len(bytes.TrimSpace(rad)) == 0 {
		return nil
	}
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
	case "user":
		// Ett verktygssvar säger om anropet lyckades. Bara felen är värda en rad.
		return claudeVerktygssvar(post)
	case "result":
		if post.IsError {
			text := strings.TrimSpace(post.Result)
			if text == "" {
				text = "agenten avslutade med fel"
			}
			return []Handelse{nyHandelse("fel", text)}
		}
		return []Handelse{nyHandelse("text", "agenten är klar")}
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

// claudeVerktygssvar plockar ut misslyckade verktygsanrop ur ett user-meddelande.
func claudeVerktygssvar(post claudeRad) []Handelse {
	innehall := post.Content
	if len(post.Message) > 0 {
		var meddelande struct {
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(post.Message, &meddelande) == nil && len(meddelande.Content) > 0 {
			innehall = meddelande.Content
		}
	}
	var block []struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
		Content any    `json:"content"`
	}
	if json.Unmarshal(innehall, &block) != nil {
		return nil
	}
	var ut []Handelse
	for _, del := range block {
		if del.Type == "tool_result" && del.IsError {
			ut = append(ut, nyHandelse("fel", "ett verktygsanrop misslyckades: "+fmt.Sprint(del.Content)))
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

// SkrivAvbrottshandelse lägger en sista rad i körningens händelsefil när
// städningen hittar en körning vars process är borta. Då ser även den som
// ansluter långt efteråt varför körningen tog slut.
func SkrivAvbrottshandelse(logg string) {
	if logg == "" {
		return
	}
	fil, err := os.OpenFile(HandelseSokvag(logg), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fil.Close()
	data, err := json.Marshal(nyHandelse("fel", "körningen avbröts, processen finns inte längre"))
	if err != nil {
		return
	}
	_, _ = fil.Write(append(data, '\n'))
}

// SvarUrClaudeStrom plockar agentens svar ur en stream-json-utdata. Utan det
// hamnar hela strömmen som kommentar på tasken, och den är oläslig.
func SvarUrClaudeStrom(utdata string) string {
	var resultat string
	var texter []string
	for _, rad := range strings.Split(utdata, "\n") {
		rad = strings.TrimSpace(rad)
		if rad == "" {
			continue
		}
		var post claudeRad
		if json.Unmarshal([]byte(rad), &post) != nil {
			continue
		}
		switch post.Type {
		case "result":
			if strings.TrimSpace(post.Result) != "" {
				resultat = post.Result
			}
		case "assistant":
			for _, del := range claudeTextblock(post) {
				texter = append(texter, del)
			}
		}
	}
	if strings.TrimSpace(resultat) != "" {
		return resultat
	}
	return strings.TrimSpace(strings.Join(texter, "\n\n"))
}

// claudeTextblock ger textdelarna i ett assistentmeddelande.
func claudeTextblock(post claudeRad) []string {
	innehall := post.Content
	if len(post.Message) > 0 {
		var meddelande struct {
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(post.Message, &meddelande) == nil && len(meddelande.Content) > 0 {
			innehall = meddelande.Content
		}
	}
	var block []claudeBlock
	if json.Unmarshal(innehall, &block) != nil {
		return nil
	}
	var ut []string
	for _, del := range block {
		if del.Type == "text" && strings.TrimSpace(del.Text) != "" {
			ut = append(ut, strings.TrimSpace(del.Text))
		}
	}
	return ut
}

// ModellUrClaudeStrom ger modellen agenten faktiskt körde med. Claude skriver
// den i sin init-rad. Namnet i konfigurationen säger bara vilken agent som
// startades, inte vilken modell som svarade.
func ModellUrClaudeStrom(utdata string) string {
	for _, rad := range strings.Split(utdata, "\n") {
		rad = strings.TrimSpace(rad)
		if rad == "" || !strings.Contains(rad, `"model"`) {
			continue
		}
		var post struct {
			Type   string `json:"type"`
			Modell string `json:"model"`
		}
		if json.Unmarshal([]byte(rad), &post) != nil {
			continue
		}
		if post.Type == "system" && strings.TrimSpace(post.Modell) != "" {
			return post.Modell
		}
	}
	return ""
}
