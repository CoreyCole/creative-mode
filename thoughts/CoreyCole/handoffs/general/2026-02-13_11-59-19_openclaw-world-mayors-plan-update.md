---
date: 2026-02-13T11:59:19-08:00
researcher: CoreyCole
git_commit: a60db1e5f86d92630ccda1f1beace9a23c76b665
branch: main
repository: creative-mode
topic: "OpenClaw World Mayors Plan Update"
tags: [implementation, strategy, openclaw, mayors, discord, agent-workspace]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Update OpenClaw World Mayors Implementation Plan

## Task(s)

- **OpenClaw source code research** — COMPLETED. Deep-dived into the OpenClaw codebase (`context/openclaw/`) to validate the world mayors plan and answer critical open questions about hot-reload, webhooks, Discord bindings, workspace structure, gateway architecture, and Claude Code integration.
- **Update the mayors implementation plan** — PLANNED. The original plan at `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` needs revision based on research findings. Two critical architectural gaps were identified.

## Critical References

1. **Original plan**: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — The 5-phase implementation plan that needs updating.
2. **Research findings**: `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` — Comprehensive OpenClaw architecture analysis with code references.
3. **OpenClaw source**: `context/openclaw/` — Cloned source code used for research.

## Recent changes

No codebase changes were made. This session was research-only. Two documents were created:
- `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` — research document

## Learnings

### Critical Gap 1: No Outbound Webhook in OpenClaw
OpenClaw has NO built-in mechanism to POST to an external URL when the mayor sends a message. Plugin hooks (`message_sent` at `context/openclaw/src/plugins/types.ts:298-312`) are in-process TypeScript only, not HTTP callbacks. The plan's Phase 5 `handleMayorMessageSync` endpoint has no natural caller.

**Recommended fix**: Run a lightweight Discord listener in Go (via `discordgo`) within the harness. The harness already posts build events to Discord — adding a listener mirrors incoming messages (both user and mayor) to SQLite, keeping Discord as the true single bus without coupling to OpenClaw internals.

### Critical Gap 2: Claude Code Integration Model
OpenClaw's natural pattern is subprocess-based — the agent spawns Claude Code via PTY (`bash pty:true background:true command:"claude '...'"` via the coding-agent skill at `context/openclaw/skills/coding-agent/SKILL.md`). However, the plan's approach of calling `POST /api/mayor/build` to trigger the existing harness checkpoint/build pipeline is still correct — it reuses fork/compile/deploy infrastructure.

### Positive Findings
- **Hot-reload works**: Agent config changes in `openclaw.json` are picked up within ~200ms via read-through cache (`context/openclaw/src/config/io.ts:812-834`). No restart needed.
- **Discord bindings work as planned**: `peer.kind: "channel"` + `peer.id` routes messages to specific agents (`context/openclaw/src/routing/resolve-route.ts:185-292`). Multiple agents share one bot token.
- **Gateway is a proper daemon**: Port 18789, HTTP+WS multiplexed, restart loop, lock file, Docker support (`context/openclaw/src/gateway/server.impl.ts:157`).
- **86+ WebSocket RPC methods**: Including `agents.create`, `agents.update`, `config.patch` — could use these instead of direct file writes to avoid races on `openclaw.json`.

### Workspace Structure Adjustments
- Plan should add `USER.md` and `IDENTITY.md` to provisioning (constants at `context/openclaw/src/agents/workspace.ts:23-31`)
- Skills need `SKILL.md` files with YAML frontmatter (name, description), not plain text
- Discord @mentions require `<@USER_ID>` format, not `@MayorName` — alternative: use text prefix like `[BUILD COMPLETE]` and instruct agent via `AGENTS.md`

## Artifacts

- `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` — Full research document with code references
- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — Original plan (to be updated)

## Action Items & Next Steps

1. **Update the plan** (`thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`) with the following revisions:
   - **Phase 1**: No changes needed (hot-reload confirmed working, Docker/OpenClaw setup is sound)
   - **Phase 3**: Update workspace provisioning to include `USER.md`, `IDENTITY.md`; use proper `SKILL.md` format with YAML frontmatter for skills; consider using gateway RPC (`agents.create`) instead of direct file writes; fix Discord @mention format
   - **Phase 4**: Replace `@%s` Discord mentions with text prefix approach (e.g., `[BUILD EVENT]`); add `discordgo` listener to harness for message mirroring (replaces the broken outbound webhook assumption)
   - **Phase 5**: Remove `handleMayorMessageSync` endpoint (no caller exists); replace with Go-based Discord listener that mirrors all channel messages to SQLite; the harness Discord bot both posts events AND listens for messages/responses
   - **Add new architectural component**: `harness/internal/discord/` package — a `discordgo`-based listener that watches world Discord channels and mirrors messages to SQLite + pushes SSE updates to browsers. This replaces the OpenClaw-to-harness sync that doesn't exist.

2. **Decide on agent management approach**: File writes with `flock` vs. gateway WebSocket RPC (`agents.create`, `config.patch`). File writes are simpler and don't depend on gateway being up. RPC is cleaner but adds a startup ordering dependency.

3. **Decide on `ANTHROPIC_API_KEY` vs credential bridge**: OpenClaw can use the env var directly or read Claude Code's OAuth tokens from keychain (`context/openclaw/src/agents/cli-credentials.ts`). Env var is simpler for Docker.

## Other Notes

- OpenClaw config path with `OPENCLAW_HOME=/data/openclaw` resolves to `/data/openclaw/openclaw.json`
- Gateway default port 18789, configurable via `OPENCLAW_GATEWAY_PORT` env var
- Config file supports JSON5 format with `$include` directives and `${ENV}` substitution (`context/openclaw/src/config/io.ts:417`)
- Gateway bind mode defaults to `loopback` (127.0.0.1) — for Docker, use `--bind lan` or set in config
- The `openclaw gateway` command is the daemon entry point; `openclaw gateway install` creates platform-native service definitions (launchd/systemd)
- Plugin hooks fire at: `message_received`, `message_sending` (can modify/cancel), `message_sent`, `before_agent_start`, `agent_end`, `before_tool_call`, `after_tool_call`, `session_start`, `session_end`, `gateway_start`, `gateway_stop`
- Discord channel limit: 500 per guild by default — worth noting for scale planning
