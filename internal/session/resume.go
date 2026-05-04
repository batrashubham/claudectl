package session

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func (l *Locator) Resume(sessionID, projectDir, projectPath string) error {
	if err := l.Restore(sessionID, projectDir); err != nil {
		return err
	}

	// Change to project directory if it exists
	if projectPath != "" {
		if _, err := os.Stat(projectPath); err == nil {
			if err := os.Chdir(projectPath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not cd to %s: %v\n", projectPath, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: project path %s no longer exists, resuming from current dir\n", projectPath)
		}
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found in PATH: %w", err)
	}

	return syscall.Exec(claudeBin, []string{"claude", "--resume", sessionID}, os.Environ())
}
