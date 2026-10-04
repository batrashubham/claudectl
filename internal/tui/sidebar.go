package tui

import (
	"sort"

	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/template"
)

type itemKind int

const (
	kindAll itemKind = iota
	kindProject
	kindHarness
	kindMachine
	kindTemplate
)

type sidebarItem struct {
	kind     itemKind
	label    string
	value    string // project name, harness name, or machine name
	isAll    bool
	isTmpl   bool
	tmplName string
	count    int
}

type paneFocus int

const (
	focusSidebar paneFocus = iota
	focusList
)

func sectionTitle(k itemKind) string {
	switch k {
	case kindHarness:
		return "AGENTS"
	case kindMachine:
		return "MACHINES"
	case kindTemplate:
		return "TEMPLATES"
	}
	return ""
}

func countBy(sessions []index.SessionMeta, key func(index.SessionMeta) string) (map[string]int, []string) {
	counts := map[string]int{}
	for _, s := range sessions {
		if s.IsGhost() {
			continue
		}
		counts[key(s)]++
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return counts, keys
}

func buildSidebarItems(sessions []index.SessionMeta, templates []template.Meta) []sidebarItem {
	projectCounts, projects := countBy(sessions, func(s index.SessionMeta) string { return s.ProjectName() })
	total := 0
	for _, c := range projectCounts {
		total += c
	}

	items := []sidebarItem{{kind: kindAll, label: "All", isAll: true, count: total}}
	for _, p := range projects {
		items = append(items, sidebarItem{kind: kindProject, label: p, value: p, count: projectCounts[p]})
	}

	// Agent and machine sections only earn their space when there is a
	// choice to make.
	harnessCounts, harnesses := countBy(sessions, func(s index.SessionMeta) string { return s.Harness })
	if len(harnesses) > 1 {
		for _, h := range harnesses {
			items = append(items, sidebarItem{kind: kindHarness, label: harness.DisplayName(h), value: h, count: harnessCounts[h]})
		}
	}
	machineCounts, machines := countBy(sessions, func(s index.SessionMeta) string { return s.Machine })
	if len(machines) > 1 {
		for _, m := range machines {
			items = append(items, sidebarItem{kind: kindMachine, label: m, value: m, count: machineCounts[m]})
		}
	}

	for _, t := range templates {
		items = append(items, sidebarItem{
			kind:     kindTemplate,
			label:    t.Name,
			isTmpl:   true,
			tmplName: t.Name,
		})
	}

	return items
}

// matchesSidebar reports whether a session passes the sidebar selection.
func (m *Model) matchesSidebar(s index.SessionMeta) bool {
	if len(m.sidebarItems) == 0 || m.sidebarCursor >= len(m.sidebarItems) {
		return true
	}
	item := m.sidebarItems[m.sidebarCursor]
	switch item.kind {
	case kindProject:
		return s.ProjectName() == item.value
	case kindHarness:
		return s.Harness == item.value
	case kindMachine:
		return s.Machine == item.value
	}
	return true
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

func (m *Model) templateMeta(name string) *template.Meta {
	for i := range m.templates {
		if m.templates[i].Name == name {
			return &m.templates[i]
		}
	}
	return nil
}
