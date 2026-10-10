package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	listJSON bool
	listAll  bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions (active and archived)",
	RunE: func(cmd *cobra.Command, args []string) error {
		builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName)
		sessions, err := builder.Build()
		if err != nil {
			return err
		}
		if !listAll {
			sessions = index.ForMachine(sessions, cfg.MachineName)
		}

		if listJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(sessions)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "STATUS\tMACHINE\tPROJECT\tPROMPTS\tLAST ACTIVE\tPREVIEW\n")

		for _, s := range sessions {
			status := "●"
			if s.Status == index.StatusArchived && s.FileSize == 0 {
				status = "△"
			} else if s.Status == index.StatusArchived {
				status = "○"
			}

			project := filepath.Base(s.Project)
			if project == "" || project == "." {
				project = s.ProjectDir
			}

			preview := s.FirstPrompt
			if preview == "" {
				preview = s.ID[:8] + "..."
			}
			if len(preview) > 50 {
				preview = preview[:47] + "..."
			}

			lastActive := humanize.Time(s.LastSeen)

			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
				status, s.Machine, project, s.PromptCount, lastActive, preview)
		}

		return w.Flush()
	},
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Include sessions from all machines")
	rootCmd.AddCommand(listCmd)
}
