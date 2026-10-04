package index

import (
	"time"

	"github.com/batrashubham/claudectl/internal/harness"
)

type SessionStatus int

const (
	StatusActive   SessionStatus = iota // Exists in the agent's live data dir
	StatusArchived                      // Only in backup
)

func (s SessionStatus) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusArchived:
		return "archived"
	default:
		return "unknown"
	}
}

// HistoryEntry is one line of Claude Code's history.jsonl.
type HistoryEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

// Location is one place a session's files were found.
type Location struct {
	Machine    string
	Root       string // harness-layout root the Files are relative to
	Live       bool
	Files      []string
	Project    string
	ProjectKey string
	Size       int64
}

type SessionMeta struct {
	ID          string
	Harness     string // "claude", "codex", ...
	Machine     string // machine the session's files come from
	Project     string // Original path (e.g., "/Users/sbatra/code/Trial/trial-voice-svc")
	ProjectDir  string // Agent's project key (Claude: "-Users-sbatra-code-Trial-trial-voice-svc")
	Title       string
	FirstPrompt string
	LastPrompt  string
	FirstSeen   time.Time
	LastSeen    time.Time
	PromptCount int
	Status      SessionStatus
	FileSize    int64
	SearchText  string // All prompts concatenated (lowercase) for full-text search

	Locations []Location       `json:"-"`
	Prompts   []harness.Prompt `json:"-"`
}

// Ghost sessions are known only from prompt history; their transcript was
// deleted before it was ever backed up, so they can't be resumed.
func (s SessionMeta) IsGhost() bool { return s.FileSize == 0 }

// Key identifies a session across harnesses, which may reuse ID formats.
func (s SessionMeta) Key() string { return s.Harness + ":" + s.ID }

// Best returns the location to read or restore from: the live copy if
// there is one, otherwise the most complete backup (transcripts only grow,
// so the largest holds the most turns), preferring this machine's on a tie.
func (s SessionMeta) Best(localMachine string) (Location, bool) {
	var best *Location
	for i := range s.Locations {
		l := &s.Locations[i]
		if len(l.Files) == 0 {
			continue
		}
		if l.Live {
			return *l, true
		}
		if best == nil || l.Size > best.Size || (l.Size == best.Size && l.Machine == localMachine && best.Machine != localMachine) {
			best = l
		}
	}
	if best == nil {
		return Location{}, false
	}
	return *best, true
}
