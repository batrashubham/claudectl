package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machine"
)

func TestRestore_CopiesFromOtherMachineIntoRemappedProject(t *testing.T) {
	tmp := t.TempDir()
	backupDir := filepath.Join(tmp, "backup")
	remoteRoot := machine.Root(backupDir, "laptop", "claude")
	src := filepath.Join(remoteRoot, "projects", "-Users-me-app")
	os.MkdirAll(filepath.Join(src, "s1", "subagents"), 0755)
	os.WriteFile(filepath.Join(src, "s1.jsonl"), []byte("transcript\n"), 0644)
	os.WriteFile(filepath.Join(src, "s1", "subagents", "a.jsonl"), []byte("sub\n"), 0644)
	machine.WriteManifest(backupDir, machine.Manifest{Name: "laptop", Home: "/Users/me"})

	live := filepath.Join(tmp, "live")
	h, _ := harness.New("claude", live)
	s := index.SessionMeta{
		ID: "s1", Harness: "claude", Machine: "laptop", Project: "/Users/me/app", ProjectDir: "-Users-me-app",
		Status: index.StatusArchived, FileSize: 11,
		Locations: []index.Location{{Machine: "laptop", Root: remoteRoot, Project: "/Users/me/app", ProjectKey: "-Users-me-app",
			Files: []string{"projects/-Users-me-app/s1.jsonl", "projects/-Users-me-app/s1"}}},
	}

	project := LocalProject(s, "desktop", backupDir, []config.PathMap{{From: "/Users/me", To: "/home/me"}})
	if project != "/home/me/app" {
		t.Fatalf("LocalProject = %q", project)
	}
	if _, err := Restore(h, s, "desktop", project); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(live, "projects", "-home-me-app")
	if data, _ := os.ReadFile(filepath.Join(dst, "s1.jsonl")); string(data) != "transcript\n" {
		t.Errorf("transcript not restored: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dst, "s1", "subagents", "a.jsonl")); err != nil {
		t.Error("companion dir not restored")
	}

	// A restore must never clobber a live copy that has grown since.
	os.WriteFile(filepath.Join(dst, "s1.jsonl"), []byte("transcript\nnewer turn\n"), 0644)
	Restore(h, s, "desktop", project)
	if data, _ := os.ReadFile(filepath.Join(dst, "s1.jsonl")); string(data) != "transcript\nnewer turn\n" {
		t.Errorf("larger live file was overwritten: %q", data)
	}
}

func TestRestore_GhostSessionErrors(t *testing.T) {
	h, _ := harness.New("claude", t.TempDir())
	s := index.SessionMeta{ID: "g", Harness: "claude", Status: index.StatusArchived}
	if _, err := Restore(h, s, "m", ""); err == nil {
		t.Error("ghost session restore should fail")
	}
}

func TestRestore_RefusesPathsEscapingRoots(t *testing.T) {
	tmp := t.TempDir()
	h, _ := harness.New("claude", filepath.Join(tmp, "live"))
	secret := filepath.Join(tmp, "secret")
	os.WriteFile(secret, []byte("key"), 0600)
	for name, s := range map[string]index.SessionMeta{
		"traversing file": {ID: "s1", Harness: "claude", FileSize: 1, Status: index.StatusArchived,
			Locations: []index.Location{{Machine: "evil", Root: filepath.Join(tmp, "backup"), Files: []string{"projects/-p/../../../secret"}}}},
		"traversing id": {ID: "../../x", Harness: "claude", FileSize: 1, Status: index.StatusArchived,
			Locations: []index.Location{{Machine: "evil", Root: filepath.Join(tmp, "backup"), Files: []string{"projects/-p/x.jsonl"}}}},
	} {
		if _, err := Restore(h, s, "me", ""); err == nil {
			t.Errorf("%s: restore should be refused", name)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(tmp, "live")); len(entries) != 0 {
		t.Errorf("nothing should have been written, got %v", entries)
	}
}
