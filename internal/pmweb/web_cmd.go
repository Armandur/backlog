package pmweb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/pm"
)

// NewWebCmd ersätter upstreams web-kommando med PM-webben: samma UI plus
// projektsamtalen på /pm/<alias>.
func NewWebCmd(hamtaRegister func() (*pm.AgentRegister, error)) *cobra.Command {
	var port int
	var bind string

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Starta PM-webben (backlog-UI plus projektsamtal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := hamtaRegister()
			if err != nil {
				return err
			}
			if _, err := pm.LasKonfig(cli.WorkDir()); err != nil {
				return err
			}
			workspace := cli.WorkDir()
			profil := profilNamn()
			srv := New(cli.DB(), cli.CurrentActor(), reg).MedUtdelare(
				func(taskID, agent string) string {
					// Körningen lever längre än HTTP-anropet.
					go func() {
						// Konfigurationen läses vid varje utdelning, så en agent
						// som lagts till i konfigvyn fungerar utan omstart.
						konfig, err := pm.LasKonfig(workspace)
						if err != nil {
							fmt.Fprintf(os.Stderr, "kunde inte läsa konfigurationen: %v\n", err)
							return
						}
						utdelare := pm.NewUtdelare(cli.DB(), konfig, pm.FranKonfig(konfig))
						_, err = utdelare.DelaUt(context.Background(), pm.UtdelInput{
							TaskID: taskID, Overstyrning: agent,
							WorkspaceDir: workspace, Profil: profil, PMBinar: pm.PMBinar(),
						})
						if err != nil {
							fmt.Fprintf(os.Stderr, "utdelning misslyckades: %v\n", err)
						}
					}()
					return "körningen startad, följ den under pågående körningar"
				})
			addr := net.JoinHostPort(bind, fmt.Sprintf("%d", port))
			vard, felVard := os.Hostname()
			if felVard != nil || vard == "" {
				vard = "ubuntu-ai"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "PM-webb: http://%s:%d/  tråd: http://%s:%d/pm/<alias>\n", vard, port, vard, port)

			httpSrv := &http.Server{
				Addr:              addr,
				Handler:           srv,
				ReadHeaderTimeout: 10 * time.Second,
			}
			return httpSrv.ListenAndServe()
		},
	}
	cmd.Flags().IntVar(&port, "port", 6060, "port att lyssna på")
	cmd.Flags().StringVar(&bind, "bind", "", "adress att binda till, tom betyder alla gränssnitt")
	return cmd
}

// profilNamn läser --profile ur argumenten, så körningar och MCP pekar på
// samma workspace som webben.
func profilNamn() string {
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
