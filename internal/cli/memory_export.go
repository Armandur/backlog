package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
)

func memoryExportCmd() *cobra.Command {
	var (
		project, tag, output string
		all                  bool
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export memory entries as readable Markdown",
		Long: "Write the full text of every memory entry as Markdown, newest first.\n" +
			"Exports one project, or every active project with --all. Projects\n" +
			"without entries are left out of an --all export.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			memSvc := service.NewMemoryService(app.DB)
			projSvc := service.NewProjectService(app.DB)

			var projects []*models.Project
			if all {
				if project != "" {
					return fmt.Errorf("use either --project or --all, not both")
				}
				ps, err := projSvc.List(cmd.Context(), false)
				if err != nil {
					return err
				}
				projects = ps
			} else {
				alias, err := requireProjectOrDefault(cmd, project)
				if err != nil {
					return err
				}
				p, err := projSvc.GetByAlias(cmd.Context(), alias)
				if err != nil {
					return fmt.Errorf("project %q: %w", alias, err)
				}
				projects = []*models.Project{p}
			}

			var buf strings.Builder
			for _, p := range projects {
				entries, err := memSvc.List(cmd.Context(), p.Alias, tag)
				if err != nil {
					return err
				}
				if all && len(entries) == 0 {
					continue
				}
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				writeMemoryMarkdown(&buf, p, entries, time.Local)
			}

			if output == "" || output == "-" {
				_, err := io.WriteString(os.Stdout, buf.String())
				return err
			}
			if err := os.WriteFile(output, []byte(buf.String()), 0o600); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}
			app.Out.CommandResult("export", "memory", "", output, "memory exported: "+output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "project alias")
	cmd.Flags().BoolVar(&all, "all", false, "export every active project")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to file instead of stdout (created with mode 0600)")
	return cmd
}

// writeMemoryMarkdown renders one project's memory entries. Bodies are written
// verbatim, so an entry that itself contains Markdown keeps its formatting.
func writeMemoryMarkdown(w io.Writer, p *models.Project, entries []*models.Memory, loc *time.Location) {
	fmt.Fprintf(w, "# Memory: %s (%s)\n\n", p.Name, p.Alias)
	if len(entries) == 0 {
		fmt.Fprintln(w, "No memory entries.")
		return
	}
	fmt.Fprintf(w, "%d entries, newest first.\n", len(entries))
	for _, e := range entries {
		ts := time.Unix(0, e.CreatedAt).In(loc).Format("2006-01-02 15:04")
		fmt.Fprintf(w, "\n## %s", ts)
		if e.Tags != "" {
			fmt.Fprintf(w, " · %s", e.Tags)
		}
		fmt.Fprintf(w, "\n\n%s\n\n", strings.TrimSpace(e.Body))
		fmt.Fprintf(w, "_%s:%s · %s_\n", e.Actor.Kind, e.Actor.Name, e.ID)
	}
}
