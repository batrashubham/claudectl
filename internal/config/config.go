package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	BackupDir     string `toml:"backup_dir"`
	ClaudeDir     string `toml:"claude_dir"`
	SyncOnStart   bool   `toml:"sync_on_start"`
	GitAutoCommit bool   `toml:"git_auto_commit"`
	GitRemote     string `toml:"git_remote"`
	GitPush       bool   `toml:"git_push"`
	TemplatesDir  string `toml:"-"` // derived from BackupDir, not stored in config
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

func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudectl", "config.toml")
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	_, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return nil, err
	}

	cfg.BackupDir = expandHome(cfg.BackupDir)
	cfg.ClaudeDir = expandHome(cfg.ClaudeDir)
	cfg.TemplatesDir = filepath.Join(cfg.BackupDir, "templates")

	return cfg, nil
}

func Init() error {
	return Save(DefaultConfig())
}

func Save(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
