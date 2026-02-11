# Wave 4: UI Overlay + Chat — Refined Implementation Plan

> **Updated 2026-02-11** based on [staff engineer review](../reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md).
> Key changes: directory-per-view structure with colocated `signals.go`/`expressions.go` (following datastarui pattern), snake_case signal names, circular import resolved, game server refcount leak fixed, SSE heartbeat added, `data-init` instead of `data-on-load`, vendored Datastar, all open questions resolved.

## Overview

Add the HTML presentation layer to the harness server using **templ** (Go HTML templating), **Datastar** (hypermedia framework with SSE), and static CSS/JS. Converts all JSON-returning handlers to templ views and adds real-time SSE event streaming for chat, build status, and notifications.

## Current State Analysis

### What Exists
- Echo server with JSON-only handlers (`internal/server/server.go`)
- Auth middleware chain: `SessionMiddleware` -> `ApprovedMiddleware` -> `AdminMiddleware`
- EventBus with global + per-world pub/sub (`internal/events/bus.go`)
- All DB queries via sqlc: worlds, checkpoints, messages, users, user_positions
- `handleChatMessage` persists messages and publishes to EventBus
- `handleClaudeEvent` receives Claude hook events and publishes to world-specific bus
- `justfile` already has `templ generate` in the `generate` recipe
- Static file serving configured: `e.Static("/static", "static")`

### What's Missing
- No templ or datastar-go dependencies in `go.mod`
- No `.templ` files, no `views/` directory tree
- No SSE endpoints (EventBus exists but nothing subscribes from HTTP)
- No `static/styles.css`, `static/game-loader.js`, or `static/datastar.js`
- No `internal/server/render.go` or `events.go`
- No `GetRecentMessagesWithUser` joined query (needed for chat history with usernames)

### Key Discoveries
- `server.go:59-60` — Static serving already configured for `/assets` and `/static`
- `server.go:78-82` — Auth groups: `authed` (session required), `approved` (role check), `admin`
- `server.go:86` — WASM artifacts served at `/wasm/:worldID/:cpID/*`
- `events/bus.go:24-48` — SubscribeGlobal/UnsubscribeGlobal with proper cleanup
- `db/db.go:81-120` — `GetCheckpointAncestry` already implemented (root-to-current chain)
- `auth.go:205` — HandleCallback redirects to `/` after login
- `auth/middleware.go:24` — Unauthenticated users redirected to `/auth/github/login`
- Echo uses `c.Response().Writer` (http.ResponseWriter) and `c.Request()` (*http.Request) — compatible with `datastar.NewSSE(w, r)`
- `Checkpoint.ServerPort` (sql.NullInt64) already populated by `BuildCheckpoint` at `claude.go:148-151` — no need to call `GameServers.Connect` for the port

## Desired End State

A working end-to-end UI where:
1. Unauthenticated users see a login page at `/`
2. Pending users see a waiting page at `/auth/pending`
3. Approved users see a lobby with world list and global chat at `/`
4. Clicking a world opens the game view with iframe + overlay
5. Chat messages sent by one user appear in all connected browsers via SSE
6. Build status updates flow in real-time (editing -> compiling -> ready -> failed)
7. Admins can manage users at `/admin/users`

### Verification
```bash
cd harness && just generate && go build ./...
```
Manual: OAuth login -> lobby -> create world -> enter world -> submit prompt -> see build status -> chat

## What We're NOT Doing

- No Tailwind CSS — plain CSS only
- No NATS/JetStream — we use the in-memory EventBus
- No hot reload / dev tooling — just `just dev` + manual refresh
- No WebSocket fallback — SSE only (Datastar default)
- No JavaScript framework — vanilla JS in `game-loader.js` only where Datastar can't reach (iframe manipulation)
- No DatastarUI component library — raw Datastar attributes with custom CSS

## Decisions (Resolved Open Questions)

1. **Chat message username resolution**: Use a joined sqlc query `GetRecentMessagesWithUser` for chat history (returns `github_username` + `avatar_url`). Real-time messages already carry username/avatar in the EventBus payload.

2. **World creation UX**: Redirect to the new world via `sse.ExecuteScript("window.location.href='/world/'+id")`.

3. **Datastar CDN vs vendored**: Vendor `datastar.js` into `static/datastar.js` for offline reliability. Download from `https://cdn.jsdelivr.net/npm/@starfederation/datastar@1/bundles/datastar.js`.

4. **Chat signal binding**: Use `datastar.ReadSignals(r, &signals)` — the Datastar-idiomatic approach. Signal struct field names match the `data-bind-*` attribute names.

5. **Prompt submission**: Convert `handlePrompt` to use `datastar.ReadSignals` since signals already contain `prompt_text` and `current_checkpoint_id`.

## Datastar-Go SDK API (Verified)

```go
import "github.com/starfederation/datastar-go/datastar"

sse := datastar.NewSSE(w, r)                           // create SSE connection
sse.PatchElementTempl(component, opts...)               // render templ component as SSE patch
sse.MarshalAndPatchSignals(signals)                     // update client-side signals
sse.PatchElements(htmlString, opts...)                  // raw HTML patch
sse.ExecuteScript(js)                                   // execute JS on client
sse.ConsoleError(err)                                   // send error to browser console
datastar.WithSelectorID("id")                           // target by ID
datastar.WithModeAppend()                               // append mode (for chat messages)
datastar.ReadSignals(r, &signals)                       // read signals from request
datastar.GetSSE("/path"), datastar.PostSSE("/path")     // generate data-init/data-on-click attrs
```

**Reference examples**: `context/northstar/` (SSE patterns), `context/datastarui/` (signal/expression patterns)

## File Structure

