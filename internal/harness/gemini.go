package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	register("gemini", "Gemini CLI", defaultGeminiHome, func(home string) Harness { return &Gemini{home: home} })
	projectResolvers["gemini"] = func(key string, known []string) string {
		for _, p := range known {
			if GeminiProjectHash(p) == key {
				return p
			}
		}
		return ""
	}
}

// GEMINI_CLI_HOME replaces the home directory, not the .gemini dir itself.
func defaultGeminiHome() string {
	if h := os.Getenv("GEMINI_CLI_HOME"); h != "" {
		return filepath.Join(h, ".gemini")
	}
	return filepath.Join(homeDir(), ".gemini")
}

// Gemini reads Gemini CLI's data directory (verified against 0.62):
//
//	projects.json                          {"projects": {"/abs/path": "<slug>"}}
//	tmp/<slug>/.project_root               the project's absolute path
//	tmp/<slug>/chats/session-<ts>-<id8>.jsonl   event-log transcript (>= 0.39)
//	tmp/<slug>/chats/session-*.json        whole-document transcript (< 0.39)
//	tmp/<slug>/chats/<sessionId>/          subagent transcripts
//	tmp/<slug>/logs.json                   prompt log
//
// Before 0.36 <slug> was sha256(project path); those dirs are still read.
// oauth_creds.json and settings.json are never backed up.
type Gemini struct{ home string }

func (g *Gemini) Name() string        { return "gemini" }
func (g *Gemini) DisplayName() string { return "Gemini CLI" }
func (g *Gemini) Home() string        { return g.home }
func (g *Gemini) Binary() string      { return "gemini" }

func (g *Gemini) SyncRoots() []SyncRoot {
	return []SyncRoot{
		{Path: "projects.json", Mutable: true},
		{Path: "tmp", Mutable: true, Skip: geminiSkip},
	}
}

// geminiSkip keeps only transcripts and project identity from tmp/, which
// otherwise holds shell history, checkpoints and scratch files.
func geminiSkip(rel string) bool {
	rel = filepath.ToSlash(rel)
	isDir := strings.HasSuffix(rel, "/")
	parts := strings.Split(strings.TrimSuffix(rel, "/"), "/")
	switch {
	case len(parts) == 1:
		return !isDir
	case len(parts) == 2:
		if isDir {
			return parts[1] != "chats"
		}
		return parts[1] != ".project_root" && parts[1] != "logs.json"
	default:
		return parts[1] != "chats"
	}
}

func GeminiProjectHash(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func geminiSlug(project string) string {
	s := strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(filepath.Base(project)), "-"), "-")
	if s == "" {
		return "project"
	}
	return s
}

type geminiMessage struct {
	ID             string          `json:"id"`
	Timestamp      string          `json:"timestamp"`
	Type           string          `json:"type"`
	Content        json.RawMessage `json:"content"`
	DisplayContent json.RawMessage `json:"displayContent"`
	ToolCalls      []struct {
		Name string `json:"name"`
	} `json:"toolCalls"`
}

type geminiChat struct {
	SessionID   string
	ProjectHash string
	StartTime   string
	LastUpdated string
	Summary     string
	Messages    []geminiMessage
}

