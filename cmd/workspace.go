package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Manage workspaces (independent backup profiles, e.g. work vs personal)",
	Long: `A workspace is an independent backup profile: its own backup repo, git
remote, and agent data directories. Use one per account or trust boundary,
e.g. a work workspace whose Claude Code data lives in ~/.claude-work and
pushes to a company remote, next to your personal default.

Named workspaces inherit nothing from the top-level config, so sessions
can't leak into the wrong remote. The top-level config is the "default"
workspace.

Select a workspace per command with --workspace, per shell with
CLAUDECTL_WORKSPACE, or persistently with 'claudectl workspace use'.`,
	Example: `  claudectl workspace add work --home claude=~/.claude-work --remote git@github.com:acme/agent-backup.git --push
  claudectl --workspace work sync
  claudectl workspace use work`,
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspaces",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\tWORKSPACE\tBACKUP\tREMOTE\tAGENT DIRS")
		for _, name := range rawCfg.WorkspaceNames() {
			r, err := rawCfg.Resolve(name)
			if err != nil {
				return err
			}
			marker := ""
			if name == cfg.Workspace {
				marker = "*"
			}
			remote := r.GitRemote
			if remote == "" {
				remote = "-"
			} else if !r.GitPush {
				remote += " (no push)"
			}
			var homes []string
			for _, h := range harness.Names() {
				if hc, ok := r.Harnesses[h]; ok && (hc.Home != "" || hc.Enabled != nil) {
					s := h + "=" + hc.Home
					if hc.Enabled != nil && !*hc.Enabled {
						s = h + "=off"
					}
					homes = append(homes, s)
				}
			}
			dirs := "defaults"
			if len(homes) > 0 {
				dirs = strings.Join(homes, " ")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", marker, name, r.BackupDir, remote, dirs)
		}
		return w.Flush()
	},
}

var (
	wsBackupDir string
	wsRemote    string
	wsPush      bool
	wsHomes     []string
	wsDisable   []string
)

var workspaceAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := config.ValidateWorkspaceName(name); err != nil {
			return err
		}
		if _, ok := rawCfg.Workspaces[name]; ok {
			return fmt.Errorf("workspace %q already exists", name)
		}
		ws := config.Workspace{
			BackupDir: wsBackupDir,
			GitRemote: wsRemote,
			Harnesses: map[string]config.HarnessConfig{},
		}
		if wsPush {
			if wsRemote == "" {
				return fmt.Errorf("--push needs --remote")
			}
			ws.GitPush = config.Bool(true)
		}
		for _, spec := range wsHomes {
			agent, dir, ok := strings.Cut(spec, "=")
			if !ok || dir == "" {
				return fmt.Errorf("--home expects agent=dir, got %q", spec)
			}
			if !harness.Known(agent) {
				return fmt.Errorf("unknown agent %q (known: %s)", agent, strings.Join(harness.Names(), ", "))
			}
			hc := ws.Harnesses[agent]
			hc.Home = dir
			ws.Harnesses[agent] = hc
		}
		for _, agent := range wsDisable {
			if !harness.Known(agent) {
				return fmt.Errorf("unknown agent %q", agent)
			}
			hc := ws.Harnesses[agent]
			hc.Enabled = config.Bool(false)
			ws.Harnesses[agent] = hc
		}
		if len(ws.Harnesses) == 0 {
			ws.Harnesses = nil
		}
		if rawCfg.Workspaces == nil {
			rawCfg.Workspaces = map[string]config.Workspace{}
		}
		rawCfg.Workspaces[name] = ws
		if err := config.Save(rawCfg); err != nil {
			return err
		}
		r, _ := rawCfg.Resolve(name)
		fmt.Printf("✓ Workspace %q created (backup: %s)\n", name, r.BackupDir)
		if len(wsHomes) == 0 {
			fmt.Println("  It reads every agent's default data dir. Point it elsewhere with --home agent=dir,")
			fmt.Println("  or it will back up the same sessions as your default workspace.")
		}
		fmt.Printf("  Use it with: claudectl --workspace %s sync   (or: claudectl workspace use %s)\n", name, name)
		return nil
	},
}

var workspaceUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Make a workspace the default for future commands",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return rawCfg.WorkspaceNames(), cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name != config.DefaultWorkspace {
			if _, ok := rawCfg.Workspaces[name]; !ok {
				return fmt.Errorf("workspace %q not found (have: %v)", name, rawCfg.WorkspaceNames())
			}
		}
		rawCfg.DefaultWorkspace = name
		if name == config.DefaultWorkspace {
			rawCfg.DefaultWorkspace = ""
		}
		if err := config.Save(rawCfg); err != nil {
			return err
		}
		fmt.Printf("✓ Now using workspace %q\n", name)
		return nil
	},
}

var workspaceRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a workspace from the config (its backup is left on disk)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name == config.DefaultWorkspace {
			return fmt.Errorf("the default workspace can't be removed")
		}
		if _, ok := rawCfg.Workspaces[name]; !ok {
			return fmt.Errorf("workspace %q not found", name)
		}
		r, _ := rawCfg.Resolve(name)
		delete(rawCfg.Workspaces, name)
		if rawCfg.DefaultWorkspace == name {
			rawCfg.DefaultWorkspace = ""
		}
		if err := config.Save(rawCfg); err != nil {
			return err
		}
		fmt.Printf("✓ Removed workspace %q. Its backup is still at %s\n", name, r.BackupDir)
		return nil
	},
}

func init() {
	workspaceAddCmd.Flags().StringVar(&wsBackupDir, "backup-dir", "", "Backup directory (default ~/.claudectl/workspaces/<name>/backup)")
	workspaceAddCmd.Flags().StringVar(&wsRemote, "remote", "", "Git remote for this workspace's backup")
	workspaceAddCmd.Flags().BoolVar(&wsPush, "push", false, "Push after each sync")
	workspaceAddCmd.Flags().StringArrayVar(&wsHomes, "home", nil, "Agent data dir as agent=dir (repeatable), e.g. claude=~/.claude-work")
	workspaceAddCmd.Flags().StringArrayVar(&wsDisable, "disable", nil, "Agent to exclude from this workspace (repeatable)")

	workspaceCmd.AddCommand(workspaceListCmd, workspaceAddCmd, workspaceUseCmd, workspaceRemoveCmd)
	rootCmd.AddCommand(workspaceCmd)
}
