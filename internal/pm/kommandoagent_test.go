package pm

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
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
