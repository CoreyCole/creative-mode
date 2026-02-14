# World Mayors — Master Implementation Plan

> **Supersedes:**
> - `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`
> - `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md`

## Overview

Replace the direct "inner Claude" pipeline with **world mayors** — persistent OpenClaw AI agents, one per world, that orchestrate all world modifications through structured conversation. Users interact with their mayor from **Discord** (the primary interface — accessible from any phone, no Tailnet required) or from the **harness UI** (which mirrors the Discord conversation). Each mayor has a unique personality crafted by the world creator through a rich context-gathering flow, its own evolving memory of the world's history, and follows a structured research → plan → clarify → implement workflow using Claude Code under the hood.

A dedicated **Mayor Dashboard** (`/mayor/:worldID`) provides full observability into the mayor's internals: memory inspection, task/session tracking, activity logs, and OpenClaw API integration via CLI wrappers. Every mayor touchpoint is instrumented to SQLite from day one.

## Current State Analysis

### What Exists
- **Inner Claude pipeline** (`harness/internal/claude/claude.go`): User prompt → `ForkCheckpoint` → write `MEMORY.md` → tmux Claude Code session → hook scripts POST events → `BuildCheckpoint` → compile + deploy
- **tmux session management** (`harness/internal/tmux/session.go`): Named sessions (`cm-{worldID}-{cpID}`) with env vars for hook script communication
- **Hook-driven pipeline** (`templates/3d/.claude/hooks/`): `on-tool-use.sh`, `on-stop.sh`, `on-notification.sh` POST JSONL events to `/api/claude-event`
- **Event-driven SSE** (`harness/internal/server/events.go`): Real-time browser updates via Datastar signal patches
- **World creation** (`harness/internal/world/manager.go`): Template copy → DB transaction → background build → game server start
- **Simple form** (`harness/views/lobby/lobby.templ:50-66`): name, description, template_type — native HTML form, no Datastar signals
- **Image generation** (`harness/internal/server/imagegen.go`): Gemini-based image gen with ReadSignals → NewSSE → patch fragments pattern
- **Chat panel** (`harness/views/chat/chat.templ`): 4 tabs (Global, World, Lineage, Assets)

### Key Constraints
- Docker image (`harness/Dockerfile`) is Go+Rust, **no Node.js runtime** — must add for OpenClaw
- 2D template has **no `.claude/` hooks** — needs hooks before mayors can orchestrate 2D builds
- Claude Code runs inside Docker via native installer (no Node.js dependency)
- `ANTHROPIC_API_KEY` already passed through to container
- OpenClaw requires Node.js 22+ and pnpm

### Key Discoveries (verified against `context/openclaw/` source)

**Agent Workspace Files** — Each agent gets isolated bootstrap files created by `ensureAgentWorkspace()` at `context/openclaw/src/agents/workspace.ts:153-226`:
- Created on `agents add`: AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, HEARTBEAT.md, BOOTSTRAP.md
- **MEMORY.md is NOT auto-created** — OpenClaw creates it on first agent memory write during a conversation
- Files written with `wx` flag (skip if exists), so our templates overwrite them after CLI creates them
- Gateway file APIs (`agents.files.list/get/set`) restrict access to bootstrap set + MEMORY.md only — custom files (skills) must be written directly to disk

**Config and Binding Behavior**:
- `openclaw config set` does a **FULL REPLACE** at the given path — must use read-modify-write for bindings
- `openclaw config get bindings --json` returns the full array
- Per-channel routing uses `peer` match: `{"agentId": "world-abc", "match": {"channel": "discord", "peer": {"kind": "channel", "id": "CHANNEL_ID"}}}`
- Route resolution priority: peer > parent-peer > guild+roles > guild > team > account > channel > default

**Gateway Hot-Reload** — Config changes picked up without restart via chokidar file watcher. Agent/binding changes classified as `"none"` in reload plan — no restart needed.

**HTTP Hooks API** — `POST /hooks/agent`: Bearer token auth, JSON body with `{message, agentId, sessionKey}`, returns `202 Accepted` with `{ok: true, runId: "..."}`. Fire-and-forget — no response body.

**Session Management APIs** — Gateway WebSocket methods: `sessions.list`, `sessions.preview`, `sessions.patch`, `sessions.reset`, `sessions.delete`, `sessions.compact`. Also `openclaw status --json --all --deep` CLI for gateway health/agent status.

### Architectural Decisions

1. **Agent management: OpenClaw CLI** — `openclaw agents add --non-interactive --json` + `openclaw config set bindings --json` from Go via `exec.Command`. CLI handles normalization, scaffolding, validation. No risk of schema drift.

2. **Message sync: `discordgo` listener** — OpenClaw has NO outbound webhook mechanism. Harness runs `discordgo` Gateway listener to mirror all world channel messages to SQLite.

3. **Build event format: text prefixes** — `[BUILD COMPLETE]` / `[BUILD FAILED]` in Discord. Mayor's `AGENTS.md` instructs it to watch for these. Simpler than Discord @mentions.

4. **API key: `ANTHROPIC_API_KEY` env var** — simpler for Docker than credential bridge.

5. **Two Discord bots** — Mayor bot (OpenClaw persona) + Harness bot (listener, channel management, build events). Clean separation, no session conflicts.

6. **Auth: Discord OAuth primary, GitHub linking** — Discord is the primary interaction channel. GitHub kept as optional account linking.

7. **Private world channels by default** — `permission_overwrites` deny @everyone, grant access to bots + world creator. Creator invites others via harness UI.

8. **OpenClaw API via CLI wrappers** — Shell out to `openclaw status --json`, `openclaw sessions list --json`, etc. via `exec.Command`. Simpler than WebSocket client for the dashboard.

9. **Mayor Dashboard as dedicated page** — `/mayor/:worldID` with full-width layout. Not embedded in the chat panel overlay.

10. **Full instrumentation from day one** — `mayor_activity`, `mayor_sessions`, `mayor_builds` tables populated at every touchpoint. Dashboard reads live data immediately.

## Desired End State

1. **Every world has a mayor** — auto-provisioned on world creation with rich personality from multi-step context gathering
2. **Discord is the primary interface** — each world gets a Discord channel; users message the mayor from their phone
3. **Harness UI mirrors Discord** — messages stored in SQLite, rendered with Datastar/templ
4. **Discord is the communication bus** — harness posts build events to Discord with `[BUILD]` prefixes; OpenClaw picks them up naturally
5. **Mayor learns over time** — OpenClaw's autonomous context management means the mayor accumulates knowledge, remembers preferences, references past builds
6. **Existing pipeline intact** — mayor composes detailed prompts and calls the harness build API; ForkCheckpoint → Claude Code → hooks → BuildCheckpoint still works
7. **Mayor Dashboard** — dedicated `/mayor/:worldID` page with memory inspector, task/session tracking, activity logs, and OpenClaw integration
8. **Full observability** — every mayor interaction, build delegation, and session event logged to SQLite and visible in the dashboard

### Verification
- Create a world → multi-step personality form gathers rich context → mayor provisioned with deep personality
- Send a message from Discord → mayor responds conversationally, asks clarifying questions
- Mayor triggers a build → existing pipeline executes, build status appears in Discord
- Open the harness UI → see the full Discord conversation mirrored
- Send a message from the harness UI → posts to Discord, mayor responds, response mirrors back
- After several interactions, mayor references past builds and user preferences from memory
- Open `/mayor/:worldID` → see memory files, active sessions, build history, activity timeline

## What We're NOT Doing

- **Replacing Claude Code** — mayors orchestrate Claude Code, they don't replace it
- **Multiple messaging platforms in MVP** — Discord first, others later via OpenClaw adapters
- **Gemini prompt attenuation** — mayor handles prompt enhancement naturally as part of its conversational workflow (Step 2: Plan in AGENTS.md). No separate "Suggest" button, SuggestionTracker, or async callback pattern
- **Per-user mayors** — one mayor per world, not per user
- **OpenClaw as separate container** — runs in the same Docker container
- **Voice interaction** — text-only for now
- **Mayor-to-mayor communication** — worlds are independent
- **Embedding Discord widgets** — we mirror messages to SQLite and render natively
- **WebSocket client for OpenClaw API** — CLI wrappers are simpler and sufficient

## Architecture

### Communication Flow

Discord is the single communication bus. All messages flow through the world's Discord channel. The harness mirrors everything to SQLite for the browser UI.

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
   |         v OpenClaw picks up the message
   |    Mayor summarizes results in Discord thread
   |
   +--- discordgo listener mirrors ALL Discord messages → SQLite → SSE → browser UI
   |
   +--- Mayor Dashboard (/mayor/:worldID) reads SQLite + OpenClaw CLI for full observability
```

### No Webhook Loop

The harness posts build events to Discord with `[BUILD COMPLETE]` or `[BUILD FAILED]` prefix. The mayor's `AGENTS.md` instructs it to watch for these. OpenClaw is already listening on the channel. No custom webhook bridge needed.

---

## Phase 1: OpenClaw + Discord in Docker

### Overview
Add Node.js runtime, OpenClaw, and Discord bot configuration to the Docker image. Get a single OpenClaw gateway running with the Discord adapter connected. Fix the 2D template hooks.

### Changes Required

#### 1. Dockerfile — Add Node.js + pnpm + OpenClaw
**File**: `harness/Dockerfile`

```dockerfile
# After Rust tools, before Go tools:

# Node.js 22 LTS (required for OpenClaw)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g pnpm@latest

# OpenClaw (AI agent framework)
# Skip pnpm ui:build — gateway doesn't need the UI, saves ~200MB
RUN git clone --depth 1 https://github.com/openclaw/openclaw.git /opt/openclaw && \
    cd /opt/openclaw && \
    pnpm install --frozen-lockfile && \
    pnpm build

ENV OPENCLAW_HOME=/data/openclaw
ENV PATH="/opt/openclaw/node_modules/.bin:$PATH"
```

#### 2. Entrypoint — Start OpenClaw gateway alongside Air
**File**: `harness/scripts/dev-entrypoint.sh`

```bash
# After tmux start-server:
if [ -f "$OPENCLAW_HOME/openclaw.json" ]; then
    echo "  Starting OpenClaw gateway..."
    cd /opt/openclaw && node src/gateway/server.js &
    OPENCLAW_PID=$!
    echo "  OpenClaw gateway PID: $OPENCLAW_PID (port 18789)"
