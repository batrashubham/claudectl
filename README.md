# claudectl

A CLI tool for managing Claude Code sessions with long-term persistence. Syncs your session data to a git-backed backup, provides a searchable TUI, and lets you resume any session — even ones Claude has cleaned up.

## Why

Claude Code deletes session transcripts after 30 days. If you want to revisit an old conversation, search through past sessions, or keep a permanent archive — you're out of luck.

`claudectl` fixes this by:
- **Syncing** all session data to a backup directory (append-only, never deletes)
- **Git-versioning** every sync so you have full history
- **Indexing** sessions with metadata from `history.jsonl` (prompts, timestamps, projects)
- **Resuming** archived sessions by restoring them back to Claude's projects directory
- **Starter sessions** — save warm sessions as templates, spawn new ones pre-loaded with context
- **Backup & restore** — push to a git remote, restore on any machine when needed

## Install

### Go install (requires Go 1.21+)

```bash
go install github.com/batrashubham/claudectl@latest
```

### From source

```bash
git clone https://github.com/batrashubham/claudectl.git
cd claudectl
go install .
```

### Install script (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/batrashubham/claudectl/main/install.sh | sh
```

Downloads the right binary for your OS/arch and installs to `/usr/local/bin`.

## Quick Start

```bash
# Install
curl -sSL https://raw.githubusercontent.com/batrashubham/claudectl/main/install.sh | sh

# Run — works immediately with zero config
claudectl
```

First run uses sane defaults (backup at `~/.claudectl/backup/`, auto-sync on launch). No setup wizard needed.

For git remote push, cron scheduling, or custom paths, run `claudectl setup`.

## Usage

```bash
claudectl                # Launch TUI (default)
claudectl sync           # One-shot sync
claudectl sync --watch   # Continuous sync (every 5m, configurable)
claudectl list           # Plain text session list
claudectl list --json    # JSON output for scripting
claudectl resume <id>    # Resume a session directly by ID
claudectl restore        # Pull latest backup from git remote
claudectl export <id>    # Export session as readable markdown
claudectl import <file> -p <project>  # Import a .jsonl session file
claudectl dashboard      # Usage analytics (tokens, activity, projects)
claudectl status         # Quick health check (backup size, sync, cron)
claudectl template save <id> --name <name>   # Save session as template
claudectl template spawn <name> --resume     # Start new session from template
claudectl template list                      # List available templates
claudectl cron install   # Add to crontab (default: every 5 min)
claudectl cron status    # Check if cron is active
claudectl cron remove    # Remove from crontab
claudectl config         # Show current configuration
claudectl setup          # Re-run onboarding wizard
```

## TUI

Two-pane layout with a project sidebar and session list:

```
⚡ CLAUDECTL  25 sessions  ·  8 projects  ✓ synced now
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PROJECTS             │  All 8   Active 6   Archive 2
                     │
▸ All 25             │  ▸ ● api-service                          3h
  my-webapp 3        │       implement rate limiting on the...
  api-service 8      │       ⊡ 21 prompts  ◈ 3.1 MB
  infra 4            │    ● api-service                          1d
  cli-tools 5        │       fix the 409 retry logic...
                     │       ⊡ 6 prompts  ◈ 183 kB
TEMPLATES            │    ○ api-service                          3w
  ◆ warm-context     │       add circuit breaker pattern...
  ◆ api-deep-dive    │       ⊡ 15 prompts  ◈ 2.1 MB
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
↑↓ navigate  tab pane  ⏎ detail/spawn  r resume  t save template
/ search  s sync  f filter  q quit
```

### Session indicators

| Icon | Meaning |
|------|---------|
| `●` | Active — exists in `~/.claude/projects/` |
| `○` | Archived — only in backup (resumable) |
| `△` | Ghost — only in history, not resumable (hidden by default) |

### Key bindings

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate (in active pane) |
| `Tab` | Switch focus between sidebar and session list |
| `Enter` | Detail view (sessions) / Spawn (templates) |
| `r` | Resume session (restores from backup if needed) |
| `t` | Save current session as a template |
| `d` | Delete template (when focused in sidebar) |
| `/` | Full-text search across all prompts |
| `f` | Cycle filter: All → Active → Archive → Ghost |
| `s` | Sync now |
| `g/G` | Jump to top/bottom |
| `Shift+D` | Usage dashboard (stats, activity, tokens) |
| `q` | Quit |

### Sidebar

The left sidebar shows:
- **Projects** with session counts — select to filter the session list
- **Templates** — select to view details, Enter to spawn, `d` to delete

Filter counts update based on the selected project.

## Starter Sessions (Templates)

### Why templates when you have CLAUDE.md?

`CLAUDE.md` tells Claude *what to do* — conventions, patterns, rules. But it doesn't give Claude *understanding*. Every new session still spends time:

- Reading through your codebase structure
- Exploring key files and understanding relationships
- Building a mental model of your architecture
- Discovering patterns that aren't documented

A **starter template** captures a session where Claude has already done all of this. It's the difference between giving someone a map (CLAUDE.md) vs. giving them a map AND having already walked the terrain with them (template).

| | CLAUDE.md | Template |
|---|---|---|
| What it provides | Instructions & rules | Deep project understanding |
| Context on startup | ~1-2k tokens | Full conversation (thousands of tokens) |
| Knows file structure | Only if documented | Yes, from exploration |
| Knows code patterns | Only if documented | Yes, from reading actual code |
| Knows gotchas/edge cases | Only if documented | Yes, from prior discovery |
| Startup time | Seconds (but then explores) | Instant (already explored) |

**Best practice**: Use both. CLAUDE.md for rules that should always apply. Templates for warm context that skips the exploration phase.

### Usage

```bash
# Save current session as a template
claudectl template save d98e8856-... --name warm-context --trim

