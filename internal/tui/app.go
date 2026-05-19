package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/sync"
	"github.com/batrashubham/claudectl/internal/template"
)

type viewState int

const (
	listView viewState = iota
	detailView
	searchView
	dashboardView
)

type filterMode int

const (
	filterAll filterMode = iota
	filterActive
	filterArchived
	filterGhost
)

func (f filterMode) label() string {
	switch f {
	case filterActive:
		return "Active"
	case filterArchived:
		return "Archive"
	case filterGhost:
		return "Ghost"
	default:
		return "All"
	}
}

func (f filterMode) count(sessions []index.SessionMeta) int {
	switch f {
	case filterActive:
		c := 0
		for _, s := range sessions {
			if s.Status == index.StatusActive {
				c++
			}
		}
		return c
	case filterArchived:
		c := 0
		for _, s := range sessions {
			if s.Status == index.StatusArchived && s.FileSize > 0 {
				c++
			}
		}
		return c
	case filterGhost:
		c := 0
		for _, s := range sessions {
			if s.FileSize == 0 {
				c++
			}
		}
		return c
	default:
		c := 0
		for _, s := range sessions {
			if s.FileSize > 0 {
				c++
			}
		}
		return c
	}
}

func (f filterMode) countFiltered(sessions []index.SessionMeta, projectFilter string) int {
	c := 0
	for _, s := range sessions {
		// Apply project filter
		if projectFilter != "" {
			project := filepath.Base(s.Project)
			if project == "" || project == "." {
				project = s.ProjectDir
			}
			if project != projectFilter {
				continue
			}
		}
		// Apply status filter
		switch f {
		case filterActive:
			if s.Status != index.StatusActive {
				continue
			}
		case filterArchived:
			if s.Status != index.StatusArchived || s.FileSize == 0 {
				continue
			}
		case filterGhost:
			if s.FileSize > 0 {
				continue
			}
		default:
			if s.FileSize == 0 {
				continue
			}
		}
		c++
	}
	return c
}

type syncDoneMsg struct {
	result *sync.Result
	err    error
}

type Model struct {
	state      viewState
	focus      paneFocus
	sessions   []index.SessionMeta
	filtered   []index.SessionMeta
	templates  []template.Meta
	cursor     int
	offset     int
	search     textinput.Model
	filter     filterMode

	// Sidebar
	sidebarItems  []sidebarItem
	sidebarCursor int
	sidebarOffset int

	width      int
	height     int
	config     *config.Config
	syncing    bool
	lastSync   time.Time
	syncResult string
	err        error
	resumeID   string
	spawnTmpl  string
}

func NewModel(cfg *config.Config, sessions []index.SessionMeta) Model {
	ti := textinput.New()
	ti.Placeholder = "type to search..."
	ti.Prompt = ""
	ti.CharLimit = 100
	ti.TextStyle = lipgloss.NewStyle().Foreground(text)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(dimGray)

	// Load templates
	store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
	templates, _ := store.ListAll()

	sidebarItems := buildSidebarItems(sessions, templates)

	m := Model{
		state:        listView,
		focus:        focusList,
		sessions:     sessions,
		templates:    templates,
		search:       ti,
		config:       cfg,
		sidebarItems: sidebarItems,
	}
	m.applyFilter()
	return m
}

func (m Model) SpawnTemplate() string {
	return m.spawnTmpl
}

