package template

import (
	"os"
	"path/filepath"
	"sort"
)

func (s *Store) List(projectDir string) ([]Meta, error) {
	projectPath := filepath.Join(s.baseDir, projectDir)

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var templates []Meta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.ReadMeta(projectDir, entry.Name())
		if err != nil {
			continue
		}
		templates = append(templates, *meta)
	}

	sort.Slice(templates, func(i, j int) bool {
		return templates[i].CreatedAt.After(templates[j].CreatedAt)
	})

	return templates, nil
}

func (s *Store) ListAll() ([]Meta, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var templates []Meta
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		projTemplates, err := s.List(projEntry.Name())
		if err != nil {
			continue
		}
		templates = append(templates, projTemplates...)
	}

	return templates, nil
}

func (s *Store) Delete(projectDir, name string) error {
	tmplDir := s.templateDir(projectDir, name)
	if _, err := os.Stat(tmplDir); os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(tmplDir)
}
