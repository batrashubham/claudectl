package meta

import (
	"os"
	"path/filepath"
	"testing"
)

const desktopTranscript = `{"parentUuid":null,"promptId":"p1","type":"user","message":{"role":"user","content":"plan the gameday"},"uuid":"u1","timestamp":"2026-10-30T06:00:00Z","entrypoint":"claude-desktop"}
{"parentUuid":"u1","type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","input":{}}]},"uuid":"a1"}
{"parentUuid":"a1","type":"user","message":{"role":"user","content":[{"tool_use_id":"t1","type":"tool_result","content":"ok"}]},"uuid":"u2"}
{"parentUuid":"u2","type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"uuid":"u3"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"Another Claude session sent this"},"uuid":"u4"}
{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued"},"uuid":"u5"}
{"type":"user","message":{"role":"user","content":"<task-notification>\n<task-id>x</task-id>"},"uuid":"u6"}
{"type":"user","message":{"role":"user","content":"<local-command-stdout>Compacted</local-command-stdout>"},"uuid":"u7"}
{"type":"user","message":{"role":"user","content":"<command-name>/compact</command-name>"},"uuid":"u8","timestamp":"2026-10-30T06:01:00Z"}
{"type":"user","message":{"role":"user","content":[{"type":"image","source":{}},{"type":"text","text":"look at this"}]},"uuid":"u9","timestamp":"2026-10-30T06:02:00Z"}
{"type":"user","message":{"role":"user","content":[{"type":"image","source":{}}]},"uuid":"u10","timestamp":"2026-10-30T06:03:00Z"}
{"parentUuid":null,"promptId":"p1","type":"user","message":{"role":"user","content":"plan the gameday"},"uuid":"u1","timestamp":"2026-10-30T06:00:00Z","entrypoint":"claude-desktop"}
`

func TestReadPrompts_DesktopTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, desktopTranscript)

	got := ReadPrompts(path)
	want := []string{"plan the gameday", "<command-name>/compact</command-name>", "look at this", "[image]"}
	if len(got) != len(want) {
		t.Fatalf("got %d prompts %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("prompt %d = %q, want %q", i, got[i].Text, w)
		}
	}
	if !got[1].IsCommand() || got[0].Time.IsZero() {
		t.Errorf("command/time: %+v", got)
	}
}

func TestRead_CountsPromptsIncrementallyWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	path, cache := filepath.Join(dir, "s.jsonl"), filepath.Join(dir, "cache")
	write(t, path, desktopTranscript)
	got := Read(path, "s", cache)
	if got.Prompts != 4 || got.FirstPrompt != "plan the gameday" {
		t.Fatalf("got %+v", got)
	}

	// A re-appended entry already counted in an earlier read stays counted once.
	appendTo(t, path, `{"type":"user","message":{"role":"user","content":"look at this"},"uuid":"u9"}`+"\n")
	appendTo(t, path, `{"type":"user","message":{"role":"user","content":"next"},"uuid":"u11"}`+"\n")
	if got := Read(path, "s", cache); got.Prompts != 5 {
		t.Errorf("after append: %d prompts", got.Prompts)
	}
}

func TestRead_OldCacheVersionRescans(t *testing.T) {
	dir := t.TempDir()
	path, cache := filepath.Join(dir, "s.jsonl"), filepath.Join(dir, "cache")
	write(t, path, desktopTranscript)
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cache, "s.json"), `{"size":1,"offset":1,"info":{"aiTitle":"stale"}}`)
	if got := Read(path, "s", cache); got.Prompts != 4 || got.AITitle == "stale" {
		t.Errorf("got %+v", got)
	}
}
