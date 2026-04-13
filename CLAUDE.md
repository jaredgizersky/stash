# stash

CLI/TUI for managing Claude Code sessions. Lets you name, bookmark ("stash"), browse, preview transcripts, and resume sessions.

## Architecture

```
cmd/stash/main.go          CLI entry point (cobra). Subcommands: (default TUI), save, hook, list
internal/claude/sessions.go Session loading from ~/.claude/projects/*/*.jsonl. Parallel scanning with file-level cache (~/.stash/session-cache.json)
internal/claude/cache.go    mtime+size-based cache for scanned JSONL metadata. Bump cacheVersion const if Session struct changes
internal/claude/active.go   Reads ~/.claude/sessions/*.json for live processes (PID, alive check, cwd)
internal/store/store.go     Stash index (~/.stash/index.json) — the ledger of explicitly stashed sessions
internal/tui/tui.go         Bubble Tea TUI. Three tabs (Stash/History/Active), unified update handler, transcript preview
```

## How Claude sessions work

- Sessions stored as JSONL at `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
- Encoding: absolute path with every non-alnum char replaced by `-` (lossy — cannot be reversed)
- Some project dirs have `sessions-index.json` but it's incomplete (only covers ~5% of sessions). We ignore it and scan all JONLs directly.
- Session names set via `/rename` or `claude -n` appear as `{"type": "agent-name", "agentName": "..."}` entries in the JSONL
- Active sessions: `~/.claude/sessions/<pid>.json` — contains pid, sessionId, cwd, name. File keyed by PID.
- Resume: `claude --resume <session-id>` in the correct cwd

## Data flow

1. `LoadAllSessions()` scans all JSONL files across all project dirs, parallelized, cached by mtime+size
2. `LoadActiveSessions()` reads `~/.claude/sessions/*.json`, checks PIDs alive via `kill -0`
3. `LinkTranscripts()` matches active sessions to their transcript Session objects
4. Stash index (`~/.stash/index.json`) marks which sessions are "stashed" — populated by the hook or `stash save`
5. TUI enriches sessions with stash names, renders three tabs

## The hook

Distributed as a Claude Code plugin via `hooks/hooks.json`. The `UserPromptSubmit` hook calls `stash hook` (must be on PATH). When user types `stash <name>`:

1. Hook blocks the prompt (Claude never sees it)
2. Sets `sessionTitle` natively via hook response (Claude writes `agent-name` to JSONL)  
3. Writes session to `~/.stash/index.json`
4. Finds Claude's PID from `~/.claude/sessions/*.json` and schedules `sleep 0.5 && kill -INT <pid>` to exit the session

## Build & install

```
go install ./cmd/stash/                  # puts binary on GOPATH/bin
claude --plugin-dir /path/to/stash       # test the plugin locally
```

## Session struct

Single `Name` field (best name wins: stash name > native agent-name > empty). `Stashed` bool set at runtime from the index. `StashName`/`NativeName` were consolidated — don't re-split them.

## Cache invalidation

Cache keys on JSONL file path + mtime + size. If the Session struct shape changes (new fields to extract), bump `cacheVersion` in cache.go or delete `~/.stash/session-cache.json`.

## TUI keybindings

- `←`/`→` or `s`/`h`/`a`: switch tabs (Stash, History, Active)
- `j`/`k` or `↑`/`↓`: navigate
- `enter`: preview transcript
- `r`: resume session (execs `claude --resume`)
- `d`: unstash (Stash tab only)
- `tab`: toggle cwd/all scope
- `n`: toggle named-only filter
- `/`: search filter
- `q`/`esc`: quit

## Future: Codex support

Not yet implemented. Codex sessions would need a separate loader but could share the same TUI and stash index.
