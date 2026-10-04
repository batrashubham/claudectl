package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockPath sits next to the backup dir rather than inside it, so it is
// never committed and each workspace's backup locks independently.
func (e *Engine) lockPath() string {
	return filepath.Clean(e.backupDir) + ".lock"
}

// acquireLock takes an advisory flock. Unlike an O_EXCL lockfile it is
// released by the kernel when the process dies, so a crashed or killed
// sync can never wedge every later one.
func (e *Engine) acquireLock() (func(), error) {
	path := e.lockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(e.lockWait)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK || time.Now().After(deadline) {
			f.Close()
			if err == syscall.EWOULDBLOCK {
				return nil, fmt.Errorf("sync already in progress (lockfile: %s)", path)
			}
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