fi
```

#### 3. Docker Compose — Expose OpenClaw port, add env vars
**File**: `harness/docker-compose.yml`

```yaml
ports:
  - "8080:8080"
  - "8081-8180:8081-8180"
  - "9001-9100:9001-9100"
  - "18789:18789"  # OpenClaw gateway
environment:
  - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
  # Discord OAuth (primary auth)
  - DISCORD_CLIENT_ID=${DISCORD_CLIENT_ID}
  - DISCORD_CLIENT_SECRET=${DISCORD_CLIENT_SECRET}
  # Two Discord bots
  - DISCORD_MAYOR_BOT_TOKEN=${DISCORD_MAYOR_BOT_TOKEN}
  - DISCORD_HARNESS_BOT_TOKEN=${DISCORD_HARNESS_BOT_TOKEN}
  - DISCORD_GUILD_ID=${DISCORD_GUILD_ID}
  # GitHub linking (optional)
  - GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
  - GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
```

**Two Discord bots** (create in Discord Developer Portal):
1. **Mayor bot** — AI persona. MESSAGE_CONTENT intent. Permissions: Send Messages, Read Message History, View Channels.
2. **Harness bot** — infrastructure. MESSAGE_CONTENT intent. Permissions: Manage Channels, Send Messages, Read Message History, View Channels, Manage Roles.

**Discord OAuth** — Create an OAuth2 application. Redirect URI: `{BASE_URL}/auth/discord/callback`. Scopes: `identify`, `guilds`.

#### 4. Base OpenClaw config
**File**: `harness/scripts/setup-openclaw.sh` (new)

Initializes `$OPENCLAW_HOME/openclaw.json` with the Discord adapter configured if `DISCORD_MAYOR_BOT_TOKEN` is set. Agents and bindings are added dynamically by the harness.

#### 5. Fix 2D template hooks
**Files**: `templates/2d/.claude/settings.json`, `templates/2d/.claude/hooks/on-stop.sh`, `on-tool-use.sh`, `on-notification.sh` (all new)

Copy the 3D hooks to the 2D template so the build pipeline works for 2D worlds.

### Success Criteria

#### Automated Verification:
- [ ] Docker image builds: `cd /Users/coreycole/cdev/creative-mode/harness && docker compose build`
- [ ] Container starts with OpenClaw gateway: `docker compose up` shows "Starting OpenClaw gateway..."
- [ ] Existing harness still works: `curl http://localhost:8080` returns lobby page
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] 2D template has `.claude/` directory with all hook scripts

#### Manual Verification:
- [ ] `just live` starts both harness and OpenClaw gateway
- [ ] World creation still works (no regression)
- [ ] OpenClaw logs show Discord adapter connected (if token configured)

---

## Phase 2: Discord OAuth + Mayor Personality + DB Schema

### Overview
Switch authentication from GitHub OAuth to Discord OAuth (primary login). Add GitHub as optional account linking. Add mayor personality fields to the world creation form with a **rich multi-step context-gathering flow**. Create all SQLite tables including instrumentation tables for the dashboard.

### Changes Required

#### 1. Database Migration — Discord auth + mayor + instrumentation tables
**File**: `harness/internal/db/migrations/004_discord_auth_and_mayor.sql` (new)

```sql
-- Discord OAuth as primary auth (replaces GitHub as login method)
-- SQLite doesn't support ALTER COLUMN to drop NOT NULL, so recreate users table.

CREATE TABLE users_new (
    id TEXT PRIMARY KEY,
    discord_id TEXT UNIQUE,
    discord_username TEXT,
    github_id INTEGER UNIQUE,        -- now nullable (optional linking)
    github_username TEXT,             -- now nullable
    avatar_url TEXT,
    role TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO users_new (id, github_id, github_username, avatar_url, role, created_at, last_seen_at)
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- Mayor identity + personality
ALTER TABLE worlds ADD COLUMN mayor_name TEXT NOT NULL DEFAULT 'Mayor';
ALTER TABLE worlds ADD COLUMN mayor_personality TEXT;
ALTER TABLE worlds ADD COLUMN mayor_tone TEXT;           -- e.g. "witty and encouraging"
ALTER TABLE worlds ADD COLUMN mayor_aesthetic TEXT;       -- e.g. "pixel art, warm palettes"
ALTER TABLE worlds ADD COLUMN mayor_lore TEXT;            -- backstory/origin
ALTER TABLE worlds ADD COLUMN mayor_examples TEXT;        -- example phrases, JSON array
ALTER TABLE worlds ADD COLUMN mayor_secret TEXT;
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

-- World channel access
CREATE TABLE world_invites (
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    invited_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (world_id, user_id)
);

-- === Instrumentation tables (dashboard reads from day one) ===

-- Mayor activity log — every significant mayor action
CREATE TABLE mayor_activity (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    activity_type TEXT NOT NULL,  -- 'message_received', 'message_sent', 'build_triggered',
                                 -- 'build_completed', 'build_failed', 'memory_updated',
                                 -- 'session_created', 'file_edited'
    detail TEXT,                  -- JSON blob with type-specific data
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_mayor_activity_world ON mayor_activity(world_id, created_at);
CREATE INDEX idx_mayor_activity_type ON mayor_activity(world_id, activity_type);

-- Mayor build delegations — links conversations to builds
CREATE TABLE mayor_builds (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT,
    prompt TEXT NOT NULL,           -- the build prompt composed by the mayor
    original_request TEXT,          -- the user's original message that led to the build
    status TEXT NOT NULL DEFAULT 'building',  -- 'building', 'completed', 'failed'
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_seconds INTEGER,
    error_message TEXT
);
CREATE INDEX idx_mayor_builds_world ON mayor_builds(world_id, started_at);

-- Mayor sessions — tracks OpenClaw sessions per agent
CREATE TABLE mayor_sessions (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    session_key TEXT NOT NULL,      -- OpenClaw session key
    label TEXT,                     -- session label from OpenClaw
    model TEXT,                     -- model used
    message_count INTEGER DEFAULT 0,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_mayor_sessions_world ON mayor_sessions(world_id, last_active_at);
```

#### 2. Register migration
**File**: `harness/internal/db/db.go`

Add `004_discord_auth_and_mayor.sql` to the `migrationFiles` slice and add bootstrap detection for the `discord_id` column.

#### 3. SQL Queries — Discord auth + mayor + instrumentation
**File**: `harness/internal/db/queries/users.sql` — Add Discord auth queries:
- `UpsertDiscordUser`, `GetUserByDiscordID`, `GetUserByID`, `LinkGitHub`, `UnlinkGitHub`

```sql
-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;
```

> **Note**: `GetUserByID` is needed by the world invite handler (Phase 3) to
> look up the invited user's Discord ID. If this query already exists in the
> codebase, skip adding it.

**File**: `harness/internal/db/queries/worlds.sql` — Update `CreateWorld` to accept mayor fields:
```sql
-- name: CreateWorld :exec
INSERT INTO worlds (id, name, description, created_by, template_type,
    mayor_name, mayor_personality, mayor_tone, mayor_aesthetic, mayor_lore, mayor_examples)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateWorldDiscordChannel :exec
UPDATE worlds SET discord_channel_id = ? WHERE id = ?;

-- name: UpdateWorldMayorSecret :exec
UPDATE worlds SET mayor_secret = ? WHERE id = ?;

-- name: GetWorldByMayorSecret :one
SELECT * FROM worlds WHERE mayor_secret = ?;

-- name: GetWorldsWithDiscordChannels :many
SELECT id, discord_channel_id FROM worlds WHERE discord_channel_id IS NOT NULL;
```

**File**: `harness/internal/db/queries/mayor_messages.sql` (new):
- `InsertMayorMessage`, `GetMayorMessages`, `GetMayorMessagesSince`, `GetMayorMessageByDiscordID`

**File**: `harness/internal/db/queries/world_invites.sql` (new):
- `InviteUserToWorld`, `GetWorldInvites`, `RemoveWorldInvite`, `IsUserInvitedToWorld`

**File**: `harness/internal/db/queries/mayor_activity.sql` (new):
```sql
-- name: InsertMayorActivity :exec
INSERT INTO mayor_activity (id, world_id, activity_type, detail, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetMayorActivity :many
SELECT * FROM mayor_activity WHERE world_id = ? ORDER BY created_at DESC LIMIT ?;

-- name: GetMayorActivityByType :many
SELECT * FROM mayor_activity WHERE world_id = ? AND activity_type = ? ORDER BY created_at DESC LIMIT ?;

-- name: GetMayorActivitySince :many
SELECT * FROM mayor_activity WHERE world_id = ? AND created_at > ? ORDER BY created_at ASC;
```

**File**: `harness/internal/db/queries/mayor_builds.sql` (new):
```sql
-- name: InsertMayorBuild :exec
INSERT INTO mayor_builds (id, world_id, checkpoint_id, prompt, original_request, status, started_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMayorBuildStatus :exec
UPDATE mayor_builds SET status = ?, completed_at = ?, duration_seconds = ?, error_message = ?
WHERE id = ?;

-- name: GetMayorBuilds :many
SELECT * FROM mayor_builds WHERE world_id = ? ORDER BY started_at DESC LIMIT ?;

-- name: GetMayorBuildByCheckpoint :one
SELECT * FROM mayor_builds WHERE world_id = ? AND checkpoint_id = ? LIMIT 1;
```

**File**: `harness/internal/db/queries/mayor_sessions.sql` (new):
```sql
-- name: UpsertMayorSession :exec
INSERT INTO mayor_sessions (id, world_id, session_key, label, model, message_count, first_seen_at, last_active_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    label = excluded.label,
    model = excluded.model,
    message_count = excluded.message_count,
    last_active_at = excluded.last_active_at;

-- name: GetMayorSessions :many
SELECT * FROM mayor_sessions WHERE world_id = ? ORDER BY last_active_at DESC LIMIT ?;
```

#### 4. sqlc config updates
**File**: `harness/sqlc.yaml`

Add column renames for all new fields:

```yaml
rename:
  discord_id: "DiscordID"
  discord_username: "DiscordUsername"
  mayor_name: "MayorName"
  mayor_personality: "MayorPersonality"
  mayor_tone: "MayorTone"
  mayor_aesthetic: "MayorAesthetic"
  mayor_lore: "MayorLore"
  mayor_examples: "MayorExamples"
  mayor_secret: "MayorSecret"
  discord_channel_id: "DiscordChannelID"
  discord_message_id: "DiscordMessageID"
  discord_thread_id: "DiscordThreadID"
  author_type: "AuthorType"
  author_name: "AuthorName"
  activity_type: "ActivityType"
  checkpoint_id: "CheckpointID"
  original_request: "OriginalRequest"
  error_message: "ErrorMessage"
  duration_seconds: "DurationSeconds"
  completed_at: "CompletedAt"
  started_at: "StartedAt"
  session_key: "SessionKey"
  last_active_at: "LastActiveAt"
  first_seen_at: "FirstSeenAt"
  message_count: "MessageCount"
```

