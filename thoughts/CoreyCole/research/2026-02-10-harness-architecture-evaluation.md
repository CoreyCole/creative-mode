---
date: 2026-02-10T23:08:00-08:00
researcher: CoreyCole
git_commit: 7ae1f2a071191710e964489e0c945c9f0bad9e43
branch: main
repository: creative-mode
topic: "Harness Architecture Evaluation: SSE Bus, Template Organization, and Anti-Patterns"
tags: [research, codebase, harness, sse, datastar, templates, architecture]
status: complete
last_updated: 2026-02-10
last_updated_by: CoreyCole
---

# Research: Harness Architecture Evaluation

**Date**: 2026-02-10T23:08:00-08:00
**Researcher**: CoreyCole
**Git Commit**: 7ae1f2a071191710e964489e0c945c9f0bad9e43
**Branch**: main
**Repository**: creative-mode

## Research Question

Evaluate the harness architecture for simplifications:
1. Is there a single SSE event bus for the entire harness UI?
2. Can templates be cleaned up and organized? (Compare with context/northstar SSE patterns and context/datastarui signals/expressions patterns)
3. Are there any bugs or anti-patterns compared to context examples?

## Summary

The harness has a **single in-memory EventBus** that fans out events to two SSE endpoints (lobby and world). This is architecturally sound but diverges from the Northstar reference pattern where each SSE handler is self-contained. Templates are functional but lack the structured organization seen in both reference projects. The biggest opportunities for simplification are: (1) adopting Northstar's handler-per-feature pattern instead of a monolithic server.go, (2) adopting datastarui's signals/expressions helpers instead of raw inline JS strings, and (3) fixing several concrete bugs and anti-patterns identified below.

---

## Detailed Findings

### 1. SSE Event Bus Architecture

**Answer: Yes, there is a single EventBus instance for the entire harness.**

Created in `harness/main.go:84`:
```go
eventBus := events.NewEventBus()
```

Shared with both Server and Orchestrator. The bus has two tiers:
- **Global subscribers** (`globalSubs []chan any`): All connected SSE clients
- **World subscribers** (`worldSubs map[string][]chan any`): Keyed by worldID

Two SSE endpoints consume the bus:
- `GET /events` (lobby) -- subscribes to global only
- `GET /world/:worldID/events` (world) -- subscribes to both global + world-specific

**Comparison with Northstar**: Northstar does NOT use a centralized event bus. Each SSE handler is self-contained:
- Monitor handler: polls system stats on a ticker, pushes signals directly
- Todos handler: watches a NATS KV store, pushes HTML fragments on change
- Counter handler: one-shot SSE, renders component and closes

The harness EventBus adds complexity (untyped `map[string]any` events, runtime type switching in `handleGlobalEvent`/`handleWorldEvent`) but is justified by the cross-cutting nature of chat + build events that need to reach multiple pages simultaneously.

**Recommendation**: The EventBus is appropriate for the harness's multi-page real-time needs. No simplification needed here. However, the untyped event map could benefit from typed event structs to catch errors at compile time.

---

### 2. Template Organization

#### Current Structure
```
views/
  layout/layout.templ          -- shared HTML shell (17 lines)
  login/login.templ            -- static login (13 lines)
  pending/pending.templ        -- static pending (18 lines)
  lobby/lobby.templ            -- world list + chat (67 lines)
  lobby/signals.go             -- LobbySignals struct
  world/world.templ            -- iframe + overlay container (34 lines)
  world/overlay.templ          -- 3 components (70 lines)
  world/checkpoint_tree.templ  -- tree view (29 lines)
  world/lineage.templ          -- ancestry view (38 lines)
  world/signals.go             -- OverlaySignals struct
  world/helpers.go             -- helper functions
  chat/chat.templ              -- chat panel + SSE fragments (59 lines)
  admin/admin.templ            -- user management (43 lines)
```

#### Issues Identified

**A. `world.templ` bypasses `layout.Base`, duplicating `<head>`**
- `layout.templ:6-12` and `world.templ:11-15` both define `<head>` with Datastar script + CSS
- `world.templ` omits `<meta name="viewport">` that layout includes
- The world page needs a custom layout (full-screen iframe), but the head should be shared

**B. Chat input bar is duplicated**
- `lobby.templ:56-62` and `chat.templ:18-23` have nearly identical markup
- Only difference: world chat has `data-show="$active_tab !== 'lineage'"`
- Should be extracted to a shared `ChatInputBar` component

**C. `#chat-log` container is duplicated**
- Both `lobby.templ:55` and `chat.templ:16` define `<div id="chat-log" class="message-log">`
- SSE fragment patching targets `#chat-log` by ID regardless of page
- Works because only one page is active, but fragile

**D. `loadCheckpoint()` JS call pattern is duplicated**
- `checkpoint_tree.templ:19-24` and `chat.templ:52-57` use identical `loadCheckpoint(evt.target.dataset.worldId, evt.target.dataset.cpId)` patterns
- Should be a shared templ component

