# claudectl

CLI tool for managing Claude Code sessions with long-term persistence, written in Go.

## Build & Run

```bash
go build -o claudectl .     # build
go install .                # install to ~/go/bin/
go build ./...              # check all packages compile
./claudectl list            # quick smoke test (no TTY needed)
./claudectl sync            # test sync (needs ~/.claudectl/config.toml)
```

No test suite yet — verify by building and running commands.

## Architecture

```
cmd/              # Cobra CLI commands (root, sync, list, resume, restore, template, cron, setup, config)
internal/
  config/         # TOML config at ~/.claudectl/config.toml
  index/          # Session index: merges history.jsonl + filesystem walk
  session/        # Locate, restore, resume (syscall.Exec)
  sync/           # Append-only copy engine + git commit/push + lockfile
  template/       # Save/spawn/list/delete session templates
  tui/            # Bubble Tea two-pane TUI (sidebar + session list)
```

## Key Design Decisions

- **Append-only sync**: never delete from backup. Copy if dest missing or source larger.
- **Session ID rewriting**: templates use `strings.ReplaceAll(line, oldUUID, newUUID)` streaming line-by-line. Safe because UUID is unique 36-char string.
- **No background pulls**: `restore` is manual-only. Sync only pushes.
- **Lockfile**: `~/.claudectl/.sync.lock` prevents concurrent sync races.
- **Git signing disabled**: all commits use `--no-gpg-sign` (1Password agent conflicts).
- **Ghost sessions** (FileSize==0): exist in history.jsonl only, file was deleted before backup. Hidden from default view, shown in Ghost filter tab.

## TUI Conventions

- Two-pane: left sidebar (projects + templates), right pane (sessions or template detail)
- Focus: Tab switches panes, j/k navigates within focused pane
- Colors: purple gradient palette, cyan cursor, green active dots
- Background fills for selection (not border boxes — they render poorly)
- lipgloss `Width()` for layout, `JoinHorizontal` for panes

## Release Process

```bash
git tag v0.X.Y
git push --tags
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

## Code Style

- Go standard formatting (gofmt)
- No comments unless non-obvious why
- Cobra commands in separate files under cmd/
- Internal packages not exported
- Error messages: lowercase, no punctuation, include context

## Session-format notes (verified against Claude Code 2.1.220)

- `--trim` is a *remove-list*: unknown entry types are always kept, so new
  Claude Code entry types are safe by default. Re-audit the list when Claude
  Code adds types; see `internal/template/save.go` for what is deliberately kept.
- Background sessions (`claude --bg`) store transcripts in the normal
  `~/.claude/projects/` tree, so the filesystem walk already covers them.
- **Fork lineage is not recoverable.** `--fork-session` rewrites every UUID and
  writes no parent reference, so a fork is indistinguishable from a fresh
  session. Content-prefix matching was evaluated as a proxy and rejected: on
  real data it produced only false positives (shared `<local-command-caveat>`
  boilerplate and repeated opening prompts).
- `SessionEnd` hooks share a 1.5s budget and do not block exit — anything slow
  must set `async: true` or it gets killed mid-operation.
