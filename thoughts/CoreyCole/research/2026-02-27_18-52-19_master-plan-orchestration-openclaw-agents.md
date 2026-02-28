---
date: 2026-02-27T18:52:19-08:00
researcher: Corey Cole
git_commit: b8ca5ea
branch: main
repository: creative-mode
topic: "Master Plan Orchestration: OpenClaw Agents for President + Mayors"
tags: [research, codebase, openclaw, mayor, president, discord, streaming, agent-orchestration]
status: complete
last_updated: 2026-02-27
last_updated_by: Corey Cole
---

# Research: Master Plan Orchestration — OpenClaw Agents for President + Mayors

**Date**: 2026-02-27T18:52:19-08:00
**Researcher**: Corey Cole
**Git Commit**: b8ca5ea
**Branch**: main
**Repository**: creative-mode

## Research Question

Design the master plan orchestration setup for OpenClaw agents (president + per-world mayors). Key questions:
1. How to hook up mayor chat in the onboarding site to harness via OpenClaw?
2. Can OpenClaw stream responses back?
3. Why aren't mayors responding to @mentions in Discord?
4. How to set up isolated mayor agents per world in OpenClaw?
5. How should the president agent work (maintainer-only, system improvement)?
6. Can we replace direct Anthropic API calls in `pkg/mayorchat` with OpenClaw?

## Summary

OpenClaw provides everything needed for multi-agent orchestration with streaming. Each world gets an isolated agent with its own workspace, session store, and Discord binding. The **critical bug** is that `BindAgentToDiscord()` is never called during mayor provisioning — agents are created but OpenClaw doesn't know which Discord channels to route to them. The president agent already binds correctly and serves as the reference implementation. Replacing direct Anthropic API calls with OpenClaw's `/v1/chat/completions` endpoint is feasible and gives us memory management, context compaction, and session continuity for free — but requires an architectural shift in how the onboarding chat works.

## Detailed Findings

### 1. OpenClaw Architecture — How It Works

OpenClaw is a TypeScript agent framework (Node 22+) built on pi-mono. Key architecture:

- **Gateway**: Single long-lived process on `ws://127.0.0.1:18789` (WS + HTTP multiplexed)
- **Per-Turn Execution**: Agents are NOT persistent processes. They're invoked per-message via the embedded `pi-coding-agent` runtime
- **Session Serialization**: Runs are serialized per session key via lanes, preventing races
- **Workspace-as-Identity**: Agent personality/memory/skills defined by markdown files on disk

**Source**: `context/openclaw/docs/concepts/architecture.md`, `context/openclaw/docs/concepts/agent-loop.md`

### 2. Streaming Capabilities — YES, Multiple Layers

OpenClaw supports streaming at 5 levels:

| Layer | Protocol | Use Case | Ref |
|-------|----------|----------|-----|
| WebSocket Events | WS frames | Real-time client (TUI, IDE) | `docs/concepts/agent-loop.md:127-131` |
| Block Streaming | Channel messages | Discord/Slack (chunked) | `docs/concepts/streaming.md:19-55` |
| Preview Streaming | Channel edits | Telegram/Discord (live edit) | `docs/concepts/streaming.md:108-156` |
| **OpenAI-compat HTTP SSE** | `text/event-stream` | **Our primary target** | `docs/gateway/openai-http-api.md:84-119` |
| OpenResponses HTTP SSE | `text/event-stream` | Alternative API | `docs/gateway/openresponses-http-api.md:267-287` |

**The OpenAI-compatible endpoint is the recommended integration point**:
- `POST /v1/chat/completions` with `stream: true` → SSE events
- Each event: `data: {"choices":[{"delta":{"content":"token"}}]}`
- Stream ends with `data: [DONE]`
- Must be enabled: `gateway.http.endpoints.chatCompletions.enabled: true` (default: false)
- Auth: `Authorization: Bearer <token>` or `none`/`password`/`trusted-proxy` modes
- Agent routing via `model` field: `"openclaw/<agentId>"` — cleaner than header-based routing

**Source**: `context/openclaw/docs/gateway/openai-http-api.md`, `context/openclaw/src/gateway/http-utils.ts:36-50`

### 3. Why Mayors Don't Respond to @Mentions — THE BUG

**Root cause**: Two gaps in the provisioning flow.

**Gap A: `BindAgentToDiscord()` is never called for mayors.**

