package meta

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Prompt is a message the user typed, read from the transcript. Sessions from
// the Claude Desktop app never reach history.jsonl, so this is the only record
// of their prompts.
type Prompt struct {
	UUID string
	Text string
	Time time.Time
}

// IsCommand reports whether the prompt is a slash command.
func (p Prompt) IsCommand() bool {
	return strings.HasPrefix(p.Text, "<command-name>")
}

const firstPromptLen = 200

var (
	toolResultID   = []byte(`"content":[{"tool_use_id"`)
	toolResultType = []byte(`"content":[{"type":"tool_result"`)
	injected       = []string{"<task-notification>", "<local-command-stdout>", "<local-command-stderr>", "<local-command-caveat>", "<artifact-content-"}
)

// ReadPrompts returns the transcript's prompts in order, once each: the
// desktop app re-appends earlier entries under the same uuid.
func ReadPrompts(path string) []Prompt {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var prompts []Prompt
	seen := make(map[string]bool)
	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if p, ok := userPrompt(line); ok && !seen[p.UUID] {
			seen[p.UUID] = true
			prompts = append(prompts, p)
		}
		if err != nil {
			return prompts
		}
	}
}

func userPrompt(line []byte) (Prompt, bool) {
	if !bytes.Contains(line, typeUser) || bytes.Contains(line, toolResultID) || bytes.Contains(line, toolResultType) {
		return Prompt{}, false
	}
	var e struct {
		Type             string    `json:"type"`
		UUID             string    `json:"uuid"`
		IsMeta           bool      `json:"isMeta"`
		IsCompactSummary bool      `json:"isCompactSummary"`
		Timestamp        time.Time `json:"timestamp"`
		Message          struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "user" || e.Message.Role != "user" ||
		e.UUID == "" || e.IsMeta || e.IsCompactSummary {
		return Prompt{}, false
	}
	text := strings.TrimSpace(contentText(e.Message.Content))
	if text == "" {
		return Prompt{}, false
	}
	for _, prefix := range injected {
		if strings.HasPrefix(text, prefix) {
			return Prompt{}, false
		}
	}
	return Prompt{UUID: e.UUID, Text: text, Time: e.Timestamp}, true
}

func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	images := false
	for _, p := range parts {
		switch p.Type {
		case "text":
			texts = append(texts, p.Text)
		case "image":
			images = true
		}
	}
	if len(texts) == 0 && images {
		return "[image]"
	}
	return strings.Join(texts, "\n")
}
