---
date: 2026-02-13T15:25:28-08:00
researcher: CoreyCole
git_commit: f551b0e36abd3a7a05efae28cb52559fcd019a4d
branch: main
repository: creative-mode
topic: "Mayor Dashboard — Debugging, Memory Editing, and Task Visibility"
tags: [implementation, strategy, openclaw, mayor, dashboard, debugging, memory-inspector]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Mayor Dashboard — Debugging, Memory Editing, and Task Visibility

## Task(s)

### Completed
1. **Deep research on `context/openclaw/` source code** — Verified all assumptions in the OpenClaw World Mayors plan against actual source. Found critical findings about CLI behavior, binding format, config hot-reload, HTTP hooks API, session management APIs, and workspace file restrictions.
2. **Plan update with research findings** — Integrated all OpenClaw research into the implementation plan. Fixed 13 issues (SQLite migration bug, missing mayor_secret generation, stale GitHubUsername references, missing dev login update, missing middleware redirects, missing view template updates, missing sqlc renames, missing invite UI, missing CreateWorld parameter).
3. **Final end-to-end plan review** — Completed full review pass. Added Phase 0 recommendation for Discord OAuth standalone migration. Added future capabilities section with HTTP hooks, session management, and ACP.

### Planned (Next Session)
4. **Design a Mayor Dashboard** — Create a plan for a comprehensive mayor debugging/editing dashboard that includes:
   - Memory inspector (browse + edit workspace markdown files the mayor loads into context)
   - Task tracking (what is the mayor currently working on, which Claude Code sessions has it delegated to)
   - Structured SQLite visibility (mayor activity logs, build delegation history, conversation stats)
   - Direct markdown file reading (SOUL.md, MEMORY.md, AGENTS.md, etc. from workspace)
   - Integration with OpenClaw session management APIs for conversation inspection

## Critical References

- **OpenClaw World Mayors plan** (just updated): `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — the ~2000-line implementation plan across 5 phases
- **Mayor Prompt Attenuation + Memory Inspector plan**: `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — has Phase 2 "Memory Inspector" with workspace file browse/edit UI. This is the starting point for the dashboard.
- **OpenClaw reference code**: `context/openclaw/` — gitignored source code, key files documented below

## Recent changes

All changes were to the plan document (no code changes):
- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — extensive updates across all sections incorporating OpenClaw research findings and fixing 13 issues

## Learnings

### OpenClaw Internals (Verified Against Source)

**Agent Workspace Files** — Each agent gets isolated bootstrap files created by `ensureAgentWorkspace()` at `context/openclaw/src/agents/workspace.ts:153-226`:
- Created on `agents add`: AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, HEARTBEAT.md, BOOTSTRAP.md
- **MEMORY.md is NOT auto-created** — OpenClaw creates it on first agent memory write during a conversation
- Files written with `wx` flag (skip if exists), so our templates overwrite them after CLI creates them
- Gateway file APIs (`agents.files.list/get/set` at `context/openclaw/src/gateway/server-methods/agents.ts:368-506`) restrict access to bootstrap set + MEMORY.md only — custom files (skills) must be written directly to disk

**Config and Binding Behavior**:
- `openclaw config set` does a **FULL REPLACE** at the given path (`setAtPath()` at `context/openclaw/src/cli/config-cli.ts:153` does direct assignment). Must use read-modify-write for bindings.
- `openclaw config get bindings --json` exists and returns the full array
- `agents add --bind` flag does incremental APPEND via `applyAgentBindings()` at `context/openclaw/src/commands/agents.bindings.ts:39-90`, but only supports `channel:accountId` format — not per-channel `peer` matching needed for same-guild routing
- Per-channel routing uses `peer` match: `{"agentId": "world-abc", "match": {"channel": "discord", "peer": {"kind": "channel", "id": "CHANNEL_ID"}}}`
- Route resolution priority at `context/openclaw/src/routing/resolve-route.ts:230-291`: peer > parent-peer > guild+roles > guild > team > account > channel > default

**Gateway Hot-Reload** — Config changes picked up without restart:
- Chokidar file watcher at `context/openclaw/src/gateway/config-reload.ts:351`
- Agent/binding changes classified as `"none"` in reload plan (lines 72-77) — no restart needed
- `writeConfigFile()` calls `clearConfigCache()` so next incoming message reads fresh config via `loadConfig()` at `context/openclaw/src/config/io.ts:812-834`

**HTTP Hooks API** — `POST /hooks/agent` at `context/openclaw/src/gateway/server-http.ts:314-340`:
- Bearer token auth via `Authorization: Bearer <token>` or `X-OpenClaw-Token`
- JSON body: `{message, name, agentId, sessionKey, deliver, channel, to, model, thinking, timeoutSeconds}`
- Returns `202 Accepted` with `{ok: true, runId: "..."}`
- Fire-and-forget — no response body with agent output

**Session Management APIs** — Gateway WebSocket methods at `context/openclaw/src/gateway/server-methods/sessions.ts`:
- `sessions.list` (lines 72-94): Lists sessions with filters (limit, activeMinutes, agentId, search, label)
- `sessions.preview` (lines 95-166): Returns conversation preview items
- `sessions.patch` (lines 189-239): Updates session properties (label, model, thinking level)
- `sessions.reset` (lines 240-293): Resets session
- `sessions.delete` (lines 294-380): Deletes session, aborts active runs, archives transcripts
- `sessions.compact` — triggers context compaction
- Also: `agents.list` (lines 168-183) returns all agents with workspace info/files
- CLI: `openclaw status --json --all --deep` for gateway health/agent status

