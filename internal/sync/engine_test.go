package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncFile_NewFileCopies(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.jsonl")
	dstPath := filepath.Join(dstDir, "test.jsonl")

	content := []byte("line1\nline2\nline3\n")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	e := &Engine{}
	synced, bytes, err := e.syncFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("syncFile error: %v", err)
	}

	if !synced {
		t.Error("expected file to be synced (new file)")
	}

	if bytes != int64(len(content)) {
		t.Errorf("expected %d bytes written, got %d", len(content), bytes)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(dstContent) != string(content) {
		t.Errorf("dest content mismatch: got %q", string(dstContent))
	}
}

func TestSyncFile_LargerSourceOverwrites(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.jsonl")
	dstPath := filepath.Join(dstDir, "test.jsonl")

	smallContent := []byte("small")
	largeContent := []byte("this is much larger content that grew over time")

	os.WriteFile(dstPath, smallContent, 0644)
	os.WriteFile(srcPath, largeContent, 0644)

	e := &Engine{}
	synced, bytes, err := e.syncFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("syncFile error: %v", err)
	}

	if !synced {
		t.Error("expected file to be synced (source is larger)")
	}

	if bytes != int64(len(largeContent)) {
		t.Errorf("expected %d bytes, got %d", len(largeContent), bytes)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(dstContent) != string(largeContent) {
		t.Errorf("dest should have large content, got %q", string(dstContent))
	}
}

func TestSyncFile_SameSizeSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.jsonl")
	dstPath := filepath.Join(dstDir, "test.jsonl")

	content := []byte("same size content")
	os.WriteFile(srcPath, content, 0644)
	os.WriteFile(dstPath, content, 0644)

	e := &Engine{}
	synced, _, err := e.syncFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("syncFile error: %v", err)
	}

	if synced {
		t.Error("expected file to be skipped (same size)")
	}
}

func TestSyncFile_SmallerSourceSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.jsonl")
	dstPath := filepath.Join(dstDir, "test.jsonl")

	smallContent := []byte("small")
	largeContent := []byte("this is larger content already in backup")

	os.WriteFile(srcPath, smallContent, 0644)
	os.WriteFile(dstPath, largeContent, 0644)

	e := &Engine{}
	synced, _, err := e.syncFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("syncFile error: %v", err)
	}

	if synced {
		t.Error("expected file to be skipped (dest is larger, append-only guarantee)")
	}

	// Verify dest was not modified
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(dstContent) != string(largeContent) {
		t.Error("dest content was incorrectly modified")
	}
}

func TestSync_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")

	// Set up source structure: history.jsonl + projects/proj1/session.jsonl
	os.MkdirAll(filepath.Join(claudeDir, "projects", "proj1"), 0755)
	os.WriteFile(filepath.Join(claudeDir, "history.jsonl"), []byte(`{"id":"1"}`+"\n"), 0644)
	os.WriteFile(filepath.Join(claudeDir, "projects", "proj1", "session.jsonl"), []byte(`{"ts":"now"}`+"\n"), 0644)

	e := NewEngine(claudeDir, backupDir)
	result, err := e.Sync()
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	// history.jsonl is updated (existed conceptually, counted as updated)
	// session.jsonl is new
	if result.NewFiles+result.UpdatedFiles < 2 {
		t.Errorf("expected at least 2 files synced, got new=%d updated=%d", result.NewFiles, result.UpdatedFiles)
	}

	// Verify history.jsonl was copied
	histContent, err := os.ReadFile(filepath.Join(backupDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("history.jsonl not copied: %v", err)
	}
	if string(histContent) != `{"id":"1"}`+"\n" {
		t.Errorf("history.jsonl content mismatch: %q", string(histContent))
	}

	// Verify project file was copied
	sessContent, err := os.ReadFile(filepath.Join(backupDir, "projects", "proj1", "session.jsonl"))
	if err != nil {
		t.Fatalf("session.jsonl not copied: %v", err)
	}
	if string(sessContent) != `{"ts":"now"}`+"\n" {
		t.Errorf("session.jsonl content mismatch: %q", string(sessContent))
	}
}

func TestSync_SkipsMemoryDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")

	// Create a file inside a memory/ directory
	memoryDir := filepath.Join(claudeDir, "projects", "proj1", "memory")
	os.MkdirAll(memoryDir, 0755)
	os.WriteFile(filepath.Join(memoryDir, "foo.md"), []byte("memory content"), 0644)

	// Also create a normal file to confirm sync works at all
	os.MkdirAll(filepath.Join(claudeDir, "projects", "proj1"), 0755)
	os.WriteFile(filepath.Join(claudeDir, "projects", "proj1", "session.jsonl"), []byte("session data"), 0644)

	e := NewEngine(claudeDir, backupDir)
	_, err := e.Sync()
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	// The memory file should NOT exist in backup
	memoryDst := filepath.Join(backupDir, "projects", "proj1", "memory", "foo.md")
	if _, err := os.Stat(memoryDst); err == nil {
		t.Error("memory/foo.md should NOT be copied to backup")
	}

	// The normal file should exist
	sessDst := filepath.Join(backupDir, "projects", "proj1", "session.jsonl")
	if _, err := os.Stat(sessDst); err != nil {
		t.Error("session.jsonl should be copied to backup")
	}
}

func TestSync_LockfilePreventsConccurentSync(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")

	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)
	os.MkdirAll(backupDir, 0755)

	e := NewEngine(claudeDir, backupDir)

	// Manually create the lock file at the engine's expected path
	lockPath := e.lockPath()
	os.MkdirAll(filepath.Dir(lockPath), 0755)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to create lock file: %v", err)
	}
	f.Close()
	defer os.Remove(lockPath)

	// Try to sync — should fail due to lock
	_, err = e.Sync()
	if err == nil {
		t.Fatal("expected error due to lockfile, got nil")
	}
	if !strings.Contains(err.Error(), "sync already in progress") {
		t.Errorf("expected 'sync already in progress' error, got: %v", err)
	}

	// Release lock manually
	os.Remove(lockPath)

	// Now sync should work
	_, err = e.Sync()
	if err != nil {
		t.Fatalf("Sync() after lock release should succeed, got: %v", err)
	}
}

func TestSync_TemplatesInBackupSurviveSync(t *testing.T) {
	// Templates live directly in backup/templates/. Verify sync doesn't disturb them.
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")

	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)
	os.MkdirAll(filepath.Join(backupDir, "templates"), 0755)

	// Pre-place a template in backup/templates/
	os.WriteFile(filepath.Join(backupDir, "templates", "meta.json"), []byte(`{"name":"warm"}`), 0644)

	e := NewEngine(claudeDir, backupDir)
	_, err := e.Sync()
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	// Template file should still be there untouched
	content, err := os.ReadFile(filepath.Join(backupDir, "templates", "meta.json"))
	if err != nil {
		t.Fatalf("template file disappeared after sync: %v", err)
	}
	if string(content) != `{"name":"warm"}` {
		t.Errorf("template content changed: %q", string(content))
	}
}
