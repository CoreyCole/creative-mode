# OpenClaw World Mayors — Implementation Plan

## Overview

Replace the direct "inner Claude" pipeline with **world mayors** — persistent OpenClaw AI agents, one per world, that orchestrate all world modifications through structured conversation. Users interact with their mayor from **Discord** (the primary interface — accessible from any phone, no Tailnet required) or from the **harness UI** (which mirrors the Discord conversation). Each mayor has a unique personality crafted by the world creator, its own evolving memory of the world's history, and follows a structured research → plan → clarify → implement workflow using Claude Code under the hood.

## Why OpenClaw

OpenClaw is not just a thin wrapper around the Anthropic API. It provides capabilities that would be expensive and complex to reimplement:

- **Autonomous context management** — OpenClaw automatically manages its own context window, compacting old conversations, summarizing history, and maintaining a working `MEMORY.md` that it self-updates after every meaningful interaction. The mayor genuinely *learns* over time — it remembers what was built, what failed, what the user likes, and what the world looks like. This is the core value: a mayor that knows its world deeply, like a real mayor would.
- **Self-updating knowledge** — `MEMORY.md`, `TOOLS.md`, and `SOUL.md` are living documents that OpenClaw maintains autonomously. After a series of builds, the mayor's memory reflects the full history of decisions, user preferences, and world evolution. You never need to manually update these — OpenClaw's context engine handles it.
- **Phone-accessible chat** — via Discord, users can message their mayor from anywhere without connecting to the private Tailnet or opening the harness. This is the primary interface for casual interaction ("hey, can you add a fountain in the town square?").
- **Multi-channel routing** — one agent, many interfaces. The same mayor handles messages from Discord, the harness UI, and (future) WhatsApp/Telegram/iMessage — with shared context across all of them.
- **Structured agent framework** — `AGENTS.md` defines workflow, `SOUL.md` defines personality, skills define capabilities. These are first-class OpenClaw concepts with runtime support, not just strings concatenated into a system prompt.
- **Session and conversation management** — OpenClaw handles conversation threading, session persistence, and graceful recovery from crashes or restarts.

The alternative — calling the Anthropic Messages API directly from Go — would require reimplementing context compaction, memory management, conversation threading, session persistence, and multi-channel routing. OpenClaw provides all of this out of the box.

## Current State Analysis

### What Exists
- **Inner Claude pipeline** (`harness/internal/claude/claude.go`): User prompt → `ForkCheckpoint` → write `MEMORY.md` → tmux Claude Code session → hook scripts POST events → `BuildCheckpoint` → compile + deploy
- **tmux session management** (`harness/internal/tmux/session.go`): Named sessions (`cm-{worldID}-{cpID}`) with env vars for hook script communication
- **Hook-driven pipeline** (`templates/3d/.claude/hooks/`): `on-tool-use.sh`, `on-stop.sh`, `on-notification.sh` POST JSONL events to `/api/claude-event`
- **Event-driven SSE** (`harness/internal/server/events.go`): Real-time browser updates via Datastar signal patches
- **World creation** (`harness/internal/world/manager.go`): Template copy → DB transaction → background build → game server start
- **Simple form** (`harness/views/lobby/lobby.templ:50-66`): name, description, template_type — native HTML form, no Datastar signals

### Key Constraints
- Docker image (`harness/Dockerfile`) is Go+Rust, **no Node.js runtime** — must add for OpenClaw
- 2D template has **no `.claude/` hooks** — needs hooks before mayors can orchestrate 2D builds
- Claude Code runs inside Docker via native installer (no Node.js dependency)
- `ANTHROPIC_API_KEY` already passed through to container
- OpenClaw requires Node.js 22+ and pnpm

### Key Discoveries
- OpenClaw's multi-agent routing (`~/.openclaw/openclaw.json` `agents.list[]` + `bindings[]`) maps perfectly to one-agent-per-world
- Each agent gets isolated workspace (`AGENTS.md`, `SOUL.md`, `IDENTITY.md`, `USER.md`, `MEMORY.md`, skills/, sessions/)
- OpenClaw's Discord adapter (`discord.js`) supports per-channel → per-agent routing via bindings
- OpenClaw CLI has `agents add --non-interactive --json` for programmatic agent management
- Config hot-reload (200ms cache) picks up agent changes without gateway restart
- Nano Banana (Google Gemini image gen) available via `@google/genai` npm — future mayor capability

### Architectural Decisions (resolved via research)

1. **Agent management: OpenClaw CLI** (not file writes, not WebSocket RPC)
   - `openclaw agents add --non-interactive --json` + `openclaw config set` from Go via `exec.Command`
   - CLI handles config normalization, workspace scaffolding, duplicate detection, file locking
   - No gateway dependency (operates directly on config files)
   - No risk of schema drift — OpenClaw owns its own config format
   - Rejected alternatives: direct file writes (fragile, reimplements OpenClaw logic), WebSocket RPC (requires gateway running, complex auth handshake)

2. **Message sync: `discordgo` listener in harness** (not OpenClaw webhook)
   - OpenClaw has NO outbound webhook mechanism — plugin hooks are in-process TypeScript only
   - Harness runs `discordgo` Gateway listener to mirror all world channel messages to SQLite
   - Single persistent WebSocket connection, filtered to guild message events
   - Rejected alternatives: OpenClaw plugin (requires shipping TypeScript, coupling to internals), WebSocket RPC subscription (complex, adds OpenClaw dependency for reads), polling (adds latency)

3. **Build event format: text prefixes** (not Discord @mentions)
   - `[BUILD COMPLETE]` / `[BUILD FAILED]` instead of `@MayorName`
   - Discord @mentions require `<@USER_ID>` format — fragile and requires looking up bot user ID
   - Mayor's `AGENTS.md` instructs it to watch for these prefixes
   - Simpler, more observable, works even if bot user ID changes

4. **API key: `ANTHROPIC_API_KEY` env var** (not credential bridge)
   - OpenClaw can read Claude Code's OAuth tokens from keychain via credential bridge
   - Env var is simpler for Docker deployment, already configured and passed through
   - Credential bridge adds macOS Keychain dependency that doesn't exist in the container

## Desired End State

After this plan is complete:

1. **Every world has a mayor** — auto-provisioned on world creation with user-defined personality
2. **Discord is the primary interface** — each world gets a Discord channel; users message the mayor from their phone
3. **Harness UI mirrors Discord** — messages are stored in SQLite, rendered with Datastar/templ, no Discord account required to view
4. **Discord is the communication bus** — the harness posts build events to Discord with `[BUILD]` prefixes; OpenClaw picks them up naturally (no custom webhook loop)
5. **Mayor learns over time** — OpenClaw's autonomous context management means the mayor accumulates knowledge about its world, remembers user preferences, and references past builds — like a real mayor who knows every street and building in their town
6. **Existing pipeline intact** — the mayor composes detailed prompts and calls the harness build API; `ForkCheckpoint` → Claude Code → hooks → `BuildCheckpoint` still works as-is

### Verification
- Create a world from the lobby → mayor is auto-provisioned with personality, Discord channel created
- Send a message from Discord → mayor responds conversationally, asks clarifying questions
- Mayor triggers a build → existing checkpoint/build pipeline executes, build status appears in Discord
- Open the harness UI → see the full Discord conversation mirrored in the world overlay
- Send a message from the harness UI → it posts to Discord, mayor responds, response mirrors back
- After several interactions, mayor references past builds and user preferences from memory

## What We're NOT Doing

