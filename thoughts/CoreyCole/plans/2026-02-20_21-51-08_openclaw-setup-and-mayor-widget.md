# OpenClaw Setup + Omnipresent Mayor Widget Implementation Plan

## Overview

Set up OpenClaw on the VPS so per-world mayor agents can run with memory/context management, then build a persistent "clippy-like" mayor chat widget in the harness UI. The widget uses OpenClaw's `/v1/chat/completions` endpoint for direct, low-latency streaming chat with full agent memory and context compaction -- no Discord round-trip needed for web conversations.

## Current State Analysis

**OpenClaw is NOT installed or running on this VPS:**
- `/opt/openclaw/` does not exist (no binary)
- `data/openclaw/` does not exist (no workspaces, no config, no agents)
- No systemd service for the gateway
- Node v22.22.0 is available; pnpm is NOT installed
- Reference source exists at `context/openclaw/` (v2026.2.20)

**Harness gracefully degrades without OpenClaw:**
- `initMayorManager` (`harness/main.go:350-389`) returns nil if Discord env vars are missing
- CLI calls fail silently when binary doesn't exist -- server keeps running
- `checkGatewayHealth()` returns false, informational only

**Mayor provisioning has a bug:**
- `provisionAgent()` (`harness/internal/mayor/openclaw.go:21-46`) creates the agent but does NOT call `BindAgentToDiscord()` -- mayors never get bound to their Discord channels
- Only the president calls channel binding (`harness/internal/president/president.go`)

**Current mayor chat is fragmented:**
- World overlay "Mayor" tab: read-only Discord mirror (`harness/views/chat/chat.templ:32-36,40`)
- Mayor dashboard: read-only messages tab
- Create-world onboarding: bidirectional but pre-world only (`harness/internal/server/create.go:127-393`)
- No interactive mayor chat after world creation except via Discord directly

### Key Discoveries:
- OpenClaw `/v1/chat/completions` returns synchronous/streaming responses, uses `messageChannel: "webchat"` -- no Discord needed (`context/openclaw/src/gateway/openai-http.ts:156-384`)
- Must be enabled via config: `gateway.http.endpoints.chatCompletions.enabled: true` (default: false)
- Gateway auth modes: `none`, `token`, `password`, `trusted-proxy` (`context/openclaw/src/config/types.gateway.ts:79,106-122`)
- OpenClaw has semantic memory search (SQLite + vector embeddings), context compaction (multi-stage summarization), and session transcript storage
- `harness-run.sh:28` already sets `OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw`
- The CM FAB button is at `fixed bottom-4 right-4` (`harness/views/world/overlay.templ:26`) -- mayor widget goes bottom-left
- Port 18789 is OpenClaw's default gateway port (`context/openclaw/src/config/types.gateway.ts:280`: `port?: number` with JSDoc "default: 18789"). No explicit port config needed in `openclaw.json`.
- OpenClaw session continuity requires either a `user` field in the request body or an `x-openclaw-session-key` header. Without either, each request gets a random UUID session key with no conversation memory. The `user` field produces deterministic keys: `agent:<agentId>:openai-user:<user>` (`context/openclaw/src/gateway/http-utils.ts:65-79`, `context/openclaw/src/routing/session-key.ts:132-139`).
- Agent routing via `model` field: OpenClaw resolves agent IDs from model strings like `openclaw/<agentId>` (`context/openclaw/src/gateway/http-utils.ts:36-50`). Cleaner than the `x-openclaw-agent` header.

## Desired End State

1. OpenClaw gateway running as a systemd service on port 18789
2. Mayor agents provisioned and bound to Discord channels (bug fixed)
3. Persistent mayor chat widget (bottom-left FAB) on lobby + world pages
4. Users can chat with their world's mayor via streaming responses from OpenClaw
5. World selector in the widget for users with multiple worlds
6. Explicit "Build" mode toggle to trigger fork pipeline from chat context
7. Mayor tab removed from the world overlay chat panel

### Verification:
- `curl http://localhost:18789/health` returns 200
- `openclaw agents list` shows registered agents
- Harness UI shows mayor FAB on lobby and world pages
- Clicking FAB opens chat panel, selecting a world loads history
- Sending a message streams a mayor response in real-time
- Build mode triggers the existing fork pipeline

## What We're NOT Doing

- Discord echo of web chat messages (web chat stays in OpenClaw sessions only)
- President agent setup (separate effort, env vars not configured)
- Modifying the create-world onboarding flow
- WebSocket RPC client (using HTTP `/v1/chat/completions` instead)
- Custom OpenClaw plugins or hooks beyond config

