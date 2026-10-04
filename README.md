# claudectl

Back up, search, and resume your coding-agent sessions — **Claude Code, Codex, Gemini CLI, opencode and Qwen Code** — across every machine you work on. Syncs session data to a git-backed backup, provides a searchable TUI, and resumes any session in the agent that created it, even ones the agent has deleted or that started on another machine.

## Why

Coding agents keep their transcripts on one machine, in their own formats, and clean them up on their own schedule (Claude Code deletes them after 30 days). That leaves you with an unsearchable pile of files per tool, per machine, that vanish if a disk dies — no way to browse past sessions, search across agents, pick up on your desktop what you started on your laptop, or skip the codebase warm-up on every new session.

`claudectl` fixes this by:
- **Syncing** every agent's sessions to a backup directory (append-only, never deletes)
- **Git-versioning** every sync so you have full history
- **Indexing** sessions from all agents into one searchable list (prompts, timestamps, projects)
- **Resuming** any session — restoring it into the agent's own directory first if needed
- **Multiple machines** — each machine syncs into its own subtree, so many machines share one remote safely
- **Workspaces** — separate backup profiles (e.g. work vs personal) that never leak into each other's remote
- **Starter sessions** — save warm Claude Code sessions as templates, spawn new ones pre-loaded with context

## Supported agents

| Agent | Data it reads | Resume command | Notes |
|-------|---------------|----------------|-------|
| Claude Code | `~/.claude` (`CLAUDE_CONFIG_DIR`) | `claude --resume <id>` | Templates, import, SessionEnd hook |
| Codex | `~/.codex` (`CODEX_HOME`) | `codex resume <id>` | Archived rollouts restore unarchived |
| Gemini CLI | `~/.gemini` (`GEMINI_CLI_HOME`) | `gemini --resume <id>` | Legacy `.json` chats and hash-keyed dirs read too |
| opencode | `~/.local/share/opencode` (`XDG_DATA_HOME`, `OPENCODE_DB`) | `opencode --session <id>` | SQLite: backed up via `opencode export`, restored via `opencode import` |
| Qwen Code | `~/.qwen` (`QWEN_HOME`) | `qwen --resume <id>` | Format verified from source, not yet against a live install |

Each agent is picked up automatically when its data directory exists. Only transcripts and prompt history are backed up — never credentials (`auth.json`, `oauth_creds.json`) or settings.

## Install

### Go install (requires Go 1.25+)

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
claudectl sync           # One-shot sync of every enabled agent
claudectl sync --watch   # Continuous sync (every 5m, configurable)
claudectl list           # Plain text session list, all agents and machines
claudectl list --agent codex --machine desktop --search "rate limit"
claudectl list --json    # JSON output for scripting
claudectl resume <id>    # Resume by ID or unique prefix (e.g. 01a1087, codex:01a1)
claudectl resume <id> --print   # Restore and print the command instead of running it
claudectl restore        # Pull the backup (incl. other machines' sessions) from the remote
claudectl export <id>    # Export a session (prompts + replies) as markdown
claudectl import <file> -p <project>  # Import a Claude Code .jsonl session
claudectl dashboard      # Usage analytics (tokens, activity, projects)
claudectl status         # Health check: agents, machines, backup, hook, cron
claudectl machine        # List machines sharing the backup
claudectl machine rename <name>       # Rename this machine
claudectl workspace list              # List workspaces
claudectl workspace add work --home claude=~/.claude-work --remote <url> --push
claudectl --workspace work sync       # Any command, in another workspace
claudectl gc             # Reclaim disk space in the backup repo
claudectl gc --keep-days 30   # Squash history older than 30 days
claudectl gc --squash    # Compact all history into one commit (max reclaim)
claudectl template save <id> --name <name>   # Save session as template
claudectl template spawn <name> --resume     # Start new session from template
claudectl template list                      # List available templates
claudectl hook install   # Back up when a Claude session ends (recommended)
claudectl hook status    # Check if the hook is active
claudectl hook remove    # Remove the hook
claudectl cron install   # Alternative: poll on a timer (default: every 5 min)
claudectl cron status    # Check if cron is active
claudectl cron remove    # Remove from crontab
claudectl config         # Show the effective configuration
claudectl setup          # Re-run onboarding wizard
claudectl completion zsh # Shell completion (bash, zsh, fish) incl. session IDs
claudectl --version
```

## TUI

Two-pane layout with a sidebar (projects, agents, machines, templates) and session list:

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
| `●` | Active — exists in the agent's own data directory on this machine |
| `○` | Archived — only in the backup, from this or another machine (resumable) |
| `△` | Ghost — only in history, not resumable (hidden by default) |

### Key bindings

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate (in active pane) |
| `Tab` | Switch focus between sidebar and session list |
| `Enter` | Detail view (sessions) / Spawn (templates) |
| `r` | Resume session in its agent (restores from backup if needed) |
| `t` | Save current session as a template |
| `d` | Delete template (when focused in sidebar) |
| `/` | Full-text search across all prompts, agents and machines |
| `f` | Cycle filter: All → Active → Archive → Ghost |
| `s` | Sync now |
| `g/G` | Jump to top/bottom |
| `Shift+D` | Usage dashboard (stats, activity, tokens) |
| `q` | Quit |

### Sidebar

The left sidebar shows:
- **Projects** with session counts — select to filter the session list
- **Agents** — shown when sessions come from more than one agent
- **Machines** — shown when the backup holds sessions from more than one machine
- **Templates** — select to view details, Enter to spawn, `d` to delete

When there's more than one agent or machine, each session row also carries an agent badge and an `@machine` tag.

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

Templates currently support Claude Code sessions.

### Usage

```bash
# Save current session as a template
claudectl template save d98e8856-... --name warm-context --trim

