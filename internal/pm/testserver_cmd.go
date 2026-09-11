package pm

import (
	"database/sql"
	"errors"
	"fmt"

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
	return NewTestserverStore(cli.DB(), konfig, cli.WorkDir()), nil
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
			return skrivTestserverStatus(cmd, server)
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
			server, err := store.Hamta(cmd.Context(), args[0])
			if errors.Is(err, sql.ErrNoRows) {
				server = &Testserver{Alias: args[0]}
			} else if err != nil {
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
	if !server.Lever {
		fmt.Fprintf(cmd.OutOrStdout(), "Testservern för %s kör inte.\n", server.Alias)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Testservern för %s kör på port %d.\nPID: %d\nLogg: %s\n",
		server.Alias, server.Port, server.PID, server.Logg)
	return nil
}
