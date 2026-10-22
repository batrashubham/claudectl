// Package machines implements machine management shared by the CLI and TUI:
// summaries, rename, and copying sessions between machines.
package machines

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/batrashubham/claudectl/internal/sync"
)

type Summary struct {
	Name     string
	This     bool
	Sessions int // with a transcript (ghosts excluded)
	Active   int // live in ~/.claude (this machine only)
	Bytes    int64
	LastSync time.Time // last commit touching the machine's folder
}

// Names lists machines in the backup plus this machine, which may not have
// synced yet.
func Names(cfg *config.Config) []string {
	names := index.Machines(cfg.BackupDir)
	if !slices.Contains(names, cfg.MachineName) {
		names = append(names, cfg.MachineName)
		slices.Sort(names)
	}
	return names
}

func Summarize(cfg *config.Config, sessions []index.SessionMeta) []Summary {
	var out []Summary
	for _, name := range Names(cfg) {
		s := Summary{Name: name, This: name == cfg.MachineName}
		for _, sess := range sessions {
			if sess.Machine != name || sess.FileSize == 0 {
				continue
			}
			s.Sessions++
			if sess.Status == index.StatusActive {
				s.Active++
			}
		}
		dir := filepath.Join(config.MachinesDir(cfg.BackupDir), name)
		filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				s.Bytes += info.Size()
			}
			return nil
		})
		s.LastSync = lastCommit(cfg.BackupDir, filepath.Join("machines", name))
		out = append(out, s)
	}
	return out
}

func lastCommit(repo, path string) time.Time {
	out, err := exec.Command("git", "-C", repo, "log", "-1", "--format=%ct", "--", path).Output()
	if err != nil {
		return time.Time{}
	}
	var sec int64
	fmt.Sscan(strings.TrimSpace(string(out)), &sec)
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// Rename moves this machine's backup folder and saves the new name to config.
func Rename(cfg *config.Config, newName string) error {
	if err := config.ValidateMachineName(newName); err != nil {
		return err
	}
	oldName := cfg.MachineName
	if newName == oldName {
		return nil
	}
	oldDir := filepath.Join(config.MachinesDir(cfg.BackupDir), oldName)
	newDir := filepath.Join(config.MachinesDir(cfg.BackupDir), newName)
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("machine %q already exists in the backup", newName)
	}

	engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir, oldName)
	unlock, err := engine.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	if _, err := os.Stat(oldDir); err == nil {
		if err := os.Rename(oldDir, newDir); err != nil {
			return fmt.Errorf("move %s: %w", oldDir, err)
		}
	}
	cfg.MachineName = newName
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return engine.Commit(fmt.Sprintf("machine: rename %s → %s", oldName, newName))
}

type CopyOutcome struct {
	*session.CopyResult
	To     string
	Pushed bool
	// NewMachine is set when no machine by that name has synced yet.
	NewMachine bool
}

// Copy copies a session into machine to's backup folder (optionally under a
// new project path), commits, and pushes when git_push is on and the copy is
// for another machine.
func Copy(cfg *config.Config, s index.SessionMeta, to, newProject string) (*CopyOutcome, error) {
	if err := config.ValidateMachineName(to); err != nil {
		return nil, err
	}
	if newProject == s.Project {
		newProject = ""
	}
	if to == cfg.MachineName && newProject == "" {
		return nil, fmt.Errorf("nothing to copy: choose another machine or a different project path")
	}

	isNew := !slices.Contains(index.Machines(cfg.BackupDir), to)
	engine := sync.NewEngine(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName)
	unlock, err := engine.Lock()
	if err != nil {
		return nil, err
	}
	res, err := session.NewLocator(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).Copy(session.CopyRequest{
		SessionID:  s.ID,
		ProjectDir: s.ProjectDir,
		Project:    s.Project,
		ToMachine:  to,
		NewProject: newProject,
	})
	if err == nil && !res.Skipped {
		err = engine.Commit(fmt.Sprintf("copy(%s → %s): %s", cfg.MachineName, to, res.SessionID))
	}
	unlock()
	if err != nil {
		return nil, err
	}

	out := &CopyOutcome{CopyResult: res, To: to, NewMachine: isNew}
	if !res.Skipped && to != cfg.MachineName && cfg.GitPush {
		if err := engine.GitPush(); err != nil {
			return out, err
		}
		out.Pushed = true
	}
	return out, nil
}
