# Component 6: UI Overlay + Chat/Notification System

## Overview

Build the DatastarUI overlay that sits on top of the game canvas, the tabbed chat/notification panel (Global/World/Lineage), the SSE event handlers that power real-time updates, and all templ views (login, pending, admin, lobby, overlay). The overlay has two states: expanded (full chrome) and minimized (floating button with unread badge).

**Dependencies**: Component 2 (auth middleware + user context), Component 3 (world management, game servers, WASM artifacts), Component 5 (EventBus, claude orchestrator, chat endpoint)
**Depends on this**: Component 7

## Directory Layout

```
harness/
├── views/
│   ├── layout.templ              # Base HTML layout (game iframe + overlay container)
│   ├── login.templ               # Login page ("Sign in with GitHub")
│   ├── pending.templ             # "Waiting for approval" page
│   ├── admin_users.templ         # Admin user management page
│   ├── lobby.templ               # World browser / lobby (full-screen, no game)
│   ├── overlay.templ             # In-game overlay (expanded + minimized states)
│   ├── chat_panel.templ          # Chat message + notification + lineage templates
│   ├── checkpoint_tree.templ     # Checkpoint tree side panel
│   └── build_log.templ           # Build log viewer
├── static/
│   ├── styles.css                # All CSS (overlay, chat, lineage, badges, etc.)
│   └── game-loader.js            # loadCheckpoint(), switchWorld(), loadLineage()
└── internal/server/
    └── events.go                 # SSE handlers (world events + global events)
```

## Implementation Details

### Datastar Signals

The overlay state is managed via Datastar signals, initialized server-side and updated in real-time via SSE:

```go
type OverlaySignals struct {
    CurrentWorldID      string `json:"currentWorldId"`
    CurrentCheckpointID string `json:"currentCheckpointId"`
    BuildStatus         string `json:"buildStatus"`      // idle, editing, compiling, ready, failed, rate_limited
    PromptText          string `json:"promptText"`
    ChatText            string `json:"chatText"`
    OverlayExpanded     bool   `json:"overlayExpanded"`   // true = full overlay, false = minimized
    ActiveTab           string `json:"activeTab"`         // "global", "world", "lineage"
    ShowCheckpointTree  bool   `json:"showCheckpointTree"`
    ShowBuildLog        bool   `json:"showBuildLog"`
    UnreadCount         int    `json:"unreadCount"`       // badge count when minimized
    RateLimitRetryAt    int64  `json:"rateLimitRetryAt"`  // Unix timestamp, 0 = not limited
}
```

### Views

#### Login Page (`harness/views/login.templ`)

Shown when user is not authenticated:
- "Sign in with GitHub" button -> links to `/auth/github/login`
- Clean, minimal design with Creative Mode branding

#### Pending Page (`harness/views/pending.templ`)

Shown when user is authenticated but `role = 'pending'`:
- User's avatar + username
- "Your request to join has been submitted. An admin will approve your access."
- Can poll `/auth/pending/status` via Datastar SSE to auto-redirect when approved

#### Admin Users Page (`harness/views/admin_users.templ`)

Admin-only page at `/admin/users`:
- Lists all users with role, avatar, username, joined date
- Pending users highlighted with "Approve" / "Reject" buttons
- Live-updates via Datastar SSE when new users request access

#### Lobby (`harness/views/lobby.templ`)

Full-screen view (no game canvas):
- User avatar + username in top-right corner
- Logout button (`POST /auth/logout`)
- List of existing worlds with name, description, created by (username + avatar), # checkpoints, last modified
- "Create New World" button with name/description input
- Chat/notification panel (optional, or just inline notifications)
- SSE connection via `data-on:load="@get('/events')"` for global notifications

#### Layout (`harness/views/layout.templ`)

The page structure for in-game view:
```html
<body data-world-id="{worldID}" data-checkpoint-id="{cpID}">
  <!-- Game iframe - full screen, behind overlay -->
  <iframe id="game-frame"
          src="/wasm/{worldID}/{cpID}/index.html?server_port={port}"
          style="position:fixed; inset:0; z-index:0; width:100%; height:100%; border:none;">
  </iframe>

  <!-- Harness overlay sits on top -->
  <div id="harness-overlay"
       data-signals='{json overlay signals}'
       data-on:load="@get('/world/{worldID}/events')">
    <!-- Expanded state -->
    <!-- Minimized state -->
  </div>

  <script src="/static/game-loader.js"></script>
</body>
```

