package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupSpawnTemplate(t *testing.T) (store *Store, templatesDir, claudeDir string) {
	t.Helper()
	templatesDir, claudeDir = setupTestDirs(t)
	store = NewStore(templatesDir, claudeDir)

	opts := SaveOptions{
		SessionID:   testSessionID,
		ProjectDir:  testProjectDir,
		Project:     "spawn-project",
		Name:        "spawn-source",
		Description: "template to spawn from",
		Trim:        false,
		Force:       false,
	}

	if err := store.Save(opts); err != nil {
		t.Fatalf("failed to save template for spawn test: %v", err)
	}

	return store, templatesDir, claudeDir
}

func TestSpawn_CreatesNewSession(t *testing.T) {
	store, _, claudeDir := setupSpawnTemplate(t)

	result, err := store.Spawn(testProjectDir, "spawn-source")
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	if result.SessionID == "" {
		t.Fatal("Spawn returned empty session ID")
	}

	// Verify new session file exists
	newSessionFile := filepath.Join(claudeDir, "projects", testProjectDir, result.SessionID+".jsonl")
	if _, err := os.Stat(newSessionFile); os.IsNotExist(err) {
		t.Fatalf("spawned session file not found at %s", newSessionFile)
	}

	// Verify file is non-empty
	data, err := os.ReadFile(newSessionFile)
	if err != nil {
		t.Fatalf("failed to read spawned session: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("spawned session file is empty")
	}
}

func TestSpawn_NoOldSessionIDReferences(t *testing.T) {
	store, _, claudeDir := setupSpawnTemplate(t)

	result, err := store.Spawn(testProjectDir, "spawn-source")
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	newSessionFile := filepath.Join(claudeDir, "projects", testProjectDir, result.SessionID+".jsonl")
	data, err := os.ReadFile(newSessionFile)
	if err != nil {
		t.Fatalf("failed to read spawned session: %v", err)
	}

	content := string(data)

	// Must not contain the old session ID
	if strings.Contains(content, testSessionID) {
		t.Errorf("spawned session still contains old session ID %q", testSessionID)
	}

	// Must contain the new session ID
	if !strings.Contains(content, result.SessionID) {
		t.Errorf("spawned session does not contain new session ID %q", result.SessionID)
	}

	// Verify every line that had the old sessionId now has the new one
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for i, line := range lines {
		if strings.Contains(line, testSessionID) {
			t.Errorf("line %d still contains old session ID: %s", i, line)
		}
	}
}

func TestSpawn_CorrectDestinationPath(t *testing.T) {
	store, _, claudeDir := setupSpawnTemplate(t)

	result, err := store.Spawn(testProjectDir, "spawn-source")
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	expectedDir := filepath.Join(claudeDir, "projects", testProjectDir)
	expectedFile := filepath.Join(expectedDir, result.SessionID+".jsonl")

	info, err := os.Stat(expectedFile)
	if err != nil {
		t.Fatalf("expected file at %s does not exist: %v", expectedFile, err)
	}
	if info.IsDir() {
		t.Fatalf("expected a file at %s, but got a directory", expectedFile)
	}
	if info.Size() == 0 {
		t.Fatal("spawned session file has zero bytes")
	}
}

func TestSpawn_ReturnsValidSpawnResult(t *testing.T) {
	store, _, _ := setupSpawnTemplate(t)

	result, err := store.Spawn(testProjectDir, "spawn-source")
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	if result == nil {
		t.Fatal("Spawn returned nil result")
	}

	// SessionID should be a UUID (36 chars: 8-4-4-4-12)
	if len(result.SessionID) != 36 {
		t.Errorf("expected UUID (36 chars), got %q (%d chars)", result.SessionID, len(result.SessionID))
	}

	// SessionID should differ from the source
	if result.SessionID == testSessionID {
		t.Error("spawned session ID is the same as the source session ID")
	}

	// ProjectDir and Project should be set
	if result.ProjectDir != testProjectDir {
		t.Errorf("expected project dir %q, got %q", testProjectDir, result.ProjectDir)
	}
	if result.Project != "spawn-project" {
		t.Errorf("expected project 'spawn-project', got %q", result.Project)
	}
}

func TestSpawn_NonExistentTemplate(t *testing.T) {
	templatesDir := t.TempDir()
	claudeDir := t.TempDir()
	store := NewStore(templatesDir, claudeDir)

	result, err := store.Spawn(testProjectDir, "nonexistent-template")
	if err == nil {
		t.Fatal("expected error when spawning from non-existent template, got nil")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}
