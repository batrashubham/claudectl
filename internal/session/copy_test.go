package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sid = "11111111-2222-3333-4444-555555555555"

func setup(t *testing.T) (claudeDir, backupDir string) {
	t.Helper()
	tmp := t.TempDir()
	claudeDir, backupDir = filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	dir := filepath.Join(claudeDir, "projects", "-Users-a-app")
	os.MkdirAll(filepath.Join(dir, sid, "subagents"), 0755)
	os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(
		`{"sessionId":"`+sid+`","cwd":"/Users/a/app"}`+"\n"+
			`{"sessionId":"`+sid+`","cwd":"/Users/a/app/sub"}`+"\n"+
			`{"cwd":"/Users/a/application"}`+"\n"), 0644)
	os.WriteFile(filepath.Join(dir, sid, "subagents", "agent-x.jsonl"), []byte(`{"sessionId":"`+sid+`"}`+"\n"), 0644)
	return claudeDir, backupDir
}

func req(to, newProject string) CopyRequest {
	return CopyRequest{SessionID: sid, ProjectDir: "-Users-a-app", Project: "/Users/a/app", ToMachine: to, NewProject: newProject}
}

func TestCopy_SameProjectKeepsID(t *testing.T) {
	claudeDir, backupDir := setup(t)
	l := NewLocator(claudeDir, backupDir, "work")

	res, err := l.Copy(req("home", ""))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(backupDir, "machines", "home", "projects", "-Users-a-app", sid+".jsonl")
	if res.Path != want || res.SessionID != sid {
		t.Errorf("got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(want), sid, "subagents", "agent-x.jsonl")); err != nil {
		t.Error("subagents not copied")
	}

	res, _ = l.Copy(req("home", ""))
	if !res.Skipped {
		t.Error("second copy should be skipped")
	}
}

func TestCopy_NewProjectRewritesIDAndCwd(t *testing.T) {
	claudeDir, backupDir := setup(t)
	res, err := NewLocator(claudeDir, backupDir, "work").Copy(req("home", "/Users/b/code/app"))
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID == sid {
		t.Fatal("expected a new session ID")
	}
	dir := filepath.Join(backupDir, "machines", "home", "projects", "-Users-b-code-app")
	if res.Path != filepath.Join(dir, res.SessionID+".jsonl") {
		t.Errorf("path %s", res.Path)
	}
	b, _ := os.ReadFile(res.Path)
	got := string(b)
	for _, want := range []string{`"cwd":"/Users/b/code/app"`, `"cwd":"/Users/b/code/app/sub"`, `"cwd":"/Users/a/application"`, res.SessionID} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, sid) {
		t.Error("old session ID left in transcript")
	}
	sub, err := os.ReadFile(filepath.Join(dir, res.SessionID, "subagents", "agent-x.jsonl"))
	if err != nil || !strings.Contains(string(sub), res.SessionID) {
		t.Errorf("subagent not rewritten: %v %s", err, sub)
	}
}

func TestCopy_FromAnotherMachinesBackup(t *testing.T) {
	tmp := t.TempDir()
	claudeDir, backupDir := filepath.Join(tmp, "claude"), filepath.Join(tmp, "backup")
	src := filepath.Join(backupDir, "machines", "home", "projects", "-p", sid+".jsonl")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("{}\n"), 0644)

	l := NewLocator(claudeDir, backupDir, "work")
	if loc := l.Locate(sid, "-p"); loc.ArchivedMachine != "home" {
		t.Fatalf("located %+v", loc)
	}
	if err := l.Restore(sid, "-p"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "projects", "-p", sid+".jsonl")); err != nil {
		t.Error("restore from another machine failed")
	}
}