# Later, start a fresh session with full project context
claudectl template spawn warm-context --resume

# Or from TUI: press 't' on a session to save, Enter on template to spawn
```

Templates are project-scoped and backed up with sync. Use `--trim` to strip non-essential entries and keep the template lean.

**What `--trim` removes** — UI state, cosmetics, and operational logs with no conversation value:
`last-prompt`, `custom-title`, `ai-title`, `agent-name`, `mode`, `queue-operation`, `progress` (hook logs), `frame-link`, `file-history-snapshot`, `file-history-delta`.

**What it keeps** — everything that carries context or affects resume behaviour:
`user`, `assistant`, `attachment`, `permission-mode` (the conversation itself), plus `system`, `pr-link` (used by `claude --from-pr`), and `agent-setting` (determines which agent restores on resume).

Unknown entry types are always kept, so templates stay safe as Claude Code adds new ones. If in doubt, omit `--trim` to keep everything.

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
# Export any agent's session as readable markdown (your prompts, the agent's
# replies, and the tools it called)
claudectl export <session-id> -o session.md
claudectl export <session-id> --prompts-only

# Import a colleague's session file
claudectl import their-session.jsonl --project /path/to/project --resume
```

Import rewrites session IDs so there are no conflicts with your own sessions.

## Multiple Machines

Every machine syncs into its own subtree of the backup (`machines/<name>/`), so any number of machines can push to one private git remote without conflicts. Before pushing, `sync` rebases onto whatever the other machines have pushed.

```bash
# On each machine: point at the same private remote
claudectl setup                      # asks for a machine name and the remote

# See what the other machines have pushed
claudectl restore                    # pull (clones on first use)
claudectl list --machine desktop     # their sessions, alongside yours
claudectl machine                    # who's in the backup, and when they last synced

# Pick up on this machine a session started on another
claudectl resume 01a1087
```

The machine name defaults to the hostname and is saved in `~/.claudectl/machine` on first use, so a hostname change doesn't start a second subtree. Rename with `claudectl machine rename <name>` or set `machine = "..."` in the config.

**Project paths across machines.** When you resume a session from another machine, its project path is translated to this one. By default the other machine's home directory is swapped for yours (`/Users/me/code/api` → `/home/me/code/api`). For anything else, add `[[path_map]]` rules (see Configuration). The session is restored where the agent will look for that path, then the agent runs from it.

Nothing is ever written into your agents' directories until you resume a session — `restore` only updates the backup.

## Workspaces

