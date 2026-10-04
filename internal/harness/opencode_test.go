package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ocSession = "ses_ef78e1ca4ffe0O3DofdnO1xIWD"

func TestOpencode_ScanLiveDatabase(t *testing.T) {
	h := mustNew(t, "opencode", "testdata/opencode/live")
	sessions, err := h.Scan("testdata/opencode/live")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != ocSession || s.Project != "/work/demo" || s.Title != "Hello from the mock model." {
		t.Errorf("session = %+v", s)
	}
	if len(s.Prompts) != 2 || s.Files[0] != "opencode.db" {
		t.Errorf("prompts = %v files = %v", promptTexts(&s), s.Files)
	}
	msgs, err := h.Messages("testdata/opencode/live", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[0].Role != "user" || msgs[1].Text != "Hello from the mock model." {
		t.Errorf("messages = %+v", msgs)
	}
}

func TestOpencode_ScanSnapshots(t *testing.T) {
	h := mustNew(t, "opencode", t.TempDir())
	sessions, err := h.Scan("testdata/opencode/backup")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != ocSession || sessions[0].Project != "/work/demo" {
		t.Fatalf("sessions = %+v", sessions)
	}
	live, _ := mustNew(t, "opencode", "testdata/opencode/live").Scan("testdata/opencode/live")
	if strings.Join(promptTexts(&sessions[0]), "|") != strings.Join(promptTexts(&live[0]), "|") {
		t.Errorf("snapshot and live prompts differ: %v vs %v", promptTexts(&sessions[0]), promptTexts(&live[0]))
	}
	msgs, _ := h.Messages("testdata/opencode/backup", sessions[0])
	if len(msgs) != 4 {
		t.Errorf("snapshot messages = %+v", msgs)
	}
}

// fakeOpencode puts an `opencode` script on PATH that serves the fixture
// snapshot for `export` and records `import` arguments.
func fakeOpencode(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	snapshot, _ := filepath.Abs("testdata/opencode/backup/sessions/" + ocSession + ".json")
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n" +
		"if [ \"$1\" = export ]; then echo 'Exporting session' >&2; cat " + snapshot + "; fi\n" +
		"if [ \"$1\" = import ]; then cp \"$2\" " + filepath.Join(dir, "imported.json") + "; fi\n"
	writeFile(t, filepath.Join(dir, "opencode"), script)
	os.Chmod(filepath.Join(dir, "opencode"), 0755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestOpencode_ExportIsIncremental(t *testing.T) {
	bin := fakeOpencode(t)
	h := mustNew(t, "opencode", "testdata/opencode/live").(*Opencode)
	dst := t.TempDir()

	written, pending, err := h.Export(dst, time.Now().Add(time.Minute))
	if err != nil || written != 1 || pending != 0 {
		t.Fatalf("first export: written=%d pending=%d err=%v", written, pending, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sessions", ocSession+".json")); err != nil {
		t.Fatal("snapshot not written")
	}
	written, _, err = h.Export(dst, time.Now().Add(time.Minute))
	if err != nil || written != 0 {
		t.Errorf("unchanged session should not be re-exported: written=%d err=%v", written, err)
	}
	calls, _ := os.ReadFile(filepath.Join(bin, "calls.log"))
	if strings.Count(string(calls), "export") != 1 {
		t.Errorf("calls = %q", calls)
	}
}

func TestOpencode_ExportWithoutBinaryWarns(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h := mustNew(t, "opencode", "testdata/opencode/live").(*Opencode)
	_, pending, err := h.Export(t.TempDir(), time.Now().Add(time.Minute))
	if err == nil || pending != 1 {
		t.Errorf("want a not-in-PATH error with 1 pending, got pending=%d err=%v", pending, err)
	}
}

func TestOpencode_ImportRemapsDirectory(t *testing.T) {
	bin := fakeOpencode(t)
	h := mustNew(t, "opencode", t.TempDir()).(*Opencode)
	sessions, _ := h.Scan("testdata/opencode/backup")
	local := t.TempDir()

	if err := h.Import("testdata/opencode/backup", sessions[0], local); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Info struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
		} `json:"info"`
	}
	data, err := os.ReadFile(filepath.Join(bin, "imported.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(data, &doc)
	if doc.Info.ID != ocSession || doc.Info.Directory != local {
		t.Errorf("imported info = %+v, want directory %s", doc.Info, local)
	}
	if got := h.ResumeArgs(sessions[0]); strings.Join(got, " ") != "--session "+ocSession {
		t.Errorf("resume args = %v", got)
	}
}
