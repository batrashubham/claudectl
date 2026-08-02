package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withSettings points SettingsPath at a temp dir and seeds it with content.
func withSettings(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "settings.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
	}
	return path
}

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, data)
	}
	return m
}

func TestInstall_CreatesSettingsWhenMissing(t *testing.T) {
	path := withSettings(t, "")

	if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, cmd, err := Installed("SessionEnd")
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if !installed {
		t.Fatal("expected hook to be installed")
	}
	// Detection must not depend on the binary being *named* claudectl — the
	// user can rename or relocate it.
	if !strings.Contains(cmd, `"/opt/tools/cctl" sync`) {
		t.Errorf("unexpected command: %q", cmd)
	}
	read(t, path) // must be valid JSON
}

// The tool must never destroy configuration it did not create.
func TestInstall_PreservesExistingSettings(t *testing.T) {
	path := withSettings(t, `{
  "model": "opus",
  "env": {"FOO": "bar"},
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre"}]}],
    "SessionEnd": [{"hooks": [{"type": "command", "command": "echo user-owned"}]}]
  }
}`)

	if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got := read(t, path)
	if got["model"] != "opus" {
		t.Errorf("model was lost: %v", got["model"])
	}
	if env, _ := got["env"].(map[string]any); env["FOO"] != "bar" {
		t.Errorf("env was lost: %v", got["env"])
	}
	if got["permissions"] == nil {
		t.Error("permissions were lost")
	}

	hooks := got["hooks"].(map[string]any)
	if hooks["PreToolUse"] == nil {
		t.Error("unrelated PreToolUse hook was lost")
	}

	// The user's own SessionEnd hook must survive alongside ours.
	se := hooks["SessionEnd"].([]any)
	if len(se) != 2 {
		t.Fatalf("expected user hook + ours = 2 groups, got %d", len(se))
	}
	var foundUser bool
	for _, g := range se {
		inner := g.(map[string]any)["hooks"].([]any)
		for _, h := range inner {
			if h.(map[string]any)["command"] == "echo user-owned" {
				foundUser = true
			}
		}
	}
	if !foundUser {
		t.Error("user's own SessionEnd hook was clobbered")
	}
}

func TestInstall_IsIdempotent(t *testing.T) {
	path := withSettings(t, "")

	for i := 0; i < 3; i++ {
		if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
			t.Fatalf("Install #%d: %v", i, err)
		}
	}

	se := read(t, path)["hooks"].(map[string]any)["SessionEnd"].([]any)
	if len(se) != 1 {
		t.Errorf("re-installing stacked duplicates: got %d groups, want 1", len(se))
	}
}

// SessionEnd has a 1.5s budget and does not block exit, so the hook must be
// async or a slow git push gets killed mid-operation.
func TestInstall_HookIsAsync(t *testing.T) {
	path := withSettings(t, "")
	if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	se := read(t, path)["hooks"].(map[string]any)["SessionEnd"].([]any)
	inner := se[0].(map[string]any)["hooks"].([]any)
	h := inner[0].(map[string]any)

	if h["async"] != true {
		t.Error("hook must set async:true or it will be killed by the SessionEnd budget")
	}
	if h["timeout"] == nil {
		t.Error("hook should declare a timeout")
	}
}

func TestRemove_LeavesUserHooksIntact(t *testing.T) {
	path := withSettings(t, `{
  "hooks": {
    "SessionEnd": [{"hooks": [{"type": "command", "command": "echo user-owned"}]}]
  }
}`)

	if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	removed, err := Remove("SessionEnd")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Fatal("expected Remove to report a removal")
	}

	se := read(t, path)["hooks"].(map[string]any)["SessionEnd"].([]any)
	if len(se) != 1 {
		t.Fatalf("expected only the user's hook to remain, got %d", len(se))
	}
	inner := se[0].(map[string]any)["hooks"].([]any)
	if inner[0].(map[string]any)["command"] != "echo user-owned" {
		t.Error("removed the wrong hook")
	}

	if installed, _, _ := Installed("SessionEnd"); installed {
		t.Error("our hook should be gone")
	}
}

func TestRemove_NoopWhenNotInstalled(t *testing.T) {
	withSettings(t, `{"model":"opus"}`)
	removed, err := Remove("SessionEnd")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed {
		t.Error("reported a removal when nothing was installed")
	}
}

// Cleaning up should not leave an empty "hooks": {} husk behind.
func TestRemove_CleansUpEmptyContainers(t *testing.T) {
	path := withSettings(t, "")
	if err := Install("SessionEnd", "/opt/tools/cctl"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := Remove("SessionEnd"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, exists := read(t, path)["hooks"]; exists {
		t.Error("empty hooks container should have been removed")
	}
}

// A corrupt settings file must fail loudly, never be silently overwritten.
func TestInstall_RefusesToClobberInvalidJSON(t *testing.T) {
	path := withSettings(t, `{"model": "opus"`) // truncated

	if err := Install("SessionEnd", "/opt/tools/cctl"); err == nil {
		t.Fatal("expected an error on malformed settings.json")
	}

	data, _ := os.ReadFile(path)
	if string(data) != `{"model": "opus"` {
		t.Error("malformed settings file was modified; it must be left untouched")
	}
}
