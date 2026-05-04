package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [session-id]",
	Short: "Resume a session by ID (restores from backup if needed)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]

		builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
		sessions, err := builder.Build()
		if err != nil {
			return err
		}

		var target *index.SessionMeta
		for i := range sessions {
			if sessions[i].ID == sessionID {
				target = &sessions[i]
				break
			}
		}

		if target == nil {
			return fmt.Errorf("session %s not found", sessionID)
		}

		locator := session.NewLocator(cfg.ClaudeDir, cfg.BackupDir)
		return locator.Resume(target.ID, target.ProjectDir, target.Project)
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}
