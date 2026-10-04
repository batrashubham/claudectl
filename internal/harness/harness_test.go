package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustNew(t *testing.T, name, home string) Harness {
	t.Helper()
	h, err := New(name, home)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func byID(sessions []Session, id string) *Session {
	for i := range sessions {
		if sessions[i].ID == id {
			return &sessions[i]
		}
	}
	return nil
}

func promptTexts(s *Session) []string {
	var out []string
	for _, p := range s.Prompts {
		out = append(out, p.Text)
	}
	return out
}

func TestRegistry(t *testing.T) {
	names := Names()
	if len(names) < 5 || names[0] != "claude" {
		t.Errorf("Names() = %v, want claude first and all adapters registered", names)
	}
	if _, err := New("nope", ""); err == nil {
		t.Error("unknown harness should error")
	}
}

func TestDefaultHomes_RespectEnv(t *testing.T) {
	t.Setenv("CODEX_HOME", "/x/codex")
	t.Setenv("GEMINI_CLI_HOME", "/x/ghome")
	t.Setenv("QWEN_HOME", "/x/qwen")
	t.Setenv("CLAUDE_CONFIG_DIR", "/x/claude")
	for name, want := range map[string]string{
		"codex": "/x/codex", "gemini": "/x/ghome/.gemini", "qwen": "/x/qwen", "claude": "/x/claude",
	} {
		if got := DefaultHome(name); got != want {
			t.Errorf("DefaultHome(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestEncodeClaudeProject(t *testing.T) {
	if got := EncodeClaudeProject("/Users/a/my_proj.x"); got != "-Users-a-my-proj-x" {
		t.Errorf("got %q", got)
	}
}

func TestClaude_ScanAndMessages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "history.jsonl"),
		`{"display":"fix the bug","timestamp":1000,"project":"/code/app","sessionId":"s1"}`+"\n"+
			`{"display":"/clear","timestamp":2000,"project":"/code/app","sessionId":"s1"}`+"\n")
	writeFile(t, filepath.Join(root, "projects", "-code-app", "s1.jsonl"),
		`{"type":"user","cwd":"/code/app","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"fix the bug"}}`+"\n"+
			`{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Done."},{"type":"tool_use","name":"Edit"}]}}`+"\n"+
			`{"type":"user","timestamp":"2026-01-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`+"\n"+
			`{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x"}}`+"\n")
	// Not in history: project and opening prompt come from the transcript.
	writeFile(t, filepath.Join(root, "projects", "-code-my-lib", "s2.jsonl"),
		`{"type":"user","cwd":"/code/my_lib","message":{"role":"user","content":"<command-name>/init</command-name>"}}`+"\n"+
			`{"type":"user","cwd":"/code/my_lib","message":{"role":"user","content":"write tests"}}`+"\n")
	writeFile(t, filepath.Join(root, "projects", "-code-app", "agent-abc.jsonl"), "{}\n")
	os.MkdirAll(filepath.Join(root, "projects", "-code-app", "s1", "subagents"), 0755)

	h := mustNew(t, "claude", root)
	sessions, err := h.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions (agent-* skipped), got %d", len(sessions))
	}
	s1 := byID(sessions, "s1")
	if s1.Project != "/code/app" || len(s1.Prompts) != 2 || len(s1.Files) != 2 {
		t.Errorf("s1 = %+v", s1)
	}
	s2 := byID(sessions, "s2")
	if s2.Project != "/code/my_lib" || strings.Join(promptTexts(s2), "|") != "write tests" {
		t.Errorf("s2 should be filled from transcript, got project=%q prompts=%v", s2.Project, promptTexts(s2))
	}

	msgs, err := h.Messages(root, *s1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range msgs {
		got = append(got, m.Role+":"+m.Text)
	}
	if want := "user:fix the bug|assistant:Done.|tool:Edit"; strings.Join(got, "|") != want {
		t.Errorf("messages = %v, want %s", got, want)
	}

	copies := h.RestoreFiles(*s1, "/home/me/app")
	if copies[0].Dst != filepath.Join("projects", "-home-me-app", "s1.jsonl") || copies[1].Dst != filepath.Join("projects", "-home-me-app", "s1") {
		t.Errorf("remapped restore = %+v", copies)
	}
	if same := h.RestoreFiles(*s1, "/code/app"); same[0].Dst != same[0].Src {
		t.Errorf("same-path restore should keep location: %+v", same)
	}
}

func TestCodex_RealRollouts(t *testing.T) {
	h := mustNew(t, "codex", "testdata/codex")
	sessions, err := h.Scan("testdata/codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	live := byID(sessions, "01a10871-4967-75d1-afdd-89b0461b92a0")
	if live == nil {
		t.Fatal("live rollout not found")
	}
	if live.Project != "/work/demo" {
		t.Errorf("project = %q", live.Project)
	}
	if got := promptTexts(live); strings.Join(got, "|") != "second session" {
		t.Errorf("prompts = %q (environment context and developer messages must be skipped)", got)
	}

	archived := byID(sessions, "01a10870-cfe8-7a50-ba72-1aaae3d972f9")
	if archived == nil {
		t.Fatal("archived rollout not found")
	}
	if got := promptTexts(archived); len(got) < 2 || got[0] != "Say hello" {
		t.Errorf("archived prompts = %q", got)
	}
	copies := h.RestoreFiles(*archived, "")
	if !strings.HasPrefix(copies[0].Dst, filepath.Join("sessions", "2026", "10", "04")) {
		t.Errorf("archived rollout should restore under sessions/<date>, got %s", copies[0].Dst)
	}

	msgs, err := h.Messages("testdata/codex", *live)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Text != "second session" || msgs[1].Role != "assistant" {
		t.Errorf("messages = %+v", msgs)
	}
	if got := h.ResumeArgs(*live); strings.Join(got, " ") != "resume "+live.ID {
		t.Errorf("resume args = %v", got)
	}
}

func TestCodex_LegacyRolloutAndHistory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sessions", "2025", "05", "01", "rollout-2025-05-01T10-00-00-abc.jsonl"),
		`{"timestamp":"2025-05-01T10:00:00Z","type":"session_meta","payload":{"id":"abc","cwd":"/p"}}`+"\n"+
			`{"timestamp":"2025-05-01T10:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`+"\n"+
			`{"timestamp":"2025-05-01T10:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`+"\n")
	writeFile(t, filepath.Join(root, "sessions", "2025", "05", "02", "rollout-2025-05-02T10-00-00-def.jsonl"),
		`{"timestamp":"2025-05-02T10:00:00Z","type":"session_meta","payload":{"id":"def","cwd":"/q"}}`+"\n")
	writeFile(t, filepath.Join(root, "history.jsonl"), `{"session_id":"def","ts":1746180000,"text":"from history"}`+"\n")

	sessions, _ := mustNew(t, "codex", root).Scan(root)
	if got := promptTexts(byID(sessions, "abc")); len(got) != 1 {
		t.Errorf("prompts recorded as both event and item must count once, got %q", got)
	}
	if got := promptTexts(byID(sessions, "def")); strings.Join(got, "") != "from history" {
		t.Errorf("history.jsonl prompts should be used, got %q", got)
	}
}

