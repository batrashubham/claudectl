package harness

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func init() {
	register("codex", "Codex", defaultCodexHome, func(home string) Harness { return &Codex{home: home} })
}

func defaultCodexHome() string {
	return envOr("CODEX_HOME", homeDir(), ".codex")
}

// Codex reads OpenAI Codex CLI's data directory:
//
//	history.jsonl                                    {"session_id","ts","text"} per prompt
//	sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl    the transcript ("rollout")
//	archived_sessions/rollout-...jsonl               rollouts archived by the user
//
// auth.json and config.toml hold credentials and settings and are never
// backed up.
type Codex struct{ home string }

func (c *Codex) Name() string        { return "codex" }
func (c *Codex) DisplayName() string { return "Codex" }
func (c *Codex) Home() string        { return c.home }
func (c *Codex) Binary() string      { return "codex" }

func (c *Codex) SyncRoots() []SyncRoot {
	return []SyncRoot{
		{Path: "history.jsonl"},
		{Path: "sessions"},
		{Path: "archived_sessions"},
	}
}

var rolloutName = regexp.MustCompile(`^rollout-(\d{4})-(\d{2})-(\d{2})T[\d-]+-(.+)\.jsonl$`)

type codexRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type codexItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Name    string          `json:"name"`
	Message string          `json:"message"`
}

func (c *Codex) Scan(root string) ([]Session, error) {
	prompts := map[string][]Prompt{}
	err := eachLine(filepath.Join(root, "history.jsonl"), func(line []byte) error {
		var e struct {
			SessionID string `json:"session_id"`
			Ts        int64  `json:"ts"`
			Text      string `json:"text"`
		}
		if json.Unmarshal(line, &e) == nil && e.SessionID != "" {
			prompts[e.SessionID] = append(prompts[e.SessionID], Prompt{Text: e.Text, Time: time.Unix(e.Ts, 0)})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var out []Session
	for _, dir := range []string{"sessions", "archived_sessions"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			s := Session{Harness: c.Name(), Files: []string{rel}, SizeBytes: info.Size()}
			if m := rolloutName.FindStringSubmatch(d.Name()); m != nil {
				s.ID = m[4]
			}
			c.readRollout(path, &s, prompts[s.ID] == nil)
			if s.ID == "" {
				return nil
			}
			if p, ok := prompts[s.ID]; ok {
				s.Prompts = p
				for _, pr := range p {
					updateSpan(&s, pr.Time)
				}
			}
			updateFromFile(&s, path, info)
			out = append(out, s)
			return nil
		})
	}
	return out, nil
}

// readRollout takes the session id and cwd from the session_meta header,
// and, when history.jsonl has nothing for the session, its typed prompts.
//
// Older rollouts record each prompt twice (an event_msg "user_message" and
// a response_item); newer ones only as a response_item. Prefer the events
// when present so prompts are never double counted.
func (c *Codex) readRollout(path string, s *Session, wantPrompts bool) {
	var events, items []Prompt
	n := 0
	eachLine(path, func(line []byte) error {
		n++
		if n == 1 {
			var rec codexRecord
			var meta codexMeta
			if json.Unmarshal(line, &rec) == nil && rec.Type == "session_meta" {
				json.Unmarshal(rec.Payload, &meta)
			} else {
				// Rollouts from early 2025 start with a bare header object.
				json.Unmarshal(line, &meta)
			}
			if meta.ID != "" {
				s.ID = meta.ID
			}
			s.Project = meta.Cwd
			updateSpan(s, parseTime(meta.Timestamp))
			return nil
		}
		if s.Project == "" && bytes.Contains(line, []byte(`"turn_context"`)) {
			var rec codexRecord
			var ctx struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &rec) == nil && json.Unmarshal(rec.Payload, &ctx) == nil {
				s.Project = ctx.Cwd
			}
		}
		if !wantPrompts {
			if s.Project != "" {
				return errStop
			}
			return nil
		}
		isEvent := bytes.Contains(line, []byte(`"user_message"`))
		if !isEvent && !bytes.Contains(line, []byte(`"role":"user"`)) {
			return nil
		}
		var rec codexRecord
		var item codexItem
		if json.Unmarshal(line, &rec) != nil || json.Unmarshal(rec.Payload, &item) != nil {
			return nil
		}
		ts := parseTime(rec.Timestamp)
		switch {
		case rec.Type == "event_msg" && item.Type == "user_message":
			if !isBoilerplate(item.Message) {
				events = append(events, Prompt{Text: item.Message, Time: ts})
			}
		case rec.Type == "response_item" && item.Type == "message" && item.Role == "user":
			if text := textContent(item.Content); !isBoilerplate(text) {
				items = append(items, Prompt{Text: text, Time: ts})
			}
		}
		return nil
	})
	if len(events) > 0 {
		s.Prompts = events
	} else {
		s.Prompts = items
	}
}

func (c *Codex) Messages(root string, s Session) ([]Message, error) {
	if len(s.Files) == 0 {
		return nil, os.ErrNotExist
	}
	var msgs []Message
	err := eachLine(filepath.Join(root, s.Files[0]), func(line []byte) error {
		var rec codexRecord
		if json.Unmarshal(line, &rec) != nil {
			return nil
		}
		raw := rec.Payload
		if rec.Type != "response_item" {
			if rec.Type != "" || len(raw) > 0 {
				return nil
			}
			raw = line // early-2025 rollouts store items unwrapped
		}
		var item codexItem
		if json.Unmarshal(raw, &item) != nil {
			return nil
		}
		ts := parseTime(rec.Timestamp)
		switch item.Type {
		case "message":
			text := textContent(item.Content)
			if item.Role == "user" && isBoilerplate(text) {
				return nil
			}
			if item.Role != "user" && item.Role != "assistant" {
				return nil
			}
			if text != "" {
				msgs = append(msgs, Message{Role: item.Role, Text: text, Time: ts})
			}
		case "function_call", "custom_tool_call", "local_shell_call":
			name := item.Name
			if name == "" {
				name = "shell"
			}
			msgs = append(msgs, Message{Role: "tool", Text: name, Time: ts})
		}
		return nil
	})
	return msgs, err
}

// RestoreFiles puts archived rollouts back under sessions/ by date, where
// `codex resume` looks for them. Rollouts are keyed by id, not project.
func (c *Codex) RestoreFiles(s Session, project string) []FileCopy {
	var out []FileCopy
	for _, f := range s.Files {
		dst := f
		name := filepath.Base(f)
		if strings.HasPrefix(filepath.ToSlash(f), "archived_sessions/") {
			if m := rolloutName.FindStringSubmatch(name); m != nil {
				dst = filepath.Join("sessions", m[1], m[2], m[3], name)
			} else {
				dst = filepath.Join("sessions", name)
			}
		}
		out = append(out, FileCopy{Src: f, Dst: dst})
	}
	return out
}

func (c *Codex) ResumeArgs(s Session) []string {
	return []string{"resume", s.ID}
}
