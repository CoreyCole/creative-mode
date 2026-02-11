# Harness Server — Developer Guide

## Architecture Overview

The harness is a Go server (Echo framework) that manages multiplayer creative worlds. Users authenticate via GitHub OAuth, browse/create worlds in a lobby, and play Bevy/WASM games inside an iframe with a Datastar-powered overlay for chat, build status, and checkpoint navigation.

**Stack**: Go + Echo + SQLite (sqlc) + templ (HTML) + Datastar (hypermedia/SSE) + static CSS/JS

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/server/` | HTTP handlers, routes, SSE events |
| `internal/auth/` | GitHub OAuth, session middleware, role checks |
| `internal/db/` | SQLite wrapper, migrations, sqlc queries |
| `internal/events/` | EventBus: global + per-world pub/sub channels |
| `internal/world/` | World creation, checkpoints, game server management |
| `internal/claude/` | Claude Code orchestrator (tmux sessions, build pipeline) |
| `views/` | templ templates (login, lobby, overlay, chat, etc.) |
| `static/` | CSS + JS served at `/static/` |

### Data Flow

```
Browser <--SSE--> Echo handlers <--EventBus--> Claude orchestrator
                       |                            |
                    SQLite DB                   Game servers
```

### Auth Middleware Chain

```
SessionMiddleware(db) -> sets c.Get("user") as *sqlc.User
  ApprovedMiddleware() -> rejects role="pending" users
    AdminMiddleware()  -> requires role="admin"
```

Extract user in any handler: `user, ok := c.Get("user").(*sqlc.User)`

### EventBus (`internal/events/bus.go`)

- `SubscribeGlobal() chan any` / `UnsubscribeGlobal(ch)` — all-player events (chat, build notifications)
- `Subscribe(worldID) chan any` / `Unsubscribe(worldID, ch)` — world-specific events (claude activity, build progress)
- `PublishGlobal(event any)` / `Publish(worldID, event any)` — non-blocking sends (drops if slow)
- Channel buffer: 100 events

### DB Queries Available

- `ListWorlds(ctx)`, `GetWorld(ctx, id)`, `CreateWorld(ctx, params)`
- `GetCheckpoint(ctx, id)`, `GetCheckpointTree(ctx, worldID)`, `CreateCheckpoint(ctx, params)`
- `GetCheckpointAncestry(ctx, worldID, cpID)` — root-to-current chain (custom method on DB wrapper)
- `GetRecentMessages(ctx, limit)`, `GetRecentMessagesByWorld(ctx, params)`, `CreateMessage(ctx, params)`
- `ListUsers(ctx)`, `GetUserByID(ctx, id)`, `UpdateUserRole(ctx, params)`

## Templ Patterns

templ is a Go HTML templating engine. Files use `.templ` extension, compiled to Go with `templ generate`.

### Basic Component

```go
package views

import "creative-mode/harness/internal/db/sqlc"

templ LoginPage() {
    <!DOCTYPE html>
    <html>
        <head>
            <title>Creative Mode</title>
            <link rel="stylesheet" href="/static/styles.css"/>
        </head>
        <body>
            <h1>Creative Mode</h1>
            <a href="/auth/github/login">Sign in with GitHub</a>
        </body>
    </html>
}
```

### Rendering from Echo Handler

```go
func render(c echo.Context, component templ.Component) error {
    c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
    return component.Render(c.Request().Context(), c.Response().Writer)
}

func (s *Server) handleRoot(c echo.Context) error {
    return render(c, views.LoginPage())
}
```

### Children / Composition

```go
templ Layout(title string) {
    <!DOCTYPE html>
    <html>
        <head><title>{ title }</title></head>
        <body>{ children... }</body>
    </html>
}

