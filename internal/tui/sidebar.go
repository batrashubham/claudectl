package tui

import (
	"path/filepath"
	"sort"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/template"
)

type sidebarItem struct {
	label    string
	project  string // full project path, empty for "All"
	isAll    bool
	isTmpl   bool
	tmplName string
	count    int

	isMachine bool
	machine   string // "" for "All machines"
}

// key identifies an item across rebuilds so the cursor can stay on it.
func (it sidebarItem) key() string {
	switch {
	case it.isMachine:
		return "m:" + it.machine
	case it.isTmpl:
		return "t:" + it.tmplName
	case it.isAll:
		return "all"
	}
	return "p:" + it.label
}

type paneFocus int

const (
	focusSidebar paneFocus = iota
	focusList
)

func buildSidebarItems(sessions []index.SessionMeta, templates []template.Meta) []sidebarItem {
	// Count sessions per project (exclude ghosts)
	projectCounts := make(map[string]int)
	totalCount := 0
	for _, s := range sessions {
		if s.FileSize == 0 {
			continue
		}
		project := filepath.Base(s.Project)
		if project == "" || project == "." {
			project = s.ProjectDir
		}
		projectCounts[project]++
		totalCount++
	}

	// Sort project names
	var projects []string
	for p := range projectCounts {
		projects = append(projects, p)
	}
	sort.Strings(projects)

	// Build items
	items := []sidebarItem{{label: "All", isAll: true, count: totalCount}}
	for _, p := range projects {
		items = append(items, sidebarItem{label: p, count: projectCounts[p]})
	}

	// Templates section
	for _, t := range templates {
		items = append(items, sidebarItem{
			label:    t.Name,
			isTmpl:   true,
			tmplName: t.Name,
		})
	}

	return items
}

func (m *Model) sidebarProjectFilter() string {
	if len(m.sidebarItems) == 0 || m.sidebarCursor >= len(m.sidebarItems) {
		return ""
	}
	item := m.sidebarItems[m.sidebarCursor]
	if item.isAll || item.isTmpl || item.isMachine {
		return ""
	}
	return item.label
}

func (m *Model) selectedTemplate() string {
	if len(m.sidebarItems) == 0 || m.sidebarCursor >= len(m.sidebarItems) {
		return ""
	}
	item := m.sidebarItems[m.sidebarCursor]
	if item.isTmpl {
		return item.tmplName
	}
	return ""
}
