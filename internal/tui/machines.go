package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machines"
	"github.com/batrashubham/claudectl/internal/meta"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
)

const newMachineChoice = "+ new machine…"

type copyStep int

const (
	copyPickMachine copyStep = iota
	copyNewMachine
	copyPath
)

type copyState struct {
	session index.SessionMeta
	step    copyStep
	choices []string
	cursor  int
	to      string
}

type summariesMsg []machines.Summary

type copyDoneMsg struct {
	out *machines.CopyOutcome
	err error
}

type renameDoneMsg struct {
	oldName, newName string
	err              error
}

func (m Model) loadSummaries() tea.Cmd {
	cfg, sessions := *m.config, m.allSessions
	return func() tea.Msg { return summariesMsg(machines.Summarize(&cfg, sessions)) }
}

// rebuildSidebar lays out machines, projects and templates, keeping the
// cursor on the same item when it still exists.
func (m *Model) rebuildSidebar() {
	prev := ""
	if m.sidebarCursor < len(m.sidebarItems) {
		prev = m.sidebarItems[m.sidebarCursor].key()
	}

	counts := make(map[string]int)
	total := 0
	for _, s := range m.allSessions {
		if s.FileSize > 0 {
			counts[s.Machine]++
			total++
		}
	}
	items := []sidebarItem{{label: "All machines", isMachine: true, count: total}}
	for _, name := range m.machines {
		label := name
		if name == m.config.MachineName {
			label += " •"
		}
		items = append(items, sidebarItem{label: label, isMachine: true, machine: name, count: counts[name]})
	}
	m.sidebarItems = append(items, buildSidebarItems(m.sessions, m.templates)...)

	if prev == "" || !m.selectSidebar(prev) {
		m.selectSidebar("all")
	}
}

func (m *Model) selectSidebar(key string) bool {
	for i, it := range m.sidebarItems {
		if it.key() == key {
			m.sidebarCursor = i
			return true
		}
	}
	return false
}

func (m *Model) selectedMachine() (string, bool) {
	if m.sidebarCursor >= len(m.sidebarItems) {
		return "", false
	}
	it := m.sidebarItems[m.sidebarCursor]
	return it.machine, it.isMachine
}

// setScope shows one machine's sessions ("" for all) and resets the list.
func (m *Model) setScope(machine string) {
	m.scope = machine
	m.cursor, m.offset = 0, 0
	m.setSessions(m.allSessions)
	m.selectSidebar("all")
	m.applyFilter()
}

func (m Model) summary(name string) *machines.Summary {
	for i := range m.summaries {
		if m.summaries[i].Name == name {
			return &m.summaries[i]
		}
	}
	return nil
}

func (m Model) renderMachinePane(w int, name string) string {
	var b strings.Builder
	lbl := func(l string) string { return lipgloss.NewStyle().Foreground(midGray).Width(14).Render("  " + l) }
	val := func(v string) string { return lipgloss.NewStyle().Foreground(text).Render(v) }
	key := func(k string) string { return lipgloss.NewStyle().Foreground(purple2).Bold(true).Render(k) }
	act := func(s string) string { return lipgloss.NewStyle().Foreground(ltGray).Render(s) }

	if name == "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  ⌂ All machines") + "\n\n")
		header := fmt.Sprintf("  %-20s %9s %10s  %s", "MACHINE", "SESSIONS", "SIZE", "LAST SYNC")
		b.WriteString(lipgloss.NewStyle().Foreground(midGray).Render(header) + "\n")
		for _, n := range m.machines {
			row := fmt.Sprintf("  %-20s %9s %10s  %s", truncRunes(n, 20), "…", "…", "…")
			if s := m.summary(n); s != nil {
				row = fmt.Sprintf("  %-20s %9d %10s  %s", truncRunes(n, 20), s.Sessions, humanize.Bytes(uint64(s.Bytes)), lastSync(s))
			}
			if n == m.config.MachineName {
				row += "  ← this machine"
			}
			b.WriteString(val(row) + "\n")
		}
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(midGray).Render("  ACTIONS") + "\n\n")
		b.WriteString("  " + key("⏎") + act(" Show sessions from every machine") + "\n")
		return b.String()
	}

	title := "  ⌂ " + name
	if name == m.config.MachineName {
		title += "  (this machine)"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render(title) + "\n\n")
	if s := m.summary(name); s != nil {
		b.WriteString(lbl("Sessions") + val(fmt.Sprintf("%d", s.Sessions)) + "\n")
		if s.This {
			b.WriteString(lbl("Active") + val(fmt.Sprintf("%d live in ~/.claude", s.Active)) + "\n")
		}
		b.WriteString(lbl("Backup size") + val(humanize.Bytes(uint64(s.Bytes))) + "\n")
		b.WriteString(lbl("Last sync") + val(lastSync(s)) + "\n")
	} else {
		b.WriteString(lbl("Sessions") + val("loading…") + "\n")
	}
	if m.scope == name {
		b.WriteString(lbl("Showing") + lipgloss.NewStyle().Foreground(green).Render("yes") + "\n")
	}

	b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(midGray).Render("  ACTIONS") + "\n\n")
	b.WriteString("  " + key("⏎") + act(" Show this machine's sessions") + "\n")
	if name == m.config.MachineName {
		b.WriteString("  " + key("R") + act(" Rename this machine") + "\n")
	} else {
		b.WriteString("  " + key("c") + act(" (on a session) Copy it to "+name) + "\n")
		b.WriteString("  " + key("r") + act(" (on a session) Resume — copies it here first") + "\n")
	}
	return b.String()
}