## Implementation Approach

**Two-track approach**: Phase 1-2 set up OpenClaw infrastructure. Phase 3-5 build the UI widget. Each phase is independently verifiable.

The widget uses OpenClaw's OpenAI-compatible `/v1/chat/completions` endpoint with `stream: true`. The harness acts as a proxy: browser -> Datastar SSE -> harness Go handler -> HTTP streaming to OpenClaw -> response tokens streamed back to browser as templ fragment patches. This gives us OpenClaw's full memory/compaction while keeping the Datastar rendering pattern.

---

## Phase 1: Install OpenClaw

### Overview
Install OpenClaw from the reference source, install pnpm, build, and create a systemd service.

### Changes Required:

#### 1. Install pnpm and build OpenClaw
```bash
# Install pnpm globally
npm install -g pnpm

# Copy reference source to /opt/openclaw/
sudo cp -r /home/deploy/creative-mode/context/openclaw /opt/openclaw
cd /opt/openclaw
sudo chown -R deploy:deploy /opt/openclaw
pnpm install --frozen-lockfile
pnpm build
```

#### 2. Create systemd service
**File**: `/etc/systemd/system/openclaw-gateway.service`
```ini
[Unit]
Description=OpenClaw Gateway
After=network.target

[Service]
Type=simple
User=deploy
WorkingDirectory=/opt/openclaw
ExecStart=/opt/openclaw/node_modules/.bin/openclaw gateway run
Restart=on-failure
RestartSec=5
Environment=OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
Environment=ANTHROPIC_API_KEY=<from harness .env>
KillMode=process

[Install]
WantedBy=multi-user.target
```

#### 3. Run setup script and configure
```bash
# Set env vars and run setup
export OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
export DISCORD_BOT_TOKEN=<from .env>
export DISCORD_GUILD_ID=<from .env>
bash /home/deploy/creative-mode/harness/scripts/setup-openclaw.sh
```

Then manually edit `$OPENCLAW_HOME/openclaw.json` to add:
```json
{
  "gateway": {
    "auth": {
      "mode": "token",
      "token": "<generate with: openssl rand -hex 32>"
    },
    "http": {
      "endpoints": {
        "chatCompletions": { "enabled": true }
      }
    }
  },
  "hooks": {
    "enabled": true,
    "token": "<generate with: openssl rand -hex 32>"
  }
}
```

#### 4. Start and verify
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now openclaw-gateway
curl http://localhost:18789/health  # should return 200
```

#### 5. Add env vars to harness
**File**: `harness/.env` (not checked in)
Add: `OPENCLAW_GATEWAY_TOKEN=<the gateway auth token from step 3>`

**File**: `harness/.env.example`
Add:
```
# OpenClaw gateway auth token (for /v1/chat/completions endpoint)
# OPENCLAW_GATEWAY_TOKEN=

# OpenClaw gateway URL (default: http://localhost:18789)
# OPENCLAW_GATEWAY_URL=http://localhost:18789
```

### Success Criteria:

#### Automated Verification:
- [ ] `/opt/openclaw/node_modules/.bin/openclaw --version` returns version
- [ ] `systemctl is-active openclaw-gateway` returns `active`
- [ ] `curl http://localhost:18789/health` returns 200
- [ ] `$OPENCLAW_HOME/openclaw.json` exists with correct config
- [ ] `$OPENCLAW_HOME/workspaces/` directory exists

#### Manual Verification:
- [ ] `journalctl -u openclaw-gateway -n 20` shows clean startup logs
- [ ] Gateway survives a restart: `sudo systemctl restart openclaw-gateway`

---

## Phase 2: Fix Mayor Provisioning + Discord Binding

### Overview
Fix the bug where mayor agents are created but never bound to Discord channels. Add binding call to provisioning flow.

### Changes Required:

#### 1. Add Discord binding to mayor provisioning
**File**: `harness/internal/mayor/openclaw.go`
**Changes**: Call `BindAgentToDiscord` after `createAgentViaCLI` in `provisionAgent()`

```go
// provisionAgent creates the OpenClaw agent, writes workspace files, and binds
// it to a Discord channel.
func (m *Manager) provisionAgent(
	agentID, worldID, worldName, mayorName, mayorSecret string,
	onboarding any,
) error {
	workspaceDir := filepath.Join(m.openclawHome, "workspaces", agentID)

	if err := writeWorkspaceFiles(
		workspaceDir, worldID, worldName, mayorName, mayorSecret,
		m.harnessURL, onboarding,
	); err != nil {
		return fmt.Errorf("writing workspace files: %w", err)
	}

	if err := m.createAgentViaCLI(agentID, workspaceDir); err != nil {
		return fmt.Errorf("creating agent: %w", err)
	}

	return nil
}
```

