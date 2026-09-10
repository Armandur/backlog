// backlog-pm är PM-lagret ovanpå backlog. Samma CLI som backlog, men med en
// spärr som hindrar körning mot vardagsdatabasen (profilen default).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/pmweb"
)

// profilFlagga läser --profile ur argumenten, så MCP-servern agenten startar
// pekar på samma workspace som körningen.
func profilFlagga() string {
	for i, a := range os.Args {
		if a == "--profile" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(a, "--profile=") {
			return strings.TrimPrefix(a, "--profile=")
		}
	}
	return ""
}

// version sätts vid bygget via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	cli.SetVersion(version)
	cli.SetGuard(pm.Guard)
	cli.SetPostOpen(pm.Migrate)

	register := pm.NewAgentRegister()
	register.Registrera(pm.NewClaudeAgent(pm.AgentBinar(), profilFlagga()))

	extra := []*cobra.Command{pm.NewPMCmd(), pm.NewSamtalCmd(register), pmweb.NewWebCmd(register)}
	if err := cli.ExecuteRoot("backlog-pm", extra...); err != nil {
		fmt.Fprintln(os.Stderr, "fel:", err)
		os.Exit(1)
	}
}
