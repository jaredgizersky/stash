# stash

`stash` is a terminal UI for bookmarking, browsing, previewing, and resuming local Claude Code and Codex sessions.

It has three views:

- **Stash**: sessions you saved
- **History**: recent Claude Code and Codex sessions
- **Active**: currently running Claude Code sessions

## Install

```sh
go install github.com/jaredgizersky/stash/cmd/stash@latest
```

Make sure your Go bin directory is on `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

From a checkout:

```sh
go install ./cmd/stash
```

## Use

```sh
stash          # open the TUI for the current project
stash --all    # show sessions from all projects
stash list     # list sessions without opening the TUI
```

Inside Claude Code:

```sh
!stash save "name"
```

## Claude Code Plugin

The plugin lets you type `stash` or `stash <name>` inside Claude Code. The prompt is intercepted locally, the session is saved to `~/.stash/index.json`, and the Claude session exits so you can resume it later from `stash`.

Install the CLI first; the plugin hook runs:

```sh
stash hook
```

After this repo is public, install the plugin from GitHub:

```sh
/plugin marketplace add jaredgizersky/stash
/plugin install stash@stash
/reload-plugins
```

CLI equivalent:

```sh
claude plugin marketplace add jaredgizersky/stash
claude plugin install stash@stash
```

For local development:

```sh
claude --plugin-dir /path/to/stash
```

Update later with:

```sh
go install github.com/jaredgizersky/stash/cmd/stash@latest
/plugin update stash
```

## Keys

- `left` / `right`: switch tabs
- `j` / `k`: move
- `enter`: preview
- `r`: resume
- `S`: add to stash
- `d`: remove from stash
- `/`: filter
- `tab`: current project / all projects
- `n`: named only
- `q` / `esc`: quit or leave preview

## Config

By default, `stash` resumes sessions without bypassing tool permissions or sandboxing.

To resume Claude with `--dangerously-skip-permissions` and Codex with `--yolo`, create `~/.stash/config.toml`:

```toml
dangerously_skip_permissions = true
```

For a one-off shell:

```sh
export STASH_YOLO=1
```

## Data

`stash` is local-only. It reads Claude Code and Codex session files and writes its own metadata under `~/.stash`.

- `~/.stash/index.json`: saved sessions
- `~/.stash/config.toml`: optional config
- `~/.stash/session-cache.json`: Claude metadata cache
- `~/.stash/codex-cache.json`: Codex message-count cache
- `~/.claude/projects/*/*.jsonl`: Claude Code transcripts
- `~/.claude/sessions/*.json`: active Claude Code sessions
- `~/.codex/state_5.sqlite`: Codex thread index

Deleting `~/.stash` removes stash metadata and caches, not Claude Code or Codex sessions.

Transcript previews may contain sensitive prompts, file paths, command output, or secrets already present in your local session logs.

## Compatibility

Claude Code and Codex session formats are private implementation details, so parser support is pinned with versioned fixtures.

Current fixtures cover:

- Claude Code `2.1.108` JSONL transcripts
- Codex CLI `0.107.0` `response_item` rollout JSONL

`stash` currently targets macOS/Linux-style environments.

## License

MIT
