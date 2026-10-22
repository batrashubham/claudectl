package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const transcript = `{"type":"permission-mode","permissionMode":"default"}
{"parentUuid":null,"type":"user","message":{"role":"user","content":"hi"},"cwd":"/p","version":"2.1.280","gitBranch":"HEAD"}
{"parentUuid":"a","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","input":{"gitBranch":"fake","version":"9.9"}}]},"type":"assistant","version":"2.1.290","gitBranch":"feature-x"}
{"type":"ai-title","aiTitle":"Generated title"}
{"type":"pr-link","prNumber":7,"prUrl":"https://github.com/o/r/pull/7","prRepository":"o/r"}
{"type":"pr-link","prNumber":7,"prUrl":"https://github.com/o/r/pull/7","prRepository":"o/r"}
{"type":"worktree-state","worktreeSession":{"worktreeName":"wt-1"}}
{"type":"agent-name","agentName":"reviewer"}
`

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func TestRead_ExtractsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, transcript)
	got := Read(path, "s", "")

	if got.Title() != "Generated title" || got.Branch != "feature-x" || got.Model != "claude-opus-5-5" ||
		got.ClaudeVersion != "2.1.290" || got.Worktree != "wt-1" || got.Agent != "reviewer" {
		t.Errorf("got %+v", got)
	}
	if len(got.PRs) != 1 || got.PRs[0].Number != 7 || got.PRs[0].Repo != "o/r" {
		t.Errorf("PRs %+v", got.PRs)
	}
}

func TestRead_CustomTitleWinsAndLatestRenameApplies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, transcript+`{"type":"custom-title","customTitle":"first name"}`+"\n"+`{"type":"custom-title","customTitle":"renamed"}`+"\n")
	if got := Read(path, "s", "").Title(); got != "renamed" {
		t.Errorf("title %q", got)
	}
}

func TestRead_IncrementalCache(t *testing.T) {
	dir := t.TempDir()
	path, cache := filepath.Join(dir, "s.jsonl"), filepath.Join(dir, "cache")
	write(t, path, transcript)
	if Read(path, "s", cache).Title() != "Generated title" {
		t.Fatal("first read")
	}

	// A partial trailing line (still being written) must not be consumed.
	appendTo(t, path, `{"type":"custom-title","customTi`)
	if Read(path, "s", cache).Title() != "Generated title" {
		t.Fatal("partial line applied")
	}
	appendTo(t, path, `tle":"Mine"}`+"\n")
	got := Read(path, "s", cache)
	if got.Title() != "Mine" || len(got.PRs) != 1 || got.Branch != "feature-x" {
		t.Errorf("after append: %+v", got)
	}

	// Cached info is used without rereading when the size is unchanged.
	data, _ := os.ReadFile(path)
	write(t, path, strings.Replace(string(data), "Mine", "Nope", 1))
	if Read(path, "s", cache).Title() != "Mine" {
		t.Error("expected cached result for unchanged size")
	}
}

func TestRead_ShrunkFileRescans(t *testing.T) {
	dir := t.TempDir()
	path, cache := filepath.Join(dir, "s.jsonl"), filepath.Join(dir, "cache")
	write(t, path, transcript)
	Read(path, "s", cache)
	write(t, path, `{"type":"custom-title","customTitle":"fresh"}`+"\n")
	if got := Read(path, "s", cache); got.Title() != "fresh" {
		t.Errorf("got %+v", got)
	}
}

func TestRead_MissingFile(t *testing.T) {
	if got := Read("/nonexistent", "x", t.TempDir()); got.Title() != "" {
		t.Error("expected empty")
	}
}