func TestGemini_RealChat(t *testing.T) {
	h := mustNew(t, "gemini", "testdata/gemini")
	sessions, err := h.Scan("testdata/gemini")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != "0742fed7-4e00-4f99-9945-8bc22e7ef3d0" || s.Project != "/work/gdemo" {
		t.Errorf("session = %+v", s)
	}
	if got := promptTexts(&s); len(got) == 0 || got[0] != "Say hello" {
		t.Errorf("prompts = %q (session_context must be skipped)", got)
	}
	if s.Title != "Hello from the mock model." {
		t.Errorf("title from $set.summary = %q", s.Title)
	}
	msgs, err := h.Messages("testdata/gemini", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Errorf("messages = %+v", msgs)
	}
	if got := h.ResumeArgs(s); strings.Join(got, " ") != "--resume "+s.ID {
		t.Errorf("resume args = %v", got)
	}
}

func TestGemini_ReplayRewindAndLegacy(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tmp", "proj", ".project_root"), "/code/proj")
	writeFile(t, filepath.Join(root, "tmp", "proj", "chats", "session-2026-01-01T00-00-aaaa1111.jsonl"),
		`{"sessionId":"aaaa1111-x","projectHash":"h","startTime":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T00:00:00Z","kind":"main"}`+"\n"+
			`{"id":"1","timestamp":"2026-01-01T00:00:01Z","type":"user","content":[{"text":"first"}]}`+"\n"+
			`{"id":"2","timestamp":"2026-01-01T00:00:02Z","type":"gemini","content":"a"}`+"\n"+
			`{"id":"3","timestamp":"2026-01-01T00:00:03Z","type":"user","content":[{"text":"oops"}]}`+"\n"+
			`{"$rewindTo":"3"}`+"\n"+
			`{"id":"2","timestamp":"2026-01-01T00:00:02Z","type":"gemini","content":"a, updated"}`+"\n"+
			`{"id":"4","timestamp":"2026-01-01T00:00:04Z","type":"user","content":[{"text":"second"}],"displayContent":[{"text":"second (typed)"}]}`+"\n")
	// Pre-0.36 hash-keyed dir with a pre-0.39 whole-document chat.
	hash := GeminiProjectHash("/code/old")
	writeFile(t, filepath.Join(root, "tmp", hash, "chats", "session-2025-01-01T00-00-bbbb2222.json"),
		`{"sessionId":"bbbb2222-y","projectHash":"`+hash+`","startTime":"2025-01-01T00:00:00Z","lastUpdated":"2025-01-01T00:00:00Z","messages":[{"id":"1","timestamp":"2025-01-01T00:00:00Z","type":"user","content":"legacy prompt"}]}`)

	h := mustNew(t, "gemini", root)
	sessions, err := h.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	a := byID(sessions, "aaaa1111-x")
	if got := strings.Join(promptTexts(a), "|"); got != "first|second (typed)" {
		t.Errorf("replayed prompts = %q", got)
	}
	msgs, _ := h.Messages(root, *a)
	if msgs[1].Text != "a, updated" {
		t.Errorf("repeated id should replace earlier line, got %+v", msgs)
	}

	b := byID(sessions, "bbbb2222-y")
	if b == nil || b.Project != "" || b.ProjectKey != hash {
		t.Fatalf("legacy session = %+v", b)
	}
	if got := ResolveProjectHash("gemini", b.ProjectKey, []string{"/elsewhere", "/code/old"}); got != "/code/old" {
		t.Errorf("hash resolution = %q", got)
	}
}

