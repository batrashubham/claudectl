package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		return installCronJob()
	},
}

func installCronJob() error {
	binary, err := os.Executable()
	if err != nil {
		binary = "claudectl"
	}

	cronExpr := fmt.Sprintf("*/%d * * * *", cronInterval)
	cronLine := fmt.Sprintf("%s %s sync >> /tmp/claudectl-sync.log 2>&1", cronExpr, binary)

	// Check if already installed
	existing, _ := exec.Command("crontab", "-l").Output()
	if strings.Contains(string(existing), "claudectl sync") {
		return nil
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
			if strings.Contains(line, "claudectl sync") {
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
			if strings.Contains(line, "claudectl sync") {
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
