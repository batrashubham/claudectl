package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	listJSON    bool
	listHarness string
	listMachine string
	listProject string
	listAll     bool
	listLimit   int
	listQuery   string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions from every agent and machine (active and archived)",
	Example: `  claudectl list --agent codex
  claudectl list --machine desktop --project api-service
  claudectl list --search "rate limit" --limit 10
  claudectl list --json | jq '.[] | select(.Harness == "gemini")'`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateHarnessFilter(listHarness); err != nil {
			return err
		}
		all, err := env.Index()
		if err != nil {
			return err
		}
		sessions := filterSessions(all)

		if listJSON {
			if sessions == nil {
				sessions = []index.SessionMeta{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(sessions)
		}

		showMachine := multipleMachines(all)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		header := "STATUS\tAGENT\tID\tPROJECT\tPROMPTS\tLAST ACTIVE\tPREVIEW"
		if showMachine {
			header = "STATUS\tAGENT\tMACHINE\tID\tPROJECT\tPROMPTS\tLAST ACTIVE\tPREVIEW"
		}
		fmt.Fprintln(w, header)

		for _, s := range sessions {
			preview := s.FirstPrompt
			if preview == "" {
				preview = "-"
			}
			if r := []rune(preview); len(r) > 50 {
				preview = string(r[:47]) + "..."
			}
			cols := []string{statusIcon(s), s.Harness}
			if showMachine {
				cols = append(cols, s.Machine)
			}
			cols = append(cols, shortID(s.ID), s.ProjectName(), fmt.Sprint(s.PromptCount), humanize.Time(s.LastSeen), preview)
			fmt.Fprintln(w, strings.Join(cols, "\t"))
		}
		return w.Flush()
	},
}

func filterSessions(all []index.SessionMeta) []index.SessionMeta {
	var out []index.SessionMeta
	q := strings.ToLower(listQuery)
	for _, s := range all {
		if !listAll && s.IsGhost() {
			continue
		}
		if listHarness != "" && s.Harness != listHarness {
			continue
		}
		if listMachine != "" && s.Machine != listMachine {
			continue
		}
		if listProject != "" && !strings.Contains(strings.ToLower(s.Project), strings.ToLower(listProject)) {
			continue
		}
		if q != "" && !strings.Contains(s.SearchText+strings.ToLower(s.Project)+" "+s.ID, q) {
			continue
		}
		out = append(out, s)
		if listLimit > 0 && len(out) >= listLimit {
			break
		}
	}
	return out
}

func statusIcon(s index.SessionMeta) string {
	switch {
	case s.IsGhost():
		return "△"
	case s.Status == index.StatusArchived:
		return "○"
	default:
		return "●"
	}
}

func multipleMachines(sessions []index.SessionMeta) bool {
	seen := ""
	for _, s := range sessions {
		if s.Machine == "" {
			continue
		}
		if seen != "" && s.Machine != seen {
			return true
		}
		seen = s.Machine
	}
	return false
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
	listCmd.Flags().StringVarP(&listHarness, "agent", "a", "", "Only sessions from this agent (claude, codex, gemini, opencode)")
	listCmd.Flags().StringVarP(&listMachine, "machine", "m", "", "Only sessions from this machine")
	listCmd.Flags().StringVarP(&listProject, "project", "p", "", "Only sessions whose project path contains this")
	listCmd.Flags().StringVarP(&listQuery, "search", "s", "", "Only sessions whose prompts contain this text")
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include ghost sessions (history only, not resumable)")
	listCmd.Flags().IntVarP(&listLimit, "limit", "n", 0, "Show at most N sessions")
	listCmd.RegisterFlagCompletionFunc("agent", completeHarnessNames)
	rootCmd.AddCommand(listCmd)
}
