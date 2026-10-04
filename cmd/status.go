package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/hook"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machine"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show backup health, agents, machines, and automation at a glance",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus() error {
	configPath := config.ConfigPath()
	configExists := "exists"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configExists = "not found, using defaults"
	}
	fmt.Printf("Config:     %s (%s)\n", configPath, configExists)
	fmt.Printf("Workspace:  %s\n", cfg.Workspace)
	fmt.Printf("Machine:    %s\n", env.Machine)

	if _, err := os.Stat(cfg.BackupDir); err == nil {
		size, files := dirSize(cfg.BackupDir)
		fmt.Printf("Backup:     %s (%s, %d files)\n", cfg.BackupDir, humanize.Bytes(uint64(size)), files)
	} else {
		fmt.Printf("Backup:     %s (not created yet — run 'claudectl sync')\n", cfg.BackupDir)
	}

	sessions, err := env.Index()
	if err != nil {
		fmt.Printf("Sessions:   error reading index: %v\n", err)
	}

	fmt.Println()
	fmt.Println("Agents:")
	for _, h := range env.All {
		state := "not found"
		switch {
		case env.IsEnabled(h.Name()):
			state = "syncing"
		case cfg.Harnesses[h.Name()].Enabled != nil:
			state = "disabled in config"
		}
		bin := "binary not in PATH"
		if _, err := exec.LookPath(env.Bin(h)); err == nil {
			bin = env.Bin(h) + " in PATH"
		}
		active, archived, ghost := countStatus(sessions, func(s index.SessionMeta) bool { return s.Harness == h.Name() })
		fmt.Printf("  %-12s %-20s %s (%s)\n", h.DisplayName(), state, h.Home(), bin)
		if active+archived+ghost > 0 {
			fmt.Printf("  %-12s %d active, %d archived, %d ghost\n", "", active, archived, ghost)
		}
	}

	fmt.Println()
	fmt.Println("Machines:")
	engine := env.Engine()
	machines := machine.List(cfg.BackupDir)
	if len(machines) == 0 {
		fmt.Printf("  %s (this machine, not synced yet)\n", env.Machine)
	}
	for _, m := range machines {
		label := m.Name
		if m.Name == env.Machine {
			label += " (this machine)"
		}
		last := "never"
		if t := engine.LastCommitTime(filepath.Join(machine.Dir, m.Name)); t != "" {
			last = relTime(t)
		}
		active, archived, _ := countStatus(sessions, func(s index.SessionMeta) bool { return s.Machine == m.Name })
		fmt.Printf("  %-28s %d sessions, last sync %s\n", label, active+archived, last)
	}

	fmt.Println()
	if t := engine.LastCommitTime(""); t != "" {
		fmt.Printf("Last sync:  %s\n", relTime(t))
	} else {
		fmt.Printf("Last sync:  never\n")
	}
	hook.SetClaudeDir(cfg.ClaudeDir)
	if installed, _, err := hook.Installed(sessionEndEvent, cfg.Workspace); err == nil && installed {
		fmt.Printf("Hook:       active (Claude Code SessionEnd)\n")
	} else {
		fmt.Printf("Hook:       not installed\n")
	}
	fmt.Printf("Cron:       %s\n", getCronStatus())

	if cfg.GitRemote != "" {
		pushStatus := "push disabled"
		if cfg.GitPush {
			pushStatus = "push enabled"
		}
		fmt.Printf("Git remote: %s (%s)\n", cfg.GitRemote, pushStatus)
	} else {
		fmt.Printf("Git remote: not configured\n")
	}
	return nil
}

func countStatus(sessions []index.SessionMeta, match func(index.SessionMeta) bool) (active, archived, ghost int) {
	for _, s := range sessions {
		if !match(s) {
			continue
		}
		switch {
		case s.IsGhost():
			ghost++
		case s.Status == index.StatusActive:
			active++
		default:
			archived++
		}
	}
	return
}

func dirSize(dir string) (int64, int) {
	var total int64
	var files int
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
			files++
		}
		return nil
	})
	return total, files
}

func relTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return humanize.RelTime(t, time.Now(), "ago", "from now")
}

func getCronStatus() string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "not configured"
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "claudectl") {
			// Extract interval from cron expression like "*/5 * * * *"
			parts := strings.Fields(line)
			if len(parts) > 0 {
				cronExpr := parts[0]
				if strings.HasPrefix(cronExpr, "*/") {
					interval := strings.TrimPrefix(cronExpr, "*/")
					return fmt.Sprintf("active (every %s min)", interval)
				}
			}
			return "active"
		}
	}
	return "not installed"
}
