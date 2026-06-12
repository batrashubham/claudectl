package sync

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	NewFiles     int
	UpdatedFiles int
	TotalBytes   int64
}

type Engine struct {
	claudeDir string
	backupDir string
}

func NewEngine(claudeDir, backupDir string) *Engine {
	return &Engine{claudeDir: claudeDir, backupDir: backupDir}
}

func (e *Engine) lockPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudectl", ".sync.lock")
}

func (e *Engine) acquireLock() error {
	lockFile := e.lockPath()
	if err := os.MkdirAll(filepath.Dir(lockFile), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("sync already in progress (lockfile: %s)", lockFile)
		}
		return err
	}
	fmt.Fprintf(f, "%d", os.Getpid())
	f.Close()
	return nil
}

func (e *Engine) releaseLock() {
	os.Remove(e.lockPath())
}

func (e *Engine) Sync() (*Result, error) {
	if err := os.MkdirAll(e.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	if err := e.acquireLock(); err != nil {
		return nil, err
	}
	defer e.releaseLock()

	result := &Result{}

	// Sync history.jsonl
	histSrc := filepath.Join(e.claudeDir, "history.jsonl")
	histDst := filepath.Join(e.backupDir, "history.jsonl")
	if synced, bytes, err := e.syncFile(histSrc, histDst); err == nil && synced {
		result.UpdatedFiles++
		result.TotalBytes += bytes
	}

	// Sync projects directory
	projectsSrc := filepath.Join(e.claudeDir, "projects")
	projectsDst := filepath.Join(e.backupDir, "projects")

	err := filepath.Walk(projectsSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(projectsSrc, path)

		// Skip memory directories
		if strings.Contains(relPath, "memory/") {
			return nil
		}

		dstPath := filepath.Join(projectsDst, relPath)

		_, existsAlready := os.Stat(dstPath)

		synced, bytes, syncErr := e.syncFile(path, dstPath)
		if syncErr != nil {
			return nil // skip problematic files
		}
		if synced {
			if existsAlready == nil {
				result.UpdatedFiles++
			} else {
				result.NewFiles++
			}
			result.TotalBytes += bytes
		}

		return nil
	})

	if err != nil {
		return result, err
	}

	return result, nil
}

func (e *Engine) syncFile(src, dst string) (synced bool, bytes int64, err error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, 0, err
	}

	dstInfo, dstErr := os.Stat(dst)
	if dstErr == nil && dstInfo.Size() >= srcInfo.Size() {
		return false, 0, nil // dest is same size or larger, skip
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return false, 0, err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return false, 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return false, 0, err
	}
	defer dstFile.Close()

	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return false, 0, err
	}

	return true, written, nil
}

func (e *Engine) GitCommit(result *Result) error {
	if result.NewFiles == 0 && result.UpdatedFiles == 0 {
		return nil
	}

	gitDir := e.backupDir

	// Initialize git repo if needed
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		if err := runGit(gitDir, "init"); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
		// Safe multi-machine merge: append-only files use union strategy
		gitattrs := filepath.Join(gitDir, ".gitattributes")
		os.WriteFile(gitattrs, []byte("history.jsonl merge=union\n"), 0644)
	}

	if err := runGit(gitDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there are actual changes staged
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = gitDir
	if err := cmd.Run(); err == nil {
		return nil // nothing staged
	}

	msg := fmt.Sprintf("sync: %d new, %d updated", result.NewFiles, result.UpdatedFiles)
	if err := runGit(gitDir, "commit", "--no-gpg-sign", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

func (e *Engine) GitSetupRemote(remote string) error {
	gitDir := e.backupDir

	// Initialize git repo if needed
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		if err := runGit(gitDir, "init"); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	// Check if origin already exists
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = gitDir
	if existing, err := cmd.Output(); err == nil {
		currentRemote := strings.TrimSpace(string(existing))
		if currentRemote == remote {
			return nil // already configured
		}
		// Update existing remote
		if err := runGit(gitDir, "remote", "set-url", "origin", remote); err != nil {
			return fmt.Errorf("git remote set-url: %w", err)
		}
	} else {
		// Add new remote
		if err := runGit(gitDir, "remote", "add", "origin", remote); err != nil {
			return fmt.Errorf("git remote add: %w", err)
		}
	}

	return nil
}

func (e *Engine) GitPush() error {
	gitDir := e.backupDir

	// Check remote exists
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("no git remote configured")
	}

	if err := runGit(gitDir, "push", "-u", "origin", "main"); err != nil {
		return fmt.Errorf("git push failed (check SSH keys / remote access): %w", err)
	}

	return nil
}

func (e *Engine) GitPull() error {
	gitDir := e.backupDir

	// Must have a git repo
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("backup dir is not a git repo")
	}

	// Check remote exists
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("no git remote configured — run 'claudectl setup' or set git_remote in config")
	}

	if err := runGit(gitDir, "pull", "--rebase", "origin", "main"); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	return nil
}

func (e *Engine) GitClone(remote string) error {
	if err := os.MkdirAll(filepath.Dir(e.backupDir), 0755); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone", remote, e.backupDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	return nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

// runGitIdentity runs git with a guaranteed committer identity and no GPG
// signing, for history-rewriting operations that must not depend on the
// user's global git config being present.
func runGitIdentity(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=claudectl", "GIT_AUTHOR_EMAIL=claudectl@local",
		"GIT_COMMITTER_NAME=claudectl", "GIT_COMMITTER_EMAIL=claudectl@local",
	)
	return cmd.Run()
}

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

	// Create an orphan branch with current state, then replace main
	if err := runGit(gitDir, "checkout", "--orphan", "squashed-tmp"); err != nil {
		return fmt.Errorf("create orphan branch: %w", err)
	}
	if err := runGit(gitDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := runGit(gitDir, "commit", "--no-gpg-sign", "-m", "squash: compact backup history"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := runGit(gitDir, "branch", "-D", "main"); err != nil {
		return fmt.Errorf("delete old main: %w", err)
	}
	if err := runGit(gitDir, "branch", "-m", "main"); err != nil {
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
	if err := runGitIdentity(gitDir, "rebase", "--onto", base, boundary, "main"); err != nil {
		runGit(gitDir, "rebase", "--abort")
		return 0, fmt.Errorf("rebase onto base failed: %w", err)
	}

	if err := e.GC(); err != nil {
		return len(lines), err
	}
	return len(lines), nil
}
