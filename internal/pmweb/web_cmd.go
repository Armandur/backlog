package pmweb

import (
	"fmt"
	"net"
	"net/http"
	"os"
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
			srv := New(cli.DB(), cli.CurrentActor(), reg)
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
