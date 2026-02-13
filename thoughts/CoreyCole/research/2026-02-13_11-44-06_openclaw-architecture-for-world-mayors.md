---
date: 2026-02-13T11:44:06-08:00
researcher: CoreyCole
git_commit: a60db1e5f86d92630ccda1f1beace9a23c76b665
branch: main
repository: creative-mode
topic: "OpenClaw Architecture Research for World Mayors Integration"
tags: [research, codebase, openclaw, mayors, discord, agent-workspace, gateway]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
---

# Research: OpenClaw Architecture for World Mayors Integration

**Date**: 2026-02-13T11:44:06-08:00
**Researcher**: CoreyCole
**Git Commit**: a60db1e5f86d92630ccda1f1beace9a23c76b665
**Branch**: main
**Repository**: creative-mode

## Research Question

Deep-dive into the OpenClaw source code (cloned to `context/openclaw/`) to validate the world mayors implementation plan, answering:
1. Does OpenClaw hot-reload agent config without restart?
2. How does OpenClaw notify external services when messages are sent/received?
3. How do Discord channel-to-agent bindings work?
4. What is the agent workspace file structure?
5. How does the gateway start and what APIs does it expose?
6. How does OpenClaw integrate with Claude Code?

## Summary

OpenClaw is a mature, well-architected Node.js gateway that validates most of the mayors plan but reveals **two critical gaps** that require plan revision:

1. **No outbound webhook** — OpenClaw cannot POST to an external URL when the mayor sends a message. The plan's `handleMayorMessageSync` endpoint has no natural caller. Requires a custom plugin or alternative architecture.
2. **Claude Code integration is subprocess-based** — OpenClaw's natural pattern is for the agent to spawn Claude Code directly via PTY, not call a REST API. This could simplify or fundamentally change the build pipeline integration.

**Positive findings**: Hot-reload works without restart. Discord bindings work exactly as planned. Multiple agents share one bot token. Workspace structure is well-documented.

## Detailed Findings

### 1. Hot-Reload: Works Without Restart

**Verdict: The plan's concern is resolved.**

Agent config changes in `openclaw.json` are classified as `kind: "none"` in the reload rules — the gateway takes no explicit action but re-reads config from disk on every request with a **200ms TTL cache** (`loadConfig()` at `src/config/io.ts:812`).

- File watcher: chokidar watches `CONFIG_PATH` (`src/gateway/config-reload.ts:351-359`)
- Debounce: 300ms stability threshold before processing changes
- Agent-related paths (`agents`, `agent`, `models`, `tools`, `routing`, `session`, `skills`) are all `"none"` at `config-reload.ts:66-87`
- New agents are visible to the gateway within ~200ms of `openclaw.json` being written
- No restart, no SIGUSR1, no manual action needed

**Implication for plan**: The harness can safely write to `openclaw.json` when provisioning a new world mayor. The running gateway will pick up the new agent on the next incoming message.

### 2. No Outbound Webhook — Critical Gap

**Verdict: The plan's Phase 5 message sync architecture is broken.**

OpenClaw has **no built-in mechanism** to POST to an external URL when an agent sends or receives a message. The webhook system is **inbound only** — external services POST to OpenClaw, not the reverse.

**What exists:**
- Inbound webhooks: `POST /hooks/wake`, `POST /hooks/agent`, `POST /hooks/<mapping>` (`src/gateway/server-http.ts:232-414`)
- Plugin hooks (in-process only): `message_received`, `message_sending`, `message_sent` (`src/plugins/types.ts:298-312`)
- Gateway WebSocket RPC: `chat.send`, `chat.history`, agent event broadcasts
- OpenAI-compatible HTTP: `POST /v1/chat/completions` (opt-in)

**What does NOT exist:**
- No `callbackUrl`, `notifyUrl`, or outbound webhook config anywhere in the schema
- Plugin hooks are in-process TypeScript handlers, not HTTP callbacks
- No built-in way to mirror messages to an external database

