package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Config file:     %s\n", config.ConfigPath())
		fmt.Printf("Claude dir:      %s\n", cfg.ClaudeDir)
		fmt.Printf("Backup dir:      %s\n", cfg.BackupDir)
		fmt.Printf("Sync on start:   %v\n", cfg.SyncOnStart)
		fmt.Printf("Git auto-commit: %v\n", cfg.GitAutoCommit)
		fmt.Printf("Git remote:      %s\n", cfg.GitRemote)
		fmt.Printf("Git push:        %v\n", cfg.GitPush)
		fmt.Printf("Templates dir:   %s\n", cfg.TemplatesDir)
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Init(); err != nil {
			return err
		}
		fmt.Printf("Config created at %s\n", config.ConfigPath())
		return nil
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
	rootCmd.AddCommand(configCmd)
}