Each view is a directory (its own Go package) with colocated `signals.go` and `expressions.go` where needed, following the datastarui component pattern. Signal structs live in `signals.go` (not inline in templ) so SSE handlers can import them.

```
harness/
├── views/
│   ├── layout/
│   │   └── layout.templ                    # Base HTML shell (vendored Datastar, CSS link)
│   ├── login/
│   │   └── login.templ                     # "Sign in with GitHub"
│   ├── pending/
│   │   └── pending.templ                   # "Waiting for approval"
│   ├── lobby/
│   │   ├── lobby.templ                     # World browser + global chat
│   │   └── signals.go                      # LobbySignals (chat_text)
│   ├── world/
│   │   ├── world.templ                     # Game iframe + overlay container (standalone page)
│   │   ├── overlay.templ                   # Expanded/minimized overlay chrome
│   │   ├── checkpoint_tree.templ           # Tree side panel
│   │   ├── lineage.templ                   # Checkpoint ancestry view
│   │   ├── signals.go                      # OverlaySignals (snake_case JSON tags)
│   │   ├── expressions.go                  # Overlay interaction expression builders
│   │   └── helpers.go                      # truncateStr, timeAgo
│   ├── chat/
│   │   └── chat.templ                      # ChatPanel + SSE fragments (ChatMessage, SystemNotification, BuildReadyNotification)
│   └── admin/
│       └── admin.templ                     # Admin user management
├── static/
│   ├── styles.css                          # All CSS
│   ├── game-loader.js                      # loadCheckpoint(), loadLineage()
│   └── datastar.js                         # Vendored Datastar bundle
└── internal/
    ├── server/
    │   ├── server.go                       # MODIFY: handlers return templ views, add SSE routes
    │   ├── events.go                       # NEW: SSE handlers (world + global) with heartbeat
    │   └── render.go                       # NEW: templ-to-echo helper
    └── db/queries/
        └── messages.sql                    # MODIFY: add GetRecentMessagesWithUser
```

**Package imports** (one-directional, no cycles):
- `internal/server` imports `views/layout`, `views/login`, `views/lobby`, `views/world`, `views/chat`, `views/admin`
- `views/world` imports `views/chat` (for `@chat.ChatPanel()`)
- `views/lobby`, `views/world`, `views/admin` import `internal/db/sqlc`
- No view package imports `internal/server`

---

## Phase 1: Foundation — Dependencies, Layout, Login, Lobby

### Step 1.1: Add Dependencies

```bash
cd harness && go get github.com/a-h/templ github.com/starfederation/datastar-go/datastar
```

### Step 1.2: Vendor Datastar JS

```bash
curl -o harness/static/datastar.js https://cdn.jsdelivr.net/npm/@starfederation/datastar@1/bundles/datastar.js
```

### Step 1.3: Create `internal/server/render.go`

```go
package server

import (
    "github.com/a-h/templ"
    "github.com/labstack/echo/v4"
)

func render(c echo.Context, component templ.Component) error {
    c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
    return component.Render(c.Request().Context(), c.Response().Writer)
}
```

### Step 1.4: Validate Echo SSE Compatibility

Before building the full implementation, add a minimal SSE test endpoint to verify Echo's middleware (logger, recovery) doesn't interfere with long-lived streaming:

```go
// In server.go — temporary, remove after validation
func (s *Server) handleSSETest(c echo.Context) error {
    sse := datastar.NewSSE(c.Response().Writer, c.Request())
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            if err := sse.MarshalAndPatchSignals(map[string]any{"tick": time.Now().Unix()}); err != nil {
                return nil
            }
        case <-c.Request().Context().Done():
            return nil
        }
    }
}
```

Test with: `curl -N http://localhost:8080/sse-test` — should see SSE events every second. If Echo's logger blocks, add a `Skipper` to the logger middleware for SSE paths:
```go
middleware.LoggerWithConfig(middleware.LoggerConfig{
    Skipper: func(c echo.Context) bool {
        return strings.HasSuffix(c.Path(), "/events") || c.Path() == "/sse-test"
    },
})
```

### Step 1.5: Create `views/layout/layout.templ`

Base HTML shell used by login, pending, lobby, and admin pages. Loads vendored Datastar and links static CSS.

```go
package layout

templ Base(title string) {
    <!DOCTYPE html>
    <html lang="en">
        <head>
            <meta charset="UTF-8"/>
            <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
            <title>{ title } — Creative Mode</title>
            <script type="module" defer src="/static/datastar.js"></script>
            <link rel="stylesheet" href="/static/styles.css"/>
        </head>
        <body>
            { children... }
        </body>
    </html>
}
```

### Step 1.6: Create `views/login/login.templ`

```go
package login

import "creative-mode/harness/views/layout"

templ Page() {
    @layout.Base("Login") {
        <div class="login-container">
            <h1>Creative Mode</h1>
            <p>Collaborative game development with AI</p>
            <a href="/auth/github/login" class="btn btn-primary">Sign in with GitHub</a>
        </div>
    }
}
```

### Step 1.7: Create `views/pending/pending.templ`

```go
package pending

import (
    "creative-mode/harness/internal/db/sqlc"
    "creative-mode/harness/views/layout"
)

templ Page(user *sqlc.User) {
    @layout.Base("Pending Approval") {
        <div class="pending-container">
            if user.AvatarURL.Valid {
                <img src={ user.AvatarURL.String } class="avatar" alt="avatar"/>
            }
            <h2>Welcome, { user.GitHubUsername }</h2>
            <p>Your request to join has been submitted. An admin will approve your access.</p>
        </div>
    }
}
```

### Step 1.8: Create `views/lobby/signals.go`