func (m Model) Init() tea.Cmd {
	if m.config.SyncOnStart {
		return m.doSync()
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.syncResult = fmt.Sprintf("error: %v", msg.err)
		} else {
			m.syncResult = fmt.Sprintf("%d new, %d updated", msg.result.NewFiles, msg.result.UpdatedFiles)
			m.lastSync = time.Now()
			builder := index.NewBuilder(m.config.ClaudeDir, m.config.BackupDir)
			if sessions, err := builder.Build(); err == nil {
				m.sessions = sessions
				m.applyFilter()
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case searchView:
			return m.updateSearch(msg)
		case detailView:
			return m.updateDetail(msg)
		case dashboardView:
			return m.updateDashboard(msg)
		default:
			return m.updateList(msg)
		}
	}

	if m.state == searchView {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "q" || msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "tab":
		if m.focus == focusSidebar {
			m.focus = focusList
		} else {
			m.focus = focusSidebar
		}
		return m, nil
	case msg.String() == "j" || msg.String() == "down":
		if m.focus == focusSidebar {
			if m.sidebarCursor < len(m.sidebarItems)-1 {
				m.sidebarCursor++
				m.applyFilter()
				m.cursor = 0
				m.offset = 0
			}
		} else {
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.ensureVisible()
			}
		}
	case msg.String() == "k" || msg.String() == "up":
		if m.focus == focusSidebar {
			if m.sidebarCursor > 0 {
				m.sidebarCursor--
				m.applyFilter()
				m.cursor = 0
				m.offset = 0
			}
		} else {
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
		}
	case msg.String() == "G":
		if m.focus == focusList && len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
			m.ensureVisible()
		}
	case msg.String() == "g":
		if m.focus == focusList {
			m.cursor = 0
			m.ensureVisible()
		}
	case msg.String() == "enter":
		if m.focus == focusSidebar {
			// If template selected, spawn it
			if tmpl := m.selectedTemplate(); tmpl != "" {
				m.spawnTmpl = tmpl
				return m, tea.Quit
			}
			// Otherwise switch focus to list
			m.focus = focusList
		} else if len(m.filtered) > 0 {
			m.state = detailView
		}
	case msg.String() == "r":
		if m.focus == focusList && len(m.filtered) > 0 {
			s := m.filtered[m.cursor]
			if s.FileSize == 0 {
				m.syncResult = "cannot resume: session file no longer exists"
			} else {
				m.resumeID = s.ID
				return m, tea.Quit
			}
		}
	case msg.String() == "/":
		m.state = searchView
		m.search.Focus()
		return m, textinput.Blink
	case msg.String() == "s":
		if !m.syncing {
			m.syncing = true
			m.syncResult = ""
			return m, m.doSync()
		}
	case msg.String() == "f":
		m.filter = (m.filter + 1) % 4
		m.applyFilter()
		m.cursor = 0
		m.offset = 0
	case msg.String() == "D":
		m.state = dashboardView
	case msg.String() == "t":
		// Save current session as template
		if m.focus == focusList && len(m.filtered) > 0 {
			s := m.filtered[m.cursor]
			if s.FileSize == 0 {
				m.syncResult = "cannot save ghost session as template"
			} else {
				project := filepath.Base(s.Project)
				name := strings.ToLower(strings.ReplaceAll(project, " ", "-"))
				name = name + "-warm"
				store := template.NewStore(m.config.TemplatesDir, m.config.ClaudeDir)
				err := store.Save(template.SaveOptions{
					SessionID:  s.ID,
					ProjectDir: s.ProjectDir,
					Project:    s.Project,
					Name:       name,
					Description: fmt.Sprintf("Warm context from %s", project),
					Trim:       true,
					Force:      true,
				})
				if err != nil {
					m.syncResult = fmt.Sprintf("save failed: %v", err)
				} else {
					m.syncResult = fmt.Sprintf("template '%s' saved", name)
					m.templates, _ = store.ListAll()
					m.sidebarItems = buildSidebarItems(m.sessions, m.templates)
				}
			}
		}
	case msg.String() == "d":
		// Delete template when focused on one in sidebar
		if m.focus == focusSidebar {
			if tmpl := m.selectedTemplate(); tmpl != "" {
				store := template.NewStore(m.config.TemplatesDir, m.config.ClaudeDir)
				cwd, _ := os.Getwd()
				projectDir := strings.ReplaceAll(cwd, "/", "-")
				store.Delete(projectDir, tmpl)
				// Rebuild sidebar
				m.templates, _ = store.ListAll()
				m.sidebarItems = buildSidebarItems(m.sessions, m.templates)
				if m.sidebarCursor >= len(m.sidebarItems) {
					m.sidebarCursor = len(m.sidebarItems) - 1
				}
				m.syncResult = fmt.Sprintf("template '%s' deleted", tmpl)
			}
		}
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = listView
		m.search.Blur()
		m.search.SetValue("")
		m.applyFilter()
		m.cursor = 0
		m.offset = 0
		return m, nil
	case "enter":
		m.state = listView
		m.search.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyFilter()
	m.cursor = 0
	m.offset = 0
	return m, cmd
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = listView
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		if len(m.filtered) > 0 {
			s := m.filtered[m.cursor]
			if s.FileSize == 0 {
				m.syncResult = "cannot resume: session file no longer exists"
				m.state = listView
			} else {
				m.resumeID = s.ID
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *Model) applyFilter() {
	query := strings.ToLower(m.search.Value())
	projectFilter := m.sidebarProjectFilter()
	m.filtered = nil

	for _, s := range m.sessions {
		// Sidebar project filter
		if projectFilter != "" {
			project := filepath.Base(s.Project)
			if project == "" || project == "." {
				project = s.ProjectDir
			}
			if project != projectFilter {
				continue
			}
		}

		// Status filter
		switch m.filter {
		case filterAll:
			if s.FileSize == 0 {
				continue
			}
		case filterActive:
			if s.Status != index.StatusActive {
				continue
			}
		case filterArchived:
			if s.Status != index.StatusArchived || s.FileSize == 0 {
				continue
			}
		case filterGhost:
			if s.FileSize > 0 {
				continue
			}
		}

		// Search
		if query != "" {
			searchable := s.SearchText + strings.ToLower(s.Project) + " " + s.ID
			if !strings.Contains(searchable, query) {
				continue
			}
		}

		m.filtered = append(m.filtered, s)
	}
}

func (m *Model) ensureVisible() {
	visibleRows := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visibleRows {
		m.offset = m.cursor - visibleRows + 1
	}
}

func (m Model) listHeight() int {
	available := m.height - 7
	if m.state == searchView {
		available -= 3
	}
	rows := available / 3
	if rows < 2 {
		rows = 2
	}
	return rows
}

func (m Model) View() string {
	if m.width == 0 {
		return "\n  Loading..."
	}

	switch m.state {
	case detailView:
		return m.viewDetail()
	case dashboardView:
		return m.viewDashboard()
	default:
		return m.viewList()
	}
}

func (m Model) viewList() string {
	w := m.width
	var b strings.Builder

	// ═══ HEADER ═══
	title := lipgloss.NewStyle().Bold(true).Foreground(purple1).Render(" ⚡ CLAUDECTL")
	stats := lipgloss.NewStyle().Foreground(midGray).Render(
		fmt.Sprintf("  %d sessions  ·  %d projects", len(m.sessions), m.projectCount()))
	syncBadge := ""
	if m.syncing {
		syncBadge = lipgloss.NewStyle().Foreground(purple2).Render("  ◈ syncing...")
	} else if !m.lastSync.IsZero() {
		syncBadge = lipgloss.NewStyle().Foreground(green).Render("  ✓ synced " + humanize.Time(m.lastSync))
	}
	b.WriteString(title + stats + syncBadge + "\n")

	// ═══ SEPARATOR ═══
	sep := lipgloss.NewStyle().Foreground(purple5).Render(strings.Repeat("━", w))
	b.WriteString(sep + "\n")

	// ═══ TWO-PANE LAYOUT ═══
	sidebarWidth := 24
	listWidth := w - sidebarWidth - 3 // 3 for separator column

	// Build sidebar content
	sidebarContent := m.renderSidebar(sidebarWidth, m.height-5)

	// Build list content
	listContent := m.renderListPane(listWidth)

	// Join horizontally
	sidebarBlock := lipgloss.NewStyle().Width(sidebarWidth).Render(sidebarContent)
	sepCol := m.renderVerticalSep(m.height - 5)
	listBlock := lipgloss.NewStyle().Width(listWidth).Render(listContent)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sidebarBlock, sepCol, listBlock))
	b.WriteString("\n")

	// ═══ FOOTER ═══
	b.WriteString(sep + "\n")
	if m.syncResult != "" {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(midGray).Render("sync: "+m.syncResult) + "\n")
	}
	b.WriteString("  " + m.renderHelp())

	return b.String()
}

