package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/search"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	searchLimit int
	searchJSON  bool
)

var searchCmd = &cobra.Command{
	Use:   `search <query...>`,
	Short: "Search the full text of all sessions",
	Long: `Search prompts, Claude's replies, commands run, files touched and tool errors.
All words must match, in any order. Quote a phrase to match it exactly:

  claudectl search kafka timeout
  claudectl search '"consumer group" rebalance'`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir).Build()
		if err != nil {
			return err
		}
		idx, err := search.BuildForSessions(cfg, sessions)
		if err != nil {
			return fmt.Errorf("build search index: %w", err)
		}

		hits := idx.Search(strings.Join(args, " "))
		if searchLimit > 0 && len(hits) > searchLimit {
			hits = hits[:searchLimit]
		}

		byID := make(map[string]index.SessionMeta, len(sessions))
		for _, s := range sessions {
			byID[s.ID] = s
		}

		if searchJSON {
			type result struct {
				ID       string  `json:"id"`
				Project  string  `json:"project"`
				LastSeen string  `json:"lastSeen"`
				Score    float64 `json:"score"`
				Snippet  string  `json:"snippet"`
			}
			out := make([]result, 0, len(hits))
			for _, h := range hits {
				s := byID[h.ID]
				out = append(out, result{h.ID, s.Project, s.LastSeen.Format("2006-01-02T15:04:05Z07:00"), h.Score, h.Snippet})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		if len(hits) == 0 {
			fmt.Println("no matches")
			return nil
		}
		for _, h := range hits {
			s := byID[h.ID]
			fmt.Printf("%s  %s  %s\n", h.ID, filepath.Base(s.Project), humanize.Time(s.LastSeen))
			if h.Snippet != "" {
				fmt.Printf("    %s\n", h.Snippet)
			}
		}
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 20, "max results (0 for all)")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(searchCmd)
}