#### Overlay (`harness/views/overlay.templ`)

Two-column layout when expanded:

```
+-------------------------------------------+----------------------+
| Creative Mode | World: My RPG v | [-]      | [Global][World][Lin.]|
| CP: castle v | [Save] [Tree] [<- Lobby]    |----------------------|
+-------------------------------------------+                      |
|                                           | Global tab:           |
|         (transparent - game visible)      | [sys] Build ready:    |
|                                           |   "add river" [>Play] |
|                                           | [alice] nice castle!  |
|                                           | [sys] Bob joined      |
|                                           |                       |
+-------------------------------------------+----------------------+
| > add a river...                  [Build] | > type a message...   |
| Building...  Players: 2           60fps   | [Send]                |
+-------------------------------------------+----------------------+
```

Minimized state: floating button bottom-right with unread badge.

**Key Datastar attributes:**
- `data-show="$overlayExpanded"` — toggle expanded/minimized
- `data-show="!$overlayExpanded"` — minimized button
- `data-on:click="$overlayExpanded = true; $unreadCount = 0"` — expand + reset badge
- `data-on:click="$overlayExpanded = false"` — minimize
- `data-on:click="$activeTab = 'global'"` — tab switching
- `data-class="{'tab-active': $activeTab === 'global'}"` — active tab styling
- `data-bind="chatText"` — chat input binding
- `data-bind="promptText"` — prompt input binding
- `data-show="$activeTab !== 'lineage'"` — hide chat input on lineage tab

#### Chat Panel Components (`harness/views/chat_panel.templ`)

**Chat message (Global/World tabs):**
```go
templ ChatMessage(username, avatarURL, content, timestamp, worldID string) {
    <div class="message chat-message" data-world-id={ worldID }>
        <img src={ avatarURL } class="avatar-sm" />
        <span class="username">{ username }</span>
        <span class="content">{ content }</span>
        <time class="ts">{ timestamp }</time>
    </div>
}
```

**System notification:**
```go
templ SystemNotification(eventType, worldID, worldName, cpID, content string) {
    <div class="message system-notification" data-world-id={ worldID }>
        <span class="sys-badge">[sys]</span>
        <span class="content">{ content }</span>
        if eventType == "build.completed" {
            <button class="play-btn"
                    data-on:click={ fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cpID) }>
                Play
            </button>
        }
    </div>
}
```

**Lineage view:**
```go
templ LineageView(checkpoints []db.Checkpoint) {
    for _, cp := range checkpoints {
        <div class="lineage-entry">
            <div class="lineage-header">
                <span class="cp-id">[{ cp.ID[:8] }]</span>
                if cp.Prompt != "" {
                    <span class="prompt">"{ truncate(cp.Prompt, 60) }"</span>
                } else {
                    <span class="prompt">Starter template</span>
                }
                if cp.CreatedBy != "" {
                    <span class="author">-- { cp.CreatedByUsername }</span>
                }
                <time class="ts">{ timeAgo(cp.CreatedAt) }</time>
            </div>
            if cp.WorkSummary != "" {
                <div class="lineage-summary">
                    <span class="claude-label">Claude:</span>
                    { cp.WorkSummary }
                    if cp.FilesChanged != "" {
                        <div class="files-changed">Files: { cp.FilesChanged }</div>
                    }
                    <div class="build-result">
                        Build: { cp.StatusIcon() } { cp.Status }
                        if cp.BuildDurationMs > 0 {
                            ({ formatDuration(cp.BuildDurationMs) })
                        }
                    </div>
                </div>
            }
        </div>
    }
    <div class="lineage-cursor">you are here</div>
}
```

#### Checkpoint Tree (`harness/views/checkpoint_tree.templ`)

Side panel (slides in from left via `data-show="$showCheckpointTree"`):
- Tree structure with indentation following parent_checkpoint_id relationships
- Each node: name/prompt snippet, status icon, click to switch
- Current checkpoint highlighted
- "Fork from here" button on ready checkpoints

