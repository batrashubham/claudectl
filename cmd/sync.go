package cmd

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/spf13/cobra"
)

var (
	syncWatch    bool
	syncInterval time.Duration
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync sessions from ~/.claude/projects to backup directory",
	Long: `Sync all session data to the backup directory.

Use --watch to run continuously in the background (e.g., for a tmux pane or launchd).
Default interval is 5 minutes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if syncWatch {
			return runSyncWatch()
		}
		return runSyncOnce()
	},
}

func init() {
	syncCmd.Flags().BoolVarP(&syncWatch, "watch", "w", false, "Run continuously at an interval")
	syncCmd.Flags().DurationVarP(&syncInterval, "interval", "i", 5*time.Minute, "Sync interval (with --watch)")
	rootCmd.AddCommand(syncCmd)
}

func runSyncOnce() error {
	engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir)

	fmt.Printf("Syncing %s → %s\n", cfg.ClaudeDir+"/projects", cfg.BackupDir)

	// Setup remote if configured
	if cfg.GitRemote != "" {
		if err := engine.GitSetupRemote(cfg.GitRemote); err != nil {
			fmt.Printf("Warning: could not setup git remote: %v\n", err)
		}
	}

	result, err := engine.Sync()
	if err != nil {
		return err
	}

	fmt.Printf("Done: %d new, %d updated (%s)\n",
		result.NewFiles, result.UpdatedFiles, humanize.Bytes(uint64(result.TotalBytes)))

	if cfg.GitAutoCommit {
		if err := engine.GitCommit(result); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}

	if cfg.GitPush {
		if err := engine.GitPush(); err != nil {
			fmt.Printf("Warning: push failed: %v\n", err)
		} else if result.NewFiles > 0 || result.UpdatedFiles > 0 {
			fmt.Println("Pushed to remote.")
		}
	}

	return nil
}

func runSyncWatch() error {
	fmt.Printf("Watching for changes every %s\n", syncInterval)
	fmt.Printf("Syncing %s → %s\n", cfg.ClaudeDir+"/projects", cfg.BackupDir)
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	for {
		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir)

		if cfg.GitRemote != "" {
			engine.GitSetupRemote(cfg.GitRemote)
		}

		result, err := engine.Sync()
		if err != nil {
			fmt.Printf("[%s] error: %v\n", time.Now().Format("15:04:05"), err)
		} else if result.NewFiles > 0 || result.UpdatedFiles > 0 {
			fmt.Printf("[%s] synced: %d new, %d updated (%s)\n",
				time.Now().Format("15:04:05"),
				result.NewFiles, result.UpdatedFiles,
				humanize.Bytes(uint64(result.TotalBytes)))
			if cfg.GitAutoCommit {
				engine.GitCommit(result)
			}
			if cfg.GitPush {
				if err := engine.GitPush(); err != nil {
					fmt.Printf("[%s] push failed: %v\n", time.Now().Format("15:04:05"), err)
				}
			}
		}

		time.Sleep(syncInterval)
	}
}