#### 5. Discord OAuth — Replace GitHub as primary auth
**File**: `harness/internal/auth/auth.go`

Replace GitHub OAuth flow with Discord OAuth:
1. Redirect to `https://discord.com/oauth2/authorize?client_id={ID}&redirect_uri={URI}&response_type=code&scope=identify+guilds`
2. Callback receives code, exchange at `https://discord.com/api/oauth2/token`
3. Fetch user from `https://discord.com/api/users/@me`
4. Upsert user by `discord_id`, create session

GitHub kept as optional linking: `GET /auth/github/link`, `GET /auth/github/callback`, `POST /auth/github/unlink`.

**Routes**:
```go
// Discord OAuth (primary login):
GET  /auth/discord/login     → redirect to Discord authorize
GET  /auth/discord/callback  → exchange code, create session
POST /auth/logout            → clear session (unchanged)

// GitHub linking (authenticated users only):
GET  /auth/github/link       → redirect to GitHub authorize
GET  /auth/github/callback   → exchange code, update github_id/github_username
POST /auth/github/unlink     → clear github_id/github_username
```

**File**: `harness/internal/auth/auth.go` — `HandleDevLogin`:
Update to generate fake Discord IDs: `fakeDiscordID := fmt.Sprintf("dev-%d", fnv32a(username))`

**File**: `harness/internal/auth/middleware.go`:
Update redirect paths from `/auth/github/login` → `/auth/discord/login`.

#### 6. View template updates
All templates referencing `user.GitHubUsername` → `user.DiscordUsername.String`:
- `harness/views/login/login.templ` — "Sign in with Discord"
- `harness/views/lobby/lobby.templ` — header username, add "Link GitHub" button
- `harness/views/admin/admin.templ` — user list
- `harness/views/pending/pending.templ` — pending message
- `harness/internal/server/server.go` — chat event username
- `harness/internal/server/events.go` — player joined/left, chat history
- `harness/internal/db/queries/messages.sql` — JOIN on `discord_username`

#### 7. World Creation Form — Multi-Step Mayor Personality Gathering
**File**: `harness/views/lobby/lobby.templ`

Replace the simple create form with a multi-step wizard that gathers rich mayor personality context. Uses Datastar signals for step navigation — no separate pages, just conditional visibility.

```go
// Signals for the wizard
type CreateWorldSignals struct {
    Step             int    `json:"create_step"`              // 1=basics, 2=personality, 3=aesthetics, 4=review
    Name             string `json:"create_name"`
    Description      string `json:"create_description"`
    TemplateType     string `json:"create_template_type"`
    MayorName        string `json:"create_mayor_name"`
    MayorPersonality string `json:"create_mayor_personality"` // core personality description
    MayorTone        string `json:"create_mayor_tone"`        // communication tone
    MayorAesthetic   string `json:"create_mayor_aesthetic"`   // visual/design sensibility
    MayorLore        string `json:"create_mayor_lore"`        // backstory/origin story
    MayorExamples    string `json:"create_mayor_examples"`    // example phrases
}
```

**Step 1: World Basics**
- World name (required)
- World description (optional)
- Template type (3D / 2D Room)
- → Next

**Step 2: Meet Your Mayor**
- Mayor's name (required) — "What should your mayor be called?"
- Core personality (textarea, required) — "Describe your mayor's personality. What makes them unique? Are they enthusiastic, methodical, witty, poetic? What drives them?"
- Tone (select + custom) — "How should they communicate?"
  - Options: Enthusiastic & encouraging, Witty & playful, Calm & thoughtful, Bold & dramatic, Mysterious & cryptic, Professional & precise, Custom...
- → Back / Next

**Step 3: World Aesthetic & Lore**
- Aesthetic sensibility (textarea, optional) — "What's the visual identity of your world? Describe the art style, color palettes, textures, or artistic influences your mayor should champion."
  - Placeholder: "e.g., Warm pixel art with autumn palette, inspired by Stardew Valley and Studio Ghibli"
- Mayor's backstory (textarea, optional) — "Give your mayor an origin story. Where do they come from? Why do they care about this world?"
  - Placeholder: "e.g., Pixel was the first sprite rendered in this world, and they've watched every building and creature take shape since day one"
- Example phrases (textarea, optional) — "What are some things your mayor might say? These help establish their voice."
  - Placeholder: "e.g., 'Now THAT is what I call a proper castle turret!' or 'Hmm, I think the lighting needs more warmth here...'"
- → Back / Next

**Step 4: Review & Create**
- Summary card showing all entered info
- Preview of how the SOUL.md will look (rendered from the template)
- "Looks good!" / Back to edit

The form posts all fields to `POST /world/create` with `{contentType: 'form'}`. The handler passes all personality fields to `CreateWorld`.

```html
<!-- Step navigation via Datastar signals -->
<div data-signals='{"create_step": 1, "create_template_type": "3d", ...}'>

    <!-- Step 1: World Basics -->
    <div data-show="$create_step === 1">
        <!-- name, description, template_type inputs -->
        <button data-on:click="$create_step = 2">Next: Meet Your Mayor →</button>
    </div>

    <!-- Step 2: Mayor Personality -->
    <div data-show="$create_step === 2">
        <!-- mayor_name, mayor_personality, mayor_tone inputs -->
        <button data-on:click="$create_step = 1">← Back</button>
        <button data-on:click="$create_step = 3">Next: Aesthetic & Lore →</button>
    </div>

    <!-- Step 3: Aesthetic & Lore -->
    <div data-show="$create_step === 3">
        <!-- mayor_aesthetic, mayor_lore, mayor_examples inputs -->
        <button data-on:click="$create_step = 2">← Back</button>
        <button data-on:click="$create_step = 4">Review →</button>
    </div>

    <!-- Step 4: Review & Create -->
    <div data-show="$create_step === 4">
        <!-- Summary of all fields -->
        <button data-on:click="$create_step = 3">← Back</button>
        <button data-on:click="@post('/world/create', {contentType: 'form'})"
                data-indicator-fetching
                data-attr-disabled="$fetching">
            Create World
        </button>
    </div>
</div>
```

#### 8. Handler — Accept expanded mayor fields
**File**: `harness/internal/server/server.go`

```go
var req struct {
    Name             string `form:"name"`
    Description      string `form:"description"`
    TemplateType     string `form:"template_type"`
    MayorName        string `form:"mayor_name"`
    MayorPersonality string `form:"mayor_personality"`
    MayorTone        string `form:"mayor_tone"`
    MayorAesthetic   string `form:"mayor_aesthetic"`
    MayorLore        string `form:"mayor_lore"`
    MayorExamples    string `form:"mayor_examples"`
}
```

#### 9. WorldManager.CreateWorld — Accept and store expanded fields
**File**: `harness/internal/world/manager.go`

Extend signature to accept all personality fields. Pass to DB.

### Success Criteria

#### Automated Verification:
- [ ] Migration applies: harness starts without DB errors
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] All instrumentation tables exist in SQLite

#### Manual Verification:
- [ ] Discord OAuth login works end-to-end
- [ ] GitHub linking works for authenticated users
- [ ] Multi-step world creation wizard navigates between all 4 steps
- [ ] Review step shows summary of all entered personality info
- [ ] Creating a world stores all mayor personality fields in DB
- [ ] Existing worlds still load (defaults applied)

---

## Phase 3: Mayor Agent Provisioning + Discord Channel