The method exists at `harness/internal/mayor/openclaw.go:75-127` and correctly constructs bindings JSON. But `provisionAgent()` at `openclaw.go:21-46` only does:
1. Write workspace files (`writeWorkspaceFiles`)
2. Create agent via CLI (`createAgentViaCLI`)

It NEVER calls `BindAgentToDiscord()`. Compare with the president, which calls `bindToChannel()` at `harness/internal/president/president.go:115-120` after agent creation. Without bindings, OpenClaw's Discord adapter has no routing table — it doesn't know which agent handles which channel.

**Gap B: New channels aren't registered with the Discord listener.**

The `discordListener` is a local variable in `main()` at `main.go:266`. `RegisterChannel()` exists on the listener but is never called. New worlds provisioned at runtime don't get Discord mirroring until harness restart.

**Fix** (already designed in the 2026-02-20 plan):
1. Call `BindAgentToDiscord(agentID, discordChannelID)` after `createAgentViaCLI` in `ProvisionFromWebhook`
2. Add `OnProvision` callback to `Manager` struct, wired in `main.go` to call `discordListener.RegisterChannel()`

**Source**: `harness/internal/mayor/openclaw.go:21-46`, `harness/internal/discord/listener.go:80-84`, `harness/internal/president/president.go:115-120`

### 4. Multi-Agent Isolation in OpenClaw — Per-World Agents

Each agent is fully isolated with:

| Resource | Isolation |
|----------|-----------|
| Workspace | `{OPENCLAW_HOME}/workspaces/world-{worldID}/` — own SOUL.md, AGENTS.md, etc. |
| State dir | `~/.openclaw/agents/<agentId>/agent` — auth profiles, model registry |
| Sessions | `~/.openclaw/agents/<agentId>/sessions` — JSONL transcripts |
| Bindings | Per-agent routing rules in `openclaw.json` bindings array |
| Sandbox | Optional per-agent tool restrictions (`tools.allow`/`tools.deny`) |

**Routing resolution** (most-specific-wins):
1. `peer` match (exact channel ID) — **this is what we use for mayors**
2. `parentPeer` match (thread inheritance)
3. `guildId + roles` (Discord role routing)
4. `guildId` only
5. `accountId` match
6. Channel-level match
7. Fallback to default agent

**Binding format**: `{"agent": "world-abc12345", "channel": "discord:<channelID>"}`

Creative Mode currently registers agents via CLI (`openclaw agents add --id world-{worldID} --workspace {dir}`) and SHOULD set bindings via `openclaw config set bindings <json>`. The read-modify-write pattern in `BindAgentToDiscord` correctly preserves existing bindings.

**Source**: `context/openclaw/docs/concepts/multi-agent.md:42-56, 173-193, 493-539`

### 5. President Agent Setup

The president is already implemented and serves as the reference for correct OpenClaw integration:

| Aspect | Implementation |
|--------|---------------|
| Agent ID | `"president"` (static) |
| Workspace | `{OPENCLAW_HOME}/workspaces/president/` |
| Files | SOUL.md, AGENTS.md, IDENTITY.md, USER.md, MEMORY.md, HEARTBEAT.md (6 files) |
| Skills | 4: `mayor-status`, `repo-build`, `template-update`, `deploy` |
| Auth | Global `PRESIDENT_SECRET`, header `X-President-Secret` |
| Discord | Bound to `#creative-mode-dev` via `bindToChannel()` during provisioning |
| Trigger | Harness startup (once, idempotent via SOUL.md existence check) |

**Operating modes** (defined in AGENTS.md):
- **Heartbeat** (every 30 min): query mayor-status, review failed builds, diagnose cross-world patterns, check stale mayors, update MEMORY.md
- **Reactive** (on message): respond to maintainer messages in `#creative-mode-dev`

**Safety rules** (from SOUL.md):
- Autonomous: templates/, hook scripts, scripts/, MEMORY.md, thoughts/
- PR Required: harness/ code, flake.nix, DB migrations
- Forbidden: .env files, force-push, deleting worlds

**Currently disabled in production** — env vars `PRESIDENT_SECRET` and `DISCORD_PRESIDENT_CHANNEL_ID` not configured.

**Source**: `harness/internal/president/president.go`, `harness/internal/president/soul.go`, `harness/internal/president/agents.go`, `harness/internal/president/skills.go`

### 6. Can We Replace Direct Anthropic API Calls with OpenClaw?

**Current state**: `pkg/mayorchat` talks directly to the Anthropic API via `anthropic-sdk-go v1.22.1`. Both site and harness call `client.Messages.NewStreaming()` for token-by-token streaming.

