# Harness Server — Developer Guide

## Architecture Overview

The harness is a Go server (Echo framework) that manages multiplayer creative worlds. Users authenticate via Discord OAuth, browse/create worlds in a lobby, and play Bevy/WASM games inside an iframe with a Datastar-powered overlay for chat, build status, and checkpoint navigation.

**Stack**: Go + Echo + SQLite (sqlc) + templ (HTML) + Datastar (hypermedia/SSE) + static CSS/JS

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/server/` | HTTP handlers, routes, SSE events |
| `internal/auth/` | Discord OAuth, session middleware, role checks |
| `internal/db/` | SQLite wrapper, migrations, sqlc queries |
| `internal/events/` | EventBus: global + per-world pub/sub channels |
| `internal/world/` | World creation, checkpoints, game server management |
| `internal/claude/` | Claude Code orchestrator: prompt-to-build pipeline (fork, tmux, build, events) |
| `internal/builder/` | Build pipeline: fork checkpoint → Claude Code → compile → deploy (renamed from `internal/build/`) |
| `internal/tmux/` | Tmux session management for Claude Code and game servers |
| `internal/logging/` | Structured JSON logger |
| `internal/gemini/` | Gemini image generation integration |
| `internal/mayor/` | Mayor agent lifecycle: OpenClaw provisioning, workspace files, Discord posting |
| `internal/president/` | President agent: provisioning, repo-level operations, deploy |
| `internal/discord/` | Discord Gateway listener: mirrors messages to DB + EventBus |
| `views/` | templ templates (login, lobby, overlay, chat, etc.) |
| `views/mayor/` | Mayor Dashboard templ templates |
| `static/` | CSS + JS served at `/static/` |

### Data Flow

```
Browser <--SSE--> Echo handlers <--EventBus--> Claude orchestrator
                       |              |              |
                    SQLite DB         |          Game servers
                       ^              |
                       |         EventMayorMessage
                       |              |
Discord <--Gateway--> Listener -------+
   ^
   |    OpenClaw Mayor/President agents
   +--- (Discord adapter for chat, skills call harness API)
```

### Auth Middleware Chain

```
SessionMiddleware(db) -> sets c.Get("user") as *sqlc.User
  ApprovedMiddleware() -> rejects role="pending" users
    AdminMiddleware()  -> requires role="admin"
