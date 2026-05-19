package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/template"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Show rich usage analytics dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

// Colors matching internal/tui/styles.go
var (
	dashPurple1 = lipgloss.Color("#c4b5fd")
	dashPurple3 = lipgloss.Color("#7c3aed")
	dashCyan    = lipgloss.Color("#22d3ee")
	dashGreen   = lipgloss.Color("#34d399")
	dashDimGray = lipgloss.Color("#4b5563")
	dashLtGray  = lipgloss.Color("#9ca3af")
	dashBright  = lipgloss.Color("#f3f4f6")
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(dashCyan)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(dashPurple1)
	labelStyle  = lipgloss.NewStyle().Foreground(dashLtGray)
	valueStyle  = lipgloss.NewStyle().Foreground(dashBright)
	dimStyle    = lipgloss.NewStyle().Foreground(dashDimGray)
	accentStyle = lipgloss.NewStyle().Foreground(dashGreen)
	barFull     = lipgloss.NewStyle().Foreground(dashPurple3)
	barEmpty    = lipgloss.NewStyle().Foreground(dashDimGray)
)

func runDashboard() error {
	builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
	sessions, err := builder.Build()
	if err != nil {
		return fmt.Errorf("building session index: %w", err)
	}

	entries := loadAllHistoryEntries(cfg.ClaudeDir, cfg.BackupDir)

	fmt.Println()
	fmt.Println(titleStyle.Render("⚡ CLAUDECTL DASHBOARD"))
	fmt.Println(dimStyle.Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
	fmt.Println()

	printSessionStats(sessions, entries)
	fmt.Println()
	printActivityStats(entries)
	fmt.Println()
	printProjectBreakdown(sessions)
	fmt.Println()
	printTemplateStats()
	fmt.Println()

	return nil
}

func printSessionStats(sessions []index.SessionMeta, entries []index.HistoryEntry) {
	fmt.Println(headerStyle.Render("SESSIONS"))

	var active, archived, ghost int
	var totalPrompts int
	var longestPrompts int
	var longestProject string
	projectCounts := make(map[string]int)

	for _, s := range sessions {
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
			if longestProject == "" || longestProject == "." {
				longestProject = s.ProjectDir
			}
		}
		projName := filepath.Base(s.Project)
		if projName == "" || projName == "." {
			projName = s.ProjectDir
		}
		projectCounts[projName]++
	}

	// Estimate tokens from all entry display text
	var totalChars int
	for _, e := range entries {
		totalChars += len(e.Display)
	}
	estTokens := totalChars / 4

	total := len(sessions)
	avgPrompts := 0
	if total > 0 {
		avgPrompts = totalPrompts / total
	}

	// Most active project
	var mostActiveProject string
	var mostActiveCount int
	for proj, count := range projectCounts {
		if count > mostActiveCount {
			mostActiveCount = count
			mostActiveProject = proj
		}
	}

	printStat("Total", fmt.Sprintf("%d (%d active, %d archived, %d ghost)", total, active, archived, ghost))
	printStat("Total prompts", humanize.Comma(int64(totalPrompts)))
	printStat("Est. tokens", "~"+humanizeTokens(estTokens))
	printStat("Avg per session", fmt.Sprintf("%d prompts", avgPrompts))
	printStat("Longest", fmt.Sprintf("%d prompts (%s)", longestPrompts, longestProject))
	printStat("Most active", fmt.Sprintf("%s (%d sessions)", mostActiveProject, mostActiveCount))
}