func TestGemini_RestoreTargetsLocalProjectSlug(t *testing.T) {
	home := t.TempDir()
	h := mustNew(t, "gemini", home).(*Gemini)
	// A different project already owns the natural slug.
	writeFile(t, filepath.Join(home, "tmp", "app", ".project_root"), "/other/app")

	s := Session{ID: "x", Project: "/Users/me/app", Files: []string{"tmp/app/chats/session-1-x.jsonl"}}
	if err := h.PrepareRestore("/home/me/app"); err != nil {
		t.Fatal(err)
	}
	copies := h.RestoreFiles(s, "/home/me/app")
	if want := filepath.Join("tmp", "app-1", "chats", "session-1-x.jsonl"); copies[0].Dst != want {
		t.Errorf("dst = %s, want %s", copies[0].Dst, want)
	}
	if root := geminiProjectRoot(filepath.Join(home, "tmp", "app-1")); root != "/home/me/app" {
		t.Errorf(".project_root = %q", root)
	}
}

func TestGemini_SyncSkip(t *testing.T) {
	for rel, skip := range map[string]bool{
		"proj/":                     false,
		"proj/chats/":               false,
		"proj/chats/session-1.json": false,
		"proj/chats/abc/sub.jsonl":  false,
		"proj/.project_root":        false,
		"proj/logs.json":            false,
		"proj/shell_history":        true,
		"proj/checkpoints/":         true,
		"stray.txt":                 true,
	} {
		if got := geminiSkip(rel); got != skip {
			t.Errorf("geminiSkip(%q) = %v, want %v", rel, got, skip)
		}
	}
}

