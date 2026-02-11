# Wave 4: UI Overlay + Chat — Refined Implementation Plan

## Overview

Add the HTML presentation layer to the harness server using **templ** (Go HTML templating), **Datastar** (hypermedia framework with SSE), and static CSS/JS. Converts all JSON-returning handlers to templ views and adds real-time SSE event streaming for chat, build status, and notifications.

## Current State Analysis

### What Exists
- Echo server with JSON-only handlers (`internal/server/server.go`)
- Auth middleware chain: `SessionMiddleware` → `ApprovedMiddleware` → `AdminMiddleware`
- EventBus with global + per-world pub/sub (`internal/events/bus.go`)
- All DB queries via sqlc: worlds, checkpoints, messages, users, user_positions
- `handleChatMessage` persists messages and publishes to EventBus
- `handleClaudeEvent` receives Claude hook events and publishes to world-specific bus
- `justfile` already has `templ generate` in the `generate` recipe
- Static file serving configured: `e.Static("/static", "static")`

### What's Missing
- No templ or datastar-go dependencies in `go.mod`
- No `.templ` files, no `views/` directory
- No SSE endpoints (EventBus exists but nothing subscribes from HTTP)
- No `static/styles.css` or `static/game-loader.js`
- No `internal/server/render.go`, `events.go`, or `signals.go`

### Key Discoveries
- `server.go:59-60` — Static serving already configured for `/assets` and `/static`
- `server.go:78-82` — Auth groups: `authed` (session required), `approved` (role check), `admin`
- `server.go:86` — WASM artifacts served at `/wasm/:worldID/:cpID/*`
- `events/bus.go:24-48` — SubscribeGlobal/UnsubscribeGlobal with proper cleanup
- `db/db.go:81-120` — `GetCheckpointAncestry` already implemented (root-to-current chain)
- `auth.go:205` — HandleCallback redirects to `/` after login
- `auth/middleware.go:24` — Unauthenticated users redirected to `/auth/github/login`
- Echo uses `c.Response().Writer` (http.ResponseWriter) and `c.Request()` (*http.Request) — compatible with `datastar.NewSSE(w, r)`

## Desired End State

A working end-to-end UI where:
1. Unauthenticated users see a login page at `/`
2. Pending users see a waiting page at `/auth/pending`
3. Approved users see a lobby with world list and global chat at `/`
4. Clicking a world opens the game view with iframe + overlay
5. Chat messages sent by one user appear in all connected browsers via SSE
6. Build status updates flow in real-time (editing → compiling → ready → failed)
7. Admins can manage users at `/admin/users`

### Verification
```bash
cd harness && just generate && go build ./...
```
Manual: OAuth login → lobby → create world → enter world → submit prompt → see build status → chat

## What We're NOT Doing

- No Tailwind CSS — plain CSS only
- No NATS/JetStream — we use the in-memory EventBus
- No hot reload / dev tooling — just `just dev` + manual refresh
- No WebSocket fallback — SSE only (Datastar default)
- No JavaScript framework — vanilla JS in `game-loader.js` only where Datastar can't reach (iframe manipulation)
- No DatastarUI component library — raw Datastar attributes with custom CSS

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
datastar.GetSSE("/path"), datastar.PostSSE("/path")     // generate data-on-load/click attrs
```

**Reference examples**: `context/northstar/` (SSE patterns), `context/datastarui/` (signal/expression patterns)

## File Structure

```
harness/
├── views/
│   ├── layout.templ                    # Base HTML shell (Datastar CDN script, CSS link)
│   ├── login.templ                     # "Sign in with GitHub"
│   ├── pending.templ                   # "Waiting for approval"
│   ├── admin.templ                     # Admin user management
│   ├── lobby.templ                     # World browser + global chat
│   ├── world.templ                     # Game iframe + overlay container (standalone page)
│   ├── overlay.templ                   # Expanded/minimized overlay chrome
│   ├── chat.templ                      # Chat messages, notifications, tabs (SSE fragments)
│   ├── checkpoint_tree.templ           # Tree side panel
│   └── lineage.templ                   # Checkpoint ancestry view
├── static/
│   ├── styles.css                      # All CSS
│   └── game-loader.js                  # loadCheckpoint(), sendChat(), submitPrompt()
└── internal/server/
    ├── server.go                       # MODIFY: handlers return templ views, add SSE routes
    ├── events.go                       # NEW: SSE handlers (world + global)
    ├── render.go                       # NEW: templ-to-echo helper
    └── signals.go                      # NEW: OverlaySignals struct
