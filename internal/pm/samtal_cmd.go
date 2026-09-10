package pm

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
	"github.com/mazen160/backlog/internal/models"
)

// NewSamtalCmd bygger kommandogrenen samtal.
func NewSamtalCmd(reg *AgentRegister) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "samtal",
		Short: "Projektsamtal: en tråd per projekt",
	}
	cmd.AddCommand(samtalAddCmd(), samtalListCmd(), samtalFragaCmd(reg))
	return cmd
}

func samtalAddCmd() *cobra.Command {
	var projekt, taskID, aktor string
	cmd := &cobra.Command{
		Use:   "add <text>",
		Short: "Skriv ett inlägg i projektets tråd",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := NewSamtalStore(cli.DB())
			projectID, err := store.ProjectIDByAlias(cmd.Context(), projekt)
			if err != nil {
				return err
			}
			a, err := aktorEller(aktor, cli.CurrentActor())
			if err != nil {
				return err
			}
			post, err := store.Add(cmd.Context(), projectID, taskID, a, args[0])
			if err != nil {
				return err
			}
			if cli.JSONOutput() {
				return skrivJSON(cmd, post)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "inlägg sparat: %s %s:%s\n", Tidstext(post.CreatedAt), post.Actor.Kind, post.Actor.Name)
			return nil
		},
	}
	projektFlagga(cmd, &projekt)
	cmd.Flags().StringVar(&taskID, "task", "", "koppla inlägget till en task (ULID)")
	cmd.Flags().StringVar(&aktor, "as-aktor", "", "aktör för inlägget, annars --as")
	return cmd
}

func samtalListCmd() *cobra.Command {
	var projekt string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Visa projektets tråd",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := NewSamtalStore(cli.DB())
			projectID, err := store.ProjectIDByAlias(cmd.Context(), projekt)
			if err != nil {
				return err
			}
			poster, err := store.List(cmd.Context(), projectID, limit)
			if err != nil {
				return err
			}
			if cli.JSONOutput() {
				return skrivJSON(cmd, map[string]any{"samtal": poster})
			}
			if len(poster) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "tråden är tom")
				return nil
			}
			for _, p := range poster {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s:%s\n%s\n\n", Tidstext(p.CreatedAt), p.Actor.Kind, p.Actor.Name, p.Text)
			}
			return nil
		},
	}
	projektFlagga(cmd, &projekt)
	cmd.Flags().IntVar(&limit, "limit", 0, "visa bara de senaste N inläggen")
	return cmd
}

func samtalFragaCmd(reg *AgentRegister) *cobra.Command {
	var projekt, agentNamn, aktor string
	cmd := &cobra.Command{
		Use:   "fraga <text>",
		Short: "Fråga en agent i tråden, med projektets kontext",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db := cli.DB()
			store := NewSamtalStore(db)
			projectID, err := store.ProjectIDByAlias(cmd.Context(), projekt)
			if err != nil {
				return err
			}
			a, err := aktorEller(aktor, cli.CurrentActor())
			if err != nil {
				return err
			}
			svar, err := Fraga(cmd.Context(), db, reg, FragaInput{
				Alias:     projekt,
				ProjectID: projectID,
				Fraga:     args[0],
				Agent:     agentNamn,
				Fragare:   a,
			})
			if err != nil {
				return err
			}
			if cli.JSONOutput() {
				return skrivJSON(cmd, svar)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s:%s svarade:\n%s\n", svar.Actor.Kind, svar.Actor.Name, svar.Text)
			return nil
		},
	}
	projektFlagga(cmd, &projekt)
	cmd.Flags().StringVar(&agentNamn, "agent", "", "agent att fråga, annars förvalet")
	cmd.Flags().StringVar(&aktor, "as-aktor", "", "aktör för frågan, annars --as")
	return cmd
}

func projektFlagga(cmd *cobra.Command, mal *string) {
	cmd.Flags().StringVarP(mal, "project", "p", "", "projektalias")
}

func aktorEller(flaggvarde string, forval models.Actor) (models.Actor, error) {
	if flaggvarde != "" {
		return ParseActor(flaggvarde)
	}
	if forval.Name == "" {
		return models.Actor{}, fmt.Errorf("ange aktör med --as human:namn")
	}
	return forval, nil
}

func skrivJSON(cmd *cobra.Command, v any) error {
	ut := cmd.OutOrStdout()
	if ut == nil {
		ut = os.Stdout
	}
	enc := json.NewEncoder(ut)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