func TestQwen_Scan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "-code-q", "chats")
	writeFile(t, filepath.Join(dir, "q1.jsonl"),
		`{"sessionId":"q1","timestamp":"2026-01-01T00:00:00Z","type":"user","cwd":"/code/q","message":{"role":"user","parts":[{"text":"hello qwen"}]}}`+"\n"+
			`{"sessionId":"q1","timestamp":"2026-01-01T00:00:01Z","type":"assistant","message":{"role":"model","parts":[{"text":"thinking","thought":true},{"text":"hi"},{"functionCall":{"name":"read_file"}}]}}`+"\n")
	writeFile(t, filepath.Join(dir, "q1.ledger.jsonl"), "{}\n")
	writeFile(t, filepath.Join(dir, "archive", "q0.jsonl"),
		`{"sessionId":"q0","type":"user","cwd":"/code/q","message":{"role":"user","parts":[{"text":"old"}]}}`+"\n")

	h := mustNew(t, "qwen", root)
	sessions, err := h.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions (sidecars aren't sessions), got %d", len(sessions))
	}
	q1 := byID(sessions, "q1")
	if q1.Project != "/code/q" || len(q1.Files) != 2 || filepath.Base(q1.Files[0]) != "q1.jsonl" {
		t.Errorf("q1 = %+v", q1)
	}
	msgs, _ := h.Messages(root, *q1)
	if len(msgs) != 3 || msgs[1].Text != "hi" || msgs[2].Text != "read_file" {
		t.Errorf("messages = %+v", msgs)
	}
	q0 := byID(sessions, "q0")
	if c := h.RestoreFiles(*q0, ""); c[0].Dst != filepath.Join("projects", "-code-q", "chats", "q0.jsonl") {
		t.Errorf("archived restore = %+v", c)
	}
}

func TestClaudeSkip_OnlyAutoMemoryDirs(t *testing.T) {
	for rel, skip := range map[string]bool{
		"-p/memory/":               true,
		"-p/memory/MEMORY.md":      true,
		"-Users-me-memory/":        false, // a project that happens to end in "memory"
		"-Users-me-memory/s.jsonl": false,
		"-p/s1/subagents/memory/":  false,
		"-p/s.jsonl":               false,
	} {
		if got := claudeSkip(rel); got != skip {
			t.Errorf("claudeSkip(%q) = %v, want %v", rel, got, skip)
		}
	}
}

func TestGemini_ResumeStubIsDeduplicated(t *testing.T) {
	root := t.TempDir()
	chats := filepath.Join(root, "tmp", "p", "chats")
	writeFile(t, filepath.Join(chats, "session-2026-01-01T00-00-aaaa1111.jsonl"),
		`{"sessionId":"aaaa1111-x","startTime":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T00:00:00Z"}`+"\n"+
			`{"id":"1","timestamp":"2026-01-01T00:00:01Z","type":"user","content":[{"text":"real"}]}`+"\n")
	writeFile(t, filepath.Join(chats, "session-2026-01-02T00-00-aaaa1111.jsonl"),
		`{"sessionId":"aaaa1111-x","startTime":"2026-01-02T00:00:00Z","lastUpdated":"2026-01-02T00:00:00Z"}`+"\n")
	sessions, _ := mustNew(t, "gemini", root).Scan(root)
	if len(sessions) != 1 || len(sessions[0].Prompts) != 1 {
		t.Errorf("want one session keeping the file with messages, got %+v", sessions)
	}
}

func TestSafeID(t *testing.T) {
	for id, ok := range map[string]bool{
		"0742fed7-4e00-4f99-9945-8bc22e7ef3d0": true,
		"ses_ef78e1ca4ffe0O3DofdnO1xIWD":       true,
		"":                                     false,
		"..":                                   false,
		"../../../.ssh":                        false,
		"a/b":                                  false,
		`a\b`:                                  false,
		"--config=x":                           false,
		"a\nb":                                 false,
	} {
		if SafeID(id) != ok {
			t.Errorf("SafeID(%q) = %v, want %v", id, !ok, ok)
		}
	}
}

func TestGemini_UnsafeSessionIDIsIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tmp", "p", "chats", "session-2026-01-01T00-00-evil.jsonl"),
		`{"sessionId":"../../../../.ssh","startTime":"2026-01-01T00:00:00Z"}`+"\n"+
			`{"id":"1","timestamp":"2026-01-01T00:00:01Z","type":"user","content":[{"text":"x"}]}`+"\n")
	if sessions, _ := mustNew(t, "gemini", root).Scan(root); len(sessions) != 0 {
		t.Errorf("session with traversing id must be skipped, got %+v", sessions)
	}
}
