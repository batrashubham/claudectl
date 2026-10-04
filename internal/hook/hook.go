// Package hook installs and removes claudectl's Claude Code hooks by
// merging into the user's settings.json without disturbing other keys.
package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// marker tags hooks we generated. It is a trailing shell comment, so it is
// inert when the command runs but lets us find our own hooks again without
// depending on the binary's name or path (which the user may change).
const marker = "# claudectl-managed"

// ownsCommand reports whether a hook command belongs to claudectl, so we can
// find and remove our own hooks without touching the user's.
func ownsCommand(cmd string) bool {
	if strings.Contains(cmd, marker) {
		return true
	}
	// Also claim hand-written `claudectl sync` hooks so `hook remove` can
	// clean up after someone who configured this manually.
	return strings.Contains(cmd, "claudectl") && strings.Contains(cmd, "sync")
}

// SessionEnd hooks share a 1.5s budget and do not block session exit, so
// the sync must run async or it gets killed mid-commit.
const timeoutSeconds = 120

var claudeDir string

// SetClaudeDir points the hook at a specific Claude Code data dir (e.g. a
// workspace's), instead of the default from CLAUDE_CONFIG_DIR or ~/.claude.
func SetClaudeDir(dir string) { claudeDir = dir }

func SettingsPath() string {
	if claudeDir != "" {
		return filepath.Join(claudeDir, "settings.json")
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// load reads settings.json into a generic map so unknown keys survive a
// round-trip. A missing file is not an error.
func load(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("settings.json is not valid JSON: %w", err)
	}
	return settings, nil
}

func save(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	// Write via a temp file so a crash can't truncate the user's settings.
	tmp := path + ".claudectl.tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// entries returns the matcher-group list for an event, plus the parent
// containers needed to write it back.
func entries(settings map[string]any, event string) []any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	list, _ := hooks[event].([]any)
	return list
}

// commandWorkspace reports which workspace a claudectl hook command syncs.
// Commands without --workspace predate workspaces and belong to the default.
func commandWorkspace(cmd string) string {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		f = strings.Trim(f, `"`)
		if f == "--workspace" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], `"`)
		}
		if v, ok := strings.CutPrefix(f, "--workspace="); ok {
			return v
		}
	}
	return "default"
}

func isOursFor(group any, workspace string) bool {
	g, ok := group.(map[string]any)
	if !ok {
		return false
	}
	inner, _ := g["hooks"].([]any)
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); ownsCommand(cmd) && commandWorkspace(cmd) == workspace {
			return true
		}
	}
	return false
}

// Installed reports whether a claudectl-owned hook exists for the event
// and workspace, and the command it runs.
func Installed(event, workspace string) (bool, string, error) {
	settings, err := load(SettingsPath())
	if err != nil {
		return false, "", err
	}
	for _, group := range entries(settings, event) {
		if !isOursFor(group, workspace) {
			continue
		}
		g := group.(map[string]any)
		inner, _ := g["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); ownsCommand(cmd) && commandWorkspace(cmd) == workspace {
				return true, cmd, nil
			}
		}
	}
	return false, "", nil
}

// Install adds a SessionEnd hook running `binary sync` in the background.
// Re-installing replaces the previous claudectl hook rather than stacking.
// Each workspace gets its own hook, pinned with an explicit --workspace so
// changing default_workspace later can't silently retarget it. Extra args
// are appended after "sync" (e.g. --wait).
func Install(event, binary, workspace string, syncArgs ...string) error {
	path := SettingsPath()
	settings, err := load(path)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}

	// Drop any previous claudectl hook for this event and workspace.
	var kept []any
	for _, group := range entries(settings, event) {
		if !isOursFor(group, workspace) {
			kept = append(kept, group)
		}
	}

	kept = append(kept, map[string]any{
		// No matcher: fire on every session-end reason.
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": syncCommand(binary, append(append([]string{}, syncArgs...), "--workspace", workspace)),
				// SessionEnd's budget is 1.5s; async detaches the sync so a
				// slow git push is never killed halfway through.
				"async":         true,
				"timeout":       timeoutSeconds,
				"statusMessage": "Backing up sessions",
			},
		},
	})
	hooks[event] = kept

	return save(path, settings)
}

func syncCommand(binary string, args []string) string {
	parts := []string{fmt.Sprintf("%q", binary), "sync"}
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"'$`\\;&|<>(){}*?[]#~") {
			a = fmt.Sprintf("%q", a)
		}
		parts = append(parts, a)
	}
	return strings.Join(append(parts, marker), " ")
}

// Remove deletes the workspace's claudectl hook for the event, leaving any
// hooks the user configured themselves (and other workspaces') untouched.
// Reports whether it removed one.
func Remove(event, workspace string) (bool, error) {
	path := SettingsPath()
	settings, err := load(path)
	if err != nil {
		return false, err
	}

	existing := entries(settings, event)
	if existing == nil {
		return false, nil
	}

	var kept []any
	removed := false
	for _, group := range existing {
		if isOursFor(group, workspace) {
			removed = true
			continue
		}
		kept = append(kept, group)
	}
	if !removed {
		return false, nil
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if len(kept) == 0 {
		delete(hooks, event)
		if len(hooks) == 0 {
			delete(settings, "hooks")
		}
	} else {
		hooks[event] = kept
	}

	return true, save(path, settings)
}
