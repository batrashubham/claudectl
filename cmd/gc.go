package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var gcSquash bool

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Reclaim disk space in the backup repo",
	Long: `Run garbage collection on the backup git repo to reclaim space.

Sessions are append-only and grow over time. Each sync re-commits the
grown file, so the .git directory accumulates old versions. This command
runs 'git gc --aggressive' to compress them.

Use --squash to also collapse all git history into a single commit.
This reclaims the most space but discards the commit history (the
time-machine view of past syncs). Your current sessions are preserved.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir)

		beforeRepo, _ := engine.RepoSize()
		beforeGit, _ := engine.GitDirSize()

		fmt.Printf("Backup size:  %s (%.0f%% is git history)\n",
			humanize.Bytes(uint64(beforeRepo)),
			pct(beforeGit, beforeRepo))

		if gcSquash {
			fmt.Println("Squashing history into a single commit...")
			if err := engine.Squash(); err != nil {
				return fmt.Errorf("squash: %w", err)
			}
		} else {
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
	rootCmd.AddCommand(gcCmd)
}
