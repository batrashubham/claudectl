package sync

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitEnv keeps git from ever blocking on a credential prompt: syncs run
// from hooks and cron where nobody can answer it.
func gitEnv(extra ...string) []string {
	return append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), extra...)
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, lastLine(msg))
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func runGit(dir string, args ...string) error {
	_, err := gitOutput(dir, args...)
	return err
}

// runGitIdentity runs git with a guaranteed committer identity and no GPG
// signing, for history-rewriting operations that must not depend on the
// user's global git config being present.
func runGitIdentity(dir string, args ...string) error {
	full := append([]string{
		"-c", "user.name=claudectl", "-c", "user.email=claudectl@local", "-c", "commit.gpgsign=false",
	}, args...)
	return runGit(dir, full...)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (e *Engine) isRepo() bool {
	_, err := os.Stat(filepath.Join(e.backupDir, ".git"))
	return err == nil
}

func (e *Engine) ensureRepo() error {
	if e.isRepo() {
		return nil
	}
	if err := os.MkdirAll(e.backupDir, 0755); err != nil {
		return err
	}
	if err := runGit(e.backupDir, "init"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	// Pin the branch name: push/pull assume it and init.defaultBranch varies.
	if err := runGit(e.backupDir, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return fmt.Errorf("set default branch: %w", err)
	}
	// Append-only histories merge safely line by line.
	gitattrs := filepath.Join(e.backupDir, ".gitattributes")
	if _, err := os.Stat(gitattrs); os.IsNotExist(err) {
		os.WriteFile(gitattrs, []byte("history.jsonl merge=union\n"), 0644)
	}
	return nil
}

func (e *Engine) branch() string {
	out, err := gitOutput(e.backupDir, "symbolic-ref", "--short", "HEAD")
	if b := strings.TrimSpace(out); err == nil && b != "" {
		return b
	}
	return "main"
}

func (e *Engine) hasCommits() bool {
	return runGit(e.backupDir, "rev-parse", "--verify", "--quiet", "HEAD") == nil
}

// commitArgs adds a fallback identity only when the user has none, so a
// fresh machine or container can still commit, while normal setups keep
// their own author.
func (e *Engine) commitArgs(args ...string) []string {
	out, _ := gitOutput(e.backupDir, "config", "user.email")
	if strings.TrimSpace(out) != "" {
		return args
	}
	who := "claudectl@" + e.machine
	if e.machine == "" {
		who = "claudectl@local"
	}
	return append([]string{"-c", "user.name=claudectl", "-c", "user.email=" + who}, args...)
}

func (e *Engine) GitCommit(result *Result) error {
	if result != nil && !result.Changed() {
		return nil
	}
	if err := e.ensureRepo(); err != nil {
		return err
	}
	if err := runGit(e.backupDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if runGit(e.backupDir, "diff", "--cached", "--quiet") == nil {
		return nil // nothing staged
	}

	msg := "sync"
	if e.machine != "" {
		msg = fmt.Sprintf("sync(%s)", e.machine)
	}
	if result != nil {
		msg += fmt.Sprintf(": %d new, %d updated", result.NewFiles, result.UpdatedFiles)
		if result.Migrated {
			msg += ", migrated legacy layout"
		}
	}
	if err := runGit(e.backupDir, e.commitArgs("commit", "--no-gpg-sign", "-m", msg)...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func (e *Engine) GitSetupRemote(remote string) error {
	if err := e.ensureRepo(); err != nil {
		return err
	}
	if existing, err := gitOutput(e.backupDir, "remote", "get-url", "origin"); err == nil {
		if strings.TrimSpace(existing) == remote {
			return nil
		}
		if err := runGit(e.backupDir, "remote", "set-url", "origin", remote); err != nil {
			return fmt.Errorf("git remote set-url: %w", err)
		}
		return nil
	}
	if err := runGit(e.backupDir, "remote", "add", "origin", remote); err != nil {
		return fmt.Errorf("git remote add: %w", err)
	}
	return nil
}

func (e *Engine) hasRemote() bool {
	return runGit(e.backupDir, "remote", "get-url", "origin") == nil
}

// integrateRemote rebases local sync commits onto whatever other machines
// have pushed. Machines write disjoint subtrees, so this only conflicts if
// two machines edited the same shared file (e.g. a template) concurrently.
func (e *Engine) integrateRemote(branch string) error {
	if err := runGit(e.backupDir, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	upstream := "refs/remotes/origin/" + branch
	if runGit(e.backupDir, "rev-parse", "--verify", "--quiet", upstream) != nil {
		return nil // remote branch doesn't exist yet: first push
	}
	if !e.hasCommits() {
		return runGit(e.backupDir, "reset", "--hard", upstream)
	}
	if runGit(e.backupDir, "merge-base", "--is-ancestor", upstream, "HEAD") == nil {
		return nil // already up to date
	}
	if err := runGitIdentity(e.backupDir, "rebase", "--autostash", upstream); err != nil {
		runGit(e.backupDir, "rebase", "--abort")
		return fmt.Errorf("could not combine with changes from other machines (resolve in %s): %w", e.backupDir, err)
	}
	return nil
}

// GitPush publishes local commits, first rebasing onto other machines'
// pushes. Reports whether anything was pushed; when the last-known remote
// state already matches HEAD it returns without touching the network.
func (e *Engine) GitPush() (bool, error) {
	if !e.isRepo() || !e.hasRemote() {
		return false, fmt.Errorf("no git remote configured")
	}
	if !e.hasCommits() {
		return false, nil
	}
	branch := e.branch()
	if head, err := gitOutput(e.backupDir, "rev-parse", "HEAD"); err == nil {
		if remote, err := gitOutput(e.backupDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil && remote == head {
			return false, nil
		}
	}
	var lastErr error
	// Another machine can push between our fetch and push; retry a few times.
	for attempt := 0; attempt < 3; attempt++ {
		if err := e.integrateRemote(branch); err != nil {
			return false, err
		}
		lastErr = runGit(e.backupDir, "push", "-u", "origin", "HEAD:refs/heads/"+branch)
		if lastErr == nil {
			return true, nil
		}
		if !isRejected(lastErr) {
			break
		}
	}
	return false, fmt.Errorf("git push failed (check SSH keys / remote access): %w", lastErr)
}

func isRejected(err error) bool {
	s := err.Error()
	return strings.Contains(s, "rejected") || strings.Contains(s, "fetch first") || strings.Contains(s, "non-fast-forward")
}

func (e *Engine) GitPull() error {
	if !e.isRepo() {
		return fmt.Errorf("backup dir is not a git repo")
	}
	if !e.hasRemote() {
		return fmt.Errorf("no git remote configured — run 'claudectl setup' or set git_remote in config")
	}
	if err := e.integrateRemote(e.branch()); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}
	return nil
}

func (e *Engine) GitClone(remote string) error {
	if err := os.MkdirAll(filepath.Dir(e.backupDir), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", remote, e.backupDir)
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, lastLine(stderr.String()))
	}
	return nil
}

// LastCommitTime returns the committer date of the newest commit touching
// path (relative to the backup), or "" if there is none.
func (e *Engine) LastCommitTime(path string) string {
	args := []string{"log", "-1", "--format=%cI"}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := gitOutput(e.backupDir, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