**E. Unused template parameters**
- `OverlayTopBar(w, cp, user)` at `overlay.templ:30-42` receives `cp` and `user` but never uses them in the rendered HTML
- These parameters should be removed

**F. No feature-based organization (vs Northstar pattern)**
Northstar organizes by feature:
```
features/<name>/
  routes.go      -- SetupRoutes() registers chi routes
  handlers.go    -- Handlers struct with HTTP handler methods
  pages/         -- templ page templates
  components/    -- templ component templates (optional)
  services/      -- business logic (optional)
```

The harness puts ALL route handlers in a single `server.go` (500+ lines). Feature-based organization would allow each feature (lobby, world, chat, admin) to own its routes, handlers, and templates.

#### Comparison with Datastarui Patterns

**Signals management**: The harness uses raw JSON struct serialization (`templ.JSONString(signals)`) which works but misses the helper patterns from datastarui:

- datastarui `utils/signals.go` provides `SignalManager` with methods like `Signal("open")` -> `$comp.open`, `Toggle("open")`, `Set("open", "false")`, etc.
- datastarui components define `expressions.go` files with handler structs that encapsulate expression building
- The harness uses raw inline strings like `$overlay_expanded = true; $unread_count = 0` directly in templates

**Component args pattern**: datastarui uses typed `args.go` structs per component with `ID`, `Class`, and `Attributes` fields. The harness passes individual parameters to template functions (up to 6 for `world.Page`).

**Recommendation**: Adopt datastarui's `SignalManager` pattern for generating signal references and expressions. This would:
- Eliminate magic string references to signal names in templates
- Make signal renames a compile-time check
- Clean up inline JS expressions

---

### 3. Bugs and Anti-Patterns

#### BUG: Negative refcount in GameServerManager

**File**: `harness/internal/world/game_server.go:86-91`

