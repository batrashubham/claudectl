package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func init() {
	register("claude", "Claude Code", defaultClaudeHome, func(home string) Harness { return &Claude{home: home} })
}

func defaultClaudeHome() string {
	return envOr("CLAUDE_CONFIG_DIR", homeDir(), ".claude")
}

// Claude reads Claude Code's data directory:
//
//	history.jsonl                       one line per prompt typed, with sessionId
//	projects/<encoded-cwd>/<id>.jsonl   the transcript
//	projects/<encoded-cwd>/<id>/        subagents and tool results
type Claude struct{ home string }

func (c *Claude) Name() string        { return "claude" }
func (c *Claude) DisplayName() string { return "Claude Code" }
func (c *Claude) Home() string        { return c.home }
func (c *Claude) Binary() string      { return "claude" }

func (c *Claude) SyncRoots() []SyncRoot {
	return []SyncRoot{
		{Path: "history.jsonl"},
		{Path: "projects", Skip: claudeSkip},
	}
}

// claudeSkip leaves out projects/<dir>/memory/: auto-memory is per-machine
// working state, not session history. Only that exact component counts, so
// a project whose own path ends in "memory" is still backed up.
func claudeSkip(rel string) bool {
	parts := strings.Split(strings.TrimSuffix(filepath.ToSlash(rel), "/"), "/")
	return len(parts) >= 2 && parts[1] == "memory"
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// EncodeClaudeProject mirrors how Claude Code names a project's directory:
// every non-alphanumeric character of the cwd becomes '-'.
func EncodeClaudeProject(path string) string {
	return nonAlnum.ReplaceAllString(path, "-")
}

// decodeClaudeProject is a best-effort inverse; it is lossy because '-',
// '.', '_' and '/' all encode to '-'. Only used when nothing better exists.
func decodeClaudeProject(dir string) string {
	if strings.HasPrefix(dir, "-") {
		return "/" + strings.ReplaceAll(dir[1:], "-", "/")
	}
	return dir
}

type claudeHistoryEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

func (c *Claude) Scan(root string) ([]Session, error) {
	byID := map[string]*Session{}
	var order []string
	get := func(id string) *Session {
		s, ok := byID[id]
		if !ok {
			s = &Session{Harness: c.Name(), ID: id}
			byID[id] = s
			order = append(order, id)
		}
		return s
	}

	err := eachLine(filepath.Join(root, "history.jsonl"), func(line []byte) error {
		var e claudeHistoryEntry
		if json.Unmarshal(line, &e) != nil || e.SessionID == "" {
			return nil
		}
		s := get(e.SessionID)
		if s.Project == "" && e.Project != "" {
			s.Project = e.Project
			s.ProjectKey = EncodeClaudeProject(e.Project)
		}
		ts := time.UnixMilli(e.Timestamp)
		s.Prompts = append(s.Prompts, Prompt{Text: e.Display, Time: ts})
		updateSpan(s, ts)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	projectsDir := filepath.Join(root, "projects")
	projects, _ := os.ReadDir(projectsDir)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(projectsDir, p.Name()))
		for _, f := range files {
			name := f.Name()
			// agent-*.jsonl are subagent sidechains written by older versions;
			// they can't be resumed on their own.
			if f.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, "agent-") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			id := strings.TrimSuffix(name, ".jsonl")
			s := get(id)
			// The file's actual location beats re-encoding the history path.
			s.ProjectKey = p.Name()
			rel := filepath.Join("projects", p.Name(), name)
			s.Files = []string{rel}
			if exists(filepath.Join(projectsDir, p.Name(), id)) {
				s.Files = append(s.Files, filepath.Join("projects", p.Name(), id))
			}
			s.SizeBytes = info.Size()
			if s.Project == "" || len(s.Prompts) == 0 {
				c.fillFromTranscript(filepath.Join(root, rel), s)
			}
			if s.Project == "" {
				s.Project = decodeClaudeProject(p.Name())
			}
			updateFromFile(s, filepath.Join(root, rel), info)
		}
	}

	out := make([]Session, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

type claudeEntry struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// fillFromTranscript recovers the cwd and opening prompt from the head of a
// transcript, for sessions that history.jsonl doesn't describe (it gets
// pruned, and non-interactive runs never appear in it).
func (c *Claude) fillFromTranscript(path string, s *Session) {
	n := 0
	eachLine(path, func(line []byte) error {
		n++
		if n > 200 {
			return errStop
		}
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil {
			return nil
		}
		if s.Project == "" && e.Cwd != "" {
			s.Project = e.Cwd
		}
		if len(s.Prompts) == 0 && e.Type == "user" && !e.IsMeta && !e.IsSidechain {
			if text := textContent(e.Message.Content); !isBoilerplate(text) {
				s.Prompts = append(s.Prompts, Prompt{Text: text, Time: parseTime(e.Timestamp), Fallback: true})
			}
		}
		if s.Project != "" && len(s.Prompts) > 0 {
			return errStop
		}
		return nil
	})
}

func (c *Claude) Messages(root string, s Session) ([]Message, error) {
	if len(s.Files) == 0 {
		return nil, os.ErrNotExist
	}
	var msgs []Message
	err := eachLine(filepath.Join(root, s.Files[0]), func(line []byte) error {
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil || e.IsMeta || e.IsSidechain {
			return nil
		}
		if e.Type != "user" && e.Type != "assistant" {
			return nil
		}
		ts := parseTime(e.Timestamp)
		for _, m := range claudeContent(e.Type, e.Message.Content) {
			m.Time = ts
			msgs = append(msgs, m)
		}
		return nil
	})
	return msgs, err
}

func claudeContent(role string, raw json.RawMessage) []Message {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if role == "user" && isBoilerplate(s) {
			return nil
		}
		return []Message{{Role: role, Text: s}}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var out []Message
	for _, p := range parts {
		switch p.Type {
		case "text":
			if role == "user" && isBoilerplate(p.Text) {
				continue
			}
			out = append(out, Message{Role: role, Text: p.Text})
		case "tool_use":
			out = append(out, Message{Role: "tool", Text: p.Name})
		}
	}
	return out
}

func (c *Claude) RestoreFiles(s Session, project string) []FileCopy {
	dstKey := s.ProjectKey
	if project != "" && project != s.Project {
		dstKey = EncodeClaudeProject(project)
	}
	var out []FileCopy
	for _, f := range s.Files {
		parts := strings.SplitN(filepath.ToSlash(f), "/", 3)
		if len(parts) != 3 {
			continue
		}
		out = append(out, FileCopy{Src: f, Dst: filepath.Join("projects", dstKey, parts[2])})
	}
	return out
}

func (c *Claude) ResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}
