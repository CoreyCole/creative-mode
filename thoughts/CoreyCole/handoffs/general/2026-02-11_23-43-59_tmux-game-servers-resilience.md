---
date: 2026-02-11T23:43:59-08:00
researcher: CoreyCole
git_commit: 4c2402934a96e58aa1103d146f3ecb21cf0a8e32
branch: main
repository: creative-mode
topic: "Tmux-Based Game Servers + Dev Server for Inner Claude"
tags: [implementation, game-servers, tmux, resilience, claude-orchestrator]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Tmux-Based Game Servers + Resilience Hardening

## Task(s)

### 1. Tmux-Based Game Servers (COMPLETED)
Replaced `exec.Cmd` child-process game servers with tmux sessions so they survive harness restarts. Added dev server (cargo watch) that runs alongside Claude editing sessions to provide live feedback via BRP queries.

### 2. Resilience Hardening (COMPLETED)
Audited and fixed all identified tmux session leak scenarios. Added periodic reaper, targeted disconnect, recovery validation, and error-path cleanup.

## Critical References
- Implementation plan was provided at session start as inline text (not a file). It covered 7 steps across 7 files.
- `harness/CLAUDE.md` — now contains full "Game Servers (tmux-based)" section documenting architecture, lifecycle, and key methods.

## Recent changes

### Core refactor: game servers in tmux
- `harness/internal/world/ports.go:50-56` — Added `MarkInUse(port)` for recovery
- `harness/internal/world/game_server.go` — Complete rewrite. Replaced `exec.Cmd` with tmux sessions. Dropped refcounting (`refCount`, `Disconnect`, `stopAfterDelay`, crash monitor goroutine). Removed `jsonlLineWriter` (replaced by `tmux pipe-pane`). Added `GameServerMode` (prod/dev), `ConnectDev()`, `Recover()`, `RecoveredServers()`, `ReapOrphans()`, `Disconnect()`. Liveness checks on `GetServer()`.
- `harness/internal/tmux/session.go:29-53` — Added `extraEnv ...string` variadic param to `Create()` for passing dev server ports to Claude sessions.
- `harness/internal/claude/claude.go:75-86` — `HandlePrompt` now starts dev server via `ConnectDev()` before creating Claude tmux session, passes `CM_GAME_PORT`/`CM_BRP_PORT` as extra env vars.
- `harness/internal/claude/claude.go:113-121` — `BuildCheckpoint` now uses targeted `Disconnect(worldID, cpID)` instead of `StopByWorldExcept(worldID, "")` to avoid killing the active prod server.
- `harness/main.go:118-149` — Startup recovery: `Recover()` + DB validation (kills dev servers and non-ready checkpoint servers, syncs ports for valid ones).
- `harness/main.go:157-168` — Periodic reaper goroutine every 5 minutes.

### Resilience fixes
- `harness/internal/claude/claude.go:93-96,100-102` — Error paths in `HandlePrompt` now call `Disconnect()` to clean up dev server if Claude session creation or prompt send fails.
- `harness/internal/claude/claude.go:257-304` — `ReapOrphanedSessions()`: kills orphaned `cm-server-*` sessions not in manager map, and orphaned `cm-*` Claude sessions whose checkpoint is no longer `building`.
- `harness/internal/world/game_server.go:245-261` — `Disconnect(worldID, cpID)` for targeted single-server stop.
- `harness/internal/world/game_server.go:299-338` — `ReapOrphans()` scans tmux for untracked `cm-server-*` sessions and kills them.

### Docs and endpoint
- `harness/internal/server/server.go:166` — Added `GET /world/:worldID/status` route.
- `harness/internal/server/server.go:672-707` — `handleWorldStatus` handler returns JSON with checkpoint + game server info.
- `harness/internal/server/server.go:441-469` — Updated `handleLogStream` to use `.log` for game-server logs (with `.jsonl` fallback).
- `template/CLAUDE.md` — Added Dev Server section with BRP query examples, updated log references from `.jsonl` to `.log`.
- `harness/CLAUDE.md` — Added "Game Servers (tmux-based)" section with lifecycle, methods, logging, and status endpoint docs.

## Learnings

- **tmux `show-environment` output format**: `GAME_PORT=9001\n` — need to split on `=` and trim. Helper methods `readTmuxEnvStr`/`readTmuxEnvInt` encapsulate this.
- **tmux session naming**: `cm-server-{worldID}-{cpID}` is safely parseable because both IDs are 8-char hex (no hyphens). Use `SplitN(line, "-", 4)` with constant `serverSessionParts = 4`.
- **gosec nolint**: `exec.CommandContext` with only literal string args (like `tmux list-sessions`) does NOT trigger gosec G204. Only needed when args contain variables. Adding unnecessary `//nolint:gosec` triggers `nolintlint`.
- **exhaustive linter**: Even with both enum cases covered in a switch, the linter wants a `default` case. Simpler to use `if/else` when there are only two modes.
- **`pipe-pane` for logging**: `tmux pipe-pane -t {session} -o 'cat >> {logFile}'` captures raw pane output. This replaces the `jsonlLineWriter` wrapper that required `exec.Cmd` stdout access. Game server logs are now raw text (`.log`), not JSONL.
- **Port range**: 9001-9999 for game ports, BRP port = game port + 1000 (so BRP range is 10001-10999).

## Artifacts
- `harness/internal/world/game_server.go` — Complete rewrite (core)
- `harness/internal/world/ports.go` — Added `MarkInUse`
- `harness/internal/tmux/session.go` — Added `extraEnv` param
- `harness/internal/claude/claude.go` — Dev server lifecycle + reaper
- `harness/main.go` — Recovery + periodic reaper wiring
- `harness/internal/server/server.go` — Status endpoint + log stream update
- `template/CLAUDE.md` — Dev server docs for inner Claude
- `harness/CLAUDE.md` — Game server architecture docs

## Action Items & Next Steps

All implementation is complete and passes `go build ./... && just lint` with zero issues. Remaining work:

1. **Manual testing** — Start harness, create world, submit prompt, verify `tmux ls` shows both `cm-server-*` and `cm-*` sessions. Kill harness, restart, verify recovery. Test build failure path doesn't kill prod server.
2. **cargo-watch dependency** — The dev server runs `cargo watch` which must be installed in the template/checkpoint environment. Verify it's present or add to setup.
3. **Consider BRP port allocation** — Currently BRP port = GAME_PORT + 1000. If game port is 9500+, BRP port exceeds 10500. The port allocator range is 9001-9999, so max BRP would be 10999. This is fine but not explicitly validated.
4. **Consider reaper interval tuning** — Currently 5 minutes. May want to make configurable or adjust based on observed leak frequency.
5. **Log rotation** — `pipe-pane` appends to `game-server.log` indefinitely. No rotation mechanism exists yet.

## Other Notes

- **Session naming convention**: Claude sessions are `cm-{worldID}-{cpID}` (3 parts), game server sessions are `cm-server-{worldID}-{cpID}` (4 parts). The reaper uses this distinction to differentiate them.
- **Graceful vs crash shutdown**: `Shutdown()` kills all tmux sessions (called on SIGINT/SIGTERM). On harness crash, sessions survive and are recovered on next startup.
- **The old `jsonlLineWriter` in `game_server.go` was duplicated** in `harness/internal/build/builder.go` which still uses it for build log capture. Only the game_server.go copy was removed.
- **`Disconnect` was previously unused** (confirmed zero callers before this work). We re-added it with different semantics — targeted stop, no refcounting.
