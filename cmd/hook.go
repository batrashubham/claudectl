package cmd

import (
	"fmt"
	"os"

	"github.com/batrashubham/claudectl/internal/hook"
	"github.com/spf13/cobra"
)

const sessionEndEvent = "SessionEnd"

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Back up automatically when a Claude Code session ends",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := rootCmd.PersistentPreRunE(cmd, args); err != nil {
			return err
		}
		hook.SetClaudeDir(cfg.ClaudeDir)
		return nil
	},
	Long: `Install a Claude Code SessionEnd hook that syncs your sessions.

This is an event-driven alternative to cron: instead of polling every few
minutes, the backup runs the moment a session ends. The hook is installed
in your Claude Code settings.json and runs in the background, so it never
delays Claude exiting.`,
}

var hookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the SessionEnd backup hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		binary, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate claudectl binary: %w", err)
		}

		// --wait: sessions often end together; queue behind a running sync
		// rather than skipping, or the second session misses this backup.
		if err := hook.Install(sessionEndEvent, binary, cfg.Workspace, "--wait", "--quiet"); err != nil {
			return err
		}

		fmt.Printf("✓ Installed SessionEnd hook in %s\n", hook.SettingsPath())
		fmt.Println("  Sessions now back up automatically when a Claude session ends.")
		fmt.Println("  Each backup covers every enabled agent, not just Claude Code.")
		fmt.Println("  Runs in the background, so it never delays Claude exiting.")
		fmt.Println()
		fmt.Println("  Already using cron? You can drop it: claudectl cron remove")
		fmt.Println("  Takes effect in new Claude sessions.")
		return nil
	},
}

var hookRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove the SessionEnd backup hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, err := hook.Remove(sessionEndEvent, cfg.Workspace)
		if err != nil {
			return err
		}
		if !removed {
			fmt.Println("No claudectl hook was installed.")
			return nil
		}
		fmt.Println("✓ Removed SessionEnd hook.")
		return nil
	},
}

var hookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether the SessionEnd hook is installed",
	RunE: func(cmd *cobra.Command, args []string) error {
		installed, command, err := hook.Installed(sessionEndEvent, cfg.Workspace)
		if err != nil {
			return err
		}
		fmt.Printf("Settings:  %s\n", hook.SettingsPath())
		if installed {
			fmt.Printf("SessionEnd: active\n")
			fmt.Printf("  %s\n", command)
		} else {
			fmt.Println("SessionEnd: not installed")
			fmt.Println("  Install with: claudectl hook install")
		}
		return nil
	},
}

func init() {
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookRemoveCmd)
	hookCmd.AddCommand(hookStatusCmd)
	rootCmd.AddCommand(hookCmd)
}
