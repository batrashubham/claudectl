package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/batrashubham/claudectl/internal/app"
	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	tea "github.com/charmbracelet/bubbletea"
)

func testModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("CLAUDECTL_HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.BackupDir = t.TempDir()
	cfg.TemplatesDir = cfg.BackupDir + "/templates"
	cfg.SyncOnStart = false
	env, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sessions := []index.SessionMeta{
		{ID: "a", Harness: "claude", Machine: "laptop", Project: "/code/api", FirstPrompt: "fix it", LastSeen: now, FileSize: 10},
		{ID: "ses_short", Harness: "opencode", Machine: "desktop", Project: "", ProjectDir: "global", LastSeen: now, FileSize: 5, Status: index.StatusArchived},
		{ID: "ghost-session-id", Harness: "codex", Machine: "laptop", Project: "/code/api", FileSize: 0},
	}
	m := NewModel(env, sessions)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model)
}

func TestView_ShowsAgentsAndMachines(t *testing.T) {
	m := testModel(t)
	out := m.View()
	for _, want := range []string{"AGENTS", "MACHINES", "Claude Code", "@desktop", "opencode", "3 agents", "2 machines"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	// Short IDs must not panic in the detail view.
	m.cursor = 1
	m.state = detailView
	if !strings.Contains(m.View(), "opencode") {
		t.Error("detail view should name the agent")
	}
}

func TestSidebar_FiltersByAgent(t *testing.T) {
	m := testModel(t)
	for i, item := range m.sidebarItems {
		if item.kind == kindHarness && item.value == "opencode" {
			m.sidebarCursor = i
		}
	}
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].Harness != "opencode" {
		t.Errorf("filtered = %+v", m.filtered)
	}
}

func TestResume_ReturnsQualifiedKey(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if got := next.(Model).ResumeID(); got != "claude:a" {
		t.Errorf("ResumeID = %q", got)
	}
}