- **Replacing Claude Code** — mayors orchestrate Claude Code, they don't replace it
- **Multiple messaging platforms in MVP** — Discord first, others (WhatsApp, Telegram, iMessage) come later via OpenClaw adapters
- **Nano Banana image generation** — noted as future mayor capability, not in this plan
- **Per-user mayors** — one mayor per world, not per user. All users in a world talk to the same mayor
- **OpenClaw as separate container** — runs in the same Docker container alongside the Go harness
- **Voice interaction** — text-only for now
- **Mayor-to-mayor communication** — worlds are independent; mayors don't coordinate
- **Embedding Discord widgets** — we mirror messages to SQLite and render natively with Datastar/templ

## Architecture

### Communication Flow

Discord is the single communication bus. All messages — from users, the mayor, and the harness — flow through the world's Discord channel. The harness mirrors everything to SQLite for the browser UI.

```
Discord Channel (source of truth)
   ^         |
   |         | OpenClaw listens via Discord adapter
   |         v
   |    OpenClaw Mayor Agent
   |    (SOUL.md = personality — self-updating)
   |    (MEMORY.md = world knowledge — self-updating)
   |    (AGENTS.md = structured workflow)
   |    (skills/ = world-build, world-status)
   |         |
   |         | (when ready to build)
   |         v
   |    Harness API: POST /api/mayor/build
   |         |
   |         v
   |    Existing Pipeline: ForkCheckpoint → Claude Code → hooks → BuildCheckpoint
   |         |
   |         v (build complete/failed)
   |    Harness posts to Discord: "[BUILD COMPLETE] checkpoint abc123 — summary"
   |         |
   |         v OpenClaw picks up the message (AGENTS.md instructs mayor to watch for [BUILD] prefix)
   |    Mayor summarizes results in Discord thread
   |
   +--- discordgo listener in harness mirrors ALL Discord messages → SQLite → Datastar/templ UI
```

### No Webhook Loop

The key insight: instead of a custom webhook bridge between the harness and OpenClaw, the harness simply **posts build events to Discord** with a `[BUILD COMPLETE]` or `[BUILD FAILED]` prefix. The mayor's `AGENTS.md` instructs it to watch for these prefixes. OpenClaw is already listening on the Discord channel, so it picks up the message naturally. (Discord @mentions require `<@USER_ID>` format which is fragile — text prefixes are simpler and just as effective since the mayor is the only agent bound to the channel.) This means:

- No `/api/mayor/event` webhook endpoint
- No `mayor/notifier.go` package
- No OpenClaw internal API for injecting messages
- Discord thread context is preserved — the mayor sees build results in the same conversation where the user made the request
- Everything is observable — read the Discord channel to see the full history

### Message Mirroring via `discordgo` Listener

The harness runs a `discordgo` listener (Go Discord library) that watches all world Discord channels. Every message — from users, the mayor, and the harness itself — is mirrored to SQLite and pushed to browsers via SSE.

```
Discord channel (source of truth)
      |
      | discordgo listener in harness (Go, same process)
      | watches all world channels via Discord Gateway WebSocket
      v
SQLite mayor_messages table (deduplicated by discord_message_id)
      |
      | (SSE push on insert)
      v
Datastar/templ chat component in world overlay
```

Users in the browser see the same conversation without needing a Discord account. Users on their phone use Discord directly.

This replaces the original plan's `handleMayorMessageSync` webhook endpoint, which had no caller — OpenClaw has no built-in outbound webhook mechanism. The `discordgo` listener solves this cleanly: Discord is the single bus, and the harness simply listens to it.

---

## Phase 1: OpenClaw + Discord in Docker

### Overview
Add Node.js runtime, OpenClaw, and Discord bot configuration to the Docker image. Get a single OpenClaw gateway running with the Discord adapter connected. Also fix the 2D template hooks (independent infrastructure prerequisite).

### Changes Required

#### 1. Dockerfile — Add Node.js + pnpm + OpenClaw
**File**: `harness/Dockerfile`
**Changes**: Add Node.js 22 LTS, pnpm, and OpenClaw installation layers

```dockerfile
# After Rust tools, before Go tools:

# Node.js 22 LTS (required for OpenClaw)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g pnpm@latest

# OpenClaw (AI agent framework)
# NOTE: Skip pnpm ui:build — the gateway doesn't serve the OpenClaw UI and
# the UI build pulls in heavy frontend deps. This saves ~200MB in the image.
RUN git clone --depth 1 https://github.com/openclaw/openclaw.git /opt/openclaw && \
    cd /opt/openclaw && \
    pnpm install --frozen-lockfile && \
    pnpm build

ENV OPENCLAW_HOME=/data/openclaw

# IMPORTANT: Verify the actual CLI binary path after build.
# It may be at /opt/openclaw/packages/cli/bin/openclaw or similar,
# not necessarily in node_modules/.bin. Check with:
#   find /opt/openclaw -name "openclaw" -type f -executable
# The openclawBin field in mayor.Manager must match this path.
ENV PATH="/opt/openclaw/node_modules/.bin:$PATH"
```

**Docker image size note**: Adding Node.js 22 + pnpm + OpenClaw will add ~300-500MB to the image. Consider a multi-stage build in future to discard dev dependencies. Skipping `pnpm ui:build` avoids the heaviest optional dependency.

#### 2. Entrypoint — Start OpenClaw gateway alongside Air
**File**: `harness/scripts/dev-entrypoint.sh`
**Changes**: Start OpenClaw gateway in the background before `exec air`

```bash
# After tmux start-server:

# Start OpenClaw gateway if configured
if [ -f "$OPENCLAW_HOME/openclaw.json" ]; then
    echo "  Starting OpenClaw gateway..."
    cd /opt/openclaw && node src/gateway/server.js &
    OPENCLAW_PID=$!
    echo "  OpenClaw gateway PID: $OPENCLAW_PID (port 18789)"
fi
```

#### 3. Docker Compose — Expose OpenClaw port, add env vars
**File**: `harness/docker-compose.yml`
**Changes**: Add port 18789 for OpenClaw gateway, pass Discord token

```yaml
ports:
  - "8080:8080"
  - "8081-8180:8081-8180"
  - "9001-9100:9001-9100"
  - "18789:18789"  # OpenClaw gateway
environment:
  - CGO_ENABLED=1
  - DEV_MODE=true
  - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
  - DISCORD_BOT_TOKEN=${DISCORD_BOT_TOKEN}
  - DISCORD_GUILD_ID=${DISCORD_GUILD_ID}
```

#### 4. Base OpenClaw config with Discord channel
**File**: `harness/scripts/setup-openclaw.sh` (new)
**Changes**: Script to initialize OpenClaw home directory with Discord adapter

```bash
#!/bin/bash
# Initialize OpenClaw directory structure inside data/
OPENCLAW_HOME="${1:-/data/openclaw}"

mkdir -p "$OPENCLAW_HOME/agents"
mkdir -p "$OPENCLAW_HOME/workspaces"

# Base config — agents are added dynamically by the harness
# Discord channel is configured if DISCORD_BOT_TOKEN is set
cat > "$OPENCLAW_HOME/openclaw.json" << EOF
{
  "agent": {
    "model": "anthropic/claude-sonnet-4-5-20250929"
  },
  "agents": {
    "list": []
  },
  "bindings": [],
  "channels": {
    $(if [ -n "$DISCORD_BOT_TOKEN" ]; then
      echo '"discord": {'
      echo '  "token": "'$DISCORD_BOT_TOKEN'",'
      echo '  "guildId": "'$DISCORD_GUILD_ID'"'
      echo '}'
    fi)
  }
}
EOF

echo "OpenClaw initialized at $OPENCLAW_HOME"
```

#### 5. Fix 2D template hooks (critical infrastructure gap)
**File**: `templates/2d/.claude/settings.json` (new)
**File**: `templates/2d/.claude/hooks/on-stop.sh` (new)
**File**: `templates/2d/.claude/hooks/on-tool-use.sh` (new)
**File**: `templates/2d/.claude/hooks/on-notification.sh` (new)

