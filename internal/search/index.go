package search

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Doc is one searchable session.
type Doc struct {
	ID       string
	Project  string
	LastSeen time.Time
	// Path is the session transcript; empty for history-only sessions.
	Path string
	// Fallback is used when there is no transcript (typed prompts only).
	Fallback string
}

type indexed struct {
	doc   Doc
	text  string
	lower string
}

type Index struct {
	docs []indexed
}

type Hit struct {
	ID      string
	Score   float64
	Snippet string
}

// Build indexes the docs, reusing cached extractions from cacheDir when the
// transcript has not grown since. Sessions are append-only, so an unchanged
// size means unchanged content.
func Build(docs []Doc, cacheDir string) (*Index, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	idx := &Index{docs: make([]indexed, 0, len(docs))}
	for _, d := range docs {
		text := d.Fallback
		if d.Path != "" {
			if t, err := cached(d, cacheDir); err == nil {
				text = t
			}
		}
		idx.docs = append(idx.docs, indexed{doc: d, text: text, lower: strings.ToLower(text)})
	}
	return idx, nil
}

const cacheHeader = "#claudectl-search v1 "

func cached(d Doc, cacheDir string) (string, error) {
	info, err := os.Stat(d.Path)
	if err != nil {
		return "", err
	}
	cachePath := filepath.Join(cacheDir, d.ID+".txt")

	if data, err := os.ReadFile(cachePath); err == nil {
		if header, body, ok := strings.Cut(string(data), "\n"); ok && strings.HasPrefix(header, cacheHeader) {
			if size, _ := strconv.ParseInt(strings.TrimPrefix(header, cacheHeader), 10, 64); size == info.Size() {
				return body, nil
			}
		}
	}

	f, err := os.Open(d.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	text, err := Extract(f)
	if err != nil {
		return "", err
	}
	_ = os.WriteFile(cachePath, []byte(fmt.Sprintf("%s%d\n%s", cacheHeader, info.Size(), text)), 0644)
	return text, nil
}

// Terms splits a query into lowercase terms. "Quoted phrases" stay together.
func Terms(q string) []string {
	var terms []string
	for i, part := range strings.Split(q, `"`) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if i%2 == 1 {
			terms = append(terms, part)
			continue
		}
		terms = append(terms, strings.Fields(part)...)
	}
	return terms
}

// Search returns sessions containing every term, best matches first.
func (idx *Index) Search(q string) []Hit {
	terms := Terms(q)
	if len(terms) == 0 {
		return nil
	}

	var hits []Hit
	now := time.Now()
	for _, d := range idx.docs {
		meta := strings.ToLower(d.doc.Project + " " + d.doc.ID)
		score := 0.0
		matched := true
		for _, t := range terms {
			n := strings.Count(d.lower, t)
			if n == 0 && !strings.Contains(meta, t) {
				matched = false
				break
			}
			score += math.Log1p(float64(n))
			if strings.Contains(meta, t) {
				score += 1
			}
		}
		if !matched {
			continue
		}
		ageDays := now.Sub(d.doc.LastSeen).Hours() / 24
		score *= 1 + 1/(1+ageDays/30)

		hits = append(hits, Hit{ID: d.doc.ID, Score: score, Snippet: snippet(d, terms)})
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

const (
	snippetRadius = 60
	maxWordSlack  = 20
)

// snippet shows the text around the rarest matching term, so the user can
// see why a session matched.
func snippet(d indexed, terms []string) string {
	text := d.text
	if len(text) != len(d.lower) {
		// Lowercasing changed byte offsets (rare Unicode); show lowercased text.
		text = d.lower
	}
	best, bestCount := -1, math.MaxInt
	for _, t := range terms {
		pos := strings.Index(d.lower, t)
		if pos < 0 {
			continue
		}
		if c := strings.Count(d.lower, t); c < bestCount {
			best, bestCount = pos, c
		}
	}
	if best < 0 {
		return ""
	}
	start := max(0, best-snippetRadius)
	end := min(len(text), best+snippetRadius*2)
	for i := 0; start > 0 && i < maxWordSlack && text[start-1] != ' ' && text[start-1] != '\n'; i++ {
		start--
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for i := 0; end < len(text) && i < maxWordSlack && text[end] != ' ' && text[end] != '\n'; i++ {
		end++
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	s := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s += "…"
	}
	return s
}