### Overview
When a world is created, auto-provision an OpenClaw agent with its own workspace. Generate rich workspace files (`SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `USER.md`, skills) from the expanded personality context gathered in Phase 2. Create a Discord channel for the world and bind the mayor to it.

### Changes Required

#### 1. Mayor package — Agent provisioning
**File**: `harness/internal/mayor/mayor.go` (new)

```go
type Manager struct {
    openclawHome      string
    openclawBin       string
    harnessURL        string
    harnessBotToken   string
    mayorBotToken     string
    mayorBotID        string   // resolved at startup via Discord API
    harnessBotID      string   // resolved at startup via Discord API
    discordGuildID    string
    logger            *slog.Logger
}

func NewManager(...) (*Manager, error)   // resolves bot user IDs at startup
func (m *Manager) ProvisionAgent(worldID, worldName string, personality MayorPersonality, templateType, creatorDiscordID string) (discordChannelID, mayorSecret string, err error)
func (m *Manager) IsGatewayHealthy() bool
```

`MayorPersonality` struct carries all gathered context:

```go
type MayorPersonality struct {
    Name        string // required
    Personality string // core personality description (required)
    Tone        string // communication tone
    Aesthetic   string // visual/design sensibility
    Lore        string // backstory/origin story
    Examples    string // example phrases
}
```

`ProvisionAgent` flow:
1. Generate 32-byte hex mayor secret
2. `openclaw agents add` via CLI — creates agent, scaffolds workspace
3. Create skill directories (`skills/world-build/`, `skills/world-status/`)
4. Write workspace files from templates using full `MayorPersonality`
5. Create private Discord channel with `permission_overwrites`
6. Bind agent to channel via `openclaw config set bindings` (read-modify-write)
7. Log `session_created` to `mayor_activity`

#### 2. Rich SOUL.md generation
**File**: `harness/internal/mayor/soul.go` (new)

The SOUL.md incorporates ALL personality context gathered from the user, giving the mayor maximum personality from day one. OpenClaw will autonomously maintain and evolve this file over time.

```go
const soulTemplate = `# Soul

You are **{{.Name}}**, the mayor of **{{.WorldName}}**.

## Your Personality
{{.Personality}}

{{if .Tone}}
## Your Voice
Your communication style is: {{.Tone}}
{{end}}

{{if .Aesthetic}}
## Your Aesthetic Sensibility
You champion this visual identity for your world:
{{.Aesthetic}}

When evaluating builds, suggesting changes, or composing build prompts, reference
this aesthetic. It's your north star for what this world should look and feel like.
{{end}}

{{if .Lore}}
## Your Origin Story
{{.Lore}}
{{end}}

{{if .Examples}}
## Things You Might Say
These phrases capture your voice:
{{.Examples}}

Use these as inspiration for your communication style, not as scripts to repeat.
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
- Use your unique voice — you're not a generic assistant, you're {{.Name}}
`
```

#### 3. AGENTS.md generation
**File**: `harness/internal/mayor/agents.go` (new)

Structured workflow template with world-specific context. Includes aesthetic awareness in the build step:

```go
const agentsTemplate = `# {{.Name}} — Mayor of {{.WorldName}}

## Role
You are the mayor of this {{.TemplateType}} game world. You orchestrate all modifications
through conversation with its builders.

## Workflow: When a user asks you to build something

### Step 1: Understand
- Read the request carefully
- Check your memory for relevant context from past builds
- If vague or ambiguous, ask clarifying questions

### Step 2: Plan
- Describe what you intend to change, in plain language
- Consider the world's aesthetic identity when composing your plan
{{if .Aesthetic}}- Reference the visual style: {{.Aesthetic}}{{end}}
- If significant, ask for confirmation before proceeding

### Step 3: Build
- Use the world-build skill to submit a detailed build prompt
- The prompt should be specific and self-contained — include context
- Incorporate aesthetic guidance so the builder maintains the world's visual identity
- Monitor build progress and report back

### Step 4: Verify
- When you see [BUILD COMPLETE], verify the world before saving a checkpoint
- Run playwright-cli console error to check for JavaScript/WASM errors
- Run playwright-cli screenshot to visually inspect the world for regressions
- Look for: broken rendering, missing elements, layout issues, error overlays
- If verification passes (no console errors, screenshot looks correct), proceed to save
- If verification fails, DO NOT save — instead diagnose the issue and either:
  - Submit a fix build to address the problem
  - Roll back to the previous checkpoint and inform the user

### Step 5: Save Checkpoint
- Only save a checkpoint when the world is in a stable, working state
- DO NOT save after every single build — accumulate changes and verify first
- A checkpoint is a curated snapshot that users can fork from or revert to
- When saving, include a clear description of what changed since the last checkpoint
- Update your memory with what was built and the checkpoint ID

### Step 6: Report
- Summarize build results to the user
- If a checkpoint was saved, mention it — "Saved checkpoint: [description]"
- If the build failed or verification failed, explain what went wrong and next steps

## Checkpoint Save Philosophy
- Checkpoints are valuable — they represent known-good states of the world
- Never spam checkpoints. Batch related changes into a single verified checkpoint
- A good checkpoint cadence: save after a coherent feature is complete and verified
- Examples of when to save: new room added and working, visual overhaul complete,
  bug fix verified, major gameplay change tested
- Examples of when NOT to save: mid-refactor, build just completed but untested,
  quick iteration where you plan to keep building

## Verification Tools
- playwright-cli console error — checks browser console for JS/WASM errors
- playwright-cli screenshot — captures the current world state as a PNG
- Use these EVERY time before saving a checkpoint, no exceptions
- If the world URL is not yet open, use playwright-cli open to navigate first

## Workflow: When a user asks a question
- Answer from your knowledge of the world
- Reference past builds and decisions

## Workflow: When a user gives feedback
- Acknowledge the feedback
- Store it in memory for future reference
- If it implies a change, offer to build it

## Workflow: When you receive a build event
- Messages with [BUILD COMPLETE] or [BUILD FAILED] prefix come from the harness
- Verify with playwright-cli before saving (see Step 4)
- Summarize results for the user
- If failed, analyze and suggest fixes

## Knowledge: General vs World-Specific

You will learn things while building. Some learnings apply ONLY to your world. Others
apply to ALL creative mode worlds. Knowing the difference matters — general learnings
should be contributed back so every mayor benefits.

### World-Specific Knowledge (save to your MEMORY.md)
- Your world's aesthetic, lore, design decisions
- Player preferences and feedback
- What features exist in your world and how they work
- Layout, room structure, entity positions
- Anything that only makes sense in YOUR world's context

Examples:
- "The tavern has a fireplace entity at (400, 200) with warm orange lighting"
- "Player prefers isometric camera angle for this world"
- "We decided to use pixel art style with 16x16 sprites"

### General Creative Mode Knowledge (contribute as a PR)
- Build gotchas that affect all worlds (Bevy patterns, Trunk issues, WASM quirks)
- Common pitfalls and their fixes
- New Bevy 0.18 patterns you discovered that should be documented
- Corrections to existing CLAUDE.md documentation that was wrong or incomplete
- Better ways to structure components, resources, or systems
- WASM-specific workarounds that every world needs to know

Examples:
- "Bevy 0.18 requires Reflect + register_type for debug queries — existing docs were missing this"
- "wasm-bindgen 0.2.108 panics if you use X pattern — use Y instead"
- "Trunk silently fails if Cargo.toml has feature Z — add this to Common Build Issues"
- "Lightyear rollback breaks if component doesn't implement PartialEq"

### How to Contribute General Learnings
When you discover something ALL worlds should know:
1. Identify the target file:
   - templates/3d/CLAUDE.md — 3D game development (Bevy + Lightyear)
   - templates/2d/CLAUDE.md — 2D game development (Bevy WASM rooms)
   - harness/CLAUDE.md — harness server patterns
   - CLAUDE.md (root) — general project knowledge
2. Use the contribute-learning skill to submit a PR
3. Keep contributions focused — one learning per PR
4. Write clearly: what the issue is, why it matters, how to handle it
5. Share the PR link in Discord so the team can review

DO NOT contribute world-specific details as PRs. If you're unsure, it's world-specific.
DO contribute anything that would have saved you time if you'd known it earlier.

## Important
- NEVER skip clarification for ambiguous requests
- NEVER save a checkpoint without verifying with playwright-cli first
- ALWAYS compose a detailed prompt for world-build — don't just forward the user's message
- ALWAYS contribute general learnings as PRs — don't hoard knowledge in your MEMORY.md
- You speak as {{.Name}}, not as "an AI assistant"
- Your world is a {{.TemplateType}} world — be aware of what's possible
`
```

#### 4. IDENTITY.md, USER.md, skill definitions
**File**: `harness/internal/mayor/identity.go` (new)
**File**: `harness/internal/mayor/user.go` (new)
**File**: `harness/internal/mayor/skills.go` (new)

Same as original plan — `IDENTITY.md` (name + role), `USER.md` (world context), `world-build` skill (curl to `/api/mayor/build`), `world-status` skill (curl to `/api/mayor/status`), `contribute-learning` skill (curl to `/api/mayor/contribute-learning`).

#### 5. Discord channel creation + invite management
**File**: `harness/internal/mayor/discord.go` (new)

- `createPrivateDiscordChannel(worldName, creatorDiscordID) (channelID, error)` — creates private text channel, denies @everyone, allows both bots + creator
- `InviteUserToChannel(channelID, userDiscordID) error` — adds permission overwrite
- `RevokeUserFromChannel(channelID, userDiscordID) error` — removes permission overwrite
- `postToDiscord(channelID, content) error`
- `PostToDiscordAndMirror(db, worldID, channelID, content, authorType, authorName) error`
- `sanitizeChannelName(name) string`

#### 6. OpenClaw CLI integration
**File**: `harness/internal/mayor/openclaw.go` (new)

- `createAgentViaCLI(agentID, workspaceDir) error` — `openclaw agents add --non-interactive --json --workspace`
- `bindAgentToDiscord(agentID, discordChannelID) error` — read-modify-write on bindings via `openclaw config get/set`
- `deleteAgent(agentID) error` — `openclaw agents delete --force`

#### 7. Hook into CreateWorld
**File**: `harness/internal/world/manager.go`

After DB transaction succeeds, construct `MayorPersonality` from the expanded form fields and call `ProvisionAgent`. Store `mayor_secret` and `discord_channel_id` in DB. Register channel with Discord listener.

```go
if m.mayorManager != nil {
    personality := mayor.MayorPersonality{
        Name:        mayorName,
        Personality: mayorPersonality,
        Tone:        mayorTone,
        Aesthetic:   mayorAesthetic,
        Lore:        mayorLore,
        Examples:    mayorExamples,
    }
    discordChannelID, mayorSecret, err := m.mayorManager.ProvisionAgent(
        worldID, name, personality, templateType, creatorDiscordID,
    )
    if err != nil {
        m.logger.Error("failed to provision mayor", "worldID", worldID, "error", err)
        // Non-fatal — world still works without mayor
    }
    if mayorSecret != "" {
        _ = m.queries.UpdateWorldMayorSecret(ctx, sqlc.UpdateWorldMayorSecretParams{
            MayorSecret: sql.NullString{String: mayorSecret, Valid: true},
            ID:          worldID,
        })
    }
    if discordChannelID != "" {
        _ = m.queries.UpdateWorldDiscordChannel(ctx, sqlc.UpdateWorldDiscordChannelParams{
            DiscordChannelID: sql.NullString{String: discordChannelID, Valid: true},
            ID:               worldID,
        })
        if m.discordListener != nil {
            m.discordListener.RegisterChannel(discordChannelID, worldID)
        }
    }
}
```

Update `CreateWorld` signature to accept all personality fields:

```go
func (m *Manager) CreateWorld(
    ctx context.Context,
    name, description, userID, creatorDiscordID, templateType string,
    mayorName, mayorPersonality, mayorTone, mayorAesthetic, mayorLore, mayorExamples string,
) (*sqlc.World, error) {
```

Add `mayorManager *mayor.Manager` and `discordListener *discord.Listener` to the `Manager` struct.

#### 8. World invite API
**File**: `harness/internal/server/world_invite.go` (new)

- `POST /world/:worldID/invite` — adds user to Discord channel + DB
- `POST /world/:worldID/revoke` — removes from Discord channel + DB

**Route registration** in `harness/internal/server/server.go`:
```go
// World invite management (session auth — world creator only)
approved.POST("/world/:worldID/invite", s.handleWorldInvite)
approved.POST("/world/:worldID/revoke", s.handleWorldRevoke)
```

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] Creating a world creates `data/openclaw/workspaces/{worldID}/` with all workspace files
- [ ] SOUL.md contains ALL personality fields (tone, aesthetic, lore, examples)

#### Manual Verification:
- [ ] Create world with rich personality → SOUL.md is deeply personalized
- [ ] AGENTS.md references world name, template type, and aesthetic
- [ ] Discord channel created as private, only creator + bots can see it
- [ ] Send a message in Discord → mayor responds with the configured personality
- [ ] Invite another user → they can see and post in the channel
- [ ] `openclaw agents list --json` shows the new agent

---

## Phase 4: Build Pipeline + Instrumentation

### Overview
Connect the existing build pipeline to Discord. Create the mayor build API. Add full SQLite instrumentation at every touchpoint — every message, build, and action logged to the instrumentation tables from Phase 2.

### Changes Required

#### 1. Mayor auth middleware
**File**: `harness/internal/server/server.go`

```go
// mayorAuthMiddleware validates X-Mayor-Secret header against per-world secrets.
func (s *Server) mayorAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        secret := c.Request().Header.Get("X-Mayor-Secret")
        if secret == "" {
            return echo.NewHTTPError(http.StatusUnauthorized, "missing X-Mayor-Secret")
        }
        world, err := s.DB.GetWorldByMayorSecret(c.Request().Context(), sql.NullString{String: secret, Valid: true})
        if err != nil {
            return echo.NewHTTPError(http.StatusUnauthorized, "invalid secret")
        }
        c.Set("mayor_world", &world)
        return next(c)
    }
}
```

#### 2. Mayor build handler
**File**: `harness/internal/server/mayor_api.go` (new)

```go
func (s *Server) handleMayorBuild(c echo.Context) error {
    var req struct {
        WorldID string `json:"world_id"`
        Prompt  string `json:"prompt"`
    }
    // ... validate, get latest checkpoint ...

    // Log to mayor_builds
    buildID := uuid.NewString()
    _ = s.DB.InsertMayorBuild(ctx, sqlc.InsertMayorBuildParams{
        ID:              buildID,
        WorldID:         req.WorldID,
        CheckpointID:    sql.NullString{String: cpID, Valid: true},
        Prompt:          req.Prompt,
        OriginalRequest: sql.NullString{}, // populated if we have the original user message
        Status:          "building",
        StartedAt:       time.Now(),
    })

    // Log to mayor_activity
    _ = s.DB.InsertMayorActivity(ctx, sqlc.InsertMayorActivityParams{
        ID:           uuid.NewString(),
        WorldID:      req.WorldID,
        ActivityType: "build_triggered",
        Detail:       sql.NullString{String: fmt.Sprintf(`{"build_id":"%s","prompt":"%s"}`, buildID, req.Prompt[:min(100, len(req.Prompt))]), Valid: true},
        CreatedAt:    time.Now(),
    })

    // Delegate to existing orchestrator
    cp, err := s.Orchestrator.HandlePrompt(ctx, req.WorldID, cpID, req.Prompt, userID)
    // ...
}
```

#### 3. Mayor status handler
**File**: `harness/internal/server/mayor_api.go`

```go
func (s *Server) handleMayorStatus(c echo.Context) error {
    worldID := c.QueryParam("world_id")
    // Return: current checkpoint, build status, recent builds, server info
}
```

#### 4. Build events → Discord + instrumentation
**File**: `harness/internal/claude/claude.go`

After build completes/fails, post to Discord AND log to instrumentation tables:

```go
if world.DiscordChannelID.Valid && o.mayorManager != nil {
    var msg string
    if buildErr == nil {
        msg = fmt.Sprintf("[BUILD COMPLETE] Checkpoint `%s` deployed. Changes: %s", cpID, workSummary)
    } else {
        msg = fmt.Sprintf("[BUILD FAILED] Checkpoint `%s`: %s", cpID, buildErr.Error())
    }
    go o.mayorManager.PostToDiscordAndMirror(o.db, worldID, world.DiscordChannelID.String, msg, "system", "Harness")
}

