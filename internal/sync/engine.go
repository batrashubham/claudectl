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
	claudeDir    string
	backupDir    string
	templatesDir string
}

func NewEngine(claudeDir, backupDir string) *Engine {
	return &Engine{claudeDir: claudeDir, backupDir: backupDir}
}

func (e *Engine) SetTemplatesDir(dir string) {
	e.templatesDir = dir
}

func (e *Engine) lockPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudectl", ".sync.lock")
}

func (e *Engine) acquireLock() error {
	lockFile := e.lockPath()
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

	// Sync templates directory
	if e.templatesDir != "" {
		if _, statErr := os.Stat(e.templatesDir); statErr == nil {
			templatesDst := filepath.Join(e.backupDir, "templates")

			filepath.Walk(e.templatesDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				relPath, _ := filepath.Rel(e.templatesDir, path)
				dstPath := filepath.Join(templatesDst, relPath)

				_, existsAlready := os.Stat(dstPath)
				synced, bytes, syncErr := e.syncFile(path, dstPath)
				if syncErr != nil {
					return nil
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
		}
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
