// Package search builds a full-text index over session transcripts:
// what you typed (including pasted content), what Claude replied, the
// commands and file paths it touched, and tool errors.
package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

const maxLine = 256 << 20

// toolFields are the tool_use inputs worth searching: commands run, files
// touched, patterns grepped, URLs fetched.
var toolFields = []string{"command", "file_path", "path", "pattern", "url", "query", "description"}

type entry struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Input   map[string]any  `json:"input"`
	IsError bool            `json:"is_error"`
	Content json.RawMessage `json:"content"`
}

// Extract returns the searchable text of a session transcript.
func Extract(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)

	var b strings.Builder
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap pre-filter: only user/assistant entries carry searchable text.
		if !containsType(line) {
			continue
		}
		var e entry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if e.Type != "user" && e.Type != "assistant" {
			continue
		}
		appendContent(&b, e.Message.Content)
	}
	return b.String(), sc.Err()
}

func containsType(line []byte) bool {
	return bytes.Contains(line, []byte(`"type":"user"`)) || bytes.Contains(line, []byte(`"type":"assistant"`))
}

func appendContent(b *strings.Builder, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		write(b, s)
		return
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return
	}
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			write(b, bl.Text)
		case "tool_use":
			for _, k := range toolFields {
				if v, ok := bl.Input[k].(string); ok {
					write(b, v)
				}
			}
		case "tool_result":
			// Successful results are mostly file contents and command output —
			// noise. Errors are what people search for later.
			if bl.IsError {
				appendContent(b, bl.Content)
			}
		}
	}
}

func write(b *strings.Builder, s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	b.WriteString(s)
	b.WriteByte('\n')
}
