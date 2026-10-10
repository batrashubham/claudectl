package session

import (
	"os"
	"path/filepath"

	"github.com/batrashubham/claudectl/internal/index"
)

type Locator struct {
	claudeDir string
	backupDir string
	machine   string
}

func NewLocator(claudeDir, backupDir, machine string) *Locator {
	return &Locator{claudeDir: claudeDir, backupDir: backupDir, machine: machine}
}

type Location struct {
	ActivePath      string // Path in ~/.claude/projects/ (empty if not present)
	ArchivedPath    string // Path in backup dir (empty if not present)
	ArchivedMachine string // Machine whose backup ArchivedPath is in
}

// archiveDirs is the search order for a backed-up session: this machine,
// the pre-machines flat layout, then other machines.
func (l *Locator) archiveDirs() (dirs, machines []string) {
	dirs = []string{filepath.Join(l.backupDir, "machines", l.machine), l.backupDir}
	machines = []string{l.machine, l.machine}
	for _, m := range index.Machines(l.backupDir) {
		if m != l.machine {
			dirs = append(dirs, filepath.Join(l.backupDir, "machines", m))
			machines = append(machines, m)
		}
	}
	return dirs, machines
}

func (l *Locator) Locate(sessionID, projectDir string) Location {
	loc := Location{}

	activePath := filepath.Join(l.claudeDir, "projects", projectDir, sessionID+".jsonl")
	if _, err := os.Stat(activePath); err == nil {
		loc.ActivePath = activePath
	}

	dirs, machines := l.archiveDirs()
	for i, dir := range dirs {
		archivedPath := filepath.Join(dir, "projects", projectDir, sessionID+".jsonl")
		if _, err := os.Stat(archivedPath); err == nil {
			loc.ArchivedPath = archivedPath
			loc.ArchivedMachine = machines[i]
			break
		}
	}

	return loc
}

// Path returns the best copy of a session file: live first, then backup.
func (l *Locator) Path(sessionID, projectDir string) string {
	loc := l.Locate(sessionID, projectDir)
	if loc.ActivePath != "" {
		return loc.ActivePath
	}
	return loc.ArchivedPath
}
