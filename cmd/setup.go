package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/machine"
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
	fmt.Println("  Let's set up session backup for your coding agents.")
	fmt.Println()

	fmt.Println("  Agents found on this machine:")
	found := 0
	for _, h := range env.All {
		mark := "·"
		state := "not found"
		if env.IsEnabled(h.Name()) {
			mark, state = "✓", "will be backed up"
			found++
		}
		fmt.Printf("    %s %-12s %s (%s)\n", mark, h.DisplayName(), h.Home(), state)
	}
	if found == 0 {
		fmt.Println("    (none yet — they're picked up automatically once installed and used)")
	}
	fmt.Println()

	// 0. Machine name
	fmt.Println("  Name this machine. Sessions are stored per machine, so several")
	fmt.Println("  machines can share one backup remote without stepping on each other.")
	fmt.Printf("  Machine name [%s]: ", env.Machine)
	machineName := readLine(reader)
	if machineName == "" {
		machineName = env.Machine
	}
	if machineName != env.Machine {
		if err := machine.ValidateName(machineName); err != nil {
			return err
		}
		if err := machine.SetLocalName(machineName); err != nil {
			return fmt.Errorf("save machine name: %w", err)
		}
	}
	fmt.Printf("  ✓ Machine: %s\n\n", machineName)

	// 1. Backup directory
	defaultBackup := cfg.BackupDir
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
		engine := sync.NewEngine(backupDir, machineName, nil)
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

	// 4. Write config, keeping anything already configured that the wizard
	// doesn't ask about (workspaces, harness overrides, path maps).
	newCfg := rawCfg
	if cfg.Workspace != config.DefaultWorkspace {
		ws := newCfg.Workspaces[cfg.Workspace]
		ws.BackupDir = backupDir
		ws.GitRemote = gitRemote
		ws.GitPush = config.Bool(gitPush)
		newCfg.Workspaces[cfg.Workspace] = ws
	} else {
		newCfg.BackupDir = backupDir
		newCfg.SyncOnStart = true
		newCfg.GitAutoCommit = true
		newCfg.GitRemote = gitRemote
		newCfg.GitPush = gitPush
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
		if err := reloadEnv(); err != nil {
			return err
		}
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
