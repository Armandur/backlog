package mcpserver

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"

	"github.com/mazen160/backlog/internal/models"
)

// ToolDefinition beskriver ett verktyg som MCP-servern erbjuder.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolHandler hanterar ett anrop till ett tilläggsverktyg.
type ToolHandler func(context.Context, map[string]interface{}) (interface{}, error)

// Extension lägger till verktyg utan att ändra den vanliga Backlog-servern.
type Extension struct {
	Tools    []ToolDefinition
	Handlers map[string]ToolHandler
}

var defaultExtension struct {
	sync.RWMutex
	extension Extension
}

// SetExtension konfigurerar tillägg för den äldre Serve-funktionen.
func SetExtension(extension Extension) error {
	validated, err := validateExtension(extension)
	if err != nil {
		return err
	}
	defaultExtension.Lock()
	defaultExtension.extension = validated
	defaultExtension.Unlock()
	return nil
}

func currentExtension() Extension {
	defaultExtension.RLock()
	defer defaultExtension.RUnlock()
	return cloneExtension(defaultExtension.extension)
}

// Serve kör MCP-servern med det registrerade standardtillägget.
func Serve(db *sql.DB, actor models.Actor) {
	serve(db, actor, currentExtension())
}

// ServeWithExtension kör en MCP-server med ett eget tillägg.
func ServeWithExtension(db *sql.DB, actor models.Actor, extension Extension) error {
	validated, err := validateExtension(extension)
	if err != nil {
		return err
	}
	serve(db, actor, validated)
	return nil
}

func serve(db *sql.DB, actor models.Actor, extension Extension) {
	srv := &server{
		db:    db,
		actor: actor,
		r:     bufio.NewReader(os.Stdin),
		w:     os.Stdout,
		extra: extension,
	}
	srv.run()
}

func validateExtension(extension Extension) (Extension, error) {
	names := make(map[string]struct{})
	for _, definition := range tools() {
		names[definition.Name] = struct{}{}
	}
	for _, definition := range extension.Tools {
		if _, exists := names[definition.Name]; exists {
			return Extension{}, fmt.Errorf("verktygsnamnet %q är redan registrerat", definition.Name)
		}
		names[definition.Name] = struct{}{}
	}
	for name := range extension.Handlers {
		for _, definition := range tools() {
			if name == definition.Name {
				return Extension{}, fmt.Errorf("verktygsnamnet %q är redan registrerat", name)
			}
		}
	}
	return cloneExtension(extension), nil
}

func cloneExtension(extension Extension) Extension {
	cloned := Extension{
		Tools:    append([]ToolDefinition(nil), extension.Tools...),
		Handlers: make(map[string]ToolHandler, len(extension.Handlers)),
	}
	for name, handler := range extension.Handlers {
		cloned.Handlers[name] = handler
	}
	return cloned
}
