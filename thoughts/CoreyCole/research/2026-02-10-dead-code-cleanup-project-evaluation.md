---
date: 2026-02-10T21:44:24-08:00
researcher: CoreyCole
git_commit: 8c4ead913eba0be789ba8c60cff792064daa2647
branch: main
repository: creative-mode
topic: "Dead Code Cleanup and Project State Evaluation"
tags: [research, codebase, dead-code, cleanup, project-evaluation, wave-status]
status: complete
last_updated: 2026-02-10
last_updated_by: CoreyCole
---

# Research: Dead Code Cleanup and Project State Evaluation

**Date**: 2026-02-10T21:44:24-08:00
**Researcher**: CoreyCole
**Git Commit**: 8c4ead913eba0be789ba8c60cff792064daa2647
**Branch**: main
**Repository**: creative-mode

## Research Question

Identify all dead code in the harness codebase and evaluate overall project status across all waves.

## Summary

The codebase is in good shape after Wave 4 completion. There are **8 dead code items** to remove, **1 orphaned component** to wire in, **1 functional gap** to address (game server Disconnect never called), and **1 duplicated utility** to deduplicate. Waves 1-4 are complete; Wave 5 (Integration + Docker) is next.

## Dead Code Inventory

### Confirmed Dead Code (Safe to Delete)

| # | Item | File | Lines | Reason |
|---|------|------|-------|--------|
| 1 | `HandleAdminUsers` | `harness/internal/auth/auth.go` | 242-272 | Replaced by `handleAdminUsers` in server.go:507-516. Old version returns JSON; new renders templ. No cascading import removals needed. |
| 2 | `LoadCheckpointExpr` | `harness/views/world/expressions.go` | 7-9 | Never called. Templ files inline the expression via `data-on-click` with `evt.target.dataset` pattern instead. |
| 3 | `LoadLineageExpr` | `harness/views/world/expressions.go` | 13-15 | Never called. Inlined in `chat.templ:13`. |
| 4 | `tmux.Session.Kill()` | `harness/internal/tmux/session.go` | 71-78 | Never called anywhere. Planned for future session cleanup. |
| 5 | `tmux.Session.IsAlive()` | `harness/internal/tmux/session.go` | 80-88 | Never called anywhere. Planned for future health checks. |

**Note on #2-3**: The entire file `expressions.go` is dead. Removing it also removes the unused `fmt` import. After deletion, re-run `templ generate` to ensure no generated code references it.

**Note on #4-5**: These are utilities that *will* be needed for Wave 5 session lifecycle management. Consider keeping them if Wave 5 is imminent.

### Unused SQL Queries (Generated but Never Called)

| # | Query | File | Lines | Notes |
|---|-------|------|-------|-------|
| 6 | `ListPendingUsers` | `harness/internal/db/queries/users.sql` | 25-26 | Could be useful for admin notification features |
| 7 | `GetRecentMessages` | `harness/internal/db/queries/messages.sql` | 5-7 | Superseded by `GetRecentMessagesWithUser` (joined query) |
| 8 | `GetRecentMessagesByWorld` | `harness/internal/db/queries/messages.sql` | 9-11 | Intended for world-scoped chat filtering, not yet implemented |

**Recommendation**: Keep #6 and #8 (they'll be needed soon). Remove #7 since it's fully superseded.

### Unused Field

| # | Item | File | Lines | Notes |
|---|------|------|-------|-------|
| 9 | `RateLimiter.maxCPPerWorld` | `harness/internal/world/rate_limit.go` | 24 | Set to 50 but never read/checked. The max-checkpoints-per-world limit is defined but unimplemented. |

## Orphaned Components (Need Wiring, Not Deletion)

### `CheckpointTree` Component

- **Defined**: `harness/views/world/checkpoint_tree.templ:5`
- **Problem**: Never rendered into the DOM. The overlay has a "Tree" button (`overlay.templ:36`) that toggles `$show_checkpoint_tree`, and `CheckpointTree` has `data-show="$show_checkpoint_tree"`, but the component is never actually injected into the page.
- **Fix**: Add `@CheckpointTree(checkpoints, cpID, worldID)` to `overlay.templ` inside the overlay-expanded div, and pass the checkpoint tree data from `handleWorldView` through to the template.

### JSON-Only Handlers (No Frontend Caller)

| Handler | Route | File:Line | Status |
|---------|-------|-----------|--------|
| `handleCheckpointView` | `GET /world/:worldID/checkpoint/:cpID` | `server.go:252-270` | No frontend calls this. `loadCheckpoint` in game-loader.js navigates to `/world/{worldID}` instead. Useful as an API endpoint. |
| `handleSaveCheckpoint` | `POST /world/:worldID/checkpoint` | `server.go:362-390` | Bookmark/rename feature with no UI wired. Keep for future use. |

## Functional Gap

### Game Server Disconnect Never Called

