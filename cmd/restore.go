package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Pull the latest backup (including other machines' sessions) from the git remote",
	Long: `Fetch the backup from the configured git remote.

Use this on a new machine, or to see sessions other machines have pushed.
If the backup directory doesn't exist yet it is cloned; otherwise local
sync commits are rebased onto the remote.

Nothing is written into your agents' own directories: sessions are only
copied back when you resume one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		engine := env.Engine()

		if cfg.GitRemote == "" {
			return fmt.Errorf("no git_remote configured — set it in %s or run 'claudectl setup'", configLocation())
		}

		if _, err := os.Stat(filepath.Join(cfg.BackupDir, ".git")); os.IsNotExist(err) {
			if entries, _ := os.ReadDir(cfg.BackupDir); len(entries) > 0 {
				return fmt.Errorf("%s exists but is not a git repo; move it aside before restoring", cfg.BackupDir)
			}
			os.Remove(cfg.BackupDir)
			fmt.Printf("Cloning from %s...\n", cfg.GitRemote)
			if err := engine.GitClone(cfg.GitRemote); err != nil {
				return err
			}
			fmt.Printf("Cloned to %s\n", cfg.BackupDir)
		} else {
			if err := engine.GitSetupRemote(cfg.GitRemote); err != nil {
				return err
			}
			fmt.Printf("Pulling from %s...\n", cfg.GitRemote)
			if err := engine.GitPull(); err != nil {
				return err
			}
			fmt.Println("Up to date.")
		}

		sessions, err := env.Index()
		if err == nil {
			counts := map[string]int{}
			for _, s := range sessions {
				if !s.IsGhost() {
					counts[s.Machine]++
				}
			}
			for m, n := range counts {
				marker := ""
				if m == env.Machine {
					marker = " (this machine)"
				}
				fmt.Printf("  %-20s %d sessions%s\n", m, n, marker)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
}