```go
package lobby

// LobbySignals defines the reactive signals for the lobby page.
// Used by the chat input binding and SSE connection.
type LobbySignals struct {
    ChatText string `json:"chat_text"`
}

func DefaultLobbySignals() LobbySignals {
    return LobbySignals{}
}
```

### Step 1.9: Create `views/lobby/lobby.templ`

```go
package lobby

import (
    "creative-mode/harness/internal/db/sqlc"
    "creative-mode/harness/views/layout"
    "github.com/starfederation/datastar-go/datastar"
)

templ Page(user *sqlc.User, worlds []sqlc.World) {
    @layout.Base("Lobby") {
        <div class="lobby"
             data-signals={ templ.JSONString(DefaultLobbySignals()) }>
            <header class="lobby-header">
                <h1>Creative Mode</h1>
                <div class="user-info">
                    if user.AvatarURL.Valid {
                        <img src={ user.AvatarURL.String } class="avatar-sm" alt=""/>
                    }
                    <span>{ user.GitHubUsername }</span>
                    if user.Role == "admin" {
                        <a href="/admin/users" class="btn btn-sm">Admin</a>
                    }
                    <form method="POST" action="/auth/logout" style="display:inline">
                        <button type="submit" class="btn btn-sm btn-muted">Logout</button>
                    </form>
                </div>
            </header>
            <div class="lobby-content">
                <div class="worlds-panel">
                    <h2>Worlds</h2>
                    <div class="world-list">
                        for _, w := range worlds {
                            <a href={ templ.SafeURL("/world/" + w.ID) } class="world-card">
                                <h3>{ w.Name }</h3>
                                if w.Description.Valid {
                                    <p>{ w.Description.String }</p>
                                }
                            </a>
                        }
                    </div>
                    <form class="create-world-form">
                        <input name="name" placeholder="World name" required/>
                        <input name="description" placeholder="Description (optional)"/>
                        <button type="submit"
                                data-on-click={ datastar.PostSSE("/world/create") + "; evt.preventDefault()" }
                                data-indicator-fetching
                                data-attr-disabled="$fetching">
                            Create World
                        </button>
                    </form>
                </div>
                <div class="chat-panel" id="chat-panel"
                     data-init={ "@get('/events',{requestCancellation: 'disabled'})" }>
                    <h3>Global Chat</h3>
                    <div id="chat-log" class="message-log"></div>
                    <div class="chat-input-bar">
                        <input placeholder="Type a message..."
                               data-bind-chat_text/>
                        <button data-on-click={ datastar.PostSSE("/api/chat") }
                                data-indicator-fetching
                                data-attr-disabled="$fetching">Send</button>
                    </div>
                </div>
            </div>
        </div>
    }
}
```

### Step 1.10: Create `static/styles.css`

Minimal CSS covering: login container, lobby layout, world cards, chat panel, buttons, avatars. See existing plan at `thoughts/CoreyCole/plans/component-6-ui-overlay-chat.md` for the full CSS spec. Start with login + lobby styles; overlay/game styles added in Phase 2.

### Step 1.11: Update `server.go` Handlers

Modify existing handlers to render templ views instead of JSON.

**New handler — `handleRoot`**: Soft session check — render Login, Pending, or Lobby based on auth state:
```go
func (s *Server) handleRoot(c echo.Context) error {
    // Try to get user from session (don't redirect on failure)
    cookie, err := c.Cookie("session")
    if err != nil || cookie.Value == "" {
        return render(c, login.Page())
    }
    ctx := c.Request().Context()
    session, err := s.DB.GetSession(ctx, cookie.Value)
    if err != nil {
        return render(c, login.Page())
    }
    user, err := s.DB.GetUserByID(ctx, session.UserID)
    if err != nil {
        return render(c, login.Page())
    }
    if user.Role == "pending" {
        return render(c, pending.Page(&user))
    }
    worlds, _ := s.DB.ListWorlds(ctx)
    return render(c, lobby.Page(&user, worlds))
}
```

**Modify `HandlePendingApproval`** in `auth.go`: Render `pending.Page(user)` instead of JSON.

**Add routes**:
```go
e.GET("/", s.handleRoot)                          // login or lobby
approved.GET("/events", s.handleGlobalSSE)         // lobby SSE
```

### Step 1.12: Verify Phase 1

```bash
cd harness && just generate && go build ./...
```
Manual: Visit `/` -> see login page -> OAuth -> redirected to `/` -> see lobby with world list.

### Success Criteria — Phase 1

#### Automated
- [ ] `just generate` succeeds (templ + sqlc)
- [ ] `go build ./...` compiles
- [ ] `just lint` passes

#### Manual
- [ ] `/` shows login page when not authenticated
- [ ] OAuth flow redirects back to `/` and shows lobby
- [ ] Lobby shows user avatar, username, logout button
- [ ] Lobby lists existing worlds
- [ ] Pending users see "waiting for approval" page
- [ ] SSE test endpoint streams events without Echo middleware issues

---

## Phase 2: Game View — Iframe + Overlay

### Step 2.1: Create `views/world/signals.go`

Signal definitions colocated with the world view. Uses snake_case JSON tags per datastarui conventions.

```go
package world

// OverlaySignals defines all reactive signals for the game overlay.
// These are initialized on the harness-overlay element via data-signals
// and updated by the SSE handler via MarshalAndPatchSignals.
type OverlaySignals struct {
    CurrentWorldID      string `json:"current_world_id"`
    CurrentCheckpointID string `json:"current_checkpoint_id"`
    BuildStatus         string `json:"build_status"`
    PromptText          string `json:"prompt_text"`
    ChatText            string `json:"chat_text"`
    OverlayExpanded     bool   `json:"overlay_expanded"`
    ActiveTab           string `json:"active_tab"`
    ShowCheckpointTree  bool   `json:"show_checkpoint_tree"`
    UnreadCount         int    `json:"unread_count"`
    RateLimitRetryAt    int64  `json:"rate_limit_retry_at"`
}

func DefaultOverlaySignals(worldID, cpID string) OverlaySignals {
    return OverlaySignals{
        CurrentWorldID:      worldID,
        CurrentCheckpointID: cpID,
        BuildStatus:         "idle",
        OverlayExpanded:     true,
        ActiveTab:           "global",
    }
}
```

