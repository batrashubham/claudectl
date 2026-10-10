package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMachineToggle(t *testing.T) {
	backupDir := t.TempDir()
	for _, m := range []string{"home", "work"} {
		os.MkdirAll(filepath.Join(backupDir, "machines", m), 0755)
	}
	cfg := &config.Config{BackupDir: backupDir, ClaudeDir: t.TempDir(), MachineName: "home"}
	sessions := []index.SessionMeta{
		{ID: "11111111-1111-1111-1111-111111111111", Project: "/me/blog", Machine: "home", FileSize: 10, FirstPrompt: "home prompt", LastSeen: time.Now()},
		{ID: "22222222-2222-2222-2222-222222222222", Project: "/work/api", Machine: "work", FileSize: 10, FirstPrompt: "work prompt", LastSeen: time.Now()},
	}

	var model tea.Model = NewModel(cfg, sessions)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := model.View()
	if !strings.Contains(view, "home prompt") || strings.Contains(view, "work prompt") {
		t.Fatalf("default view should show only this machine:\n%s", view)
	}
	if !strings.Contains(view, "⌂ home") {
		t.Error("missing machine indicator")
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	view = model.View()
	for _, want := range []string{"home prompt", "work prompt", "all 2 machines", "⌂ work"} {
		if !strings.Contains(view, want) {
			t.Errorf("all-machines view missing %q", want)
		}
	}
}