templ MyPage() {
    @Layout("My Page") {
        <div>Content goes here</div>
    }
}
```

### Conditional Rendering

```go
templ UserStatus(role string) {
    switch role {
        case "admin":
            <span class="badge-admin">Admin</span>
        case "pending":
            <span class="badge-pending">Pending</span>
        default:
            <span class="badge-user">User</span>
    }
}
```

### Gotchas

- Use `templ.SafeURL(href)` for dynamic href attributes
- Use `templ.JSONString(data)` to serialize Go structs into `data-signals` attributes
- All content must be inside a single root or layout wrapper
- `{ children... }` allows component composition

## Datastar — Go Server SDK (datastar-go)

**Import**: `github.com/starfederation/datastar-go/datastar`

### Creating SSE Connections

```go
func (s *Server) handleWorldSSE(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)

    // Long-lived loop
    for {
        select {
        case event := <-eventCh:
            // Send templ component as HTML patch
            sse.PatchElementTempl(views.ChatMessage(msg))
            // Or with targeting options
            sse.PatchElementTempl(views.ChatMessage(msg), datastar.WithSelectorID("chat-log"), datastar.WithModeAppend())
            // Update client-side signals
            sse.MarshalAndPatchSignals(signalStruct)
        case <-r.Context().Done():
            return nil
        }
    }
}
```

### Key SSE Methods

| Method | Purpose |
|--------|---------|
| `datastar.NewSSE(w, r)` | Create SSE connection from http.ResponseWriter + *http.Request |
| `sse.PatchElementTempl(component, opts...)` | Render templ component, send as HTML patch |
| `sse.MarshalAndPatchSignals(signals)` | Marshal struct/map to JSON, update client signals |
| `sse.PatchElements(htmlString, opts...)` | Raw HTML string patch |
| `sse.ExecuteScript(js)` | Execute JavaScript on client |
| `sse.ConsoleError(err)` | Send error to browser console |

### Patch Options

| Option | Purpose |
|--------|---------|
| `datastar.WithSelectorID("id")` | Target element by ID |
| `datastar.WithModeAppend()` | Append instead of replace |

### Reading Signals from Requests

```go
type ChatSignals struct {
    ChatText string `json:"chatText"`
}
var signals ChatSignals
if err := datastar.ReadSignals(r, &signals); err != nil {
    // handle error
}
```

### Templ Helpers for Client Attributes

```go
// In .templ files — generate data-on:click/data-init expressions
datastar.GetSSE("/path")     // GET SSE request
datastar.PostSSE("/path")    // POST SSE request
datastar.PutSSE("/path")     // PUT SSE request
datastar.DeleteSSE("/path")  // DELETE SSE request

// Usage:
<button data-on:click={ datastar.PostSSE("/api/chat") }>Send</button>
<div data-init={ datastar.GetSSE("/world/abc/events") }></div>
```

> **IMPORTANT — Datastar v1.0.0-RC.6 attribute syntax**:
> - **Event handlers use colon syntax**: `data-on:click`, `data-on:keydown`, etc. (NOT `data-on-click` with a dash — dashes break the plugin lookup via HTML dataset camelCase conversion)
> - **SSE on load uses `data-init`**: NOT `data-on-load` (which registers a DOM `load` event that only fires on resource-loading elements like img/script/iframe, not divs)

## Datastar — Client-Side Attributes

Datastar is a hypermedia framework. All reactivity is declarative via `data-*` attributes. The server drives state; the client renders it.

### Signals (Reactive State)

```html
<!-- Initialize signals with JSON -->
<div data-signals='{"count": 0, "name": "hello", "expanded": true}'>

<!-- Or from Go struct via templ -->
<div data-signals={ templ.JSONString(signals) }>

<!-- Signal access uses $ prefix -->
<span data-text="$count"></span>
<div data-show="$expanded">Visible when expanded</div>
```

### Key Attributes

| Attribute | Purpose | Example |
|-----------|---------|---------|
| `data-signals` | Initialize reactive signals | `data-signals='{"open": false}'` |
| `data-text` | Bind text content to expression | `data-text="$count"` |
| `data-show` | Conditional visibility | `data-show="$isVisible"` |
| `data-class` | Conditional CSS classes (object syntax) | `data-class="{'active': $tab === 'chat'}"` |
| `data-bind` | Two-way input binding | `data-bind-chatText` |
| `data-on:click` | Click handler | `data-on:click="$count++"` |
| `data-init` | Run when element is first processed | `data-init="@get('/events')"` |
| `data-on:keydown` | Keyboard handler | `data-on:keydown="evt.key === 'Enter' && @post('/send')"` |
| `data-attr-*` | Dynamic attribute | `data-attr-disabled="$loading"` |
| `data-indicator` | Track fetch in-progress | `data-indicator-fetching` |

### Expressions

Datastar expressions are JavaScript-like, evaluated by the framework:

```html
<!-- Signal access -->
data-text="$user.name"
data-show="$items.length > 0"

