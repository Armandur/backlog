package cli

import (
	"fmt"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
	"github.com/spf13/cobra"
)

func newCommentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "comment", Short: "Manage comments"}
	cmd.AddCommand(commentAddCmd(), commentListCmd(), commentDeleteCmd())
	return cmd
}

func commentAddCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:     "add [<task>] <body>",
		Short:   "Add a comment to a task",
		Example: "  backlog comment add TASK-12 \"Fixed in abc123\"\n  backlog comment add \"Fixed in abc123\" --task TASK-12",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			body := args[len(args)-1]
			if len(args) == 2 {
				if taskID != "" {
					return fmt.Errorf("give the task either as the first argument or with --task, not both")
				}
				taskID = args[0]
			}
			if taskID == "" {
				return fmt.Errorf("task is required: backlog comment add TASK-N <body>")
			}
			resolvedID, err := resolveTaskRef(ctx, taskID)
			if err != nil {
				return err
			}
			svc := service.NewCommentService(app.DB)
			c, err := svc.Create(ctx, models.CreateCommentInput{
				TaskID: resolvedID,
				Body:   body,
				Actor:  app.Actor,
			})
			if err != nil {
				return err
			}
			if flagJSON {
				app.Out.PrintJSON(c)
			} else {
				app.Out.Success("comment added: " + c.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID (alternative to the first argument)")
	return cmd
}

func commentListCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List comments for a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			resolvedID, err := resolveTaskRef(ctx, taskID)
			if err != nil {
				return err
			}
			svc := service.NewCommentService(app.DB)
			comments, err := svc.ListForTask(ctx, resolvedID)
			if err != nil {
				return err
			}
			app.Out.Comments(comments)
			return nil
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID")
	cmd.MarkFlagRequired("task")
	return cmd
}

func commentDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a comment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := service.NewCommentService(app.DB)
			if err := svc.Delete(cmd.Context(), args[0], app.Actor); err != nil {
				return err
			}
			ref := args[0]
			if len(ref) > 8 {
				ref = ref[:8]
			}
			app.Out.CommandResult("delete", "comment", args[0], ref, "comment deleted: "+ref)
			return nil
		},
	}
}