// Log to mayor_builds
status := "completed"
var errMsg string
if buildErr != nil {
    status = "failed"
    errMsg = buildErr.Error()
}
_ = o.db.UpdateMayorBuildStatus(ctx, sqlc.UpdateMayorBuildStatusParams{
    Status:          status,
    CompletedAt:     sql.NullTime{Time: time.Now(), Valid: true},
    DurationSeconds: sql.NullInt64{Int64: int64(time.Since(startTime).Seconds()), Valid: true},
    ErrorMessage:    sql.NullString{String: errMsg, Valid: errMsg != ""},
    ID:              buildID,
})

// Log to mayor_activity
_ = o.db.InsertMayorActivity(ctx, sqlc.InsertMayorActivityParams{
    ID:           uuid.NewString(),
    WorldID:      worldID,
    ActivityType: "build_" + status,
    Detail:       sql.NullString{String: fmt.Sprintf(`{"checkpoint_id":"%s","duration_s":%d}`, cpID, duration), Valid: true},
    CreatedAt:    time.Now(),
})
```

#### 5. Contribute-learning handler
**File**: `harness/internal/server/mayor_api.go`

Mayors can submit general learnings as PRs to the creative-mode repo. The harness
creates the branch, applies the edit, and opens a PR via the GitHub API.

```go
func (s *Server) handleContributeLearning(c echo.Context) error {
    world := c.Get("mayor_world").(*sqlc.World)

    var req struct {
        TargetFile  string `json:"target_file"`  // e.g. "templates/2d/CLAUDE.md"
        Section     string `json:"section"`       // e.g. "Common Build Issues"
        Learning    string `json:"learning"`      // the content to add
        Description string `json:"description"`   // PR title/description
    }
    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
    }

    // Validate target file is in the allowlist.
    allowedTargets := map[string]bool{
        "templates/3d/CLAUDE.md": true,
        "templates/2d/CLAUDE.md": true,
        "harness/CLAUDE.md":      true,
        "CLAUDE.md":              true,
    }
    if !allowedTargets[req.TargetFile] {
        return echo.NewHTTPError(http.StatusBadRequest,
            "target_file must be one of: templates/3d/CLAUDE.md, templates/2d/CLAUDE.md, harness/CLAUDE.md, CLAUDE.md")
    }

    // Create branch, apply edit, push, create PR.
    prURL, err := s.MayorManager.CreateLearningPR(
        world.MayorName.String,
        world.Name,
        req.TargetFile,
        req.Section,
        req.Learning,
        req.Description,
    )
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to create PR: "+err.Error())
    }

    // Log to mayor_activity.
    _ = s.DB.InsertMayorActivity(ctx, sqlc.InsertMayorActivityParams{
        ID:           uuid.NewString(),
        WorldID:      world.ID,
        ActivityType: "learning_contributed",
        Detail:       sql.NullString{String: fmt.Sprintf(`{"pr_url":"%s","target":"%s"}`, prURL, req.TargetFile), Valid: true},
        CreatedAt:    time.Now(),
    })

    return c.JSON(http.StatusOK, map[string]string{"pr_url": prURL})
}
```

#### 6. Learning PR creation logic
**File**: `harness/internal/mayor/learning.go` (new)

Creates a PR on `https://github.com/coreycole/creative-mode` using the GitHub API.
The harness needs a `GITHUB_TOKEN` env var with repo push access.

```go
func (m *Manager) CreateLearningPR(
    mayorName, worldName, targetFile, section, learning, description string,
) (prURL string, err error) {
    // 1. Read the target file from the repo working tree.
    content, err := os.ReadFile(filepath.Join(m.repoRoot, targetFile))
    if err != nil {
        return "", fmt.Errorf("reading %s: %w", targetFile, err)
    }

    // 2. Apply the learning.
    //    If section is specified, find the section header and append after it.
    //    If not, append to the end of the file.
    newContent := applyLearning(string(content), section, learning)

    // 3. Create branch via GitHub API.
    branchName := fmt.Sprintf("mayor/%s/%s", sanitizeBranchName(mayorName),
        time.Now().Format("2006-01-02-150405"))

    // 4. Create commit on branch with the modified file.
    // 5. Create PR.
    //    Title: "[Mayor: {mayorName}] {description}"
    //    Body: "## Learning from {mayorName} (Mayor of {worldName})\n\n{learning}\n\n---\nTarget: `{targetFile}`\nSection: {section}"
    //    Base: main
    //    Head: branchName

    return prURL, nil
}
```

The `applyLearning` function intelligently inserts the learning:
- If `section` matches an existing `##` or `###` header, append the learning below that section
- If no match, append a new subsection at the end of the file
- Always add a blank line before the learning for clean formatting

**Rate limiting**: One PR per mayor per hour to prevent spam. Tracked in `mayor_activity`.

#### 7. Contribute-learning skill definition
**File**: `harness/internal/mayor/skills.go`

Added alongside `world-build` and `world-status`:

```go
const contributeLearningSkill = `---
name: contribute-learning
description: >
  Submit a general Creative Mode learning as a pull request to the platform repo.
  Use when you discover a build pattern, gotcha, or fix that ALL worlds should know.
  Do NOT use for world-specific knowledge — save that to MEMORY.md instead.
---

# Contribute Learning

Submit a general learning that benefits all Creative Mode mayors and worlds.

## When to Use

Use this skill when you discover something that:
- Applies to ALL {{.TemplateType}} worlds, not just yours
- Would have saved you time if you'd known it earlier
- Corrects or improves existing documentation
- Documents a Bevy/Trunk/WASM pattern or gotcha

Do NOT use for world-specific details (aesthetics, layout, player preferences).

## How to Use

curl -s -X POST {{.HarnessURL}}/api/mayor/contribute-learning \
  -H "X-Mayor-Secret: {{.MayorSecret}}" \
  -H "Content-Type: application/json" \
  -d '{
    "target_file": "TARGET",
    "section": "SECTION_HEADER",
    "learning": "CONTENT",
    "description": "SHORT_DESCRIPTION"
  }'

## Target Files

| File | What belongs here |
|------|------------------|
| templates/3d/CLAUDE.md | 3D world development: Bevy + Lightyear, server-authoritative patterns, replication |
| templates/2d/CLAUDE.md | 2D world development: Bevy WASM, room system, data-driven content |
| harness/CLAUDE.md | Harness server patterns: Datastar, SSE, templ, DB queries |
| CLAUDE.md | General project: Docker, build system, debugging tools |

## Response

Returns {"pr_url": "https://github.com/coreycole/creative-mode/pull/123"}

Share the PR URL in Discord so the team can review and merge it.

## Examples

### Build gotcha
target_file: "templates/2d/CLAUDE.md"
section: "Common Build Issues"
learning: "| Bevy panics on startup | Missing default_font feature | Add default_font to Bevy features in Cargo.toml |"
description: "Document missing default_font panic"

### New pattern
target_file: "templates/3d/CLAUDE.md"
section: "Key Patterns"
learning: "- When adding physics colliders, always set the CollisionGroups on both client and server. Mismatched groups cause ghost collisions where the server resolves differently than the client prediction."
description: "Document CollisionGroups requirement for physics"
`
```

