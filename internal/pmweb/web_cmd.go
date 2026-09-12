package pmweb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/pm"
)

const (
	standardWebbport = 6060
	// PM provar 6060 och de tjugo portarna efter den innan det blir fel.
	webbportSpann = 20
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
			// En körning som dog med förra servern ska inte se ut att pågå.
			if antal, err := pm.NewKorningStore(cli.DB()).StadaOvergivna(cmd.Context()); err != nil {
				fmt.Fprintf(os.Stderr, "kunde inte städa övergivna körningar: %v\n", err)
			} else if antal > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Städade %d övergiven körning som saknade process.\n", antal)
			}
			srv := New(cli.DB(), cli.CurrentActor(), reg).MedUtdelare(
				func(taskID, agent, modell, anstrangning string) string {
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
							Modell: modell, Anstrangning: anstrangning,
							WorkspaceDir: workspace, Profil: profil, PMBinar: pm.PMBinar(),
						})
						if err != nil {
							fmt.Fprintf(os.Stderr, "utdelning misslyckades: %v\n", err)
						}
					}()
					return "körningen startad, följ den under pågående körningar"
				})
			lyssnare, port, err := lyssna(bind, port, cmd.Flags().Changed("port"))
			if err != nil {
				return err
			}
			vard, felVard := os.Hostname()
			if felVard != nil || vard == "" {
				vard = "localhost"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "PM-webb: http://%s:%d/  tråd: http://%s:%d/pm/<alias>\n", vard, port, vard, port)

			httpSrv := &http.Server{
				Handler:           srv,
				ReadHeaderTimeout: 10 * time.Second,
			}
			return httpSrv.Serve(lyssnare)
		},
	}
	cmd.Flags().IntVar(&port, "port", standardWebbport, "port att lyssna på, utan flaggan tar PM nästa lediga")
	cmd.Flags().StringVar(&bind, "bind", "", "adress att binda till, tom betyder alla gränssnitt")
	return cmd
}

// lyssna öppnar porten. En vald port måste vara ledig, annars säger PM vilken
// port som är ledig i stället. Utan flagga letar PM själv uppåt från 6060.
func lyssna(bind string, port int, valdAvAnvandaren bool) (net.Listener, int, error) {
	sista := port
	if !valdAvAnvandaren {
		sista = port + webbportSpann
	}
	for prova := port; prova <= sista; prova++ {
		lyssnare, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(prova)))
		if err == nil {
			return lyssnare, prova, nil
		}
		if !upptagen(err) {
			return nil, 0, fmt.Errorf("PM kunde inte öppna port %d: %w", prova, err)
		}
	}
	if valdAvAnvandaren {
		return nil, 0, fmt.Errorf("port %d är upptagen. Välj en annan port med --port, eller kör utan flaggan så letar PM själv", port)
	}
	return nil, 0, fmt.Errorf("portarna %d till %d är upptagna. Välj en ledig port med --port", port, sista)
}

func upptagen(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES)
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
