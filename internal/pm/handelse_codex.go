package pm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Codex format utvecklas oberoende av övriga agentströmmar, så dess
// avkodning hålls samlad här för att begränsa framtida formatändringar.
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
