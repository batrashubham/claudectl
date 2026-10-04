package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/batrashubham/claudectl/internal/harness"
)

// Source is one directory laid out like a harness's data dir: the live one
// or a machine's copy inside the backup.
type Source struct {
	Harness harness.Harness
	Machine string
	Root    string
	Live    bool
}

type Builder struct {
	sources []Source
	local   string
	// Warnings collects per-source scan failures; one unreadable source
	// shouldn't hide every other session.
	Warnings []string
}

func NewBuilder(localMachine string, sources ...Source) *Builder {
	return &Builder{local: localMachine, sources: sources}
}

type promptKey struct {
	ms   int64
	text string
}

type accum struct {
	meta     *SessionMeta
	seen     map[promptKey]bool
	maxCount int
}

func (b *Builder) Build() ([]SessionMeta, error) {
	all := map[string]*accum{}
	var order []string
	b.Warnings = nil

	for _, src := range b.sources {
		if _, err := os.Stat(src.Root); err != nil {
			continue
		}
		sessions, err := src.Harness.Scan(src.Root)
		if err != nil {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s (%s): %v", src.Harness.Name(), src.Root, err))
			continue
		}
		for _, s := range sessions {
			key := s.Harness + ":" + s.ID
			a, ok := all[key]
			if !ok {
				a = &accum{meta: &SessionMeta{ID: s.ID, Harness: s.Harness}, seen: map[promptKey]bool{}}
				all[key] = a
				order = append(order, key)
			}
			a.add(src, s)
		}
	}

	result := make([]SessionMeta, 0, len(all))
	for _, key := range order {
		result = append(result, all[key].finish(b.local))
	}

	resolveUnknownProjects(result)

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].LastSeen.After(result[j].LastSeen)
	})
	return result, nil
}

func (a *accum) add(src Source, s harness.Session) {
	m := a.meta
	m.Locations = append(m.Locations, Location{
		Machine:    src.Machine,
		Root:       src.Root,
		Live:       src.Live,
		Files:      s.Files,
		Project:    s.Project,
		ProjectKey: s.ProjectKey,
		Size:       s.SizeBytes,
	})
	// The live copy's view of the project wins; otherwise first one found.
	if s.Project != "" && (m.Project == "" || src.Live) {
		m.Project = s.Project
	}
	if s.ProjectKey != "" && (m.ProjectDir == "" || src.Live) {
		m.ProjectDir = s.ProjectKey
	}
	if m.Title == "" {
		m.Title = s.Title
	}
	for _, p := range s.Prompts {
		k := promptKey{p.Time.UnixMilli(), p.Text}
		if a.seen[k] {
			continue
		}
		a.seen[k] = true
		m.Prompts = append(m.Prompts, p)
	}
	if s.PromptCount > a.maxCount {
		a.maxCount = s.PromptCount
	}
	if !s.Start.IsZero() && (m.FirstSeen.IsZero() || s.Start.Before(m.FirstSeen)) {
		m.FirstSeen = s.Start
	}
	if s.Updated.After(m.LastSeen) {
		m.LastSeen = s.Updated
	}
	if s.SizeBytes > m.FileSize && len(s.Files) > 0 {
		m.FileSize = s.SizeBytes
	}
}