The caller `ProvisionFromWebhook` (`mayor.go:142-153`) already has `discordChannelID` available. Add the binding call there after `provisionAgent` returns, before updating the DB:

```go
	// Bind agent to its Discord channel so it responds to messages.
	if err := m.BindAgentToDiscord(agentID, discordChannelID); err != nil {
		m.logger.Warn("failed to bind agent to discord, agent created but won't respond",
			"agent_id", agentID,
			"channel_id", discordChannelID,
			"error", err,
		)
		// Non-fatal: agent exists but won't auto-respond in Discord
	}
```

#### 2. Register new Discord channels with the listener
**File**: `harness/internal/mayor/mayor.go`
**Changes**: After successful provisioning, call `discordListener.RegisterChannel()` so the listener starts mirroring messages immediately (without requiring harness restart).

This requires passing the Discord listener to the mayor manager or using an event callback. Simplest approach: add a `OnProvision` callback field to `Manager`:

```go
type Manager struct {
	// ... existing fields ...
	OnProvision func(channelID, worldID string) // called after successful provisioning
}
```

Wire it in `main.go` to call `discordListener.RegisterChannel(channelID, worldID)`. Guard against nil listener:

```go
// In main.go, after discordListener is initialized:
if discordListener != nil && mayorManager != nil {
    mayorManager.OnProvision = func(channelID, worldID string) {
        discordListener.RegisterChannel(channelID, worldID)
    }
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes from project root
- [ ] Harness builds: `cd harness && go build -o /tmp/harness .`

#### Manual Verification:
- [ ] Create a new world via the site onboarding flow
- [ ] `openclaw agents list` shows the new `world-{id}` agent
- [ ] `openclaw config get bindings` includes a binding for the new agent's Discord channel
- [ ] The mayor responds to messages in the Discord channel
- [ ] Messages from Discord appear in `mayor_messages` table (listener mirroring works)

---

## Phase 3: OpenClaw HTTP Client in Harness

### Overview
Build a Go HTTP client that calls OpenClaw's `/v1/chat/completions` with streaming. This is the backend for the mayor chat widget.

### Changes Required:

#### 1. New OpenClaw client package
**File**: `harness/internal/openclaw/client.go` (new)

```go
package openclaw

// Client calls the OpenClaw gateway's OpenAI-compatible API.
type Client struct {
	baseURL string // e.g. "http://localhost:18789"
	token   string // gateway auth token
	client  *http.Client
}