If `Disconnect()` is called when `refCount[key]` is already 0 (or the key doesn't exist), `m.refCount[key]--` sets it to -1. This spawns a `stopAfterDelay` goroutine and leaves a stale `-1` entry. Not a crash (the `ok` check on `m.servers[key]` at line 165 saves it), but leaks goroutines and map entries.

**Fix**: Guard with `if m.refCount[key] > 0` before decrementing.

#### BUG: Inconsistent SSE expression syntax

**File**: `harness/views/world/world.templ:28`
```go
data-on-load={ fmt.Sprintf("@get('/world/%s/events',{requestCancellation: 'disabled'})", w.ID) }
```

vs `harness/views/lobby/lobby.templ:53`:
```go
data-on-load={ datastar.GetSSE("/events") }
```

The world page uses raw `fmt.Sprintf` with `@get(...)` syntax while the lobby uses the `datastar.GetSSE()` helper. This inconsistency means if the Datastar API changes, the world page will break silently.

**Fix**: Use `datastar.GetSSE()` everywhere. For the `requestCancellation` option, check if the Go helper supports options or contribute the option upstream.

#### BUG: `evt.preventDefault()` string concatenation

**File**: `harness/views/lobby/lobby.templ:45`
```go
data-on-click={ datastar.PostSSE("/world/create") + "; evt.preventDefault()" }
```

This manually concatenates JS onto a generated expression string. Fragile -- if `PostSSE()` changes its output format, the concatenation could produce invalid JS.

**Fix**: Use Datastar's `data-on-submit` with `@post` instead, or use the proper Datastar modifier `__prevent` (e.g., `data-on-click__prevent`).

#### ANTI-PATTERN: Untyped event map

**File**: `harness/internal/events/bus.go`

All events are `map[string]any` with a string `"event"` key. The handler functions (`handleGlobalEvent`, `handleWorldEvent`) use runtime type assertions and string matching. Northstar avoids this entirely by not having a shared bus -- each handler produces its own events directly.

**Fix**: Define typed event structs:
```go
type ChatMessageEvent struct { ... }
type BuildCompletedEvent struct { ... }
type PlayerJoinedEvent struct { Username string; AvatarURL string }
```

Then use a type switch instead of `event["event"].(string)` matching.

#### ANTI-PATTERN: Monolithic server.go (500+ lines)

All HTTP handlers, route registration, SSE handling, and business logic live in one file. Northstar's feature-based `routes.go` + `handlers.go` pattern is much more maintainable.

**Recommendation**: Split into feature modules:
```
internal/server/
  server.go          -- core setup, middleware registration
  routes.go          -- route tree (calls feature SetupRoutes)
  lobby_handlers.go  -- lobby page + SSE
  world_handlers.go  -- world page + SSE + prompt
  chat_handlers.go   -- chat message handler
  admin_handlers.go  -- admin pages
  events.go          -- SSE helpers (keep)
  render.go          -- render helper (keep)
```

#### ANTI-PATTERN: Missing `data-show` + `style="display: none;"` pattern

**Datastarui pattern** (`dialog.templ:41`, `sheet.templ:77`): Components that start hidden use BOTH `data-show` (for Datastar reactivity) AND `style="display: none;"` (to prevent flash of content before Datastar initializes).

The harness uses `data-show` alone (e.g., `overlay.templ:12`, `chat.templ:10,14,17`). This can cause a flash of hidden content on page load before Datastar JS initializes and evaluates the `data-show` expression.

**Fix**: Add `style="display: none;"` to all elements that start with `data-show` evaluating to false.

#### ANTI-PATTERN: `sendChatHistory` error silently closes SSE

**File**: `harness/internal/server/events.go:78-79`
```go
if err := s.sendChatHistory(sse, ctx); err != nil {
    return nil
}
```

If chat history fails to load (e.g., DB error), the SSE connection closes silently. The user sees a blank chat with no error indication.

**Fix**: Log the error and continue the SSE loop without history, or send an error notification via `sse.ConsoleError()`.

#### ANTI-PATTERN: Unbounded rate limiter map

**File**: `harness/internal/world/rate_limit.go:22`

`lastSubmit map[string]time.Time` grows without bound. No eviction of old entries.

**Fix**: Use a TTL cache or periodically prune entries older than the cooldown window.

#### ANTI-PATTERN: Background builds use `context.Background()`

**File**: `harness/internal/world/manager.go:120-121`

Fire-and-forget goroutines for builds use `context.Background()`, meaning they are not cancellable on server shutdown. Same at `server.go:441`.

**Fix**: Pass a server-scoped context that is cancelled on graceful shutdown.

#### ANTI-PATTERN: `/api/claude-event` has no authentication

**File**: `harness/internal/server/server.go:102`

The webhook endpoint for Claude hook scripts is registered outside all auth middleware. While it's intended for same-machine communication, any process on the machine can publish events to any world.

**Fix**: Add a shared secret or HMAC verification for hook events.

---

## Architecture Simplification Roadmap

### Phase 1: Quick Wins (Low Risk)
1. Remove unused `cp` and `user` params from `OverlayTopBar`
2. Extract shared `ChatInputBar` templ component
3. Extract shared `CheckpointLoadButton` templ component
4. Add `style="display: none;"` to hidden-by-default elements
5. Fix negative refcount guard in GameServerManager
6. Use `datastar.GetSSE()` consistently (remove raw `@get` strings)

### Phase 2: Signal Helpers (Medium Risk)
1. Port datastarui's `SignalManager` pattern to harness `views/` package
2. Create `expressions.go` for overlay signal expressions
3. Replace raw `$signal_name` strings in templates with generated references
4. Share `<head>` between layout.templ and world.templ

### Phase 3: Structural Refactor (Higher Risk)
1. Split `server.go` into feature-based handler files
2. Define typed event structs for the EventBus
3. Add feature-based route registration (Northstar `SetupRoutes` pattern)
4. Add graceful shutdown context propagation to background builds

---

## Code References

- `harness/internal/events/bus.go` -- EventBus implementation (104 lines)
- `harness/internal/server/events.go` -- SSE handlers and event dispatch
- `harness/internal/server/server.go` -- All route registration and handlers (500+ lines)
- `harness/views/world/signals.go:6-17` -- OverlaySignals struct (10 fields)
- `harness/views/world/world.templ:28` -- Raw `@get` SSE expression
- `harness/views/lobby/lobby.templ:53` -- `datastar.GetSSE()` helper usage
- `harness/internal/world/game_server.go:86-91` -- Negative refcount bug
- `context/northstar/features/` -- Feature-based organization reference
- `context/datastarui/utils/signals.go` -- SignalManager reference implementation
- `context/datastarui/utils/expressions.go` -- Expression builder reference

## Architecture Insights

### What the harness does well:
- The EventBus fan-out pattern is appropriate for cross-cutting chat + build events
- SSE heartbeat mechanism (30s) with dead-connection detection is solid
- Reference-counted game server management with grace period is well-designed
- The fire-and-forget publish semantics (drop on backpressure) prevents slow clients from blocking producers

### What the reference projects do better:
- **Northstar**: Self-contained feature modules with own routes, handlers, and templates. Each SSE handler manages its own data flow (ticker, NATS watcher, one-shot) rather than consuming from a shared bus. Uses `datastar.NewSSE()` inline for both long-lived and one-shot patterns.
- **Datastarui**: Typed signal management via `SignalManager` with expression-generating methods. Component handler pattern (`expressions.go`) encapsulates all Datastar JS expression building. Args structs provide clean component interfaces. `data-show` + `style="display: none;"` prevents FOUC.

## Open Questions

1. Should the EventBus be replaced with a more direct approach (each handler manages its own state), or is the centralized bus worth keeping for the multiplayer chat use case?
2. Is the `requestCancellation: 'disabled'` option supported by the `datastar-go` helper, or does it require raw string expressions?
3. Should the harness adopt datastarui's full component library pattern (args.go + expressions.go + component.templ per component), or is a lighter-weight signal helper sufficient?
