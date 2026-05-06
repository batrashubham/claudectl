package index

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Builder struct {
	claudeDir string
	backupDir string
}

func NewBuilder(claudeDir, backupDir string) *Builder {
	return &Builder{claudeDir: claudeDir, backupDir: backupDir}
}

func (b *Builder) Build() ([]SessionMeta, error) {
	sessions := make(map[string]*SessionMeta)

	if err := b.parseHistory(sessions); err != nil {
		return nil, err
	}

	b.scanProjectsDir(sessions, b.claudeDir, StatusActive)
	b.scanProjectsDir(sessions, b.backupDir, StatusArchived)

	result := make([]SessionMeta, 0, len(sessions))
	for _, s := range sessions {
		b.resolveStatus(s)
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].LastSeen.After(result[j].LastSeen)
	})

	return result, nil
}

func (b *Builder) parseHistory(sessions map[string]*SessionMeta) error {
	// Merge both live and backup history files for resilience.
	// If Claude cleans the live history.jsonl, the backup still has all prior entries.
	seen := make(map[string]bool) // dedup key: sessionID+timestamp

	livePath := filepath.Join(b.claudeDir, "history.jsonl")
	backupPath := filepath.Join(b.backupDir, "history.jsonl")

	b.parseHistoryFile(livePath, sessions, seen)
	b.parseHistoryFile(backupPath, sessions, seen)

	return nil
}

func (b *Builder) parseHistoryFile(path string, sessions map[string]*SessionMeta, seen map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

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
				ProjectDir: projectToDir(entry.Project),
			}
			sessions[entry.SessionID] = s
		}

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

func (b *Builder) scanProjectsDir(sessions map[string]*SessionMeta, baseDir string, markStatus SessionStatus) {
	projectsDir := filepath.Join(baseDir, "projects")
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
				s = &SessionMeta{
					ID:         sessionID,
					ProjectDir: projDir,
					Project:    dirToProject(projDir),
				}
				sessions[sessionID] = s
			}

			if markStatus == StatusActive {
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

func projectToDir(project string) string {
	return strings.ReplaceAll(project, "/", "-")
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
	historyPath := filepath.Join(b.claudeDir, "history.jsonl")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		historyPath = filepath.Join(b.backupDir, "history.jsonl")
	}

	f, err := os.Open(historyPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.SessionID == sessionID && entry.Display != "" && !isCommand(entry.Display) {
			entries = append(entries, entry)
		}
	}

	return entries, scanner.Err()
}
