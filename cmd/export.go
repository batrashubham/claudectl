package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/spf13/cobra"
)

var (
	exportOutput      string
	exportPromptsOnly bool
)

var exportCmd = &cobra.Command{
	Use:   "export <session-id>",
	Short: "Export a session as a readable markdown document",
	Long: `Export a session's conversation (your prompts and the agent's replies, with
tool calls noted) as markdown. Works for every supported agent and for
sessions that only exist in the backup.

Use --prompts-only for just the prompts you typed.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSessionIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := findSession(args[0])
		if err != nil {
			return err
		}

		var msgs []harness.Message
		if !exportPromptsOnly && !target.IsGhost() {
			msgs, err = env.Messages(*target)
			if err != nil {
				return fmt.Errorf("read transcript: %w", err)
			}
		}
		if len(msgs) == 0 {
			// Ghost sessions, or a transcript with no readable turns: fall
			// back to the prompt history.
			for _, e := range index.PromptEntries(*target) {
				msgs = append(msgs, harness.Message{Role: "user", Text: e.Display, Time: time.UnixMilli(e.Timestamp)})
			}
		}

		var w io.Writer = os.Stdout
		if exportOutput != "" {
			f, err := os.Create(exportOutput)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()
			w = f
		}

		return writeMarkdown(w, target, msgs)
	},
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Write to file instead of stdout")
	exportCmd.Flags().BoolVar(&exportPromptsOnly, "prompts-only", false, "Only include the prompts you typed")
	rootCmd.AddCommand(exportCmd)
}

func writeMarkdown(w io.Writer, meta *index.SessionMeta, msgs []harness.Message) error {
	fmt.Fprintf(w, "# Session Export\n\n")
	fmt.Fprintf(w, "| Field | Value |\n")
	fmt.Fprintf(w, "|-------|-------|\n")
	fmt.Fprintf(w, "| Agent | %s |\n", harness.DisplayName(meta.Harness))
	fmt.Fprintf(w, "| Project | %s |\n", meta.Project)
	if meta.Machine != "" {
		fmt.Fprintf(w, "| Machine | %s |\n", meta.Machine)
	}
	fmt.Fprintf(w, "| Session ID | `%s` |\n", meta.ID)
	if meta.Title != "" {
		fmt.Fprintf(w, "| Title | %s |\n", meta.Title)
	}
	fmt.Fprintf(w, "| Date range | %s - %s |\n", meta.FirstSeen.Format("2006-01-02 15:04"), meta.LastSeen.Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "| Prompts | %d |\n", meta.PromptCount)
	fmt.Fprintf(w, "\n---\n\n")

	prompt := 0
	var tools []string
	flushTools := func() {
		if len(tools) > 0 {
			fmt.Fprintf(w, "> 🔧 %s\n\n", strings.Join(tools, ", "))
			tools = nil
		}
	}
	for _, m := range msgs {
		switch m.Role {
		case "tool":
			tools = append(tools, m.Text)
			continue
		case "user":
			flushTools()
			prompt++
			fmt.Fprintf(w, "## Prompt %d\n\n", prompt)
			if !m.Time.IsZero() {
				fmt.Fprintf(w, "**%s**\n\n", m.Time.Local().Format("2006-01-02 15:04:05"))
			}
		default:
			flushTools()
			fmt.Fprintf(w, "### %s\n\n", harness.DisplayName(meta.Harness))
		}
		fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(m.Text))
	}
	flushTools()
	return nil
}
