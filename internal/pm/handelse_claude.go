package pm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Claude kan bära samma innehåll i flera stream-json-former, så all
// normalisering av formatet hålls samlad här.
type claudeRad struct {
	Type    string          `json:"type"`
	Usage   json.RawMessage `json:"usage"`
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
	case "rate_limit_event":
		// Kvotläget plockas ur den samlade utdatan efteråt, som modellen.
		// Förloppsvyn blir inte begripligare av en rad om kvoten.
		return nil
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
		handelser := tolkaClaudeInnehall(innehall)
		// Tokensiffran räknar bara nya tokens, alltså in och ut. Läsning ur
		// cachen kostar inget nytt och skulle blåsa upp talet.
		if tokens := claudeNyaTokens(post.Usage, post.Message); tokens > 0 {
			handelser = append(handelser, nyHandelse("tokens", strconv.Itoa(tokens)))
		}
		return handelser
	case "tool_use":
		return []Handelse{claudeVerktyg(post.Name, post.Input)}
	case "user":
		// Ett verktygssvar säger om anropet lyckades. Bara felen är värda en rad.
		return claudeVerktygssvar(post)
	case "result":
		// Resultatraden bär hela körningens summa, inte stegets. Den ersätter
		// det klienten räknat ihop, annars dubbelräknas varje steg.
		if tokens := claudeNyaTokens(post.Usage, post.Message); tokens > 0 {
			handelser := []Handelse{nyHandelse("tokens_total", strconv.Itoa(tokens))}
			if post.IsError {
				text := strings.TrimSpace(post.Result)
				if text == "" {
					text = "agenten avslutade med fel"
				}
				return append(handelser, nyHandelse("fel", text))
			}
			return append(handelser, nyHandelse("text", "agenten är klar"))
		}
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

// claudeNyaTokens summerar in- och utgående tokens för ett steg. Talet finns
// antingen direkt på raden eller inne i message.
func claudeNyaTokens(usage, meddelande json.RawMessage) int {
	las := func(rat json.RawMessage) int {
		if len(rat) == 0 {
			return 0
		}
		var u struct {
			In  int `json:"input_tokens"`
			Ut  int `json:"output_tokens"`
			Nyc int `json:"cache_creation_input_tokens"`
		}
		if json.Unmarshal(rat, &u) != nil {
			return 0
		}
		return u.In + u.Ut + u.Nyc
	}
	if tokens := las(usage); tokens > 0 {
		return tokens
	}
	if len(meddelande) == 0 {
		return 0
	}
	var m struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(meddelande, &m) != nil {
		return 0
	}
	return las(m.Usage)
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
