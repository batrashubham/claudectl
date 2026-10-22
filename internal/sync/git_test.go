package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedGit hides the user's global git config (e.g. init.defaultBranch)
// so tests see what a fresh machine would.
func isolatedGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"} {
		t.Setenv(k, v)
	}
}

func TestFirstSyncOnFreshMachinePushesToMain(t *testing.T) {
	isolatedGit(t)
	tmp := t.TempDir()
	remote := filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	write(t, filepath.Join(claudeDir, "projects", "-p", "s.jsonl"), "x\n")

	e := NewEngine(claudeDir, backupDir, "work")
	if err := e.GitSetupRemote(remote); err != nil {
		t.Fatalf("setup remote before backup dir exists: %v", err)
	}
	res, err := e.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.GitCommit(res); err != nil {
		t.Fatal(err)
	}
	if err := e.GitPush(); err != nil {
		t.Fatalf("first push: %v", err)
	}

	out, _ := exec.Command("git", "-C", remote, "branch", "--list").Output()
	if !strings.Contains(string(out), "main") {
		t.Errorf("remote branches: %q", out)
	}
	if _, err := os.Stat(filepath.Join(backupDir, ".git")); err != nil {
		t.Error("backup not a git repo")
	}
}