- `GameServerManager.Disconnect()` at `harness/internal/world/game_server.go:83` is **never invoked**.
- When users leave a world SSE connection (`events.go:101-110`), the handler publishes a `player.left` event but does not call `Disconnect()`.
- **Impact**: Game server processes started by `Connect()` at `claude.go:143` accumulate and never stop via the grace period mechanism. They only terminate at application `Shutdown()`.
- **Fix**: Call `s.WorldManager.GameServers.Disconnect(worldID, cpID)` in the `ctx.Done()` case of `handleWorldSSE`.

## Duplicated Code

### `jsonlLineWriter` Struct

Identically defined in two locations:
- `harness/internal/build/builder.go:236-266`
- `harness/internal/world/game_server.go:191-221`

**Recommendation**: Extract to a shared `internal/logging/` package or `internal/jsonl/` package.

## Project Wave Status

| Wave | Component | Status | Key Commit |
|------|-----------|--------|------------|
| Wave 1 | #1 Harness Server + DB | COMPLETE | Handoff: `2026-02-10_17-03-43` |
| Wave 1 | #4 Bevy Game Template | COMPLETE | (parallel with #1) |
| Wave 2 | #2 Auth + Admin | COMPLETE | Handoff: `2026-02-10_17-13-36` |
| Wave 2 | #3 World Management + Build | COMPLETE | (parallel with #2) |
| Wave 3 | #5 Claude Integration + tmux | COMPLETE | Commit: `25cd877` |
| Wave 4 | #6 UI Overlay + Chat | COMPLETE (unstaged) | Handoff: `2026-02-10_21-20-11` |
| Wave 5 | #7 Integration + Docker | NOT STARTED | Spec: `component-7-integration-docker.md` |

### Current Git State

- **19 new files** and **4+ modified files** from Wave 4 are **unstaged**
- No commits have been made for Wave 4 yet
- The `.golangci.yml` changes in the diff are from a previous session (Wave 3 lint setup)

### What Wave 5 Covers (Component 7)

Per `thoughts/CoreyCole/plans/component-7-integration-docker.md`:
1. Dockerfile (Ubuntu 24.04 with Rust, Go, templ, Claude Code CLI, tmux)
2. docker-compose.yml (port mapping, volume mounts for cargo cache)
3. setup.sh (pre-build template dependencies)
4. .env.example
5. Main.go integration wiring (graceful shutdown, tmux cleanup)
6. Full 22-step end-to-end manual test checklist

### Deferred Items (From Staff Review)

1. Shared CARGO_HOME race conditions under concurrent builds
2. Input sanitization for display names
3. MEMORY.md per-checkpoint fork semantics
4. Claude Code hook payload format verification
5. Game server health checks (periodic heartbeats)

## Recommended Action Plan

### Immediate (Before Committing Wave 4)

1. **Delete dead code**: `HandleAdminUsers` in auth.go, `expressions.go` file, `GetRecentMessages` query
2. **Wire CheckpointTree**: Add to overlay.templ, pass data from handleWorldView
3. **Run `just check`** to verify build/lint pass
4. **Commit Wave 4** as a single commit

### Short-Term (Wave 5 Prep)

5. Fix game server Disconnect gap in handleWorldSSE
6. Deduplicate `jsonlLineWriter`
7. Implement `maxCPPerWorld` check or remove the field
8. Begin Wave 5 (Integration + Docker)

### Keep for Now

- `tmux.Kill()` / `tmux.IsAlive()` — needed for Wave 5 graceful shutdown
- `ListPendingUsers` query — useful for admin notifications
- `GetRecentMessagesByWorld` query — needed for world-scoped chat
- `handleCheckpointView` / `handleSaveCheckpoint` — API endpoints for future UI features

## Code References

- `harness/internal/auth/auth.go:242-272` — Dead `HandleAdminUsers`
- `harness/views/world/expressions.go:1-16` — Entirely dead file
- `harness/internal/tmux/session.go:71-88` — Unused Kill/IsAlive methods
- `harness/internal/world/rate_limit.go:24` — Unused maxCPPerWorld field
- `harness/internal/world/game_server.go:83` — Disconnect never called
- `harness/internal/build/builder.go:236-266` — Duplicated jsonlLineWriter
- `harness/internal/world/game_server.go:191-221` — Duplicated jsonlLineWriter
- `harness/views/world/checkpoint_tree.templ:5` — Orphaned component
- `harness/internal/server/events.go:101-110` — Missing Disconnect call

## Historical Context

- `thoughts/CoreyCole/plans/README.md` — Wave structure and dependency graph
- `thoughts/CoreyCole/plans/component-7-integration-docker.md` — Wave 5 spec
- `thoughts/CoreyCole/handoffs/general/2026-02-10_21-20-11_wave4-ui-overlay-chat-implementation.md` — Wave 4 completion handoff
- `thoughts/CoreyCole/reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md` — Staff review of Wave 4 plan

## Open Questions

1. Should we keep `tmux.Kill()`/`IsAlive()` given Wave 5 is next, or remove and re-implement?
2. Should `handleCheckpointView` be converted to return templ HTML or kept as a JSON API?
3. Is the `maxCPPerWorld` limit still desired, or should the field be removed entirely?
