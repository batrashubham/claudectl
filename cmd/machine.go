package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/spf13/cobra"
)

var machineCmd = &cobra.Command{
	Use:   "machine",
	Short: "List machines in the backup",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).Build()
		if err != nil {
			return err
		}
		counts := make(map[string]int)
		for _, s := range sessions {
			if s.FileSize > 0 {
				counts[s.Machine]++
			}
		}

		names := index.Machines(cfg.BackupDir)
		if !slices.Contains(names, cfg.MachineName) {
			names = append(names, cfg.MachineName)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MACHINE\tSESSIONS\t")
		for _, n := range names {
			marker := ""
			if n == cfg.MachineName {
				marker = "← this machine"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\n", n, counts[n], marker)
		}
		return w.Flush()
	},
}

var machineRenameCmd = &cobra.Command{
	Use:   "rename <new-name>",
	Short: "Rename this machine (moves its folder in the backup)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		newName := args[0]
		if err := config.ValidateMachineName(newName); err != nil {
			return err
		}
		oldName := cfg.MachineName
		if newName == oldName {
			return nil
		}
		oldDir := filepath.Join(config.MachinesDir(cfg.BackupDir), oldName)
		newDir := filepath.Join(config.MachinesDir(cfg.BackupDir), newName)
		if _, err := os.Stat(newDir); err == nil {
			return fmt.Errorf("machine %q already exists in the backup", newName)
		}

		engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir, oldName)
		unlock, err := engine.Lock()
		if err != nil {
			return err
		}
		defer unlock()

		if _, err := os.Stat(oldDir); err == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("move %s: %w", oldDir, err)
			}
		}
		cfg.MachineName = newName
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		if err := engine.Commit(fmt.Sprintf("machine: rename %s → %s", oldName, newName)); err != nil {
			return err
		}
		fmt.Printf("Renamed %s → %s\n", oldName, newName)
		return nil
	},
}

func init() {
	machineCmd.AddCommand(machineRenameCmd)
	rootCmd.AddCommand(machineCmd)
}