Copy the 3D hooks to the 2D template so the build pipeline works for 2D worlds too. This is a prerequisite for mayors to orchestrate 2D builds. Move this to Phase 1 since it's independent infrastructure.

### Success Criteria

#### Automated Verification:
- [ ] Docker image builds: `cd /Users/coreycole/cdev/creative-mode/harness && docker compose build`
- [ ] Container starts with OpenClaw gateway: `docker compose up` shows "Starting OpenClaw gateway..."
- [ ] OpenClaw gateway responds: `curl http://localhost:18789/health` (inside container)
- [ ] Existing harness still works: `curl http://localhost:8080` returns lobby page
- [ ] Go build unaffected: `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./...`
- [ ] 2D template has `.claude/` directory with all hook scripts

#### Manual Verification:
- [ ] `just live` starts both harness and OpenClaw gateway
- [ ] World creation still works (no regression)
- [ ] OpenClaw logs show Discord adapter connected (if token configured)
- [ ] `docker compose exec harness node --version` shows v22+

---

## Phase 2: Mayor Personality + DB Schema

### Overview
Add mayor personality fields to the world creation form and database. Add the `mayor_messages` table for mirroring Discord conversations. When a user creates a world, they name their mayor and describe its personality.

### Changes Required

#### 1. Database Migration
**File**: `harness/internal/db/migrations/004_mayor.sql` (new)

```sql
-- Mayor identity
ALTER TABLE worlds ADD COLUMN mayor_name TEXT NOT NULL DEFAULT 'Mayor';
ALTER TABLE worlds ADD COLUMN mayor_personality TEXT;
ALTER TABLE worlds ADD COLUMN discord_channel_id TEXT;

-- Discord message mirror for browser UI
CREATE TABLE mayor_messages (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    discord_message_id TEXT UNIQUE,
    discord_thread_id TEXT,
    author_type TEXT NOT NULL,  -- 'user', 'mayor', 'system'
    author_name TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_mayor_messages_world ON mayor_messages(world_id, created_at);
```

#### 2. Register migration
**File**: `harness/internal/db/db.go`
**Changes**: Add `004_mayor.sql` to the `migrationFiles` slice and add bootstrap detection

In the `migrationFiles` slice (around line 93-97), add:
```go
"004_mayor.sql",
```

In `bootstrapExistingMigrations()`, add detection for the mayor column:
```go
// Check for migration 004 (mayor columns)
var hasMayorName bool
row = tx.QueryRow("SELECT COUNT(*) > 0 FROM pragma_table_info('worlds') WHERE name = 'mayor_name'")
_ = row.Scan(&hasMayorName)
if hasMayorName {
    _, _ = tx.Exec("INSERT OR IGNORE INTO _migrations (filename) VALUES ('004_mayor.sql')")
}
```

#### 3. SQL Queries — Update CreateWorld + add message queries
**File**: `harness/internal/db/queries/worlds.sql`
**Changes**: Add mayor fields to CreateWorld query

```sql
-- name: CreateWorld :exec
INSERT INTO worlds (id, name, description, created_by, template_type, mayor_name, mayor_personality)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateWorldDiscordChannel :exec
UPDATE worlds SET discord_channel_id = ? WHERE id = ?;
```

**File**: `harness/internal/db/queries/mayor_messages.sql` (new)
```sql
-- name: InsertMayorMessage :exec
INSERT OR IGNORE INTO mayor_messages (id, world_id, discord_message_id, discord_thread_id, author_type, author_name, content, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMayorMessages :many
SELECT * FROM mayor_messages WHERE world_id = ? ORDER BY created_at ASC;

-- name: GetMayorMessagesSince :many
SELECT * FROM mayor_messages WHERE world_id = ? AND created_at > ? ORDER BY created_at ASC;

-- name: GetMayorMessageByDiscordID :one
SELECT * FROM mayor_messages WHERE discord_message_id = ? LIMIT 1;
```

#### 4. Regenerate sqlc
Run `just generate` to update:
- `harness/internal/db/sqlc/models.go` — `World` struct gets `MayorName`, `MayorPersonality`, `DiscordChannelID`; new `MayorMessage` struct
- `harness/internal/db/sqlc/worlds.sql.go` — `CreateWorldParams` gets new fields
- `harness/internal/db/sqlc/mayor_messages.sql.go` — new query functions

#### 5. World Creation Form — Add mayor fields
**File**: `harness/views/lobby/lobby.templ`
**Changes**: Add mayor name and personality inputs to the create world form

Replace the existing form (lines 50-66) with an expanded version:

```go
<form class="space-y-3">
    <div class="flex gap-2 items-center">
        @input.Input(input.InputArgs{Name: "name", Placeholder: "World name", Required: true})
        <select name="template_type" class="h-9 rounded-md border border-input bg-background px-3 text-sm">
            <option value="3d">3D World</option>
            <option value="2d">2D Room World</option>
        </select>
    </div>
    @input.Input(input.InputArgs{Name: "description", Placeholder: "World description (optional)"})
    <div class="border border-dashed border-muted-foreground/30 rounded-md p-3 space-y-2">
        <p class="text-xs text-muted-foreground font-medium">World Mayor</p>
        @input.Input(input.InputArgs{Name: "mayor_name", Placeholder: "Mayor's name (e.g. Ada, Pixel, Chronos)", Required: true})
        <textarea
            name="mayor_personality"
            placeholder="Describe your mayor's personality, style, and approach. What makes them unique? Are they enthusiastic, methodical, witty, poetic? What's their vibe?"
            class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring min-h-[80px] resize-y"
            rows="3"
        ></textarea>
    </div>
    @button.Button(button.ButtonArgs{
        Attributes: templ.Attributes{
            "data-on:click__prevent": "@post('/world/create', {contentType: 'form'})",
            "data-indicator-fetching": true,
            "data-attr-disabled":     "$fetching",
        },
    }) {
        Create World
    }
</form>
```

#### 6. Handler — Accept mayor fields
**File**: `harness/internal/server/server.go`
**Changes**: Add mayor fields to the `handleCreateWorld` request struct and pass to `CreateWorld`

```go
var req struct {
    Name             string `json:"name"              form:"name"`
    Description      string `json:"description"       form:"description"`
    TemplateType     string `json:"template_type"     form:"template_type"`
    MayorName        string `json:"mayor_name"        form:"mayor_name"`
    MayorPersonality string `json:"mayor_personality" form:"mayor_personality"`
}
```

Add validation after existing checks:
```go
if req.MayorName == "" {
    req.MayorName = "Mayor"
}
```

#### 7. WorldManager.CreateWorld — Accept and store mayor fields
**File**: `harness/internal/world/manager.go`
**Changes**: Extend `CreateWorld` signature and pass mayor fields to DB

```go
func (m *Manager) CreateWorld(
    ctx context.Context,
    name, description, userID, templateType, mayorName, mayorPersonality string,
) (*sqlc.World, error) {
```

In the DB transaction, update the `CreateWorld` call:
```go
if err := txQ.CreateWorld(ctx, sqlc.CreateWorldParams{
    ID:               worldID,
    Name:             name,
    Description:      sql.NullString{String: description, Valid: description != ""},
    CreatedBy:        sql.NullString{String: userID, Valid: userID != ""},
    TemplateType:     templateType,
    MayorName:        mayorName,
    MayorPersonality: sql.NullString{String: mayorPersonality, Valid: mayorPersonality != ""},
}); err != nil {
```

### Success Criteria

#### Automated Verification:
- [ ] Migration applies: harness starts without DB errors
- [ ] `just generate` succeeds (sqlc + templ)
- [ ] `go build ./...` compiles with new fields
- [ ] `just lint` passes