func lastSync(s *machines.Summary) string {
	if s.LastSync.IsZero() {
		return "never"
	}
	return humanize.Time(s.LastSync)
}

func (m Model) startCopy(s index.SessionMeta) (tea.Model, tea.Cmd) {
	if s.FileSize == 0 {
		m.syncResult = "cannot copy: session file no longer exists"
		return m, nil
	}
	choices := append([]string{}, m.machines...)
	choices = append(choices, newMachineChoice)
	cursor := len(choices) - 1
	for i, c := range choices {
		if c != s.Machine && c != newMachineChoice {
			cursor = i
			break
		}
	}
	m.copying = copyState{session: s, choices: choices, cursor: cursor}
	m.state = copyView
	return m, nil
}

func (m Model) updateCopy(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.copying
	if msg.String() == "esc" {
		m.state = listView
		m.search.Blur()
		m.search.SetValue("")
		return m, nil
	}

	switch c.step {
	case copyPickMachine:
		switch msg.String() {
		case "j", "down":
			if c.cursor < len(c.choices)-1 {
				c.cursor++
			}
		case "k", "up":
			if c.cursor > 0 {
				c.cursor--
			}
		case "enter":
			if c.choices[c.cursor] == newMachineChoice {
				c.step = copyNewMachine
				m.search.SetValue("")
			} else {
				c.to = c.choices[c.cursor]
				c.step = copyPath
				m.search.SetValue(c.session.Project)
			}
			m.search.CursorEnd()
			m.search.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case copyNewMachine:
		if msg.String() == "enter" {
			name := strings.ToLower(strings.TrimSpace(m.search.Value()))
			if err := config.ValidateMachineName(name); err != nil {
				m.syncResult = err.Error()
				return m, nil
			}
			c.to = name
			c.step = copyPath
			m.search.SetValue(c.session.Project)
			m.search.CursorEnd()
			return m, nil
		}

	case copyPath:
		if msg.String() == "enter" {
			project := strings.TrimSpace(m.search.Value())
			if strings.HasPrefix(project, "~/") {
				home, _ := os.UserHomeDir()
				project = filepath.Join(home, project[2:])
			}
			m.search.Blur()
			m.search.SetValue("")
			m.state = listView
			m.syncResult = fmt.Sprintf("copying to %s…", c.to)
			cfg, s, to := *m.config, c.session, c.to
			return m, func() tea.Msg {
				out, err := machines.Copy(&cfg, s, to, project)
				return copyDoneMsg{out: out, err: err}
			}
		}
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m Model) handleCopyDone(msg copyDoneMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.syncResult = "copy failed: " + msg.err.Error()
	case msg.out.Skipped:
		m.syncResult = fmt.Sprintf("%s already has this session", msg.out.To)
	default:
		m.syncResult = fmt.Sprintf("copied to %s", msg.out.To)
		if msg.out.SessionID != m.copying.session.ID {
			m.syncResult += " as " + index.ShortID(msg.out.SessionID, 8)
		}
		if msg.out.Pushed {
			m.syncResult += " and pushed; run 'claudectl restore' there"
		}
	}
	return m, m.reload()
}

func (m Model) startRename() (tea.Model, tea.Cmd) {
	m.state = renameView
	m.search.SetValue(m.config.MachineName)
	m.search.CursorEnd()
	m.search.Focus()
	return m, textinput.Blink
}

func (m Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = listView
		m.search.Blur()
		m.search.SetValue("")
		return m, nil
	case "enter":
		newName := strings.ToLower(strings.TrimSpace(m.search.Value()))
		m.search.Blur()
		m.search.SetValue("")
		m.state = listView
		if err := config.ValidateMachineName(newName); err != nil {
			m.syncResult = err.Error()
			return m, nil
		}
		cfg := *m.config
		oldName := cfg.MachineName
		return m, func() tea.Msg {
			err := machines.Rename(&cfg, newName)
			return renameDoneMsg{oldName: oldName, newName: newName, err: err}
		}
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m Model) handleRenameDone(msg renameDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.syncResult = "rename failed: " + msg.err.Error()
		return m, nil
	}
	m.config.MachineName = msg.newName
	if m.scope == msg.oldName {
		m.scope = msg.newName
	}
	m.syncResult = fmt.Sprintf("renamed %s → %s", msg.oldName, msg.newName)
	return m, m.reload()
}

