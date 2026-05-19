package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	home, _ := os.UserHomeDir()

	expectedBackup := filepath.Join(home, ".claudectl", "backup")
	if cfg.BackupDir != expectedBackup {
		t.Errorf("BackupDir = %q, want %q", cfg.BackupDir, expectedBackup)
	}

	expectedClaude := filepath.Join(home, ".claude")
	if cfg.ClaudeDir != expectedClaude {
		t.Errorf("ClaudeDir = %q, want %q", cfg.ClaudeDir, expectedClaude)
	}

	if !cfg.SyncOnStart {
		t.Error("SyncOnStart should default to true")
	}

	if !cfg.GitAutoCommit {
		t.Error("GitAutoCommit should default to true")
	}

	if cfg.GitRemote != "" {
		t.Errorf("GitRemote should default to empty, got %q", cfg.GitRemote)
	}

	if cfg.GitPush {
		t.Error("GitPush should default to false")
	}

	expectedTemplates := filepath.Join(home, ".claudectl", "backup", "templates")
	if cfg.TemplatesDir != expectedTemplates {
		t.Errorf("TemplatesDir = %q, want %q", cfg.TemplatesDir, expectedTemplates)
	}
}

func TestLoad_NoFileReturnsDefaults(t *testing.T) {
	// Load() reads from ConfigPath() which is a fixed location.
	// If the config file does not exist, it should return defaults.
	// We can't easily control ConfigPath(), but we can verify the behavior
	// by checking that Load() does not error when the file is absent.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Should have default values (same as DefaultConfig)
	defaults := DefaultConfig()
	if cfg.SyncOnStart != defaults.SyncOnStart {
		t.Errorf("SyncOnStart = %v, want %v", cfg.SyncOnStart, defaults.SyncOnStart)
	}
	if cfg.GitAutoCommit != defaults.GitAutoCommit {
		t.Errorf("GitAutoCommit = %v, want %v", cfg.GitAutoCommit, defaults.GitAutoCommit)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	// Test the encoding/decoding logic by writing to a temp file and reading back.
	// We cannot easily override ConfigPath(), so we test the TOML encode/decode directly.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.toml")

	original := &Config{
		BackupDir:     "/custom/backup",
		ClaudeDir:     "/custom/claude",
		SyncOnStart:   false,
		GitAutoCommit: true,
		GitRemote:     "git@github.com:user/repo.git",
		GitPush:       true,
	}

	// Encode to file
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if err := toml.NewEncoder(f).Encode(original); err != nil {
		f.Close()
		t.Fatalf("encode config: %v", err)
	}
	f.Close()

	// Decode from file
	loaded := &Config{}
	if _, err := toml.DecodeFile(tmpFile, loaded); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if loaded.BackupDir != original.BackupDir {
		t.Errorf("BackupDir = %q, want %q", loaded.BackupDir, original.BackupDir)
	}
	if loaded.ClaudeDir != original.ClaudeDir {
		t.Errorf("ClaudeDir = %q, want %q", loaded.ClaudeDir, original.ClaudeDir)
	}
	if loaded.SyncOnStart != original.SyncOnStart {
		t.Errorf("SyncOnStart = %v, want %v", loaded.SyncOnStart, original.SyncOnStart)
	}
	if loaded.GitAutoCommit != original.GitAutoCommit {
		t.Errorf("GitAutoCommit = %v, want %v", loaded.GitAutoCommit, original.GitAutoCommit)
	}
	if loaded.GitRemote != original.GitRemote {
		t.Errorf("GitRemote = %q, want %q", loaded.GitRemote, original.GitRemote)
	}
	if loaded.GitPush != original.GitPush {
		t.Errorf("GitPush = %v, want %v", loaded.GitPush, original.GitPush)
	}
	// TemplatesDir is derived from BackupDir, not stored in TOML
	if loaded.TemplatesDir != "" {
		t.Errorf("TemplatesDir should be empty from TOML decode (derived field), got %q", loaded.TemplatesDir)
	}
}

func TestExpandHome_TildePrefix(t *testing.T) {
	home, _ := os.UserHomeDir()

	result := expandHome("~/foo")
	expected := filepath.Join(home, "foo")
	if result != expected {
		t.Errorf("expandHome(\"~/foo\") = %q, want %q", result, expected)
	}
}

func TestExpandHome_AbsolutePathUnchanged(t *testing.T) {
	input := "/absolute/path"
	result := expandHome(input)
	if result != input {
		t.Errorf("expandHome(%q) = %q, want unchanged", input, result)
	}
}

func TestExpandHome_RelativePathUnchanged(t *testing.T) {
	input := "relative/path"
	result := expandHome(input)
	if result != input {
		t.Errorf("expandHome(%q) = %q, want unchanged", input, result)
	}
}

func TestExpandHome_TildeOnly(t *testing.T) {
	// "~" alone (without /) should stay unchanged because the function
	// only checks for "~/" prefix (len > 1 and path[:2] == "~/")
	input := "~"
	result := expandHome(input)
	if result != input {
		t.Errorf("expandHome(%q) = %q, want unchanged", input, result)
	}
}

func TestExpandHome_TildeSubpath(t *testing.T) {
	home, _ := os.UserHomeDir()

	result := expandHome("~/deeply/nested/path")
	if !strings.HasPrefix(result, home) {
		t.Errorf("expandHome(\"~/deeply/nested/path\") = %q, should start with %q", result, home)
	}
	if !strings.HasSuffix(result, "deeply/nested/path") {
		t.Errorf("expandHome result %q should end with deeply/nested/path", result)
	}
}

func TestConfigPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".claudectl", "config.toml")
	if ConfigPath() != expected {
		t.Errorf("ConfigPath() = %q, want %q", ConfigPath(), expected)
	}
}
