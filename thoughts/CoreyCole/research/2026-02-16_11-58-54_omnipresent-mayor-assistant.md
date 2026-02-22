---
date: 2026-02-16T11:58:54-08:00
researcher: CoreyCole
git_commit: a9ad7f5a52eabdedeff48af51c45921e3b258def
branch: main
repository: creative-mode
topic: "Omnipresent Mayor Assistant: World Selector, Clippy-like Chat, and Plan-Before-Fork"
tags: [research, codebase, mayor, chat, world-selector, build-pipeline, clippy-assistant]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Omnipresent Mayor Assistant

**Date**: 2026-02-16T11:58:54-08:00
**Researcher**: CoreyCole
**Git Commit**: a9ad7f5a52eabdedeff48af51c45921e3b258def
**Branch**: main
**Repository**: creative-mode

## Research Question

We want to make the mayor omnipresent when in the harness server. A harness can have multiple mayors, so we need a "world selector" to decide which mayor is visible. The mayor should be a clippy-style assistant guiding users through the platform. Currently, text chat always triggers a fork/build -- we want to allow planning before kicking off the fork.

## Summary

The current architecture has **three separate chat contexts** (world overlay Mayor tab, mayor dashboard, create-world onboarding) with no persistent cross-page chat widget. There is **no shared navigation** in the base layout -- each page renders independently. The build pipeline goes **straight from prompt to fork** with no planning phase. Making the mayor omnipresent requires: (1) a persistent chat widget injected at the layout level, (2) a world selector to multiplex between mayors, (3) a new "plan" conversation mode that doesn't trigger forks, and (4) a new API endpoint for direct mayor chat that bypasses the build pipeline.

## Detailed Findings

### 1. Current Chat Architecture (Three Isolated Contexts)

The mayor chat exists in three disconnected places:

**A. World Overlay "Mayor" Tab** (`harness/views/world/overlay.templ`, `harness/views/chat/chat.templ:40`)
- Tab within a 5-tab chat panel (Global, World, Lineage, Assets, Mayor)
- Messages come from Discord mirroring via EventBus -> SSE -> `#mayor-chat-log`
- Only visible when on `/world/:worldID` page with overlay expanded and Mayor tab selected
- No input mechanism for sending messages TO the mayor -- it's read-only display of Discord messages

**B. Mayor Dashboard** (`harness/views/mayor/dashboard.templ`)
- Full-page view at `/mayor/:worldID` with 5 tabs (Overview, Builds, Activity, Messages, Memory)
- Messages tab shows all `mayor_messages` from SQLite
- Also read-only -- no way to send messages to the mayor from the dashboard

**C. Create-World Onboarding Chat** (`harness/views/create/page.templ`)
- Full-page conversational UI at `/create`
- Direct Anthropic API (Claude Sonnet 4.5) streaming, NOT Discord/OpenClaw
- Uses `pkg/mayorchat` for conversation management
- Has bidirectional chat (user types, mayor responds via SSE streaming)
- This is the ONLY place with actual interactive mayor chat

**Key gap**: After world creation, there is no way for users to chat with their mayor through the harness UI. All post-creation mayor communication happens exclusively through Discord.

### 2. Current Build Pipeline (No Planning Phase)

The prompt-to-build flow has **zero planning steps**:

```
User prompt -> handlePrompt (server.go:451)
  -> Orchestrator.HandlePrompt (claude.go:73)
    -> WorldManager.ForkCheckpoint (manager.go:224)  // IMMEDIATE FORK
      -> copyDir (entire project directory)
      -> cloneBuildCache (hardlinks for target/)
      -> DB insert: status="building"
    -> updateMemory (append prompt to MEMORY.md)
    -> session.Create (tmux session)
    -> session.SendPrompt (Claude Code execution)
```

The fork happens immediately at `manager.go:224-308` -- the entire source directory is copied before any planning. The only "plan" concept is advisory text in the mayor's `AGENTS.md` (`mayor/agents.go:16`) which tells the OpenClaw agent to "Plan" before building, but this is not enforced by code.

