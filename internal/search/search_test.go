package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const transcript = `{"type":"summary","summary":"ignored"}
{"type":"user","message":{"role":"user","content":"why does the kafka consumer stall"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Let me check the rebalance config."},{"type":"tool_use","name":"Bash","input":{"command":"grep -r session.timeout.ms ./conf","timeout":5000}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"huge file contents nobody searches","is_error":false}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"ECONNREFUSED 127.0.0.1:9092"}],"is_error":true}]}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"private"},{"type":"tool_use","name":"Edit","input":{"file_path":"/src/consumer.go","old_string":"x"}}]}}
`

func TestExtract(t *testing.T) {
	got, err := Extract(strings.NewReader(transcript))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kafka consumer stall", "rebalance config", "session.timeout.ms", "ECONNREFUSED", "/src/consumer.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"ignored", "huge file contents", "private", "5000", "old_string"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("should not contain %q", unwanted)
		}
	}
}

func TestExtract_LongLine(t *testing.T) {
	long := `{"type":"user","message":{"content":"needle ` + strings.Repeat("x", 3<<20) + `"}}` + "\n"
	got, err := Extract(strings.NewReader(long))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "needle") {
		t.Error("long line not extracted")
	}
}

func TestTerms(t *testing.T) {
	got := Terms(`Kafka "rebalance config"  timeout`)
	want := []string{"kafka", "rebalance config", "timeout"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func newIndex(t *testing.T, docs ...Doc) *Index {
	t.Helper()
	idx, err := Build(docs, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestSearch_AllTermsRequiredInAnyOrder(t *testing.T) {
	idx := newIndex(t,
		Doc{ID: "a", Fallback: "fix the kafka consumer timeout", LastSeen: time.Now()},
		Doc{ID: "b", Fallback: "kafka producer only", LastSeen: time.Now()},
	)
	hits := idx.Search("timeout kafka")
	if len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("got %+v", hits)
	}
}

func TestSearch_PhraseMustBeContiguous(t *testing.T) {
	idx := newIndex(t,
		Doc{ID: "a", Fallback: "consumer group rebalance"},
		Doc{ID: "b", Fallback: "rebalance the consumer"},
	)
	hits := idx.Search(`"consumer group"`)
	if len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("got %+v", hits)
	}
}

func TestSearch_MatchesProjectPath(t *testing.T) {
	idx := newIndex(t, Doc{ID: "a", Project: "/code/payments-api", Fallback: "fix flaky test"})
	if hits := idx.Search("payments flaky"); len(hits) != 1 {
		t.Fatalf("got %+v", hits)
	}
}

func TestSearch_RanksFrequencyAndRecency(t *testing.T) {
	now := time.Now()
	idx := newIndex(t,
		Doc{ID: "once", Fallback: "redis", LastSeen: now},
		Doc{ID: "often", Fallback: strings.Repeat("redis ", 20), LastSeen: now},
		Doc{ID: "old", Fallback: strings.Repeat("redis ", 20), LastSeen: now.AddDate(-1, 0, 0)},
	)
	hits := idx.Search("redis")
	if len(hits) != 3 || hits[0].ID != "often" {
		t.Fatalf("got %+v", hits)
	}
}

func TestSearch_Snippet(t *testing.T) {
	text := strings.Repeat("padding words here ", 20) + "the ECONNREFUSED error on port 9092 " + strings.Repeat("more trailing text ", 20)
	idx := newIndex(t, Doc{ID: "a", Fallback: text})
	s := idx.Search("econnrefused")[0].Snippet
	if !strings.Contains(s, "ECONNREFUSED error on port 9092") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Errorf("snippet %q", s)
	}
	if len(s) > 250 {
		t.Errorf("snippet too long: %d", len(s))
	}
}

func TestSearch_SnippetUnicodeSafe(t *testing.T) {
	text := strings.Repeat("é", 100) + " needle " + strings.Repeat("ü", 100)
	idx := newIndex(t, Doc{ID: "a", Fallback: text})
	s := idx.Search("needle")[0].Snippet
	if !strings.Contains(s, "needle") || !utf8Valid(s) {
		t.Errorf("snippet %q", s)
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }

func TestBuild_CachesUntilFileGrows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	cache := filepath.Join(dir, "cache")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"type":"user","message":{"content":"first"}}` + "\n")
	if _, err := Build([]Doc{{ID: "a", Path: path}}, cache); err != nil {
		t.Fatal(err)
	}

	// Same size, different content: cache is trusted (sessions are append-only).
	write(`{"type":"user","message":{"content":"other"}}` + "\n")
	idx, _ := Build([]Doc{{ID: "a", Path: path}}, cache)
	if len(idx.Search("first")) != 1 {
		t.Error("expected cached text to be used")
	}

	write(`{"type":"user","message":{"content":"other"}}` + "\n" + `{"type":"user","message":{"content":"appended"}}` + "\n")
	idx, _ = Build([]Doc{{ID: "a", Path: path}}, cache)
	if len(idx.Search("appended")) != 1 || len(idx.Search("first")) != 0 {
		t.Error("expected cache to be refreshed after file grew")
	}
}

func TestBuild_MissingFileUsesFallback(t *testing.T) {
	idx := newIndex(t, Doc{ID: "ghost", Path: "/nonexistent.jsonl", Fallback: "typed prompt"})
	if len(idx.Search("typed")) != 1 {
		t.Error("expected fallback text")
	}
}
