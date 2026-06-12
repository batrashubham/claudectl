package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	gcSquash   bool
	gcKeepDays int
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Reclaim disk space in the backup repo",
	Long: `Run garbage collection on the backup git repo to reclaim space.

Sessions are append-only and grow over time. Each sync re-commits the
grown file, so the .git directory accumulates old versions. This command
runs 'git gc --aggressive' to compress them.

  claudectl gc                 # compress git objects (keeps all history)
  claudectl gc --keep-days 30  # squash history older than 30 days
  claudectl gc --squash        # collapse ALL history into one commit

--keep-days preserves recent commit history (the time-machine view) while
squashing older bloat. --squash discards all history. Sessions are always
preserved regardless.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir)

		beforeRepo, _ := engine.RepoSize()
		beforeGit, _ := engine.GitDirSize()

		fmt.Printf("Backup size:  %s (%.0f%% is git history)\n",
			humanize.Bytes(uint64(beforeRepo)),
			pct(beforeGit, beforeRepo))

		switch {
		case gcSquash:
			fmt.Println("Squashing all history into a single commit...")
			if err := engine.Squash(); err != nil {
				return fmt.Errorf("squash: %w", err)
			}
		case gcKeepDays > 0:
			fmt.Printf("Squashing history older than %d days...\n", gcKeepDays)
			kept, err := engine.SquashOlderThan(gcKeepDays)
			if err != nil {
				return fmt.Errorf("squash: %w", err)
			}
			fmt.Printf("Kept %d recent commits.\n", kept)
		default:
			fmt.Println("Running git gc...")
			if err := engine.GC(); err != nil {
				return fmt.Errorf("gc: %w", err)
			}
		}

		afterRepo, _ := engine.RepoSize()
		afterGit, _ := engine.GitDirSize()
		reclaimed := beforeRepo - afterRepo

		fmt.Printf("Done. Now %s (git: %s)\n",
			humanize.Bytes(uint64(afterRepo)),
			humanize.Bytes(uint64(afterGit)))
		if reclaimed > 0 {
			fmt.Printf("Reclaimed %s\n", humanize.Bytes(uint64(reclaimed)))
		}
		return nil
	},
}

func pct(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func init() {
	gcCmd.Flags().BoolVar(&gcSquash, "squash", false, "Collapse all history into one commit (reclaims most space)")
	gcCmd.Flags().IntVar(&gcKeepDays, "keep-days", 0, "Squash history older than N days, keep recent commits")
	rootCmd.AddCommand(gcCmd)
}
