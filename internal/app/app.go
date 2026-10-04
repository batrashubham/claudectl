// Package app wires a resolved workspace config to the harnesses, backup
// engine and index, so commands and the TUI share one view of the world.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/machine"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/batrashubham/claudectl/internal/sync"
)

// Version is set by the binary at startup and recorded in sync commits.
var Version = "dev"

type Env struct {
	Cfg     *config.Config
	Machine string
	// All holds every known harness (pointing at its configured home), so
	// backups made on other machines are readable even if this machine
	// doesn't use that agent.
	All []harness.Harness
	// Enabled holds the harnesses this machine syncs and scans live.
	Enabled []harness.Harness
}

func New(cfg *config.Config) (*Env, error) {
	name, err := machine.LocalName(cfg.Machine)
	if err != nil {
		return nil, err
	}
	for n := range cfg.Harnesses {
		if !harness.Known(n) {
			return nil, fmt.Errorf("config: unknown harness %q (known: %v)", n, harness.Names())
		}
	}
	env := &Env{Cfg: cfg, Machine: name}
	for _, n := range harness.Names() {
		hc := cfg.Harnesses[n]
		h, err := harness.New(n, hc.Home)
		if err != nil {
			return nil, err
		}
		if bs, ok := h.(harness.BinarySetter); ok && hc.Bin != "" {
			bs.SetBinary(hc.Bin)
		}
		env.All = append(env.All, h)
		if enabled(hc, h) {
			env.Enabled = append(env.Enabled, h)
		}
	}
	return env, nil
}

func enabled(hc config.HarnessConfig, h harness.Harness) bool {
	if hc.Enabled != nil {
		return *hc.Enabled
	}
	_, err := os.Stat(h.Home())
	return err == nil
}

func (e *Env) Harness(name string) (harness.Harness, error) {
	for _, h := range e.All {
		if h.Name() == name {
			return h, nil
		}
	}
	return nil, fmt.Errorf("unknown harness %q", name)
}

func (e *Env) IsEnabled(name string) bool {
	for _, h := range e.Enabled {
		if h.Name() == name {
			return true
		}
	}
	return false
}

func (e *Env) Bin(h harness.Harness) string {
	if b := e.Cfg.Harnesses[h.Name()].Bin; b != "" {
		return b
	}
	return h.Binary()
}

func (e *Env) Manifest() machine.Manifest {
	homes := map[string]string{}
	for _, h := range e.Enabled {
		homes[h.Name()] = h.Home()
	}
	return machine.Local(e.Machine, homes)
}

func (e *Env) Engine() *sync.Engine {
	return sync.NewEngine(e.Cfg.BackupDir, e.Machine, e.Enabled).WithManifest(e.Manifest())
}

// Sources lists every directory sessions can be read from: each enabled
// harness's live dir, each machine's backup subtree, and the pre-machines
// backup layout if a backup still has one.
func (e *Env) Sources() []index.Source {
	var out []index.Source
	for _, h := range e.Enabled {
		out = append(out, index.Source{Harness: h, Machine: e.Machine, Root: h.Home(), Live: true})
	}
	for _, m := range machine.List(e.Cfg.BackupDir) {
		for _, h := range e.All {
			root := machine.Root(e.Cfg.BackupDir, m.Name, h.Name())
			if _, err := os.Stat(root); err == nil {
				out = append(out, index.Source{Harness: h, Machine: m.Name, Root: root})
			}
		}
	}
	if claude, err := e.Harness("claude"); err == nil && hasLegacyLayout(e.Cfg.BackupDir) {
		out = append(out, index.Source{Harness: claude, Machine: machine.Legacy, Root: e.Cfg.BackupDir})
	}
	return out
}

func hasLegacyLayout(backupDir string) bool {
	for _, p := range []string{"projects", "history.jsonl"} {
		if _, err := os.Stat(filepath.Join(backupDir, p)); err == nil {
			return true
		}
	}
	return false
}

func (e *Env) Builder() *index.Builder {
	return index.NewBuilder(e.Machine, e.Sources()...)
}

func (e *Env) Index() ([]index.SessionMeta, error) {
	return e.Builder().Build()
}

func (e *Env) Find(ref string) (*index.SessionMeta, error) {
	sessions, err := e.Index()
	if err != nil {
		return nil, err
	}
	return index.Find(sessions, ref)
}

func (e *Env) LocalProject(s index.SessionMeta) string {
	return session.LocalProject(s, e.Machine, e.Cfg.BackupDir, e.Cfg.PathMap)
}

// Restore brings a backed-up session into the agent's live directory and
// returns the local project directory to run the agent from.
func (e *Env) Restore(s index.SessionMeta) (string, error) {
	h, err := e.Harness(s.Harness)
	if err != nil {
		return "", err
	}
	project := e.LocalProject(s)
	if _, err := session.Restore(h, s, e.Machine, project); err != nil {
		return "", err
	}
	return project, nil
}

// Resume restores if needed, then execs the agent. Only returns on error.
func (e *Env) Resume(s index.SessionMeta, extraArgs ...string) error {
	if s.IsGhost() {
		return fmt.Errorf("cannot resume %s: session file was deleted before backup, only history metadata remains", s.ID)
	}
	h, err := e.Harness(s.Harness)
	if err != nil {
		return err
	}
	project, err := e.Restore(s)
	if err != nil {
		return err
	}
	hs := harness.Session{Harness: s.Harness, ID: s.ID, Project: project, ProjectKey: s.ProjectDir}
	args := append(h.ResumeArgs(hs), extraArgs...)
	return session.Exec(e.Bin(h), args, project)
}

// Messages reads a session's transcript from its best available copy.
func (e *Env) Messages(s index.SessionMeta) ([]harness.Message, error) {
	h, err := e.Harness(s.Harness)
	if err != nil {
		return nil, err
	}
	loc, ok := s.Best(e.Machine)
	if !ok {
		return nil, fmt.Errorf("session %s has no transcript", s.ID)
	}
	return h.Messages(loc.Root, harness.Session{Harness: s.Harness, ID: s.ID, Project: loc.Project, ProjectKey: loc.ProjectKey, Files: loc.Files})
}

type SyncOutcome struct {
	Result   *sync.Result
	Pushed   bool
	Warnings []string
}

// Sync runs one full backup cycle: copy, commit, and push if configured.
// Copy failures abort; commit and push problems are reported as warnings
// so a flaky network never loses the local backup.
func (e *Env) Sync(lockWait time.Duration) (*SyncOutcome, error) {
	engine := e.Engine().WaitForLock(lockWait)
	unlock, err := engine.Lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	out := &SyncOutcome{}
	if e.Cfg.GitRemote != "" {
		if err := engine.GitSetupRemote(e.Cfg.GitRemote); err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("could not set up git remote: %v", err))
		}
	}
	result, err := engine.SyncLocked()
	if err != nil {
		return nil, err
	}
	out.Result = result
	out.Warnings = append(out.Warnings, result.Warnings...)
	if e.Cfg.GitAutoCommit {
		if err := engine.GitCommit(result); err != nil {
			out.Warnings = append(out.Warnings, err.Error())
			return out, nil
		}
	}
	if e.Cfg.GitPush && e.Cfg.GitRemote != "" {
		pushed, err := engine.GitPush()
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("push failed: %v", err))
		}
		out.Pushed = pushed
	}
	return out, nil
}
