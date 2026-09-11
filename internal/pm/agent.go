package pm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mazen160/backlog/internal/cli"
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

// Forval ger namnet på agenten som svarar när inget namn anges.
func (r *AgentRegister) Forval() string { return r.forval }

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

// FranKonfig bygger registret ur konfigurationen. Alla agenter är
// kommandoagenter, så en tredje agent läggs till i pm.toml utan kodändring.
func FranKonfig(k Konfig) *AgentRegister {
	reg := NewAgentRegister()
	namn := make([]string, 0, len(k.Agenter))
	for n := range k.Agenter {
		namn = append(namn, n)
	}
	sort.Strings(namn)
	for _, n := range namn {
		reg.Registrera(NewKommandoAgent(n, k.Agenter[n]))
	}
	if k.DefaultAgent != "" {
		reg.SattForval(k.DefaultAgent)
	}
	return reg
}

// HamtaKorare ger agenten som körare, för utdelaren.
func (r *AgentRegister) HamtaKorare(namn string) (Korare, error) {
	a, err := r.Hamta(namn)
	if err != nil {
		return nil, err
	}
	k, ok := a.(Korare)
	if !ok {
		return nil, fmt.Errorf("agenten %q kan inte köra tasks, bara svara i samtal", a.Namn())
	}
	return k, nil
}

// RegistreraKorare lägger in en körare i registret. Används av test.
func (r *AgentRegister) RegistreraKorare(k Korare) {
	if a, ok := k.(Agent); ok {
		r.Registrera(a)
	}
}

// PMBinar ger sökvägen till den körande binären, för MCP-configen.
func PMBinar() string {
	exe, err := os.Executable()
	if err != nil {
		return "backlog-pm"
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// RegisterFranProfil bygger agentregistret ur profilens pm.toml när ett
// kommando körs. Ändras konfigfilen behövs ingen ombyggnad.
func RegisterFranProfil() (*AgentRegister, error) {
	k, err := LasKonfig(cli.WorkDir())
	if err != nil {
		return nil, err
	}
	return FranKonfig(k), nil
}
