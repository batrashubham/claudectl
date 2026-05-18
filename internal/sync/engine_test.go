package sync

import (
	"os"
	"path/filepath"
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
