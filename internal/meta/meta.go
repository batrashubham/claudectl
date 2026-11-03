// Package meta extracts session metadata Claude Code writes into transcripts:
// the /rename title, the generated title, git branch, model, Claude Code
// version, linked PRs, worktree and agent names.
package meta

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Repo   string `json:"repo"`
}

type Info struct {
	CustomTitle   string `json:"customTitle,omitempty"`
	AITitle       string `json:"aiTitle,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Model         string `json:"model,omitempty"`
	ClaudeVersion string `json:"claudeVersion,omitempty"`
	Worktree      string `json:"worktree,omitempty"`
	Agent         string `json:"agent,omitempty"`
	PRs           []PR   `json:"prs,omitempty"`
	Prompts       int    `json:"prompts,omitempty"`
	FirstPrompt   string `json:"firstPrompt,omitempty"`
}

// CacheDir is where metadata is cached between runs.
func CacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudectl", "cache", "meta")
}

// Title is the name to show: the /rename title, else Claude's generated one.
func (i Info) Title() string {
	if i.CustomTitle != "" {
		return i.CustomTitle
	}
	return i.AITitle
}

// cacheEntry records how far into the transcript Info has been computed.
// Transcripts are append-only, so only bytes past Offset need reading.
type cacheEntry struct {
	Version   int      `json:"v"`
	Size      int64    `json:"size"`
	Offset    int64    `json:"offset"`
	Info      Info     `json:"info"`
	PromptIDs []string `json:"promptIds,omitempty"`
}

const cacheVersion = 2

// Read returns metadata for the transcript at path. With a cacheDir it reads
// only what was appended since the last call.
func Read(path, id, cacheDir string) Info {
	st, err := os.Stat(path)
	if err != nil {
		return Info{}
	}

	var c cacheEntry
	cachePath := ""
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, id+".json")
		if data, err := os.ReadFile(cachePath); err == nil {
			json.Unmarshal(data, &c)
		}
		if c.Version != cacheVersion || c.Offset > st.Size() {
			c = cacheEntry{}
		}
		if c.Size == st.Size() {
			return c.Info
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return c.Info
	}
	defer f.Close()
	if _, err := f.Seek(c.Offset, io.SeekStart); err != nil {
		return c.Info
	}
	seen := make(map[string]bool, len(c.PromptIDs))
	for _, id := range c.PromptIDs {
		seen[id] = true
	}
	c.Offset += scan(f, &c.Info, seen)
	c.Size = st.Size()
	c.Version = cacheVersion
	c.PromptIDs = c.PromptIDs[:0]
	for id := range seen {
		c.PromptIDs = append(c.PromptIDs, id)
	}

	if cachePath != "" {
		if data, err := json.Marshal(c); err == nil {
			os.MkdirAll(cacheDir, 0755)
			os.WriteFile(cachePath, data, 0644)
		}
	}
	return c.Info
}

var (
	typeUser      = []byte(`"type":"user"`)
	typeAssistant = []byte(`"type":"assistant"`)
	keyBranch     = []byte(`"gitBranch":"`)
	keyVersion    = []byte(`"version":"`)
	keyModel      = []byte(`"model":"`)
)

// scan applies every complete line in r to info and returns the bytes
// consumed. A trailing partial line (still being written) is left for later.
func scan(r io.Reader, info *Info, seen map[string]bool) int64 {
	br := bufio.NewReaderSize(r, 256*1024)
	var consumed int64
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return consumed
		}
		consumed += int64(len(line))
		apply(line, info, seen)
	}
}

func apply(line []byte, info *Info, seen map[string]bool) {
	// Conversation lines can be megabytes; pull the few header fields with a
	// byte search instead of decoding the whole entry.
	if bytes.Contains(line, typeUser) || bytes.Contains(line, typeAssistant) {
		// Top-level gitBranch/version follow the message body, which may hold
		// tool inputs with the same keys, so take the last match; model is the
		// first key inside message.
		if v := field(line, keyBranch, true); v != "" && v != "HEAD" {
			info.Branch = v
		}
		if v := field(line, keyVersion, true); v != "" {
			info.ClaudeVersion = v
		}
		if bytes.Contains(line, typeAssistant) {
			if v := field(line, keyModel, false); v != "" && v != "<synthetic>" {
				info.Model = v
			}
		}
		if p, ok := userPrompt(line); ok && !seen[p.UUID] {
			seen[p.UUID] = true
			info.Prompts++
			if info.FirstPrompt == "" && !p.IsCommand() {
				info.FirstPrompt = truncRunes(p.Text, firstPromptLen)
			}
		}
		return
	}

	var e struct {
		Type            string `json:"type"`
		CustomTitle     string `json:"customTitle"`
		AITitle         string `json:"aiTitle"`
		AgentName       string `json:"agentName"`
		PRNumber        int    `json:"prNumber"`
		PRURL           string `json:"prUrl"`
		PRRepository    string `json:"prRepository"`
		WorktreeSession *struct {
			WorktreeName string `json:"worktreeName"`
		} `json:"worktreeSession"`
	}
	if json.Unmarshal(line, &e) != nil {
		return
	}
	switch e.Type {
	case "custom-title":
		info.CustomTitle = e.CustomTitle
	case "ai-title":
		info.AITitle = e.AITitle
	case "agent-name":
		info.Agent = e.AgentName
	case "worktree-state":
		if e.WorktreeSession != nil && e.WorktreeSession.WorktreeName != "" {
			info.Worktree = e.WorktreeSession.WorktreeName
		}
	case "pr-link":
		if e.PRURL == "" {
			return
		}
		for _, p := range info.PRs {
			if p.URL == e.PRURL {
				return
			}
		}
		info.PRs = append(info.PRs, PR{Number: e.PRNumber, URL: e.PRURL, Repo: e.PRRepository})
	}
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// field returns the string value following key, e.g. `"gitBranch":"main"`.
func field(line, key []byte, last bool) string {
	i := bytes.Index(line, key)
	if last {
		i = bytes.LastIndex(line, key)
	}
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 || j > 256 {
		return ""
	}
	return string(rest[:j])
}