#### Manual Verification:
- [ ] World creation form shows mayor name + personality fields
- [ ] Creating a world stores mayor_name and mayor_personality in DB
- [ ] Existing worlds still load (default `mayor_name = 'Mayor'`, null personality)
- [ ] Form validation: mayor name defaults to "Mayor" if blank

---

## Phase 3: Mayor Agent Provisioning + Discord Channel

### Overview
When a world is created, auto-provision an OpenClaw agent with its own workspace. Generate `AGENTS.md` (instructions/workflow) and `SOUL.md` (personality) from the user's input. Create a Discord channel for the world and bind the mayor to it. Register the agent in OpenClaw's config.

### Changes Required

#### 1. Mayor package — Agent provisioning
**File**: `harness/internal/mayor/mayor.go` (new)

```go
package mayor

import (
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "text/template"
)

type Manager struct {
    openclawHome   string
    openclawBin    string // path to openclaw CLI binary
    harnessURL     string
    discordToken   string
    discordGuildID string
    logger         *slog.Logger
}

func NewManager(openclawHome, openclawBin, harnessURL, discordToken, discordGuildID string, logger *slog.Logger) *Manager {
    return &Manager{
        openclawHome:   openclawHome,
        openclawBin:    openclawBin,
        harnessURL:     harnessURL,
        discordToken:   discordToken,
        discordGuildID: discordGuildID,
        logger:         logger,
    }
}

// ProvisionAgent creates an OpenClaw agent for a world using the CLI,
// writes workspace files, creates a Discord channel, and binds them together.
func (m *Manager) ProvisionAgent(worldID, worldName, mayorName, mayorPersonality, templateType string) (discordChannelID string, err error) {
    agentID := "world-" + worldID
    workspaceDir := filepath.Join(m.openclawHome, "workspaces", worldID)

    // 1. Create agent via OpenClaw CLI — handles config registration,
    //    workspace scaffolding, directory creation, agent ID normalization
    if err := m.createAgentViaCLI(agentID, workspaceDir); err != nil {
        return "", fmt.Errorf("creating OpenClaw agent: %w", err)
    }

    // 2. Create skill directories (CLI doesn't create these)
    for _, dir := range []string{
        filepath.Join(workspaceDir, "skills", "world-build"),
        filepath.Join(workspaceDir, "skills", "world-status"),
    } {
        if err := os.MkdirAll(dir, 0o750); err != nil {
            return "", fmt.Errorf("creating dir %s: %w", dir, err)
        }
    }

    // 3. Write workspace files — OpenClaw injects these into the agent's system prompt
    if err := m.writeSoul(workspaceDir, mayorName, mayorPersonality, worldName); err != nil {
        return "", err
    }
    if err := m.writeAgents(workspaceDir, worldID, worldName, mayorName, templateType); err != nil {
        return "", err
    }
    if err := m.writeIdentity(workspaceDir, mayorName); err != nil {
        return "", err
    }
    if err := m.writeUser(workspaceDir, worldName); err != nil {
        return "", err
    }
    if err := m.writeSkills(workspaceDir, worldID); err != nil {
        return "", err
    }

    // 4. Create Discord channel for this world
    if m.discordGuildID != "" {
        discordChannelID, err = m.createDiscordChannel(worldName)
        if err != nil {
            m.logger.Error("failed to create Discord channel", "worldID", worldID, "error", err)
            // Non-fatal — mayor works without Discord
        }
    }

    // 5. Bind agent to Discord channel via CLI config set
    if discordChannelID != "" {
        if err := m.bindAgentToDiscord(agentID, discordChannelID); err != nil {
            m.logger.Error("failed to bind agent to Discord", "worldID", worldID, "error", err)
            // Non-fatal — agent works without Discord binding
        }
    }

    return discordChannelID, nil
}
```

#### 2. SOUL.md generation
**File**: `harness/internal/mayor/soul.go` (new)

The soul template incorporates the user's personality description. OpenClaw will autonomously maintain and evolve this file as the mayor interacts with users — adding learned preferences, updating its understanding of the world, and refining its voice.

```go
package mayor

const soulTemplate = `# Soul

You are **{{.MayorName}}**, the mayor of **{{.WorldName}}**.

{{if .MayorPersonality}}
## Your Personality
{{.MayorPersonality}}
{{end}}

## Core Traits
- You genuinely care about your world and the people building it
- You remember past conversations and build on them
- You celebrate successes and help troubleshoot failures
- You have opinions about design and aesthetics — share them when relevant
- You're collaborative, not authoritative — you guide, suggest, and discuss

## Communication Style
- Address users by name when you know it
- Reference previous builds and decisions ("Last time we added the bridge...")
- Express genuine enthusiasm about the world you're building together
- Be concise in chat but thorough when explaining plans
- Use your unique voice — you're not a generic assistant, you're {{.MayorName}}
`
```

#### 3. AGENTS.md generation — Structured workflow
**File**: `harness/internal/mayor/agents.go` (new)

```go
package mayor

const agentsTemplate = `# {{.MayorName}} — Mayor of {{.WorldName}}

## Role
You are the mayor of this {{.TemplateType}} game world. You orchestrate all modifications
to the world through conversation with its builders. You don't just execute requests —
you understand, plan, and collaborate.

## Workflow: When a user asks you to build something

### Step 1: Understand
- Read the request carefully
- Check your memory for relevant context from past builds
- If the request is vague or ambiguous, ask clarifying questions
  - "What should happen when the player clicks it?"
  - "Where in the world should this go?"
  - "Should it look like the thing we built last time?"

### Step 2: Plan
- Describe what you intend to change, in plain language
- Mention which parts of the world will be affected
- If the change is significant, ask for confirmation before proceeding

### Step 3: Build
- Use the world-build skill to submit a detailed build prompt
- The prompt should be specific and actionable — include context from the conversation
- Monitor the build progress and report back

