package pm

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/service"
)

// NewDelaUtCmd bygger kommandot dela-ut.
func NewDelaUtCmd(profil func() string) *cobra.Command {
	var agent string
	var neka bool
	var koMinuter int

	cmd := &cobra.Command{
		Use:   "dela-ut <task>",
		Short: "Dela ut en task till en agent och kör den headless",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db := cli.DB()
			konfig, err := LasKonfig(cli.WorkDir())
			if err != nil {
				return err
			}
			tasks := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db))
			taskID, err := tasks.ResolveRef(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("hittade inte tasken %q i PM-workspacet: %w", args[0], err)
			}

			utdelare := NewUtdelare(db, konfig, FranKonfig(konfig))
			korning, err := utdelare.DelaUt(cmd.Context(), UtdelInput{
				TaskID:       taskID,
				Overstyrning: agent,
				WorkspaceDir: cli.WorkDir(),
				Profil:       profil(),
				PMBinar:      PMBinar(),
				KoTimeout:    time.Duration(koMinuter) * time.Minute,
				Neka:         neka,
			})
			if korning != nil && cli.JSONOutput() {
				_ = skrivJSON(cmd, korning)
			}
			if err != nil {
				return err
			}
			if !cli.JSONOutput() {
				fmt.Fprintf(cmd.OutOrStdout(), "körning %s: %s (agent %s, exitkod %d)\n%s\nlogg: %s\n",
					korning.ID, korning.Status, korning.Agent, exitVarde(korning), korning.Motivering, korning.Logg)
			}
			if korning.Status == StatusFel {
				return fmt.Errorf("körningen misslyckades, tasken är tillbaka i todo")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "överstyr regelvalet")
	cmd.Flags().BoolVar(&neka, "neka", false, "neka i stället för att köa när repot är upptaget")
	cmd.Flags().IntVar(&koMinuter, "ko-minuter", 30, "hur länge körningen står köad innan den ger upp")
	return cmd
}

// NewKorningCmd bygger kommandogrenen korning.
func NewKorningCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "korning", Short: "Utdelade körningar"}
	cmd.AddCommand(korningListCmd(), korningShowCmd())
	return cmd
}

func korningListCmd() *cobra.Command {
	var projekt string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista körningar",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := cli.DB()
			projectID := ""
			if projekt != "" {
				var err error
				projectID, err = NewSamtalStore(db).ProjectIDByAlias(cmd.Context(), projekt)
				if err != nil {
					return err
				}
			}
			korningar, err := NewKorningStore(db).Lista(cmd.Context(), projectID, limit)
			if err != nil {
				return err
			}
			if cli.JSONOutput() {
				return skrivJSON(cmd, map[string]any{"korningar": korningar})
			}
			if len(korningar) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "inga körningar")
				return nil
			}
			for _, k := range korningar {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %-5s  %-8s  %-10s  exit=%s  %s\n",
					k.ID, k.TaskRef, k.Status, k.Agent, exitText(k.ExitKod), Tidstext(k.SkapadAt))
			}
			return nil
		},
	}
	projektFlagga(cmd, &projekt)
	cmd.Flags().IntVar(&limit, "limit", 0, "visa bara de senaste N körningarna")
	return cmd
}

func korningShowCmd() *cobra.Command {
	var visaLogg bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Visa en körning med status, exitkod och logg",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := NewKorningStore(cli.DB()).Hamta(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if cli.JSONOutput() {
				return skrivJSON(cmd, k)
			}
			ut := cmd.OutOrStdout()
			fmt.Fprintf(ut, "Körning:    %s\nTask:       %s\nAgent:      %s\nVald av:    %s\nStatus:     %s\nExitkod:    %s\nRepo:       %s\nSkapad:     %s\n",
				k.ID, k.TaskRef, k.Agent, k.Motivering, k.Status, exitText(k.ExitKod), k.RepoPath, Tidstext(k.SkapadAt))
			if k.StartadAt != nil {
				fmt.Fprintf(ut, "Startad:    %s\n", Tidstext(*k.StartadAt))
			}
			if k.SlutAt != nil {
				fmt.Fprintf(ut, "Slut:       %s\n", Tidstext(*k.SlutAt))
			}
			fmt.Fprintf(ut, "Logg:       %s\n", k.Logg)
			if visaLogg {
				fmt.Fprintf(ut, "\n--- logg ---\n%s\n", LasLogg(k.Logg))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&visaLogg, "logg", false, "skriv ut loggen")
	return cmd
}

func exitText(kod *int) string {
	if kod == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *kod)
}

func exitVarde(k *Korning) int {
	if k.ExitKod == nil {
		return -1
	}
	return *k.ExitKod
}