// reload rebuilds the index after the backup changed underneath us.
func (m Model) reload() tea.Cmd {
	cfg := *m.config
	return func() tea.Msg {
		sessions, err := newBuilder(&cfg).Build()
		if err != nil {
			return nil
		}
		return reloadedMsg(sessions)
	}
}

type reloadedMsg []index.SessionMeta

func newBuilder(cfg *config.Config) *index.Builder {
	return index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).WithMetaCache(meta.CacheDir())
}

func (m Model) inputBox(label string, color lipgloss.Color, w int) string {
	input := lipgloss.NewStyle().Foreground(text).Render(m.search.View())
	content := lipgloss.NewStyle().Foreground(color).Bold(true).Render(label) + input
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).
		Width(w-4).PaddingLeft(1).Render(content) + "\n"
}

func (m Model) renderCopyPane(w int) string {
	c := m.copying
	var b strings.Builder
	label := c.session.Label()
	if label == "" {
		label = index.ShortID(c.session.ID, 12)
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  ⇄ Copy session") + "\n")
	b.WriteString("  " + lipgloss.NewStyle().Foreground(ltGray).Render(truncRunes(label, w-6)) + "\n")
	b.WriteString("  " + lipgloss.NewStyle().Foreground(dimGray).Render("from "+c.session.Machine+" · "+c.session.Project) + "\n\n")

	switch c.step {
	case copyPickMachine:
		b.WriteString(lipgloss.NewStyle().Foreground(midGray).Render("  TO MACHINE") + "\n\n")
		for i, choice := range c.choices {
			name := choice
			if choice == m.config.MachineName {
				name += "  (this machine)"
			}
			if choice == c.session.Machine {
				name += "  (has it)"
			}
			if i == c.cursor {
				b.WriteString(" " + lipgloss.NewStyle().Foreground(cyan).Bold(true).Render("▸ "+name) + "\n")
			} else {
				b.WriteString("   " + lipgloss.NewStyle().Foreground(ltGray).Render(name) + "\n")
			}
		}
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(dimGray).Render("⏎ choose   esc cancel") + "\n")
	case copyNewMachine:
		b.WriteString(m.inputBox("machine: ", green, w))
		b.WriteString("  " + lipgloss.NewStyle().Foreground(dimGray).Render("lowercase letters, digits, hyphens   ⏎ next   esc cancel") + "\n")
	case copyPath:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(midGray).Render("to "+c.to) + "\n")
		b.WriteString(m.inputBox("project: ", green, w))
		b.WriteString("  " + lipgloss.NewStyle().Foreground(dimGray).Render("path on "+c.to+"; a different path gives the copy a new session ID") + "\n")
		b.WriteString("  " + lipgloss.NewStyle().Foreground(dimGray).Render("⏎ copy   esc cancel") + "\n")
	}
	return b.String()
}

func (m Model) renderRenamePane(w int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  ⌂ Rename this machine") + "\n\n")
	b.WriteString(m.inputBox("name: ", green, w))
	b.WriteString("  " + lipgloss.NewStyle().Foreground(dimGray).Render("moves machines/"+m.config.MachineName+"/ in the backup and updates config   ⏎ rename   esc cancel") + "\n")
	return b.String()
}
