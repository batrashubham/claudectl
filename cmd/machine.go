package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/machines"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var machineCmd = &cobra.Command{
	Use:   "machine",
	Short: "List machines in the backup",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := newBuilder().Build()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MACHINE\tSESSIONS\tSIZE\tLAST SYNC\t")
		for _, s := range machines.Summarize(cfg, sessions) {
			last, marker := "never", ""
			if !s.LastSync.IsZero() {
				last = humanize.Time(s.LastSync)
			}
			if s.This {
				marker = "← this machine"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", s.Name, s.Sessions, humanize.Bytes(uint64(s.Bytes)), last, marker)
		}
		return w.Flush()
	},
}

var machineRenameCmd = &cobra.Command{
	Use:   "rename <new-name>",
	Short: "Rename this machine (moves its folder in the backup)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldName := cfg.MachineName
		if err := machines.Rename(cfg, args[0]); err != nil {
			return err
		}
		fmt.Printf("Renamed %s → %s\n", oldName, cfg.MachineName)
		return nil
	},
}

func init() {
	machineCmd.AddCommand(machineRenameCmd)
	rootCmd.AddCommand(machineCmd)
}