func (m Model) renderSidebar(w, h int) string {
	var b strings.Builder

	// Title
	sidebarTitle := "PROJECTS"
	if m.focus == focusSidebar {
		sidebarTitle = lipgloss.NewStyle().Bold(true).Foreground(purple2).Render(sidebarTitle)
	} else {
		sidebarTitle = lipgloss.NewStyle().Foreground(dimGray).Render(sidebarTitle)
	}
	b.WriteString(" " + sidebarTitle + "\n\n")

	lines := 2
	templatesStarted := false

	for i, item := range m.sidebarItems {
		if lines >= h-1 {
			break
		}

		// Templates section header
		if item.isTmpl && !templatesStarted {
			templatesStarted = true
			b.WriteString("\n")
			tmplHeader := lipgloss.NewStyle().Foreground(dimGray).Render(" TEMPLATES")
			if m.focus == focusSidebar {
				tmplHeader = lipgloss.NewStyle().Foreground(purple2).Render(" TEMPLATES")
			}
			b.WriteString(tmplHeader + "\n")
			lines += 2
		}

		selected := (i == m.sidebarCursor) && m.focus == focusSidebar
		label := item.label
		if len(label) > w-6 {
			label = label[:w-6]
		}

		var line string
		if selected {
			cursor := lipgloss.NewStyle().Foreground(cyan).Bold(true).Render("▸")
			name := lipgloss.NewStyle().Foreground(white).Bold(true).Render(label)
			if item.isTmpl {
				line = " " + cursor + " " + lipgloss.NewStyle().Foreground(purple1).Render("◆") + " " + name
			} else {
				countStr := lipgloss.NewStyle().Foreground(cyan).Render(fmt.Sprintf(" %d", item.count))
				line = " " + cursor + " " + name + countStr
			}
		} else {
			if item.isTmpl {
				name := lipgloss.NewStyle().Foreground(midGray).Render(label)
				line = "   " + lipgloss.NewStyle().Foreground(purple4).Render("◆") + " " + name
			} else if item.isAll {
				name := lipgloss.NewStyle().Foreground(ltGray).Render(label)
				countStr := lipgloss.NewStyle().Foreground(dimGray).Render(fmt.Sprintf(" %d", item.count))
				line = "   " + name + countStr
			} else {
				name := lipgloss.NewStyle().Foreground(ltGray).Render(label)
				countStr := lipgloss.NewStyle().Foreground(dimGray).Render(fmt.Sprintf(" %d", item.count))
				line = "   " + name + countStr
			}
		}

		b.WriteString(line + "\n")
		lines++
	}

	return b.String()
}