### Step 2.2: Create `views/world/expressions.go`

Expression builders for overlay interactions, following the datastarui handler pattern.

```go
package world

import "fmt"

// LoadCheckpointExpr returns a data-on-click expression that calls
// the loadCheckpoint JS function with the given IDs.
// Uses data-* attributes + evt.target to avoid XSS risk from fmt.Sprintf in onclick.
func LoadCheckpointExpr(worldID, cpID string) string {
    return fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cpID)
}

// LoadLineageExpr returns a data-on-click expression that reads
// current signal values and calls loadLineage.
func LoadLineageExpr() string {
    return "loadLineage($current_world_id, $current_checkpoint_id)"
}
```

### Step 2.3: Create `views/world/helpers.go`

```go
package world

import (
    "fmt"
    "time"
)

func TruncateStr(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}

func TimeAgo(t time.Time) string {
    d := time.Since(t)
    switch {
    case d < time.Minute:
        return "just now"
    case d < time.Hour:
        return fmt.Sprintf("%dm ago", int(d.Minutes()))
    case d < 24*time.Hour:
        return fmt.Sprintf("%dh ago", int(d.Hours()))
    default:
        return fmt.Sprintf("%dd ago", int(d.Hours()/24))
    }
}
```

### Step 2.4: Create `views/chat/chat.templ`

Shared chat components used by both lobby (global SSE) and world (overlay SSE) handlers. This is a separate package so both `views/world` and `internal/server/events.go` can import it without cycles.

```go
package chat

import (
    "github.com/starfederation/datastar-go/datastar"
)

// ChatPanel renders the tabbed chat UI for the world overlay.
// Lobby uses its own simpler inline chat panel.
templ ChatPanel() {
    <div class="chat-panel">
        <div class="tab-bar">
            <button data-on-click="$active_tab = 'global'"
                    data-class="{'tab-active': $active_tab === 'global'}">Global</button>
            <button data-on-click="$active_tab = 'world'"
                    data-class="{'tab-active': $active_tab === 'world'}">World</button>
            <button data-on-click="$active_tab = 'lineage'; loadLineage($current_world_id, $current_checkpoint_id)"
                    data-class="{'tab-active': $active_tab === 'lineage'}">Lineage</button>
        </div>
        <div id="chat-log" class="message-log"></div>
        <div id="lineage-view" class="message-log" data-show="$active_tab === 'lineage'"></div>
        <div class="chat-input-bar" data-show="$active_tab !== 'lineage'">
            <input placeholder="Type a message..." data-bind-chat_text/>
            <button data-on-click={ datastar.PostSSE("/api/chat") }
                    data-indicator-fetching
                    data-attr-disabled="$fetching">Send</button>
        </div>
    </div>
}

// SSE fragment: individual chat message (appended to #chat-log)
templ Message(username, avatarURL, content, timestamp string) {
    <div class="message chat-message">
        if avatarURL != "" {
            <img src={ avatarURL } class="avatar-xs" alt=""/>
        }
        <span class="username">{ username }</span>
        <span class="content">{ content }</span>
        <time class="ts">{ timestamp }</time>
    </div>
}

// SSE fragment: system notification
templ SystemNotification(content string) {
    <div class="message system-notification">
        <span class="sys-badge">[sys]</span>
        <span class="content">{ content }</span>
    </div>
}

// SSE fragment: build ready with play button
// Uses data-on-click instead of onclick to avoid XSS risk from string interpolation.
templ BuildReadyNotification(worldID, cpID, worldName string) {
    <div class="message system-notification build-ready">
        <span class="sys-badge">[build]</span>
        <span class="content">Build ready in { worldName }</span>
        <button class="play-btn"
                data-world-id={ worldID }
                data-cp-id={ cpID }
                data-on-click="loadCheckpoint(evt.target.dataset.worldId, evt.target.dataset.cpId)">
            Play
        </button>
    </div>
}
```

### Step 2.5: Create `views/world/world.templ`

Standalone HTML page (not using layout — different structure with full-screen iframe).

**Key fix**: No import of `internal/server` — signals defined in this package. Server port read from DB checkpoint, not from `GameServers.Connect`.

```go
package world

import (
    "fmt"
    "creative-mode/harness/internal/db/sqlc"
)

templ Page(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User, signals OverlaySignals, serverPort int) {
    <!DOCTYPE html>
    <html lang="en">
        <head>
            <meta charset="UTF-8"/>
            <title>{ w.Name } — Creative Mode</title>
            <script type="module" defer src="/static/datastar.js"></script>
            <link rel="stylesheet" href="/static/styles.css"/>
        </head>
        <body>
            <iframe id="game-frame"
                    src={ fmt.Sprintf("/wasm/%s/%s/index.html?server_port=%d", w.ID, cp.ID, serverPort) }
                    class="game-iframe">
            </iframe>
            <div id="harness-overlay"
                 data-signals={ templ.JSONString(signals) }
                 data-init={ fmt.Sprintf("@get('/world/%s/events',{requestCancellation: 'disabled'})", w.ID) }>
                @Overlay(w, cp, user)
            </div>
            <script src="/static/game-loader.js"></script>
        </body>
    </html>
}
```

### Step 2.6: Create `views/world/overlay.templ`