// readGeminiChat replays a transcript. JSONL files are an event log: a
// metadata line, message lines (a repeated id replaces the earlier line),
// {"$set": {...}} merges (a messages key replaces the whole list) and
// {"$rewindTo": id} which drops that message and everything after it.
func readGeminiChat(path string) (*geminiChat, error) {
	chat := &geminiChat{}
	if strings.HasSuffix(path, ".json") {
		var doc struct {
			SessionID   string          `json:"sessionId"`
			ProjectHash string          `json:"projectHash"`
			StartTime   string          `json:"startTime"`
			LastUpdated string          `json:"lastUpdated"`
			Summary     string          `json:"summary"`
			Messages    []geminiMessage `json:"messages"`
		}
		if err := readJSON(path, &doc); err != nil {
			return nil, err
		}
		*chat = geminiChat{doc.SessionID, doc.ProjectHash, doc.StartTime, doc.LastUpdated, doc.Summary, doc.Messages}
		return chat, nil
	}

	pos := map[string]int{}
	reindex := func() {
		pos = map[string]int{}
		for i, m := range chat.Messages {
			pos[m.ID] = i
		}
	}
	err := eachLine(path, func(line []byte) error {
		var rec map[string]json.RawMessage
		if json.Unmarshal(line, &rec) != nil {
			return nil
		}
		if raw, ok := rec["$set"]; ok {
			var set map[string]json.RawMessage
			if json.Unmarshal(raw, &set) == nil {
				chat.applyMeta(set)
				if msgs, ok := set["messages"]; ok {
					chat.Messages = nil
					json.Unmarshal(msgs, &chat.Messages)
					reindex()
				}
			}
			return nil
		}
		if raw, ok := rec["$rewindTo"]; ok {
			var id string
			json.Unmarshal(raw, &id)
			if i, ok := pos[id]; ok {
				chat.Messages = chat.Messages[:i]
				reindex()
			}
			return nil
		}
		if _, ok := rec["id"]; ok {
			var m geminiMessage
			if json.Unmarshal(line, &m) != nil {
				return nil
			}
			if i, ok := pos[m.ID]; ok {
				chat.Messages[i] = m
			} else {
				pos[m.ID] = len(chat.Messages)
				chat.Messages = append(chat.Messages, m)
			}
			return nil
		}
		// Metadata header; written again each time the session is resumed.
		chat.applyMeta(rec)
		return nil
	})
	return chat, err
}

func (c *geminiChat) applyMeta(m map[string]json.RawMessage) {
	str := func(k string) string {
		var s string
		json.Unmarshal(m[k], &s)
		return s
	}
	if v := str("sessionId"); v != "" {
		c.SessionID = v
	}
	if v := str("projectHash"); v != "" {
		c.ProjectHash = v
	}
	if v := str("startTime"); v != "" && (c.StartTime == "" || v < c.StartTime) {
		c.StartTime = v
	}
	if v := str("lastUpdated"); v > c.LastUpdated {
		c.LastUpdated = v
	}
	if v := str("summary"); v != "" {
		c.Summary = v
	}
}

func (m geminiMessage) text() string {
	if t := textContent(m.DisplayContent); t != "" {
		return t
	}
	return textContent(m.Content)
}