A workspace is an independent backup profile with its own backup repo, git remote, and agent data directories. Use one per account or trust boundary. For example, a work Claude Code account kept in `~/.claude-work` should back up to the company remote, never to your personal one:

```bash
claudectl workspace add work \
  --home claude=~/.claude-work \
  --remote git@github.com:acme/agent-backup.git --push

claudectl --workspace work sync      # one command
export CLAUDECTL_WORKSPACE=work      # one shell
claudectl workspace use work         # the default from now on
```

Named workspaces inherit **nothing** from the top-level config (which is the `default` workspace): no remote, no agent directories. So a misconfiguration can't push sessions into the wrong remote. Each workspace gets its own hook and cron job, pinned to it with an explicit `--workspace`, so switching `workspace use` later never retargets them.

## Automatic Backup

Two ways to keep the backup current without thinking about it:

```bash
claudectl hook install    # event-driven: backs up when a session ends
claudectl cron install    # timer-driven: backs up every 5 minutes
```

Every sync backs up all enabled agents. The hook fires on Claude Code session ends; use cron if you mostly use other agents.

**The hook is usually the better choice.** It runs the moment a Claude session ends, so a session is backed up as soon as it's finished rather than up to five minutes later, and nothing runs while you're idle.

It's installed into your Claude Code `settings.json` as a `SessionEnd` hook and runs in the background — Claude never waits on it. (`SessionEnd` hooks share a 1.5-second budget and don't block exit, so a synchronous git push would be killed part-way; running detached avoids that.) Existing hooks and settings are preserved; only claudectl's own entry is added or removed.

Takes effect in new Claude sessions. Either mechanism is enough on its own — the sync lock means running both is harmless, just redundant. The hook waits for a sync that's already running instead of skipping, so sessions ending together are all captured.

## Managing Backup Size

Sessions are append-only and grow over time. Since each sync re-commits the grown file, the backup's git history accumulates old versions of large session files. Over months, the `.git` directory can grow significantly.

Run `claudectl gc` periodically to reclaim space:

```bash
claudectl gc                 # git gc --aggressive (safe, keeps all history)
claudectl gc --keep-days 30  # squash history older than 30 days
claudectl gc --squash        # collapse ALL history into one commit (max reclaim)
```

- **`gc`** compresses git's object store without losing anything.
- **`--keep-days N`** keeps the last N days of commit history (the time-machine view) and squashes everything older into a single base commit. Best balance of space vs. history.
- **`--squash`** discards all commit history, keeping only current sessions. Maximum reclaim.

Your current sessions are always preserved regardless of which option you use. In testing, a 1.3 GB backup compressed to 265 MB with plain `gc`.

With a git remote configured, `--squash` and `--keep-days` are refused: rewriting history would stop the backup from pushing, for every machine sharing it. Plain `gc` is always safe. To shrink a shared backup, create a fresh empty remote, point `git_remote` at it, and squash before the first push.

## Configuration

Config lives at `~/.claudectl/config.toml` (`claudectl config path`; `CLAUDECTL_HOME` relocates `~/.claudectl`):

```toml
backup_dir = "~/.claudectl/backup"
sync_on_start = true
git_auto_commit = true
git_remote = "git@github.com:you/agent-backup.git"
git_push = true
# machine = "laptop"            # defaults to the saved hostname-derived name

# Per-agent overrides. Agents are on whenever their data dir exists.
[harnesses.codex]
home = "~/.codex"
[harnesses.gemini]
enabled = false                 # never back up Gemini CLI
[harnesses.opencode]
bin = "/opt/opencode/bin/opencode"

# Map project paths recorded on other machines to this one.
[[path_map]]
from = "/Users/me/code"
to = "/home/me/src"

# Independent backup profiles.
[workspaces.work]
git_remote = "git@github.com:acme/agent-backup.git"
git_push = true
[workspaces.work.harnesses.claude]
home = "~/.claude-work"
```