Two-state overlay: expanded and minimized. Signal references use snake_case (`$overlay_expanded`, `$build_status`, etc.).

```go
package world

import (
    "fmt"
    "creative-mode/harness/internal/db/sqlc"
    "creative-mode/harness/views/chat"
    "github.com/starfederation/datastar-go/datastar"
)

templ Overlay(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User) {
    // Expanded state
    <div class="overlay-expanded" data-show="$overlay_expanded">
        @OverlayTopBar(w, cp, user)
        <div class="overlay-middle">
            <div class="game-area"></div>
            @chat.ChatPanel()
        </div>
        @OverlayBottomBar(w)
    </div>
    // Minimized state
    <div class="overlay-minimized" data-show="!$overlay_expanded">
        <button data-on-click="$overlay_expanded = true; $unread_count = 0">
            CM
            <span class="badge" data-show="$unread_count > 0" data-text="$unread_count"></span>
        </button>
    </div>
}

templ OverlayTopBar(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User) {
    <div class="overlay-bar overlay-top-bar">
        <div class="top-bar-left">
            <span class="brand">Creative Mode</span>
            <span class="world-name">{ w.Name }</span>
        </div>
        <div class="top-bar-right">
            <button class="btn btn-sm" data-on-click="$show_checkpoint_tree = !$show_checkpoint_tree">Tree</button>
            <a href="/" class="btn btn-sm btn-muted">Lobby</a>
            <button class="btn btn-sm btn-muted" data-on-click="$overlay_expanded = false">—</button>
        </div>
    </div>
}

templ OverlayBottomBar(w sqlc.World) {
    <div class="overlay-bar overlay-bottom-bar">
        <div class="prompt-bar">
            <input placeholder="Describe what to build..."
                   data-bind-prompt_text
                   class="prompt-input"/>
            <button class="btn btn-primary"
                    data-on-click={ datastar.PostSSE(fmt.Sprintf("/world/%s/prompt", w.ID)) }
                    data-indicator-fetching
                    data-attr-disabled="$fetching">
                Build
            </button>
        </div>
        <div class="status-bar">
            <span class="build-status"
                  data-text="$build_status"
                  data-class="{
                      'status-idle': $build_status === 'idle',
                      'status-editing': $build_status === 'editing',
                      'status-compiling': $build_status === 'compiling',
                      'status-ready': $build_status === 'ready',
                      'status-failed': $build_status === 'failed',
                      'status-rate-limited': $build_status === 'rate_limited'
                  }"></span>
        </div>
    </div>
}
```

### Step 2.7: Create `static/game-loader.js`

**Key fix**: `loadLineage()` accepts signal values as arguments (passed from `data-on-click` expression) instead of reading stale `dataset.signals`.

```javascript
// loadCheckpoint navigates to a checkpoint. Cross-world loads do a full page nav.
window.loadCheckpoint = function(worldID, checkpointID) {
    // Always do a full page load for now (cross-world or same-world).
    // Could optimize same-world case later with iframe src swap + signal update.
    window.location.href = '/world/' + worldID;
};

// loadLineage fetches the checkpoint ancestry and renders it into the lineage view.
// Called from data-on-click="loadLineage($current_world_id, $current_checkpoint_id)"
// so signal values are passed as arguments (not read from stale DOM attributes).
window.loadLineage = function(worldID, cpID) {
    if (!worldID || !cpID) return;

    fetch('/world/' + worldID + '/lineage/' + cpID)
        .then(function(r) { return r.text(); })
        .then(function(html) {
            document.getElementById('lineage-view').innerHTML = html;
        });
};
```

### Step 2.8: Update `handleWorldView` in `server.go`

**Key fix**: Read `server_port` from the checkpoint's DB column (already populated by `BuildCheckpoint`). Do NOT call `GameServers.Connect` — that would leak refcounts since there's no matching `Disconnect` in a regular page handler.

```go
func (s *Server) handleWorldView(c echo.Context) error {
    ctx := c.Request().Context()
    user, err := requireUser(c)
    if err != nil {
        return err
    }
    worldID := c.Param("worldID")

    w, err := s.DB.GetWorld(ctx, worldID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return echo.NewHTTPError(http.StatusNotFound, "world not found")
        }
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to get world")
    }

    cpID, _ := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)
    if cpID == "" {
        // Get root checkpoint
        checkpoints, _ := s.WorldManager.GetCheckpointTree(ctx, worldID)
        if len(checkpoints) > 0 {
            cpID = checkpoints[0].ID
        }
    }

    cp, err := s.DB.GetCheckpoint(ctx, cpID)
    if err != nil {
        return echo.NewHTTPError(http.StatusNotFound, "checkpoint not found")
    }

    // Read server port from DB — already stored by BuildCheckpoint (claude.go:148-151).
    // Do NOT call GameServers.Connect here — no matching Disconnect would leak refcounts.
    serverPort := 0
    if cp.ServerPort.Valid {
        serverPort = int(cp.ServerPort.Int64)
    }

    signals := world.DefaultOverlaySignals(worldID, cpID)
    return render(c, world.Page(w, cp, user, signals, serverPort))
}
```

### Success Criteria — Phase 2

#### Automated
- [ ] `just generate && go build ./...` compiles (no circular imports!)

#### Manual
- [ ] Clicking a world in lobby opens `/world/:id` with game iframe + overlay
- [ ] Overlay shows top bar (world name, lobby link, minimize button)
- [ ] Bottom bar shows prompt input and build button
- [ ] Minimize/expand toggle works
- [ ] Chat panel shows tabs (Global/World/Lineage)

---

## Phase 3: SSE + Real-Time Chat

### Step 3.1: Add `GetRecentMessagesWithUser` sqlc query