### SSE Event Handlers (`harness/internal/server/events.go`)

#### World SSE (`GET /world/:worldID/events`)

Subscribes to **both** the global event bus and world-specific event bus:

```go
func (s *Server) handleSSEEvents(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)

    worldID := c.Param("worldID")
    user := c.Get("user").(*db.User)

    // Subscribe to both buses
    globalCh := s.eventBus.SubscribeGlobal()
    defer s.eventBus.UnsubscribeGlobal(globalCh)
    worldCh := s.eventBus.Subscribe(worldID)
    defer s.eventBus.Unsubscribe(worldID, worldCh)

    // Send recent message history (last 50) on connect
    recentMessages, _ := s.db.GetRecentMessages(50)
    sse.PatchElements(renderMessageLog(recentMessages))

    // Record player joined
    s.eventBus.PublishGlobal(map[string]any{
        "event": "player.joined", "username": user.GitHubUsername,
        "worldID": worldID, "avatarURL": user.AvatarURL,
    })

    for {
        select {
        case event := <-globalCh:
            // Chat messages, build notifications (all worlds)
            e, _ := event.(map[string]any)
            eventType, _ := e["event"].(string)

            switch eventType {
            case "chat.message":
                sse.PatchElements(renderChatMessage(e))
                sse.MarshalAndPatchSignals(map[string]any{
                    "unreadCount": "$unreadCount + 1",
                })
            case "build.completed", "build.started", "build.failed",
                 "player.joined", "player.left":
                sse.PatchElements(renderNotification(e))
            }

        case event := <-worldCh:
            // Build progress, claude activity (current world only)
            e, _ := event.(map[string]any)
            eventType, _ := e["event"].(string)

            switch {
            case eventType == "claude.tool_use.pre":
                tool, _ := e["tool"].(string)
                file, _ := e["file"].(string)
                sse.PatchElements(renderClaudeActivity(tool, file))
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "editing",
                })
            case eventType == "claude.tool_use.post":
                sse.PatchElements(renderClaudeActivityDone(e))
            case eventType == "claude.session_stopped":
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "compiling",
                })
            case eventType == "build.output":
                line, _ := e["line"].(string)
                sse.PatchElements(renderBuildLogLine(line))
            case eventType == "build.completed":
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "ready",
                })
                sse.PatchElements(renderCheckpointTree(worldID, s.db))
            case eventType == "build.failed":
                errMsg, _ := e["error"].(string)
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "failed",
                })
                sse.PatchElements(renderBuildStatus("failed", errMsg))
            case eventType == "claude.rate_limited":
                retryAfter, _ := e["retryAfterSec"].(float64)
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus":      "rate_limited",
                    "rateLimitRetryAt": time.Now().Add(time.Duration(retryAfter) * time.Second).Unix(),
                })
                sse.PatchElements(renderRateLimitBanner(int(retryAfter)))
            }

        case <-r.Context().Done():
            s.eventBus.PublishGlobal(map[string]any{
                "event": "player.left", "username": user.GitHubUsername,
                "worldID": worldID,
            })
            return nil
        }
    }
}
```

#### Global SSE (`GET /events`)

For the lobby page — subscribes only to the global bus:

```go
func (s *Server) handleGlobalSSE(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)

    globalCh := s.eventBus.SubscribeGlobal()
    defer s.eventBus.UnsubscribeGlobal(globalCh)

    // Send recent messages
    recentMessages, _ := s.db.GetRecentMessages(50)
    sse.PatchElements(renderMessageLog(recentMessages))

    for {
        select {
        case event := <-globalCh:
            e, _ := event.(map[string]any)
            eventType, _ := e["event"].(string)
            switch eventType {
            case "chat.message":
                sse.PatchElements(renderChatMessage(e))
            case "build.completed", "build.started", "build.failed",
                 "player.joined", "player.left":
                sse.PatchElements(renderNotification(e))
            }
        case <-r.Context().Done():
            return nil
        }
    }
}
```

#### Lineage Endpoint (`GET /world/:worldID/lineage/:cpID`)

Not SSE — a one-shot HTML response:

