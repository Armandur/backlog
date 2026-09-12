package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestStandardverktygenArOförandrade(t *testing.T) {
	var ut bytes.Buffer
	srv := &server{w: &ut}
	srv.handle(message{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	var svar struct {
		Result struct {
			Tools []ToolDefinition `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(ut.Bytes(), &svar); err != nil {
		t.Fatalf("tolka tools/list: %v", err)
	}
	var namn []string
	for _, verktyg := range svar.Result.Tools {
		namn = append(namn, verktyg.Name)
	}
	vantade := []string{
		"project_list",
		"task_create",
		"task_list",
		"task_show",
		"task_update",
		"task_move",
		"plan_add",
		"plan_update",
		"plan_history",
		"comment_add",
		"memory_add",
		"memory_list",
		"doc_add",
		"doc_list",
		"doc_show",
		"doc_update",
	}
	if !reflect.DeepEqual(namn, vantade) {
		t.Fatalf("vanliga backlog fick en ändrad verktygslista:\n%v", namn)
	}
}

func TestServerLaggerTillOchAnroparExtension(t *testing.T) {
	anropad := false
	extra := Extension{
		Tools: []ToolDefinition{{Name: "extra", Description: "Extra tool", InputSchema: props()}},
		Handlers: map[string]ToolHandler{
			"extra": func(_ context.Context, args map[string]interface{}) (interface{}, error) {
				anropad = args["value"] == "ok"
				return TextResult(map[string]bool{"ok": true}), nil
			},
		},
	}
	var ut bytes.Buffer
	srv := &server{w: &ut, extra: extra}
	srv.handle(message{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	var listSvar struct {
		Result struct {
			Tools []ToolDefinition `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(ut.Bytes(), &listSvar); err != nil {
		t.Fatalf("tolka tools/list: %v", err)
	}
	if sista := listSvar.Result.Tools[len(listSvar.Result.Tools)-1].Name; sista != "extra" {
		t.Fatalf("extensionverktyget saknas, sista verktyget är %q", sista)
	}

	params, _ := json.Marshal(map[string]interface{}{
		"name": "extra", "arguments": map[string]interface{}{"value": "ok"},
	})
	if _, err := srv.callTool(context.Background(), params); err != nil {
		t.Fatalf("anropa extensionverktyg: %v", err)
	}
	if !anropad {
		t.Fatal("extensionhanteraren fick inte argumenten")
	}
}