// ChatCompletionRequest mirrors the OpenAI chat completion request format.
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	User     string        `json:"user,omitempty"` // deterministic session key for conversation continuity
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChat sends a streaming chat request and calls onDelta for each token.
// Returns the full accumulated response text.
//
// The userID is set as the `user` field in the request, which OpenClaw uses to
// build a deterministic session key: agent:<agentId>:openai-user:<userID>.
// This ensures conversation continuity across requests for the same user+agent.
func (c *Client) StreamChat(ctx context.Context, agentID, userID string, messages []ChatMessage, onDelta func(text string)) (string, error) {
	req := ChatCompletionRequest{
		Model:    "openclaw/" + agentID, // agent routing via model field
		Messages: messages,
		Stream:   true,
		User:     userID, // deterministic session key
	}
	// POST to /v1/chat/completions
	// Set Authorization: Bearer <token>
	// Parse SSE response, extract delta content tokens
	// Call onDelta for each token
	// Return accumulated full text
}
```

Key implementation details:
- Uses `Authorization: Bearer <token>` for gateway auth
- Sets `model` to `"openclaw/" + agentID` for agent routing. OpenClaw resolves agent IDs from model strings matching `openclaw/<agentId>` (`http-utils.ts:43`). This is cleaner than the `x-openclaw-agent` header and follows OpenAI-compatible conventions.
- Sets `user` to `userID` for deterministic session keys. OpenClaw builds key `agent:<agentId>:openai-user:<userID>` — same user+agent always gets the same conversation history.
- Parses SSE stream in OpenAI format: `data: {"choices":[{"delta":{"content":"token"}}]}`
- Handles `data: [DONE]` sentinel
- Returns a clear error when the gateway is unreachable (used by chat handler for "mayor offline" UX)

#### 2. Wire client into server
**File**: `harness/internal/server/server.go`
Add field:
```go
type Server struct {
	// ... existing fields ...
	OpenClawClient *openclaw.Client
}
```

**File**: `harness/main.go`
Initialize if gateway token is configured:
```go
if token := os.Getenv("OPENCLAW_GATEWAY_TOKEN"); token != "" {
	gatewayURL := os.Getenv("OPENCLAW_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:18789"
	}
	srv.OpenClawClient = openclaw.NewClient(gatewayURL, token)
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `cd harness && go build -o /tmp/harness .` succeeds

#### Manual Verification:
- [ ] With a provisioned agent, test the client manually via a temporary test handler
- [ ] Verify streaming tokens arrive incrementally

---

## Phase 4: Mayor Widget UI

### Overview
Build the persistent bottom-left FAB and chat panel. This is the core user-facing feature.

### Changes Required:

#### 1. Copy mayor image
```bash
cp site/static/img/square-black-bg.jpeg harness/static/img/square-black-bg.jpeg
```

#### 2. New widget templ package
**Directory**: `harness/views/mayorwidget/`

**File**: `harness/views/mayorwidget/signals.go`
```go
type MayorWidgetSignals struct {
	MayorPanelOpen  bool   `json:"mayor_panel_open"`
	SelectedWorldID string `json:"selected_world_id"`
	MayorInput      string `json:"mayor_input"`
	MayorBuildMode  bool   `json:"mayor_build_mode"`
}
```

Embed into `LobbySignals` and `OverlaySignals`.

**File**: `harness/views/mayorwidget/expressions.go`
Expression helpers using `dsutil.SignalManager` pattern (follow `harness/views/world/expressions.go`).

**File**: `harness/views/mayorwidget/widget.templ`

Two states:

If `worlds` is empty (no worlds with mayors), the widget is not rendered at all — users without worlds should create one via `/create`.

**FAB Button** (bottom-left):
```
fixed bottom-4 left-4, w-12 h-12 rounded-full
<img> with square-black-bg.jpeg, object-cover rounded-full
data-show="!$mayor_panel_open"
data-on:click="$mayor_panel_open = true"
Unread badge (same pattern as CM button at overlay.templ:31-37)
```

**Chat Panel** (above FAB):
```
fixed bottom-20 left-4 w-80 max-h-[70vh] z-50
flex flex-col, bg-[rgba(17,17,17,0.95)] backdrop-blur-lg rounded-lg border border-border
data-show="$mayor_panel_open"
```

Panel sections:
- **Header**: World selector `<select data-bind:selected_world_id>` + close button. `data-on:change` triggers `@post('/api/mayor-widget/load')` to reload content.
- **Message area**: `<div id="mayor-chat-log" class="flex-1 overflow-y-auto">` -- initially empty or pre-loaded
- **Input area**: Text input bound to `$mayor_input` + mode toggle (Chat/Build) + Send button

**File**: `harness/views/mayorwidget/messages.templ`
Reuse `MayorChatMessage` from `harness/views/world/mayor_chat.templ` for display format consistency. Add a streaming placeholder template (pulsing cursor, like `create/fragments.templ:20-31`).

#### 3. Widget API handlers
**File**: `harness/internal/server/mayor_widget.go` (new)

**`POST /api/mayor-widget/load`** -- Load content for selected world:
1. Read `selected_world_id` from Datastar signals
2. Fetch recent messages: `GetRecentMayorMessages(ctx, worldID, 50)`
3. Return SSE patches replacing `#mayor-chat-messages` with message history

**`GET /api/mayor-widget/events`** -- Single SSE connection for the widget (lazy: established only when panel is open AND a world is selected):
1. Read `selected_world_id` from Datastar signals (sent as query param on GET)
2. Subscribe to `EventBus.Subscribe(selectedWorldID)` for initial world
3. On heartbeat tick (30s), re-read the current `selected_world_id` signal — if world changed, unsubscribe old, subscribe new
4. Stream `EventMayorMessage` events for the currently-selected world, patching `MayorChatMessage` into `#mayor-chat-messages` with append mode

The widget's `data-init` for this SSE is on an inner element with `data-show="$mayor_panel_open && $selected_world_id"`. This means no SSE connection until the user opens the panel AND selects a world. World switches within the same SSE connection avoid the `data-init` re-fire problem (Datastar's `data-init` only fires once per element lifecycle).