```

---

## Phase 1: Foundation — Dependencies, Layout, Login, Lobby

### Step 1.1: Add Dependencies

```bash
cd harness && go get github.com/a-h/templ github.com/starfederation/datastar-go/datastar
```

### Step 1.2: Create `internal/server/render.go`

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

### Step 1.3: Create `views/layout.templ`

Base HTML shell used by login, pending, lobby, and admin pages. Loads Datastar from CDN and links static CSS.

```go
package views

templ Layout(title string) {
    <!DOCTYPE html>
    <html lang="en">
        <head>
            <meta charset="UTF-8"/>
            <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
            <title>{ title } — Creative Mode</title>
            <script type="module" defer src="https://cdn.jsdelivr.net/npm/@starfederation/datastar@1/bundles/datastar.js"></script>
            <link rel="stylesheet" href="/static/styles.css"/>
        </head>
        <body>
            { children... }
        </body>
    </html>
}
```

### Step 1.4: Create `views/login.templ`

```go
package views

templ Login() {
    @Layout("Login") {
        <div class="login-container">
            <h1>Creative Mode</h1>
            <p>Collaborative game development with AI</p>
            <a href="/auth/github/login" class="btn btn-primary">Sign in with GitHub</a>
        </div>
    }
}
```

### Step 1.5: Create `views/pending.templ`

```go
package views

import "creative-mode/harness/internal/db/sqlc"

