package cmd

import (
	"fmt"
	"os"

	"github.com/batrashubham/claudectl/internal/machines"
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
		newProject := ""
		if copyProject != "" {
			home, _ := os.UserHomeDir()
			newProject = expandPath(copyProject, home)
		}

		sessions, err := newBuilder().Build()
		if err != nil {
			return err
		}
		target := findSession(sessions, args[0])
		if target == nil {
			return fmt.Errorf("session %s not found", args[0])
		}

		out, err := machines.Copy(cfg, *target, to, newProject)
		if err != nil {
			return err
		}
		if out.Skipped {
			fmt.Printf("%s already has session %s\n", to, out.SessionID)
			return nil
		}
		fmt.Printf("Copied to %s: %s\n", to, out.Path)
		if out.SessionID != target.ID {
			fmt.Printf("New session ID: %s (project %s)\n", out.SessionID, out.Project)
		}
		if out.NewMachine {
			fmt.Printf("Note: %q isn't in your local backup yet (run 'claudectl restore' to refresh). It gets this copy once a machine with that name pulls.\n", to)
		}
		switch {
		case out.Pushed:
			fmt.Printf("Pushed. On %s, run 'claudectl restore' to pull it.\n", to)
		case to != cfg.MachineName:
			fmt.Println("git_push is off; push the backup repo yourself to share it.")
		}
		return nil
	},
}

func init() {
	copyCmd.Flags().StringVar(&copyTo, "to", "", "destination machine (default: this machine)")
	copyCmd.Flags().StringVarP(&copyProject, "project", "p", "", "project path for the copy on the destination")
	rootCmd.AddCommand(copyCmd)
}