**What OpenClaw gives us that direct API doesn't**:
- Session continuity (conversation memory across requests)
- Context compaction (multi-stage summarization for long conversations)
- Semantic memory search (SQLite + vector embeddings)
- Agent personality via workspace files (SOUL.md, etc.)
- Tool use (skills for world-build, world-status)
- Unified conversation across web + Discord

**Integration options**:

#### Option A: OpenClaw HTTP API (Recommended for post-onboarding chat)

Use `/v1/chat/completions` with `stream: true` for the mayor chat widget (already planned in Phase 3-4 of the 2026-02-20 plan):

```
Browser → Datastar SSE → harness Go handler → HTTP streaming to OpenClaw → tokens back → templ patches
```

- Agent routing: `model: "openclaw/world-{worldID}"`
- Session continuity: `user: "{userID}"` → deterministic key `agent:<agentId>:openai-user:<userID>`
- The harness acts as a streaming proxy, not an API consumer

**Already designed**: `harness/internal/openclaw/client.go` with `StreamChat()` method (Phase 3 of the 2026-02-20 plan).

#### Option B: Keep Direct Anthropic API for Onboarding (Recommended)

The onboarding chat in `pkg/mayorchat` has special requirements that make OpenClaw replacement complex:

1. **Pre-world**: During onboarding, no world exists yet → no OpenClaw agent exists yet → can't route to one
2. **WORLD_READY marker**: The system prompt instructs Claude to emit `WORLD_READY|...|...` when ready — this is parsed by Go code to trigger world creation. Moving the prompt into OpenClaw's SOUL.md means the parsing logic must still work on the harness side
3. **Template type detection**: The harness variant adds template type to the marker (7 fields vs 6)
4. **Scripted fallback**: When API is unavailable, the system falls back to hardcoded responses — this would bypass OpenClaw entirely anyway
5. **Cover art generation**: Integrated into the chat flow, uses Gemini (not Anthropic)