func (m Model) renderListPane(w int) string {
	// If a template is selected in sidebar, show template detail instead
	if tmpl := m.selectedTemplate(); tmpl != "" {
		return m.renderTemplatePane(w, tmpl)
	}

	var b strings.Builder

	// Filter bar
	b.WriteString(m.renderFilters() + "\n")

	// Search (if active)
	if m.state == searchView {
		searchInput := lipgloss.NewStyle().Foreground(text).Render(m.search.View())
		searchContent := lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("/") + " " + searchInput
		searchBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple3).
			Width(w - 4).
			PaddingLeft(1).
			Render(searchContent)
		b.WriteString(searchBox + "\n")
	}

	// Sessions
	visibleRows := m.listHeight()
	for i := m.offset; i < len(m.filtered) && i < m.offset+visibleRows; i++ {
		s := m.filtered[i]
		isSelected := (i == m.cursor) && m.focus == focusList
		b.WriteString(m.renderSessionRow(s, isSelected, w))
	}

	// Pad remaining space (always fill to fixed height)
	rendered := min(len(m.filtered)-m.offset, visibleRows)
	if len(m.filtered) == 0 {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(dimGray).Render("  No sessions") + "\n\n")
		rendered = 1
	}
	for i := rendered; i < visibleRows; i++ {
		b.WriteString("\n\n\n")
	}

	return b.String()
}