**Options to solve:**
1. **Custom OpenClaw plugin** — Write a TypeScript plugin that registers `message_sent` hooks and POSTs to the harness. Requires understanding the plugin API and shipping plugin code in the Docker image.
2. **Gateway WebSocket listener** — The harness connects to OpenClaw's WebSocket gateway (port 18789) and subscribes to `chat` events. Agent responses stream as `chat` broadcast events to all connected WebSocket clients.
3. **Poll gateway API** — The harness periodically calls `chat.history` via WebSocket RPC to fetch new messages. Adds latency.
4. **Discord bot listener** — Instead of getting messages from OpenClaw, the harness runs its own lightweight Discord listener (using `discordgo` in Go) that watches world channels and mirrors messages to SQLite. This avoids the OpenClaw sync problem entirely.

**Recommendation**: Option 4 (Discord bot listener in Go) is cleanest. The harness already needs to POST build events to Discord. Adding a listener for incoming messages keeps Discord as the true single bus and avoids coupling to OpenClaw's internal event system.

### 3. Discord Adapter: Validates Plan

**Verdict: The plan's Discord architecture is sound.**

The binding config matches the plan's structure exactly:

```json
{
  "bindings": [
    {
      "agentId": "world-abc123",
      "match": {
        "channel": "discord",
        "peer": { "kind": "channel", "id": "DISCORD_CHANNEL_ID" }
      }
    }
  ]
}
```

Key details:
- **Single token, multiple agents**: One Discord bot token creates one gateway connection. `resolveAgentRoute()` dispatches per-message based on bindings (`src/routing/resolve-route.ts:185-292`).
- **Priority cascade**: peer > parent thread > guild+roles > guild > team > account > channel > default
- **Thread inheritance**: Messages in threads inherit the parent channel's binding (`resolve-route.ts:238-246`)
- **Guild config**: Per-guild/per-channel allowlists via `channels.discord.guilds` (`src/config/types.discord.ts:49-62`)
- **Multi-account**: Supports multiple bot tokens via `discord.accounts` map, each with independent gateway connections

**Config structure for world mayors:**
```json
{
  "channels": {
    "discord": {
      "token": "${DISCORD_BOT_TOKEN}",
      "guilds": {
        "${DISCORD_GUILD_ID}": {
          "channels": { "*": { "allow": true } }
        }
      }
    }
  },
  "bindings": [
    { "agentId": "world-abc", "match": { "channel": "discord", "peer": { "kind": "channel", "id": "111222333" } } },
    { "agentId": "world-def", "match": { "channel": "discord", "peer": { "kind": "channel", "id": "444555666" } } }
  ]
}
```

### 4. Agent Workspace Structure

**Verdict: Plan's workspace layout needs minor adjustments.**

Expected files (`src/agents/workspace.ts:23-31`):

| File | Purpose | Injected Every Turn? |
|------|---------|---------------------|
| `AGENTS.md` | Operating instructions, workflow | Yes |
| `SOUL.md` | Persona, tone, boundaries | Yes (special treatment: "embody its persona") |
| `USER.md` | Who the user is | Yes |
| `IDENTITY.md` | Agent name, vibe, emoji | Yes |
| `TOOLS.md` | Tool guidance (does NOT control tool availability) | Yes |
| `MEMORY.md` | Curated long-term memory | Yes (main session only) |
| `HEARTBEAT.md` | Short checklist for heartbeat runs | Yes |
| `BOOTSTRAP.md` | One-time first-run ritual | Only once, then deleted |
| `memory/YYYY-MM-DD.md` | Daily memory logs | No (accessed via `memory_search` tool) |
| `skills/` | Workspace-specific skills | Metadata always; SKILL.md on demand |

**Key behaviors:**
- All files are plain Markdown, injected into the system prompt as `## <filename>` sections
- Truncated at 20,000 chars (70% head + 20% tail) via `bootstrapMaxChars`
- `MEMORY.md` is maintained by the agent (LLM), NOT by OpenClaw programmatically
- Pre-compaction memory flush: OpenClaw triggers a silent agent turn before context compaction, prompting the agent to write durable memories (`agents.defaults.compaction.memoryFlush`)
- Skills use progressive disclosure: metadata always in context, SKILL.md loaded on demand by the model
- Sub-agents only receive `AGENTS.md` and `TOOLS.md`
- Workspaces are initialized as git repos

