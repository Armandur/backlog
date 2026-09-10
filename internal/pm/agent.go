package pm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Agent är adaptergränssnittet mot en modell. Utdelaren i delsteg 3 bygger
// vidare på det - frågeflödet får inte anta att just Claude finns.
type Agent interface {
	// Namn används som aktörsnamn på svaret, t.ex. ai:claude-opus-5.
	Namn() string
	// Fraga skickar en färdig prompt och ger svaret som text.
	Fraga(ctx context.Context, prompt string) (string, error)
}

// AgentRegister håller de agenter binären känner till.
type AgentRegister struct {
	agenter map[string]Agent
	forval  string
}

func NewAgentRegister() *AgentRegister {
	return &AgentRegister{agenter: map[string]Agent{}}
}

// Registrera lägger till en agent. Den första blir förval.
func (r *AgentRegister) Registrera(a Agent) {
	if r.agenter == nil {
		r.agenter = map[string]Agent{}
	}
	r.agenter[a.Namn()] = a
	if r.forval == "" {
		r.forval = a.Namn()
	}
}

// SattForval pekar ut vilken agent som används utan --agent.
func (r *AgentRegister) SattForval(namn string) { r.forval = namn }

// Hamta ger agenten med namnet, eller förvalet om namnet är tomt.
func (r *AgentRegister) Hamta(namn string) (Agent, error) {
	if namn == "" {
		namn = r.forval
	}
	if namn == "" {
		return nil, fmt.Errorf("ingen agent är registrerad")
	}
	a, ok := r.agenter[namn]
	if !ok {
		return nil, fmt.Errorf("agenten %q finns inte. Kända agenter: %s", namn, strings.Join(r.Namn(), ", "))
	}
	return a, nil
}

// Namn listar registrerade agenter i bokstavsordning.
func (r *AgentRegister) Namn() []string {
	namn := make([]string, 0, len(r.agenter))
	for n := range r.agenter {
		namn = append(namn, n)
	}
	sort.Strings(namn)
	return namn
}

// ClaudeAgent kör Claude headless (claude -p) och ger den PM-workspacet som
// MCP-server, så agenten kan slå upp mer än det som ryms i prompten.
type ClaudeAgent struct {
	Binar     string // claude, kan pekas om i test
	Modell    string // aktörsnamnet på svaret
	PMBinar   string // sökväg till backlog-pm för mcp-servern
	Profil    string // profilen mcp-servern ska köra mot
	MCPConfig string // färdig config-fil, annars skrivs en temporär
}

func NewClaudeAgent(pmBinar, profil string) *ClaudeAgent {
	return &ClaudeAgent{Binar: "claude", Modell: "claude-opus-5", PMBinar: pmBinar, Profil: profil}
}

func (c *ClaudeAgent) Namn() string {
	if c.Modell == "" {
		return "claude"
	}
	return c.Modell
}

func (c *ClaudeAgent) Fraga(ctx context.Context, prompt string) (string, error) {
	binar := c.Binar
	if binar == "" {
		binar = "claude"
	}
	args := []string{"-p", prompt}
	if cfg, cleanup, err := c.mcpConfig(); err == nil && cfg != "" {
		defer cleanup()
		args = append(args, "--mcp-config", cfg)
	}

	cmd := exec.CommandContext(ctx, binar, args...)
	cmd.Stdin = nil
	ut, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return "", fmt.Errorf("agenten %s svarade inte: %v: %s", c.Namn(), err, stderr)
		}
		return "", fmt.Errorf("agenten %s svarade inte: %w", c.Namn(), err)
	}
	svar := strings.TrimSpace(string(ut))
	if svar == "" {
		return "", fmt.Errorf("agenten %s gav ett tomt svar", c.Namn())
	}
	return svar, nil
}

// mcpConfig skriver en tillfällig MCP-config som pekar på PM-workspacet.
func (c *ClaudeAgent) mcpConfig() (string, func(), error) {
	if c.MCPConfig != "" {
		return c.MCPConfig, func() {}, nil
	}
	if c.PMBinar == "" || c.Profil == "" {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp("", "backlog-pm-mcp-*.json")
	if err != nil {
		return "", func() {}, err
	}
	cfg := fmt.Sprintf(`{"mcpServers":{"backlog-pm":{"command":%q,"args":["--profile",%q,"mcp","serve"]}}}`,
		c.PMBinar, c.Profil)
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// AgentBinar ger sökvägen till den körande binären, för MCP-configen.
func AgentBinar() string {
	exe, err := os.Executable()
	if err != nil {
		return "backlog-pm"
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}
