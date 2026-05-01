# stash

`stash` is a terminal UI for bookmarking, browsing, previewing, and resuming local Claude Code and Codex sessions.

![stash TUI demo](demo/demo.gif)

It has three views:

- **Stash**: sessions you saved
- **History**: recent sessions
- **Active**: currently running Claude Code sessions

## Install

```sh
go install github.com/jaredgizersky/stash/cmd/stash@latest
```

Make sure your Go bin directory is on `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Use

```sh
stash        # open the TUI
stash --all  # show sessions from all projects
stash list   # print saved/recent sessions
```

Claude Code and Codex history show up automatically from local session files.

To type `stash` or `stash <name>` inside an agent session and have it save, rename, and exit that session, install a `UserPromptSubmit` hook that runs:

```sh
stash hook
```

The Claude Code plugin installs that hook:

```sh
/plugin marketplace add jaredgizersky/stash
/plugin install stash@stash
/reload-plugins
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

For one shell only:

```sh
export STASH_YOLO=1
```

## License

MIT