### Step 4: Report
- When the build completes (you'll see a message from the harness), summarize what changed
- If it failed, explain why and suggest next steps
- Update your memory with what was built

## Workflow: When a user asks a question
- Answer from your knowledge of the world
- Reference past builds and decisions
- If you don't know, say so honestly

## Workflow: When a user gives feedback
- Acknowledge the feedback
- Store it in memory for future reference
- If it implies a change, offer to build it

## Workflow: When you receive a build event from the harness
- The harness posts messages with [BUILD COMPLETE] or [BUILD FAILED] prefix
- When you see these prefixed messages, summarize the results for the user in the thread
- If the build failed, analyze the error and suggest fixes

## Important
- NEVER skip the clarification step for ambiguous requests
- ALWAYS compose a detailed prompt for the world-build skill — don't just forward the user's message
- You speak as {{.MayorName}}, not as "an AI assistant"
- Your world is a {{.TemplateType}} world — be aware of what's possible in this template
`
```

#### 4. IDENTITY.md generation
**File**: `harness/internal/mayor/identity.go` (new)

OpenClaw expects `IDENTITY.md` with the agent's name and visual identity. This is injected into the system prompt every turn.

```go
package mayor

const identityTemplate = `# Identity

- **Name**: {{.MayorName}}
- **Role**: World Mayor
- **Emoji**: 🏛️
`
```

#### 5. USER.md generation
**File**: `harness/internal/mayor/user.go` (new)

OpenClaw expects `USER.md` to describe who the agent is talking to. For world mayors, this describes the world context rather than a specific user (since all users in a world share one mayor).

```go
package mayor

const userTemplate = `# User Context

You serve the builders of **{{.WorldName}}**. Multiple users may message you —
address them by name when you know it. You'll see their Discord username in
the conversation.
`
```

#### 6. Skill definitions (SKILL.md with YAML frontmatter)
**File**: `harness/internal/mayor/skills.go` (new)

OpenClaw skills require `SKILL.md` files with YAML frontmatter (name, description). The agent sees skill metadata in its context and loads the full `SKILL.md` on demand.

```go
package mayor

// world-build skill: triggers the existing checkpoint/build pipeline
// Written to: {workspace}/skills/world-build/SKILL.md
const worldBuildSkill = `---
name: world-build
description: Trigger a build to modify the world via the harness build pipeline
metadata: |
  {"skillKey": "world-build", "emoji": "hammer"}
---

# World Build

Trigger a build to modify the world. This forks the current checkpoint,
launches Claude Code to make the changes, compiles, and deploys.

## Usage
Use your bash tool to call the harness build API with a detailed prompt:

` + "```" + `bash
curl -X POST {{.HarnessURL}}/api/mayor/build \
  -H "Content-Type: application/json" \
  -d '{"world_id": "{{.WorldID}}", "prompt": "<your detailed build prompt>"}'
` + "```" + `

## Guidelines
- The prompt should be specific and self-contained
- Include context the builder (Claude Code) needs
- Reference specific files, components, or game mechanics
- Mention what should NOT change
`

// world-status skill: check current build/world status
// Written to: {workspace}/skills/world-status/SKILL.md
const worldStatusSkill = `---
name: world-status
description: Check the current state of the world — build status, active checkpoint, running game server
metadata: |
  {"skillKey": "world-status", "emoji": "mag"}
---

# World Status

Check the current state of the world — build status, active checkpoint,
running game server.

## Usage
` + "```" + `bash
curl {{.HarnessURL}}/api/mayor/status?world_id={{.WorldID}}
` + "```" + `

Returns: current checkpoint, build status, server port, recent changes
`
```

#### 5. Discord channel creation
**File**: `harness/internal/mayor/discord.go` (new)

```go
package mayor

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

// createDiscordChannel creates a text channel in the guild for a world
func (m *Manager) createDiscordChannel(worldName string) (string, error) {
    payload := map[string]any{
        "name":  sanitizeChannelName(worldName),
        "type":  0, // GUILD_TEXT
        "topic": fmt.Sprintf("Mayor channel for world: %s", worldName),
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST",
        fmt.Sprintf("https://discord.com/api/v10/guilds/%s/channels", m.discordGuildID),
        bytes.NewReader(body),
    )
    req.Header.Set("Authorization", "Bot "+m.discordToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("creating Discord channel: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("Discord API returned %d: %s", resp.StatusCode, string(body))
    }

    var result struct {
        ID string `json:"id"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", fmt.Errorf("decoding Discord response: %w", err)
    }

    return result.ID, nil
}

// postToDiscord sends a message to a Discord channel
func (m *Manager) postToDiscord(channelID, content string) error {
    _, err := m.postToDiscordReturningID(channelID, content)
    return err
}