**Rate limiting** exists (`world/rate_limit.go:33-74`) -- 30-second cooldown between prompts and prevents concurrent builds per user. But there's no "draft" or "plan" state before committing to a fork.

### 3. Base Layout (No Persistent UI Elements)

The base layout at `harness/views/layout/layout.templ:25-38` is minimal:

```go
templ Base(title string) {
    <!DOCTYPE html>
    <html lang="en" class="dark">
        @Head(title)
        <body>
            <div id="page-content">
                { children... }
            </div>
        </body>
    </html>
}
```

**No shared navigation, no sidebar, no persistent header.** Each page renders its own chrome:
- Lobby: has its own header + right sidebar chat panel
- World: fullscreen iframe + overlay with "CM" floating button
- Create: full-page chat UI
- Mayor Dashboard: standalone tabbed view
- Login/Pending: standalone pages

### 4. World Selection (Currently URL-Only)

Worlds are selected via standard `<a>` link navigation from the lobby grid (`lobby.templ:42-68`). There is:
- **No world switcher dropdown** anywhere
- **No persistent world list** outside the lobby
- **No "active world" session state** -- context is purely URL-driven (`:worldID` param)
- The only cross-world navigation is `game-loader.js:17-19` which handles `navigate-world` postMessages from WASM games

Per-user checkpoint position is tracked in `user_positions` table, but only within a single world.

### 5. The "CM" Button (Closest to Clippy)

The floating "CM" button (`overlay.templ:26-39`) is the closest existing UI to a clippy assistant:
- Fixed bottom-right, 48px circle, primary color
- Shows unread badge when `$unread_count > 0`
- Clicking expands the full overlay (chat tabs + game controls)
- Only visible on world pages, not lobby or other pages

### 6. Multi-World Mayor Support

The harness already supports multiple worlds with mayors:
- `mayor.Manager` is a singleton handling all worlds via `worldID` key
- EventBus has per-world subscriber channels (`bus.go:11`: `worldSubs map[string][]chan any`)
- Discord listener maps channel IDs to world IDs (`listener.go:27`: `channelMap map[string]string`)
- Each mayor authenticates via unique `mayor_secret` per world
- `ListWorlds` query returns all worlds (`worlds.sql.go:161-202`)

### 7. Existing Prompt UI

The prompt input in the world overlay (`harness/views/world/overlay.templ:79-127`) uses:
- Signal: `prompt_text` bound to textarea
- Signal: `current_checkpoint_id` for fork source
- Submit: `@post('/world/:worldID/prompt')` via Datastar
- Build status indicator via `$build_status` signal

## Architecture Proposal

### A. Persistent Mayor Chat Widget

**Injection point**: Add to `layout.Base` after `#page-content`, conditionally rendered for authenticated users:

```go
templ Base(title string) {
    <!DOCTYPE html>
    <html lang="en" class="dark">
        @Head(title)
        <body>
            <div id="page-content">
                { children... }
            </div>
            <div id="mayor-assistant"></div>  <!-- persistent widget mount point -->
        </body>
    </html>
}
```

Or better: create a new `AuthenticatedBase` layout that wraps `Base` and adds the widget, used by lobby/world/mayor-dashboard/create pages but not login/pending.

**Widget UI**: Floating bottom-right button (like current "CM" button) that expands to a chat panel. Should have:
- World selector dropdown at the top
- Chat message history from `mayor_messages` table
- Input field for sending messages to the mayor
- Streaming response display (like create-world chat)
- Minimize/expand toggle

### B. World Selector

**Data source**: `ListWorlds` already returns all worlds. Need a new query `GetUserWorlds` that returns worlds the user has visited (via `user_positions` table join) or all worlds they have access to.

**Signal**: Add `selected_world_id` signal to the persistent widget. Changing it:
1. Loads that world's mayor message history via new API endpoint
2. Switches the SSE subscription to that world's events
3. Updates the chat panel header with world name + mayor name

