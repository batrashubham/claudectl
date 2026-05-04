package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current claudectl status overview",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus() error {
	// Config file
	configPath := config.ConfigPath()
	configExists := "exists"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configExists = "not found"
	}
	fmt.Printf("Config:     %s (%s)\n", configPath, configExists)

	// Backup directory size
	var totalSize int64
	var fileCount int
	if _, err := os.Stat(cfg.BackupDir); err == nil {
		filepath.Walk(cfg.BackupDir, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() {
				totalSize += info.Size()
				fileCount++
			}
			return nil
		})
		fmt.Printf("Backup:     %s (%s, %d files)\n", cfg.BackupDir, humanize.Bytes(uint64(totalSize)), fileCount)
	} else {
		fmt.Printf("Backup:     %s (not created)\n", cfg.BackupDir)
	}

	// Sessions count
	builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
	sessions, err := builder.Build()
	if err == nil {
		var active, archived, ghost int
		for _, s := range sessions {
			if s.FileSize == 0 {
				ghost++
			} else if s.Status == index.StatusActive {
				active++
			} else {
				archived++
			}
		}
		total := len(sessions)
		fmt.Printf("Sessions:   %d total (%d active, %d archived, %d ghost)\n", total, active, archived, ghost)
	} else {
		fmt.Printf("Sessions:   error reading index: %v\n", err)
	}

	// Last sync time (git log in backup dir)
	lastSync := lastSyncTime(cfg.BackupDir)
	if lastSync != "" {
		fmt.Printf("Last sync:  %s\n", lastSync)
	} else {
		fmt.Printf("Last sync:  never\n")
	}

	// Cron status
	cronStatus := getCronStatus()
	fmt.Printf("Cron:       %s\n", cronStatus)

	// Git remote
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

func lastSyncTime(backupDir string) string {
	cmd := exec.Command("git", "-C", backupDir, "log", "-1", "--format=%ci")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return ""
	}

	t, err := time.Parse("2006-01-02 15:04:05 -0700", line)
	if err != nil {
		return line
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
