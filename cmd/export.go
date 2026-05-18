package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/spf13/cobra"
)

var exportOutput string

var exportCmd = &cobra.Command{
	Use:   "export <session-id>",
	Short: "Export a session as a readable markdown document",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]

		builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
		sessions, err := builder.Build()
		if err != nil {
			return err
		}

		var target *index.SessionMeta
		for i := range sessions {
			if sessions[i].ID == sessionID {
				target = &sessions[i]
				break
			}
		}

		if target == nil {
			return fmt.Errorf("session %s not found", sessionID)
		}

		entries, err := builder.GetSessionEntries(sessionID)
		if err != nil {
			return fmt.Errorf("read session entries: %w", err)
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Timestamp < entries[j].Timestamp
		})

		var w io.Writer = os.Stdout
		if exportOutput != "" {
			f, err := os.Create(exportOutput)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()
			w = f
		}

		return writeMarkdown(w, target, entries)
	},
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Write to file instead of stdout")
	rootCmd.AddCommand(exportCmd)
}

func writeMarkdown(w io.Writer, meta *index.SessionMeta, entries []index.HistoryEntry) error {
	// Header
	fmt.Fprintf(w, "# Session Export\n\n")
	fmt.Fprintf(w, "| Field | Value |\n")
	fmt.Fprintf(w, "|-------|-------|\n")
	fmt.Fprintf(w, "| Project | %s |\n", meta.Project)
	fmt.Fprintf(w, "| Session ID | `%s` |\n", meta.ID)

	if len(entries) > 0 {
		first := time.UnixMilli(entries[0].Timestamp)
		last := time.UnixMilli(entries[len(entries)-1].Timestamp)
		fmt.Fprintf(w, "| Date range | %s - %s |\n", first.Format("2006-01-02 15:04"), last.Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintf(w, "| Date range | %s - %s |\n", meta.FirstSeen.Format("2006-01-02 15:04"), meta.LastSeen.Format("2006-01-02 15:04"))
	}

	fmt.Fprintf(w, "| Prompts | %d |\n", meta.PromptCount)
	fmt.Fprintf(w, "\n---\n\n")

	// Entries
	for i, entry := range entries {
		ts := time.UnixMilli(entry.Timestamp)
		fmt.Fprintf(w, "## Prompt %d\n\n", i+1)
		fmt.Fprintf(w, "**%s**\n\n", ts.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(w, "%s\n\n", entry.Display)
	}

	return nil
}
