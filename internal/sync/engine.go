package sync

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/machine"
)

type Result struct {
	NewFiles     int
	UpdatedFiles int
	TotalBytes   int64
	PerHarness   map[string]int
	Warnings     []string
	// Migrated is set when a pre-multi-machine backup layout was moved
	// under this machine's subtree during the sync.
	Migrated bool
}

func (r *Result) Changed() bool {
	return r.NewFiles > 0 || r.UpdatedFiles > 0 || r.Migrated
}

const exportBudget = 60 * time.Second

type Engine struct {
	backupDir string
	machine   string
	harnesses []harness.Harness
	manifest  *machine.Manifest
	lockWait  time.Duration
}

func NewEngine(backupDir, machineName string, harnesses []harness.Harness) *Engine {
	return &Engine{backupDir: backupDir, machine: machineName, harnesses: harnesses}
}

// WithManifest makes Sync publish this machine's manifest into the backup.
func (e *Engine) WithManifest(m machine.Manifest) *Engine {
	e.manifest = &m
	return e
}

// WaitForLock makes Sync wait up to d for a concurrent sync to finish
// instead of failing immediately.
func (e *Engine) WaitForLock(d time.Duration) *Engine {
	e.lockWait = d
	return e
}

func (e *Engine) BackupDir() string { return e.backupDir }

func (e *Engine) Sync() (*Result, error) {
	if e.machine == "" {
		return nil, fmt.Errorf("sync needs a machine name")
	}
	if err := os.MkdirAll(e.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	unlock, err := e.acquireLock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	result := &Result{PerHarness: map[string]int{}}

	migrated, err := e.migrateLegacy()
	if err != nil {
		return nil, fmt.Errorf("migrate legacy backup layout: %w", err)
	}
	result.Migrated = migrated

	// Exports shell out to the agent and can be slow; bound them so a sync
	// fits inside hook timeouts. Whatever is left continues next time.
	deadline := time.Now().Add(exportBudget)
	for _, h := range e.harnesses {
		dstRoot := machine.Root(e.backupDir, e.machine, h.Name())
		for _, root := range h.SyncRoots() {
			e.syncRoot(h.Name(), filepath.Join(h.Home(), root.Path), filepath.Join(dstRoot, root.Path), root, result)
		}
		if ex, ok := h.(harness.Exporter); ok {
			written, pending, err := ex.Export(dstRoot, deadline)
			result.UpdatedFiles += written
			if written > 0 {
				result.PerHarness[h.Name()] += written
			}
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", h.DisplayName(), err))
			} else if pending > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %d sessions still to back up, continuing next sync", h.DisplayName(), pending))
			}
		}
	}

	if e.manifest != nil {
		changed, err := machine.WriteManifest(e.backupDir, *e.manifest)
		if err != nil {
			return result, fmt.Errorf("write machine manifest: %w", err)
		}
		if changed {
			result.UpdatedFiles++
		}
	}

	return result, nil
}

func (e *Engine) syncRoot(name, src, dst string, root harness.SyncRoot, result *Result) {
	info, err := os.Stat(src)
	if err != nil {
		return
	}
	record := func(srcPath, dstPath string) {
		_, statErr := os.Stat(dstPath)
		synced, n, err := syncFileMode(srcPath, dstPath, root.Mutable)
		if err != nil || !synced {
			return
		}
		if statErr == nil {
			result.UpdatedFiles++
		} else {
			result.NewFiles++
		}
		result.TotalBytes += n
		result.PerHarness[name]++
	}

	if !info.IsDir() {
		record(src, dst)
		return
	}

	filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if rel != "." && root.Skip != nil && root.Skip(rel+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if root.Skip != nil && root.Skip(rel) {
			return nil
		}
		record(path, filepath.Join(dst, rel))
		return nil
	})
}

// syncFile copies append-only files: only when the backup is missing or
// smaller, so the backup never loses lines the source has since dropped.
func (e *Engine) syncFile(src, dst string) (synced bool, bytes int64, err error) {
	return syncFileMode(src, dst, false)
}

// syncFileMode also handles documents agents rewrite in place; for those a
// newer modification time wins even if the file shrank.
func syncFileMode(src, dst string, mutable bool) (synced bool, bytes int64, err error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, 0, err
	}

	if dstInfo, dstErr := os.Stat(dst); dstErr == nil {
		switch {
		case srcInfo.Size() > dstInfo.Size():
		case mutable && srcInfo.ModTime().After(dstInfo.ModTime()) && !sameContent(src, dst, srcInfo.Size(), dstInfo.Size()):
		default:
			return false, 0, nil
		}
	}

	written, err := copyAtomic(src, dst)
	if err != nil {
		return false, 0, err
	}
	os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
	return true, written, nil
}

func sameContent(a, b string, sizeA, sizeB int64) bool {
	if sizeA != sizeB {
		return false
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return string(da) == string(db)
}

// copyAtomic writes through a temp file and renames it into place, so a
// sync killed mid-copy (hook timeout, Ctrl+C) never leaves a truncated file
// in the backup that a later, smaller source would refuse to overwrite.
func copyAtomic(src, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".claudectl-*.tmp")
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(tmp, in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	return written, nil
}

// migrateLegacy moves a pre-multi-machine backup (Claude data at the backup
// root) under this machine's claude subtree. Nothing is dropped: files the
// subtree already has are kept if at least as large, and history lines are
// unioned.
func (e *Engine) migrateLegacy() (bool, error) {
	dstRoot := machine.Root(e.backupDir, e.machine, "claude")
	migrated := false
	for _, name := range []string{"history.jsonl", "projects"} {
		src := filepath.Join(e.backupDir, name)
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(dstRoot, name)
		if info.IsDir() {
			err = mergeDir(src, dst)
		} else {
			err = mergeFile(src, dst, name == "history.jsonl")
		}
		if err != nil {
			return migrated, err
		}
		migrated = true
	}
	return migrated, nil
}

func mergeDir(src, dst string) error {
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		return mergeFile(path, filepath.Join(dst, rel), false)
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func mergeFile(src, dst string, unionLines bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	dstInfo, err := os.Stat(dst)
	if os.IsNotExist(err) {
		return os.Rename(src, dst)
	}
	if err != nil {
		return err
	}
	if unionLines {
		if err := appendMissingLines(src, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.Size() > dstInfo.Size() {
		return os.Rename(src, dst)
	}
	return os.Remove(src)
}

func appendMissingLines(src, dst string) error {
	have := map[string]bool{}
	if err := eachLine(dst, func(l string) { have[l] = true }); err != nil {
		return err
	}
	var missing []string
	if err := eachLine(src, func(l string) {
		if !have[l] {
			missing = append(missing, l)
			have[l] = true
		}
	}); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if info, err := f.Stat(); err == nil && info.Size() > 0 && !endsWithNewline(dst) {
		w.WriteString("\n")
	}
	for _, l := range missing {
		w.WriteString(l + "\n")
	}
	return w.Flush()
}

func endsWithNewline(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1)
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	if _, err := f.ReadAt(buf, info.Size()-1); err != nil {
		return false
	}
	return buf[0] == '\n'
}

func eachLine(path string, fn func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			fn(l)
		}
	}
	return sc.Err()
}