**`POST /api/mayor-widget/chat`** -- Send chat message to mayor:
1. Call `requireUser(c)` to get the authenticated user
2. Read `selected_world_id` + `mayor_input` from signals
3. Look up world, verify it has an OpenClaw agent
4. Store user message in `mayor_messages` table with `author_type="user"`, `author_name=user.DiscordUsername`
5. Publish `EventMayorMessage` to EventBus for immediate UI feedback
6. Call `OpenClawClient.StreamChat()` with agent ID and `user.ID` as the userID (for session continuity)
7. Stream each delta token back as templ fragment patches (append to `#mayor-chat-messages`)
8. When complete, store the full assistant response in `mayor_messages` table
9. Clear `mayor_input` signal
10. **Error handling**: If `StreamChat` returns an error (gateway unreachable, timeout, etc.), patch a system message "Mayor is currently offline. Try again later." into `#mayor-chat-messages`, store it in `mayor_messages` with `author_type="system"`, and still clear `mayor_input`.

**`POST /api/mayor-widget/build`** -- Trigger build from chat:
1. Call `requireUser(c)` to get the authenticated user
2. Read `selected_world_id` + `mayor_input` from signals
3. Look up the user's current checkpoint via `WorldManager.GetUserPosition(ctx, user.ID, worldID)`; if no position, fall back to first checkpoint from `WorldManager.GetCheckpointTree(ctx, worldID)`
4. Call `Orchestrator.HandlePrompt(ctx, worldID, cpID, promptText, user.ID)`
5. Return a confirmation message patched into the chat log
6. Clear `mayor_input`, set `mayor_build_mode = false`

#### 4. Add SQL query for worlds with mayors
**File**: `harness/internal/db/queries/worlds.sql`
```sql
-- name: GetWorldsWithMayors :many
SELECT id, name, mayor_name, discord_channel_id, cover_image_path
FROM worlds
WHERE mayor_name IS NOT NULL AND discord_channel_id IS NOT NULL
ORDER BY created_at ASC;
```

Run: `cd harness/internal/db && sqlc generate`

#### 5. Inject widget into pages

**File**: `harness/views/lobby/lobby.templ`
Add after the main flex container (around line 87):
```go
@mayorwidget.Widget(worlds, "")
```

**File**: `harness/views/lobby/signals.go`
Embed `MayorWidgetSignals` into `LobbySignals`.

**File**: `harness/views/world/world.templ`
Add inside the layout, after the overlay (around line 63):
```go
@mayorwidget.Widget(worlds, w.ID)
```
The second param pre-selects the current world.

**File**: `harness/views/world/signals.go`
Embed `MayorWidgetSignals` into `OverlaySignals`.

**File**: `harness/internal/server/server.go`
- Modify `handleRoot` to also fetch `GetWorldsWithMayors` and pass to lobby template
- Modify `handleWorldView` to also fetch `GetWorldsWithMayors` and pass to world template
- Register new routes under `approved` group

#### 6. Remove Mayor tab from overlay

**File**: `harness/views/chat/chat.templ`
- Delete Mayor tab button (lines 32-36)
- Delete `#mayor-chat-log` div (line 40)
- Update `data-show` on `#chat-log` (line 38): remove `&& $active_tab !== 'mayor'`