Add to `internal/db/queries/messages.sql`:

```sql
-- name: GetRecentMessagesWithUser :many
SELECT m.id, m.type, m.user_id, m.world_id, m.checkpoint_id, m.content, m.created_at,
       u.github_username, u.avatar_url
FROM messages m
LEFT JOIN users u ON m.user_id = u.id
ORDER BY m.created_at DESC LIMIT ?;
```

Run `just generate` to regenerate sqlc types.

### Step 3.2: Create `internal/server/events.go`

Two SSE handlers: one for the world view (global + world events), one for the lobby (global only). Both include a 30-second heartbeat ticker and `ConsoleError` fallback per northstar patterns.

```go
package server

import (
    "log/slog"
    "time"

    "github.com/labstack/echo/v4"
    "github.com/starfederation/datastar-go/datastar"

    "creative-mode/harness/views/chat"
)

const sseHeartbeatInterval = 30 * time.Second

// handleWorldSSE streams global + world-specific events to the overlay.
func (s *Server) handleWorldSSE(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)
    worldID := c.Param("worldID")
    user, _ := requireUser(c)

    globalCh := s.EventBus.SubscribeGlobal()
    defer s.EventBus.UnsubscribeGlobal(globalCh)
    worldCh := s.EventBus.Subscribe(worldID)
    defer s.EventBus.Unsubscribe(worldID, worldCh)

    heartbeat := time.NewTicker(sseHeartbeatInterval)
    defer heartbeat.Stop()

    // Send recent message history (with usernames from joined query)
    ctx := r.Context()
    recentMsgs, _ := s.DB.GetRecentMessagesWithUser(ctx, 50)
    for i := len(recentMsgs) - 1; i >= 0; i-- {
        msg := recentMsgs[i]
        if err := sse.PatchElementTempl(
            chat.Message(msg.GithubUsername, msg.AvatarUrl.String, msg.Content, msg.CreatedAt.Format("15:04")),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE error", "err", err)
            }
            return nil
        }
    }

    // Announce player joined
    if user != nil {
        s.EventBus.PublishGlobal(map[string]any{
            "event": "player.joined", "username": user.GitHubUsername,
            "worldID": worldID,
        })
    }

    for {
        select {
        case event := <-globalCh:
            s.handleGlobalEvent(sse, event)
        case event := <-worldCh:
            s.handleWorldEvent(sse, event)
        case <-heartbeat.C:
            // Keepalive — prevents proxies/browsers from closing idle connections.
            if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
                return nil
            }
        case <-ctx.Done():
            if user != nil {
                s.EventBus.PublishGlobal(map[string]any{
                    "event": "player.left", "username": user.GitHubUsername,
                    "worldID": worldID,
                })
            }
            return nil
        }
    }
}

// handleGlobalSSE streams global-only events for the lobby.
func (s *Server) handleGlobalSSE(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)

    globalCh := s.EventBus.SubscribeGlobal()
    defer s.EventBus.UnsubscribeGlobal(globalCh)

    heartbeat := time.NewTicker(sseHeartbeatInterval)
    defer heartbeat.Stop()

    ctx := r.Context()
    recentMsgs, _ := s.DB.GetRecentMessagesWithUser(ctx, 50)
    for i := len(recentMsgs) - 1; i >= 0; i-- {
        msg := recentMsgs[i]
        if err := sse.PatchElementTempl(
            chat.Message(msg.GithubUsername, msg.AvatarUrl.String, msg.Content, msg.CreatedAt.Format("15:04")),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE error", "err", err)
            }
            return nil
        }
    }

    for {
        select {
        case event := <-globalCh:
            s.handleGlobalEvent(sse, event)
        case <-heartbeat.C:
            if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
                return nil
            }
        case <-ctx.Done():
            return nil
        }
    }
}

// handleGlobalEvent processes a global event and sends SSE patches.
func (s *Server) handleGlobalEvent(sse *datastar.SSE, event any) {
    e, ok := event.(map[string]any)
    if !ok {
        return
    }
    eventType, _ := e["event"].(string)
    switch eventType {
    case "chat.message":
        username, _ := e["username"].(string)
        avatar, _ := e["avatar"].(string)
        content, _ := e["content"].(string)
        ts, _ := e["ts"].(string)
        if err := sse.PatchElementTempl(
            chat.Message(username, avatar, content, ts),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE chat error", "err", err)
            }
        }
    case "player.joined":
        username, _ := e["username"].(string)
        if err := sse.PatchElementTempl(
            chat.SystemNotification(username+" joined"),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE notification error", "err", err)
            }
        }
    case "player.left":
        username, _ := e["username"].(string)
        if err := sse.PatchElementTempl(
            chat.SystemNotification(username+" left"),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE notification error", "err", err)
            }
        }
    }
}

// handleWorldEvent processes a world-specific event and sends SSE patches.
// Signal keys use snake_case to match OverlaySignals JSON tags.
func (s *Server) handleWorldEvent(sse *datastar.SSE, event any) {
    e, ok := event.(map[string]any)
    if !ok {
        return
    }
    eventType, _ := e["event"].(string)
    switch eventType {
    case "claude.tool_use.pre":
        if err := sse.MarshalAndPatchSignals(map[string]any{"build_status": "editing"}); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE signal error", "err", err)
            }
        }
    case "claude.session_stopped":
        if err := sse.MarshalAndPatchSignals(map[string]any{"build_status": "compiling"}); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE signal error", "err", err)
            }
        }
    case "build.completed":
        if err := sse.MarshalAndPatchSignals(map[string]any{"build_status": "ready"}); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE signal error", "err", err)
            }
        }
        worldID, _ := e["worldID"].(string)
        cpID, _ := e["cpID"].(string)
        worldName, _ := e["worldName"].(string)
        if err := sse.PatchElementTempl(
            chat.BuildReadyNotification(worldID, cpID, worldName),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE build notification error", "err", err)
            }
        }
    case "build.failed":
        if err := sse.MarshalAndPatchSignals(map[string]any{"build_status": "failed"}); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE signal error", "err", err)
            }
        }
        errMsg, _ := e["error"].(string)
        if err := sse.PatchElementTempl(
            chat.SystemNotification("Build failed: "+errMsg),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        ); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE build error notification", "err", err)
            }
        }
    case "claude.rate_limited":
        if err := sse.MarshalAndPatchSignals(map[string]any{"build_status": "rate_limited"}); err != nil {
            if cErr := sse.ConsoleError(err); cErr != nil {
                slog.Error("SSE signal error", "err", err)
            }
        }
    }
}
```

