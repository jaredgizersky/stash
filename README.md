# stash

`stash` is a small CLI/TUI for bookmarking, browsing, previewing, and resuming Claude Code and Codex sessions.

It gives you one terminal UI with three views:

- Stash: sessions you explicitly saved
- History: recent Claude Code and Codex sessions
- Active: currently running Claude Code sessions

You can name sessions, save them for later, filter by project, preview transcripts, and jump back into the right tool with the right working directory.

## Install

Install the latest published version with Go:

```sh
go install github.com/jaredgizersky/stash/cmd/stash@latest
```

Or build from a local checkout:

```sh
go install ./cmd/stash
```

That installs `stash` into your Go binary directory, usually `$(go env GOPATH)/bin`. Make sure that directory is on your `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

For local development, you can also build a repo-local binary:

```sh
go build -o stash ./cmd/stash
```

## Usage

Open the TUI in the current project:

```sh
stash
```

Show sessions from all projects:

```sh
stash --all
```

List stashed sessions without opening the TUI:

```sh
stash list
stash list --all
```

From inside a Claude Code session, save the current session manually:

```sh
!stash save "name for this session"
```

## Claude Code Hook Setup

This repo includes a Claude Code plugin hook at `hooks/hooks.json`. The hook watches submitted prompts for:

```text
stash
stash <name>
```

When triggered, the hook blocks the prompt from reaching Claude, saves the session into `~/.stash/index.json`, updates the Claude session title when a name is provided, and exits the current Claude session so you can pick it back up later from `stash`.

The Claude Code plugin only installs the hook configuration. Install the `stash` CLI first and make sure it is on `PATH`, because the hook command is:

```sh
stash hook
```

### Install From GitHub

After this repository is public, add it as a Claude Code plugin marketplace:

```sh
/plugin marketplace add jaredgizersky/stash
/plugin install stash@stash
/reload-plugins
```

You can also do the same non-interactively:

```sh
claude plugin marketplace add jaredgizersky/stash
claude plugin install stash@stash
```

Claude Code installs plugins to user scope by default. To share the marketplace or plugin through a project repository, install with project scope:

```sh
claude plugin marketplace add jaredgizersky/stash --scope project
claude plugin install stash@stash --scope project
```

The marketplace is defined at `.claude-plugin/marketplace.json`. Its plugin source is a relative path to this repository root, which works when the marketplace is added from GitHub.

### Local Plugin Development

To test the plugin directly from a checkout:

```sh
claude --plugin-dir /path/to/stash
```

Or add the local checkout as a marketplace:

```sh
/plugin marketplace add /path/to/stash
/plugin install stash@stash
```

### Updates

Update the CLI with:

```sh
go install github.com/jaredgizersky/stash/cmd/stash@latest
```

Update the Claude Code plugin with:

```sh
/plugin update stash
```

## Codex Support

`stash` also reads Codex sessions from `~/.codex/state_5.sqlite` and shows top-level CLI/exec/VS Code threads in the same TUI.

Codex sessions can be previewed and resumed:

```sh
codex resume <session-id>
```

The TUI routes this automatically when you resume a Codex session. Codex message counts and transcript previews depend on the current Codex local state format.

## Data Paths

`stash` reads and writes local files only:

- `~/.stash/index.json`: sessions explicitly saved with `stash`
- `~/.stash/session-cache.json`: Claude session metadata cache
- `~/.stash/codex-cache.json`: Codex message-count cache
- `~/.claude/projects/*/*.jsonl`: Claude Code session transcripts
- `~/.claude/sessions/*.json`: active Claude Code session state
- `~/.codex/state_5.sqlite`: Codex thread index
- Codex rollout JSONL paths referenced by the SQLite database

Deleting files under `~/.stash` removes stash metadata and caches, but does not delete Claude Code or Codex sessions.

## Keybindings

- `left` / `right`: switch tabs
- `s` / `h` / `a`: jump to Stash, History, or Active
- `j` / `k` or `down` / `up`: move selection
- `g` / `G`: jump to top or bottom
- `enter`: preview transcript
- `r`: resume selected session
- `S`: add a History or Active item to the stash
- `d`: remove a Stash item from the stash
- `tab`: toggle current-directory vs all-project scope
- `n`: toggle named-only sessions
- `/`: search/filter
- `q` / `esc`: quit or leave preview

## Safety And Privacy

`stash` is local-first. It does not upload transcripts, prompts, session metadata, or stash entries anywhere.

It does read local Claude Code and Codex history files, so transcript previews may show sensitive prompts, tool outputs, file paths, command output, or secrets that already exist in those local session logs. Treat terminal output and screenshots of the TUI with the same care you would give the underlying transcripts.

The Claude Code hook intentionally intercepts prompts that begin with `stash`. Those prompts are blocked from reaching Claude and are used as local session-management commands instead.

By default, resuming a Claude Code session runs `claude --resume <session-id>`. To also pass Claude's `--dangerously-skip-permissions` flag, set:

```sh
export STASH_CLAUDE_RESUME_DANGEROUSLY_SKIP_PERMISSIONS=1
```

## Compatibility

Claude Code and Codex store local sessions in implementation-specific formats. Parser support is pinned with versioned fixtures so updates can add support for new formats without silently dropping old ones.

Current compatibility fixtures cover:

- Claude Code `2.1.108` JSONL transcripts
- Codex CLI `0.107.0` `response_item` rollout JSONL

## Platform Caveats

`stash` currently assumes Unix-like local paths and process behavior:

- Claude Code data under `~/.claude`
- Codex data under `~/.codex`
- POSIX process checks/signals for active sessions and hook-triggered exits
- A shell available for delayed session interrupt handling

It is primarily intended for macOS/Linux-style developer environments. Windows support is not currently guaranteed.

## License

MIT. See [LICENSE](LICENSE).
