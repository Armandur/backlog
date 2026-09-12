package pm

import (
	"context"
	"fmt"

	"github.com/mazen160/backlog/internal/mcpserver"
)

// TestserverMCP bygger PM:s MCP-verktyg för testservern.
func TestserverMCP(nyStore func() (*TestserverStore, error)) mcpserver.Extension {
	handler := func(
		anrop func(context.Context, *TestserverStore, string) (*Testserver, error),
		kraverKonfig bool,
	) mcpserver.ToolHandler {
		return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			alias, _ := args["project"].(string)
			if alias == "" {
				return nil, fmt.Errorf("projektalias saknas")
			}
			store, err := nyStore()
			if err != nil {
				return nil, err
			}
			if _, finns := store.konfig.Testserver[alias]; kraverKonfig && !finns {
				return nil, fmt.Errorf("projektet %q saknar konfiguration för testserver", alias)
			}
			server, err := anrop(ctx, store, alias)
			if err != nil {
				return nil, err
			}
			return mcpserver.TextResult(server), nil
		}
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project": map[string]string{"type": "string", "description": "project alias"},
		},
		"required": []string{"project"},
	}
	return mcpserver.Extension{
		Tools: []mcpserver.ToolDefinition{
			{Name: "testserver_start", Description: "Start a project test server managed by PM", InputSchema: schema},
			{Name: "testserver_stop", Description: "Stop a project test server managed by PM", InputSchema: schema},
			{Name: "testserver_status", Description: "Show a project test server status", InputSchema: schema},
		},
		Handlers: map[string]mcpserver.ToolHandler{
			"testserver_start": handler(func(ctx context.Context, store *TestserverStore, alias string) (*Testserver, error) {
				return store.Starta(ctx, alias)
			}, true),
			"testserver_stop": handler(func(ctx context.Context, store *TestserverStore, alias string) (*Testserver, error) {
				return store.Stoppa(ctx, alias)
			}, true),
			"testserver_status": handler(func(ctx context.Context, store *TestserverStore, alias string) (*Testserver, error) {
				return store.Status(ctx, alias)
			}, false),
		},
	}
}
