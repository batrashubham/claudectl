package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	register("qwen", "Qwen Code", defaultQwenHome, func(home string) Harness { return &Qwen{home: home} })
}

func defaultQwenHome() string {
	return envOr("QWEN_HOME", homeDir(), ".qwen")
}

// Qwen reads Qwen Code's data directory (from 0.24 source):
//
//	projects/<sanitized-cwd>/chats/<sessionId>.jsonl       transcript
//	projects/<sanitized-cwd>/chats/<sessionId>.*           sidecars (ledger, worktree)
//	projects/<sanitized-cwd>/chats/archive/<sessionId>.jsonl
//
// The cwd is sanitized like Claude Code's (non-alphanumerics become '-').
type Qwen struct{ home string }

func (q *Qwen) Name() string        { return "qwen" }
func (q *Qwen) DisplayName() string { return "Qwen Code" }
func (q *Qwen) Home() string        { return q.home }
func (q *Qwen) Binary() string      { return "qwen" }

func (q *Qwen) SyncRoots() []SyncRoot {
	return []SyncRoot{{Path: "projects", Skip: func(rel string) bool {
		parts := strings.Split(strings.TrimSuffix(filepath.ToSlash(rel), "/"), "/")
		return len(parts) >= 2 && parts[1] != "chats"
	}}}
}

type qwenRecord struct {
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Role  string `json:"role"`
		Parts []struct {
			Text         string `json:"text"`
			Thought      bool   `json:"thought"`
			FunctionCall *struct {
				Name string `json:"name"`
			} `json:"functionCall"`
		} `json:"parts"`
	} `json:"message"`
}

func (r qwenRecord) text() string {
	var b []string
	for _, p := range r.Message.Parts {
		if p.Text != "" && !p.Thought {
			b = append(b, p.Text)
		}
	}
	return strings.Join(b, "\n")
}

func (q *Qwen) Scan(root string) ([]Session, error) {
	var out []Session
	projects, _ := os.ReadDir(filepath.Join(root, "projects"))
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		for _, sub := range []string{"chats", filepath.Join("chats", "archive")} {
			dir := filepath.Join(root, "projects", p.Name(), sub)
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				name := f.Name()
				id := strings.TrimSuffix(name, ".jsonl")
				if f.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.Contains(id, ".") {
					continue
				}
				info, err := f.Info()
				if err != nil {
					continue
				}
				s := Session{Harness: q.Name(), ID: id, ProjectKey: p.Name(), SizeBytes: info.Size()}
				for _, other := range files {
					if strings.HasPrefix(other.Name(), id) {
						s.Files = append(s.Files, filepath.Join("projects", p.Name(), sub, other.Name()))
					}
				}
				// Transcript first.
				for i, f := range s.Files {
					if filepath.Base(f) == name {
						s.Files[0], s.Files[i] = s.Files[i], s.Files[0]
					}
				}
				eachLine(filepath.Join(dir, name), func(line []byte) error {
					var r qwenRecord
					if json.Unmarshal(line, &r) != nil {
						return nil
					}
					ts := parseTime(r.Timestamp)
					updateSpan(&s, ts)
					if s.Project == "" {
						s.Project = r.Cwd
					}
					if r.Type == "user" && r.Message.Role == "user" {
						if text := r.text(); !isBoilerplate(text) {
							s.Prompts = append(s.Prompts, Prompt{Text: text, Time: ts})
						}
					}
					return nil
				})
				if s.Project == "" {
					s.Project = decodeClaudeProject(p.Name())
				}
				if s.Updated.IsZero() {
					updateSpan(&s, info.ModTime())
				}
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (q *Qwen) Messages(root string, s Session) ([]Message, error) {
	if len(s.Files) == 0 {
		return nil, os.ErrNotExist
	}
	var msgs []Message
	err := eachLine(filepath.Join(root, s.Files[0]), func(line []byte) error {
		var r qwenRecord
		if json.Unmarshal(line, &r) != nil {
			return nil
		}
		ts := parseTime(r.Timestamp)
		switch r.Type {
		case "user":
			if text := r.text(); !isBoilerplate(text) {
				msgs = append(msgs, Message{Role: "user", Text: text, Time: ts})
			}
		case "assistant":
			if text := r.text(); text != "" {
				msgs = append(msgs, Message{Role: "assistant", Text: text, Time: ts})
			}
			for _, p := range r.Message.Parts {
				if p.FunctionCall != nil {
					msgs = append(msgs, Message{Role: "tool", Text: p.FunctionCall.Name, Time: ts})
				}
			}
		}
		return nil
	})
	return msgs, err
}

// RestoreFiles un-archives and, for another machine's path, re-keys the
// project dir, since Qwen looks sessions up under the current cwd.
func (q *Qwen) RestoreFiles(s Session, project string) []FileCopy {
	key := s.ProjectKey
	if project != "" && project != s.Project {
		key = EncodeClaudeProject(project)
	}
	var out []FileCopy
	for _, f := range s.Files {
		out = append(out, FileCopy{Src: f, Dst: filepath.Join("projects", key, "chats", filepath.Base(f))})
	}
	return out
}

func (q *Qwen) ResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}