#### 8. Route registration
**File**: `harness/internal/server/server.go`

```go
// Mayor API — X-Mayor-Secret auth (NOT session cookies)
mayor := e.Group("/api/mayor")
mayor.Use(s.mayorAuthMiddleware)
mayor.POST("/build", s.handleMayorBuild)
mayor.GET("/status", s.handleMayorStatus)
mayor.POST("/contribute-learning", s.handleContributeLearning)
```

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] `curl -X POST localhost:8080/api/mayor/build -H "X-Mayor-Secret: ..." -d '{"world_id":"...","prompt":"..."}' ` returns 200
- [ ] `curl -X POST localhost:8080/api/mayor/contribute-learning -H "X-Mayor-Secret: ..." -d '{"target_file":"templates/2d/CLAUDE.md","section":"Common Build Issues","learning":"...","description":"..."}' ` returns 200 with `pr_url`

#### Manual Verification:
- [ ] Mayor build API triggers the full pipeline
- [ ] Build completion appears in Discord with `[BUILD COMPLETE]` prefix
- [ ] Mayor responds in Discord summarizing results
- [ ] `mayor_builds` table has a row with status, duration, checkpoint_id
- [ ] `mayor_activity` table has `build_triggered` and `build_completed` entries
- [ ] Contribute-learning creates a real PR on `https://github.com/coreycole/creative-mode`
- [ ] PR title has `[Mayor: {name}]` prefix
- [ ] PR targets the correct file and section
- [ ] `mayor_activity` has `learning_contributed` entry with PR URL
- [ ] Rate limit: second PR within 1 hour is rejected

---

## Phase 5: Discord Listener + Chat + Prompt Routing

### Overview
Add a `discordgo` listener that mirrors all Discord messages to SQLite. Render the conversation in the world overlay. Route browser prompts through Discord. Instrument message events to `mayor_activity`.

### Changes Required

#### 1. Discord listener package
**File**: `harness/internal/discord/listener.go` (new)

`discordgo`-based Gateway WebSocket listener. Watches all world channels, mirrors to SQLite, pushes SSE events. Classifies author types (user/mayor/system) and deduplicates against harness-sent messages.

```go
type Listener struct {
    session    *discordgo.Session
    db         *sqlc.Queries
    eventBus   EventPublisher
    mu         sync.RWMutex
    channelMap map[string]string  // discord_channel_id → world_id
    logger     *slog.Logger
}

func NewListener(token string, db, eventBus, logger) (*Listener, error)
func (l *Listener) Start() error     // loads channel map from DB, opens Gateway
func (l *Listener) Stop() error
func (l *Listener) RegisterChannel(discordChannelID, worldID string)
```

`handleMessage` flow:
1. Look up `worldID` from `channelMap`
2. Deduplicate via `GetMayorMessageByDiscordID`
3. Classify author type (user/mayor/system based on bot flag + content prefix)
4. Insert into `mayor_messages`
5. **Log to `mayor_activity`**: `message_received` for user messages, `message_sent` for mayor messages
6. Push SSE event via `eventBus.PublishWorld(worldID, ...)`

#### 2. Wire listener into harness startup
**File**: `harness/main.go`

Start Discord listener if `DISCORD_HARNESS_BOT_TOKEN` is set. Make available to server and mayor manager.

#### 3. Chat component — render mayor_messages
**File**: `harness/views/world/chat.templ` (new)

`MayorChat(messages, mayorName)` — renders conversation with distinct styles for user/mayor/system messages. Updated via SSE append on new messages.

#### 4. Add chat to world overlay
**File**: `harness/views/world/overlay.templ`

Include mayor chat component. SSE handler for `mayor.message` events appends new messages via `PatchElementTempl` with `WithModeAppend` targeting `#mayor-chat`.

#### 5. Update prompt handler to route through Discord
**File**: `harness/internal/server/server.go`

`handlePrompt`: If world has Discord channel + mayor + healthy gateway → post to Discord via `PostToDiscordAndMirror`. Log `message_received` to `mayor_activity`. Clear prompt input. Fall back to direct pipeline if gateway unhealthy.

#### 6. Invite controls
**File**: `harness/views/world/chat.templ`

World creator sees "Manage Access" section with invite/revoke controls.

#### 7. Go dependency
Add `github.com/bwmarrin/discordgo` to `go.mod`.

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`

#### Manual Verification:
- [ ] Open world in browser → see conversation history from Discord
- [ ] Submit prompt from browser → message appears in Discord channel
- [ ] Mayor responds in Discord → response appears in browser chat
- [ ] Build events appear in both Discord and browser chat
- [ ] `mayor_activity` has `message_received` and `message_sent` entries for every message
- [ ] Multiple browser clients see messages in real time via SSE
- [ ] Fallback: without Discord, direct pipeline still works

---

## Phase 6: Mayor Dashboard

### Overview
Create a dedicated `/mayor/:worldID` page with full-width layout providing comprehensive mayor observability. Four sections: Memory Inspector, Task/Session Tracking, Build History, and Activity Timeline. OpenClaw integration via CLI wrappers.

### Changes Required

#### 1. OpenClaw CLI wrapper package
**File**: `harness/internal/mayor/openclaw_query.go` (new)

CLI wrappers for querying OpenClaw state. All commands use `exec.Command` with `OPENCLAW_HOME` env var and parse JSON output.

```go
// GatewayStatus runs `openclaw status --json` and returns parsed health info.
type GatewayHealth struct {
    Status     string `json:"status"`    // "ok", "error"
    Uptime     string `json:"uptime"`
    AgentCount int    `json:"agentCount"`
    Channels   []struct {
        Name   string `json:"name"`
        Status string `json:"status"`
    } `json:"channels"`
}
func (m *Manager) GatewayStatus() (*GatewayHealth, error)

// ListSessions runs `openclaw sessions list --agent-id world-{worldID} --json`
type SessionInfo struct {
    Key          string `json:"key"`
    Label        string `json:"label"`
    Model        string `json:"model"`
    MessageCount int    `json:"messageCount"`
    LastActive   string `json:"lastActive"`
}
func (m *Manager) ListSessions(worldID string) ([]SessionInfo, error)

// SessionPreview runs `openclaw sessions preview --key {key} --json`
type SessionPreviewItem struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
func (m *Manager) SessionPreview(sessionKey string) ([]SessionPreviewItem, error)

// AgentStatus runs `openclaw agents list --json` and filters by agent ID
type AgentInfo struct {
    ID        string   `json:"agentId"`
    Name      string   `json:"name"`
    Workspace string   `json:"workspace"`
    Files     []string `json:"files"`
}
func (m *Manager) AgentStatus(worldID string) (*AgentInfo, error)
```

#### 2. Dashboard page and route
**File**: `harness/views/mayor/dashboard.templ` (new)

Full-width dedicated page at `/mayor/:worldID`. Uses Datastar signals for tab navigation within the dashboard.

```go
templ DashboardPage(world *sqlc.World, mayorName string) {
    <!DOCTYPE html>
    <html>
    <head>
        <title>{ mayorName } — Mayor Dashboard</title>
        <link rel="stylesheet" href="/static/styles.css"/>
        <script type="module" src="https://cdn.jsdelivr.net/npm/@starfederation/datastar@1.0.0-rc.6/bundles/datastar.js"></script>
    </head>
    <body class="bg-background text-foreground min-h-screen">
        <div class="max-w-7xl mx-auto px-4 py-6"
             data-signals={ templ.JSONString(DashboardSignals{
                 ActiveSection: "memory",
                 CurrentWorldID: world.ID,
             }) }
             data-init={ datastar.GetSSE(fmt.Sprintf("/mayor/%s/events", world.ID)) }>

            <!-- Header -->
            <div class="flex items-center justify-between mb-6">
                <div>
                    <h1 class="text-2xl font-bold">{ mayorName }</h1>
                    <p class="text-sm text-muted-foreground">Mayor of { world.Name }</p>
                </div>
                <div id="gateway-status" class="text-xs">
                    <!-- patched by SSE with gateway health -->
                </div>
                <a href={ templ.SafeURL(fmt.Sprintf("/world/%s", world.ID)) }
                   class="text-sm text-muted-foreground hover:text-foreground">
                    ← Back to World
                </a>
            </div>

            <!-- Section tabs -->
            <div class="flex gap-1 border-b border-border mb-4">
                <button data-on:click="$active_section = 'memory'"
                        data-class="{'border-b-2 border-primary text-foreground': $active_section === 'memory', 'text-muted-foreground': $active_section !== 'memory'}"
                        class="px-4 py-2 text-sm">Memory</button>
                <button data-on:click="$active_section = 'sessions'"
                        data-class="{'border-b-2 border-primary text-foreground': $active_section === 'sessions', 'text-muted-foreground': $active_section !== 'sessions'}"
                        class="px-4 py-2 text-sm">Sessions</button>
                <button data-on:click="$active_section = 'builds'"
                        data-class="{'border-b-2 border-primary text-foreground': $active_section === 'builds', 'text-muted-foreground': $active_section !== 'builds'}"
                        class="px-4 py-2 text-sm">Builds</button>
                <button data-on:click="$active_section = 'activity'"
                        data-class="{'border-b-2 border-primary text-foreground': $active_section === 'activity', 'text-muted-foreground': $active_section !== 'activity'}"
                        class="px-4 py-2 text-sm">Activity</button>
            </div>

            <!-- Section content -->
            @MemorySection()
            @SessionsSection()
            @BuildsSection()
            @ActivitySection()
        </div>
    </body>
    </html>
}
```

**Dashboard signals:**
```go
type DashboardSignals struct {
    ActiveSection    string `json:"active_section"`
    CurrentWorldID   string `json:"current_world_id"`
    MayorFileContent string `json:"mayor_file_content"` // for memory editor
}
```

#### 3. Memory Inspector section
**File**: `harness/views/mayor/memory.templ` (new)

Browse and edit workspace files. Bootstrap files (SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md) are editable. Skill files are read-only.

```go
templ MemorySection() {
    <div data-show="$active_section === 'memory'" style="display: none;">
        <div class="grid grid-cols-3 gap-4">
            <!-- File list (1/3 width) -->
            <div id="mayor-files-list" class="col-span-1 border rounded-md p-3">
                <!-- Patched by SSE with file list -->
            </div>
            <!-- File editor (2/3 width) -->
            <div id="mayor-file-editor" class="col-span-2 border rounded-md p-3">
                <!-- Patched by SSE with file content -->
            </div>
        </div>
    </div>
}

