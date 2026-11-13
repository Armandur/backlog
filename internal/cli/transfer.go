package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/service"
)

func taskTransferCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "transfer <id>...",
		Short: "Move tasks to another project",
		Long: "Move one or more tasks to another project. The TASK-N reference, plans,\n" +
			"comments and attachments follow the task. Labels are recreated by name in\n" +
			"the target project. Stops at the first task that cannot be moved.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := newTaskSvc()
			for _, ref := range args {
				t, err := svc.Transfer(cmd.Context(), ref, to, app.Actor)
				if err != nil {
					return err
				}
				taskRef := fmt.Sprintf("TASK-%d", t.Seq)
				app.Out.CommandResult("transfer", "task", t.ID, taskRef, "task moved: "+taskRef+" -> "+to)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "alias of the target project")
	cmd.MarkFlagRequired("to")
	return cmd
}

func docTransferCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "transfer <doc-id>",
		Short: "Move a doc to another project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := service.NewDocService(app.DB)
			d, err := svc.Transfer(cmd.Context(), args[0], to, app.Actor)
			if err != nil {
				return err
			}
			app.Out.CommandResult("transfer", "doc", d.ID, d.ID[:8], "doc moved: "+d.ID[:8]+" -> "+to)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "alias of the target project")
	cmd.MarkFlagRequired("to")
	return cmd
}