**Recommendation**: Keep `pkg/mayorchat` with direct Anthropic API for the onboarding conversation (it's a pre-agent, ephemeral interaction). After world creation + mayor provisioning, all subsequent chat goes through OpenClaw.

#### Option C: "Greeter Agent" in OpenClaw (Future possibility)

Create a special OpenClaw agent dedicated to onboarding:
- Agent ID: `"greeter"` (singleton)
- System prompt in SOUL.md contains the same onboarding instructions
- Route via `/v1/chat/completions` with `model: "openclaw/greeter"`
- Parse `WORLD_READY` markers from the streaming response

This would unify the chat backend but adds complexity. The greeter agent would need different workspace content than mayors. Consider this for a future iteration.

### 7. Current Mayor Chat Architecture

**Flow for both site and harness**:
```
1. User types message
2. Rate limit check (2s cooldown)
3. Message persisted via MessageStore interface
4. Claude streaming request via anthropic-sdk-go
5. Tokens streamed to browser via Datastar SSE
6. Incremental markdown rendering
7. WORLD_READY marker parsed on completion
8. World created (site: Discord channel + webhook; harness: direct DB insert)
```

**Key interfaces** (API-agnostic — would survive an OpenClaw migration):
- `MessageStore` / `ImageStore` interfaces (`conversation.go:11-25`)
- `ConversationManager` state machine (`conversation.go:56-62`)
- `Message` struct (`message.go:14-18`)
- `ParseWorldReady()` / `StripWorldReadyMarker()` (pure string parsing)
- All scripted fallback logic

**Would need to change for OpenClaw**:
- `client.go` — `NewClient()` returns `anthropic.Client` → replace with OpenClaw client
- `stream.go:110-138` — `BuildAnthropicMessages()` constructs Anthropic params → adapt for OpenClaw message format
- `stream.go:97-106` — `IsBillingOrOverloadError()` checks Anthropic error types → adapt for OpenClaw errors
- Streaming loops in `site/internal/mayor/handler.go:219-296` and `harness/internal/server/create.go:244-339`

### 8. OpenClaw Discord Adapter

OpenClaw has a Discord extension at `context/openclaw/extensions/discord/` that implements the `ChannelPlugin` interface. It:

- Uses discord.js (not discordgo)
- Supports direct messages, channel messages, and threads
- Has block streaming with coalescing (`minChars: 1500, idleMs: 1000`)
- Configured via `channels.discord.token` + guild settings in `openclaw.json`

**The harness Discord listener is separate** — it uses discordgo to mirror messages to SQLite for the dashboard/SSE pipeline. Both systems share the same bot token and operate independently.

**Important**: The Discord adapter must be configured in `openclaw.json` with the guild and channel permissions. The setup script handles this:
```bash
openclaw config set channels.discord.token "$DISCORD_BOT_TOKEN"
openclaw config set "channels.discord.guilds.$DISCORD_GUILD_ID.channels.*" '{"allow": true}'
```

## Architecture: Master Plan

```
┌─────────────────────────────────────────────────────────────────┐
│                    OpenClaw Gateway (:18789)                     │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Discord      │  │ HTTP API     │  │ Agent Runtime        │  │
│  │ Adapter      │  │ /v1/chat/    │  │ (pi-coding-agent)    │  │
│  │ (discord.js) │  │ completions  │  │ Per-turn execution   │  │
│  └──────┬───────┘  └──────┬───────┘  │ Session serialization│  │
│         │                  │          │ Context compaction   │  │
│         │    ┌─────────────┘          │ Memory + tools       │  │
│         │    │                        └──────────────────────┘  │
│  ┌──────┴────┴──────────────────────────────────────────────┐  │
│  │                    Binding Router                          │  │
│  │  world-abc → discord:#abc-channel                         │  │
│  │  world-def → discord:#def-channel                         │  │
│  │  president → discord:#creative-mode-dev                   │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    Agent Workspaces                        │  │
│  │  workspaces/world-abc/  (SOUL, AGENTS, IDENTITY, skills/) │  │
│  │  workspaces/world-def/  (SOUL, AGENTS, IDENTITY, skills/) │  │
│  │  workspaces/president/  (SOUL, AGENTS, HEARTBEAT, skills/)│  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
         ↑ Discord msgs          ↑ HTTP streaming
         │                       │
         │              ┌────────┴─────────┐
         │              │   Harness (:8080) │
         │              │                   │
         │              │  Mayor Widget     │──→ SSE → Browser
         │              │  (Go HTTP client  │
         │              │   streaming proxy)│
         │              │                   │
         │              │  Mayor Manager    │──→ CLI: openclaw agents add/bind
         │              │  President Mgr    │──→ CLI: openclaw agents add/bind
         │              │                   │
         │              │  Discord Listener │──→ Mirror to SQLite + EventBus
         │              │  (discordgo GW)   │
         │              └──────────────────┘
         │
    ┌────┴─────┐
    │ Discord  │  Users @mention mayor → OpenClaw responds
    │ Server   │  Build notifications → harness PostToDiscord()
    └──────────┘

    ┌──────────────────┐
    │ Site (EC2)       │
    │                  │
    │ Onboarding Chat  │──→ Direct Anthropic API (pre-agent)
    │ (pkg/mayorchat)  │
    │                  │
    │ POST /api/       │──→ Harness webhook → ProvisionFromWebhook
    │ world-hatched    │    → createAgent + BindAgentToDiscord (FIX NEEDED)
    └──────────────────┘
```

## Implementation Priority

### Phase 1: Fix the Discord Binding Bug (Immediate)
1. Call `BindAgentToDiscord(agentID, discordChannelID)` in `ProvisionFromWebhook` after `provisionAgent`
2. Add `OnProvision` callback to wire `discordListener.RegisterChannel()`
3. This unblocks mayors responding in Discord

### Phase 2: OpenClaw Gateway Installation (In Progress — plan exists)
1. Install OpenClaw on VPS (`scripts/vps-bootstrap.sh` Step 15e)
2. Configure Discord adapter + chatCompletions endpoint
3. Create systemd service for gateway
4. Already designed in `thoughts/CoreyCole/plans/2026-02-22_openclaw-gateway-install.md`

### Phase 3: OpenClaw HTTP Client in Harness
1. New `harness/internal/openclaw/client.go` with `StreamChat()`
2. OpenAI-compatible SSE parsing
3. Agent routing via `model: "openclaw/world-{worldID}"`
4. Session continuity via `user: "{userID}"`

### Phase 4: Mayor Widget UI
1. Persistent bottom-left FAB with chat panel
2. World selector, streaming responses, build mode toggle
3. Replaces read-only Mayor tab in overlay
4. Already designed in `thoughts/CoreyCole/plans/2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md`

### Phase 5: President Agent Activation
1. Set `PRESIDENT_SECRET` and `DISCORD_PRESIDENT_CHANNEL_ID` env vars
2. Code already implemented — just needs env vars configured
3. Validate heartbeat cycle and skill execution

### Phase 6: Onboarding → OpenClaw Migration (Future)
1. Consider "greeter" agent for onboarding chat
2. Move system prompt into OpenClaw workspace
3. Preserve WORLD_READY marker parsing

## Code References

- `harness/internal/mayor/openclaw.go:21-46` — `provisionAgent()` (missing bind call)
- `harness/internal/mayor/openclaw.go:75-127` — `BindAgentToDiscord()` (exists but unused)
- `harness/internal/president/president.go:115-120` — President bind (reference impl)
- `harness/internal/discord/listener.go:80-84` — `RegisterChannel()` (exists but never called)
- `harness/internal/discord/listener.go:109-153` — Message handler (mirror only, no forwarding)
- `pkg/mayorchat/client.go:9-14` — Direct Anthropic client
- `pkg/mayorchat/stream.go:110-138` — `BuildAnthropicMessages()` (Anthropic-specific)
- `pkg/mayorchat/prompt.go:9-95` — System prompt (would move to SOUL.md for OpenClaw)
- `harness/internal/mayor/workspace.go:11-48` — Workspace file generation
- `harness/internal/mayor/skills.go:10-79` — Mayor skills (world-build, world-status)
- `harness/internal/president/skills.go:10-88` — President skills (4 skills)
- `context/openclaw/docs/gateway/openai-http-api.md` — HTTP streaming API docs
- `context/openclaw/docs/concepts/multi-agent.md` — Agent isolation + routing

## Architecture Insights

1. **OpenClaw is per-turn, not persistent**: Agents are invoked when messages arrive, not running as daemons. This means "binding" is routing config, not process management.

2. **Dual Discord systems coexist**: The harness discordgo listener (for DB mirroring + SSE to browser) and OpenClaw's discord.js adapter (for agent responses) both connect to Discord with the same bot token but serve different purposes. This is intentional and correct.

3. **CLI-as-integration is the current pattern**: All OpenClaw management is via `exec.CommandContext` to the CLI. No Go SDK exists. The HTTP API (`/v1/chat/completions`) is the runtime integration path.

4. **Session key determinism is critical**: Setting `user: "{userID}"` in chat completions requests produces deterministic session keys (`agent:<agentId>:openai-user:<userID>`). This gives conversation continuity without managing sessions in Go.

5. **The onboarding chat is fundamentally different** from post-creation mayor chat. Onboarding is ephemeral, pre-agent, and produces structured output (WORLD_READY markers). Post-creation chat is persistent, agent-backed, and conversational. Keep them separate.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` — Final Master Plan defining the president + mayor hierarchy, skills, and Discord integration
- `thoughts/CoreyCole/plans/2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md` — Detailed 5-phase plan for OpenClaw installation + mayor widget (most current)
- `thoughts/CoreyCole/plans/2026-02-22_openclaw-gateway-install.md` — Updated installation plan using proper CLI tools
- `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` — Deep dive on OpenClaw internals
- `thoughts/CoreyCole/research/2026-02-16_09-56-21_openclaw-personality-context-for-mayor-chat.md` — SOUL.md/BOOTSTRAP.md patterns for mayor seeding
- `thoughts/CoreyCole/research/2026-02-16_11-58-54_omnipresent-mayor-assistant.md` — Always-available mayor chat concept (precursor to widget)

## Open Questions

1. **Discord adapter bot conflict**: Both discordgo (harness listener) and discord.js (OpenClaw) connect as the same bot. Do they interfere with each other's message handling? (They shouldn't — separate Gateway sessions, different intents)

2. **Onboarding-to-agent memory transfer**: When a world is created from onboarding, the conversation is pinned in Discord. Does the OpenClaw agent pick this up automatically from its Discord channel, or do we need to seed it into MEMORY.md? (Currently seeded into SOUL.md's "Origin" section)

3. **Gateway token sharing**: The harness `.env` `EnvironmentFile` is shared by both systemd services. Is the `OPENCLAW_GATEWAY_TOKEN` picked up correctly by both? (Yes — gateway reads it via env fallback for `--token`)

4. **Session key collision**: If a user chats with the same mayor via both Discord and the web widget, do they share session history? (No — Discord uses `discord:<userId>:<channelId>` keys; web uses `openai-user:<userId>` keys. Different session stores.)

5. **Rate limiting for OpenClaw**: Does OpenClaw have built-in rate limiting for the HTTP API, or do we need to implement it in the harness proxy? (OpenClaw has `rateLimit` config per agent but check if it applies to HTTP API)
