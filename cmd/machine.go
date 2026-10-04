package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machine"
	"github.com/spf13/cobra"
)

var machineCmd = &cobra.Command{
	Use:     "machine",
	Aliases: []string{"machines"},
	Short:   "List and name the machines sharing this backup",
	Long: `Every machine syncs into its own subtree of the backup (machines/<name>/),
so any number of machines can push to one git remote. Run 'claudectl
restore' to pull what the others have pushed; their sessions then show up
in list, search and the TUI, and can be resumed here.

When a session from another machine is resumed, its project path is
translated to this machine: first by [[path_map]] rules in the config, then
by swapping the other machine's home directory for this one's.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return machineListCmd.RunE(cmd, args)
	},
}

var machineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List machines in the backup",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		machines := machine.List(cfg.BackupDir)
		sessions, _ := env.Index()
		engine := env.Engine()

		seenLocal := false
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\tMACHINE\tOS\tHOME\tAGENTS\tSESSIONS\tLAST SYNC")
		for _, m := range machines {
			marker := ""
			if m.Name == env.Machine {
				marker = "*"
				seenLocal = true
			}
			var agents []string
			for a := range m.Harnesses {
				agents = append(agents, a)
			}
			sort.Strings(agents)
			active, archived, _ := countStatus(sessions, func(s index.SessionMeta) bool { return s.Machine == m.Name })
			last := "never"
			if t := engine.LastCommitTime(filepath.Join(machine.Dir, m.Name)); t != "" {
				last = relTime(t)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", marker, m.Name, orDash(m.OS), orDash(m.Home), orDash(strings.Join(agents, ",")), active+archived, last)
		}
		if !seenLocal {
			fmt.Fprintf(w, "*\t%s\t-\t-\t-\t-\tnot synced yet\n", env.Machine)
		}
		return w.Flush()
	},
}

var machineRenameCmd = &cobra.Command{
	Use:   "rename <new-name>",
	Short: "Rename this machine (moves its subtree in the backup)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		newName := args[0]
		if err := machine.ValidateName(newName); err != nil {
			return err
		}
		old := env.Machine
		if newName == old {
			return nil
		}
		oldDir := filepath.Join(cfg.BackupDir, machine.Dir, old)
		newDir := filepath.Join(cfg.BackupDir, machine.Dir, newName)
		if _, err := os.Stat(newDir); err == nil {
			return fmt.Errorf("machine %q already exists in the backup", newName)
		}

		unlock, err := env.Engine().Lock()
		if err != nil {
			return err
		}
		defer unlock()

		if rawCfg.Machine != "" {
			rawCfg.Machine = newName
			if err := config.Save(rawCfg); err != nil {
				return err
			}
		} else if err := machine.SetLocalName(newName); err != nil {
			return err
		}

		if _, err := os.Stat(oldDir); err == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("move backup subtree: %w", err)
			}
			if err := reloadEnv(); err != nil {
				return err
			}
			engine := env.Engine()
			if _, err := machine.WriteManifest(cfg.BackupDir, env.Manifest()); err != nil {
				return err
			}
			if cfg.GitAutoCommit {
				if err := engine.GitCommit(nil); err != nil {
					return err
				}
			}
		}
		fmt.Printf("✓ This machine is now %q (was %q)\n", newName, old)
		if cfg.GitPush {
			fmt.Println("  The rename is pushed with the next sync.")
		}
		return nil
	},
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	machineCmd.AddCommand(machineListCmd, machineRenameCmd)
	rootCmd.AddCommand(machineCmd)
}
