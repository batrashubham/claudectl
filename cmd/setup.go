package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive first-time setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetup()
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

func needsSetup() bool {
	_, err := os.Stat(config.ConfigPath())
	return os.IsNotExist(err)
}

func runSetup() error {
	reader := bufio.NewReader(os.Stdin)
	home, _ := os.UserHomeDir()

	fmt.Println()
	fmt.Println("  ⚡ Welcome to claudectl")
	fmt.Println("  ─────────────────────────────────────")
	fmt.Println("  Let's set up session backup for Claude Code.")
	fmt.Println()

	// 1. Backup directory
	defaultBackup := filepath.Join(home, ".claudectl", "backup")
	fmt.Printf("  Backup directory [%s]: ", defaultBackup)
	backupDir := readLine(reader)
	if backupDir == "" {
		backupDir = defaultBackup
	}
	backupDir = expandPath(backupDir, home)

	// Create it
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("could not create backup dir: %w", err)
	}
	fmt.Printf("  ✓ Backup directory: %s\n\n", backupDir)

	// 2. Git remote
	fmt.Println("  Do you want to push backups to a git remote?")
	fmt.Println("  This keeps your sessions safe even if your machine is lost.")
	fmt.Print("  Git remote URL (blank to skip): ")
	gitRemote := readLine(reader)
	gitPush := gitRemote != ""

	if gitPush {
		engine := sync.NewEngine(filepath.Join(home, ".claude"), backupDir)
		if err := engine.GitSetupRemote(gitRemote); err != nil {
			fmt.Printf("  ⚠ Could not configure remote: %v\n", err)
			fmt.Println("  You can set this up later in ~/.claudectl/config.toml")
			gitPush = false
			gitRemote = ""
		} else {
			fmt.Printf("  ✓ Remote configured: %s\n", gitRemote)
		}
	}
	fmt.Println()

	// 3. Cron
	fmt.Println("  Do you want to sync automatically in the background?")
	fmt.Print("  Install cron job? [Y/n]: ")
	cronAnswer := strings.ToLower(strings.TrimSpace(readLine(reader)))
	installCron := cronAnswer == "" || cronAnswer == "y" || cronAnswer == "yes"

	if installCron {
		fmt.Print("  Sync interval in minutes [5]: ")
		intervalStr := readLine(reader)
		interval := 5
		if intervalStr != "" {
			fmt.Sscanf(intervalStr, "%d", &interval)
		}
		if interval < 1 {
			interval = 5
		}

		cronInterval = interval
		// Reuse the cron install logic
		if err := installCronJob(); err != nil {
			fmt.Printf("  ⚠ Could not install cron: %v\n", err)
		} else {
			fmt.Printf("  ✓ Cron installed: syncing every %d minutes\n", interval)
		}
	}
	fmt.Println()

	// 4. Write config
	newCfg := &config.Config{
		BackupDir:     backupDir,
		ClaudeDir:     filepath.Join(home, ".claude"),
		SyncOnStart:   true,
		GitAutoCommit: true,
		GitRemote:     gitRemote,
		GitPush:       gitPush,
	}

	if err := config.Save(newCfg); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}
	fmt.Printf("  ✓ Config saved: %s\n", config.ConfigPath())

	// 5. Initial sync
	fmt.Println()
	fmt.Print("  Run initial sync now? [Y/n]: ")
	syncAnswer := strings.ToLower(strings.TrimSpace(readLine(reader)))
	if syncAnswer == "" || syncAnswer == "y" || syncAnswer == "yes" {
		fmt.Println()
		cfg = newCfg
		if err := runSyncOnce(); err != nil {
			fmt.Printf("  ⚠ Sync error: %v\n", err)
		}
	}

	fmt.Println()
	fmt.Println("  ─────────────────────────────────────")
	fmt.Println("  Setup complete! Run 'claudectl' to launch the TUI.")
	fmt.Println()

	return nil
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func expandPath(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			return abs
		}
	}
	return path
}