```go
func (s *Server) handleLineage(c echo.Context) error {
    worldID := c.Param("worldID")
    cpID := c.Param("cpID")
    ancestry, _ := s.db.GetCheckpointAncestry(worldID, cpID)
    return render(c, LineageView(ancestry))
}
```

### JavaScript (`harness/static/game-loader.js`)

```javascript
// Load a checkpoint (same-world = iframe swap, cross-world = full page nav)
window.loadCheckpoint = function(worldID, checkpointID, serverPort) {
    const currentWorldID = document.body.dataset.worldId;

    if (worldID !== currentWorldID) {
        // Cross-world: full page load (new SSE connection)
        window.location.href = `/world/${worldID}?checkpoint=${checkpointID}`;
        return;
    }

    // Same-world: swap iframe src (instant)
    const iframe = document.getElementById('game-frame');
    iframe.src = `/wasm/${worldID}/${checkpointID}/index.html?server_port=${serverPort}`;

    // Update server-side user position
    fetch(`/world/${worldID}/checkpoint/${checkpointID}/select`, { method: 'POST' });

    // Update body data attributes for lineage
    document.body.dataset.checkpointId = checkpointID;
};

// Switch world via top bar dropdown
window.switchWorld = function(worldID) {
    window.location.href = `/world/${worldID}`;
};

// Fetch lineage when tab is selected or checkpoint changes
window.loadLineage = function() {
    const worldID = document.body.dataset.worldId;
    const cpID = document.body.dataset.checkpointId;
    fetch(`/world/${worldID}/lineage/${cpID}`)
        .then(r => r.text())
        .then(html => {
            document.getElementById('lineage-view').innerHTML = html;
        });
};
```

### CSS (`harness/static/styles.css`)

Full CSS for the overlay system:

```css
/* Overlay container - covers viewport, pointer-events pass through */
#harness-overlay {
    position: fixed;
    inset: 0;
    z-index: 10;
    pointer-events: none;
}

/* Expanded: two-column grid (game area left, chat right) */
.overlay-expanded {
    display: grid;
    grid-template-columns: 1fr 320px;
    grid-template-rows: auto 1fr auto;
    height: 100vh;
    pointer-events: none;
}
.overlay-expanded > * {
    pointer-events: auto;
}

/* Top/bottom bars */
.overlay-bar {
    background: rgba(15, 15, 15, 0.85);
    backdrop-filter: blur(10px);
    color: white;
    padding: 8px 16px;
}

/* Chat panel (right column, full height) */
.chat-panel {
    grid-column: 2;
    grid-row: 1 / -1;
    background: rgba(15, 15, 15, 0.90);
    backdrop-filter: blur(10px);
    color: white;
    display: flex;
    flex-direction: column;
    pointer-events: auto;
}

/* Tab bar */
.tab-bar {
    display: flex;
    border-bottom: 1px solid rgba(255,255,255,0.1);
    padding: 0 4px;
}
.tab-bar button {
    flex: 1;
    padding: 8px 4px;
    background: none;
    border: none;
    color: #888;
    cursor: pointer;
    font-size: 12px;
    border-bottom: 2px solid transparent;
}
.tab-bar .tab-active {
    color: white;
    border-bottom-color: #2563eb;
}

/* Message log */
.message-log {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
}
.message { padding: 4px 8px; font-size: 13px; }
.system-notification { color: #888; }
.chat-message .username { font-weight: 600; margin-right: 4px; }
.avatar-sm { width: 16px; height: 16px; border-radius: 50%; vertical-align: middle; margin-right: 4px; }

/* Chat input */
.chat-input-bar {
    display: flex;
    padding: 8px;
    border-top: 1px solid rgba(255,255,255,0.1);
}
.chat-input-bar input {
    flex: 1;
    background: rgba(255,255,255,0.1);
    border: none;
    color: white;
    padding: 6px 8px;
    border-radius: 4px;
}
.chat-input-bar button {
    margin-left: 4px;
    background: #2563eb;
    color: white;
    border: none;
    padding: 6px 12px;
    border-radius: 4px;
    cursor: pointer;
}

/* Lineage view */
.lineage-entry {
    padding: 8px;
    border-left: 2px solid rgba(255,255,255,0.1);
    margin-left: 8px;
    margin-bottom: 4px;
}
.lineage-header { font-size: 13px; }
.lineage-header .cp-id { color: #666; font-family: monospace; font-size: 11px; }
.lineage-header .prompt { color: #e0e0e0; }
.lineage-header .author { color: #888; font-size: 12px; }
.lineage-summary {
    margin-top: 4px;
    padding-left: 8px;
    font-size: 12px;
    color: #aaa;
}
.lineage-summary .claude-label { color: #2563eb; font-weight: 600; }
.lineage-summary .files-changed { color: #666; font-size: 11px; margin-top: 2px; }
.lineage-summary .build-result { color: #666; font-size: 11px; }
.lineage-cursor { color: #2563eb; font-style: italic; padding: 8px; font-size: 13px; }

/* Play button in notifications */
.play-btn {
    background: #2563eb;
    color: white;
    border: none;
    border-radius: 4px;
    padding: 2px 8px;
    cursor: pointer;
    font-size: 12px;
    margin-left: 4px;
}

/* Game area (left column) - transparent, clicks pass through to iframe */
.game-area {
    grid-column: 1;
    pointer-events: none;
}

/* Minimized overlay: floating button */
.overlay-minimized {
    position: fixed;
    bottom: 24px;
    right: 24px;
    pointer-events: auto;
}
.overlay-minimized button {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    background: rgba(15, 15, 15, 0.85);
    color: white;
    border: 1px solid rgba(255,255,255,0.2);
    cursor: pointer;
    position: relative;
}

/* Unread badge */
.badge {
    position: absolute;
    top: -4px;
    right: -4px;
    background: #ef4444;
    color: white;
    border-radius: 50%;
    width: 20px;
    height: 20px;
    font-size: 11px;
    display: flex;
    align-items: center;
    justify-content: center;
}
```

