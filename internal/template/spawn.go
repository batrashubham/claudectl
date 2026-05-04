package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type SpawnResult struct {
	SessionID  string
	ProjectDir string
	Project    string
}

func (s *Store) Spawn(projectDir, name string) (*SpawnResult, error) {
	meta, err := s.ReadMeta(projectDir, name)
	if err != nil {
		return nil, fmt.Errorf("template '%s' not found: %w", name, err)
	}

	newID := uuid.New().String()
	oldID := meta.SourceSessionID

	// Destination in Claude's projects directory
	destDir := filepath.Join(s.claudeDir, "projects", projectDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	destFile := filepath.Join(destDir, newID+".jsonl")

	// Stream-rewrite session JSONL with new session ID
	srcFile := s.sessionPath(projectDir, name)
	src, err := os.Open(srcFile)
	if err != nil {
		return nil, fmt.Errorf("open template: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destFile)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer dst.Close()

	if _, err := RewriteSessionID(src, dst, oldID, newID); err != nil {
		os.Remove(destFile)
		return nil, fmt.Errorf("rewrite session: %w", err)
	}

	// Copy and rewrite subagents if they exist
	subagentsSrc := filepath.Join(s.templateDir(projectDir, name), "subagents")
	if _, err := os.Stat(subagentsSrc); err == nil {
		subagentsDst := filepath.Join(destDir, newID, "subagents")
		if err := os.MkdirAll(subagentsDst, 0755); err == nil {
			filepath.Walk(subagentsSrc, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(subagentsSrc, path)
				target := filepath.Join(subagentsDst, rel)
				os.MkdirAll(filepath.Dir(target), 0755)

				if strings.HasSuffix(path, ".jsonl") {
					// Rewrite session IDs in subagent JSONL
					in, err := os.Open(path)
					if err != nil {
						return nil
					}
					defer in.Close()
					out, err := os.Create(target)
					if err != nil {
						return nil
					}
					defer out.Close()
					RewriteSessionID(in, out, oldID, newID)
				} else {
					copyFile(path, target)
				}
				return nil
			})
		}
	}

	return &SpawnResult{
		SessionID:  newID,
		ProjectDir: projectDir,
		Project:    meta.SourceProject,
	}, nil
}
