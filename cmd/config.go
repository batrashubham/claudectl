package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the effective configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Config file:     %s\n", config.ConfigPath())
		fmt.Printf("Workspace:       %s\n", cfg.Workspace)
		fmt.Printf("Machine:         %s\n", env.Machine)
		fmt.Printf("Backup dir:      %s\n", cfg.BackupDir)
		fmt.Printf("Templates dir:   %s\n", cfg.TemplatesDir)
		fmt.Printf("Sync on start:   %v\n", cfg.SyncOnStart)
		fmt.Printf("Git auto-commit: %v\n", cfg.GitAutoCommit)
		fmt.Printf("Git remote:      %s\n", cfg.GitRemote)
		fmt.Printf("Git push:        %v\n", cfg.GitPush)
		fmt.Println("Agents:")
		for _, h := range env.All {
			state := "off (not found)"
			if env.IsEnabled(h.Name()) {
				state = "on"
			} else if cfg.Harnesses[h.Name()].Enabled != nil {
				state = "off (config)"
			}
			fmt.Printf("  %-12s %-16s %s  [bin: %s]\n", h.DisplayName(), state, h.Home(), env.Bin(h))
		}
		if len(cfg.PathMap) > 0 {
			fmt.Println("Path map:")
			for _, m := range cfg.PathMap {
				fmt.Printf("  %s → %s\n", m.From, m.To)
			}
		}
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default config file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !needsSetup() {
			return fmt.Errorf("%s already exists", config.ConfigPath())
		}
		if err := config.Init(); err != nil {
			return err
		}
		fmt.Printf("Config created at %s\n", config.ConfigPath())
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(config.ConfigPath())
	},
}

func init() {
	configCmd.AddCommand(configInitCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