```

Extract user in any handler: `user, ok := c.Get("user").(*sqlc.User)`

### Mayor API (`internal/server/mayor_api.go`)

**Auth**: `mayorAuthMiddleware` validates `X-Mayor-Secret` header against per-world secrets in DB, sets `c.Get("mayor_world")` as `*sqlc.World`.

| Route | Method | Purpose |
|-------|--------|---------|
| `/api/world-hatched` | POST | Webhook from site — creates world + provisions OpenClaw agent (auth: `hookSecretMiddleware`) |
| `/api/mayor/build` | POST | Trigger build pipeline for mayor's world |
| `/api/mayor/status` | GET | World state: checkpoints, game server, latest status |
| `/api/mayor/contribute-learning` | POST | Queue a knowledge contribution (PR or file update) |

### President API (`internal/server/president_api.go`)

**Auth**: `presidentAuthMiddleware` validates `X-President-Secret` header against `PRESIDENT_SECRET` env var.

| Route | Method | Purpose |
|-------|--------|---------|
| `/api/president/mayor-status` | GET | Status of all worlds with mayors |
| `/api/president/repo-build` | POST | Run `just check` in a tmux session |
| `/api/president/template-update` | POST | Spawn Claude Code session at repo root |
| `/api/president/deploy` | POST | Run `just vps-deploy` in a tmux session |

**Notes**: Auto-provisions on startup when `PRESIDENT_SECRET` and `DISCORD_PRESIDENT_CHANNEL_ID` env vars are set (currently disabled in production — env vars not configured). Spawned tmux sessions are fire-and-forget (no status polling endpoint).

### Mayor Dashboard (`internal/server/mayor_dashboard.go`)

Approved-user routes for world observability:

| Route | Method | Purpose |
|-------|--------|---------|
| `/mayor/:worldID` | GET | Dashboard page: builds, activity, messages, sessions |
| `/mayor/:worldID/events` | GET | SSE stream for live dashboard updates |
| `/mayor/:worldID/file` | GET | Read workspace file (allowlist: SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md) |
| `/mayor/:worldID/file` | PUT | Edit workspace file (allowlist: SOUL.md, MEMORY.md, AGENTS.md) |

### Discord Listener (`internal/discord/listener.go`)

A separate discordgo Gateway session (distinct from the REST-only `worldchannel` client) that mirrors Discord messages to the DB and EventBus.

- **Channel map**: Loaded from DB on startup via `GetWorldsWithDiscordChannels()`, updated dynamically via `RegisterChannel(channelID, worldID)`
- **Message classification**: `author_type` is `user`, `mayor`, or `system` — based on bot flag and message content prefix: non-bot messages → `user`, bot messages with `[BUILD`/`[SYSTEM` prefix → `system`, other bot messages → `mayor`
- **Channel registration**: Channel map is loaded from DB on startup; `RegisterChannel()` exists but new worlds provisioned at runtime won't have Discord mirroring until harness restart
- **Flow**: Discord message → `MessageCreate` handler → lookup world by channel → `CreateMayorMessage` in DB → `Publish(worldID, EventMayorMessage)` to EventBus → SSE → browser

### OpenClaw Integration (`internal/mayor/manager.go`)

Mayors and the president are OpenClaw agents managed via CLI (`exec.CommandContext`).

**Workspace structure** (`{OPENCLAW_HOME}/workspaces/`):
- `world-{worldID}/` — mayor workspace: `SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `USER.md`, `MEMORY.md`, `skills/`
- `president/` — president workspace: `SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `USER.md`, `MEMORY.md`, `HEARTBEAT.md`, `skills/` (4 skills: `mayor-status`, `repo-build`, `template-update`, `deploy`)

**Key operations**:
- `ProvisionFromWebhook()` — creates world record, generates agent workspace files from onboarding data, registers Discord channel
- `PostToDiscord()` — send messages to a world's Discord channel (build notifications)
- `ContributeLearning()` — queue knowledge contributions from mayors
- `BindAgentToDiscord()` — exists but is not called during mayor provisioning (only president uses channel binding)

**OpenClaw gateway**: Health checked at `localhost:18789`.

### Build Notifications

The `OnBuildComplete` callback is wired in `main.go` to post build results to Discord:
- On success: `[BUILD COMPLETE] Checkpoint {cpID} — {summary}`
- On failure: `[BUILD FAILED] Checkpoint {cpID} — {summary}`
- Sent via `mayorManager.PostToDiscord()` to the world's Discord channel
- The Discord listener then mirrors these back as `system`-type messages (matched by `[BUILD` prefix)

### EventBus (`internal/events/bus.go`)

- `SubscribeGlobal() chan any` / `UnsubscribeGlobal(ch)` — all-player events (chat, build notifications)
- `Subscribe(worldID) chan any` / `Unsubscribe(worldID, ch)` — world-specific events (claude activity, build progress, mayor messages)
- `PublishGlobal(event any)` / `Publish(worldID, event any)` — non-blocking sends (drops if slow)
- Channel buffer: 100 events
- Event types include `EventMayorMessage = "mayor.message"` (published by Discord listener)

### DB Queries Available

- `ListWorlds(ctx)`, `GetWorld(ctx, id)`, `CreateWorld(ctx, params)`
- `GetCheckpoint(ctx, id)`, `GetCheckpointTree(ctx, worldID)`, `CreateCheckpoint(ctx, params)`
- `GetCheckpointAncestry(ctx, worldID, cpID)` — root-to-current chain (custom method on DB wrapper)
- `GetRecentMessagesWithUser(ctx, limit)`, `GetRecentMessagesByWorld(ctx, params)`, `CreateMessage(ctx, params)`
- `ListUsers(ctx)`, `GetUserByID(ctx, id)`, `UpdateUserRole(ctx, params)`
- **Mayor/world queries**: `GetWorldByMayorSecret(ctx, secret)`, `GetWorldByDiscordChannel(ctx, channelID)`, `UpdateWorldMayor(ctx, params)`, `GetWorldsWithDiscordChannels(ctx)`
- **Mayor messages**: `CreateMayorMessage(ctx, params)`, `GetMayorMessages(ctx, worldID)`, `GetRecentMayorMessages(ctx, params)`, `GetMayorMessageByDiscordID(ctx, discordMsgID)`
- **Mayor instrumentation**: `CreateMayorActivity(ctx, params)`, `GetMayorActivity(ctx, params)`, `GetMayorBuilds(ctx, params)`, `GetMayorSessions(ctx, params)`

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
            <a href="/auth/discord/login">Sign in with Discord</a>
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
> - **All plugin suffixes use colon syntax**: `data-on:click`, `data-bind:chat_text`, etc. (NOT `data-on-click` or `data-bind-chat_text` with dashes — dashes break the plugin lookup because HTML's dataset API converts `data-bind-foo` → `bindFoo` via camelCase, mangling the plugin name)
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
| `data-bind` | Two-way input binding | `data-bind:chatText` |
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

## Iframe + Datastar: Keyboard Events and Focus

The game runs inside a cross-origin iframe (Trunk on a different port in dev mode). This has important implications for keyboard event handling.

### Iframe steals focus on load

When the Bevy WASM iframe loads, it captures focus from the parent window. This means:
- `data-on:keydown__window` on parent elements **will not fire** — keyboard events go to the iframe's `window`, not the parent's
- `window.addEventListener('keydown', ...)` on the parent also won't fire
- Clicking any element in the parent document (e.g., the CM button) returns focus to the parent, after which `data-on:keydown__window` works normally

### postMessage bridge pattern

To handle keyboard events that originate inside the iframe, each template's `index.html` forwards specific keys via `postMessage`:

```
iframe keydown (e.g., backtick)
  → window.parent.postMessage({ type: 'toggle-overlay' }, '*')
  → game-loader.js message listener
  → document.getElementById('game-overlay-toggle-trigger').click()
  → Datastar data-on:click handler toggles $overlay_expanded
```

This pattern bypasses the focus boundary. The hidden trigger buttons in `world.templ` bridge between plain JS `postMessage` and Datastar's signal system.

### Datastar initialization is NOT the issue

`data-init` (SSE connection), `data-signals`, and `data-on:*` handlers on the same element are all processed synchronously during `apply()`. The `@get()` action in `data-init` is async and returns immediately. Signals are reactive as soon as they're created. There is no lazy initialization or activation step — if `data-on:keydown__window` doesn't fire, it's a focus issue, not a Datastar timing issue.

### Adding new keyboard shortcuts

If you need a keyboard shortcut that works regardless of iframe focus:
1. Add a `document.addEventListener('keydown', ...)` in the template's `index.html` (inside the iframe)
2. Forward the key via `postMessage` to the parent
3. Handle it in `game-loader.js` by clicking a hidden trigger button
4. The trigger button's `data-on:click` bridges into Datastar's signal system

Do NOT rely solely on `data-on:keydown__window` — it only works when the parent window has focus.

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

## Game Servers (tmux-based)

Game servers run in dedicated tmux sessions named `cm-server-{worldID}-{cpID}`. This makes them survive harness restarts — on startup, `GameServerManager.Recover()` scans `tmux list-sessions` and re-registers any surviving servers.

### Two Modes

| Mode | Command | When |
|------|---------|------|
| `prod` | `./target/release/server` | After build completes (`BuildCheckpoint`) |
| `dev` | `cargo watch -w shared -w server -x 'run -p server'` | During Claude editing session (`HandlePrompt`) |

### Session Naming

- Game server: `cm-server-{worldID}-{cpID}` (parseable — both IDs are 8-char hex, no hyphens)
- Trunk serve: `cm-trunk-{worldID}-{cpID}` (WASM dev server)
- Claude Code: `cm-{worldID}-{cpID}` (managed by `internal/tmux/`)

### Environment Variables

Each tmux session gets `GAME_PORT`, `BRP_PORT` (GAME_PORT+1000), and `CM_SERVER_MODE` (prod/dev). Recovery reads these via `tmux show-environment`.

### Lifecycle

1. **Prompt submitted** → `HandlePrompt` forks checkpoint, starts dev server (`ConnectDev`), creates Claude tmux session with `CM_GAME_PORT`/`CM_BRP_PORT` env vars
2. **Claude editing** → dev server auto-rebuilds on file changes, inner Claude can query BRP
3. **Claude stops** → `BuildCheckpoint` kills dev server + Claude session, runs release build, starts prod server (`Connect`)
4. **Harness restart** → `Recover()` finds surviving tmux sessions, syncs ports back to SQLite

### Key Methods (`GameServerManager`)

| Method | Purpose |
|--------|---------|
| `Connect(worldID, cpID, dir)` | Start/reuse prod server |
| `ConnectDev(worldID, cpID, dir)` | Start/reuse dev server (cargo watch) |
| `GetServer(worldID, cpID)` | Lookup with liveness check (cleans stale entries) |
| `StopByWorldExcept(worldID, keepCPID)` | Kill all servers for a world except one |
| `Recover()` | Scan tmux sessions on startup, re-register servers |
| `RecoveredServers()` | Snapshot for DB port sync |
| `Shutdown()` | Kill all (graceful exit only — crash leaves sessions alive) |

### Logging

Game server output is captured via `tmux pipe-pane` to `{logsDir}/worlds/{worldID}/{cpID}/game-server.log` (raw text, not JSONL). Build logs remain JSONL.

### Status Endpoint

`GET /world/:worldID/status` returns JSON with the user's current checkpoint and game server state:
```json
{
  "world_id": "abc12345",
  "checkpoint_id": "def67890",
  "build_status": "ready",
  "game_server": { "running": true, "port": 9001, "brp_port": 10001, "mode": "prod" }
}
```

## Build & Development

```bash
cd harness
just generate    # sqlc generate + templ generate + tailwind build
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

## Datastar Best Practices (The Tao of Datastar)

Reference: https://data-star.dev/guide/the_tao_of_datastar/

### 1. Backend is the Source of Truth

Most state should live on the server. The frontend is exposed to the user and should not be trusted as authoritative. Drive UI updates from the backend via HTML fragment patching, not by managing complex state as client-side signals.

**Prefer `PatchElementTempl` over `MarshalAndPatchSignals`** for anything beyond simple input clearing:

```go
// GOOD — Server renders the UI state and patches the DOM
sse.PatchElementTempl(views.ImagePreview(previewURL, status),
    datastar.WithSelectorID("image-preview"))

// AVOID — Managing complex server state as client signals
sse.MarshalAndPatchSignals(map[string]any{
    "image_gen_status": "complete",
    "image_preview_url": "/api/images/preview/abc123",
    "image_saved_path":  "/worlds/xyz/assets/sprite.png",
    "image_error_msg":   "",
})
```

`MarshalAndPatchSignals` is appropriate for clearing form inputs after submission (e.g., resetting `chat_text` to `""`), but should not be used to shuttle complex server state to the client.

### 2. Use Signals Sparingly

Signals should only be used for two things:

1. **User input binding** — `data-bind:chat_text` on an input field so the value is sent with `@post`
2. **Simple UI toggles** — `data-show="$expanded"`, tab selection, visibility flags

If you find yourself defining signals for status strings, URLs, file paths, or other server-derived data, that state belongs in a server-rendered HTML fragment instead.

### 3. How Signals Are Sent with Actions

`@post`, `@get`, etc. **automatically include all signals** in the request (except those prefixed with `_`). No manual wiring is needed:

- **GET requests**: signals sent as query parameters
- **POST/PUT/PATCH/DELETE**: signals sent as JSON body under `{datastar: {…}}`

To limit which signals are sent, use the `filterSignals` option:

```html
<!-- Only send signals matching the regex -->
<button data-on:click="@post('/api/images/generate', {filterSignals: {include: /^image_/}})">
    Generate
</button>
```

On the server, read signals with:

```go
type ImageSignals struct {
    ImagePrompt string `json:"image_prompt"`
}
var signals ImageSignals
if err := datastar.ReadSignals(r, &signals); err != nil { ... }
```

### 4. Avoid Optimistic UI

Do not show success before the server confirms it. Use `data-indicator` to show loading state, and let the backend's SSE response update the DOM with the real result:

```html
<button data-indicator-generating
        data-on:click="@post('/api/images/generate')"
        data-attr-disabled="$generating">
    Generate
</button>
<div data-show="$generating">Generating image...</div>
```

The backend SSE response then patches in the actual result via `PatchElementTempl`, which naturally hides the indicator.

### 5. CQRS: Separate Reads from Writes

- **Reads** (`@get`, `data-init`): Long-lived SSE connections that stream updates. One connection per page/component.
- **Writes** (`@post`, `@put`, `@delete`): Short-lived requests that send user input and receive a confirmation or DOM patch.

```html
<!-- READ: Long-lived SSE for real-time updates -->
<div data-init="@get('/world/abc/events')"></div>

<!-- WRITE: Short-lived POST on user action -->
<button data-on:click="@post('/api/chat')">Send</button>
```

### 6. Let the Browser Handle Navigation

Use standard `<a>` tags for navigation. Use backend redirects when actions need to change pages. Don't build custom SPA-style routing — the browser handles history automatically.

### 7. Use Morphing for Efficient Updates

Datastar's morphing only updates changed DOM nodes, preserving input focus and scroll position. You can safely send large HTML fragments ("fat morph") — the framework handles diffing. Use `data-ignore-morph` on elements that should not be touched (e.g., video players, iframes).

### 8. Working Pattern Reference

The chat system demonstrates correct Datastar patterns:

- **Input binding**: `data-bind:chat_text` on the text input
- **Action**: `@post('/api/chat')` sends all signals (including `chat_text`) automatically
- **Server read**: `datastar.ReadSignals(r, &signals)` extracts `chat_text`
- **Server response**: `sse.PatchElementTempl(views.ChatMessage(msg))` renders the new message as HTML
- **Signal reset**: `sse.MarshalAndPatchSignals(...)` only to clear `chat_text` back to `""`

Follow this pattern: bind inputs → post action → read signals on server → patch HTML back → optionally clear input signal.
