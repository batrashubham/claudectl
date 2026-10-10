package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

	builder := NewBuilder(claudeDir, backupDir, "this")
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

	builder := NewBuilder(claudeDir, backupDir, "this")
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

	builder := NewBuilder(claudeDir, backupDir, "this")
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

	builder := NewBuilder(claudeDir, backupDir, "this")
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

	builder := NewBuilder(claudeDir, backupDir, "this")
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

// Claude Code replaces every non-alphanumeric character, not just "/".
// Verified empirically: "/…/T/cc.enc_test v2" → "-…-T-cc-enc-test-v2".
func TestProjectDir_MatchesClaudeEncoding(t *testing.T) {
	cases := map[string]string{
		"/Users/me/code/app":        "-Users-me-code-app",
		"/Users/me/code/my.app":     "-Users-me-code-my-app",
		"/Users/me/code/my_app":     "-Users-me-code-my-app",
		"/Users/me/My Projects/app": "-Users-me-My-Projects-app",
		"/tmp/cc.enc_test v2":       "-tmp-cc-enc-test-v2",
	}
	for in, want := range cases {
		if got := ProjectDir(in); got != want {
			t.Errorf("ProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuild_HistoryLineOverOneMBDoesNotEndScan(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "claude")
	os.MkdirAll(claudeDir, 0755)

	big := make([]byte, 3<<20)
	for i := range big {
		big[i] = 'x'
	}
	writeHistoryJSONL(t, filepath.Join(claudeDir, "history.jsonl"), []HistoryEntry{
		{Display: string(big), Timestamp: 1000, Project: "/proj/a", SessionID: "session-big"},
		{Display: "after", Timestamp: 2000, Project: "/proj/a", SessionID: "session-after"},
	})

	sessions, err := NewBuilder(claudeDir, filepath.Join(tmpDir, "backup"), "this").Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func byID(sessions []SessionMeta) map[string]SessionMeta {
	m := make(map[string]SessionMeta)
	for _, s := range sessions {
		m[s.ID] = s
	}
	return m
}

func TestBuild_LabelsSessionsByMachine(t *testing.T) {
	tmp := t.TempDir()
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	writeFile(t, filepath.Join(claudeDir, "projects", "-p", "live.jsonl"), "{}\n")
	writeFile(t, filepath.Join(backupDir, "machines", "work", "projects", "-p", "mine.jsonl"), "{}\n")
	writeFile(t, filepath.Join(backupDir, "projects", "-p", "legacy.jsonl"), "{}\n")
	writeFile(t, filepath.Join(backupDir, "machines", "home", "projects", "-p", "theirs.jsonl"), "{}\n")
	writeFile(t, filepath.Join(backupDir, "machines", "home", "projects", "-p", "both.jsonl"), "{}\n")
	writeFile(t, filepath.Join(backupDir, "machines", "work", "projects", "-p", "both.jsonl"), "{}\n")
	writeHistoryJSONL(t, filepath.Join(backupDir, "machines", "home", "history.jsonl"), []HistoryEntry{
		{Display: "only on home", Timestamp: 1000, Project: "/p", SessionID: "ghost"},
	})

	sessions, err := NewBuilder(claudeDir, backupDir, "work").Build()
	if err != nil {
		t.Fatal(err)
	}
	got := byID(sessions)
	want := map[string]string{"live": "work", "mine": "work", "legacy": "work", "theirs": "home", "both": "work", "ghost": "home"}
	for id, m := range want {
		if got[id].Machine != m {
			t.Errorf("%s: machine = %q, want %q", id, got[id].Machine, m)
		}
	}
	if got["live"].Status != StatusActive || got["theirs"].Status != StatusArchived {
		t.Error("wrong status")
	}
}

func TestBuild_ProjectFromTranscriptCwdWhenNoHistory(t *testing.T) {
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, "claude")
	writeFile(t, filepath.Join(claudeDir, "projects", "-code-my-app", "s.jsonl"),
		`{"type":"permission-mode"}`+"\n"+`{"type":"user","cwd":"/code/my-app"}`+"\n")

	sessions, _ := NewBuilder(claudeDir, filepath.Join(tmp, "backup"), "work").Build()
	if sessions[0].Project != "/code/my-app" {
		t.Errorf("project = %q, want /code/my-app (folder decoding gives /code/my/app)", sessions[0].Project)
	}
}

func TestMachines(t *testing.T) {
	backupDir := t.TempDir()
	for _, m := range []string{"work", "home"} {
		os.MkdirAll(filepath.Join(backupDir, "machines", m), 0755)
	}
	os.MkdirAll(filepath.Join(backupDir, "machines", ".tmp"), 0755)
	got := Machines(backupDir)
	if len(got) != 2 || got[0] != "home" || got[1] != "work" {
		t.Errorf("got %v", got)
	}
}

func TestShortID(t *testing.T) {
	if ShortID("abc", 8) != "abc" || ShortID("0123456789", 4) != "0123" {
		t.Error("ShortID")
	}
}
