package template

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSessionID = "test-uuid-1234-5678-abcd-ef0123456789"
const testProjectDir = "test-project"

func sampleJSONL() string {
	lines := []string{
		`{"type":"permission-mode","permissionMode":"default","sessionId":"` + testSessionID + `"}`,
		`{"type":"user","sessionId":"` + testSessionID + `","message":{"content":[{"type":"text","text":"hello"}]},"timestamp":1700000000000}`,
		`{"type":"assistant","sessionId":"` + testSessionID + `","message":{"content":[{"type":"text","text":"hi"}]},"timestamp":1700000001000}`,
		`{"type":"last-prompt","sessionId":"` + testSessionID + `"}`,
		`{"type":"custom-title","sessionId":"` + testSessionID + `"}`,
		`{"type":"agent-name","sessionId":"` + testSessionID + `"}`,
	}
	return strings.Join(lines, "\n") + "\n"
}

func setupTestDirs(t *testing.T) (templatesDir, claudeDir string) {
	t.Helper()
	templatesDir = t.TempDir()
	claudeDir = t.TempDir()

	sessionDir := filepath.Join(claudeDir, "projects", testProjectDir)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("failed to create session dir: %v", err)
	}

	sessionFile := filepath.Join(sessionDir, testSessionID+".jsonl")
	if err := os.WriteFile(sessionFile, []byte(sampleJSONL()), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	return templatesDir, claudeDir
}

func TestSave_Success(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:   testSessionID,
		ProjectDir:  testProjectDir,
		Project:     "my-project",
		Name:        "test-template",
		Description: "a test template",
		Trim:        false,
		Force:       false,
	}

	if err := store.Save(opts); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	// Verify meta.json exists and is valid
	metaPath := filepath.Join(templatesDir, testProjectDir, "test-template", "meta.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("meta.json not found: %v", err)
	}

	var meta Meta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("meta.json is not valid JSON: %v", err)
	}

	if meta.Name != "test-template" {
		t.Errorf("expected name 'test-template', got %q", meta.Name)
	}
	if meta.SourceSessionID != testSessionID {
		t.Errorf("expected source session ID %q, got %q", testSessionID, meta.SourceSessionID)
	}
	if meta.SourceProject != "my-project" {
		t.Errorf("expected source project 'my-project', got %q", meta.SourceProject)
	}
	if meta.EntryCount != 6 {
		t.Errorf("expected 6 entries (no trim), got %d", meta.EntryCount)
	}

	// Verify session.jsonl exists
	sessionPath := filepath.Join(templatesDir, testProjectDir, "test-template", "session.jsonl")
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		t.Fatal("session.jsonl was not created")
	}

	sessionData, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("failed to read session.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(sessionData)), "\n")
	if len(lines) != 6 {
		t.Errorf("expected 6 lines in session.jsonl, got %d", len(lines))
	}
}

func TestSave_WithTrim(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:   testSessionID,
		ProjectDir:  testProjectDir,
		Project:     "my-project",
		Name:        "trimmed-template",
		Description: "trimmed",
		Trim:        true,
		Force:       false,
	}

	if err := store.Save(opts); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	sessionPath := filepath.Join(templatesDir, testProjectDir, "trimmed-template", "session.jsonl")
	sessionData, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("failed to read session.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(sessionData)), "\n")

	// Original has 6 lines; trim removes last-prompt, custom-title, agent-name = 3 trimmed
	// Remaining: permission-mode, user, assistant = 3
	if len(lines) != 3 {
		t.Errorf("expected 3 lines after trim, got %d", len(lines))
	}

	// Verify trimmed types are gone
	for _, line := range lines {
		entryType := extractType(line)
		if trimTypes[entryType] {
			t.Errorf("trimmed type %q still present in output", entryType)
		}
	}

	// Verify meta reflects trimmed count
	meta, err := store.ReadMeta(testProjectDir, "trimmed-template")
	if err != nil {
		t.Fatalf("failed to read meta: %v", err)
	}
	if meta.EntryCount != 3 {
		t.Errorf("expected meta entry count 3, got %d", meta.EntryCount)
	}
	if !meta.Trimmed {
		t.Error("expected meta.Trimmed to be true")
	}
}

func TestSave_SessionFileNotFound(t *testing.T) {
	templatesDir := t.TempDir()
	claudeDir := t.TempDir()
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:  "nonexistent-uuid",
		ProjectDir: testProjectDir,
		Project:    "my-project",
		Name:       "fail-template",
		Trim:       false,
		Force:      false,
	}

	err := store.Save(opts)
	if err == nil {
		t.Fatal("expected error for missing session file, got nil")
	}
	if !strings.Contains(err.Error(), "session file not found") {
		t.Errorf("expected 'session file not found' error, got: %v", err)
	}
}

func TestSave_InvalidName_TooShort(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:  testSessionID,
		ProjectDir: testProjectDir,
		Name:       "x",
		Trim:       false,
		Force:      false,
	}

	err := store.Save(opts)
	if err == nil {
		t.Fatal("expected error for short name, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2 characters") {
		t.Errorf("expected name length error, got: %v", err)
	}
}

func TestSave_InvalidName_InvalidChars(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:  testSessionID,
		ProjectDir: testProjectDir,
		Name:       "Bad_Name!",
		Trim:       false,
		Force:      false,
	}

	err := store.Save(opts)
	if err == nil {
		t.Fatal("expected error for invalid name, got nil")
	}
	if !strings.Contains(err.Error(), "lowercase alphanumeric") {
		t.Errorf("expected naming convention error, got: %v", err)
	}
}

func TestSave_ForceOverwrite(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:   testSessionID,
		ProjectDir:  testProjectDir,
		Project:     "my-project",
		Name:        "overwrite-me",
		Description: "first save",
		Trim:        false,
		Force:       false,
	}

	if err := store.Save(opts); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}

	// Second save without force should fail
	opts.Description = "second save"
	err := store.Save(opts)
	if err == nil {
		t.Fatal("expected error on duplicate name without --force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}

	// With force, should succeed
	opts.Force = true
	opts.Description = "overwritten"
	if err := store.Save(opts); err != nil {
		t.Fatalf("Save with --force failed: %v", err)
	}

	meta, err := store.ReadMeta(testProjectDir, "overwrite-me")
	if err != nil {
		t.Fatalf("failed to read meta after overwrite: %v", err)
	}
	if meta.Description != "overwritten" {
		t.Errorf("expected description 'overwritten', got %q", meta.Description)
	}
}

func TestSave_WithoutForce_ErrorsOnExisting(t *testing.T) {
	templatesDir, claudeDir := setupTestDirs(t)
	store := NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:  testSessionID,
		ProjectDir: testProjectDir,
		Project:    "my-project",
		Name:       "no-force",
		Trim:       false,
		Force:      false,
	}

	if err := store.Save(opts); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}

	err := store.Save(opts)
	if err == nil {
		t.Fatal("expected error when saving without --force to existing template")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected error to mention --force, got: %v", err)
	}
}
