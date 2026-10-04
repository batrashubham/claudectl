package sync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoSize returns the total size of the backup directory in bytes.
func (e *Engine) RepoSize() (int64, error) {
	var total int64
	err := filepath.Walk(e.backupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// GitDirSize returns the size of just the .git directory in bytes.
func (e *Engine) GitDirSize() (int64, error) {
	gitDir := filepath.Join(e.backupDir, ".git")
	var total int64
	err := filepath.Walk(gitDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// GC runs git garbage collection to reclaim space.
func (e *Engine) GC() error {
	gitDir := e.backupDir
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("backup is not a git repo")
	}
	return runGit(gitDir, "gc", "--aggressive", "--prune=now")
}

// Squash collapses all git history into a single commit, discarding
// the commit history but keeping the current file state. This reclaims
// space when accumulated history of large append-only files grows the repo.
func (e *Engine) Squash() error {
	gitDir := e.backupDir
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("backup is not a git repo")
	}

	branch := e.branch()

	// Create an orphan branch with current state, then replace the branch
	if err := runGit(gitDir, "checkout", "--orphan", "squashed-tmp"); err != nil {
		return fmt.Errorf("create orphan branch: %w", err)
	}
	if err := runGit(gitDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := runGit(gitDir, e.commitArgs("commit", "--no-gpg-sign", "-m", "squash: compact backup history")...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := runGit(gitDir, "branch", "-D", branch); err != nil {
		return fmt.Errorf("delete old %s: %w", branch, err)
	}
	if err := runGit(gitDir, "branch", "-m", branch); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}
	return e.GC()
}

// SquashOlderThan keeps commit history for the last `days` days and
// collapses everything older into a single base commit. Returns the
// number of commits preserved.
func (e *Engine) SquashOlderThan(days int) (int, error) {
	gitDir := e.backupDir
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		return 0, fmt.Errorf("backup is not a git repo")
	}

	since := fmt.Sprintf("--since=%d days ago", days)

	// Find the oldest commit within the retention window
	cmd := exec.Command("git", "log", since, "--reverse", "--format=%H")
	cmd.Dir = gitDir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		// No commits within the window — everything is old, full squash
		return 0, e.Squash()
	}

	oldestKept := lines[0]

	// Find the parent of the oldest kept commit (the squash boundary)
	cmd = exec.Command("git", "rev-parse", oldestKept+"^")
	cmd.Dir = gitDir
	parentOut, err := cmd.Output()
	if err != nil {
		// oldestKept is the root commit — nothing older to squash
		return len(lines), nil
	}
	boundary := strings.TrimSpace(string(parentOut))

	// Build a single base commit holding the boundary's full tree (orphan, no parent),
	// then rebase the kept commits (boundary..main) onto that base. This collapses
	// all history up to the boundary into one commit while preserving recent commits.
	treeCmd := exec.Command("git", "rev-parse", boundary+"^{tree}")
	treeCmd.Dir = gitDir
	treeOut, err := treeCmd.Output()
	if err != nil {
		return 0, fmt.Errorf("resolve boundary tree: %w", err)
	}
	tree := strings.TrimSpace(string(treeOut))

	msg := fmt.Sprintf("squash: history before last %d days", days)
	ctCmd := exec.Command("git",
		"-c", "commit.gpgsign=false",
		"-c", "user.name=claudectl",
		"-c", "user.email=claudectl@local",
		"commit-tree", tree, "-m", msg)
	ctCmd.Dir = gitDir
	baseOut, err := ctCmd.Output()
	if err != nil {
		return 0, fmt.Errorf("create base commit: %w", err)
	}
	base := strings.TrimSpace(string(baseOut))

	// Rebase boundary..main onto the new base commit
	if err := runGitIdentity(gitDir, "rebase", "--onto", base, boundary, e.branch()); err != nil {
		runGit(gitDir, "rebase", "--abort")
		return 0, fmt.Errorf("rebase onto base failed: %w", err)
	}

	if err := e.GC(); err != nil {
		return len(lines), err
	}
	return len(lines), nil
}