func geminiProjectRoot(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".project_root"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (g *Gemini) Scan(root string) ([]Session, error) {
	registry := map[string]string{} // slug -> project
	var reg struct {
		Projects map[string]string `json:"projects"`
	}
	if readJSON(filepath.Join(root, "projects.json"), &reg) == nil {
		for path, slug := range reg.Projects {
			registry[slug] = path
		}
	}

	var out []Session
	// Resuming leaves a stub file with the same sessionId; keep the fuller one.
	seen := map[string]int{}
	slugs, _ := os.ReadDir(filepath.Join(root, "tmp"))
	for _, slug := range slugs {
		if !slug.IsDir() {
			continue
		}
		dir := filepath.Join(root, "tmp", slug.Name())
		project := geminiProjectRoot(dir)
		if project == "" {
			project = registry[slug.Name()]
		}
		chats, _ := os.ReadDir(filepath.Join(dir, "chats"))
		for _, f := range chats {
			name := f.Name()
			if f.IsDir() || !strings.HasPrefix(name, "session-") || !(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".json")) {
				continue
			}
			path := filepath.Join(dir, "chats", name)
			chat, err := readGeminiChat(path)
			if err != nil || chat.SessionID == "" {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			rel := filepath.Join("tmp", slug.Name(), "chats", name)
			s := Session{
				Harness:    g.Name(),
				ID:         chat.SessionID,
				Project:    project,
				ProjectKey: chat.ProjectHash,
				Title:      chat.Summary,
				SizeBytes:  info.Size(),
				Files:      []string{rel},
			}
			if s.ProjectKey == "" {
				s.ProjectKey = slug.Name()
			}
			if exists(filepath.Join(dir, "chats", chat.SessionID)) {
				s.Files = append(s.Files, filepath.Join("tmp", slug.Name(), "chats", chat.SessionID))
			}
			for _, m := range chat.Messages {
				ts := parseTime(m.Timestamp)
				updateSpan(&s, ts)
				if m.Type == "user" {
					if text := m.text(); !isBoilerplate(text) {
						s.Prompts = append(s.Prompts, Prompt{Text: text, Time: ts})
					}
				}
			}
			updateSpan(&s, parseTime(chat.StartTime))
			updateSpan(&s, parseTime(chat.LastUpdated))
			if s.Updated.IsZero() {
				updateSpan(&s, info.ModTime())
			}
			if i, ok := seen[s.ID]; ok {
				if len(s.Prompts) > len(out[i].Prompts) || (len(s.Prompts) == len(out[i].Prompts) && s.SizeBytes > out[i].SizeBytes) {
					out[i] = s
				}
				continue
			}
			seen[s.ID] = len(out)
			out = append(out, s)
		}
	}
	return out, nil
}

func (g *Gemini) Messages(root string, s Session) ([]Message, error) {
	if len(s.Files) == 0 {
		return nil, os.ErrNotExist
	}
	chat, err := readGeminiChat(filepath.Join(root, s.Files[0]))
	if err != nil {
		return nil, err
	}
	var msgs []Message
	for _, m := range chat.Messages {
		ts := parseTime(m.Timestamp)
		switch m.Type {
		case "user":
			if text := m.text(); !isBoilerplate(text) {
				msgs = append(msgs, Message{Role: "user", Text: text, Time: ts})
			}
		case "gemini":
			if text := m.text(); text != "" {
				msgs = append(msgs, Message{Role: "assistant", Text: text, Time: ts})
			}
			for _, tc := range m.ToolCalls {
				msgs = append(msgs, Message{Role: "tool", Text: tc.Name, Time: ts})
			}
		}
	}
	return msgs, nil
}

// RestoreFiles places the transcript in the chats dir Gemini CLI will
// search when run from project: the dir already registered for that path,
// else a new slug dir. `gemini --resume` only looks in the current
// project's dir, so the slug must match the local path, not the original.
func (g *Gemini) RestoreFiles(s Session, project string) []FileCopy {
	if project == "" {
		project = s.Project
	}
	slug := g.slugFor(project)
	var out []FileCopy
	for _, f := range s.Files {
		out = append(out, FileCopy{Src: f, Dst: filepath.Join("tmp", slug, "chats", filepath.Base(f))})
	}
	return out
}

func (g *Gemini) slugFor(project string) string {
	var reg struct {
		Projects map[string]string `json:"projects"`
	}
	if readJSON(filepath.Join(g.home, "projects.json"), &reg) == nil {
		if slug, ok := reg.Projects[project]; ok && slug != "" {
			return slug
		}
	}
	base := geminiSlug(project)
	for i := 0; ; i++ {
		slug := base
		if i > 0 {
			slug = base + "-" + strconv.Itoa(i)
		}
		dir := filepath.Join(g.home, "tmp", slug)
		if !exists(dir) {
			return slug
		}
		if root := geminiProjectRoot(dir); root == project || root == "" {
			return slug
		}
	}
}

// PrepareRestore records which project a restored slug dir belongs to,
// the way Gemini CLI itself does when it creates one.
func (g *Gemini) PrepareRestore(project string) error {
	if project == "" {
		return nil
	}
	dir := filepath.Join(g.home, "tmp", g.slugFor(project))
	if geminiProjectRoot(dir) != "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".project_root"), []byte(project), 0644)
}

func (g *Gemini) ResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}
