package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/batrashubham/claudectl/internal/harness"
)

// newClaudeBuilder indexes a live Claude dir plus a backup in the legacy
// (pre-machines) layout, which uses the same structure.
func newClaudeBuilder(t *testing.T, claudeDir, backupDir string) *Builder {
	t.Helper()
	h, err := harness.New("claude", claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	return NewBuilder("local",
		Source{Harness: h, Machine: "local", Root: claudeDir, Live: true},
		Source{Harness: h, Machine: "local", Root: backupDir},
	)
}

func writeHistoryJSONL(t *testing.T, path string, entries []HistoryEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create history file: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("failed to write entry: %v", err)
		}
	}
}

func TestBuild_CorrectSessionCount(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(backupDir, 0755)

	entries := []HistoryEntry{
		{Display: "hello", Timestamp: 1000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "world", Timestamp: 2000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "foo", Timestamp: 3000, Project: "/proj/b", SessionID: "session-bbb"},
	}
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), entries)

	builder := newClaudeBuilder(t, claudeDir, backupDir)
	sessions, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestBuild_FirstPromptLastPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(backupDir, 0755)

	entries := []HistoryEntry{
		{Display: "first prompt", Timestamp: 1000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "middle prompt", Timestamp: 2000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "last prompt", Timestamp: 3000, Project: "/proj/a", SessionID: "session-aaa"},
	}
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), entries)

	builder := newClaudeBuilder(t, claudeDir, backupDir)
	sessions, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.FirstPrompt != "first prompt" {
		t.Errorf("expected FirstPrompt=%q, got %q", "first prompt", s.FirstPrompt)
	}
	if s.LastPrompt != "last prompt" {
		t.Errorf("expected LastPrompt=%q, got %q", "last prompt", s.LastPrompt)
	}
}

func TestBuild_PromptCount(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(backupDir, 0755)

	entries := []HistoryEntry{
		{Display: "one", Timestamp: 1000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "two", Timestamp: 2000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "three", Timestamp: 3000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "four", Timestamp: 4000, Project: "/proj/a", SessionID: "session-aaa"},
	}
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), entries)

	builder := newClaudeBuilder(t, claudeDir, backupDir)
	sessions, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	if sessions[0].PromptCount != 4 {
		t.Errorf("expected PromptCount=4, got %d", sessions[0].PromptCount)
	}
}

func TestBuild_CommandsExcludedFromPrompts(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(backupDir, 0755)

	entries := []HistoryEntry{
		{Display: "real prompt", Timestamp: 1000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "/help", Timestamp: 2000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "/clear", Timestamp: 3000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "another prompt", Timestamp: 4000, Project: "/proj/a", SessionID: "session-aaa"},
	}
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), entries)

	builder := newClaudeBuilder(t, claudeDir, backupDir)
	sessions, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	// Commands still increment PromptCount (the code increments unconditionally)
	// but they don't set FirstPrompt/LastPrompt
	if s.FirstPrompt != "real prompt" {
		t.Errorf("expected FirstPrompt=%q, got %q", "real prompt", s.FirstPrompt)
	}
	if s.LastPrompt != "another prompt" {
		t.Errorf("expected LastPrompt=%q, got %q", "another prompt", s.LastPrompt)
	}
}

func TestBuild_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	backupDir := filepath.Join(tmpDir, "backup")
	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(backupDir, 0755)

	entries := []HistoryEntry{
		{Display: "hello", Timestamp: 1000, Project: "/proj/a", SessionID: "session-aaa"},
		{Display: "world", Timestamp: 2000, Project: "/proj/a", SessionID: "session-aaa"},
	}

	// Write the same entries to both live and backup history
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), entries)
	writeHistoryJSONL(t, filepath.Join(backupDir, "history.jsonl"), entries)

	builder := newClaudeBuilder(t, claudeDir, backupDir)
	sessions, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// Without dedup we'd get PromptCount=4, with dedup it should be 2
	if sessions[0].PromptCount != 2 {
		t.Errorf("expected PromptCount=2 (deduplicated), got %d", sessions[0].PromptCount)
	}
}

func TestBuild_MergesMachinesAndPrefersLive(t *testing.T) {
	tmp := t.TempDir()
	live := filepath.Join(tmp, "live")
	laptop := filepath.Join(tmp, "backup", "machines", "laptop", "claude")
	desktop := filepath.Join(tmp, "backup", "machines", "desktop", "claude")
	write := func(root, dir, id, content string) {
		os.MkdirAll(filepath.Join(root, "projects", dir), 0755)
		os.WriteFile(filepath.Join(root, "projects", dir, id+".jsonl"), []byte(content), 0644)
	}
	write(live, "-p", "shared", `{"type":"user","cwd":"/p","message":{"role":"user","content":"hi"}}`+"\n")
	write(laptop, "-p", "shared", `{"type":"user","cwd":"/p","message":{"role":"user","content":"hi"}}`+"\n")
	write(desktop, "-q", "remote-only", `{"type":"user","cwd":"/q","message":{"role":"user","content":"from desktop"}}`+"\n")

	h, _ := harness.New("claude", live)
	b := NewBuilder("laptop",
		Source{Harness: h, Machine: "laptop", Root: live, Live: true},
		Source{Harness: h, Machine: "laptop", Root: laptop},
		Source{Harness: h, Machine: "desktop", Root: desktop},
	)
	sessions, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	shared, _ := Find(sessions, "shared")
	if shared.Status != StatusActive || shared.Machine != "laptop" || len(shared.Locations) != 2 || shared.PromptCount != 1 {
		t.Errorf("shared = %+v", shared)
	}
	remote, _ := Find(sessions, "remote")
	if remote.Status != StatusArchived || remote.Machine != "desktop" || remote.Project != "/q" {
		t.Errorf("remote = %+v", remote)
	}
	if loc, ok := remote.Best("laptop"); !ok || loc.Root != desktop {
		t.Errorf("Best = %+v", loc)
	}
}

func TestFind_PrefixAndHarnessQualifier(t *testing.T) {
	sessions := []SessionMeta{
		{ID: "abc111", Harness: "claude"},
		{ID: "abc222", Harness: "codex"},
	}
	if _, err := Find(sessions, "abc"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("want ambiguity error, got %v", err)
	}
	if s, err := Find(sessions, "codex:abc"); err != nil || s.ID != "abc222" {
		t.Errorf("qualified lookup = %v, %v", s, err)
	}
	if s, err := Find(sessions, "abc111"); err != nil || s.Harness != "claude" {
		t.Errorf("exact lookup = %v, %v", s, err)
	}
	if _, err := Find(sessions, "zzz"); err == nil {
		t.Error("missing session should error")
	}
}
