package harness

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// Transcript lines can embed whole files or base64 images, so lines well
// beyond bufio's 64KB default are normal.
const maxLine = 64 * 1024 * 1024

var errStop = errors.New("stop")

// eachLine calls fn for every non-empty line of a file. fn returning
// errStop ends the scan early without error.
func eachLine(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return eachLineReader(f, fn)
}

func eachLineReader(r io.Reader, fn func(line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			if err == errStop {
				return nil
			}
			return err
		}
	}
	return sc.Err()
}

var timestampField = regexp.MustCompile(`"timestamp":"([^"]{10,40})"`)

// lastTimestamp finds the newest "timestamp" in the tail of a JSONL file.
// File mtimes can't be used for backups: a git checkout resets them.
func lastTimestamp(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}
	}
	const window = 256 * 1024
	off := info.Size() - window
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return time.Time{}
	}
	var latest time.Time
	for _, m := range timestampField.FindAllSubmatch(buf, -1) {
		if t := parseTime(string(m[1])); t.After(latest) {
			latest = t
		}
	}
	return latest
}

// updateFromFile extends a session's span using the file's own
// timestamps, falling back to its mtime only when it has none.
func updateFromFile(s *Session, path string, info os.FileInfo) {
	if t := lastTimestamp(path); !t.IsZero() {
		updateSpan(s, t)
		return
	}
	updateSpan(s, info.ModTime())
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// textContent flattens the content shapes agents use: a plain string, or an
// array of typed parts where the text lives under "text".
func textContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
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
	var b strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if p.Type != "" && p.Type != "text" && p.Type != "input_text" && p.Type != "output_text" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// isBoilerplate reports text agents inject into user turns themselves
// (environment context, instructions, command wrappers) rather than
// something the person typed.
func isBoilerplate(text string) bool {
	t := strings.TrimSpace(text)
	return t == "" || strings.HasPrefix(t, "<")
}

func updateSpan(s *Session, t time.Time) {
	if t.IsZero() {
		return
	}
	if s.Start.IsZero() || t.Before(s.Start) {
		s.Start = t
	}
	if t.After(s.Updated) {
		s.Updated = t
	}
}