**Plan adjustments needed:**
- Add `USER.md` and `IDENTITY.md` to provisioning
- Skills should be `SKILL.md` files with YAML frontmatter (name, description, metadata), not just plain text
- The plan's `skills/world-build/` and `skills/world-status/` need proper `SKILL.md` format with frontmatter

### 5. Gateway Architecture

**Verdict: Plan's Docker integration approach is feasible.**

- **Entry point**: `openclaw.mjs` -> `src/entry.ts` -> `src/cli/run-main.ts` -> `openclaw gateway run`
- **Default port**: 18789 (`src/config/paths.ts:216`)
- **Single-port multiplexing**: HTTP + WebSocket on same port
- **86+ WebSocket RPC methods** including `agents.create`, `agents.update`, `agents.delete`, `agents.files.*`, `chat.send`, `chat.history`, etc.
- **Daemon support**: `while(true)` restart loop with SIGUSR1 self-restart (`src/cli/gateway-cli/run-loop.ts:117`)
- **Lock file**: `acquireGatewayLock()` prevents concurrent instances
- **Docker**: `CMD ["node", "openclaw.mjs", "gateway", "--allow-unconfigured"]` (`Dockerfile:48`)
- **Bind modes**: `loopback` (default), `lan`, `tailnet`, `auto`, `custom`

**Gateway RPC for agent management:**
- `agents.create` / `agents.update` / `agents.delete` — CRUD for agents
- `agents.files.read` / `agents.files.write` — Read/write workspace files
- `config.set` / `config.patch` — Modify config programmatically

**Implication**: Instead of manually writing `openclaw.json`, the harness could use the WebSocket RPC to call `agents.create` and `config.patch`. This avoids file-level races entirely.

### 6. Claude Code Integration

**Verdict: The plan should consider direct Claude Code spawning vs. harness build API.**

OpenClaw integrates with Claude Code in three ways:

**(a) Coding Agent Skill** (`skills/coding-agent/SKILL.md`): The primary mechanism. The agent spawns Claude Code as a background PTY subprocess:
```bash
bash pty:true workdir:~/project background:true command:"claude 'Your task'"
```
- Agent monitors progress via `process action:log sessionId:XXX`
- Claude Code can notify completion via `openclaw system event --text "Done"`
- Requires `claude` binary on PATH

**(b) CLI Backend** (`docs/gateway/cli-backends.md`): Fallback runtime when API providers fail. Runs `claude -p --output-format json --dangerously-skip-permissions` as text-only.

**(c) Credential Bridge** (`src/agents/cli-credentials.ts`): Reads Claude Code's OAuth tokens from macOS Keychain or `~/.claude/.credentials.json`. Can write refreshed tokens back.

**Architecture decision**: The plan has the mayor calling `POST /api/mayor/build` which triggers the harness's existing `ForkCheckpoint -> Claude Code -> hooks -> BuildCheckpoint` pipeline. But OpenClaw's natural pattern is for the agent to spawn Claude Code directly via the coding-agent skill. Two options:

