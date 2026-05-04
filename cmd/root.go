package cmd

import (
	"fmt"
	"os"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/spf13/cobra"
)

var cfg *config.Config

var rootCmd = &cobra.Command{
	Use:   "claudectl",
	Short: "Manage, backup, and resume Claude Code sessions",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if needsSetup() {
			fmt.Println("Using defaults. Run 'claudectl setup' to customize.")
		}
		return runTUI()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
