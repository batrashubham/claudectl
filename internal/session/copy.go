package session

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/google/uuid"
)

type CopyRequest struct {
	SessionID  string
	ProjectDir string
	Project    string
	ToMachine  string
	// NewProject relocates the copy to another project path. The copy then
	// gets a new session ID so it can't be confused with the original.
	NewProject string
}

type CopyResult struct {
	SessionID string
	Project   string
	Path      string
	Skipped   bool // destination already had this session, same size or larger
}

func (l *Locator) Copy(req CopyRequest) (*CopyResult, error) {
	loc := l.Locate(req.SessionID, req.ProjectDir)
	src := loc.ActivePath
	if src == "" {
		src = loc.ArchivedPath
	}
	if src == "" {
		return nil, fmt.Errorf("session %s: no session file exists", req.SessionID)
	}

	res := &CopyResult{SessionID: req.SessionID, Project: req.Project}
	var replacer *strings.Replacer
	if req.NewProject != "" && req.NewProject != req.Project {
		res.SessionID = uuid.New().String()
		res.Project = req.NewProject
		replacer = strings.NewReplacer(
			req.SessionID, res.SessionID,
			`"cwd":"`+req.Project+`"`, `"cwd":"`+req.NewProject+`"`,
			`"cwd":"`+req.Project+`/`, `"cwd":"`+req.NewProject+`/`,
		)
	}

	destDir := filepath.Join(l.backupDir, "machines", req.ToMachine, "projects", index.ProjectDir(res.Project))
	res.Path = filepath.Join(destDir, res.SessionID+".jsonl")

	if replacer == nil {
		srcInfo, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		if dst, err := os.Stat(res.Path); err == nil && dst.Size() >= srcInfo.Size() {
			res.Skipped = true
			return res, nil
		}
	}

	if err := copyRewritten(src, res.Path, replacer); err != nil {
		return nil, fmt.Errorf("copy session file: %w", err)
	}

	subDir := filepath.Join(filepath.Dir(src), req.SessionID)
	if info, err := os.Stat(subDir); err == nil && info.IsDir() {
		destSub := filepath.Join(destDir, res.SessionID)
		err := filepath.Walk(subDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(subDir, path)
			if strings.HasSuffix(path, ".jsonl") {
				return copyRewritten(path, filepath.Join(destSub, rel), replacer)
			}
			return copyRewritten(path, filepath.Join(destSub, rel), nil)
		})
		if err != nil {
			return nil, fmt.Errorf("copy session subdir: %w", err)
		}
	}
	return res, nil
}

// copyRewritten copies src to dst, applying r to each line when r is set.
func copyRewritten(src, dst string, r *strings.Replacer) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if r == nil {
		return copyFile(src, dst)
	}
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

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), index.MaxLine)
	w := bufio.NewWriter(out)
	for sc.Scan() {
		if _, err := w.WriteString(r.Replace(sc.Text()) + "\n"); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return w.Flush()
}
