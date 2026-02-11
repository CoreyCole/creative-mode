---
date: 2026-02-11T03:12:29Z
reviewer: Claude (Staff Eng Review)
git_commit: e683fe4641149551a9641f9de4a29d7c32bbd79c
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-11-wave4-ui-overlay-chat.md
status: complete
type: plan_review
---

# Plan Review: Wave 4 — UI Overlay + Chat

### Summary

The plan is thorough and well-structured with 6 phases, verified Datastar API, and clear success criteria. However, it contains a **circular import that will prevent compilation**, a **non-existent method call** (`ConnectGameServer`), a **game server reference-count leak**, and several Datastar usage patterns that diverge from the reference implementations in `context/northstar/` and `context/datastarui/`. With the critical issues fixed, this is a solid plan.

### Critical Issues (Must Address Before Implementation)

These issues will cause compilation failures or runtime bugs:

1. **Circular Import: `views` <-> `internal/server`**
   - Problem: `views/world.templ` imports `server.OverlaySignals` from `internal/server`, but `internal/server/server.go` imports `views` to call `render(c, views.World(...))`. Go does not allow circular imports — this will fail to compile.
   - Risk: Complete build failure. This is a design-time error that can't be worked around.
   - Suggestion: Move `OverlaySignals` to the `views` package (since it's a view-model), or create a shared `internal/types` package. The simplest fix is defining it in `views/signals.go` since it's only used as a data transfer object for templates.

2. **Non-Existent Method: `s.WorldManager.ConnectGameServer`**
   - Problem: Step 2.6 calls `s.WorldManager.ConnectGameServer(ctx, worldID, cpID)` (plan line 596). This method does not exist. The actual API is `s.WorldManager.GameServers.Connect(worldID, cpID, checkpointDir)` — different name, different signature (no `ctx`, requires `checkpointDir`).
   - Risk: Compilation failure.
   - Suggestion: Use `s.WorldManager.GameServers.Connect(worldID, cpID, cp.DirPath)` after fetching the checkpoint. Note that `Connect` also increments a refcount that must be decremented (see issue #3).

3. **Game Server Reference-Count Leak**
   - Problem: The plan calls `Connect` in `handleWorldView` (a regular page handler) to get the server port, but never calls `Disconnect`. Every page load increments the refcount. Servers will never be stopped because the refcount never reaches zero.
   - Risk: Resource leak — game server processes accumulate indefinitely, exhausting ports (pool is 9001-9999).
   - Suggestion: Two options:
     - **Option A (recommended)**: Don't call `Connect` in `handleWorldView`. Instead, read the port from the checkpoint's `server_port` DB column (already populated by `BuildCheckpoint` at `claude.go:148-151`). Only call `Connect`/`Disconnect` in the SSE handler where the lifecycle is managed by the connection.
     - **Option B**: Call `Connect` in the SSE handler with `defer Disconnect`, and pass `serverPort: 0` initially, letting the SSE handler send a signal update with the port once connected.

4. **`game-loader.js` Reads Stale Signal Data**
   - Problem: The `loadLineage()` function (plan line 547-549) reads `overlay.dataset.signals` to get `currentWorldId` and `currentCheckpointId`. After Datastar hydrates, signal values are managed internally and are NOT reflected back to the `data-signals` HTML attribute. The `dataset.signals` will always contain the initial values from page load, never updated values.
   - Risk: Lineage tab always shows the initial checkpoint's lineage, even after the user loads a different checkpoint.
   - Suggestion: Use Datastar's signal access from JavaScript. In Datastar v1, you can access signals via `window.ds.signal('currentWorldId').value` or pass the values as function arguments from a `data-on-click` expression: `data-on-click="loadLineage($currentWorldId, $currentCheckpointId)"`.

### Concerns (Should Address)

These warrant attention but aren't compilation blockers:

5. **`data-on-load` vs `data-init` for SSE Connections**
   - Observation: The plan uses `data-on-load={ datastar.GetSSE("/events") }` (lines 243, 393) for establishing SSE connections. However, the northstar reference consistently uses `data-init` for this purpose (`features/counter/pages/counter.templ:59`, `features/monitor/pages/monitor.templ:23`, `features/index/pages/index.templ:12`). Specifically, the todo feature uses `data-init="@get('/api/todos',{requestCancellation: 'disabled'})"` with the `requestCancellation: 'disabled'` option.
   - Suggestion: Verify that `data-on-load` correctly establishes long-lived SSE connections in the current Datastar v1 version. Consider using `data-init` with `requestCancellation: 'disabled'` to prevent Datastar from canceling the SSE connection when other actions are triggered, matching the proven northstar pattern.

6. **Chat Message Username Resolution Not Planned**
   - Observation: Acknowledged as Open Question #1 but not resolved. The `GetRecentMessages` query returns `user_id` (a `sql.NullString`), not a username. The SSE history loop at plan line 652 uses `msg.UserID.String` as the display name, which would show raw UUIDs (e.g., "a3f8b2c1") to users.
   - Suggestion: Create a new sqlc query `GetRecentMessagesWithUser` that joins `messages` and `users` tables, returning `username` and `avatar_url`. This is a one-time addition:
     ```sql
     -- name: GetRecentMessagesWithUser :many
     SELECT m.*, u.github_username, u.avatar_url
     FROM messages m LEFT JOIN users u ON m.user_id = u.id
     ORDER BY m.created_at DESC LIMIT ?;
     ```

7. **No SSE Heartbeat / Keepalive**
   - Observation: The long-lived SSE handlers block on `select` waiting for EventBus events. If no events occur for extended periods (minutes), proxies, load balancers, or browsers may silently close the connection. The northstar monitor avoids this with 1-second tickers, but the world/global SSE handlers have no such mechanism.
   - Suggestion: Add a periodic heartbeat (e.g., every 30 seconds) using `time.NewTicker`. Send a Datastar comment or no-op signal patch to keep the connection alive.

8. **Echo Middleware Compatibility with SSE**
   - Observation: The request logger middleware (`server.go:44-55`) calls `next(c)` and then logs the response. For SSE handlers, `next(c)` blocks for the lifetime of the connection (potentially hours). Additionally, Echo's recovery middleware wraps the response writer, which could interfere with streaming. This pattern works in northstar with chi router but hasn't been validated with Echo.
   - Suggestion: Either (a) exempt SSE routes from the logger/recovery middleware, or (b) test SSE streaming early in Phase 1 with a minimal endpoint before building the full implementation. Consider using Echo's `Skipper` function on the logger middleware for SSE paths.

9. **XSS Risk in `onclick` Attributes with `fmt.Sprintf`**
   - Observation: Several templ components use `onclick={ fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cp.ID) }` (lines 524, 895). While the IDs are 8-char hex UUIDs (safe in practice), this pattern injects unescaped strings into JavaScript. templ does not escape `onclick` attribute content.
   - Suggestion: Use `data-on-click` with Datastar expressions instead of raw `onclick`, or pass data via `data-*` attributes and read them in JS. For example: `data-on-click={ fmt.Sprintf("loadCheckpoint(evt.target.dataset.worldId, evt.target.dataset.cpId)") }` with `data-world-id` and `data-cp-id` attributes.

10. **Missing `fmt` Import in `helpers.go`**
    - Observation: The `timeAgo` function (plan line 966) uses `fmt.Sprintf` but the import block only declares `"time"`.
    - Suggestion: Add `"fmt"` to the import block.

11. **Signal Naming Convention Mismatch**
    - Observation: The plan uses camelCase for signal names (`chatText`, `currentWorldId`, `overlayExpanded`, etc.) in `OverlaySignals`. The datastarui rules (`.cursor/rules/datastar.mdc` lines 250-253) mandate lowercase with underscores only. While this is from the datastarui project (not a Datastar framework requirement), inconsistency with the reference could cause confusion for future contributors.
    - Suggestion: Consider using snake_case (`chat_text`, `current_world_id`, `overlay_expanded`) to align with the datastarui conventions, or document the conscious departure.

### Questions (Need Clarification)

1. **World creation response**: Open Question #2 asks whether world creation should redirect or re-render. The lobby uses `datastar.PostSSE("/world/create")` which establishes an SSE connection for the response. Should the handler (a) use `sse.ExecuteScript("window.location.href='/world/'+id")` to redirect, (b) use `sse.PatchElementTempl` to append the new world card to the list, or (c) something else? This needs a decision before implementation.

2. **Prompt submission with Datastar signals**: Open Question #5 asks about `handlePrompt` conversion. The current handler reads `{prompt, checkpoint_id}` from JSON. With Datastar, these become `$promptText` and `$currentCheckpointId` signals. Using `datastar.ReadSignals` is the Datastar-idiomatic approach. Should we convert `handlePrompt` now (Phase 2) or leave it as a separate task?

3. **World-scoped chat messages**: The chat panel has Global/World/Lineage tabs, but the SSE handlers don't differentiate — all messages go to `#chat-log`. How should world-scoped messages be filtered? Should the server filter based on `$activeTab`, or should messages be sent to separate `#global-chat-log` and `#world-chat-log` elements?

4. **Static Datastar vs CDN**: Open Question #3 — the northstar reference vendors `datastar.js` into `web/resources/static/datastar/`. For a dev tool that may be used in environments without reliable internet, vendoring seems safer. Worth deciding now since it affects the layout template.

### Suggestions (Nice to Have)

1. **Use `templ.JSONString` consistently for `data-signals`**: The plan correctly uses it in `world.templ` (line 392) but could benefit from the `utils.Signals()` pattern from datastarui for type-safe signal namespacing if the signal struct grows.

2. **Add `data-indicator-fetching` to interactive buttons**: The Send chat button and Build button should show loading states using Datastar's built-in fetch indicator pattern (documented in datastarui). Example: `data-indicator-fetching` on the button, `data-attr-disabled="$fetching"`, and a spinner `data-show="$fetching"`.

3. **Consider the northstar pattern of separating mutation from rendering**: Northstar's todo feature updates state (NATS KV) in mutation handlers, and the long-lived SSE watcher pushes the rendered update. The plan already follows this pattern (chat mutation -> EventBus -> SSE handler), which is good. But `handleCreateWorld` and `handlePrompt` should also publish events that the SSE handler renders, rather than trying to respond with SSE patches directly.

4. **Add `ConsoleError` fallback**: Northstar uses a two-tier error pattern for SSE handlers: try `sse.ConsoleError(err)`, fall back to `http.Error()`. The plan's SSE handlers silently discard errors with `_ = sse.PatchElementTempl(...)`. Consider logging or sending console errors.

5. **CSS approach**: The plan defers CSS details to the component-6 spec with "start with login + lobby styles". Consider creating a minimal CSS file in Phase 1 that covers all phases (even if some styles aren't used yet) to avoid blocking Phase 2+ on CSS work.

### What's Good

- **Verified Datastar API section** (lines 62-77): Documenting the exact SDK API with method signatures before coding prevents guessing.
- **Phased approach with per-phase success criteria**: Each phase has clear automated (`go build`) and manual verification steps.
- **EventBus subscription before history**: The SSE handlers correctly subscribe to EventBus channels before fetching message history, preventing the race condition where events arrive between history fetch and subscription.
- **Correct use of `datastar.WithModeAppend()`**: Chat messages are appended to the log, not replacing it.
- **"What We're NOT Doing" section**: Explicit scope exclusions (no Tailwind, no WebSocket, no JS framework) prevent scope creep.
- **handleRoot soft session check**: The approach of doing a soft cookie check without middleware (plan lines 277-298) elegantly handles the login/lobby/pending routing without requiring separate unauthenticated routes.

### Recommended Next Steps

1. **Fix the circular import** (Critical #1) — move `OverlaySignals` to `views` package or create `internal/types`
2. **Fix `ConnectGameServer`** (Critical #2) — use `cp.ServerPort` from DB instead of calling Connect
3. **Fix `loadLineage` signal access** (Critical #4) — use Datastar's JavaScript signal API
4. **Create a `GetRecentMessagesWithUser` sqlc query** (Concern #6) — needed for chat history display
5. **Decide on Open Questions** #1 (username), #2 (world creation UX), #5 (prompt submission) before starting Phase 1
6. **Validate Echo SSE compatibility** (Concern #8) — write a minimal SSE endpoint and test with Echo's middleware before building the full implementation
7. **Add SSE heartbeat** (Concern #7) — add a 30-second ticker to all long-lived SSE handlers
