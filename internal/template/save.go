package template

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var trimTypes = map[string]bool{
	"last-prompt":          true,
	"custom-title":         true,
	"agent-name":           true,
	"queue-operation":      true,
	"file-history-snapshot": true,
}

type SaveOptions struct {
	SessionID   string
	ProjectDir  string
	Project     string
	Name        string
	Description string
	Trim        bool
	Force       bool
}

func (s *Store) Save(opts SaveOptions) error {
	if err := ValidateName(opts.Name); err != nil {
		return err
	}

	if s.Exists(opts.ProjectDir, opts.Name) && !opts.Force {
		return fmt.Errorf("template '%s' already exists (use --force to overwrite)", opts.Name)
	}

	// Find the session file
	sessionFile := filepath.Join(s.claudeDir, "projects", opts.ProjectDir, opts.SessionID+".jsonl")
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		return fmt.Errorf("session file not found: %s", sessionFile)
	}

	// Create template directory
	tmplDir := s.templateDir(opts.ProjectDir, opts.Name)
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		return err
	}

	// Copy/trim JSONL
	entryCount, sizeBytes, err := s.copySession(sessionFile, s.sessionPath(opts.ProjectDir, opts.Name), opts.Trim)
	if err != nil {
		os.RemoveAll(tmplDir)
		return fmt.Errorf("copy session: %w", err)
	}

	// Copy companion directory (subagents, tool-results)
	hasSubagents := false
	companionDir := filepath.Join(filepath.Dir(sessionFile), opts.SessionID)
	if info, statErr := os.Stat(companionDir); statErr == nil && info.IsDir() {
		subagentsDir := filepath.Join(companionDir, "subagents")
		if _, err := os.Stat(subagentsDir); err == nil {
			hasSubagents = true
			destSubagents := filepath.Join(tmplDir, "subagents")
			if err := copyDir(subagentsDir, destSubagents); err != nil {
				os.RemoveAll(tmplDir)
				return fmt.Errorf("copy subagents: %w", err)
			}
		}
	}

	// Write metadata
	meta := &Meta{
		Name:            opts.Name,
		Description:     opts.Description,
		SourceProject:   opts.Project,
		ProjectDir:      opts.ProjectDir,
		SourceSessionID: opts.SessionID,
		CreatedAt:       time.Now(),
		EntryCount:      entryCount,
		SizeBytes:       sizeBytes,
		Trimmed:         opts.Trim,
		HasSubagents:    hasSubagents,
	}

	if err := s.writeMeta(opts.ProjectDir, opts.Name, meta); err != nil {
		os.RemoveAll(tmplDir)
		return err
	}

	return nil
}

func (s *Store) copySession(src, dst string, trim bool) (entryCount int, sizeBytes int64, err error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, 0, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, 0, err
	}
	defer out.Close()

	bw := bufio.NewWriter(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if trim {
			entryType := extractType(line)
			if trimTypes[entryType] {
				continue
			}
		}

		if _, err := bw.WriteString(line + "\n"); err != nil {
			return entryCount, 0, err
		}
		entryCount++
	}

	if err := bw.Flush(); err != nil {
		return entryCount, 0, err
	}

	if err := scanner.Err(); err != nil {
		return entryCount, 0, err
	}

	info, err := os.Stat(dst)
	if err != nil {
		return entryCount, 0, err
	}

	return entryCount, info.Size(), nil
}

func extractType(line string) string {
	var entry struct {
		Type string `json:"type"`
	}
	json.Unmarshal([]byte(line), &entry)
	return entry.Type
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

