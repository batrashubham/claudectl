package index

import "time"

type SessionStatus int

const (
	StatusActive   SessionStatus = iota // Exists in ~/.claude/projects/
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

type HistoryEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

type SessionMeta struct {
	ID          string
	Project     string // Original path (e.g., "/Users/sbatra/code/Trial/trial-voice-svc")
	ProjectDir  string // Encoded dir name (e.g., "-Users-sbatra-code-Trial-trial-voice-svc")
	FirstPrompt string
	LastPrompt  string
	FirstSeen   time.Time
	LastSeen    time.Time
	PromptCount int
	Status      SessionStatus
	FileSize    int64
	SearchText  string // All prompts concatenated (lowercase) for full-text search

	// Used during index building
	activeExists    bool
	archivedExists  bool
	firstPromptTime time.Time
	lastPromptTime  time.Time
}
