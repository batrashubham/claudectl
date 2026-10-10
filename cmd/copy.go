package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/spf13/cobra"
)

var (
	copyTo      string
	copyProject string
)

var copyCmd = &cobra.Command{
	Use:   "copy <session-id>",
	Short: "Copy a session to another machine's backup",
	Long: `Copy a session into another machine's folder in the backup, then commit
(and push, if git_push is on). The other machine sees it after 'claudectl restore'.

--project puts the copy under a different project path, e.g. when your home
directory differs between machines. The copy then gets a new session ID.

  claudectl copy 3f2a... --to personal
  claudectl copy 3f2a... --to personal --project /Users/me/code/app
  claudectl copy 3f2a... --project ~/code/app-v2      # relocate on this machine`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		to := copyTo
		if to == "" {
			to = cfg.MachineName
		}
		if err := config.ValidateMachineName(to); err != nil {
			return err
		}
		newProject := ""
		if copyProject != "" {
			home, _ := os.UserHomeDir()
			newProject = expandPath(copyProject, home)
		}
		if to == cfg.MachineName && newProject == "" {
			return fmt.Errorf("nothing to copy: give --to <machine> and/or --project <path>")
		}

		sessions, err := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).Build()
		if err != nil {
			return err
		}
		var target *index.SessionMeta
		for i := range sessions {
			if sessions[i].ID == args[0] {
				target = &sessions[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("session %s not found", args[0])
		}

		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName)
		unlock, err := engine.Lock()
		if err != nil {
			return err
		}
		res, err := session.NewLocator(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).Copy(session.CopyRequest{
			SessionID:  target.ID,
			ProjectDir: target.ProjectDir,
			Project:    target.Project,
			ToMachine:  to,
			NewProject: newProject,
		})
		if err != nil {
			unlock()
			return err
		}
		if res.Skipped {
			unlock()
			fmt.Printf("%s already has session %s\n", to, res.SessionID)
			return nil
		}
		commitErr := engine.Commit(fmt.Sprintf("copy(%s → %s): %s", cfg.MachineName, to, res.SessionID))
		unlock()
		if commitErr != nil {
			return commitErr
		}

		fmt.Printf("Copied to %s: %s\n", to, res.Path)
		if res.SessionID != target.ID {
			fmt.Printf("New session ID: %s (project %s)\n", res.SessionID, res.Project)
		}
		if !slices.Contains(index.Machines(cfg.BackupDir), to) {
			fmt.Printf("Note: no machine named %q has synced yet; it will see this once it uses that machine_name.\n", to)
		}
		if to != cfg.MachineName {
			if cfg.GitPush {
				if err := engine.GitPush(); err != nil {
					return err
				}
				fmt.Printf("Pushed. On %s, run 'claudectl restore' to pull it.\n", to)
			} else {
				fmt.Println("git_push is off; push the backup repo yourself to share it.")
			}
		}
		return nil
	},
}

func init() {
	copyCmd.Flags().StringVar(&copyTo, "to", "", "destination machine (default: this machine)")
	copyCmd.Flags().StringVarP(&copyProject, "project", "p", "", "project path for the copy on the destination")
	rootCmd.AddCommand(copyCmd)
}