### Route Registration

```go
// Approved users
approved.GET("/", s.handleLobby)
approved.GET("/events", s.handleGlobalSSE)

w := approved.Group("/world")
w.GET("/:worldID", s.handleWorldView)
w.GET("/:worldID/checkpoint/:cpID", s.handleCheckpointView)
w.GET("/:worldID/events", s.handleSSEEvents)
w.GET("/:worldID/lineage/:cpID", s.handleLineage)
```

## Interface Contract

This component provides:

1. **All templ views** — the visual layer of the application
2. **SSE handlers** — real-time UI updates via Datastar
3. **Lineage endpoint** — checkpoint ancestry for the Lineage tab
4. **Game-loader.js** — iframe switching, world navigation, lineage loading
5. **CSS** — all overlay styling

This component consumes:

1. **Component 2** — auth middleware, user in Echo context
2. **Component 3** — world/checkpoint data, WASM artifacts, game server ports
3. **Component 5** — EventBus subscriptions, chat/notification messages

## Success Criteria

### Automated Verification
- [ ] `templ generate` succeeds
- [ ] `cd harness && go build ./...` compiles with all views
- [ ] All templ files render without errors

### Manual Verification
- [ ] Login page shows "Sign in with GitHub" button
- [ ] Unauthenticated users are redirected to login
- [ ] Pending users see "waiting for approval" page
- [ ] Lobby shows user avatar, username, logout, and world list
- [ ] Overlay renders on top of game canvas
- [ ] Game canvas receives mouse/keyboard input through transparent areas
- [ ] World selector switches between worlds
- [ ] Checkpoint selector switches between checkpoints
- [ ] Prompt input submits and triggers build
- [ ] Build status updates in real-time via SSE
- [ ] Checkpoint tree shows correct fork structure
- [ ] Chat messages sent by one user appear in all connected browsers
- [ ] Build completion notifications appear with clickable [Play] button
- [ ] Clicking [Play] switches iframe to that checkpoint (same world) or navigates (cross world)
- [ ] Minimizing overlay hides chrome, shows floating button
- [ ] New messages while minimized increment unread badge
- [ ] Expanding overlay resets badge to 0, shows full history
- [ ] Lineage tab shows prompt/response chain from root to current checkpoint
- [ ] Lineage tab is read-only (no chat input)
- [ ] Chat log loads last 50 messages on connect
- [ ] Player joined/left notifications appear
