# claudectl

A CLI tool for managing Claude Code sessions with long-term persistence. Syncs your session data to a git-backed backup, provides a searchable TUI, and lets you resume any session — even ones Claude has cleaned up.

## Why

Claude Code deletes session transcripts after 30 days. If you want to revisit an old conversation, search through past sessions, or keep a permanent archive — you're out of luck.

`claudectl` fixes this by:
- **Syncing** all session data to a backup directory (append-only, never deletes)
- **Git-versioning** every sync so you have full history
- **Indexing** sessions with metadata from `history.jsonl` (prompts, timestamps, projects)
- **Resuming** archived sessions by restoring them back to Claude's projects directory

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

### Homebrew (coming soon)

```bash
brew install batrashubham/tap/claudectl
```

## Quick Start

Run `claudectl` for the first time and it walks you through setup:

```
$ claudectl

⚡ Welcome to claudectl
─────────────────────────────────────
Let's set up session backup for Claude Code.

Backup directory [~/.claudectl/backup]:
✓ Backup directory: /home/user/.claudectl/backup

Git remote URL (blank to skip): git@github.com:you/claude-backup.git
✓ Remote configured

Install cron job? [Y/n]: y
Sync interval in minutes [5]:
✓ Cron installed: syncing every 5 minutes

✓ Config saved: ~/.claudectl/config.toml

Run initial sync now? [Y/n]: y
Done: 295 new, 0 updated (87 MB)
─────────────────────────────────────
Setup complete! Run 'claudectl' to launch the TUI.
```

After setup, just run `claudectl` to open the TUI.

## Usage

```bash
claudectl                # Launch TUI (default)
claudectl sync           # One-shot sync
claudectl sync --watch   # Continuous sync (every 5m, configurable)
claudectl list           # Plain text session list
claudectl list --json    # JSON output for scripting
claudectl resume <id>    # Resume a session directly by ID
claudectl cron install   # Add to crontab (default: every 5 min)
claudectl cron status    # Check if cron is active
claudectl cron remove    # Remove from crontab
claudectl config         # Show current configuration
claudectl setup          # Re-run onboarding wizard
```

## TUI

The TUI shows all your sessions — active, archived, and ghost (history-only):

```
⚡ CLAUDECTL  12 sessions  ·  4 projects  ✓ synced now

 All 12   Active 8   Archive 4
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
▸ ● my-webapp                                            6m
     add dark mode support to the settings page
     ⊡ 15 prompts  ◈ 2.1 MB
  ● api-service                                          3h
     implement rate limiting on the /users endpoint...
     ⊡ 21 prompts  ◈ 3.1 MB
  ○ infra-migration                                      3w
     help me migrate from EC2 to ECS Fargate...
     ⊡ 40 prompts  ◈ 3.2 MB
  △ old-prototype                                       2mo
     scaffold the initial project structure...
     ⊡ 4 prompts
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
↑↓ navigate   ⏎ detail   r resume   / search   s sync   f filter   q quit
```

### Session indicators

| Icon | Meaning |
|------|---------|
| `●` | Active — exists in `~/.claude/projects/` |
| `○` | Archived — only in backup (resumable) |
| `△` | Ghost — only in history, file was deleted before backup (not resumable) |

### Key bindings

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate |
| `Enter` | Detail view (split panel: metadata + conversation) |
| `r` | Resume session (restores from backup if needed) |
| `/` | Search by project name or prompt text |
| `f` or `Tab` | Cycle filter: All → Active → Archive |
| `s` | Sync now |
| `g/G` | Jump to top/bottom |
| `q` | Quit |

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
└── backup/               # git repo
    ├── history.jsonl
    └── projects/
        ├── -Users-you-code-project-a/
        │   ├── abc123.jsonl
        │   └── abc123/
        │       └── subagents/
        └── -Users-you-code-project-b/
            └── def456.jsonl
```

## Requirements

- Claude Code CLI installed (`claude` in PATH)
- Go 1.21+ (for building from source)
- Git (for backup versioning)
- macOS or Linux

## License

MIT
