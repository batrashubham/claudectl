package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"

	"github.com/spf13/cobra"
)

var cronInterval int

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Manage automatic background sync via crontab",
}

var cronInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Add claudectl sync to your crontab",
	Long: `Add claudectl sync to your crontab.

Default interval is 5 minutes. Change with -i flag:
  claudectl cron install -i 10    # every 10 minutes
  claudectl cron install -i 1     # every minute`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := installCronJob(); err != nil {
			return err
		}
		fmt.Printf("✓ Installed: syncing every %d minutes\n", cronInterval)
		fmt.Printf("  Logs: %s\n", cronLogPath())
		fmt.Printf("  Remove with: claudectl cron remove\n")
		return nil
	},
}

func installCronJob() error {
	binary, err := os.Executable()
	if err != nil {
		binary = "claudectl"
	}

	cronExpr := fmt.Sprintf("*/%d * * * *", cronInterval)
	cronLine := fmt.Sprintf("%s %q sync --quiet%s >> %q 2>&1", cronExpr, binary, cronWorkspaceArg(), cronLogPath())

	// Check if already installed
	existing, _ := exec.Command("crontab", "-l").Output()
	for _, line := range strings.Split(string(existing), "\n") {
		if cronMatches(line) {
			return nil
		}
	}

	// Append to crontab
	newCrontab := string(existing)
	if !strings.HasSuffix(newCrontab, "\n") && newCrontab != "" {
		newCrontab += "\n"
	}
	newCrontab += cronLine + "\n"

	installCmd := exec.Command("crontab", "-")
	installCmd.Stdin = strings.NewReader(newCrontab)
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install crontab: %w", err)
	}

	return nil
}

var cronRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove claudectl sync from your crontab",
	RunE: func(cmd *cobra.Command, args []string) error {
		existing, err := exec.Command("crontab", "-l").Output()
		if err != nil {
			fmt.Println("No crontab found.")
			return nil
		}

		lines := strings.Split(string(existing), "\n")
		var filtered []string
		removed := false
		for _, line := range lines {
			if cronMatches(line) {
				removed = true
				continue
			}
			filtered = append(filtered, line)
		}

		if !removed {
			fmt.Println("claudectl sync not found in crontab.")
			return nil
		}

		newCrontab := strings.Join(filtered, "\n")
		installCmd := exec.Command("crontab", "-")
		installCmd.Stdin = strings.NewReader(newCrontab)
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to update crontab: %w", err)
		}

		fmt.Println("Removed claudectl sync from crontab.")
		return nil
	},
}

var cronStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if claudectl sync is in your crontab",
	RunE: func(cmd *cobra.Command, args []string) error {
		existing, err := exec.Command("crontab", "-l").Output()
		if err != nil {
			fmt.Println("No crontab configured.")
			return nil
		}

		found := false
		for _, line := range strings.Split(string(existing), "\n") {
			if cronMatches(line) {
				fmt.Println("Active:")
				fmt.Println("  " + line)
				found = true
			}
		}

		if !found {
			fmt.Println("Not installed. Run 'claudectl cron install' to set up.")
		}

		return nil
	},
}

func init() {
	cronInstallCmd.Flags().IntVarP(&cronInterval, "interval", "i", 5, "Sync interval in minutes")
	cronCmd.AddCommand(cronInstallCmd)
	cronCmd.AddCommand(cronRemoveCmd)
	cronCmd.AddCommand(cronStatusCmd)
	rootCmd.AddCommand(cronCmd)
}

// cronWorkspaceArg pins the workspace explicitly, even the default, so a
// later 'workspace use' can't silently retarget this job.
func cronWorkspaceArg() string {
	return " --workspace " + cfg.Workspace
}

func cronLogPath() string {
	name := "sync.log"
	if cfg.Workspace != config.DefaultWorkspace {
		name = "sync-" + cfg.Workspace + ".log"
	}
	return filepath.Join(config.Dir(), name)
}

// cronMatches finds this workspace's sync line: each workspace gets its own.
func cronMatches(line string) bool {
	if !strings.Contains(line, "claudectl") || !strings.Contains(line, " sync") {
		return false
	}
	if strings.Contains(line+" ", "--workspace "+cfg.Workspace+" ") {
		return true
	}
	// Lines from before workspaces carry no flag and belong to the default.
	return cfg.Workspace == config.DefaultWorkspace && !strings.Contains(line, "--workspace")
}