**ACP (Agent Client Protocol)** — `context/openclaw/src/acp/client.ts` and `context/openclaw/src/acp/translator.ts`:
- Stdio-based agent-to-agent communication via `@agentclientprotocol/sdk`
- Could enable future mayor-to-mayor or supervisor patterns

### Current Auth System (Being Replaced in Mayor Plan Phase 2)
- `github_id INTEGER UNIQUE NOT NULL` in users table prevents Discord-only users — fixed in plan with table recreation migration
- Dev login (`HandleDevLogin` at `harness/internal/auth/auth.go:423`) generates fake negative `github_id` from FNV-32a hash — needs update to generate fake `discord_id` strings
- All view templates reference `user.GitHubUsername` — comprehensive list of touchpoints documented in the updated plan

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — the full implementation plan (5 phases + Phase 0, ~2100 lines), updated with all research findings and 13 bug fixes
- `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — prompt attenuation + memory inspector plan (Phase 2 has the memory inspector UI design that should be incorporated into the dashboard)
- `thoughts/CoreyCole/handoffs/general/2026-02-13_14-13-15_openclaw-world-mayors-plan-review.md` — the handoff that started this session

## Action Items & Next Steps

1. **Design the Mayor Dashboard** — Create a comprehensive plan for a dashboard that provides visibility into mayor internals. Incorporate the following:

   **From the Memory Inspector plan** (`thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md`, Phase 2):
   - "Mayor" tab in chat panel with file list + textarea editor
   - Browse/edit SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md
   - Read-only view of skill files (SKILL.md)
   - Workspace file allowlist + path traversal security
   - Signal binding for editor content (`mayor_file_content`)
   - Relevant code: `harness/views/mayor/mayor.templ`, `harness/internal/server/mayor.go`

   **New: Task/Session Tracking**:
   - What task is the mayor currently working on? (OpenClaw sessions have labels, models, thinking levels)
   - Which Claude Code sessions has the mayor delegated to? (The `world-build` skill triggers builds via `POST /api/mayor/build` which creates tmux sessions)
   - Build delegation history: which prompts led to which builds, success/fail rates
   - Conversation stats: message counts, response times, memory growth over time

   **New: SQLite Structured Visibility**:
   - `mayor_activity` table — log mayor actions (message received, build triggered, build completed, memory updated)
   - `mayor_sessions` table — track OpenClaw sessions per agent (session key, created, last active, token count)
   - `mayor_builds` table — link mayor conversation → build delegation (prompt, checkpoint_id, status, duration)
   - Use existing `mayor_messages` table (from main plan) for conversation history

   **New: Direct Markdown File Reading**:
   - Read MEMORY.md to show what the mayor "knows" about the world
   - Diff view: show what changed in MEMORY.md after each conversation (OpenClaw auto-updates it)
   - Read SOUL.md to show/edit the mayor's personality
   - Read session transcripts (if OpenClaw stores them in the workspace)

   **New: OpenClaw API Integration**:
   - `sessions.list` — show active/recent sessions per agent
   - `sessions.preview` — show conversation history without reading transcript files
   - `agents.list` — show agent status, workspace info
   - `openclaw status --json` — gateway health, connected channels
   - Consider WebSocket client in Go for gateway method calls, or CLI wrappers

2. **Consider how the dashboard fits with existing plans** — The memory inspector is already designed as Phase 2 of the prompt attenuation plan. The dashboard could either:
   - Expand Phase 2 of the attenuation plan into a full dashboard
   - Be a separate Phase 6 in the main mayor plan (after Phase 5: UI + chat)
   - Be its own standalone plan document

3. **Consider observability hooks** — Where in the existing pipeline can we capture mayor activity data?
   - The `discordgo` listener (Phase 5) already mirrors all messages — good for conversation tracking
   - The `handleMayorBuild` endpoint (Phase 4) is the build delegation point — can log to `mayor_builds`
   - OpenClaw's `POST /hooks/agent` returns a `runId` — can we track runs?
   - Build completion events (Phase 4) already post to Discord — can log duration/status

## Other Notes

### Key OpenClaw Source Locations for Dashboard Design
- Gateway WebSocket methods: `context/openclaw/src/gateway/server-methods-list.ts:3-93` — full list of all available methods
- Session management: `context/openclaw/src/gateway/server-methods/sessions.ts` — list, preview, patch, reset, delete, compact
- Agent CRUD + file APIs: `context/openclaw/src/gateway/server-methods/agents.ts` — create, update, delete, files.list/get/set
- HTTP hooks: `context/openclaw/src/gateway/server-http.ts:190-415` — POST /hooks/agent, hook mappings
- Config methods: `context/openclaw/src/gateway/server-methods/config.ts` — get, set, patch, apply
- Agent workspace resolution: `context/openclaw/src/agents/agent-scope.ts:166-182` — resolveAgentWorkspaceDir
- Session transcript storage: `context/openclaw/src/commands/onboard-helpers.ts:297-299` — session transcripts dir

### Harness Key Locations for Dashboard Integration
- World overlay (where Mayor tab lives): `harness/views/world/overlay.templ`
- Chat tab system: `harness/views/chat/chat.templ:11-24` — Global, World, Lineage, Assets tabs
- SSE event bus: `harness/internal/server/events.go` — `PublishWorld()` for real-time updates
- Build pipeline entry: `harness/internal/claude/claude.go` — `ForkCheckpoint` / `BuildCheckpoint`
- tmux session tracking: `harness/internal/tmux/session.go` — Claude Code sessions
- Existing file serving security pattern: `harness/internal/server/server.go:523-556` — `filepath.Clean` + `strings.HasPrefix`

### Discord Bot Token Setup Needed
Before any mayor infrastructure works, two Discord bots and one OAuth app need to be created in the Discord Developer Portal. See the Dependencies table in the main plan for full details.