templ Pending(user *sqlc.User) {
    @Layout("Pending Approval") {
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

### Step 1.6: Create `views/lobby.templ`

```go
package views

import (
    "creative-mode/harness/internal/db/sqlc"
    "github.com/starfederation/datastar-go/datastar"
)

templ Lobby(user *sqlc.User, worlds []sqlc.World) {
    @Layout("Lobby") {
        <div class="lobby">
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
                                data-on-click={ datastar.PostSSE("/world/create", ) + "; evt.preventDefault()" }>
                            Create World
                        </button>
                    </form>
                </div>
                <div class="chat-panel" id="chat-panel"
                     data-on-load={ datastar.GetSSE("/events") }>
                    <h3>Global Chat</h3>
                    <div id="chat-log" class="message-log"></div>
                    <div class="chat-input-bar">
                        <input name="content" placeholder="Type a message..."
                               data-bind-chatText/>
                        <button data-on-click={ datastar.PostSSE("/api/chat") }>Send</button>
                    </div>
                </div>
            </div>
        </div>
    }
}
```

### Step 1.7: Create `static/styles.css`

Minimal CSS covering: login container, lobby layout, world cards, chat panel, buttons, avatars. See existing plan at `thoughts/CoreyCole/plans/component-6-ui-overlay-chat.md` for the full CSS spec. Start with login + lobby styles; overlay/game styles added in Phase 2.

### Step 1.8: Update `server.go` Handlers

Modify existing handlers to render templ views instead of JSON:

**New handler — `handleRoot`**: Render Login or Lobby based on auth state.
- If no auth handler configured: render Login
- Route: `e.GET("/", s.handleRoot)` (before auth middleware, with optional auth check)

Actually, since `SessionMiddleware` redirects unauthenticated users to `/auth/github/login`, we need:
- `e.GET("/", s.handleLogin)` — unauthenticated root shows login page
- `approved.GET("/lobby", s.handleLobby)` — authenticated lobby
- Change `HandleCallback` redirect from `/` to `/lobby`
- OR: make `handleRoot` try to read session cookie without middleware, render Login if no session, Lobby if session exists

**Recommended approach**: Add a `handleRoot` that does a soft session check:
```go
func (s *Server) handleRoot(c echo.Context) error {
    // Try to get user from session (don't redirect on failure)
    cookie, err := c.Cookie("session")
    if err != nil || cookie.Value == "" {
        return render(c, views.Login())
    }
    ctx := c.Request().Context()
    session, err := s.DB.GetSession(ctx, cookie.Value)
    if err != nil {
        return render(c, views.Login())
    }
    user, err := s.DB.GetUserByID(ctx, session.UserID)
    if err != nil {
        return render(c, views.Login())
    }
    if user.Role == "pending" {
        return render(c, views.Pending(&user))
    }
    worlds, _ := s.DB.ListWorlds(ctx)
    return render(c, views.Lobby(&user, worlds))
}
```

**Modify `HandlePendingApproval`** in `auth.go`: Render `views.Pending(user)` instead of JSON.

**Add routes**:
```go
e.GET("/", s.handleRoot)                          // login or lobby
approved.GET("/events", s.handleGlobalSSE)         // lobby SSE
```

### Step 1.9: Verify Phase 1

```bash
cd harness && just generate && go build ./...
```
Manual: Visit `/` → see login page → OAuth → redirected to `/` → see lobby with world list.

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

---

## Phase 2: Game View — Iframe + Overlay

### Step 2.1: Create `internal/server/signals.go`

```go
package server

type OverlaySignals struct {
    CurrentWorldID      string `json:"currentWorldId"`
    CurrentCheckpointID string `json:"currentCheckpointId"`
    BuildStatus         string `json:"buildStatus"`
    PromptText          string `json:"promptText"`
    ChatText            string `json:"chatText"`
    OverlayExpanded     bool   `json:"overlayExpanded"`
    ActiveTab           string `json:"activeTab"`
    ShowCheckpointTree  bool   `json:"showCheckpointTree"`
    UnreadCount         int    `json:"unreadCount"`
    RateLimitRetryAt    int64  `json:"rateLimitRetryAt"`
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

### Step 2.2: Create `views/world.templ`

Standalone HTML page (not using Layout — different structure with full-screen iframe):

```go
package views

import (
    "fmt"
    "creative-mode/harness/internal/db/sqlc"
    "creative-mode/harness/internal/server"
    "github.com/starfederation/datastar-go/datastar"
)

templ World(world sqlc.World, cp sqlc.Checkpoint, user *sqlc.User, signals server.OverlaySignals, serverPort int) {
    <!DOCTYPE html>
    <html lang="en">
        <head>
            <meta charset="UTF-8"/>
            <title>{ world.Name } — Creative Mode</title>
            <script type="module" defer src="https://cdn.jsdelivr.net/npm/@starfederation/datastar@1/bundles/datastar.js"></script>
            <link rel="stylesheet" href="/static/styles.css"/>
        </head>
        <body>
            <iframe id="game-frame"
                    src={ fmt.Sprintf("/wasm/%s/%s/index.html?server_port=%d", world.ID, cp.ID, serverPort) }
                    class="game-iframe">
            </iframe>
            <div id="harness-overlay"
                 data-signals={ templ.JSONString(signals) }
                 data-on-load={ datastar.GetSSE(fmt.Sprintf("/world/%s/events", world.ID)) }>
                @Overlay(world, cp, user)
            </div>
            <script src="/static/game-loader.js"></script>
        </body>
    </html>
}
```

### Step 2.3: Create `views/overlay.templ`

Two-state overlay: expanded and minimized.

```go
package views

import (
    "fmt"
    "creative-mode/harness/internal/db/sqlc"
    "github.com/starfederation/datastar-go/datastar"
)

templ Overlay(world sqlc.World, cp sqlc.Checkpoint, user *sqlc.User) {
    // Expanded state
    <div class="overlay-expanded" data-show="$overlayExpanded">
        @OverlayTopBar(world, cp, user)
        <div class="overlay-middle">
            <div class="game-area"></div>
            @ChatPanel()
        </div>
        @OverlayBottomBar(world)
    </div>
    // Minimized state
    <div class="overlay-minimized" data-show="!$overlayExpanded">
        <button data-on-click="$overlayExpanded = true; $unreadCount = 0">
            CM
            <span class="badge" data-show="$unreadCount > 0" data-text="$unreadCount"></span>
        </button>
    </div>
}

templ OverlayTopBar(world sqlc.World, cp sqlc.Checkpoint, user *sqlc.User) {
    <div class="overlay-bar overlay-top-bar">
        <div class="top-bar-left">
            <span class="brand">Creative Mode</span>
            <span class="world-name">{ world.Name }</span>
        </div>
        <div class="top-bar-right">
            <button class="btn btn-sm" data-on-click="$showCheckpointTree = !$showCheckpointTree">Tree</button>
            <a href="/" class="btn btn-sm btn-muted">Lobby</a>
            <button class="btn btn-sm btn-muted" data-on-click="$overlayExpanded = false">—</button>
        </div>
    </div>
}

templ OverlayBottomBar(world sqlc.World) {
    <div class="overlay-bar overlay-bottom-bar">
        <div class="prompt-bar">
            <input placeholder="Describe what to build..."
                   data-bind-promptText
                   class="prompt-input"/>
            <button class="btn btn-primary"
                    data-on-click={ datastar.PostSSE(fmt.Sprintf("/world/%s/prompt", world.ID)) }>
                Build
            </button>
        </div>
        <div class="status-bar">
            <span class="build-status" data-text="$buildStatus"></span>
        </div>
    </div>
}
```

### Step 2.4: Create `views/chat.templ`

Tab bar + message log + chat input, plus SSE fragment components:

```go
package views

import (
    "creative-mode/harness/internal/db/sqlc"
    "fmt"
    "github.com/starfederation/datastar-go/datastar"
)

templ ChatPanel() {
    <div class="chat-panel">
        <div class="tab-bar">
            <button data-on-click="$activeTab = 'global'"
                    data-class="{'tab-active': $activeTab === 'global'}">Global</button>
            <button data-on-click="$activeTab = 'world'"
                    data-class="{'tab-active': $activeTab === 'world'}">World</button>
            <button data-on-click="$activeTab = 'lineage'; loadLineage()"
                    data-class="{'tab-active': $activeTab === 'lineage'}">Lineage</button>
        </div>
        <div id="chat-log" class="message-log"></div>
        <div id="lineage-view" class="message-log" data-show="$activeTab === 'lineage'"></div>
        <div class="chat-input-bar" data-show="$activeTab !== 'lineage'">
            <input placeholder="Type a message..." data-bind-chatText/>
            <button data-on-click={ datastar.PostSSE("/api/chat") }>Send</button>
        </div>
    </div>
}

// SSE fragment: individual chat message (appended to #chat-log)
templ ChatMessage(username, avatarURL, content, timestamp string) {
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
templ BuildReadyNotification(worldID, cpID, worldName string) {
    <div class="message system-notification build-ready">
        <span class="sys-badge">[build]</span>
        <span class="content">Build ready in { worldName }</span>
        <button class="play-btn"
                onclick={ fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cpID) }>
            Play
        </button>
    </div>
}
```

### Step 2.5: Create `static/game-loader.js`

```javascript
window.loadCheckpoint = function(worldID, checkpointID) {
    const currentWorldID = document.body.querySelector('#harness-overlay')
        ? new URLSearchParams(document.getElementById('game-frame').src.split('?')[1] || '').get('world')
        : null;

    // Cross-world: full page load
    window.location.href = '/world/' + worldID;
};

window.loadLineage = function() {
    // Read from signals — Datastar sets these
    const overlay = document.getElementById('harness-overlay');
    if (!overlay) return;
    const worldID = overlay.dataset.signals ? JSON.parse(overlay.dataset.signals).currentWorldId : '';
    const cpID = overlay.dataset.signals ? JSON.parse(overlay.dataset.signals).currentCheckpointId : '';
    if (!worldID || !cpID) return;

    fetch('/world/' + worldID + '/lineage/' + cpID)
        .then(r => r.text())
        .then(html => {
            document.getElementById('lineage-view').innerHTML = html;
        });
};
```

### Step 2.6: Update `handleWorldView` in `server.go`

Convert from JSON to templ rendering:

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

    serverPort := 0
    gs, gsErr := s.WorldManager.ConnectGameServer(ctx, worldID, cpID)
    if gsErr == nil {
        serverPort = gs.Port
    }

    signals := DefaultOverlaySignals(worldID, cpID)
    return render(c, views.World(w, cp, user, signals, serverPort))
}
```

### Success Criteria — Phase 2

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Clicking a world in lobby opens `/world/:id` with game iframe + overlay
- [ ] Overlay shows top bar (world name, lobby link, minimize button)
- [ ] Bottom bar shows prompt input and build button
- [ ] Minimize/expand toggle works
- [ ] Chat panel shows tabs (Global/World/Lineage)

---

## Phase 3: SSE + Real-Time Chat

### Step 3.1: Create `internal/server/events.go`

Two SSE handlers: one for the world view (global + world events), one for the lobby (global only).

```go
package server

import (
    "github.com/labstack/echo/v4"
    "github.com/starfederation/datastar-go/datastar"
    "creative-mode/harness/views"
)

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

    // Send recent message history
    ctx := r.Context()
    recentMsgs, _ := s.DB.GetRecentMessages(ctx, 50)
    for i := len(recentMsgs) - 1; i >= 0; i-- {
        msg := recentMsgs[i]
        username := msg.UserID.String  // TODO: join with users table or store username in message
        sse.PatchElementTempl(
            views.ChatMessage(username, "", msg.Content, msg.CreatedAt.Format("15:04")),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
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

    ctx := r.Context()
    recentMsgs, _ := s.DB.GetRecentMessages(ctx, 50)
    for i := len(recentMsgs) - 1; i >= 0; i-- {
        msg := recentMsgs[i]
        sse.PatchElementTempl(
            views.ChatMessage(msg.UserID.String, "", msg.Content, msg.CreatedAt.Format("15:04")),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    }

    for {
        select {
        case event := <-globalCh:
            s.handleGlobalEvent(sse, event)
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
        _ = sse.PatchElementTempl(
            views.ChatMessage(username, avatar, content, ts),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    case "player.joined":
        username, _ := e["username"].(string)
        _ = sse.PatchElementTempl(
            views.SystemNotification(username+" joined"),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    case "player.left":
        username, _ := e["username"].(string)
        _ = sse.PatchElementTempl(
            views.SystemNotification(username+" left"),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    }
}

// handleWorldEvent processes a world-specific event and sends SSE patches.
func (s *Server) handleWorldEvent(sse *datastar.SSE, event any) {
    e, ok := event.(map[string]any)
    if !ok {
        return
    }
    eventType, _ := e["event"].(string)
    switch eventType {
    case "claude.tool_use.pre":
        _ = sse.MarshalAndPatchSignals(map[string]any{"buildStatus": "editing"})
    case "claude.session_stopped":
        _ = sse.MarshalAndPatchSignals(map[string]any{"buildStatus": "compiling"})
    case "build.completed":
        _ = sse.MarshalAndPatchSignals(map[string]any{"buildStatus": "ready"})
        worldID, _ := e["worldID"].(string)
        cpID, _ := e["cpID"].(string)
        worldName, _ := e["worldName"].(string)
        _ = sse.PatchElementTempl(
            views.BuildReadyNotification(worldID, cpID, worldName),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    case "build.failed":
        _ = sse.MarshalAndPatchSignals(map[string]any{"buildStatus": "failed"})
        errMsg, _ := e["error"].(string)
        _ = sse.PatchElementTempl(
            views.SystemNotification("Build failed: "+errMsg),
            datastar.WithSelectorID("chat-log"),
            datastar.WithModeAppend(),
        )
    case "claude.rate_limited":
        _ = sse.MarshalAndPatchSignals(map[string]any{"buildStatus": "rate_limited"})
    }
}
```

### Step 3.2: Register SSE Routes in `server.go`

```go
// In registerWorldRoutes or RegisterRoutes:
approved.GET("/events", s.handleGlobalSSE)
w.GET("/:worldID/events", s.handleWorldSSE)
```

### Step 3.3: Update `handleChatMessage` to use Datastar ReadSignals

The current `handleChatMessage` reads JSON body. With Datastar, the chat input is bound to `$chatText` signal. The `@post('/api/chat')` action sends signals as JSON body. The current handler uses `c.Bind(&body)` which should work with Datastar's JSON body, but we need to ensure the field name matches the signal name.

**Option A**: Keep using `c.Bind` — Datastar sends `{"chatText": "hello"}`, so change the struct tag.
**Option B**: Use `datastar.ReadSignals` — more idiomatic.

Recommend Option A (simpler): just rename `content` to `chatText` in the bind struct.

### Success Criteria — Phase 3

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Lobby SSE connection established (check Network tab for `/events`)
- [ ] World SSE connection established (check for `/world/:id/events`)
- [ ] Sending a chat message in one browser appears in another browser
- [ ] Player joined/left notifications appear when opening/closing tabs
- [ ] Last 50 messages loaded on connect

---

## Phase 4: Build Status + Notifications

### Step 4.1: Build Status CSS

Add color-coded build status indicators to `static/styles.css`:

```css
.build-status[data-text="idle"] { color: #888; }
.build-status[data-text="editing"] { color: #f59e0b; }
.build-status[data-text="compiling"] { color: #3b82f6; }
.build-status[data-text="ready"] { color: #22c55e; }
.build-status[data-text="failed"] { color: #ef4444; }
.build-status[data-text="rate_limited"] { color: #f97316; }
```

Note: Since `data-text` is a Datastar attribute, CSS attribute selectors won't work on the computed text. Instead, use `data-class` with signal values:

```html
<span class="build-status"
      data-text="$buildStatus"
      data-class="{
          'status-idle': $buildStatus === 'idle',
          'status-editing': $buildStatus === 'editing',
          'status-compiling': $buildStatus === 'compiling',
          'status-ready': $buildStatus === 'ready',
          'status-failed': $buildStatus === 'failed'
      }"></span>
```

### Step 4.2: Verify Build Event Flow

The existing `handleClaudeEvent` already publishes to `EventBus.Publish(worldID, event)`. The world SSE handler in Phase 3 already handles these events. Verify the event types match what the Claude orchestrator publishes:

- `claude.tool_use.pre` → `buildStatus: "editing"`
- `claude.session_stopped` → `buildStatus: "compiling"`
- `build.completed` → `buildStatus: "ready"` + BuildReadyNotification
- `build.failed` → `buildStatus: "failed"` + error notification

### Success Criteria — Phase 4

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Submit prompt → status changes: idle → editing → compiling → ready/failed
- [ ] Build ready notification appears with [Play] button
- [ ] Clicking [Play] reloads the game iframe

---

## Phase 5: Checkpoint Tree + Lineage

### Step 5.1: Create `views/checkpoint_tree.templ`

```go
package views

import (
    "creative-mode/harness/internal/db/sqlc"
    "fmt"
)

templ CheckpointTree(checkpoints []sqlc.Checkpoint, currentCPID string, worldID string) {
    <div class="checkpoint-tree" data-show="$showCheckpointTree">
        <h3>Checkpoints</h3>
        for _, cp := range checkpoints {
            <div class={ "tree-node", templ.KV("tree-node-current", cp.ID == currentCPID) }>
                <span class={ "status-dot", "status-" + cp.Status }></span>
                if cp.Name.Valid {
                    <span class="node-name">{ cp.Name.String }</span>
                } else if cp.Prompt.Valid {
                    <span class="node-prompt">{ truncateStr(cp.Prompt.String, 40) }</span>
                } else {
                    <span class="node-name">Root</span>
                }
                if cp.ID != currentCPID && cp.Status == "ready" {
                    <button class="btn btn-xs"
                            onclick={ fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cp.ID) }>
                        Load
                    </button>
                }
            </div>
        }
    </div>
}
```

### Step 5.2: Create `views/lineage.templ`

```go
package views

import (
    "creative-mode/harness/internal/db/sqlc"
    "fmt"
    "time"
)

templ Lineage(ancestry []sqlc.Checkpoint) {
    for _, cp := range ancestry {
        <div class="lineage-entry">
            <div class="lineage-header">
                <span class="cp-id">[{ cp.ID[:8] }]</span>
                if cp.Prompt.Valid {
                    <span class="prompt">"{ truncateStr(cp.Prompt.String, 60) }"</span>
                } else {
                    <span class="prompt">Starter template</span>
                }
                <time class="ts">{ timeAgo(cp.CreatedAt) }</time>
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

### Step 5.3: Add Helper Functions

In `views/` create a `helpers.go` file with `truncateStr` and `timeAgo`:

```go
package views

import "time"

func truncateStr(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}

func timeAgo(t time.Time) string {
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

### Step 5.4: Add `handleLineage` Handler

```go
func (s *Server) handleLineage(c echo.Context) error {
    ctx := c.Request().Context()
    worldID := c.Param("worldID")
    cpID := c.Param("cpID")
    ancestry, err := s.DB.GetCheckpointAncestry(ctx, worldID, cpID)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to get lineage")
    }
    return render(c, views.Lineage(ancestry))
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

### Step 6.1: Create `views/admin.templ`

```go
package views

import (
    "creative-mode/harness/internal/db/sqlc"
    "github.com/starfederation/datastar-go/datastar"
)

templ Admin(users []sqlc.User) {
    @Layout("Admin") {
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
                                    data-on-click={ datastar.PostSSE("/admin/users/" + u.ID + "/approve") }>
                                Approve
                            </button>
                            <button class="btn btn-sm btn-danger"
                                    data-on-click={ datastar.PostSSE("/admin/users/" + u.ID + "/reject") }>
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

### Step 6.2: Update `HandleAdminUsers` in `auth.go`

Change from JSON to templ rendering. This requires either:
- Moving the handler to `server.go` (has access to `render()`)
- Or passing a render function to the auth handler
- Or adding a `render` method to the auth package

**Recommended**: Move admin page rendering to `server.go`, keep auth logic in `auth.go`:

```go
// In server.go
func (s *Server) handleAdminUsers(c echo.Context) error {
    ctx := c.Request().Context()
    users, err := s.DB.ListUsers(ctx)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to list users")
    }
    return render(c, views.Admin(users))
}
```

Update route registration: `admin.GET("/users", s.handleAdminUsers)` (instead of `s.AuthHandler.HandleAdminUsers`)

The approve/reject POST handlers in `auth.go` can stay as JSON — Datastar SSE will handle the response, or we can have them re-render the user list.

### Success Criteria — Phase 6

#### Automated
- [ ] `just generate && go build ./...` compiles

#### Manual
- [ ] Admin page at `/admin/users` shows user list
- [ ] Pending users have Approve/Reject buttons
- [ ] Approve/Reject buttons work

---

## Open Questions for Discussion

1. **Chat message username resolution**: Currently `handleChatMessage` stores `user_id` in messages, but SSE needs the username for display. Options:
   - Store username in the message table (denormalize)
   - Join with users table when fetching recent messages (new sqlc query)
   - Use the EventBus event payload (already has username) for real-time, and join for history

2. **World creation UX**: The lobby's "Create World" form currently does a Datastar `@post` which returns an SSE patch. Should it redirect to the new world, or re-render the world list?

3. **Datastar CDN vs vendored**: The plan uses CDN. Should we vendor `datastar.js` into `static/` for offline/reliability?

4. **Chat signal binding**: Datastar `data-bind-chatText` creates a `$chatText` signal. When `@post('/api/chat')` fires, it sends all signals. The backend needs to know the field name. Confirm this works with Echo's `c.Bind()` or if we need `datastar.ReadSignals()`.

5. **`handlePrompt` conversion**: The current prompt handler reads `{"prompt": "...", "checkpoint_id": "..."}` via JSON bind. With Datastar, the prompt text is in `$promptText` signal and checkpoint ID is in `$currentCheckpointId`. Should we use `datastar.ReadSignals` or keep form-based submission?

## Testing Strategy

### Automated
- `just generate` — templ compilation
- `go build ./...` — full build
- `just lint` — linting

### Manual Testing Steps
1. Visit `/` without auth → see login page
2. OAuth login → land on lobby with worlds
3. Create a world → appears in list
4. Click world → game iframe + overlay loads
5. Open second browser → send chat → appears in first
6. Submit prompt → build status updates in real-time
7. Build completes → [Play] button appears → click loads new checkpoint
8. Click Tree → side panel with checkpoints
9. Click Lineage tab → ancestry view
10. Minimize overlay → floating button with badge
11. New chat while minimized → badge increments
12. Expand → badge resets, messages visible
13. Admin page → approve/reject users

## References

- Original component spec: `thoughts/CoreyCole/plans/component-6-ui-overlay-chat.md`
- Master plan: `thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md`
- SSE patterns: `context/northstar/` (especially `features/counter/`, `features/index/`, `features/monitor/`)
- Datastar patterns: `context/datastarui/` (especially `.cursor/rules/datastar.mdc`)
- Harness CLAUDE.md: `harness/CLAUDE.md` (distilled reference)
