package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/template"
)

func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "D", "backspace":
		m.state = listView
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewDashboard() string {
	w := m.width
	var b strings.Builder

	entries := m.loadHistoryEntries()

	// Header
	title := lipgloss.NewStyle().Bold(true).Foreground(cyan).Render(" ⚡ DASHBOARD")
	back := lipgloss.NewStyle().Foreground(midGray).Render("  (esc to go back)")
	b.WriteString(title + back + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(purple5).Render(strings.Repeat("━", w)) + "\n\n")

	// === SESSIONS ===
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  SESSIONS") + "\n")

	var active, archived, ghost, totalPrompts, longestPrompts int
	var longestProject string
	projectCounts := make(map[string]int)

	for _, s := range m.sessions {
		if s.FileSize == 0 {
			ghost++
		} else if s.Status == index.StatusActive {
			active++
		} else {
			archived++
		}
		totalPrompts += s.PromptCount
		if s.PromptCount > longestPrompts {
			longestPrompts = s.PromptCount
			longestProject = filepath.Base(s.Project)
		}
		projName := filepath.Base(s.Project)
		if projName == "" || projName == "." {
			projName = s.ProjectDir
		}
		projectCounts[projName]++
	}

	var totalChars int
	for _, e := range entries {
		totalChars += len(e.Display)
	}
	estTokens := totalChars / 4

	total := len(m.sessions)
	avgPrompts := 0
	if total > 0 {
		avgPrompts = totalPrompts / total
	}

	var mostActiveProject string
	var mostActiveCount int
	for proj, count := range projectCounts {
		if count > mostActiveCount {
			mostActiveCount = count
			mostActiveProject = proj
		}
	}

	b.WriteString(m.dashStat("Total", fmt.Sprintf("%d (%d active, %d archived, %d ghost)", total, active, archived, ghost)))
	b.WriteString(m.dashStat("Total prompts", humanize.Comma(int64(totalPrompts))))
	b.WriteString(m.dashStat("Est. tokens", "~"+humanizeTokens(estTokens)))
	b.WriteString(m.dashStat("Avg/session", fmt.Sprintf("%d prompts", avgPrompts)))
	b.WriteString(m.dashStat("Longest", fmt.Sprintf("%d prompts (%s)", longestPrompts, longestProject)))
	b.WriteString(m.dashStat("Most active", fmt.Sprintf("%s (%d sessions)", mostActiveProject, mostActiveCount)))
	b.WriteString("\n")

	// === ACTIVITY ===
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  ACTIVITY (last 4 weeks)") + "\n")

	now := time.Now()
	type weekBucket struct {
		start   time.Time
		end     time.Time
		prompts int
	}
	weeks := make([]weekBucket, 4)
	for i := range weeks {
		endOffset := i * 7
		startOffset := endOffset + 7
		weeks[i] = weekBucket{
			start: startOfDay(now.AddDate(0, 0, -startOffset+1)),
			end:   endOfDay(now.AddDate(0, 0, -endOffset)),
		}
	}

	dayCounts := make(map[time.Weekday]int)
	hourCounts := make(map[int]int)
	activeDays := make(map[string]bool)

	for _, e := range entries {
		ts := time.UnixMilli(e.Timestamp)
		for i := range weeks {
			if !ts.Before(weeks[i].start) && !ts.After(weeks[i].end) {
				weeks[i].prompts++
				break
			}
		}
		dayCounts[ts.Weekday()]++
		hourCounts[ts.Hour()]++
		activeDays[ts.Format("2006-01-02")] = true
	}

	maxWeekPrompts := 0
	for _, wk := range weeks {
		if wk.prompts > maxWeekPrompts {
			maxWeekPrompts = wk.prompts
		}
	}

	barWidth := 16
	for _, wk := range weeks {
		label := wk.start.Format("Jan 02") + "-" + wk.end.Format("02")
		filled := 0
		if maxWeekPrompts > 0 {
			filled = (wk.prompts * barWidth) / maxWeekPrompts
		}
		bar := lipgloss.NewStyle().Foreground(purple3).Render(strings.Repeat("▓", filled)) +
			lipgloss.NewStyle().Foreground(dimGray).Render(strings.Repeat("░", barWidth-filled))
		promptLabel := lipgloss.NewStyle().Foreground(text).Render(fmt.Sprintf("%d", wk.prompts))
		b.WriteString(fmt.Sprintf("    %-12s %s %s\n",
			lipgloss.NewStyle().Foreground(ltGray).Render(label), bar, promptLabel))
	}
	b.WriteString("\n")

	var bestDay time.Weekday
	var bestDayCount int
	for day, count := range dayCounts {
		if count > bestDayCount {
			bestDayCount = count
			bestDay = day
		}
	}
	var bestHour, bestHourCount int
	for hour, count := range hourCounts {
		if count > bestHourCount {
			bestHourCount = count
			bestHour = hour
		}
	}

	b.WriteString(m.dashStat("Peak day", bestDay.String()))
	b.WriteString(m.dashStat("Peak hour", fmt.Sprintf("%02d:00-%02d:00", bestHour, bestHour+1)))
	b.WriteString(m.dashStat("Active days", fmt.Sprintf("%d", len(activeDays))))
	b.WriteString("\n")

	// === PROJECTS ===
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  PROJECTS") + "\n")

	type projectStats struct {
		name     string
		sessions int
		prompts  int
		lastSeen time.Time
	}
	projectMap := make(map[string]*projectStats)
	for _, s := range m.sessions {
		projName := filepath.Base(s.Project)
		if projName == "" || projName == "." {
			projName = s.ProjectDir
		}
		ps, ok := projectMap[projName]
		if !ok {
			ps = &projectStats{name: projName}
			projectMap[projName] = ps
		}
		ps.sessions++
		ps.prompts += s.PromptCount
		if s.LastSeen.After(ps.lastSeen) {
			ps.lastSeen = s.LastSeen
		}
	}

	projects := make([]*projectStats, 0, len(projectMap))
	for _, ps := range projectMap {
		projects = append(projects, ps)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].lastSeen.After(projects[j].lastSeen)
	})

	b.WriteString(fmt.Sprintf("    %-20s %8s  %8s  %s\n",
		lipgloss.NewStyle().Foreground(dimGray).Render("NAME"),
		lipgloss.NewStyle().Foreground(dimGray).Render("SESSIONS"),
		lipgloss.NewStyle().Foreground(dimGray).Render("PROMPTS"),
		lipgloss.NewStyle().Foreground(dimGray).Render("LAST ACTIVE")))

	maxRows := min(8, len(projects))
	for i := 0; i < maxRows; i++ {
		ps := projects[i]
		name := ps.name
		if len(name) > 20 {
			name = name[:17] + "..."
		}
		b.WriteString(fmt.Sprintf("    %-20s %8d  %8d  %s\n",
			lipgloss.NewStyle().Foreground(green).Render(name),
			ps.sessions, ps.prompts,
			humanize.Time(ps.lastSeen)))
	}
	if len(projects) > maxRows {
		b.WriteString(fmt.Sprintf("    %s\n", lipgloss.NewStyle().Foreground(dimGray).Render(
			fmt.Sprintf("... and %d more", len(projects)-maxRows))))
	}
	b.WriteString("\n")

	// === TEMPLATES ===
	store := template.NewStore(m.config.TemplatesDir, m.config.ClaudeDir)
	templates, _ := store.ListAll()
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(purple1).Render("  TEMPLATES") + "\n")
	if len(templates) == 0 {
		b.WriteString(m.dashStat("Count", "none saved"))
	} else {
		b.WriteString(m.dashStat("Count", fmt.Sprintf("%d", len(templates))))
		var names []string
		for _, t := range templates {
			names = append(names, t.Name)
		}
		b.WriteString(m.dashStat("Saved", strings.Join(names, ", ")))
	}

	return b.String()
}

func (m Model) dashStat(label, value string) string {
	return fmt.Sprintf("    %-16s %s\n",
		lipgloss.NewStyle().Foreground(ltGray).Render(label+":"),
		lipgloss.NewStyle().Foreground(text).Render(value))
}

func (m Model) loadHistoryEntries() []index.HistoryEntry {
	seen := make(map[string]bool)
	var entries []index.HistoryEntry

	for _, path := range []string{
		filepath.Join(m.config.ClaudeDir, "history.jsonl"),
		filepath.Join(m.config.BackupDir, "history.jsonl"),
	} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			var entry index.HistoryEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			if entry.SessionID == "" {
				continue
			}
			key := fmt.Sprintf("%s:%d", entry.SessionID, entry.Timestamp)
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
		f.Close()
	}
	return entries
}

func humanizeTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.0fK", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
}