**UI**: Dropdown or tab strip showing world names + mayor names. Could show unread counts per world.

### C. Plan-Before-Fork Mode

**New conversation mode**: When user sends a message to the mayor via the persistent widget, it should NOT immediately fork. Instead:

1. **Chat mode** (default): Message goes to mayor for conversational response. No fork, no build. Mayor can answer questions about the platform, explain features, discuss ideas.

2. **Build mode** (explicit): User says "build this" or clicks a "Start Build" button. Only THEN does the fork/build pipeline trigger.

**Implementation options**:

- **Option 1: Direct Anthropic API** (like create-world chat): New `POST /api/mayor/chat` endpoint that uses the same Claude streaming approach as `/create/chat`. The mayor's personality comes from its workspace files (SOUL.md, MEMORY.md). No Discord involvement. Messages stored in a new `mayor_chat_messages` table (separate from Discord-mirrored `mayor_messages`).

- **Option 2: Through OpenClaw**: Send messages to the OpenClaw agent via its API, let it respond. The response flows back through Discord -> listener -> EventBus -> SSE. This keeps conversation history unified in Discord but adds latency and requires Discord as intermediary.

- **Option 3: Hybrid**: Chat mode uses direct Anthropic API for low-latency responses. Build mode delegates to the existing pipeline. Mayor memory (SOUL.md, MEMORY.md) is shared between both modes.

**Recommendation**: Option 3 (Hybrid). Chat responses should be fast and don't need Discord. Build requests go through the existing proven pipeline.