<!-- Assignment -->
data-on:click="$count++; $message = 'Updated'"

<!-- Ternary (works with data-text, data-show, data-attr-*, NOT data-class) -->
data-text="$count > 0 ? $count + ' items' : 'No items'"

<!-- Actions (@ prefix for SSE requests) -->
data-on:click="@post('/api/endpoint')"
data-on:click="$count++; @post('/api/count')"

<!-- Event context -->
data-on:keydown="evt.key === 'Enter' && @post('/search')"
data-on:input="$value = evt.target.value"

<!-- Multiple statements (semicolons) -->
data-on:click="$expanded = true; $unreadCount = 0"
```

### Forms

```html
<!-- Send form data (not signals) to backend -->
<form>
    <input name="prompt" required />
    <button data-on:click="@post('/world/abc/prompt', {contentType: 'form'})">
        Submit
    </button>
</form>
```

### Loading Indicators

```html
<button id="sendBtn"
        data-indicator-fetching
        data-on:click="@post('/api/chat')"
        data-attr-disabled="$fetching">
    Send
</button>
<div class="indicator" data-class="{'loading': $fetching}">Sending...</div>
```

## SSE Pattern: Long-Lived Connection with EventBus

This is the primary pattern for real-time updates in the harness:

```go
func (s *Server) handleWorldSSE(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)
    worldID := c.Param("worldID")

    globalCh := s.EventBus.SubscribeGlobal()
    defer s.EventBus.UnsubscribeGlobal(globalCh)
    worldCh := s.EventBus.Subscribe(worldID)
    defer s.EventBus.Unsubscribe(worldID, worldCh)

    // Send initial state
    recentMsgs, _ := s.DB.GetRecentMessages(r.Context(), 50)
    for _, msg := range recentMsgs {
        sse.PatchElementTempl(views.ChatMessage(msg), datastar.WithSelectorID("chat-log"), datastar.WithModeAppend())
    }

    for {
        select {
        case event := <-globalCh:
            // Handle chat messages, system notifications
        case event := <-worldCh:
            // Handle build progress, claude activity
        case <-r.Context().Done():
            return nil
        }
    }
}
```

## Build & Development

```bash
cd harness
just generate    # sqlc generate + templ generate
just build       # go build -o harness .
just dev         # go run .
just lint        # golangci-lint run ./...
just fmt         # golangci-lint fmt ./...
```

## Reference Examples

For in-depth working examples of these patterns, see the `context/` directory:

### `context/northstar/` — SSE + Datastar Go Server Patterns
- **`features/counter/`** — Signal structs, PatchElementTempl, MarshalAndPatchSignals, PostSSE/GetSSE
- **`features/index/`** — Long-lived SSE with watcher loop, ReadSignals from requests, TodoMVC
- **`features/monitor/`** — Continuous monitoring with tickers, partial signal updates (omitempty)
- **`features/common/layouts/base.templ`** — Base HTML layout with Datastar script loading, hot reload
- **`router/router.go`** — SSE hot reload pattern, ExecuteScript

### `context/datastarui/` — Datastar Client-Side Signal/Expression Patterns
- **`.cursor/rules/datastar.mdc`** — Comprehensive Datastar attribute reference, expression syntax, form handling
- **`components/`** — Reusable UI components (button, input, dialog, checkbox, tabs, etc.)
- **Signal namespacing** — Using `props.ID` to namespace signals: `$myComponent.open`, `$myComponent.selected`
- **Event handling** — `data-on:click`, `data-on:keydown`, `data-on:click__outside`
- **Conditional classes** — `data-class="{'active': $tab === 'chat'}"` (object syntax required)
- **Fetch indicators** — `data-indicator-fetching`, `data-attr-disabled="$fetching"`
