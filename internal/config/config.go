package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	BackupDir     string `toml:"backup_dir"`
	ClaudeDir     string `toml:"claude_dir"`
	SyncOnStart   bool   `toml:"sync_on_start"`
	GitAutoCommit bool   `toml:"git_auto_commit"`
	GitRemote     string `toml:"git_remote"`
	GitPush       bool   `toml:"git_push"`
	MachineName   string `toml:"machine_name"`
	TemplatesDir  string `toml:"-"` // derived from BackupDir, not stored in config

	// MachineNameDefaulted is set when machine_name was absent and the
	// hostname was used instead.
	MachineNameDefaulted bool `toml:"-"`
}

// MachineDir is where this machine's sessions live in the backup.
func (c *Config) MachineDir() string {
	return filepath.Join(MachinesDir(c.BackupDir), c.MachineName)
}

func MachinesDir(backupDir string) string {
	return filepath.Join(backupDir, "machines")
}

var validMachine = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func ValidateMachineName(name string) error {
	if !validMachine.MatchString(name) {
		return fmt.Errorf("machine name %q must be lowercase letters, digits and hyphens (e.g. 'work-laptop')", name)
	}
	return nil
}

// DefaultMachineName derives a valid machine name from the hostname.
func DefaultMachineName() string {
	host, _ := os.Hostname()
	host = strings.ToLower(strings.TrimSuffix(host, ".local"))
	host = strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(host, "-"), "-")
	if host == "" {
		return "default"
	}
	return host
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

func (c *Config) defaultMachine() {
	if c.MachineName == "" {
		c.MachineName = DefaultMachineName()
		c.MachineNameDefaulted = true
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
		cfg.defaultMachine()
		return cfg, nil
	}

	_, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return nil, err
	}

	cfg.BackupDir = expandHome(cfg.BackupDir)
	cfg.ClaudeDir = expandHome(cfg.ClaudeDir)
	cfg.TemplatesDir = filepath.Join(cfg.BackupDir, "templates")
	cfg.defaultMachine()

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
