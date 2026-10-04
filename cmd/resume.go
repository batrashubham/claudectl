package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var resumePrint bool

var resumeCmd = &cobra.Command{
	Use:   "resume <session-id>",
	Short: "Resume a session by ID (restores from backup if needed)",
	Long: `Resume a session in the agent that created it, restoring it from the backup
first if the agent has deleted it or it was created on another machine.

The ID may be a unique prefix, and may be qualified with the agent name
(e.g. codex:0199a2) when two agents share a prefix. Sessions from other
machines are restored into the matching local project path (see path_map
in the config).`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSessionIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := findSession(args[0])
		if err != nil {
			return err
		}
		if resumePrint {
			project, err := env.Restore(*target)
			if err != nil {
				return err
			}
			h, err := env.Harness(target.Harness)
			if err != nil {
				return err
			}
			fmt.Printf("cd %s && %s", shellQuote(project), shellQuote(env.Bin(h)))
			for _, a := range h.ResumeArgs(sessionFor(*target, project)) {
				fmt.Printf(" %s", shellQuote(a))
			}
			fmt.Println()
			return nil
		}
		return env.Resume(*target)
	},
}

func init() {
	resumeCmd.Flags().BoolVar(&resumePrint, "print", false, "Restore the session and print the resume command instead of running it")
	rootCmd.AddCommand(resumeCmd)
}
