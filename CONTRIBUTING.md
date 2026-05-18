# Contributing to claudectl

Thanks for your interest in contributing! This project is in active development and welcomes contributions of all kinds.

## Getting Started

```bash
git clone https://github.com/batrashubham/claudectl.git
cd claudectl
go build -o claudectl .
./claudectl --help
```

Requires Go 1.21+.

## Development Workflow

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Run `go build ./...` and `go vet ./...`
4. Run `go test ./...`
5. Open a PR against `main`

## Project Structure

```
cmd/              # CLI commands (one file per command)
internal/
  config/         # TOML config loading
  index/          # Session index from history.jsonl + filesystem
  session/        # Locate, restore, resume sessions
  sync/           # Append-only backup engine
  template/       # Session templates (save/spawn)
  tui/            # Bubble Tea terminal UI
```

## Code Style

- Standard `gofmt` formatting
- No comments unless explaining a non-obvious *why*
- Error messages: lowercase, no punctuation, include context
- Internal packages stay internal — don't export unless necessary

## What to Work On

Check [GitHub Issues](https://github.com/batrashubham/claudectl/issues) for open tasks. Good first issues are tagged `good-first-issue`.

**Areas that need help:**
- Test coverage (especially edge cases in sync and template rewriting)
- Windows support (syscall.Exec doesn't work — need os/exec fallback)
- TUI polish (rendering edge cases on different terminal sizes)
- Documentation improvements

## Running Tests

```bash
go test ./...                    # all tests
go test ./internal/template/...  # specific package
go test -v -run TestRewrite ...  # specific test
```

## Releases

Releases are automated via GitHub Actions on tag push. To cut a release:

```bash
git tag v0.X.Y
git push --tags
# GitHub Actions runs goreleaser automatically
```

## Reporting Bugs

Include:
- `claudectl --help` output (shows version)
- `claudectl status` output
- Your terminal emulator and OS
- Steps to reproduce

## Feature Requests

Open an issue with the `enhancement` label. Describe:
- The problem you're trying to solve
- Your proposed solution (if any)
- Whether you'd be willing to implement it
