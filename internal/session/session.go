// Package session restores backed-up sessions into an agent's live data
// directory and hands the terminal over to the agent to resume them.
package session

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machine"
)

// LocalProject is where a session's project lives on this machine. Sessions
// from other machines get their path translated (path_map rules, then the
// other machine's home dir swapped for ours).
func LocalProject(s index.SessionMeta, localMachine, backupDir string, rules []config.PathMap) string {
	if s.Status == index.StatusActive || s.Machine == localMachine || s.Machine == "" {
		return machine.MapPath(s.Project, rules, "", "")
	}
	fromHome := ""
	if m, ok := machine.Find(backupDir, s.Machine); ok {
		fromHome = m.Home
	}
	home, _ := os.UserHomeDir()
	return machine.MapPath(s.Project, rules, fromHome, home)
}

// Restore copies a backed-up session into the harness's live directory so
// the agent can find it. Active sessions are left alone. Returns whether
// anything was copied.
func Restore(h harness.Harness, s index.SessionMeta, localMachine, project string) (bool, error) {
	if s.Status == index.StatusActive {
		return false, nil
	}
	loc, ok := s.Best(localMachine)
	if !ok {
		return false, fmt.Errorf("session %s: no session file exists (only found in history, file was deleted before backup)", s.ID)
	}

	src := harness.Session{
		Harness:    s.Harness,
		ID:         s.ID,
		Project:    loc.Project,
		ProjectKey: loc.ProjectKey,
		Files:      loc.Files,
	}
	if !harness.SafeID(s.ID) {
		return false, fmt.Errorf("refusing to restore session with unsafe id %q", s.ID)
	}
	for _, f := range loc.Files {
		if _, ok := within(loc.Root, f); !ok {
			return false, fmt.Errorf("session %s: refusing to restore %q: path escapes the backup", s.ID, f)
		}
	}
	if imp, ok := h.(harness.Importer); ok {
		if err := imp.Import(loc.Root, src, project); err != nil {
			return false, err
		}
		return true, nil
	}
	copies := h.RestoreFiles(src, project)
	if len(copies) == 0 {
		return false, fmt.Errorf("session %s: %s cannot restore this session", s.ID, h.DisplayName())
	}
	if p, ok := h.(harness.RestorePreparer); ok {
		if err := p.PrepareRestore(project); err != nil {
			return false, err
		}
	}
	for _, c := range copies {
		from, ok1 := within(loc.Root, c.Src)
		to, ok2 := within(h.Home(), c.Dst)
		if !ok1 || !ok2 {
			return false, fmt.Errorf("session %s: refusing to restore %q to %q: path escapes its directory", s.ID, c.Src, c.Dst)
		}
		if err := copyPath(from, to); err != nil {
			return false, fmt.Errorf("restore %s: %w", c.Src, err)
		}
	}
	return true, nil
}

// within joins rel onto root and reports whether the result stays inside
// root. Backups may come from other machines, so their paths are untrusted.
func within(root, rel string) (string, bool) {
	if filepath.IsAbs(rel) {
		return "", false
	}
	full := filepath.Join(root, rel)
	r, err := filepath.Rel(root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// Exec replaces this process with the agent, run from the project dir.
func Exec(bin string, args []string, project string) error {
	if project != "" {
		if _, err := os.Stat(project); err == nil {
			if err := os.Chdir(project); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not cd to %s: %v\n", project, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: project path %s does not exist here, resuming from current dir\n", project)
		}
	}

	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%s not found in PATH: %w", bin, err)
	}
	return syscall.Exec(path, append([]string{bin}, args...), os.Environ())
}

// copyPath copies a file or directory tree, never replacing a destination
// file that is already at least as large (it may hold newer turns).
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.Size() >= srcInfo.Size() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".claudectl.tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Chtimes(tmp, srcInfo.ModTime(), srcInfo.ModTime())
	return os.Rename(tmp, dst)
}
