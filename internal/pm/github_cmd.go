package pm

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mazen160/backlog/internal/cli"
)

// NewGitHubCmd bygger kommandogrenen för PM:s GitHub-gränssnitt.
func NewGitHubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Visa och använd PM:s kontrollerade GitHub-åtkomst",
	}
	cmd.AddCommand(githubStatusCmd())
	return cmd
}

func githubStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Visa GitHub CLI och PM:s GitHub-behörighet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			konfig, err := LasKonfig(cli.WorkDir())
			if err != nil {
				return err
			}
			version, versionsfel := NewGitHubKlient(konfig.GitHub).Version(cmd.Context())
			fmt.Fprint(cmd.OutOrStdout(), GitHubStatus(konfig.GitHub, version, versionsfel))
			return nil
		},
	}
}

// GitHubStatus formaterar diagnostik utan hemliga värden.
func GitHubStatus(konfig GitHubKonfig, version string, versionsfel error) string {
	token := os.Getenv("GH_TOKEN")
	ghVersion := strings.TrimSpace(strings.SplitN(doljGitHubToken(version, token), "\n", 2)[0])
	if versionsfel != nil {
		ghVersion = doljGitHubToken(versionsfel.Error(), token)
	}
	if ghVersion == "" {
		ghVersion = "okänd"
	}
	inloggning := "saknas"
	if token != "" {
		inloggning = "GH_TOKEN"
	}
	skrivlage := "av"
	if konfig.Skrivlage {
		skrivlage = "på"
	}
	repon := append([]string(nil), konfig.TillatnaRepon...)
	sort.Strings(repon)
	if len(repon) == 0 {
		repon = []string{"inga"}
	}
	return fmt.Sprintf(
		"gh-version: %s\ninloggning: %s\ntillåtna repon: %s\nskrivläge: %s\n",
		ghVersion,
		inloggning,
		strings.Join(repon, ", "),
		skrivlage,
	)
}