| Field | Default | Description |
|-------|---------|-------------|
| `backup_dir` | `~/.claudectl/backup` | Where to store the backup (git repo) |
| `claude_dir` | `~/.claude` | Shorthand for `[harnesses.claude] home` (respects `CLAUDE_CONFIG_DIR`) |
| `sync_on_start` | `true` | Auto-sync when TUI launches |
| `git_auto_commit` | `true` | Commit after each sync |
| `git_remote` | `""` | Git remote URL for pushing backups |
| `git_push` | `false` | Push to remote after each commit |
| `machine` | hostname | This machine's name in the backup |
| `default_workspace` | `default` | Workspace used when none is given |
| `[harnesses.<agent>]` | auto | `enabled`, `home`, `bin` per agent (`claude`, `codex`, `gemini`, `opencode`, `qwen`) |
| `[[path_map]]` | — | `from`/`to` prefixes for translating other machines' project paths |
| `[workspaces.<name>]` | — | `backup_dir`, `git_remote`, `git_push`, `git_auto_commit`, `sync_on_start`, `harnesses` |

Templates are stored at `<backup_dir>/templates/` — automatically git-versioned with the rest of your backup.

## How It Works

### Sync

For every enabled agent, `claudectl` copies its transcripts and prompt history into `machines/<this machine>/<agent>/` in the backup, mirroring the agent's own layout:
- **New files**: copied immediately
- **Growing files**: overwritten (JSONL transcripts only grow via append)
- **Rewritten documents** (Gemini's legacy JSON chats): newer version wins
- **Databases** (opencode): one JSON snapshot per changed session via `opencode export`, so the backup diffs cleanly in git
- **Never deletes**: if a session is removed from source, the backup keeps it

Copies are atomic (temp file + rename), and a kernel lock (`<backup_dir>.lock`), held across copy, commit and push, keeps concurrent syncs from racing and is released even if a sync crashes.

Backups can come from other machines, so everything read from them is treated as untrusted: session IDs that could escape a directory or be parsed as a CLI flag are ignored, and restores never write outside the agent's own directory. After copying, it commits to git and, if enabled, rebases onto the remote and pushes.

Backups made before multi-machine support (Claude data at the backup root) are moved under this machine's subtree automatically on the first sync, if they were never pushed; nothing is dropped. A legacy backup that is already on a remote may hold any machine's sessions, so it stays where it is and shows up as machine `legacy`.

### Index

Sessions are indexed from every agent's live directory plus every machine's subtree in the backup, merged by agent and session ID. Prompt history from all copies is unioned, so even if an agent prunes its own history, the backup preserves it.

### Resume

When you resume a session that isn't live on this machine:
1. Its project path is translated for this machine (path_map, then home-dir swap)
2. Its files are restored to where the agent looks for that project (or imported, for opencode)
3. The agent's resume command is exec'd from the project directory

## Data Layout

```
~/.claudectl/
├── config.toml
├── machine                       # this machine's name
├── backup.lock
└── backup/                       # git repo (synced + pushed)
    ├── .gitattributes            # merge=union for history.jsonl
    ├── templates/                # Claude Code session templates (shared)
    └── machines/
        ├── laptop/
        │   ├── machine.json      # os, home dir, agent dirs
        │   ├── claude/
        │   │   ├── history.jsonl
        │   │   └── projects/-Users-you-code-app/<id>.jsonl
        │   ├── codex/
        │   │   └── sessions/2026/10/04/rollout-...-<id>.jsonl
        │   ├── gemini/
        │   │   └── tmp/app/chats/session-...-<id8>.jsonl
        │   └── opencode/
        │       └── sessions/ses_<id>.json
        └── desktop/
            └── ...
```

## Security

**Your session transcripts may contain secrets.** Agent sessions can include API keys, passwords, tokens, and other sensitive data that appeared in tool results or conversation context.

- **Always use a private git remote** for your backup repo
- **Never push to a public repository** — your entire agent history would be exposed
- Use a separate workspace (and remote) for each account or employer
- claudectl does not encrypt data at rest or in transit (beyond what git/SSH provides)
- Consider using a dedicated private repo (not your main code repo) for backups

If you accidentally push to a public repo, rotate any credentials that may have appeared in your sessions immediately.

## Requirements

- At least one supported agent installed (its CLI in `PATH` to resume; opencode's to back up)
- Go 1.25+ (for building from source)
- Git (for backup versioning)
- macOS or Linux

## License

MIT