### D. New API Endpoints Needed

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/mayor-chat/:worldID` | POST | Send chat message to mayor (no fork) |
| `/api/mayor-chat/:worldID/history` | GET | Load chat history for a world's mayor |
| `/api/mayor-chat/:worldID/events` | GET | SSE stream for mayor chat responses |
| `/api/mayor-chat/:worldID/build` | POST | Explicitly trigger a build from chat context |

### E. Database Changes

New table for direct mayor chat (separate from Discord-mirrored messages):

```sql
CREATE TABLE mayor_chat_messages (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL,  -- 'user' or 'assistant'
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_mayor_chat_world_user ON mayor_chat_messages(world_id, user_id, created_at);
```

## Code References

- `harness/views/layout/layout.templ:25-38` - Base layout (injection point for persistent widget)
- `harness/views/world/overlay.templ:26-39` - "CM" floating button (closest clippy model)
- `harness/views/chat/chat.templ:9-46` - Tabbed chat panel with Mayor tab
- `harness/views/world/mayor_chat.templ:6-26` - Mayor chat message display templates
- `harness/views/create/page.templ:8-87` - Create-world onboarding chat (model for bidirectional chat)
- `harness/views/create/fragments.templ` - Streaming message fragment templates
- `harness/internal/server/server.go:451-535` - `handlePrompt` (current fork trigger)
- `harness/internal/server/server.go:714-756` - `handleChat` (global chat handler)
- `harness/internal/server/create.go:127-393` - `handleCreateChat` (streaming mayor chat model)
- `harness/internal/claude/claude.go:73-135` - `HandlePrompt` (orchestrator fork + Claude Code)
- `harness/internal/world/manager.go:224-308` - `ForkCheckpoint` (directory copy)
- `harness/internal/events/bus.go:1-105` - EventBus (pub/sub for SSE)
- `harness/internal/events/types.go:3-15` - Event type constants
- `harness/internal/discord/listener.go:109-153` - Discord message mirroring
- `harness/internal/mayor/mayor.go:54-191` - Mayor provisioning
- `harness/internal/mayor/workspace.go:11-48` - Workspace file writing (SOUL.md, MEMORY.md etc.)
- `harness/internal/server/events.go:56-114` - World SSE handler
- `harness/internal/server/events.go:327-341` - Mayor message SSE patching
- `harness/internal/server/mayor_dashboard.go:19-117` - Mayor dashboard + SSE
- `harness/internal/server/mayor_api.go:83-174` - Mayor auth middleware + build handler
- `harness/views/world/signals.go:13-51` - Overlay signals definition
- `harness/views/lobby/lobby.templ:42-68` - World card grid
- `harness/internal/auth/middleware.go` - Session/auth middleware chain
- `pkg/mayorchat/` - Mayor chat conversation management package

## Architecture Insights

1. **Datastar SSE pattern is well-established**: The codebase consistently uses `data-signals` + `data-init={ dsutil.GetSSENoCancel(...) }` for reactive state. The persistent widget should follow this same pattern.

2. **Fragment patching is the rendering model**: Server renders templ components and sends them as SSE `patchElement` events. The widget should use the same approach -- server-rendered HTML fragments, not client-side JS rendering.

3. **EventBus per-world multiplexing works**: The existing `worldSubs` map already supports multiple concurrent world subscriptions. The widget's SSE connection needs to subscribe/unsubscribe as the user switches worlds.

4. **The create-world chat is the best model**: `create.go:127-393` demonstrates bidirectional streaming mayor chat using the Anthropic API with Datastar SSE. This is the pattern to replicate for the persistent chat widget.

5. **No SPA routing -- full page reloads**: The app uses standard link navigation. The persistent widget must survive page transitions. Options: (a) re-initialize on each page load with chat history from DB, or (b) use an iframe for the widget so it persists across navigations. Option (a) is more aligned with the current architecture.

6. **Mayor personality files are on disk**: SOUL.md, MEMORY.md, IDENTITY.md live in `{OPENCLAW_HOME}/workspaces/world-{worldID}/`. The persistent chat's system prompt should read these files to maintain mayor personality.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` - Final master plan for the agent hierarchy
- `thoughts/CoreyCole/plans/2026-02-10-component-6-ui-overlay-chat.md` - Original UI overlay + chat system plan
- `thoughts/CoreyCole/plans/2026-02-11-wave4-ui-overlay-chat.md` - Refined chat implementation plan
- `thoughts/CoreyCole/plans/2026-02-14_03-02-42_meet-the-mayor-site-page.md` - "Meet the Mayor" conversational page plan (model for bidirectional chat)
- `thoughts/CoreyCole/research/2026-02-10-harness-architecture-evaluation.md` - Early harness architecture evaluation including world selector concepts
- `thoughts/CoreyCole/handoffs/general/2026-02-13_15-25-28_mayor-dashboard-design.md` - Mayor dashboard design notes
- `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` - OpenClaw integration research

## Open Questions

1. **Should the persistent chat replace the current Mayor tab in the world overlay?** Having two places to see mayor messages could be confusing. The persistent widget could subsume the Mayor tab entirely.

2. **Should chat messages flow through Discord?** The persistent chat could either: (a) use direct Anthropic API (faster, simpler), (b) post to Discord so the mayor agent sees them (unified conversation), or (c) hybrid where chat is direct but builds go through Discord. This affects conversation continuity between browser and Discord.

3. **How should the "plan" phase work concretely?** Options:
   - Mayor responds with a structured plan (markdown), user clicks "Approve & Build"
   - Free-form conversation until user explicitly triggers build
   - Mayor asks clarifying questions, then presents a diff preview before forking

4. **Should the widget be visible on ALL authenticated pages or just specific ones?** Lobby, world, and mayor dashboard make sense. Admin page maybe not. Create-world page already has its own chat.

5. **SSE connection management**: Should the widget maintain its own SSE connection separate from the page's connection? Or should there be a single SSE connection per page that includes widget events?

6. **Conversation persistence scope**: Per-user-per-world? Or global per-user with world context switching? The `mayor_chat_messages` table design above is per-world-per-user, which seems right.

7. **Rate limiting for chat vs build**: Chat responses (no fork) should have lighter rate limits than build requests. The current 30-second cooldown applies to forks -- chat should be near-instant.
