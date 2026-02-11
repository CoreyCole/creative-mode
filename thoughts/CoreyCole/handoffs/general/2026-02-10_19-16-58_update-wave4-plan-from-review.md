---
date: 2026-02-11T03:16:58Z
researcher: Claude (Staff Eng Review)
git_commit: e683fe4641149551a9641f9de4a29d7c32bbd79c
branch: main
repository: creative-mode
topic: "Wave 4 UI Overlay + Chat Plan Refinement from Review"
tags: [implementation, plan-refinement, wave4, ui-overlay, chat, datastar, templ]
status: complete
last_updated: 2026-02-10
last_updated_by: Claude
type: implementation_strategy
---

# Handoff: Update Wave 4 Plan Based on Staff Review

## Task(s)

- **Plan Review** (completed): Performed a staff-engineer-level review of the Wave 4 UI Overlay + Chat implementation plan. Examined the northstar and datastarui reference implementations for Datastar best practices. Verified factual claims in the plan against the actual codebase. Found 4 critical issues, 7 concerns, and 4 open questions.
- **Plan Update** (next step): The plan at `thoughts/CoreyCole/plans/2026-02-11-wave4-ui-overlay-chat.md` needs to be updated to address the review findings before implementation begins.

## Critical References

1. **Plan to update**: `thoughts/CoreyCole/plans/2026-02-11-wave4-ui-overlay-chat.md`
2. **Review document**: `thoughts/CoreyCole/reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md`
3. **Northstar reference** (Datastar SSE patterns): `context/northstar/` — especially `features/index/pages/index.templ` for `data-init` with `requestCancellation: 'disabled'`

## Recent changes

- `thoughts/CoreyCole/reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md` — new review document

## Learnings

### Critical Issues Found (must fix in plan)

1. **Circular import `views` <-> `internal/server`**: `views/world.templ` imports `server.OverlaySignals`, but `server.go` imports `views` to render templates. Go forbids this. Fix: move `OverlaySignals` to the `views` package or a shared `internal/types` package.

2. **Non-existent method `s.WorldManager.ConnectGameServer`**: The actual API is `s.WorldManager.GameServers.Connect(worldID, cpID, checkpointDir)` — different name, different signature (no ctx, requires checkpointDir). See `harness/internal/world/game_server.go:50-79`.

3. **Game server refcount leak**: Plan calls `Connect` in `handleWorldView` (regular handler) but never calls `Disconnect`. Recommended fix: read `server_port` from the checkpoint DB column instead of calling `Connect` (the port is already stored by `BuildCheckpoint` at `harness/internal/claude/claude.go:148-151`).

4. **`game-loader.js` reads stale `data-signals`**: `loadLineage()` reads `overlay.dataset.signals` but Datastar doesn't reflect updated signal values back to the HTML attribute. Fix: use Datastar's JS signal API or pass values via `data-on-click` expression arguments.

### Datastar Pattern Divergences from References

- Northstar uses `data-init` (not `data-on-load`) for SSE connections, with `requestCancellation: 'disabled'` option for long-lived connections
- DatastarUI rules mandate lowercase+underscore signal names (plan uses camelCase)
- Northstar uses `sse.ConsoleError(err)` as fallback for SSE errors; plan silently discards with `_ =`
- No SSE heartbeat/keepalive in the plan (proxies may kill idle connections)

### Other Concerns

- `GetRecentMessages` returns `user_id` (UUID string), not username — need a new joined query
- Echo middleware (logger, recovery) may interfere with long-lived SSE connections
- `fmt.Sprintf` in `onclick` attrs has theoretical XSS risk (safe with UUID IDs but bad pattern)
- Missing `fmt` import in `helpers.go` `timeAgo` function
- Open questions #1-5 in the plan need decisions

## Artifacts

- `thoughts/CoreyCole/reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md` — full review with all findings
- `thoughts/CoreyCole/plans/2026-02-11-wave4-ui-overlay-chat.md` — the plan to be updated

## Action Items & Next Steps

The next agent should update the plan document to address the review findings, in this priority order:

1. **Fix Critical Issue #1**: Resolve the circular import — move `OverlaySignals` out of `internal/server` (recommend putting in `views/` package as `views/signals.go`)
2. **Fix Critical Issue #2**: Replace `ConnectGameServer` with correct API or (better) use DB `server_port` column
3. **Fix Critical Issue #3**: Remove `Connect` from `handleWorldView`, use checkpoint's `ServerPort` field from DB
4. **Fix Critical Issue #4**: Rewrite `loadLineage()` JS to use Datastar signal API instead of `dataset.signals`
5. **Add `GetRecentMessagesWithUser` sqlc query** to the plan (join messages + users for username/avatar)
6. **Switch `data-on-load` to `data-init`** with `requestCancellation: 'disabled'` for SSE connections, matching northstar
7. **Add SSE heartbeat** (30s ticker) to all long-lived SSE handlers
8. **Add Echo SSE validation step** early in Phase 1 — test that SSE streaming works with Echo's middleware
9. **Resolve open questions** — make decisions on: username storage, world creation UX, Datastar CDN vs vendored, chat signal binding approach, prompt submission conversion
10. **Fix minor issues**: missing `fmt` import in `helpers.go`, add `ConsoleError` fallback in SSE handlers, consider `data-indicator-fetching` for buttons

## Other Notes

### Key file locations in the harness codebase
- Server routes & handlers: `harness/internal/server/server.go`
- EventBus: `harness/internal/events/bus.go`
- Auth flow: `harness/internal/auth/auth.go`, `harness/internal/auth/middleware.go`
- Game server manager: `harness/internal/world/game_server.go` (Connect at line 50, Disconnect at line 83)
- Claude orchestrator build: `harness/internal/claude/claude.go` (stores server_port at line 148-151)
- DB types: `harness/internal/db/sqlc/models.go`
- Message queries: `harness/internal/db/sqlc/messages.sql.go`
- SQL query definitions: `harness/internal/db/queries/messages.sql`

### Northstar reference patterns to follow
- SSE init: `context/northstar/features/index/pages/index.templ:12` — `data-init` with `requestCancellation: 'disabled'`
- Long-lived SSE handler: `context/northstar/features/index/handlers.go:32-71` — subscribe, initial render, select loop
- Error handling: `context/northstar/features/index/handlers.go:63-68` — ConsoleError fallback pattern
- Signal struct: `context/northstar/features/counter/pages/counter.templ:47` — `data-signals={ templ.JSONString(signals) }`

### DatastarUI conventions
- Signal naming: lowercase + underscores only (`.cursor/rules/datastar.mdc:250-253`)
- DataClass builder: `context/datastarui/utils/data_class.go` — for conditional CSS
- Signal manager: `context/datastarui/utils/signals.go` — type-safe signal expressions