func (a *accum) finish(local string) SessionMeta {
	m := a.meta
	hasHistory := false
	for _, p := range m.Prompts {
		if !p.Fallback {
			hasHistory = true
			break
		}
	}
	if hasHistory {
		kept := m.Prompts[:0]
		for _, p := range m.Prompts {
			if !p.Fallback {
				kept = append(kept, p)
			}
		}
		m.Prompts = kept
	}
	sort.SliceStable(m.Prompts, func(i, j int) bool { return m.Prompts[i].Time.Before(m.Prompts[j].Time) })

	m.PromptCount = len(m.Prompts)
	if a.maxCount > m.PromptCount {
		m.PromptCount = a.maxCount
	}

	var search strings.Builder
	if m.Title != "" {
		search.WriteString(strings.ToLower(m.Title) + " ")
	}
	for _, p := range m.Prompts {
		if p.Text == "" || isCommand(p.Text) {
			continue
		}
		if m.FirstPrompt == "" {
			m.FirstPrompt = truncate(p.Text, 80)
		}
		m.LastPrompt = truncate(p.Text, 80)
		search.WriteString(strings.ToLower(p.Text) + " ")
	}
	if m.FirstPrompt == "" && m.Title != "" {
		m.FirstPrompt = truncate(m.Title, 80)
	}
	m.SearchText = search.String()

	m.Status = StatusArchived
	for _, l := range m.Locations {
		if l.Live && len(l.Files) > 0 {
			m.Status = StatusActive
		}
	}
	if best, ok := m.Best(local); ok {
		m.Machine = best.Machine
	} else if len(m.Locations) > 0 {
		m.Machine = m.Locations[0].Machine
	}
	return *m
}

// resolveUnknownProjects fills in projects for agents that only record a
// path hash (Gemini CLI) by hashing every project path seen elsewhere.
func resolveUnknownProjects(sessions []SessionMeta) {
	var known []string
	seen := map[string]bool{}
	for _, s := range sessions {
		if s.Project != "" && !seen[s.Project] {
			seen[s.Project] = true
			known = append(known, s.Project)
		}
	}
	if cwd, err := os.Getwd(); err == nil && !seen[cwd] {
		known = append(known, cwd)
	}
	for i := range sessions {
		s := &sessions[i]
		if s.Project != "" || s.ProjectDir == "" {
			continue
		}
		if p := harness.ResolveProjectHash(s.Harness, s.ProjectDir, known); p != "" {
			s.Project = p
		}
	}
}

// ProjectName is the short label shown for a session's project.
func (s SessionMeta) ProjectName() string {
	project := filepath.Base(s.Project)
	if s.Project == "" || project == "." || project == "/" {
		if s.ProjectDir != "" {
			return s.ProjectDir
		}
		return "(unknown)"
	}
	return project
}

func isCommand(s string) bool {
	return len(s) > 0 && s[0] == '/'
}

func truncate(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}

// Find resolves a session reference: a full ID, a unique ID prefix, or
// "harness:id" to disambiguate. Ghost sessions are matched too, so the
// caller can explain why they can't be used.
func Find(sessions []SessionMeta, ref string) (*SessionMeta, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty session id")
	}
	harnessName := ""
	if i := strings.IndexByte(ref, ':'); i > 0 && harness.Known(ref[:i]) {
		harnessName, ref = ref[:i], ref[i+1:]
	}
	var matches []*SessionMeta
	for i := range sessions {
		s := &sessions[i]
		if harnessName != "" && s.Harness != harnessName {
			continue
		}
		if s.ID == ref {
			return s, nil
		}
		if strings.HasPrefix(s.ID, ref) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("session %s not found", ref)
	case 1:
		return matches[0], nil
	}
	var ids []string
	for i, m := range matches {
		if i == 5 {
			ids = append(ids, "...")
			break
		}
		ids = append(ids, m.Harness+":"+m.ID)
	}
	return nil, fmt.Errorf("session prefix %q is ambiguous: %s", ref, strings.Join(ids, ", "))
}

func PromptEntries(s SessionMeta) []HistoryEntry {
	var entries []HistoryEntry
	for _, p := range s.Prompts {
		if p.Text == "" || isCommand(p.Text) {
			continue
		}
		entries = append(entries, HistoryEntry{
			Display:   p.Text,
			Timestamp: p.Time.UnixMilli(),
			Project:   s.Project,
			SessionID: s.ID,
		})
	}
	return entries
}

// AllPromptEntries flattens every session's prompts, for analytics.
func AllPromptEntries(sessions []SessionMeta) []HistoryEntry {
	var out []HistoryEntry
	for _, s := range sessions {
		for _, p := range s.Prompts {
			out = append(out, HistoryEntry{Display: p.Text, Timestamp: p.Time.UnixMilli(), Project: s.Project, SessionID: s.ID})
		}
	}
	return out
}
