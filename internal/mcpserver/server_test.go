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

func TestExtensionAvvisarInbyggtVerktygsnamn(t *testing.T) {
	t.Cleanup(func() {
		if err := SetExtension(Extension{}); err != nil {
			t.Fatalf("återställ tillägg: %v", err)
		}
	})
	if err := SetExtension(Extension{
		Tools: []ToolDefinition{{Name: "project_list", InputSchema: props()}},
		Handlers: map[string]ToolHandler{
			"project_list": func(context.Context, map[string]interface{}) (interface{}, error) {
				return TextResult("fel hanterare"), nil
			},
		},
	}); err == nil {
		t.Fatal("namnkonflikten godkändes")
	}

	var ut bytes.Buffer
	srv := &server{w: &ut, extra: currentExtension()}
	srv.handle(message{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	var svar struct {
		Result struct {
			Tools []ToolDefinition `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(ut.Bytes(), &svar); err != nil {
		t.Fatalf("tolka tools/list: %v", err)
	}
	antal := 0
	for _, verktyg := range svar.Result.Tools {
		if verktyg.Name == "project_list" {
			antal++
		}
	}
	if antal != 1 {
		t.Fatalf("project_list förekommer %d gånger", antal)
	}
}

func TestServerSvararEfterPanikIHanterare(t *testing.T) {
	extra, err := validateExtension(Extension{
		Tools: []ToolDefinition{{Name: "panik", InputSchema: props()}},
		Handlers: map[string]ToolHandler{
			"panik": func(context.Context, map[string]interface{}) (interface{}, error) {
				panic("testpanik")
			},
		},
	})
	if err != nil {
		t.Fatalf("validera tillägg: %v", err)
	}
	var ut bytes.Buffer
	srv := &server{w: &ut, extra: extra}
	params, _ := json.Marshal(map[string]interface{}{"name": "panik"})
	srv.handle(message{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
	srv.handle(message{JSONRPC: "2.0", ID: 2, Method: "tools/list"})

	decoder := json.NewDecoder(&ut)
	var felsvar message
	if err := decoder.Decode(&felsvar); err != nil {
		t.Fatalf("tolka felsvar: %v", err)
	}
	if felsvar.Error == nil || felsvar.Error.Code != -32603 {
		t.Fatalf("paniken gav inte JSON-RPC-felet -32603: %#v", felsvar.Error)
	}
	var listsvar message
	if err := decoder.Decode(&listsvar); err != nil {
		t.Fatalf("servern svarade inte efter paniken: %v", err)
	}
	if listsvar.ID != float64(2) && listsvar.ID != 2 {
		t.Fatalf("oväntat svar efter paniken: %#v", listsvar.ID)
	}
}
