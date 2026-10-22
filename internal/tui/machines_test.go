package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	homeSID = "11111111-1111-1111-1111-111111111111"
	workSID = "22222222-2222-2222-2222-222222222222"
)

func machineFixture(t *testing.T) (*config.Config, tea.Model) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg := &config.Config{ClaudeDir: filepath.Join(tmp, "claude"), BackupDir: filepath.Join(tmp, "backup"), MachineName: "home"}
	cfg.TemplatesDir = filepath.Join(cfg.BackupDir, "templates")
	write := func(path, s string) {
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(s), 0644)
	}
	write(filepath.Join(cfg.ClaudeDir, "projects", "-me-blog", homeSID+".jsonl"),
		`{"type":"user","cwd":"/me/blog","message":{"content":"x"}}`+"\n"+`{"type":"custom-title","customTitle":"blog post"}`+"\n")
	write(filepath.Join(cfg.BackupDir, "machines", "home", "projects", "-me-blog", homeSID+".jsonl"), "{}\n")
	write(filepath.Join(cfg.BackupDir, "machines", "work", "projects", "-work-api", workSID+".jsonl"),
		`{"type":"user","cwd":"/work/api"}`+"\n"+`{"type":"custom-title","customTitle":"api fix"}`+"\n")

	sessions, err := newBuilder(cfg).Build()
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(cfg, sessions)
	m.search.Cursor.SetMode(cursor.CursorStatic)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	return cfg, model
}

func press(t *testing.T, model tea.Model, keys ...string) tea.Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		model, cmd = model.Update(msg)
		model = drain(t, model, cmd)
	}
	return model
}

// drain runs commands synchronously, feeding results back, skipping blinks.
func drain(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return model
		case tea.BatchMsg:
			for _, c := range msg {
				model = drain(t, model, c)
			}
			return model
		case copyDoneMsg, renameDoneMsg, reloadedMsg, summariesMsg, searchIndexMsg:
			model, cmd = model.Update(msg)
		default:
			return model
		}
	}
	return model
}

func TestMachineSidebarAndScope(t *testing.T) {
	_, model := machineFixture(t)
	view := model.View()
	for _, want := range []string{"MACHINES", "home •", "All machines", "PROJECTS", "blog post"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(view, "api fix") {
		t.Error("other machine's session shown by default")
	}

	// tab to sidebar ("All" projects), up once = "work" machine
	model = press(t, model, "tab", "k")
	view = model.View()
	if !strings.Contains(view, "⌂ work") || !strings.Contains(view, "Copy it to work") {
		t.Fatalf("machine pane not shown:\n%s", view)
	}
	model = press(t, model, "enter")
	view = model.View()
	if !strings.Contains(view, "api fix") || strings.Contains(view, "blog post") {
		t.Errorf("scope to work failed:\n%s", view)
	}
}

func TestCopyDialogCopiesToMachine(t *testing.T) {
	cfg, model := machineFixture(t)
	model = press(t, model, "c")
	if !strings.Contains(model.View(), "TO MACHINE") {
		t.Fatalf("copy dialog not shown:\n%s", model.View())
	}
	// cursor starts on "work" (first machine without the session); keep path
	model = press(t, model, "enter", "enter")

	dst := filepath.Join(cfg.BackupDir, "machines", "work", "projects", "-me-blog", homeSID+".jsonl")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("not copied: %v", err)
	}
	if v := model.View(); !strings.Contains(v, "copied to work") {
		t.Errorf("no confirmation:\n%s", v)
	}
}

func TestCopyDialogNewMachineAndRelocate(t *testing.T) {
	cfg, model := machineFixture(t)
	model = press(t, model, "c", "j", "enter", "laptop", "enter")
	for range "/me/blog" {
		model = press(t, model, "backspace")
	}
	model = press(t, model, "/x/blog", "enter")

	matches, _ := filepath.Glob(filepath.Join(cfg.BackupDir, "machines", "laptop", "projects", "-x-blog", "*.jsonl"))
	if len(matches) != 1 || strings.Contains(matches[0], homeSID) {
		t.Fatalf("expected relocated copy with new ID, got %v", matches)
	}
}

func TestRenameThisMachine(t *testing.T) {
	cfg, model := machineFixture(t)
	// tab to sidebar, up twice = "home •"
	model = press(t, model, "tab", "k", "k", "R")
	if !strings.Contains(model.View(), "Rename this machine") {
		t.Fatalf("rename pane not shown:\n%s", model.View())
	}
	model = press(t, model, "backspace", "backspace", "backspace", "backspace", "laptop", "enter")

	if cfg.MachineName != "laptop" {
		t.Errorf("config machine = %q", cfg.MachineName)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, "machines", "laptop", "projects", "-me-blog", homeSID+".jsonl")); err != nil {
		t.Error("machine folder not moved")
	}
	view := model.View()
	if !strings.Contains(view, "laptop •") || !strings.Contains(view, "blog post") {
		t.Errorf("view not refreshed after rename:\n%s", view)
	}
	saved, _ := config.Load()
	if saved.MachineName != "laptop" {
		t.Errorf("saved machine = %q", saved.MachineName)
	}
}