### Step 3.3: Register SSE Routes in `server.go`

```go
// In registerWorldRoutes or RegisterRoutes:
approved.GET("/events", s.handleGlobalSSE)
w.GET("/:worldID/events", s.handleWorldSSE)
```

### Step 3.4: Update `handleChatMessage` to use Datastar ReadSignals

Use `datastar.ReadSignals` — the Datastar-idiomatic approach:

```go
func (s *Server) handleChatMessage(c echo.Context) error {
    user, err := requireUser(c)
    if err != nil {
        return err
    }

    type ChatInput struct {
        ChatText string `json:"chat_text"`
    }
    var input ChatInput
    if err := datastar.ReadSignals(c.Request(), &input); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
    }
    if input.ChatText == "" {
        return echo.NewHTTPError(http.StatusBadRequest, "empty message")
    }

    // Persist message
    ctx := c.Request().Context()
    msgID := generateID()
    s.DB.CreateMessage(ctx, sqlc.CreateMessageParams{
        ID:      msgID,
        Type:    "chat",
        UserID:  sql.NullString{String: user.ID, Valid: true},
        Content: input.ChatText,
    })

    // Publish to EventBus for real-time delivery
    s.EventBus.PublishGlobal(map[string]any{
        "event":    "chat.message",
        "username": user.GitHubUsername,
        "avatar":   user.AvatarURL.String,
        "content":  input.ChatText,
        "ts":       time.Now().Format("15:04"),
    })

    // Clear the chat input signal
    sse := datastar.NewSSE(c.Response().Writer, c.Request())
    return sse.MarshalAndPatchSignals(map[string]any{"chat_text": ""})
}
```

### Success Criteria — Phase 3

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Lobby SSE connection established (check Network tab for `/events`)
- [ ] World SSE connection established (check for `/world/:id/events`)
- [ ] Sending a chat message in one browser appears in another browser
- [ ] Player joined/left notifications appear when opening/closing tabs
- [ ] Last 50 messages loaded on connect (with actual usernames, not UUIDs)
- [ ] SSE connections stay alive for > 2 minutes (heartbeat working)

---

## Phase 4: Build Status + Notifications

### Step 4.1: Build Status CSS

Add color-coded build status indicators to `static/styles.css`. Since `data-text` computed values can't be targeted by CSS attribute selectors, use `data-class` (already in overlay.templ Step 2.6):

```css
.status-idle { color: #888; }
.status-editing { color: #f59e0b; }
.status-compiling { color: #3b82f6; }
.status-ready { color: #22c55e; }
.status-failed { color: #ef4444; }
.status-rate-limited { color: #f97316; }
```

### Step 4.2: Verify Build Event Flow

The existing `handleClaudeEvent` already publishes to `EventBus.Publish(worldID, event)`. The world SSE handler in Phase 3 already handles these events. Verify the event types match what the Claude orchestrator publishes:

- `claude.tool_use.pre` -> `build_status: "editing"`
- `claude.session_stopped` -> `build_status: "compiling"`
- `build.completed` -> `build_status: "ready"` + BuildReadyNotification
- `build.failed` -> `build_status: "failed"` + error notification

### Success Criteria — Phase 4

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Submit prompt -> status changes: idle -> editing -> compiling -> ready/failed
- [ ] Build ready notification appears with [Play] button
- [ ] Clicking [Play] reloads the game iframe

---

## Phase 5: Checkpoint Tree + Lineage

### Step 5.1: Create `views/world/checkpoint_tree.templ`

```go
package world

import (
    "creative-mode/harness/internal/db/sqlc"
)

templ CheckpointTree(checkpoints []sqlc.Checkpoint, currentCPID string, worldID string) {
    <div class="checkpoint-tree" data-show="$show_checkpoint_tree">
        <h3>Checkpoints</h3>
        for _, cp := range checkpoints {
            <div class={ "tree-node", templ.KV("tree-node-current", cp.ID == currentCPID) }>
                <span class={ "status-dot", "status-" + cp.Status }></span>
                if cp.Name.Valid {
                    <span class="node-name">{ cp.Name.String }</span>
                } else if cp.Prompt.Valid {
                    <span class="node-prompt">{ TruncateStr(cp.Prompt.String, 40) }</span>
                } else {
                    <span class="node-name">Root</span>
                }
                if cp.ID != currentCPID && cp.Status == "ready" {
                    <button class="btn btn-xs"
                            data-world-id={ worldID }
                            data-cp-id={ cp.ID }
                            data-on-click="loadCheckpoint(evt.target.dataset.worldId, evt.target.dataset.cpId)">
                        Load
                    </button>
                }
            </div>
        }
    </div>
}
```

### Step 5.2: Create `views/world/lineage.templ`