func printActivityStats(entries []index.HistoryEntry) {
	fmt.Println(headerStyle.Render("ACTIVITY (last 4 weeks)"))

	now := time.Now()

	// Group prompts by ISO week (last 4 weeks)
	type weekBucket struct {
		start   time.Time
		end     time.Time
		prompts int
	}

	weeks := make([]weekBucket, 4)
	for i := range weeks {
		// Week ending on current Saturday going backwards
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

	// Find max for bar scaling
	maxPrompts := 0
	for _, w := range weeks {
		if w.prompts > maxPrompts {
			maxPrompts = w.prompts
		}
	}

	barWidth := 16
	for _, w := range weeks {
		label := fmt.Sprintf("  %s:", w.start.Format("Jan 02")+"-"+w.end.Format("02"))
		filled := 0
		if maxPrompts > 0 {
			filled = (w.prompts * barWidth) / maxPrompts
		}
		bar := barFull.Render(strings.Repeat("▓", filled)) + barEmpty.Render(strings.Repeat("░", barWidth-filled))
		fmt.Printf("  %-14s %s %s\n", labelStyle.Render(label), bar, valueStyle.Render(fmt.Sprintf("%d prompts", w.prompts)))
	}
	fmt.Println()

	// Most active day
	var bestDay time.Weekday
	var bestDayCount int
	for day, count := range dayCounts {
		if count > bestDayCount {
			bestDayCount = count
			bestDay = day
		}
	}

	// Most active hour
	var bestHour int
	var bestHourCount int
	for hour, count := range hourCounts {
		if count > bestHourCount {
			bestHourCount = count
			bestHour = hour
		}
	}

	printStat("Most active day", bestDay.String())
	printStat("Most active hour", fmt.Sprintf("%02d:00-%02d:00", bestHour, bestHour+1))
	printStat("Total active days", fmt.Sprintf("%d", len(activeDays)))
}

func printProjectBreakdown(sessions []index.SessionMeta) {
	fmt.Println(headerStyle.Render("PROJECTS"))

	type projectStats struct {
		name     string
		sessions int
		prompts  int
		tokens   int
		lastSeen time.Time
	}

	projectMap := make(map[string]*projectStats)
	for _, s := range sessions {
		projName := filepath.Base(s.Project)
		if projName == "" || projName == "." {
			projName = s.ProjectDir
		}
		ps, exists := projectMap[projName]
		if !exists {
			ps = &projectStats{name: projName}
			projectMap[projName] = ps
		}
		ps.sessions++
		ps.prompts += s.PromptCount
		if s.LastSeen.After(ps.lastSeen) {
			ps.lastSeen = s.LastSeen
		}
	}

	// Estimate tokens per project from SearchText length (chars/4)
	for _, s := range sessions {
		projName := filepath.Base(s.Project)
		if projName == "" || projName == "." {
			projName = s.ProjectDir
		}
		if ps, ok := projectMap[projName]; ok {
			ps.tokens += len(s.SearchText) / 4
		}
	}

	// Sort by most recently active
	projects := make([]*projectStats, 0, len(projectMap))
	for _, ps := range projectMap {
		projects = append(projects, ps)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].lastSeen.After(projects[j].lastSeen)
	})

	// Header row
	fmt.Printf("  %-24s %8s  %8s  %8s  %s\n",
		dimStyle.Render("NAME"),
		dimStyle.Render("SESSIONS"),
		dimStyle.Render("PROMPTS"),
		dimStyle.Render("TOKENS"),
		dimStyle.Render("LAST ACTIVE"),
	)

	maxRows := 10
	for i, ps := range projects {
		if i >= maxRows {
			fmt.Printf("  %s\n", dimStyle.Render(fmt.Sprintf("... and %d more", len(projects)-maxRows)))
			break
		}
		name := ps.name
		if len(name) > 24 {
			name = name[:21] + "..."
		}
		fmt.Printf("  %-24s %8d  %8d  %8s  %s\n",
			accentStyle.Render(name),
			ps.sessions,
			ps.prompts,
			"~"+humanizeTokens(ps.tokens),
			humanize.Time(ps.lastSeen),
		)
	}
}

func printTemplateStats() {
	fmt.Println(headerStyle.Render("TEMPLATES"))

	store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
	templates, err := store.ListAll()
	if err != nil || len(templates) == 0 {
		printStat("Templates", "none saved")
		return
	}

	printStat("Count", fmt.Sprintf("%d template(s)", len(templates)))
	names := make([]string, 0, len(templates))
	for _, t := range templates {
		names = append(names, t.Name)
	}
	if len(names) > 5 {
		names = names[:5]
		names = append(names, "...")
	}
	printStat("Saved", strings.Join(names, ", "))
}

// --- Helpers ---

func printStat(label, value string) {
	fmt.Printf("  %-18s %s\n", labelStyle.Render(label+":"), valueStyle.Render(value))
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

func loadAllHistoryEntries(claudeDir, backupDir string) []index.HistoryEntry {
	seen := make(map[string]bool)
	var entries []index.HistoryEntry

	for _, path := range []string{
		filepath.Join(claudeDir, "history.jsonl"),
		filepath.Join(backupDir, "history.jsonl"),
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
			dedupKey := fmt.Sprintf("%s:%d", entry.SessionID, entry.Timestamp)
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			entries = append(entries, entry)
		}
		f.Close()
	}

	return entries
}