func (m Model) renderTemplatePane(w int, name string) string {
	var b strings.Builder

	// Find template meta
	var meta *template.Meta
	for i := range m.templates {
		if m.templates[i].Name == name {
			meta = &m.templates[i]
			break
		}
	}

	if meta == nil {
		b.WriteString(lipgloss.NewStyle().Foreground(dimGray).Render("  Template not found") + "\n")
		return b.String()
	}

	// Header
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  ◆ "+meta.Name) + "\n\n")

	// Metadata
	lbl := func(l string) string { return lipgloss.NewStyle().Foreground(midGray).Width(14).Render("  " + l) }
	val := func(v string) string { return lipgloss.NewStyle().Foreground(text).Render(v) }

	if meta.Description != "" {
		b.WriteString(lbl("Description") + val(meta.Description) + "\n")
	}
	b.WriteString(lbl("Project") + val(filepath.Base(meta.SourceProject)) + "\n")
	b.WriteString(lbl("Created") + val(humanize.Time(meta.CreatedAt)) + "\n")
	b.WriteString(lbl("Entries") + val(fmt.Sprintf("%d", meta.EntryCount)) + "\n")
	b.WriteString(lbl("Size") + val(humanize.Bytes(uint64(meta.SizeBytes))) + "\n")
	if meta.Trimmed {
		b.WriteString(lbl("Trimmed") + val("yes") + "\n")
	}

	// Actions
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(midGray).Render("  ACTIONS") + "\n\n")

	enterKey := lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("⏎")
	deleteKey := lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("d")

	b.WriteString("  " + enterKey + lipgloss.NewStyle().Foreground(ltGray).Render(" Spawn new session from this template") + "\n")
	b.WriteString("  " + deleteKey + lipgloss.NewStyle().Foreground(ltGray).Render(" Delete this template") + "\n")

	return b.String()
}

func (m Model) renderVerticalSep(h int) string {
	var lines []string
	sepChar := lipgloss.NewStyle().Foreground(purple5).Render("│")
	for i := 0; i < h; i++ {
		lines = append(lines, " "+sepChar+" ")
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderFilters() string {
	filters := []filterMode{filterAll, filterActive, filterArchived, filterGhost}
	var parts []string

	for _, f := range filters {
		count := f.countFiltered(m.sessions, m.sidebarProjectFilter())
		label := fmt.Sprintf(" %s %d ", f.label(), count)
		if f == m.filter {
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).
				Foreground(white).
				Background(purple4).
				Render(label))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(midGray).
				Render(label))
		}
	}

	return " " + strings.Join(parts, "  ")
}

func (m Model) renderSessionRow(s index.SessionMeta, selected bool, w int) string {
	isArchived := s.Status == index.StatusArchived

	// Ghost session = exists in history only, no file
	isGhost := s.FileSize == 0

	// Colors
	var projFg, prevFg, metaFg, ageFg, dotFg lipgloss.Color
	var bg lipgloss.Color

	if selected {
		projFg = white
		prevFg = purple1
		metaFg = purple2
		ageFg = cyan
		dotFg = green
		bg = lipgloss.Color("#3b2578")
		if isArchived || isGhost {
			dotFg = midGray
		}
	} else if isGhost {
		projFg = dimGray
		prevFg = lipgloss.Color("#3b4252")
		metaFg = lipgloss.Color("#3b4252")
		ageFg = dimGray
		dotFg = lipgloss.Color("#4a3060")
		bg = lipgloss.Color("")
	} else if isArchived {
		projFg = midGray
		prevFg = dimGray
		metaFg = dimGray
		ageFg = dimGray
		dotFg = midGray
		bg = lipgloss.Color("")
	} else {
		projFg = bright
		prevFg = ltGray
		metaFg = midGray
		ageFg = purple2
		dotFg = green
		bg = lipgloss.Color("")
	}

	contentWidth := w - 8

	// Cursor — bright and visible
	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(cyan).Bold(true).Render("▸ ")
	}

	// Dot: ● active, ○ archived, △ ghost (no file)
	dotChar := "●"
	if isGhost {
		dotChar = "△"
	} else if isArchived {
		dotChar = "○"
	}
	dot := lipgloss.NewStyle().Foreground(dotFg).Render(dotChar)

	// Project name
	project := filepath.Base(s.Project)
	if project == "" || project == "." {
		project = s.ProjectDir
	}
	if len(project) > 24 {
		project = project[:24]
	}
	projRendered := lipgloss.NewStyle().Bold(true).Foreground(projFg).Render(project)

	// Age (right-aligned)
	age := shortAge(s.LastSeen)
	ageRendered := lipgloss.NewStyle().Foreground(ageFg).Render(age)
	usedLine1 := 4 + lipgloss.Width(project) + lipgloss.Width(age)
	gap1 := max(2, contentWidth-usedLine1)

	line1 := cursor + dot + " " + projRendered + strings.Repeat(" ", gap1) + ageRendered

	// Preview
	preview := s.FirstPrompt
	if preview == "" {
		preview = s.ID[:12] + "..."
	}
	maxPrev := contentWidth - 2
	if len(preview) > maxPrev {
		preview = preview[:maxPrev-3] + "..."
	}
	line2 := "     " + lipgloss.NewStyle().Foreground(prevFg).Render(preview)

	// Meta
	metaParts := fmt.Sprintf("⊡ %d prompts", s.PromptCount)
	if s.FileSize > 0 {
		metaParts += "  ◈ " + humanize.Bytes(uint64(s.FileSize))
	}
	line3 := "     " + lipgloss.NewStyle().Foreground(metaFg).Render(metaParts)

	content := line1 + "\n" + line2 + "\n" + line3

	// Apply background for selected row
	if bg != "" {
		// Render each line with background to avoid bleeding
		lines := strings.Split(content, "\n")
		var rendered []string
		for _, l := range lines {
			rendered = append(rendered, lipgloss.NewStyle().Background(bg).Width(w).Render(l))
		}
		return strings.Join(rendered, "\n") + "\n"
	}

	return content + "\n"
}