templ MayorFileList(files []MayorFileInfo) {
    <div id="mayor-files-list" class="space-y-1">
        <h3 class="text-sm font-medium mb-2">Workspace Files</h3>
        for _, f := range files {
            <button
                class="w-full text-left px-2 py-1.5 rounded text-sm hover:bg-muted/50 flex justify-between items-center"
                data-on:click={ datastar.GetSSE(fmt.Sprintf("/mayor/%s/file?name=%s", f.WorldID, f.Name)) }>
                <span>{ f.Name }</span>
                <span class="text-xs text-muted-foreground">
                    if f.Exists {
                        { f.Size }
                    } else {
                        (not yet created)
                    }
                </span>
            </button>
        }
    </div>
}

templ MayorFileEditor(filename string, editable bool, worldID string) {
    <div id="mayor-file-editor" class="space-y-2">
        <div class="flex items-center justify-between">
            <h3 class="text-sm font-medium">{ filename }</h3>
            if !editable {
                <span class="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded">read-only</span>
            }
        </div>
        <textarea
            class="w-full min-h-[400px] rounded border border-border bg-background px-3 py-2 text-sm font-mono resize-y focus:outline-none focus:ring-1 focus:ring-ring"
            data-bind:mayor_file_content
            if !editable {
                disabled
            }
        ></textarea>
        if editable {
            <div class="flex items-center gap-2">
                <button
                    data-on:click={ datastar.PutSSE(fmt.Sprintf("/mayor/%s/file?name=%s", worldID, filename)) }
                    data-indicator-saving
                    data-attr-disabled="$saving"
                    class="px-3 py-1.5 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90">
                    Save
                </button>
                <div id="mayor-save-status"></div>
            </div>
        }
    </div>
}
```

#### 4. Sessions section
**File**: `harness/views/mayor/sessions.templ` (new)

Shows active/recent OpenClaw sessions. Data fetched via CLI wrappers.

```go
templ SessionsSection() {
    <div data-show="$active_section === 'sessions'" style="display: none;">
        <div id="mayor-sessions-list">
            <!-- Patched by SSE with session data -->
        </div>
    </div>
}

templ SessionsList(sessions []SessionInfo, worldID string) {
    <div id="mayor-sessions-list" class="space-y-2">
        <div class="flex items-center justify-between mb-2">
            <h3 class="text-sm font-medium">OpenClaw Sessions</h3>
            <button data-on:click={ datastar.GetSSE(fmt.Sprintf("/mayor/%s/sessions", worldID)) }
                    class="text-xs text-muted-foreground hover:text-foreground">
                Refresh
            </button>
        </div>
        if len(sessions) == 0 {
            <p class="text-sm text-muted-foreground">No active sessions</p>
        }
        for _, s := range sessions {
            <div class="border rounded-md p-3 space-y-1">
                <div class="flex justify-between text-sm">
                    <span class="font-medium">{ s.Label }</span>
                    <span class="text-xs text-muted-foreground">{ s.Model }</span>
                </div>
                <div class="text-xs text-muted-foreground">
                    { fmt.Sprintf("%d messages", s.MessageCount) } · Last active: { s.LastActive }
                </div>
                <button
                    data-on:click={ datastar.GetSSE(fmt.Sprintf("/mayor/%s/session-preview?key=%s", worldID, s.Key)) }
                    class="text-xs text-primary hover:underline">
                    View conversation →
                </button>
            </div>
        }
    </div>
}

templ SessionPreview(items []SessionPreviewItem) {
    <div id="mayor-session-preview" class="border rounded-md p-3 mt-2 max-h-[400px] overflow-y-auto">
        for _, item := range items {
            <div class={ sessionItemClass(item.Role) }>
                <span class="text-xs font-medium">{ item.Role }</span>
                <p class="text-sm whitespace-pre-wrap">{ item.Content }</p>
            </div>
        }
    </div>
}
```

#### 5. Builds section
**File**: `harness/views/mayor/builds.templ` (new)

Build delegation history from `mayor_builds` table.

```go
templ BuildsSection() {
    <div data-show="$active_section === 'builds'" style="display: none;">
        <div id="mayor-builds-list">
            <!-- Patched by SSE with build data -->
        </div>
    </div>
}

templ BuildsList(builds []sqlc.MayorBuild) {
    <div id="mayor-builds-list" class="space-y-2">
        <h3 class="text-sm font-medium mb-2">Build History</h3>
        if len(builds) == 0 {
            <p class="text-sm text-muted-foreground">No builds yet</p>
        }
        for _, b := range builds {
            <div class={ buildCardClass(b.Status) }>
                <div class="flex justify-between items-start">
                    <div class="flex-1">
                        <span class={ buildStatusBadge(b.Status) }>{ b.Status }</span>
                        if b.CheckpointID.Valid {
                            <span class="text-xs text-muted-foreground ml-2">{ b.CheckpointID.String[:8] }</span>
                        }
                    </div>
                    <span class="text-xs text-muted-foreground">{ b.StartedAt.Format("Jan 2, 3:04 PM") }</span>
                </div>
                <p class="text-sm mt-1 line-clamp-2">{ b.Prompt }</p>
                if b.DurationSeconds.Valid {
                    <span class="text-xs text-muted-foreground">
                        { fmt.Sprintf("%ds", b.DurationSeconds.Int64) }
                    </span>
                }
                if b.ErrorMessage.Valid {
                    <p class="text-xs text-red-400 mt-1">{ b.ErrorMessage.String }</p>
                }
            </div>
        }
    </div>
}
```

#### 6. Activity Timeline section
**File**: `harness/views/mayor/activity.templ` (new)

Chronological timeline from `mayor_activity` table.

```go
templ ActivitySection() {
    <div data-show="$active_section === 'activity'" style="display: none;">
        <div id="mayor-activity-list">
            <!-- Patched by SSE with activity data -->
        </div>
    </div>
}

templ ActivityTimeline(activities []sqlc.MayorActivity) {
    <div id="mayor-activity-list" class="space-y-1">
        <h3 class="text-sm font-medium mb-2">Activity Timeline</h3>
        for _, a := range activities {
            <div class="flex items-start gap-3 py-1.5 border-b border-border/50 last:border-0">
                <span class="text-xs mt-0.5">{ activityIcon(a.ActivityType) }</span>
                <div class="flex-1 min-w-0">
                    <span class="text-sm">{ activityDescription(a.ActivityType, a.Detail) }</span>
                    <span class="text-xs text-muted-foreground ml-2">{ a.CreatedAt.Format("3:04 PM") }</span>
                </div>
            </div>
        }
    </div>
}
```

Helper functions for icons/descriptions:
```go
func activityIcon(actType string) string {
    switch actType {
    case "message_received": return "💬"
    case "message_sent":     return "🤖"
    case "build_triggered":  return "🔨"
    case "build_completed":  return "✅"
    case "build_failed":     return "❌"
    case "session_created":  return "📋"
    case "file_edited":      return "📝"
    default:                 return "•"
    }
}
```

> **Note on `memory_updated`**: OpenClaw autonomously updates MEMORY.md during
> agent conversations, but the harness has no way to detect this today. A future
> enhancement could use `fsnotify` to watch workspace files for changes and log
> `memory_updated` events, but this is out of scope for the initial implementation.
> The activity type is intentionally omitted from the icon function until a
> detection mechanism exists.
```

#### 7. Dashboard server handlers
**File**: `harness/internal/server/mayor_dashboard.go` (new)

**Security**: File allowlist + path traversal check (reusing pattern from `handleSharedAssets`).

```go
var editableFiles = map[string]bool{
    "SOUL.md": true, "MEMORY.md": true, "AGENTS.md": true,
    "IDENTITY.md": true, "USER.md": true,
}

var readOnlyFiles = map[string]bool{
    "skills/world-build/SKILL.md":          true,
    "skills/world-status/SKILL.md":         true,
    "skills/contribute-learning/SKILL.md":  true,
}
```

**Handlers:**

`GET /mayor/:worldID` — `handleMayorDashboard`:
- Render `DashboardPage` with world info

`GET /mayor/:worldID/events` — `handleMayorDashboardSSE`:
- Long-lived SSE connection
- On connect: patch file list, recent builds, recent activity, gateway status
- On new events: append to activity timeline

`GET /mayor/:worldID/files` — `handleMayorFiles` (SSE):
- List workspace files with sizes
- Patch `MayorFileList`

`GET /mayor/:worldID/file?name=SOUL.md` — `handleMayorFileRead` (SSE):
- Validate filename in allowlist, path traversal check
- Read content, patch `MayorFileEditor` + signal `mayor_file_content`

`PUT /mayor/:worldID/file?name=SOUL.md` — `handleMayorFileSave` (SSE):
- ReadSignals before NewSSE
- Validate in `editableFiles` only
- Atomic write (`.tmp` + `os.Rename`)
- Log `file_edited` to `mayor_activity`
- Patch confirmation

`GET /mayor/:worldID/sessions` — `handleMayorSessions` (SSE):
- Call `m.MayorManager.ListSessions(worldID)`
- Also sync to `mayor_sessions` table (upsert)
- Patch `SessionsList`

`GET /mayor/:worldID/session-preview?key=...` — `handleMayorSessionPreview` (SSE):
- Call `m.MayorManager.SessionPreview(key)`
- Patch `SessionPreview`

`GET /mayor/:worldID/builds` — `handleMayorBuilds` (SSE):
- Query `mayor_builds` from DB
- Patch `BuildsList`

`GET /mayor/:worldID/activity` — `handleMayorActivity` (SSE):
- Query `mayor_activity` from DB
- Patch `ActivityTimeline`

`GET /mayor/:worldID/gateway-status` — `handleMayorGatewayStatus` (SSE):
- Call `m.MayorManager.GatewayStatus()`
- Patch `#gateway-status` with health badge

#### 8. Route registration
**File**: `harness/internal/server/server.go`

```go
// Mayor Dashboard (session auth — browser access)
approved.GET("/mayor/:worldID", s.handleMayorDashboard)
approved.GET("/mayor/:worldID/events", s.handleMayorDashboardSSE)
approved.GET("/mayor/:worldID/files", s.handleMayorFiles)
approved.GET("/mayor/:worldID/file", s.handleMayorFileRead)
approved.PUT("/mayor/:worldID/file", s.handleMayorFileSave)
approved.GET("/mayor/:worldID/sessions", s.handleMayorSessions)
approved.GET("/mayor/:worldID/session-preview", s.handleMayorSessionPreview)
approved.GET("/mayor/:worldID/builds", s.handleMayorBuilds)
approved.GET("/mayor/:worldID/activity", s.handleMayorActivity)
approved.GET("/mayor/:worldID/gateway-status", s.handleMayorGatewayStatus)
```