```go
package world

import (
    "creative-mode/harness/internal/db/sqlc"
    "fmt"
)

templ Lineage(ancestry []sqlc.Checkpoint) {
    for _, cp := range ancestry {
        <div class="lineage-entry">
            <div class="lineage-header">
                <span class="cp-id">[{ cp.ID[:8] }]</span>
                if cp.Prompt.Valid {
                    <span class="prompt">"{ TruncateStr(cp.Prompt.String, 60) }"</span>
                } else {
                    <span class="prompt">Starter template</span>
                }
                <time class="ts">{ TimeAgo(cp.CreatedAt) }</time>
            </div>
            if cp.WorkSummary.Valid {
                <div class="lineage-summary">
                    <span class="claude-label">Claude:</span>
                    { cp.WorkSummary.String }
                    if cp.FilesChanged.Valid {
                        <div class="files-changed">Files: { cp.FilesChanged.String }</div>
                    }
                    <div class="build-result">
                        Build: { cp.Status }
                        if cp.BuildDurationMs.Valid {
                            ({ fmt.Sprintf("%dms", cp.BuildDurationMs.Int64) })
                        }
                    </div>
                </div>
            }
        </div>
    }
    <div class="lineage-cursor">you are here</div>
}
```

### Step 5.3: Add `handleLineage` Handler

```go
func (s *Server) handleLineage(c echo.Context) error {
    ctx := c.Request().Context()
    worldID := c.Param("worldID")
    cpID := c.Param("cpID")
    ancestry, err := s.DB.GetCheckpointAncestry(ctx, worldID, cpID)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to get lineage")
    }
    return render(c, world.Lineage(ancestry))
}
```

Register route: `w.GET("/:worldID/lineage/:cpID", s.handleLineage)`

### Success Criteria — Phase 5

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Tree button toggles checkpoint tree panel
- [ ] Tree shows all checkpoints with status indicators
- [ ] Current checkpoint is highlighted
- [ ] Load button switches to that checkpoint
- [ ] Lineage tab shows ancestry from root to current

---

## Phase 6: Admin Page

### Step 6.1: Create `views/admin/admin.templ`

```go
package admin

import (
    "creative-mode/harness/internal/db/sqlc"
    "creative-mode/harness/views/layout"
    "github.com/starfederation/datastar-go/datastar"
)

templ Page(users []sqlc.User) {
    @layout.Base("Admin") {
        <div class="admin-container">
            <header class="admin-header">
                <h1>Admin — User Management</h1>
                <a href="/" class="btn btn-sm btn-muted">Back to Lobby</a>
            </header>
            <div class="user-list">
                for _, u := range users {
                    <div class="user-row">
                        if u.AvatarURL.Valid {
                            <img src={ u.AvatarURL.String } class="avatar-sm" alt=""/>
                        }
                        <span class="username">{ u.GitHubUsername }</span>
                        <span class={ "role-badge", "role-" + u.Role }>{ u.Role }</span>
                        if u.Role == "pending" {
                            <button class="btn btn-sm btn-primary"
                                    data-on-click={ datastar.PostSSE("/admin/users/" + u.ID + "/approve") }
                                    data-indicator-fetching
                                    data-attr-disabled="$fetching">
                                Approve
                            </button>
                            <button class="btn btn-sm btn-danger"
                                    data-on-click={ datastar.PostSSE("/admin/users/" + u.ID + "/reject") }
                                    data-indicator-fetching
                                    data-attr-disabled="$fetching">
                                Reject
                            </button>
                        }
                    </div>
                }
            </div>
        </div>
    }
}
```

### Step 6.2: Update `HandleAdminUsers` in `server.go`

Move admin page rendering to `server.go`, keep auth logic in `auth.go`:

```go
// In server.go
func (s *Server) handleAdminUsers(c echo.Context) error {
    ctx := c.Request().Context()
    users, err := s.DB.ListUsers(ctx)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to list users")
    }
    return render(c, admin.Page(users))
}
```

Update route registration: `admin.GET("/users", s.handleAdminUsers)` (instead of `s.AuthHandler.HandleAdminUsers`)

The approve/reject POST handlers in `auth.go` can stay as-is — after approval, respond with SSE that re-renders the user row or the full user list.

### Success Criteria — Phase 6

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Admin page at `/admin/users` shows user list
- [ ] Pending users have Approve/Reject buttons
- [ ] Approve/Reject buttons work

---

## Testing Strategy

### Automated
- `just generate` — templ compilation + sqlc generation
- `go build ./...` — full build
- `just lint` — linting

### Manual Testing Steps
1. Visit `/` without auth -> see login page
2. OAuth login -> land on lobby with worlds
3. Create a world -> redirected to new world
4. Click world -> game iframe + overlay loads
5. Open second browser -> send chat -> appears in first (with username, not UUID)
6. Submit prompt -> build status updates in real-time
7. Build completes -> [Play] button appears -> click loads new checkpoint
8. Click Tree -> side panel with checkpoints
9. Click Lineage tab -> ancestry view
10. Minimize overlay -> floating button with badge
11. New chat while minimized -> badge increments
12. Expand -> badge resets, messages visible
13. Admin page -> approve/reject users
14. Leave SSE connection idle for > 2 minutes -> verify it stays alive (heartbeat)

## References

- Original component spec: `thoughts/CoreyCole/plans/component-6-ui-overlay-chat.md`
- Master plan: `thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md`
- Staff engineer review: `thoughts/CoreyCole/reviews/2026-02-10_19-12-29_wave4-ui-overlay-chat_review.md`
- SSE patterns: `context/northstar/` (especially `features/counter/`, `features/index/`, `features/monitor/`)
- Datastar patterns: `context/datastarui/` (especially `.cursor/rules/datastar.mdc`, `components/dialog/`, `components/tabs/`)
- Harness CLAUDE.md: `harness/CLAUDE.md` (distilled reference)