func (m Model) renderHelp() string {
	type binding struct {
		key  string
		desc string
	}
	bindings := []binding{
		{"↑↓", "navigate"},
		{"tab", "pane"},
		{"⏎", "detail/spawn"},
		{"r", "resume"},
		{"t", "template"},
		{"/", "search"},
		{"s", "sync"},
		{"f", "filter"},
		{"D", "dashboard"},
		{"q", "quit"},
	}

	var parts []string
	for _, b := range bindings {
		k := lipgloss.NewStyle().Foreground(purple2).Bold(true).Render(b.key)
		d := lipgloss.NewStyle().Foreground(midGray).Render(b.desc)
		parts = append(parts, k+" "+d)
	}

	return strings.Join(parts, "   ")
}

func (m Model) viewDetail() string {
	if len(m.filtered) == 0 {
		return "No session selected"
	}

	s := m.filtered[m.cursor]
	w := m.width

	var b strings.Builder

	// Header bar
	back := lipgloss.NewStyle().Foreground(ltGray).Render(" ‹ back (esc)")
	resumeBtn := lipgloss.NewStyle().Bold(true).Foreground(white).Background(purple4).Padding(0, 2).Render("Resume (r)")
	gap := max(1, w-lipgloss.Width(back)-lipgloss.Width(resumeBtn)-2)
	headerBar := lipgloss.NewStyle().
		Width(w).
		Background(lipgloss.Color("#1a1535")).
		Render(back + strings.Repeat(" ", gap) + resumeBtn)
	b.WriteString(headerBar + "\n\n")

	// Left panel width
	leftWidth := 36
	rightWidth := w - leftWidth - 4

	// === LEFT: Metadata ===
	var left strings.Builder

	project := filepath.Base(s.Project)
	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(white).Render(project) + "\n")

	path := s.Project
	if len(path) > leftWidth-2 {
		path = "…" + path[len(path)-leftWidth+3:]
	}
	left.WriteString(lipgloss.NewStyle().Foreground(purple2).Render(path) + "\n")
	left.WriteString("\n")

	// Status
	statusDot := lipgloss.NewStyle().Foreground(green).Render("●")
	statusText := "Active"
	if s.Status == index.StatusArchived {
		statusDot = lipgloss.NewStyle().Foreground(dimGray).Render("○")
		statusText = "Archived"
	}

	lbl := func(l string) string { return lipgloss.NewStyle().Foreground(midGray).Width(11).Render(l) }
	val := func(v string) string { return lipgloss.NewStyle().Foreground(text).Render(v) }

	left.WriteString(lbl("Status") + statusDot + " " + val(statusText) + "\n")
	left.WriteString(lbl("Session") + val(s.ID[:16]+"…") + "\n")
	left.WriteString(lbl("Started") + val(s.FirstSeen.Format("Jan 2 15:04")) + "\n")
	left.WriteString(lbl("Last") + val(s.LastSeen.Format("Jan 2 15:04")+" ("+shortAge(s.LastSeen)+" ago)") + "\n")
	if s.FileSize > 0 {
		left.WriteString(lbl("Size") + val(humanize.Bytes(uint64(s.FileSize))) + "\n")
	}
	left.WriteString(lbl("Prompts") + val(fmt.Sprintf("%d", s.PromptCount)) + "\n")

	// === RIGHT: Conversation ===
	var right strings.Builder
	right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple2).Render("━━ CONVERSATION ━━") + "\n\n")

	prompts := m.getSessionPrompts(s.ID)
	maxPrompts := m.height - 8
	if maxPrompts < 5 {
		maxPrompts = 5
	}
	if len(prompts) > maxPrompts {
		prompts = prompts[len(prompts)-maxPrompts:]
	}

	for _, p := range prompts {
		ts := time.UnixMilli(p.Timestamp).Format("15:04")
		timeStr := lipgloss.NewStyle().Foreground(purple4).Render(ts)

		prompt := strings.ReplaceAll(p.Display, "\n", " ")
		maxLen := rightWidth - 10
		if maxLen < 20 {
			maxLen = 20
		}
		if len(prompt) > maxLen {
			prompt = prompt[:maxLen-3] + "..."
		}
		promptStr := lipgloss.NewStyle().Foreground(ltGray).Render(prompt)

		// Left border indicator
		border := lipgloss.NewStyle().Foreground(purple4).Render("│")
		right.WriteString(" " + border + " " + timeStr + "  " + promptStr + "\n")
	}

	if len(prompts) == 0 {
		right.WriteString(lipgloss.NewStyle().Foreground(dimGray).Render("  No prompt history available") + "\n")
	}

	// Vertical separator
	leftBlock := lipgloss.NewStyle().Width(leftWidth).Render(left.String())
	separator := lipgloss.NewStyle().Foreground(purple5).Render("┃")
	rightBlock := lipgloss.NewStyle().Width(rightWidth).Render(right.String())

	// Join panels line by line
	leftLines := strings.Split(leftBlock, "\n")
	rightLines := strings.Split(rightBlock, "\n")
	maxLines := max(len(leftLines), len(rightLines))

	for i := 0; i < maxLines; i++ {
		ll := ""
		rl := ""
		if i < len(leftLines) {
			ll = leftLines[i]
		}
		if i < len(rightLines) {
			rl = rightLines[i]
		}
		// Pad left to fixed width
		llWidth := lipgloss.Width(ll)
		if llWidth < leftWidth {
			ll += strings.Repeat(" ", leftWidth-llWidth)
		}
		b.WriteString(" " + ll + " " + separator + " " + rl + "\n")
	}

	// Help
	b.WriteString("\n")
	helpLine := lipgloss.NewStyle().Foreground(dimGray).PaddingLeft(1).Render(
		lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("r") + " resume   " +
			lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("esc") + " back   " +
			lipgloss.NewStyle().Foreground(purple2).Bold(true).Render("q") + " quit")
	b.WriteString(helpLine)

	return b.String()
}

func (m Model) getSessionPrompts(sessionID string) []index.HistoryEntry {
	builder := index.NewBuilder(m.config.ClaudeDir, m.config.BackupDir)
	entries, _ := builder.GetSessionEntries(sessionID)
	return entries
}

func (m Model) projectCount() int {
	projects := make(map[string]bool)
	for _, s := range m.sessions {
		projects[s.Project] = true
	}
	return len(projects)
}

func (m Model) doSync() tea.Cmd {
	return func() tea.Msg {
		engine := sync.NewEngine(m.config.ClaudeDir, m.config.BackupDir)
		engine.SetTemplatesDir(m.config.TemplatesDir)

		if m.config.GitRemote != "" {
			engine.GitSetupRemote(m.config.GitRemote)
		}

		result, err := engine.Sync()
		if err != nil {
			return syncDoneMsg{err: err}
		}
		if m.config.GitAutoCommit {
			engine.GitCommit(result)
		}
		if m.config.GitPush {
			engine.GitPush()
		}
		return syncDoneMsg{result: result}
	}
}

func (m Model) ResumeID() string {
	return m.resumeID
}

func shortAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
