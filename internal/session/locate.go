package session

import (
	"os"
	"path/filepath"
)

type Locator struct {
	claudeDir string
	backupDir string
}

func NewLocator(claudeDir, backupDir string) *Locator {
	return &Locator{claudeDir: claudeDir, backupDir: backupDir}
}

type Location struct {
	ActivePath   string // Path in ~/.claude/projects/ (empty if not present)
	ArchivedPath string // Path in backup dir (empty if not present)
}

func (l *Locator) Locate(sessionID, projectDir string) Location {
	loc := Location{}

	activePath := filepath.Join(l.claudeDir, "projects", projectDir, sessionID+".jsonl")
	if _, err := os.Stat(activePath); err == nil {
		loc.ActivePath = activePath
	}

	archivedPath := filepath.Join(l.backupDir, "projects", projectDir, sessionID+".jsonl")
	if _, err := os.Stat(archivedPath); err == nil {
		loc.ArchivedPath = archivedPath
	}

	return loc
}