1. **Keep harness build API** (plan's approach): Mayor calls the harness API, which manages the full pipeline. Pro: reuses existing infrastructure, centralized control. Con: mayor needs a custom skill to call the REST API, doesn't leverage OpenClaw's built-in Claude Code integration.

2. **Direct Claude Code spawning**: Mayor uses the coding-agent skill to run Claude Code directly on the world's source code. Pro: uses OpenClaw's built-in capability, simpler. Con: bypasses harness checkpoint/build pipeline, needs reimplementation of fork/compile/deploy logic.

**Recommendation**: Keep the harness build API approach. The checkpoint/build/deploy pipeline is valuable infrastructure that shouldn't be duplicated. Write a custom `world-build` skill that calls the harness API rather than spawning Claude Code directly.

## Architecture Insights

### OpenClaw Config Path
Default: `~/.openclaw/openclaw.json` (or `${OPENCLAW_HOME}/openclaw.json`)
The plan sets `OPENCLAW_HOME=/data/openclaw`, so config is at `/data/openclaw/openclaw.json`.

### Gateway RPC vs File Writes for Agent Management
The plan writes `openclaw.json` directly from Go. The gateway also exposes `agents.create`, `config.patch` etc. via WebSocket RPC. Using the RPC avoids file races but adds a dependency on the gateway being up during world creation. File writes work fine given the 200ms cache — just add a file lock (Go `flock`) to prevent concurrent writes.

### Plugin System for Outbound Events
If choosing the plugin approach for message sync, plugins are registered via the plugin API:
```typescript
on("message_sent", async (event) => {
  await fetch("http://localhost:8080/api/mayor/message-sync", {
    method: "POST",
    body: JSON.stringify({ ... }),
  });
});
```
Plugin files go in `~/.openclaw/plugins/` or are registered in config under `plugins`. This requires shipping TypeScript code in the Docker image.

### Discord @mention Format
The plan uses `@%s` with mayor name string. Discord requires `<@USER_ID>` for user mentions or `<@&ROLE_ID>` for role mentions. Since the mayor IS the bot, the harness would need the bot's user ID (available from the Discord API `GET /users/@me`) or could simply use a text prefix like `[BUILD COMPLETE]` that the agent's `AGENTS.md` instructs it to watch for.

### Skill Format
Skills are directories with `SKILL.md` containing YAML frontmatter:
```markdown
---
name: world-build
description: Trigger a build to modify the world
metadata: |
  {"skillKey": "world-build", "emoji": "hammer"}
---

# World Build

[skill body with instructions]
```

## Code References

- Config hot-reload rules: `context/openclaw/src/gateway/config-reload.ts:47-87`
- Config cache (200ms TTL): `context/openclaw/src/config/io.ts:812-834`
- Plugin hook types: `context/openclaw/src/plugins/types.ts:298-312`
- Discord binding schema: `context/openclaw/src/config/zod-schema.agents.ts:14-44`
- Agent route resolution: `context/openclaw/src/routing/resolve-route.ts:185-292`
- Workspace file constants: `context/openclaw/src/agents/workspace.ts:23-31`
- Bootstrap file loading: `context/openclaw/src/agents/workspace.ts:265-319`
- System prompt injection: `context/openclaw/src/agents/system-prompt.ts:552-572`
- Memory flush config: `context/openclaw/docs/concepts/memory.md:39-77`
- Coding agent skill: `context/openclaw/skills/coding-agent/SKILL.md`
- CLI backend config: `context/openclaw/docs/gateway/cli-backends.md:186-194`
- Credential bridge: `context/openclaw/src/agents/cli-credentials.ts`
- Gateway entry: `context/openclaw/src/gateway/server.impl.ts:157`
- Gateway HTTP routes: `context/openclaw/src/gateway/server-http.ts:417-577`
- Gateway Dockerfile: `context/openclaw/Dockerfile:48`
- Inbound webhook handlers: `context/openclaw/src/gateway/server/hooks.ts:32-107`

## Open Questions

1. **Discord bot listener in Go**: If adopting Option 4 for message sync, which Go Discord library to use? `discordgo` is the standard choice. Adds a Go dependency but avoids OpenClaw plugin complexity.
2. **Agent provisioning via RPC vs file**: Should the harness call `agents.create` via WebSocket RPC or write `openclaw.json` directly? RPC is cleaner but requires the gateway to be running during world creation.
3. **Skill format for world-build**: The skill needs to call a REST API (harness build endpoint). OpenClaw skills are just instructions — the agent reads them and decides what tools to use. The skill would instruct the agent to use its `bash` tool to call `curl` against the harness API, or use a future HTTP tool.
4. **ANTHROPIC_API_KEY vs credential bridge**: OpenClaw can use Claude Code's OAuth tokens via the credential bridge, or use `ANTHROPIC_API_KEY` directly. Which approach for the Docker deployment?
