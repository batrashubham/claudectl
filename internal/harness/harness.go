// Package harness abstracts the coding agents (Claude Code, Codex, Gemini
// CLI, opencode, ...) whose sessions claudectl indexes, backs up and resumes.
//
// Every adapter reads a "root" laid out exactly like the agent's own data
// directory. The backup mirrors that layout per machine, so the same Scan
// code reads both the live directory and any backed-up copy of it.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Prompt struct {
	Text string
	Time time.Time
	// Fallback marks a prompt recovered from a transcript because no prompt
	// history was available; real history entries supersede it.
	Fallback bool
}

// Session is what an adapter knows about one session inside one root.
type Session struct {
	Harness string
	ID      string
	// Project is the working directory recorded by the agent, as it was on
	// the machine that created the session. May be empty if unknown.
	Project string
	// ProjectKey is the agent's own grouping key for the project (Claude's
	// encoded dir, Gemini's path hash, opencode's project ID).
	ProjectKey string
	Title      string
	Prompts    []Prompt
	// PromptCount overrides len(Prompts) when an agent records more turns
	// than it exposes text for. Zero means use len(Prompts).
	PromptCount int
	Start       time.Time
	Updated     time.Time
	SizeBytes   int64
	// Files are paths relative to the root that together make up the
	// session; the first is the main transcript. Directories are allowed.
	Files []string
}

type Message struct {
	Role string // "user", "assistant", "tool"
	Text string
	Time time.Time
}

// SyncRoot is a file or directory, relative to the agent's home, that sync
// copies into the backup.
type SyncRoot struct {
	Path string
	// Mutable marks documents that agents rewrite in place (JSON files)
	// rather than append to (JSONL), so a newer copy wins even if smaller.
	Mutable bool
	// Skip reports whether a path (relative to Path) should not be copied.
	Skip func(rel string) bool
}

// FileCopy maps a file or directory in a backup root to its destination in
// the live home when restoring a session.
type FileCopy struct {
	Src string
	Dst string
}

type Harness interface {
	Name() string
	DisplayName() string
	// Home is the live data directory this instance reads.
	Home() string
	// Binary is the default executable name used to resume sessions.
	Binary() string
	SyncRoots() []SyncRoot
	Scan(root string) ([]Session, error)
	Messages(root string, s Session) ([]Message, error)
	// RestoreFiles lists what to copy back into the live home so the agent
	// can resume s from the given (possibly remapped) project directory.
	RestoreFiles(s Session, project string) []FileCopy
	ResumeArgs(s Session) []string
}

// RestorePreparer is implemented by harnesses that need bookkeeping beyond
// copying files before a restored session can be resumed.
type RestorePreparer interface {
	PrepareRestore(project string) error
}

// Exporter is implemented by harnesses whose data can't simply be copied
// (e.g. a live database). Export writes per-session snapshots under dstRoot
// laid out so Scan can read them, stopping at the deadline. It reports how
// many it wrote and how many are still pending.
type Exporter interface {
	Export(dstRoot string, deadline time.Time) (written, pending int, err error)
}

// Importer is implemented by harnesses that restore a session through the
// agent itself rather than by copying files into its home.
type Importer interface {
	Import(root string, s Session, project string) error
}

// BinarySetter lets the configured executable override the default for
// harnesses that shell out to their agent while syncing or restoring.
type BinarySetter interface {
	SetBinary(bin string)
}

type factory struct {
	display    string
	defaultDir func() string
	build      func(home string) Harness
}

var registry = map[string]factory{}

func register(name, display string, defaultDir func() string, build func(home string) Harness) {
	registry[name] = factory{display: display, defaultDir: defaultDir, build: build}
}

// Names returns all registered harness names in a stable order, with
// Claude Code first since it's the original and most common target.
func Names() []string {
	var names []string
	for n := range registry {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "claude" || names[j] == "claude" {
			return names[i] == "claude"
		}
		return names[i] < names[j]
	})
	return names
}

func Known(name string) bool {
	_, ok := registry[name]
	return ok
}

func DisplayName(name string) string {
	if f, ok := registry[name]; ok {
		return f.display
	}
	return name
}

// DefaultHome returns where the agent keeps its data on this machine,
// honouring the agent's own relocation env vars.
func DefaultHome(name string) string {
	if f, ok := registry[name]; ok {
		return f.defaultDir()
	}
	return ""
}

// New builds an adapter reading the given home ("" means the default).
func New(name, home string) (Harness, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	if home == "" {
		home = f.defaultDir()
	}
	return f.build(home), nil
}

// SafeID reports whether a session ID is safe to use in file paths and as
// a CLI argument. IDs are read from file contents, and a backup can come
// from another machine or a shared remote, so they are untrusted: "../.."
// would escape the agent's directory on restore, and "-x" would be parsed
// as a flag by the agent.
func SafeID(id string) bool {
	if id == "" || len(id) > 200 || id == "." || id == ".." || strings.HasPrefix(id, "-") {
		return false
	}
	for _, r := range id {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

var projectResolvers = map[string]func(key string, known []string) string{}

// ResolveProjectHash recovers a project path for agents that only record a
// one-way key (e.g. a path hash), by testing candidate paths against it.
func ResolveProjectHash(name, key string, known []string) string {
	if f := projectResolvers[name]; f != nil {
		return f(key, known)
	}
	return ""
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func envOr(env string, fallback ...string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return filepath.Join(fallback...)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