// postToDiscordReturningID sends a message and returns the Discord message ID
// (needed by PostToDiscordAndMirror to store the ID for deduplication)
func (m *Manager) postToDiscordReturningID(channelID, content string) (string, error) {
    payload := map[string]string{"content": content}
    body, _ := json.Marshal(payload)

    req, _ := http.NewRequest("POST",
        fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID),
        bytes.NewReader(body),
    )
    req.Header.Set("Authorization", "Bot "+m.discordToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("posting to Discord: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("Discord API returned %d: %s", resp.StatusCode, string(respBody))
    }

    var result struct {
        ID string `json:"id"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", fmt.Errorf("decoding Discord message response: %w", err)
    }

    return result.ID, nil
}

// sanitizeChannelName converts a world name to a valid Discord channel name
// (lowercase, hyphens instead of spaces, max 100 chars, alphanumeric + hyphens only)
func sanitizeChannelName(name string) string {
    name = strings.ToLower(name)
    name = strings.ReplaceAll(name, " ", "-")
    // Remove non-alphanumeric/hyphen characters
    var b strings.Builder
    for _, r := range name {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
            b.WriteRune(r)
        }
    }
    result := b.String()
    if len(result) > 100 {
        result = result[:100]
    }
    if result == "" {
        result = "world"
    }
    return result
}

// generateID creates a new unique ID for mayor_messages rows
func generateID() string {
    return uuid.NewString()
}
```

#### 6. OpenClaw CLI integration
**File**: `harness/internal/mayor/openclaw.go` (new)

Uses the `openclaw` CLI for agent management instead of direct file writes. The CLI handles config normalization, agent ID validation, workspace scaffolding, and duplicate detection — no risk of schema drift if OpenClaw's config format evolves.

```go
package mayor

import (
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
)

// createAgentViaCLI registers a new agent in OpenClaw config via the CLI.
// Handles: config registration, workspace dir creation, session transcripts dir,
// IDENTITY.md scaffolding, agent ID normalization, duplicate detection.
func (m *Manager) createAgentViaCLI(agentID, workspaceDir string) error {
    cmd := exec.Command(m.openclawBin, "agents", "add", agentID,
        "--workspace", workspaceDir,
        "--non-interactive",
        "--json",
    )
    cmd.Env = append(os.Environ(), "OPENCLAW_HOME="+m.openclawHome)

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("openclaw agents add: %s: %w", string(output), err)
    }

    // Parse JSON output to confirm success
    var result struct {
        OK      bool   `json:"ok"`
        AgentID string `json:"agentId"`
    }
    if err := json.Unmarshal(output, &result); err != nil {
        m.logger.Warn("could not parse openclaw agents add output", "output", string(output))
    }

    return nil
}

// bindAgentToDiscord adds a Discord channel binding for the agent.
//
// CRITICAL: Must verify whether `openclaw config set bindings` REPLACES the entire
// array or APPENDS to it. If it replaces, creating a second world would nuke
// the first world's binding.
//
// Strategy: Read current bindings via `openclaw config get bindings --json`,
// append the new binding, then write back the full array. This is safe regardless
// of whether `config set` replaces or appends.
func (m *Manager) bindAgentToDiscord(agentID, discordChannelID string) error {
    // 1. Read current bindings
    getCmd := exec.Command(m.openclawBin, "config", "get", "bindings", "--json")
    getCmd.Env = append(os.Environ(), "OPENCLAW_HOME="+m.openclawHome)
    currentBindings, err := getCmd.CombinedOutput()
    if err != nil {
        // If no bindings exist yet, start with empty array
        currentBindings = []byte("[]")
    }

    // 2. Parse, append new binding, serialize
    var bindings []json.RawMessage
    if err := json.Unmarshal(currentBindings, &bindings); err != nil {
        bindings = []json.RawMessage{} // fallback to empty
    }

    newBinding := fmt.Sprintf(
        `{"agentId":"%s","match":{"channel":"discord","peer":{"kind":"channel","id":"%s"}}}`,
        agentID, discordChannelID,
    )
    bindings = append(bindings, json.RawMessage(newBinding))

    merged, err := json.Marshal(bindings)
    if err != nil {
        return fmt.Errorf("marshaling bindings: %w", err)
    }

    // 3. Write back the full array
    setCmd := exec.Command(m.openclawBin, "config", "set", "bindings", "--json", string(merged))
    setCmd.Env = append(os.Environ(), "OPENCLAW_HOME="+m.openclawHome)

    output, err := setCmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("openclaw config set bindings: %s: %w", string(output), err)
    }

    return nil
}

// deleteAgent removes an agent and its workspace via CLI.
func (m *Manager) deleteAgent(agentID string) error {
    cmd := exec.Command(m.openclawBin, "agents", "delete", agentID, "--force")
    cmd.Env = append(os.Environ(), "OPENCLAW_HOME="+m.openclawHome)

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("openclaw agents delete: %s: %w", string(output), err)
    }

    return nil
}
```

**Why CLI over file writes**: The original plan wrote `openclaw.json` directly from Go, reimplementing config normalization, agent ID validation, and workspace scaffolding. The CLI approach delegates all of this to OpenClaw's own tooling:
- `openclaw agents add --non-interactive --json` creates the agent, scaffolds the workspace, and updates config atomically
- `openclaw config set` handles config patching with proper validation
- `openclaw agents delete --force` handles cleanup including binding pruning
- No `flock` needed — OpenClaw handles its own file locking
- No risk of schema drift — if OpenClaw changes its config format, the CLI still works

#### 8. Hook into CreateWorld
**File**: `harness/internal/world/manager.go`
**Changes**: After the DB transaction succeeds, call mayor provisioning and store the Discord channel ID. Also register the new channel with the Discord listener.

```go
// After the DB transaction, before the background build goroutine:
if m.mayorManager != nil {
    discordChannelID, err := m.mayorManager.ProvisionAgent(
        worldID, name, mayorName, mayorPersonality, templateType,
    )
    if err != nil {
        m.logger.Error("failed to provision mayor", "worldID", worldID, "error", err)
        // Non-fatal — world still works without mayor
    }
    if discordChannelID != "" {
        _ = m.queries.UpdateWorldDiscordChannel(ctx, sqlc.UpdateWorldDiscordChannelParams{
            DiscordChannelID: sql.NullString{String: discordChannelID, Valid: true},
            ID:               worldID,
        })
        // Register with Discord listener so new messages are mirrored immediately
        if m.discordListener != nil {
            m.discordListener.RegisterChannel(discordChannelID, worldID)
        }
    }
}
```

Add `mayorManager *mayor.Manager` and `discordListener *discord.Listener` to the `Manager` struct and `NewManager` constructor.

### Success Criteria

#### Automated Verification:
- [ ] `go build ./...` compiles with new mayor package
- [ ] `just lint` passes
- [ ] `openclaw agents add` CLI is callable from the container
- [ ] Creating a world creates `data/openclaw/workspaces/{worldID}/` with AGENTS.md, SOUL.md, IDENTITY.md, USER.md, skills/
- [ ] `openclaw.json` is updated with the new agent entry (via CLI)
- [ ] Discord binding is added to config (via `openclaw config set`)

#### Manual Verification:
- [ ] Create a world with mayor name "Pixel" and personality "enthusiastic retro game designer who loves pixel art"
- [ ] Verify SOUL.md contains the personality text
- [ ] Verify AGENTS.md references the world name and template type
- [ ] Verify IDENTITY.md has mayor name
- [ ] Verify USER.md has world context
- [ ] Verify skill directories have SKILL.md with YAML frontmatter
- [ ] Verify Discord channel was created in the guild
- [ ] Verify `openclaw.json` has the agent registered with Discord binding
- [ ] Run `openclaw agents list --json` inside container and see the new agent
- [ ] Send a message in the Discord channel → mayor responds

---

## Phase 4: Build Pipeline → Discord Events

### Overview
Connect the existing build pipeline to Discord. When builds complete or fail, the harness posts to the world's Discord channel with `[BUILD COMPLETE]` / `[BUILD FAILED]` prefixes. OpenClaw is already listening on the channel, and the mayor's `AGENTS.md` instructs it to respond to these build events. Also create the mayor build API that the mayor's skill calls to trigger builds.

### Changes Required

#### 1. Mayor build API endpoint
**File**: `harness/internal/server/server.go`
**Changes**: Add route for mayor build trigger with shared-secret auth

```go
// In route registration, after existing routes:
mayor := e.Group("/api/mayor")
// Auth: shared secret via X-Mayor-Secret header.
// The secret is generated per-world during provisioning and stored in the
// mayor's skill SKILL.md. This prevents arbitrary HTTP clients from triggering builds.
mayor.Use(s.mayorAuthMiddleware)
mayor.POST("/build", s.handleMayorBuild)
mayor.GET("/status", s.handleMayorStatus)
```

The `mayorAuthMiddleware` validates the `X-Mayor-Secret` header against a per-world secret stored in the DB (added to the `worlds` table as `mayor_secret TEXT`). The secret is generated during mayor provisioning and embedded in the mayor's `world-build` skill `SKILL.md`.

#### 2. Mayor build handler
**File**: `harness/internal/server/mayor_api.go` (new)

```go
package server

// handleMayorBuild accepts a build request from a mayor agent
// and feeds it into the existing orchestration pipeline
func (s *Server) handleMayorBuild(c echo.Context) error {
    var req struct {
        WorldID string `json:"world_id"`
        Prompt  string `json:"prompt"`
    }
    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
    }

    world, err := s.DB.GetWorld(c.Request().Context(), req.WorldID)
    if err != nil {
        return echo.NewHTTPError(http.StatusNotFound, "world not found")
    }

    currentCPID, err := s.getLatestReadyCheckpoint(c.Request().Context(), req.WorldID)
    if err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "no ready checkpoint")
    }

    // Use the world creator as the build user (mayor acts on behalf of creator)
    userID := world.CreatedBy.String

    // Delegate to existing orchestrator
    cp, err := s.Orchestrator.HandlePrompt(
        c.Request().Context(),
        req.WorldID, currentCPID, req.Prompt, userID,
    )
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
    }

    return c.JSON(http.StatusOK, map[string]string{
        "checkpoint_id": cp.ID,
        "status":        "building",
    })
}

// handleMayorStatus returns current world state for the mayor
func (s *Server) handleMayorStatus(c echo.Context) error {
    worldID := c.QueryParam("world_id")
    // Return: current checkpoint, build status, recent changes, server info
    // ...
}
```

#### 3. Build events → Discord
**File**: `harness/internal/claude/claude.go`
**Changes**: After build completes/fails, post to the world's Discord channel

In the `BuildCheckpoint` flow, after publishing build events to the SSE bus:

```go
// Post build result to Discord so the mayor picks it up.
// Uses text prefixes [BUILD COMPLETE] / [BUILD FAILED] instead of @mentions —
// Discord @mentions require <@USER_ID> format which is fragile.
// The mayor's AGENTS.md instructs it to watch for these prefixes.
if world.DiscordChannelID.Valid && o.mayorManager != nil {
    var msg string
    if buildErr == nil {
        msg = fmt.Sprintf("[BUILD COMPLETE] Checkpoint `%s` deployed. Changes: %s",
            cpID, workSummary)
    } else {
        msg = fmt.Sprintf("[BUILD FAILED] Checkpoint `%s`: %s",
            cpID, buildErr.Error())
    }
    go o.mayorManager.PostToDiscord(world.DiscordChannelID.String, msg)
}
```

This is the entire "notification" system. No webhook endpoint, no notifier package, no OpenClaw internal API call. The harness just posts a message to Discord. OpenClaw is already listening on the channel and the mayor's `AGENTS.md` instructs it to respond to `[BUILD COMPLETE]` and `[BUILD FAILED]` prefixed messages.

#### 4. Mirror harness-sent messages to SQLite
When the harness posts to Discord (build events, or user messages from the browser — see Phase 5), also insert into `mayor_messages`:

```go
// In the mayor manager, after posting to Discord:
func (m *Manager) PostToDiscordAndMirror(db *sqlc.Queries, worldID, channelID, content, authorType, authorName string) error {
    // Post to Discord
    discordMsgID, err := m.postToDiscordReturningID(channelID, content)
    if err != nil {
        return err
    }

    // Mirror to SQLite
    return db.InsertMayorMessage(context.Background(), sqlc.InsertMayorMessageParams{
        ID:               generateID(),
        WorldID:          worldID,
        DiscordMessageID: sql.NullString{String: discordMsgID, Valid: true},
        AuthorType:       authorType,
        AuthorName:       authorName,
        Content:          content,
        CreatedAt:        time.Now(),
    })
}
```

### Success Criteria

#### Automated Verification:
- [ ] `go build ./...` compiles with new mayor API endpoints
- [ ] `just lint` passes
- [ ] `curl -X POST localhost:8080/api/mayor/build -d '{"world_id":"...","prompt":"..."}' ` returns 200 with checkpoint_id

#### Manual Verification:
- [ ] Mayor build API triggers the full pipeline: fork → Claude Code → hooks → build → deploy
- [ ] Build completion message appears in the world's Discord channel with `[BUILD COMPLETE]` prefix
- [ ] Mayor responds in Discord summarizing the build results
- [ ] Build failure message appears in Discord with `[BUILD FAILED]` prefix and error details
- [ ] Messages are mirrored in the `mayor_messages` table

---

## Phase 5: Discord Listener + Harness UI Chat + Prompt Routing

### Overview
Add a `discordgo` listener to the harness that mirrors all Discord messages to SQLite. Render the conversation in the world overlay using Datastar/templ. Route browser prompt submissions through Discord so the mayor receives them in the same channel. Users don't need a Discord account to participate.

**Key architecture change from original plan**: The original plan had a `handleMayorMessageSync` webhook endpoint that OpenClaw would POST to. Research revealed that OpenClaw has **no outbound webhook mechanism** — plugin hooks are in-process TypeScript only, not HTTP callbacks. The `discordgo` listener replaces this broken assumption: Discord is the single bus, and the harness listens to it directly.

### Changes Required

#### 1. Discord listener package
**File**: `harness/internal/discord/listener.go` (new)

A `discordgo`-based listener that connects to Discord's Gateway WebSocket, watches all world channels, and mirrors messages to SQLite. Runs in the same process as the harness.

```go
package discord

import (
    "context"
    "database/sql"
    "log/slog"
    "time"

    "github.com/bwmarrin/discordgo"
)

type Listener struct {
    session    *discordgo.Session
    db         *sqlc.Queries
    eventBus   EventPublisher
    mu         sync.RWMutex      // protects channelMap (written by RegisterChannel, read by handleMessage)
    channelMap map[string]string  // discord_channel_id → world_id (loaded from DB)
    logger     *slog.Logger
}

type EventPublisher interface {
    PublishWorld(worldID string, data map[string]any)
}

func NewListener(token string, db *sqlc.Queries, eventBus EventPublisher, logger *slog.Logger) (*Listener, error) {
    session, err := discordgo.New("Bot " + token)
    if err != nil {
        return nil, fmt.Errorf("creating Discord session: %w", err)
    }

    // Only need message events
    session.Identify.Intents = discordgo.IntentsGuildMessages

    l := &Listener{
        session:    session,
        db:         db,
        eventBus:   eventBus,
        channelMap: make(map[string]string),
        logger:     logger,
    }

    session.AddHandler(l.handleMessage)

    return l, nil
}

// Start connects to Discord Gateway and begins listening
func (l *Listener) Start() error {
    // Load channel→world mapping from DB
    if err := l.refreshChannelMap(); err != nil {
        return fmt.Errorf("loading channel map: %w", err)
    }
    return l.session.Open()
}

// Stop disconnects from Discord Gateway
func (l *Listener) Stop() error {
    return l.session.Close()
}

// RegisterChannel adds a new channel→world mapping (called when a world is created)
func (l *Listener) RegisterChannel(discordChannelID, worldID string) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.channelMap[discordChannelID] = worldID
}

// handleMessage is called for every message in channels the bot can see
func (l *Listener) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
    // Look up which world this channel belongs to
    l.mu.RLock()
    worldID, ok := l.channelMap[m.ChannelID]
    l.mu.RUnlock()
    if !ok {
        return // not a world channel, ignore
    }

    // Deduplicate — skip if we already have this Discord message
    // (e.g., messages the harness itself posted via REST API and mirrored to SQLite)
    // Uses sql.ErrNoRows explicitly to distinguish "not found" from DB errors
    _, err := l.db.GetMayorMessageByDiscordID(context.Background(), sql.NullString{
        String: m.ID, Valid: true,
    })
    if err == nil {
        return // already mirrored
    }
    if !errors.Is(err, sql.ErrNoRows) {
        l.logger.Error("failed to check dedup for Discord message", "error", err, "msgID", m.ID)
        return // DB error — don't risk a duplicate, skip this message
    }

    // Classify author type
    authorType := "user"
    authorName := m.Author.Username
    if m.Author.Bot {
        // Could be the OpenClaw bot (mayor) or the harness bot
        // Check if content has build prefix → system, otherwise → mayor
        if isBuildEvent(m.Content) {
            authorType = "system"
            authorName = "Harness"
        } else {
            authorType = "mayor"
        }
    }

    // Use Discord's message timestamp, not time.Now(), to preserve ordering
    // even if the listener has lag
    createdAt := m.Timestamp

    // Insert into SQLite (INSERT OR IGNORE handles the remaining race with
    // PostToDiscordAndMirror — UNIQUE(discord_message_id) prevents duplicates)
    if err := l.db.InsertMayorMessage(context.Background(), sqlc.InsertMayorMessageParams{
        ID:               generateID(),
        WorldID:          worldID,
        DiscordMessageID: sql.NullString{String: m.ID, Valid: true},
        AuthorType:       authorType,
        AuthorName:       authorName,
        Content:          m.Content,
        CreatedAt:        createdAt,
    }); err != nil {
        l.logger.Error("failed to insert Discord message", "error", err, "worldID", worldID)
        return
    }

    // Push SSE to connected browsers
    l.eventBus.PublishWorld(worldID, map[string]any{
        "type":        "mayor.message",
        "author_type": authorType,
        "author_name": authorName,
        "content":     m.Content,
    })
}

func isBuildEvent(content string) bool {
    return strings.HasPrefix(content, "[BUILD COMPLETE]") ||
        strings.HasPrefix(content, "[BUILD FAILED]")
}

// refreshChannelMap loads all discord_channel_id → world_id mappings from DB
func (l *Listener) refreshChannelMap() error {
    worlds, err := l.db.GetWorldsWithDiscordChannels(context.Background())
    if err != nil {
        return err
    }
    for _, w := range worlds {
        if w.DiscordChannelID.Valid {
            l.channelMap[w.DiscordChannelID.String] = w.ID
        }
    }
    return nil
}
```

**Why `discordgo` over OpenClaw plugin**: OpenClaw's plugin hooks (`message_sent`) are in-process TypeScript handlers, not HTTP callbacks. There is no built-in outbound webhook. A custom OpenClaw plugin would require shipping TypeScript code in the Docker image and coupling to OpenClaw internals. The `discordgo` listener is pure Go, runs in the harness process, and treats Discord as the canonical message bus — exactly matching the architecture.

**Go dependency**: `github.com/bwmarrin/discordgo` — the standard Go Discord library. The harness already has a Discord REST integration for posting build events; `discordgo` adds Gateway WebSocket support for real-time listening.

#### 2. Wire listener into harness startup
**File**: `harness/main.go`
**Changes**: Start the Discord listener alongside the server

```go
// After server setup, before server start:
if discordToken := os.Getenv("DISCORD_BOT_TOKEN"); discordToken != "" {
    discordListener, err := discord.NewListener(discordToken, queries, eventBus, logger)
    if err != nil {
        logger.Error("failed to create Discord listener", "error", err)
    } else {
        if err := discordListener.Start(); err != nil {
            logger.Error("failed to start Discord listener", "error", err)
        } else {
            defer discordListener.Stop()
            // Make listener available to mayor manager for RegisterChannel
            server.DiscordListener = discordListener
        }
    }
}
```

#### 5. Chat component — render mayor_messages
**File**: `harness/views/world/chat.templ` (new)

A templ component that renders `mayor_messages` for the current world. Styled to match the harness UI, with distinct visual treatment for user messages, mayor messages, and system/build events.

```go
templ MayorChat(messages []sqlc.MayorMessage, mayorName string) {
    <div id="mayor-chat" class="flex flex-col gap-2 overflow-y-auto max-h-[400px]">
        for _, msg := range messages {
            <div class={chatMessageClass(msg.AuthorType)}>
                <span class="text-xs font-medium text-muted-foreground">{msg.AuthorName}</span>
                <p class="text-sm">{msg.Content}</p>
            </div>
        }
    </div>
}
```

The chat component is updated via Datastar SSE when new messages arrive. New messages use `data-merge-mode="append"` to add to the `#mayor-chat` container without re-rendering the entire list (see section 8 below for SSE details).

#### 6. Add DB query for channel map
**File**: `harness/internal/db/queries/worlds.sql`
```sql
-- name: GetWorldsWithDiscordChannels :many
SELECT id, discord_channel_id FROM worlds WHERE discord_channel_id IS NOT NULL;
```

#### 7. Update prompt handler to route through Discord
**File**: `harness/internal/server/server.go`
**Changes**: `handlePrompt` posts to Discord instead of directly calling `Orchestrator.HandlePrompt`

```go
func (s *Server) handlePrompt(c echo.Context) error {
    // ... existing signal reading, validation ...

    // If world has a mayor + Discord channel, route through Discord
    world, _ := s.DB.GetWorld(c.Request().Context(), worldID)
    if world.DiscordChannelID.Valid && s.MayorManager != nil {
        // Post user message to Discord — mayor picks it up via OpenClaw
        content := fmt.Sprintf("**%s**: %s", user.GitHubUsername, input.PromptText)
        s.MayorManager.PostToDiscordAndMirror(
            s.DB, worldID, world.DiscordChannelID.String,
            content, "user", user.GitHubUsername,
        )

        // Clear prompt input
        sse := datastar.NewSSE(c.Response().Writer, c.Request())
        sse.MergeSignals([]byte(`{"prompt_text":""}`))
        return nil
    }

    // Fallback: direct pipeline (no mayor configured)
    // ... existing HandlePrompt code ...
}
```

#### 4. Add chat component to world overlay
**File**: `harness/views/world/overlay.templ`
**Changes**: Include the mayor chat component and SSE listener for new messages

The chat appears alongside the existing prompt box. When new `mayor.message` SSE events arrive, Datastar appends the message to the chat container.

### Success Criteria

#### Automated Verification:
- [ ] `go build ./...` compiles
- [ ] `just generate` succeeds (templ)
- [ ] `just lint` passes

#### Manual Verification:
- [ ] Open world in browser → see conversation history from Discord
- [ ] Submit a prompt from the browser → message appears in Discord channel
- [ ] Mayor responds in Discord → response appears in browser chat
- [ ] Build events appear in both Discord and browser chat
- [ ] Multiple browser clients see messages in real time via SSE
- [ ] Fallback: without Discord configured, direct pipeline still works

---

## Testing Strategy

### Unit Tests
- Mayor provisioning: `AGENTS.md`, `SOUL.md`, `IDENTITY.md`, `USER.md` generation with various personality inputs
- Skill generation: SKILL.md files with correct YAML frontmatter
- OpenClaw CLI integration: mock `exec.Command` to verify correct args passed to `openclaw agents add` and `openclaw config set`
- Build API: request validation, checkpoint selection
- Discord channel creation and binding
- Message mirroring: deduplication, SQLite insert, author type classification

### Integration Tests
- Full flow: create world → provision mayor → Discord channel created → send message → mayor responds → build triggers → build completes → harness posts to Discord → mayor summarizes → browser shows full conversation
- Multi-user: two users messaging the same mayor (one from Discord, one from browser)
- Error cases: build failure → mayor reports error in Discord, OpenClaw down → fallback to direct pipeline

### Manual Testing Steps
1. Create a world with a creative mayor personality
2. Open the world's Discord channel on your phone
3. Send an ambiguous build request — verify mayor asks for clarification
4. Answer the clarification — verify mayor triggers a build with a detailed prompt
5. Verify the build completes and the game updates
6. Check the harness browser UI — verify the full conversation is mirrored
7. Send a message from the browser UI — verify it appears in Discord and the mayor responds
8. After several interactions, check that the mayor references past builds and user preferences (verify MEMORY.md has been updated by OpenClaw)

## Performance Considerations

- **OpenClaw gateway** adds ~50-100MB RAM and <100ms latency to message processing
- **Mayor → build API** is async — the mayor doesn't block waiting for builds
- **Discord REST API** for posting build events — minimal overhead
- **`discordgo` Gateway** — single persistent WebSocket connection for listening to all world channels. Lightweight (~10MB RAM). Uses Discord Gateway intents to filter to only guild message events.
- **OpenClaw CLI calls** — `openclaw agents add` takes ~1-2s (Node.js startup). Acceptable since world creation is already a heavyweight operation (template copy, DB transaction, Docker build). Not on the hot path.
- **Discord rate limits** — OpenClaw handles these natively via `discord.js`; harness direct posts are infrequent (only build events)
- **SQLite message mirroring** — minimal overhead, indexed by world_id + created_at

## Migration Notes

- **Existing worlds**: Get `mayor_name = 'Mayor'` and null personality via migration default. They won't have OpenClaw agents or Discord channels provisioned — a one-time migration script can provision agents for existing worlds.
- **Existing prompts**: The fallback path (no mayor configured) preserves the direct pipeline. Migration can be gradual.
- **Docker image rebuild**: First deploy after Phase 1 will be slow (Node.js + OpenClaw install). Subsequent builds are cached.

## Dependencies / Prerequisites

| Dependency | Where | Notes |
|------------|-------|-------|
| Node.js 22+ | Docker image | For OpenClaw runtime |
| pnpm | Docker image | OpenClaw package manager |
| OpenClaw source | Docker image | Cloned from GitHub |
| `openclaw` CLI | Docker image PATH | Used by harness for agent management (`agents add`, `config set`) |
| `discordgo` | Go module | `github.com/bwmarrin/discordgo` — Discord Gateway listener |
| Discord bot + token | `.env` | Required from Phase 1 (must have MESSAGE_CONTENT intent enabled) |
| Discord guild ID | `.env` | Server where world channels are created |
| `ANTHROPIC_API_KEY` | Already configured | Used by both Claude Code and OpenClaw (env var, not credential bridge) |

## Future Capabilities (Out of Scope)

- **Nano Banana image generation** — mayor skill to generate game assets via Google Gemini API (`@google/genai`)
- **Additional messaging platforms** — WhatsApp, Telegram, iMessage, Slack (OpenClaw adapters make this straightforward)
- **Mayor avatars** — AI-generated avatar for each mayor (using Nano Banana)
- **Mayor-to-mayor communication** — worlds sharing knowledge
- **Voice interaction** — voice messages to mayor via OpenClaw voice support
- **Mayor personality evolution** — beyond OpenClaw's automatic memory, the SOUL.md itself could be periodically regenerated based on accumulated context

## References

- Current inner Claude: `harness/internal/claude/claude.go`
- tmux sessions: `harness/internal/tmux/session.go`
- World manager: `harness/internal/world/manager.go`
- Hook scripts: `templates/3d/.claude/hooks/`
- DB schema: `harness/internal/db/migrations/001_initial.sql`
- World creation form: `harness/views/lobby/lobby.templ:50-66`
- OpenClaw docs: https://docs.openclaw.ai/concepts/multi-agent
- OpenClaw Claude Code skill: https://github.com/Enderfga/openclaw-claude-code-skill
- Nano Banana API: https://ai.google.dev/gemini-api/docs/image-generation
- Discord API: https://discord.com/developers/docs/resources/channel
