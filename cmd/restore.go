package cmd

import (
	"fmt"
	"os"

	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore sessions from git remote backup",
	Long: `Restore session backups from the configured git remote.

Use this to restore your backed-up sessions on a new or different machine.

If the backup directory doesn't exist yet and a git_remote is configured,
this will clone the repo. Otherwise it pulls the latest changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir)

		if cfg.GitRemote == "" {
			return fmt.Errorf("no git_remote configured — set it in ~/.claudectl/config.toml or run 'claudectl setup'")
		}

		// If backup dir doesn't exist or isn't a git repo, clone
		gitDir := cfg.BackupDir + "/.git"
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			fmt.Printf("Cloning from %s...\n", cfg.GitRemote)
			if err := engine.GitClone(cfg.GitRemote); err != nil {
				return err
			}
			fmt.Printf("Cloned to %s\n", cfg.BackupDir)
		} else {
			fmt.Printf("Pulling from %s...\n", cfg.GitRemote)
			if err := engine.GitPull(); err != nil {
				return err
			}
			fmt.Println("Up to date.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
}
