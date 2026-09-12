package pm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
)

// NewTestserverCmd bygger kommandona för projektens testservrar.
func NewTestserverCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "testserver", Short: "Starta, stoppa och visa projektets testserver"}
	cmd.AddCommand(testserverStartCmd(), testserverStopCmd(), testserverStatusCmd())
	return cmd
}

func testserverStoreFranCLI() (*TestserverStore, error) {
	konfig, err := LasKonfig(cli.WorkDir())
	if err != nil {
		return nil, err
	}
	store := NewTestserverStore(cli.DB(), konfig, cli.WorkDir())
	store.cachetid = 250 * time.Millisecond
	return store, nil
}

func testserverStartCmd() *cobra.Command {
	return &cobra.Command{
		Use: "start <alias>", Short: "Starta projektets testserver", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := testserverStoreFranCLI()
			if err != nil {
				return err
			}
			server, err := store.Starta(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			server, err = vantaPaTestserver(cmd.Context(), store, args[0], 15*time.Second)
			if err != nil {
				return err
			}
			if server.Status == TestserverUppe {
				return skrivTestserverStatus(cmd, server)
			}
			if err := skrivTestserverStatus(cmd, server); err != nil {
				return err
			}
			if err := skrivSistaLoggrader(cmd, server.Logg, 20); err != nil {
				return err
			}
			if server.Status == TestserverKrasch {
				return fmt.Errorf("testservern för %q kraschade innan den svarade. Läs loggen med backlog-pm testserver logg %s", args[0], args[0])
			}
			return fmt.Errorf("testservern för %q svarade inte inom 15 sekunder. Läs loggen med backlog-pm testserver logg %s", args[0], args[0])
		},
	}
}

func testserverStopCmd() *cobra.Command {
	return &cobra.Command{
		Use: "stop <alias>", Short: "Stoppa projektets testserver", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := testserverStoreFranCLI()
			if err != nil {
				return err
			}
			server, err := store.Stoppa(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return skrivTestserverStatus(cmd, server)
		},
	}
}

func testserverStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status <alias>", Short: "Visa projektets testserver", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := testserverStoreFranCLI()
			if err != nil {
				return err
			}
			server, err := store.Status(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return skrivTestserverStatus(cmd, server)
		},
	}
}

func skrivTestserverStatus(cmd *cobra.Command, server *Testserver) error {
	if cli.JSONOutput() {
		return skrivJSON(cmd, server)
	}
	switch server.Status {
	case TestserverNere, "":
		fmt.Fprintf(cmd.OutOrStdout(), "Testservern för %s kör inte.\n", server.Alias)
	case TestserverStartar:
		fmt.Fprintf(cmd.OutOrStdout(),
			"Testservern för %s startar på port %d.\nPID: %d\nLogg: %s\n",
			server.Alias, server.Port, server.PID, server.Logg)
	case TestserverUppe:
		fmt.Fprintf(cmd.OutOrStdout(),
			"Testservern för %s svarar på port %d.\nPID: %d\nLogg: %s\n",
			server.Alias, server.Port, server.PID, server.Logg)
	case TestserverKrasch:
		exittext := "okänd"
		if server.Exitkod != nil {
			exittext = fmt.Sprint(*server.Exitkod)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Testservern för %s har kraschat.\nExitkod: %s\nLogg: %s\n",
			server.Alias, exittext, server.Logg)
	}
	return nil
}

func vantaPaTestserver(
	ctx context.Context,
	store *TestserverStore,
	alias string,
	vantetid time.Duration,
) (*Testserver, error) {
	tidsgrans := time.NewTimer(vantetid)
	defer tidsgrans.Stop()
	pollning := time.NewTicker(250 * time.Millisecond)
	defer pollning.Stop()
	var senaste *Testserver
	for {
		server, err := store.Status(ctx, alias)
		if err != nil {
			return nil, err
		}
		senaste = server
		if server.Status == TestserverUppe || server.Status == TestserverKrasch {
			return server, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tidsgrans.C:
			return senaste, nil
		case <-pollning.C:
		}
	}
}

func skrivSistaLoggrader(cmd *cobra.Command, sokvag string, antal int) error {
	fil, err := os.Open(sokvag)
	if err != nil {
		return fmt.Errorf("kunde inte läsa testserverns logg: %w", err)
	}
	defer fil.Close()
	info, err := fil.Stat()
	if err != nil {
		return err
	}
	const maxLasning = 64 * 1024
	start := info.Size() - maxLasning
	if start < 0 {
		start = 0
	}
	if _, err := fil.Seek(start, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(fil, maxLasning))
	if err != nil {
		return err
	}
	if start > 0 {
		if brytpunkt := strings.IndexByte(string(data), '\n'); brytpunkt >= 0 {
			data = data[brytpunkt+1:]
		}
	}
	rader := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rader) > antal {
		rader = rader[len(rader)-antal:]
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Senaste loggraderna:")
	if len(rader) == 1 && rader[0] == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "(loggen är tom)")
		return nil
	}
	fmt.Fprintln(cmd.ErrOrStderr(), strings.Join(rader, "\n"))
	return nil
}
