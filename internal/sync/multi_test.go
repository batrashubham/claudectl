package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/machine"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestSync_MigratesLegacyLayout(t *testing.T) {
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, "claude")
	backupDir := filepath.Join(tmp, "backup")
	os.MkdirAll(claudeDir, 0755)

	write(t, filepath.Join(backupDir, "projects", "-p", "old.jsonl"), "old session\n")
	write(t, filepath.Join(backupDir, "history.jsonl"), "{\"a\":1}\n{\"b\":2}\n")
	// The new subtree already has one of the lines plus one of its own.
	write(t, filepath.Join(backupDir, "machines", "m1", "claude", "history.jsonl"), "{\"b\":2}\n{\"c\":3}\n")

	e := newClaudeEngine(t, claudeDir, backupDir)
	res, err := e.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated || !res.Changed() {
		t.Error("expected migration to be reported")
	}

	root := filepath.Join(backupDir, "machines", "m1", "claude")
	if got := read(t, filepath.Join(root, "projects", "-p", "old.jsonl")); got != "old session\n" {
		t.Errorf("migrated session content = %q", got)
	}
	hist := read(t, filepath.Join(root, "history.jsonl"))
	for _, want := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		if strings.Count(hist, want) != 1 {
			t.Errorf("history should contain %s exactly once, got:\n%s", want, hist)
		}
	}
	for _, gone := range []string{"projects", "history.jsonl"} {
		if _, err := os.Stat(filepath.Join(backupDir, gone)); err == nil {
			t.Errorf("legacy %s should have been moved", gone)
		}
	}
}

func TestSync_MigrationKeepsLargerCopy(t *testing.T) {
	tmp := t.TempDir()
	backupDir := filepath.Join(tmp, "backup")
	write(t, filepath.Join(backupDir, "projects", "-p", "s.jsonl"), "short\n")
	write(t, filepath.Join(backupDir, "machines", "m1", "claude", "projects", "-p", "s.jsonl"), "much longer content\n")

	e := newClaudeEngine(t, filepath.Join(tmp, "claude"), backupDir)
	if _, err := e.Sync(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(backupDir, "machines", "m1", "claude", "projects", "-p", "s.jsonl"))
	if got != "much longer content\n" {
		t.Errorf("larger copy should win, got %q", got)
	}
}

func TestSyncFileMode_MutableNewerWinsEvenIfSmaller(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	dst := filepath.Join(tmp, "dst.json")
	write(t, dst, `{"messages":[1,2,3,4,5]}`)
	write(t, src, `{"messages":[1]}`)
	future := time.Now().Add(time.Hour)
	os.Chtimes(src, future, future)

	if synced, _, _ := syncFileMode(src, dst, false); synced {
		t.Error("append-only mode must not replace a larger backup")
	}
	if synced, _, err := syncFileMode(src, dst, true); err != nil || !synced {
		t.Fatalf("mutable mode should copy newer file: synced=%v err=%v", synced, err)
	}
	if got := read(t, dst); got != `{"messages":[1]}` {
		t.Errorf("dst = %q", got)
	}
	if synced, _, _ := syncFileMode(src, dst, true); synced {
		t.Error("second sync of unchanged file should be a no-op")
	}
}

func TestSync_WritesManifestOnlyWhenChanged(t *testing.T) {
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, "claude")
	backupDir := filepath.Join(tmp, "backup")
	os.MkdirAll(claudeDir, 0755)

	e := newClaudeEngine(t, claudeDir, backupDir).WithManifest(machine.Manifest{Name: "m1", OS: "linux"})
	res, err := e.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdatedFiles != 1 {
		t.Errorf("first sync should write the manifest, got %+v", res)
	}
	res, err = e.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed() {
		t.Errorf("unchanged manifest should not count as a change, got %+v", res)
	}
}

func TestSync_SyncsEveryEnabledHarness(t *testing.T) {
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, "claude")
	codexDir := filepath.Join(tmp, "codex")
	backupDir := filepath.Join(tmp, "backup")
	write(t, filepath.Join(claudeDir, "projects", "-p", "a.jsonl"), "a\n")
	write(t, filepath.Join(codexDir, "sessions", "2026", "01", "02", "rollout-x.jsonl"), "x\n")
	write(t, filepath.Join(codexDir, "auth.json"), `{"secret":true}`)

	claude, _ := harness.New("claude", claudeDir)
	codex, _ := harness.New("codex", codexDir)
	res, err := NewEngine(backupDir, "m1", []harness.Harness{claude, codex}).Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.PerHarness["claude"] != 1 || res.PerHarness["codex"] != 1 {
		t.Errorf("per-harness counts = %v", res.PerHarness)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "machines", "m1", "codex", "sessions", "2026", "01", "02", "rollout-x.jsonl")); err != nil {
		t.Error("codex rollout should be backed up")
	}
	if _, err := os.Stat(filepath.Join(backupDir, "machines", "m1", "codex", "auth.json")); err == nil {
		t.Error("credentials must never be backed up")
	}
}

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// Isolate from the developer's global git config (signing, hooks).
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestGit_TwoMachinesShareOneRemote(t *testing.T) {
	gitAvailable(t)
	tmp := t.TempDir()
	remote := filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}

	type box struct {
		claude string
		e      *Engine
	}
	newBox := func(name string) box {
		claudeDir := filepath.Join(tmp, name, "claude")
		h, _ := harness.New("claude", claudeDir)
		e := NewEngine(filepath.Join(tmp, name, "backup"), name, []harness.Harness{h}).
			WithManifest(machine.Manifest{Name: name})
		return box{claudeDir, e}
	}
	syncAndPush := func(b box) {
		t.Helper()
		if err := b.e.GitSetupRemote(remote); err != nil {
			t.Fatal(err)
		}
		res, err := b.e.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if err := b.e.GitCommit(res); err != nil {
			t.Fatal(err)
		}
		if _, err := b.e.GitPush(); err != nil {
			t.Fatal(err)
		}
	}

	laptop, desktop := newBox("laptop"), newBox("desktop")
	write(t, filepath.Join(laptop.claude, "projects", "-p", "l1.jsonl"), "laptop\n")
	write(t, filepath.Join(desktop.claude, "projects", "-p", "d1.jsonl"), "desktop\n")

	syncAndPush(laptop)
	// desktop never pulled: its push must rebase onto laptop's, not fail.
	syncAndPush(desktop)

	write(t, filepath.Join(laptop.claude, "projects", "-p", "l2.jsonl"), "laptop again\n")
	syncAndPush(laptop)

	if err := desktop.e.GitPull(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"machines/laptop/claude/projects/-p/l1.jsonl",
		"machines/laptop/claude/projects/-p/l2.jsonl",
		"machines/desktop/claude/projects/-p/d1.jsonl",
		"machines/laptop/machine.json",
	} {
		if _, err := os.Stat(filepath.Join(desktop.e.BackupDir(), rel)); err != nil {
			t.Errorf("desktop backup missing %s after pull", rel)
		}
	}
	if b := desktop.e.branch(); b != "main" {
		t.Errorf("branch = %q, want main regardless of init.defaultBranch", b)
	}

	if pushed, err := desktop.e.GitPush(); err != nil || pushed {
		t.Errorf("push with nothing new should be a no-op: pushed=%v err=%v", pushed, err)
	}
}