# Later, start a fresh session with full project context
claudectl template spawn warm-context --resume

# Or from TUI: press 't' on a session to save, Enter on template to spawn
```

Templates are project-scoped and backed up with sync. Use `--trim` to strip non-essential entries and keep the template lean.

**What `--trim` removes:** `last-prompt` (UI state), `custom-title` (cosmetic), `agent-name` (cosmetic), `queue-operation` (operational logs), `file-history-snapshot` (stale file state). **What it keeps:** all `user`, `assistant`, `attachment`, and `permission-mode` entries — the actual conversation and tool results that form Claude's understanding. If in doubt, omit `--trim` to keep everything.

## Dashboard & Analytics

Press `Shift+D` in the TUI (or run `claudectl dashboard`) to see:

- **Session stats** — total count, prompts, estimated tokens, longest session
- **Weekly activity** — bar chart of prompts per week, peak day/hour
- **Project breakdown** — sessions, prompts, and last active per project
- **Template stats** — count and names

All computed locally from your `history.jsonl` — no telemetry, no cloud.

## Import & Export

Share sessions with teammates:

```bash
# Export a session as readable markdown
claudectl export <session-id> -o session.md

# Import a colleague's session file
claudectl import their-session.jsonl --project /path/to/project --resume
```

Import rewrites session IDs so there are no conflicts with your own sessions.

## Backup & Restore

Push your backup to a git remote for safekeeping. Restore on another machine when needed:

```bash
# Back up (happens automatically, or manually)
claudectl sync

# Restore from remote on a different machine
claudectl restore

# Browse and resume any session
claudectl
```

This is backup/restore, not real-time sync — you control when to push and when to pull.

## Configuration

Config lives at `~/.claudectl/config.toml`:

```toml
backup_dir = "~/.claudectl/backup"
claude_dir = "~/.claude"
sync_on_start = true
git_auto_commit = true
git_remote = "git@github.com:you/claude-backup.git"
git_push = true
```

| Field | Default | Description |
|-------|---------|-------------|
| `backup_dir` | `~/.claudectl/backup` | Where to store the backup (git repo) |
| `claude_dir` | `~/.claude` | Claude Code's config directory |
| `sync_on_start` | `true` | Auto-sync when TUI launches |
| `git_auto_commit` | `true` | Commit after each sync |
| `git_remote` | `""` | Git remote URL for pushing backups |
| `git_push` | `false` | Push to remote after each commit |

Templates are stored at `<backup_dir>/templates/` — automatically git-versioned with the rest of your backup.

## How It Works

### Sync

`claudectl` walks `~/.claude/projects/` and copies session files to the backup directory:
- **New files**: copied immediately
- **Growing files**: overwritten (sessions only grow via append)
- **Never deletes**: if a session is removed from source, the backup keeps it

After copying, it commits to git (and optionally pushes to remote).

### Index

Sessions are indexed by merging two sources:
1. **`~/.claude/history.jsonl`** — every prompt you've typed, linked to session IDs
2. **Filesystem walk** — catches sessions not in history

Both the live and backup copies of `history.jsonl` are merged and deduplicated, so even if Claude cleans the live file, your backup preserves all metadata.

### Resume

When you resume an archived session:
1. The `.jsonl` file is copied from backup back to `~/.claude/projects/`
2. Any subagent/tool-result subdirectories are also restored
3. `claude --resume <session-id>` is exec'd

## Data Layout

```
~/.claudectl/
├── config.toml
└── backup/                   # git repo (synced + pushed)
    ├── .gitattributes        # merge=union for history.jsonl
    ├── history.jsonl
    ├── templates/            # session templates (project-scoped)
    │   └── -Users-you-code-project-a/
    │       └── warm-context/
    │           ├── meta.json
    │           └── session.jsonl
    └── projects/
        ├── -Users-you-code-project-a/
        │   ├── abc123.jsonl
        │   └── abc123/
        │       └── subagents/
        └── -Users-you-code-project-b/
            └── def456.jsonl
```

## Security

**Your session transcripts may contain secrets.** Claude Code sessions can include API keys, passwords, tokens, and other sensitive data that appeared in tool results or conversation context.

- **Always use a private git remote** for your backup repo
- **Never push to a public repository** — your entire Claude Code history would be exposed
- claudectl does not encrypt data at rest or in transit (beyond what git/SSH provides)
- Consider using a dedicated private repo (not your main code repo) for backups

If you accidentally push to a public repo, rotate any credentials that may have appeared in your sessions immediately.

## Requirements

- Claude Code CLI installed (`claude` in PATH)
- Go 1.21+ (for building from source)
- Git (for backup versioning)
- macOS or Linux

## License

MIT