#### 9. Dashboard link from world overlay
**File**: `harness/views/world/overlay.templ`

Add a small "Mayor Dashboard" link/button in the overlay header that navigates to `/mayor/:worldID`.

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] Path traversal check rejects `../../etc/passwd`
- [ ] Read-only files cannot be saved (handler rejects PUT)

#### Manual Verification:
- [ ] Navigate to `/mayor/:worldID` → full-width dashboard loads
- [ ] Memory tab: file list shows all workspace files with sizes
- [ ] Click SOUL.md → editor opens with content → edit + save → file updated on disk
- [ ] MEMORY.md shows as "not yet created" if it doesn't exist
- [ ] Skill files open read-only (no Save button)
- [ ] Sessions tab: shows OpenClaw sessions for the world's agent
- [ ] Click "View conversation" → session preview loads
- [ ] Builds tab: shows build history with status badges, durations, prompts
- [ ] Activity tab: chronological timeline of all mayor actions
- [ ] Gateway status badge shows in header (green = healthy, red = down)
- [ ] Dashboard link visible from world overlay

---

## Implementation Sequence

1. **Phase 1** — Dockerfile, entrypoint, docker-compose, setup script, 2D hooks
2. **Phase 2** — Migration, queries, sqlc, Discord OAuth, view updates, multi-step world creation form
3. **Phase 3** — Mayor package (provisioning, workspace files, Discord channel, CLI integration), hook into CreateWorld
4. **Phase 4** — Mayor auth middleware, build API, build→Discord events, instrumentation logging
5. **Phase 5** — Discord listener, chat component, prompt routing, message instrumentation
6. **Phase 6** — OpenClaw CLI wrappers, dashboard page, memory inspector, sessions, builds, activity, routes

Each phase builds on the previous and is independently testable.

---

## Testing Strategy

### Unit Tests
- Mayor provisioning: SOUL.md, AGENTS.md generation with various personality inputs (all fields, minimal fields, empty optional fields)
- Skill generation: SKILL.md with correct YAML frontmatter
- OpenClaw CLI integration: mock `exec.Command` to verify correct args
- Build API: request validation, checkpoint selection
- Discord channel creation, binding, invite/revoke
- Message mirroring: deduplication, author type classification
- File security: path traversal rejection, allowlist enforcement

### Integration Tests
- Full flow: create world → provision mayor → Discord channel → message → mayor responds → build → completes → Discord → browser → dashboard shows everything
- Multi-user: two users messaging the same mayor (Discord + browser)
- Error cases: build failure → mayor reports error, OpenClaw down → fallback to direct pipeline

### Manual Testing Steps
1. Create a world with rich mayor personality (all fields filled)
2. Verify SOUL.md contains all personality details
3. Open Discord channel → send an ambiguous request → mayor asks for clarification with personality
4. Answer → mayor triggers build → verify full pipeline
5. Check harness browser UI → conversation mirrored
6. Open `/mayor/:worldID` → verify all dashboard sections populated
7. Edit SOUL.md from dashboard → verify file updated → mayor uses new personality
8. After several interactions, check MEMORY.md → verify OpenClaw auto-updated it

## Performance Considerations

- **OpenClaw gateway** — ~50-100MB RAM, <100ms message latency
- **`discordgo` Gateway** — single persistent WebSocket, ~10MB RAM
- **OpenClaw CLI calls** — ~1-2s (Node.js startup). Acceptable for provisioning and dashboard queries (not on hot path)
- **SQLite instrumentation** — minimal overhead, all tables indexed
- **Dashboard SSE** — one connection per dashboard viewer, patched on load + on new events
- **Multi-step form** — client-side only (Datastar signals), no server round-trips between steps

## Migration Notes

- **Existing worlds** — get `mayor_name = 'Mayor'`, null personality. No OpenClaw agent or Discord channel. A migration script can provision agents for existing worlds.
- **Existing users** — have `github_id` but no `discord_id`. Must re-authenticate via Discord on next login.
- **Docker image rebuild** — first deploy after Phase 1 will be slow (Node.js + OpenClaw). Cached after that.

## Dependencies

| Dependency | Where | Notes |
|------------|-------|-------|
| Node.js 22+ | Docker image | OpenClaw runtime |
| pnpm | Docker image | OpenClaw package manager |
| OpenClaw source | Docker image | Cloned from GitHub |
| `openclaw` CLI | Docker PATH | Agent management |
| `discordgo` | Go module | `github.com/bwmarrin/discordgo` |
| `google/uuid` | Go module | ID generation |
| Discord OAuth app | Developer Portal | Primary auth |
| Discord Mayor bot | Developer Portal | OpenClaw persona |
| Discord Harness bot | Developer Portal | Listener + channel management |
| Discord guild | `.env` | `DISCORD_GUILD_ID` |
| GitHub OAuth app | Developer Settings | Optional account linking |
| `ANTHROPIC_API_KEY` | Already configured | Claude Code + OpenClaw |
| `GITHUB_TOKEN` | `.env` | GitHub API for learning PRs (repo push access) |

## File Inventory

### New files
| File | Phase | Purpose |
|------|-------|---------|
| `harness/scripts/setup-openclaw.sh` | 1 | Initialize OpenClaw home dir |
| `templates/2d/.claude/settings.json` | 1 | 2D template Claude settings |
| `templates/2d/.claude/hooks/*.sh` | 1 | 2D template build hooks |
| `harness/internal/db/migrations/004_discord_auth_and_mayor.sql` | 2 | Full schema migration |
| `harness/internal/db/queries/mayor_messages.sql` | 2 | Message queries |
| `harness/internal/db/queries/world_invites.sql` | 2 | Invite queries |
| `harness/internal/db/queries/mayor_activity.sql` | 2 | Activity log queries |
| `harness/internal/db/queries/mayor_builds.sql` | 2 | Build history queries |
| `harness/internal/db/queries/mayor_sessions.sql` | 2 | Session tracking queries |
| `harness/internal/mayor/mayor.go` | 3 | Manager + provisioning |
| `harness/internal/mayor/soul.go` | 3 | Rich SOUL.md template |
| `harness/internal/mayor/agents.go` | 3 | AGENTS.md template |
| `harness/internal/mayor/identity.go` | 3 | IDENTITY.md template |
| `harness/internal/mayor/user.go` | 3 | USER.md template |
| `harness/internal/mayor/skills.go` | 3 | Skill SKILL.md templates |
| `harness/internal/mayor/discord.go` | 3 | Discord channel management |
| `harness/internal/mayor/openclaw.go` | 3 | CLI integration (agent CRUD) |
| `harness/internal/mayor/openclaw_query.go` | 6 | CLI wrappers (dashboard queries) |
| `harness/internal/mayor/learning.go` | 4 | Learning PR creation via GitHub API |
| `harness/internal/server/mayor_api.go` | 4 | Build + status + contribute-learning API (mayor secret auth) |
| `harness/internal/server/world_invite.go` | 3 | Invite/revoke handlers |
| `harness/internal/discord/listener.go` | 5 | discordgo Gateway listener |
| `harness/views/world/chat.templ` | 5 | Mayor chat component |
| `harness/internal/server/mayor_dashboard.go` | 6 | Dashboard handlers |
| `harness/views/mayor/dashboard.templ` | 6 | Dashboard page layout |
| `harness/views/mayor/memory.templ` | 6 | Memory inspector section |
| `harness/views/mayor/sessions.templ` | 6 | Sessions section |
| `harness/views/mayor/builds.templ` | 6 | Builds section |
| `harness/views/mayor/activity.templ` | 6 | Activity timeline section |
| `harness/views/mayor/types.go` | 6 | Dashboard types |

### Modified files
| File | Phase | Changes |
|------|-------|---------|
| `harness/Dockerfile` | 1 | Add Node.js + OpenClaw |
| `harness/scripts/dev-entrypoint.sh` | 1 | Start OpenClaw gateway |
| `harness/docker-compose.yml` | 1 | Ports + env vars |
| `harness/internal/db/db.go` | 2 | Register migration |
| `harness/sqlc.yaml` | 2 | Column renames |
| `harness/internal/auth/auth.go` | 2 | Discord OAuth + dev login |
| `harness/internal/auth/middleware.go` | 2 | Redirect paths |
| `harness/views/login/login.templ` | 2 | Discord sign-in |
| `harness/views/lobby/lobby.templ` | 2 | Multi-step wizard + GitHub link |
| `harness/views/admin/admin.templ` | 2 | Discord username |
| `harness/views/pending/pending.templ` | 2 | Discord username |
| `harness/internal/db/queries/users.sql` | 2 | Discord auth queries |
| `harness/internal/db/queries/worlds.sql` | 2 | Mayor fields + channel |
| `harness/internal/db/queries/messages.sql` | 2 | JOIN discord_username |
| `harness/internal/world/manager.go` | 3 | Mayor provisioning hook |
| `harness/internal/server/server.go` | 2-6 | Routes, middleware, auth |
| `harness/internal/server/events.go` | 2 | Discord username refs |
| `harness/internal/claude/claude.go` | 4 | Build→Discord events + instrumentation |
| `harness/main.go` | 2-5 | Env vars, listener, mayor manager |
| `harness/views/world/overlay.templ` | 5-6 | Chat component + dashboard link |
| `harness/go.mod` / `go.sum` | 5 | discordgo dependency |

## References

- Previous plan (superseded): `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`
- Previous plan (superseded): `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md`
- OpenClaw research: `thoughts/CoreyCole/handoffs/general/2026-02-13_14-13-15_openclaw-world-mayors-plan-review.md`
- Dashboard handoff: `thoughts/CoreyCole/handoffs/general/2026-02-13_15-25-28_mayor-dashboard-design.md`
- Current inner Claude: `harness/internal/claude/claude.go`
- tmux sessions: `harness/internal/tmux/session.go`
- World manager: `harness/internal/world/manager.go`
- Hook scripts: `templates/3d/.claude/hooks/`
- DB schema: `harness/internal/db/migrations/001_initial.sql`
- World creation form: `harness/views/lobby/lobby.templ:50-66`
- Image gen handler: `harness/internal/server/imagegen.go`
- Chat tab system: `harness/views/chat/chat.templ:11-24`
- File serving security: `harness/internal/server/server.go:523-556`
