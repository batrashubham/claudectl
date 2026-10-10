package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestSync_MigratesFlatLayoutIntoMachineDir(t *testing.T) {
	tmp := t.TempDir()
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)

	write(t, filepath.Join(backupDir, "history.jsonl"), "old history\n")
	write(t, filepath.Join(backupDir, "projects", "-p", "a.jsonl"), "archived\n")
	write(t, filepath.Join(backupDir, "projects", "-p", "a", "subagents", "x.jsonl"), "sub\n")
	write(t, filepath.Join(backupDir, "templates", "-p", "warm", "meta.json"), "{}")

	res, err := NewEngine(claudeDir, backupDir, "work").Sync()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated {
		t.Error("expected Migrated")
	}

	m := filepath.Join(backupDir, "machines", "work")
	if read(t, filepath.Join(m, "projects", "-p", "a.jsonl")) != "archived\n" {
		t.Error("session not moved")
	}
	read(t, filepath.Join(m, "projects", "-p", "a", "subagents", "x.jsonl"))
	if read(t, filepath.Join(m, "history.jsonl")) != "old history\n" {
		t.Error("history not moved")
	}
	for _, gone := range []string{"projects", "history.jsonl"} {
		if _, err := os.Stat(filepath.Join(backupDir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still at backup root", gone)
		}
	}
	read(t, filepath.Join(backupDir, "templates", "-p", "warm", "meta.json"))

	res, _ = NewEngine(claudeDir, backupDir, "work").Sync()
	if res.Migrated {
		t.Error("second sync should not migrate again")
	}
}

func TestSync_MigrationMergesKeepingLargerFile(t *testing.T) {
	tmp := t.TempDir()
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)
	m := filepath.Join(backupDir, "machines", "work", "projects", "-p")

	write(t, filepath.Join(m, "grown.jsonl"), "1\n2\n3\n")
	write(t, filepath.Join(backupDir, "projects", "-p", "grown.jsonl"), "1\n")
	write(t, filepath.Join(m, "stale.jsonl"), "1\n")
	write(t, filepath.Join(backupDir, "projects", "-p", "stale.jsonl"), "1\n2\n")
	write(t, filepath.Join(backupDir, "projects", "-p", "new.jsonl"), "n\n")

	if _, err := NewEngine(claudeDir, backupDir, "work").Sync(); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(m, "grown.jsonl")) != "1\n2\n3\n" {
		t.Error("larger machine copy was overwritten")
	}
	if read(t, filepath.Join(m, "stale.jsonl")) != "1\n2\n" {
		t.Error("larger root copy was not kept")
	}
	read(t, filepath.Join(m, "new.jsonl"))
	if _, err := os.Stat(filepath.Join(backupDir, "projects")); !os.IsNotExist(err) {
		t.Error("root projects/ should be removed")
	}
}

func TestSync_WritesOnlyToOwnMachine(t *testing.T) {
	tmp := t.TempDir()
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	write(t, filepath.Join(claudeDir, "projects", "-p", "s.jsonl"), "x\n")
	write(t, filepath.Join(backupDir, "machines", "home", "projects", "-p", "h.jsonl"), "home\n")

	if _, err := NewEngine(claudeDir, backupDir, "work").Sync(); err != nil {
		t.Fatal(err)
	}
	read(t, filepath.Join(backupDir, "machines", "work", "projects", "-p", "s.jsonl"))
	if _, err := os.Stat(filepath.Join(backupDir, "machines", "home", "projects", "-p", "s.jsonl")); !os.IsNotExist(err) {
		t.Error("wrote into another machine's folder")
	}
}
