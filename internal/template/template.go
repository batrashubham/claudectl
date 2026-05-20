package template

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Meta struct {
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	RewarmPrompt    string    `json:"rewarmPrompt,omitempty"`
	SourceProject   string    `json:"sourceProject"`
	ProjectDir      string    `json:"projectDir"`
	SourceSessionID string    `json:"sourceSessionId"`
	CreatedAt       time.Time `json:"createdAt"`
	EntryCount      int       `json:"entryCount"`
	SizeBytes       int64     `json:"sizeBytes"`
	Trimmed         bool      `json:"trimmed"`
	HasSubagents    bool      `json:"hasSubagents"`
}

const DefaultRewarmPrompt = "The codebase has evolved since your last exploration. Please re-read the project structure, check for new/changed files, and update your understanding of the architecture, patterns, and key decisions. Focus on what has changed rather than re-reading everything."

type Store struct {
	baseDir   string
	claudeDir string
}

func NewStore(templatesDir, claudeDir string) *Store {
	return &Store{baseDir: templatesDir, claudeDir: claudeDir}
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

func ValidateName(name string) error {
	if len(name) < 2 {
		return fmt.Errorf("template name must be at least 2 characters")
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("template name must be lowercase alphanumeric with hyphens (e.g., 'warm-context')")
	}
	return nil
}

func (s *Store) templateDir(projectDir, name string) string {
	return filepath.Join(s.baseDir, projectDir, name)
}

func (s *Store) metaPath(projectDir, name string) string {
	return filepath.Join(s.templateDir(projectDir, name), "meta.json")
}

func (s *Store) sessionPath(projectDir, name string) string {
	return filepath.Join(s.templateDir(projectDir, name), "session.jsonl")
}

func (s *Store) ReadMeta(projectDir, name string) (*Meta, error) {
	data, err := os.ReadFile(s.metaPath(projectDir, name))
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) writeMeta(projectDir, name string, meta *Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(projectDir, name), data, 0644)
}

func (s *Store) Exists(projectDir, name string) bool {
	_, err := os.Stat(s.metaPath(projectDir, name))
	return err == nil
}
