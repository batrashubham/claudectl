package index

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Builder struct {
	claudeDir string
	backupDir string
	machine   string
}

func NewBuilder(claudeDir, backupDir, machine string) *Builder {
	return &Builder{claudeDir: claudeDir, backupDir: backupDir, machine: machine}
}

// source is one place sessions are read from, and the machine they belong to.
type source struct {
	dir     string
	machine string
	status  SessionStatus
}

// sources lists the live Claude dir, this machine's backup, the pre-machines
// flat backup (treated as this machine's), then every other machine.
func (b *Builder) sources() []source {
	srcs := []source{
		{b.claudeDir, b.machine, StatusActive},
		{filepath.Join(b.backupDir, "machines", b.machine), b.machine, StatusArchived},
		{b.backupDir, b.machine, StatusArchived},
	}
	for _, m := range Machines(b.backupDir) {
		if m != b.machine {
			srcs = append(srcs, source{filepath.Join(b.backupDir, "machines", m), m, StatusArchived})
		}
	}
	return srcs
}

// Machines lists the machine folders in the backup, sorted.
func Machines(backupDir string) []string {
	entries, _ := os.ReadDir(filepath.Join(backupDir, "machines"))
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// HistoryFiles are this machine's history files: live, backed up, and legacy.
func HistoryFiles(claudeDir, backupDir, machine string) []string {
	return []string{
		filepath.Join(claudeDir, "history.jsonl"),
		filepath.Join(backupDir, "machines", machine, "history.jsonl"),
		filepath.Join(backupDir, "history.jsonl"),
	}
}

func (b *Builder) Build() ([]SessionMeta, error) {
	sessions := make(map[string]*SessionMeta)
	seen := make(map[string]bool) // dedup key: sessionID+timestamp

	srcs := b.sources()
	for _, src := range srcs {
		b.parseHistoryFile(filepath.Join(src.dir, "history.jsonl"), src.machine, sessions, seen)
	}
	for _, src := range srcs {
		b.scanProjectsDir(sessions, src)
	}

	result := make([]SessionMeta, 0, len(sessions))
	for _, s := range sessions {
		b.resolveStatus(s)
		s.Machine = b.resolveMachine(s)
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].LastSeen.After(result[j].LastSeen)
	})

	return result, nil
}

// resolveMachine prefers this machine when a session exists here and on
// another machine (e.g. after a copy).
func (b *Builder) resolveMachine(s *SessionMeta) string {
	if s.machines[b.machine] {
		return b.machine
	}
	var names []string
	for m := range s.machines {
		names = append(names, m)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return b.machine
	}
	return names[0]
}

func (b *Builder) parseHistoryFile(path, machine string, sessions map[string]*SessionMeta, seen map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLine)

	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.SessionID == "" {
			continue
		}

		// Deduplicate across live + backup
		dedupKey := fmt.Sprintf("%s:%d", entry.SessionID, entry.Timestamp)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		s, exists := sessions[entry.SessionID]
		if !exists {
			s = &SessionMeta{
				ID:         entry.SessionID,
				Project:    entry.Project,
				ProjectDir: ProjectDir(entry.Project),
			}
			sessions[entry.SessionID] = s
		}
		s.addMachine(machine)

		ts := time.UnixMilli(entry.Timestamp)
		if s.FirstSeen.IsZero() || ts.Before(s.FirstSeen) {
			s.FirstSeen = ts
		}
		if ts.After(s.LastSeen) {
			s.LastSeen = ts
		}

		if entry.Display != "" && !isCommand(entry.Display) {
			prompt := truncate(entry.Display, 80)
			if s.FirstPrompt == "" || ts.Before(s.firstPromptTime) {
				s.FirstPrompt = prompt
				s.firstPromptTime = ts
			}
			if ts.After(s.lastPromptTime) {
				s.LastPrompt = prompt
				s.lastPromptTime = ts
			}
			s.SearchText += strings.ToLower(entry.Display) + " "
		}
		s.PromptCount++
	}
}

func (b *Builder) scanProjectsDir(sessions map[string]*SessionMeta, src source) {
	projectsDir := filepath.Join(src.dir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return
	}

	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		projDir := projEntry.Name()
		projPath := filepath.Join(projectsDir, projDir)

		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}

		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(file.Name(), ".jsonl")

			info, err := file.Info()
			if err != nil {
				continue
			}

			s, exists := sessions[sessionID]
			if !exists {
				project := firstCwd(filepath.Join(projPath, file.Name()))
				if project == "" {
					project = dirToProject(projDir)
				}
				s = &SessionMeta{
					ID:         sessionID,
					ProjectDir: projDir,
					Project:    project,
				}
				sessions[sessionID] = s
			}
			s.addMachine(src.machine)

			if src.status == StatusActive {
				s.activeExists = true
			} else {
				s.archivedExists = true
			}

			if info.Size() > s.FileSize {
				s.FileSize = info.Size()
			}

			if s.FirstSeen.IsZero() {
				s.FirstSeen = info.ModTime()
				s.LastSeen = info.ModTime()
			}
		}
	}
}

func (b *Builder) resolveStatus(s *SessionMeta) {
	sourcePath := filepath.Join(b.claudeDir, "projects", s.ProjectDir, s.ID+".jsonl")
	if _, err := os.Stat(sourcePath); err == nil {
		s.Status = StatusActive
	} else {
		s.Status = StatusArchived
	}
}

// MaxLine bounds one JSONL line. Pasted content and tool results can exceed
// the 1 MB scanner default, which would silently end the scan.
const MaxLine = 256 << 20

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// ProjectDir encodes a project path the way Claude Code names its
// ~/.claude/projects/ folders: every non-alphanumeric character becomes "-".
func ProjectDir(project string) string {
	return nonAlnum.ReplaceAllString(project, "-")
}

func dirToProject(dir string) string {
	if len(dir) > 0 && dir[0] == '-' {
		return "/" + strings.ReplaceAll(dir[1:], "-", "/")
	}
	return dir
}

func isCommand(s string) bool {
	return len(s) > 0 && s[0] == '/'
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func (b *Builder) GetSessionEntries(sessionID string) ([]HistoryEntry, error) {
	var entries []HistoryEntry
	seen := make(map[int64]bool)
	for _, src := range b.sources() {
		f, err := os.Open(filepath.Join(src.dir, "history.jsonl"))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), MaxLine)
		for scanner.Scan() {
			var entry HistoryEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			if entry.SessionID == sessionID && entry.Display != "" && !isCommand(entry.Display) && !seen[entry.Timestamp] {
				seen[entry.Timestamp] = true
				entries = append(entries, entry)
			}
		}
		f.Close()
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp < entries[j].Timestamp })
	return entries, nil
}

// firstCwd reads the project path a session started in, for sessions with no
// history entry. Decoding the folder name is lossy ("-" could be "/" or "-").
func firstCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for i := 0; i < 20; i++ {
		line, err := r.ReadSlice('\n')
		var e struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &e) == nil && e.Cwd != "" {
			return e.Cwd
		}
		if err == bufio.ErrBufferFull {
			for err == bufio.ErrBufferFull {
				_, err = r.ReadSlice('\n')
			}
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

func ForMachine(sessions []SessionMeta, machine string) []SessionMeta {
	var out []SessionMeta
	for _, s := range sessions {
		if s.Machine == machine {
			out = append(out, s)
		}
	}
	return out
}

// ShortID returns at most the first n characters of a session ID.
func ShortID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n]
}
