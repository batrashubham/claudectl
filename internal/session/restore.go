package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (l *Locator) Restore(sessionID, projectDir string) error {
	loc := l.Locate(sessionID, projectDir)

	if loc.ActivePath != "" {
		return nil // already active, nothing to restore
	}

	if loc.ArchivedPath == "" {
		return fmt.Errorf("session %s: no session file exists (only found in history, file was deleted before backup)", sessionID)
	}

	destDir := filepath.Join(l.claudeDir, "projects", projectDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	destPath := filepath.Join(destDir, sessionID+".jsonl")
	if err := copyFile(loc.ArchivedPath, destPath); err != nil {
		return fmt.Errorf("copy session file: %w", err)
	}

	// Also restore session subdirectory if it exists (subagents, tool-results)
	sessionSubDir := filepath.Join(filepath.Dir(loc.ArchivedPath), sessionID)
	if info, err := os.Stat(sessionSubDir); err == nil && info.IsDir() {
		destSubDir := filepath.Join(destDir, sessionID)
		if err := copyDir(sessionSubDir, destSubDir); err != nil {
			return fmt.Errorf("copy session subdir: %w", err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})
}
