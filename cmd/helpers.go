package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/batrashubham/claudectl/internal/app"
	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/spf13/cobra"
)

// completeSessionIDs offers session IDs (most recent first) for shell
// completion, with the opening prompt as the description.
func completeSessionIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 || env == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sessions, err := env.Index()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, s := range sessions {
		if s.IsGhost() || !strings.HasPrefix(s.ID, toComplete) {
			continue
		}
		desc := s.FirstPrompt
		if desc == "" {
			desc = s.ProjectName()
		}
		out = append(out, fmt.Sprintf("%s\t[%s] %s", s.ID, s.Harness, desc))
		if len(out) >= 50 {
			break
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeHarnessNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return harness.Names(), cobra.ShellCompDirectiveNoFileComp
}

func findSession(ref string) (*index.SessionMeta, error) {
	return env.Find(ref)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func validateHarnessFilter(name string) error {
	if name != "" && !harness.Known(name) {
		return fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(harness.Names(), ", "))
	}
	return nil
}

func sessionFor(s index.SessionMeta, project string) harness.Session {
	return harness.Session{Harness: s.Harness, ID: s.ID, Project: project, ProjectKey: s.ProjectDir}
}

func configLocation() string {
	return config.ConfigPath()
}

// reloadEnv re-reads the config after a command changed it.
func reloadEnv() error {
	var err error
	rawCfg, err = config.Load()
	if err != nil {
		return err
	}
	cfg, err = rawCfg.Resolve(rawCfg.SelectWorkspace(workspaceArg))
	if err != nil {
		return err
	}
	env, err = app.New(cfg)
	return err
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
