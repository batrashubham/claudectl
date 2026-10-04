package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func decode(t *testing.T, src string) *Config {
	t.Helper()
	cfg := DefaultConfig()
	if _, err := toml.Decode(src, cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestResolve_DefaultKeepsLegacyClaudeDir(t *testing.T) {
	cfg := decode(t, `claude_dir = "/legacy/claude"`)
	r, err := cfg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if r.Harnesses["claude"].Home != "/legacy/claude" || r.ClaudeDir != "/legacy/claude" {
		t.Errorf("claude home = %q", r.Harnesses["claude"].Home)
	}
}

func TestResolve_HarnessHomeBeatsClaudeDir(t *testing.T) {
	cfg := decode(t, "claude_dir = \"/legacy\"\n[harnesses.claude]\nhome = \"/new\"\n")
	r, _ := cfg.Resolve("")
	if r.ClaudeDir != "/new" {
		t.Errorf("ClaudeDir = %q, want explicit harness home", r.ClaudeDir)
	}
}

func TestResolve_NamedWorkspaceInheritsNothingRisky(t *testing.T) {
	t.Setenv("CLAUDECTL_HOME", "/cctl")
	cfg := decode(t, `
git_remote = "git@personal:backup.git"
git_push = true
backup_dir = "/personal/backup"

[workspaces.work]
[workspaces.work.harnesses.claude]
home = "/home/me/.claude-work"
`)
	r, err := cfg.Resolve("work")
	if err != nil {
		t.Fatal(err)
	}
	if r.GitRemote != "" || r.GitPush {
		t.Errorf("work workspace must not inherit the personal remote: %q push=%v", r.GitRemote, r.GitPush)
	}
	if r.BackupDir != filepath.Join("/cctl", "workspaces", "work", "backup") {
		t.Errorf("BackupDir = %q", r.BackupDir)
	}
	if r.TemplatesDir != filepath.Join(r.BackupDir, "templates") {
		t.Errorf("TemplatesDir = %q", r.TemplatesDir)
	}
	if r.ClaudeDir != "/home/me/.claude-work" || r.Workspace != "work" {
		t.Errorf("resolved = %+v", r)
	}
	if _, err := cfg.Resolve("nope"); err == nil {
		t.Error("unknown workspace should error")
	}
}

func TestSelectWorkspace_Precedence(t *testing.T) {
	cfg := decode(t, "default_workspace = \"b\"\n[workspaces.a]\n[workspaces.b]\n")
	t.Setenv("CLAUDECTL_WORKSPACE", "")
	if got := cfg.SelectWorkspace(""); got != "b" {
		t.Errorf("config default: got %q", got)
	}
	t.Setenv("CLAUDECTL_WORKSPACE", "a")
	if got := cfg.SelectWorkspace(""); got != "a" {
		t.Errorf("env beats config: got %q", got)
	}
	if got := cfg.SelectWorkspace("default"); got != "default" {
		t.Errorf("flag beats env: got %q", got)
	}
}

func TestValidate_RejectsBadConfig(t *testing.T) {
	for name, src := range map[string]string{
		"undefined default": `default_workspace = "ghost"`,
		"bad name":          "[workspaces.\"has space\"]\n",
		"reserved name":     "[workspaces.default]\n",
		"half path map":     "[[path_map]]\nfrom = \"/a\"\n",
	} {
		cfg := DefaultConfig()
		if _, err := toml.Decode(src, cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Validate() == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestSave_RoundTripsWorkspaces(t *testing.T) {
	t.Setenv("CLAUDECTL_HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Workspaces = map[string]Workspace{"work": {GitRemote: "r", GitPush: Bool(true)}}
	cfg.PathMap = []PathMap{{From: "/Users/me", To: "/home/me"}}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workspaces["work"].GitRemote != "r" || len(loaded.PathMap) != 1 {
		t.Errorf("loaded = %+v", loaded)
	}
	if _, err := os.Stat(ConfigPath() + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
}