**File**: `harness/internal/server/events.go`
- Remove the `case events.EventMayorMessage:` block (lines 327-341) from `handleWorldEvent` -- the widget's own SSE handles this now

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness/internal/db && sqlc generate` succeeds
- [ ] `cd harness && templ generate` succeeds
- [ ] `just check` passes from project root
- [ ] `cd harness && go build -o /tmp/harness .` succeeds

#### Manual Verification:
- [ ] Mayor FAB visible bottom-left on lobby page with mayor image
- [ ] Mayor FAB visible bottom-left on world page
- [ ] CM button still works bottom-right on world page (no conflict)
- [ ] Clicking FAB opens chat panel
- [ ] World selector shows worlds with mayors
- [ ] Selecting a world loads message history
- [ ] Sending a message shows it immediately, then streams mayor response
- [ ] Responses render incrementally (streaming visible)
- [ ] Closing panel and reopening preserves state
- [ ] Mayor tab is gone from world overlay chat panel
- [ ] Build mode: toggling to Build and sending triggers fork pipeline

---

## Phase 5: Build Mode Integration

### Overview
Add explicit Build mode toggle so users can plan in chat then trigger forks.

### Changes Required:

#### 1. Mode toggle in widget input
In `widget.templ`, the input area has:
- Small toggle button: "Chat" / "Build" text that toggles `$mayor_build_mode`
- Visual indicator: input bg changes (slightly different border/label) when in build mode
- Send button label changes: "Send" vs "Build"
- Conditional POST target: `@post('/api/mayor-widget/chat')` vs `@post('/api/mayor-widget/build')`

#### 2. Build handler
The `POST /api/mayor-widget/build` handler (already detailed in Phase 4 section 3):
- Calls `requireUser(c)` to get the authenticated user
- Reads `selected_world_id` + `mayor_input` from signals
- Resolves checkpoint server-side: `WorldManager.GetUserPosition(ctx, userID, worldID)`, falling back to first checkpoint from `WorldManager.GetCheckpointTree(ctx, worldID)` if no position exists
- Calls `Orchestrator.HandlePrompt(ctx, worldID, cpID, promptText, userID)`
- Returns a confirmation message patched into the chat log
- Sets `build_status` signal to "editing" to show build progress
- Clears `mayor_input`, sets `mayor_build_mode = false`

### Success Criteria:

#### Manual Verification:
- [ ] Toggle switches between Chat and Build modes visually
- [ ] Chat mode sends to OpenClaw, streams response
- [ ] Build mode triggers fork + Claude Code session
- [ ] Build status shows in the world overlay (if on world page)

---

## Testing Strategy

### Manual Testing Steps:
1. Start harness, navigate to lobby -- verify FAB appears
2. Click FAB -- verify panel opens with world selector
3. Select a world with a mayor -- verify history loads
4. Type a message and send -- verify instant echo + streaming response
5. Navigate to a world page -- verify FAB appears with world pre-selected
6. Verify CM button (bottom-right) still works independently
7. Open overlay chat -- verify Mayor tab is gone
8. Toggle to Build mode -- send a build prompt -- verify fork starts
9. Check Discord channel -- verify mayor agent responds to direct Discord messages too

## Performance Considerations

- `/v1/chat/completions` streaming avoids buffering the full response
- Widget SSE is **lazy** — no connection until panel is opened AND a world is selected. This means zero SSE overhead when the widget is minimized.
- Widget uses a single SSE endpoint that switches world subscriptions internally (no connection teardown on world switch)
- Message history limited to 50 recent messages on load
- OpenClaw handles context compaction internally -- no harness-side concern
- World page has two SSE connections (overlay + widget) — both are lightweight and serve different purposes. The widget SSE only activates when the panel is open.

## Migration Notes

- No DB schema changes required (reusing existing `mayor_messages` table)
- Web chat messages are stored in the same `mayor_messages` table as Discord-mirrored messages. This is intentional — it gives a unified conversation view where users see both Discord and web chat history in the widget. The `author_type` field distinguishes sources. Discord-originated messages have `discord_message_id` set; web chat messages have it NULL.
- New SQL query `GetWorldsWithMayors` is additive
- Existing worlds without mayors are unaffected (widget is hidden entirely when no worlds have mayors)
- OpenClaw config hot-reloads on changes (~200ms via chokidar)

## References

- Review: `thoughts/CoreyCole/reviews/2026-02-20_21-58-45_openclaw-setup-and-mayor-widget_review.md`
- Research: `thoughts/CoreyCole/research/2026-02-16_11-58-54_omnipresent-mayor-assistant.md`
- Prior plan: `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md`
- OpenClaw source: `context/openclaw/` (v2026.2.20)
- OpenClaw gateway hooks: `context/openclaw/src/gateway/hooks.ts`
- OpenClaw chat completions: `context/openclaw/src/gateway/openai-http.ts:156-384`
- OpenClaw config types: `context/openclaw/src/config/types.gateway.ts:170-256`
- CM button pattern: `harness/views/world/overlay.templ:26-39`
- Create-world chat model: `harness/internal/server/create.go:127-393`
- Mayor provisioning: `harness/internal/mayor/mayor.go:54-191`
- Mayor provisioning bug (no binding): `harness/internal/mayor/openclaw.go:21-46`
- Discord listener: `harness/internal/discord/listener.go:109-153`
- EventBus: `harness/internal/events/bus.go`
- World SSE mayor handling (to remove): `harness/internal/server/events.go:327-341`
- OpenClaw session key resolution: `context/openclaw/src/gateway/http-utils.ts:65-79`
- OpenClaw session key format: `context/openclaw/src/routing/session-key.ts:132-139`
- OpenClaw agent ID from model: `context/openclaw/src/gateway/http-utils.ts:36-50`
