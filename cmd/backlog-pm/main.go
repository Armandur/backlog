// backlog-pm är PM-lagret ovanpå backlog. Samma CLI som backlog, men med en
// spärr som hindrar körning mot vardagsdatabasen (profilen default).
package main

import (
	"fmt"
	"os"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/pm"
)

// version sätts vid bygget via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	cli.SetVersion(version)
	cli.SetGuard(pm.Guard)
	if err := cli.ExecuteRoot("backlog-pm", pm.NewPMCmd()); err != nil {
		fmt.Fprintln(os.Stderr, "fel:", err)
		os.Exit(1)
	}
}
