package sync

import (
	"os"
	"path/filepath"
)

// migrateFlatLayout moves a pre-machines backup (projects/ and history.jsonl
// at the root) into machines/<name>/. It also absorbs files written to the
// root later by an older claudectl, keeping the larger copy of each file.
func (e *Engine) migrateFlatLayout() (bool, error) {
	moved := false
	for _, name := range []string{"projects", "history.jsonl"} {
		src := filepath.Join(e.backupDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := mergeMove(src, filepath.Join(e.machineDir(), name)); err != nil {
			return moved, err
		}
		moved = true
	}
	return moved, nil
}

func mergeMove(src, dst string) error {
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		return os.Rename(src, dst)
	}

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if t, err := os.Stat(target); err == nil && t.Size() >= info.Size() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.Rename(path, target)
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(src)
}
