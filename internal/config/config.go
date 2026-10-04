package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"
)

const DefaultWorkspace = "default"

// HarnessConfig tunes one coding agent. Enabled nil means auto: the agent
// is included whenever its data directory exists.
type HarnessConfig struct {
	Enabled *bool  `toml:"enabled,omitempty"`
	Home    string `toml:"home,omitempty"`
	Bin     string `toml:"bin,omitempty"`
}

// PathMap rewrites project paths recorded on another machine to where the
// same checkout lives on this one, e.g. /Users/me/code -> /home/me/code.
type PathMap struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// Workspace is an independent backup profile: its own backup repo, remote
// and agent data directories. Nothing is inherited from the top level, so
// a work workspace can never push to a personal remote by accident.
type Workspace struct {
	BackupDir     string                   `toml:"backup_dir,omitempty"`
	SyncOnStart   *bool                    `toml:"sync_on_start,omitempty"`
	GitAutoCommit *bool                    `toml:"git_auto_commit,omitempty"`
	GitRemote     string                   `toml:"git_remote,omitempty"`
	GitPush       *bool                    `toml:"git_push,omitempty"`
	Harnesses     map[string]HarnessConfig `toml:"harnesses,omitempty"`
}

type Config struct {
	// Machine names this computer inside the backup. Empty means use the
	// name stored in ~/.claudectl/machine (derived from the hostname).
	Machine          string `toml:"machine,omitempty"`
	DefaultWorkspace string `toml:"default_workspace,omitempty"`

	BackupDir     string `toml:"backup_dir"`
	ClaudeDir     string `toml:"claude_dir"` // shorthand for [harnesses.claude] home
	SyncOnStart   bool   `toml:"sync_on_start"`
	GitAutoCommit bool   `toml:"git_auto_commit"`
	GitRemote     string `toml:"git_remote"`
	GitPush       bool   `toml:"git_push"`

	Harnesses  map[string]HarnessConfig `toml:"harnesses,omitempty"`
	PathMap    []PathMap                `toml:"path_map,omitempty"`
	Workspaces map[string]Workspace     `toml:"workspaces,omitempty"`

	Workspace    string `toml:"-"` // which workspace this config was resolved for
	TemplatesDir string `toml:"-"` // derived from BackupDir, not stored in config
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	backupDir := filepath.Join(home, ".claudectl", "backup")
	return &Config{
		BackupDir:     backupDir,
		ClaudeDir:     defaultClaudeDir(),
		SyncOnStart:   true,
		GitAutoCommit: true,
		GitRemote:     "",
		GitPush:       false,
		Workspace:     DefaultWorkspace,
		TemplatesDir:  filepath.Join(backupDir, "templates"),
	}
}

// defaultClaudeDir respects CLAUDE_CONFIG_DIR if set, else ~/.claude
func defaultClaudeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// Dir is claudectl's own state directory. CLAUDECTL_HOME relocates it,
// which also keeps tests and sandboxes away from the real one.
func Dir() string {
	if dir := os.Getenv("CLAUDECTL_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudectl")
}

func ConfigPath() string {
	return filepath.Join(Dir(), "config.toml")
}

// Load reads the config file as written, without resolving a workspace.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	cfg.BackupDir = expandHome(cfg.BackupDir)
	cfg.ClaudeDir = expandHome(cfg.ClaudeDir)
	cfg.TemplatesDir = filepath.Join(cfg.BackupDir, "templates")
	for i := range cfg.PathMap {
		cfg.PathMap[i].From = expandHome(cfg.PathMap[i].From)
		cfg.PathMap[i].To = expandHome(cfg.PathMap[i].To)
	}

	return cfg, cfg.Validate()
}

var validWorkspace = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func ValidateWorkspaceName(name string) error {
	if !validWorkspace.MatchString(name) {
		return fmt.Errorf("invalid workspace name %q: use letters, digits, '-' and '_'", name)
	}
	if name == DefaultWorkspace {
		return fmt.Errorf("%q is reserved for the top-level config", DefaultWorkspace)
	}
	return nil
}

func (c *Config) Validate() error {
	for name := range c.Workspaces {
		if err := ValidateWorkspaceName(name); err != nil {
			return err
		}
	}
	if c.DefaultWorkspace != "" && c.DefaultWorkspace != DefaultWorkspace {
		if _, ok := c.Workspaces[c.DefaultWorkspace]; !ok {
			return fmt.Errorf("default_workspace %q is not defined under [workspaces]", c.DefaultWorkspace)
		}
	}
	for _, m := range c.PathMap {
		if m.From == "" || m.To == "" {
			return fmt.Errorf("path_map entries need both from and to")
		}
	}
	return nil
}

// WorkspaceNames lists "default" followed by named workspaces, sorted.
func (c *Config) WorkspaceNames() []string {
	names := []string{}
	for n := range c.Workspaces {
		names = append(names, n)
	}
	sort.Strings(names)
	return append([]string{DefaultWorkspace}, names...)
}

// SelectWorkspace picks the workspace to use: an explicit choice, then
// CLAUDECTL_WORKSPACE, then default_workspace, then the top level.
func (c *Config) SelectWorkspace(explicit string) string {
	for _, w := range []string{explicit, os.Getenv("CLAUDECTL_WORKSPACE"), c.DefaultWorkspace} {
		if w != "" {
			return w
		}
	}
	return DefaultWorkspace
}

// Resolve returns the effective config for a workspace. The result is for
// reading only; save the unresolved config to persist changes.
func (c *Config) Resolve(workspace string) (*Config, error) {
	r := *c
	r.Workspace = DefaultWorkspace
	r.Harnesses = map[string]HarnessConfig{}
	for k, v := range c.Harnesses {
		r.Harnesses[k] = v
	}
	if workspace != "" && workspace != DefaultWorkspace {
		ws, ok := c.Workspaces[workspace]
		if !ok {
			return nil, fmt.Errorf("workspace %q not found (have: %v)", workspace, c.WorkspaceNames())
		}
		def := DefaultConfig()
		r.Workspace = workspace
		r.BackupDir = expandHome(ws.BackupDir)
		if r.BackupDir == "" {
			r.BackupDir = filepath.Join(Dir(), "workspaces", workspace, "backup")
		}
		r.SyncOnStart = boolOr(ws.SyncOnStart, def.SyncOnStart)
		r.GitAutoCommit = boolOr(ws.GitAutoCommit, def.GitAutoCommit)
		r.GitRemote = ws.GitRemote
		r.GitPush = boolOr(ws.GitPush, def.GitPush)
		r.ClaudeDir = def.ClaudeDir
		r.Harnesses = map[string]HarnessConfig{}
		for k, v := range ws.Harnesses {
			r.Harnesses[k] = v
		}
	}
	// claude_dir predates [harnesses]; an explicit harness home wins.
	if hc := r.Harnesses["claude"]; hc.Home == "" && r.ClaudeDir != "" {
		hc.Home = r.ClaudeDir
		r.Harnesses["claude"] = hc
	}
	for k, v := range r.Harnesses {
		v.Home = expandHome(v.Home)
		r.Harnesses[k] = v
	}
	r.ClaudeDir = r.Harnesses["claude"].Home
	r.TemplatesDir = filepath.Join(r.BackupDir, "templates")
	return &r, nil
}

func boolOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

func Bool(b bool) *bool { return &b }

func Init() error {
	return Save(DefaultConfig())
}

// Save writes the config atomically so an interrupted write can't leave a
// truncated file behind.
func Save(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func ExpandHome(path string) string { return expandHome(path) }

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
