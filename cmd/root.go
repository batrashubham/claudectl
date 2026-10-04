package cmd

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/batrashubham/claudectl/internal/app"
	"github.com/batrashubham/claudectl/internal/config"
	"github.com/spf13/cobra"
)

var (
	// rawCfg is the config file as written; save this one. cfg is resolved
	// for the selected workspace and is what commands read.
	rawCfg       *config.Config
	cfg          *config.Config
	env          *app.Env
	workspaceArg string
)

var rootCmd = &cobra.Command{
	Use:   "claudectl",
	Short: "Back up, search, and resume coding-agent sessions (Claude Code, Codex, Gemini CLI, opencode)",
	Long: `claudectl backs up the sessions of your coding agents to a git repo, indexes
them for search, and resumes any of them — even ones the agent has deleted —
across every machine that shares the backup.

Supported agents: Claude Code, Codex, Gemini CLI, opencode. Each is picked
up automatically when its data directory exists.`,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		rawCfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg, err = rawCfg.Resolve(rawCfg.SelectWorkspace(workspaceArg))
		if err != nil {
			return err
		}
		env, err = app.New(cfg)
		if err != nil {
			return err
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

func Execute(version string) {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	app.Version = version
	rootCmd.Version = version

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.SilenceErrors = true
	rootCmd.PersistentFlags().StringVar(&workspaceArg, "workspace", "", "Workspace to use (default: $CLAUDECTL_WORKSPACE, then default_workspace)")
	rootCmd.RegisterFlagCompletionFunc("workspace", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		c, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return c.WorkspaceNames(), cobra.ShellCompDirectiveNoFileComp
	})
}
