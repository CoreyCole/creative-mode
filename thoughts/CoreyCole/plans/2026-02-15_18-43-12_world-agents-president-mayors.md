# World Agents: President + Mayors — Final Master Plan

## Overview

Creative Mode is a multiplayer creative sandbox (Go harness + Bevy/WASM clients). Users create worlds from templates, and AI agents build them. This plan introduces a hierarchical agent system: a **president** that manages the repo, and **mayors** that manage individual worlds. All agents use OpenClaw (agent framework) and communicate via Discord.

**Supersedes**: All previous world mayors plans and reviews.

**Key corrections from reviews**:
- Both site AND harness use **Discord OAuth** (not GitHub — verified in `harness/internal/auth/auth.go`)
- OpenClaw `context/` directory doesn't exist on VPS (gitignored, was on macOS). Must clone + verify in Phase 0
- Gateway entry is `openclaw gateway run` CLI command, not `node src/gateway/server.js`
- No outbound webhook in OpenClaw — use `discordgo` listener in harness for message mirroring

## Current State Analysis

### Existing Infrastructure:
- **Harness**: Go server (Echo + SQLite + Datastar + templ) running as systemd service on VPS
- **Site**: Marketing + "Meet the Mayor" onboarding on EC2, creates Discord channels via `pkg/worldchannel`
- **worldchannel package**: REST-only Discord bot client for channel creation, onboarding data pinning
- **Claude orchestrator**: tmux-based build pipeline (fork checkpoint → Claude edits → build → deploy)
- **3D template hooks**: `.claude/hooks/` scripts POST events to harness on tool use and session stop
- **2D template**: Missing `.claude/` hooks directory (needs copy from 3D)

### Key Discoveries:
- `harness/go.mod:5` — local replace directive for `pkg/worldchannel`
- `pkg/worldchannel/client.go:18` — REST-only client (no gateway WebSocket)
- `harness/internal/claude/claude.go:134` — `BuildCheckpoint` is the post-edit pipeline entry point
- `harness/internal/server/server.go:568-580` — `hookSecretMiddleware()` pattern for API auth
- `harness/internal/db/db.go:93-97` — migration file list + bootstrap pattern
- `site/internal/mayor/handler.go:267-320` — `hatchWorld()` creates channel + pins onboarding
- `harness/views/world/overlay.templ:12-40` — Overlay structure with chat panel

### Missing:
- No OpenClaw integration
- No mayor agent provisioning from onboarding data
- No Discord message mirroring to SQLite
- No president agent for repo-level oversight
- No mayor dashboard for observability
- 2D template missing Claude Code hooks

## Desired End State

A fully operational hierarchical agent system where:
1. Users complete "Meet the Mayor" onboarding → world hatches → Discord channel created → OpenClaw agent provisioned with personality from onboarding conversation
2. Users message mayor in Discord or browser → mayor responds with personality → can trigger builds
3. President agent monitors all mayors, fixes template bugs, creates PRs for harness changes
4. `/mayor/:worldID` dashboard shows full observability (sessions, builds, activity, workspace files)

### Verification:
1. Site hatch → webhook → harness provisions OpenClaw agent → mayor responds in Discord
2. Browser prompt → routes through Discord → mayor triggers build → result visible in both Discord and browser
3. President heartbeat checks mayor activity, spots patterns, creates PRs
4. Dashboard shows all mayor activity with editable workspace files

## Agent Hierarchy

```
President (1 per repo)
  |- Scope: /home/deploy/creative-mode/ (repo root)
  |- Channel: #creative-mode-dev
  |- Trigger: HEARTBEAT.md (every 30 min) + Discord messages
  |- Role: repo-level ops, mayor oversight, template improvements
  |
  +- Mayor A (world "cyber-arena")
  |    |- Scope: data/worlds/<dir>/<cpID>/ (checkpoint)
  |    |- Channel: #cyber-arena (private)
  |    |- Trigger: Discord messages only
  |
  +- Mayor B (world "cozy-village") ...
```

| Aspect | Mayor | President |
|--------|-------|-----------|
| Working dir | Checkpoint dir | Repo root |
| Builds via | `POST /api/mayor/build` → existing pipeline | `just check`, `just vps-build`, git PR |
| Can modify | World game code (shared/, server/, client/) | harness/, templates/, scripts/, docs |
| Autonomy | Full within world | Safe changes = auto-commit; harness code = PR |
| OpenClaw workspace | `$OPENCLAW_HOME/workspaces/world-<id>/` | `$OPENCLAW_HOME/workspaces/president/` |

## Deployment Architecture

| Service | Host | Runtime |
|---------|------|---------|
| Harness | Tailnet VPS | Go binary, systemd (`creative-mode.service`) |
| Site | EC2 | Go binary, systemd (`creative-mode-site.service`) |
| OpenClaw Gateway | Tailnet VPS | Node.js, systemd (`openclaw-gateway.service`) — NEW |

**Single Discord bot** (`DISCORD_BOT_TOKEN`). Message classification by content format:
- `[BUILD` or `[SYSTEM` prefix → `author_type = "system"`
- Bot message otherwise → `author_type = "mayor"` (OpenClaw response)
- Not our bot → `author_type = "user"`

## What We're NOT Doing

- Changing auth in either site or harness (both keep Discord OAuth as-is)
- Replacing Claude Code (mayors orchestrate it, they don't replace it)
- Per-user mayors (one mayor per world)
- Voice interaction
- Mayor-to-mayor communication
- Docker changes as primary (VPS native; Docker for macOS dev only)

---

## Phase 0: Prerequisites + OpenClaw Verification

Manual setup. No code changes.

### Overview
Install dependencies, clone OpenClaw, create Discord infrastructure, configure env vars.

### Changes Required:

#### 1. Install Rust Toolchain
```bash
sudo mkdir -p /usr/local/rustup /usr/local/cargo
sudo RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
  sh -c 'curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable'
rustup target add wasm32-unknown-unknown
cargo install trunk wasm-bindgen-cli@0.2.108 cargo-watch
```

#### 2. Add Node.js to flake.nix
**File**: `flake.nix` — add `nodejs_22` to packages. Then `direnv reload`.

Fallback (if Nix complications): `curl -fsSL https://deb.nodesource.com/setup_22.x | sudo bash - && sudo apt-get install -y nodejs && sudo npm install -g pnpm@latest`

#### 3. Clone, Build, and VERIFY OpenClaw
```bash
sudo git clone --depth 1 https://github.com/openclaw/openclaw.git /opt/openclaw
cd /opt/openclaw && sudo pnpm install --frozen-lockfile && sudo pnpm build
sudo chown -R deploy:deploy /opt/openclaw
```

**CRITICAL — Verify CLI before proceeding:**
```bash
/opt/openclaw/node_modules/.bin/openclaw --help
/opt/openclaw/node_modules/.bin/openclaw agents --help
/opt/openclaw/node_modules/.bin/openclaw config --help
/opt/openclaw/node_modules/.bin/openclaw gateway --help
```

Document actual flag names. If they differ from the research (`thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md`), update Phase 1 and Phase 3 accordingly.

Verify gateway startup:
```bash
OPENCLAW_HOME=/tmp/test-openclaw openclaw gateway run &
curl -sf http://localhost:18789/health  # check if health endpoint exists
kill %1
```

#### 4. Create Discord Infrastructure
1. Create Discord guild (or use existing). Note guild ID.
2. Create "Worlds" channel category. Note category ID.
3. Create "#creative-mode-dev" channel for president. Note channel ID.
4. Ensure bot has MESSAGE_CONTENT intent enabled.
5. Verify bot permissions: Send Messages, Read Message History, View Channels, Manage Messages.

#### 5. Configure Environment Variables
**File**: `harness/.env` — add:
```bash
DISCORD_BOT_TOKEN=<bot token>
DISCORD_GUILD_ID=<guild ID>
DISCORD_WORLDS_CATEGORY_ID=<worlds category ID>
DISCORD_PRESIDENT_CHANNEL_ID=<#creative-mode-dev channel ID>
OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
PRESIDENT_SECRET=<openssl rand -hex 32>
CM_HOOK_SECRET=<openssl rand -hex 32>  # UNCOMMENT and set this
```

**File**: site env — add `CM_HOOK_SECRET` (same value) and verify `HARNESS_URL` is set.

#### 6. Install playwright-cli
```bash
npx playwright install chromium
```

### Success Criteria:

#### Automated Verification:
- [ ] `cargo --version` returns stable toolchain
- [ ] `trunk --version` returns installed version
- [ ] `node --version` returns v22+
- [ ] `pnpm --version` returns installed version
- [ ] `/opt/openclaw/node_modules/.bin/openclaw --help` runs without error
- [ ] `OPENCLAW_HOME=/tmp/test-openclaw openclaw gateway run` starts and `curl -sf http://localhost:18789/health` responds

#### Manual Verification:
- [ ] OpenClaw CLI flags documented and match plan assumptions (or plan updated)
- [ ] `harness/.env` has all new vars configured
- [ ] `CM_HOOK_SECRET` is enabled (not commented out)
- [ ] Bot is in guild with MESSAGE_CONTENT intent
- [ ] "Worlds" category and "#creative-mode-dev" channel exist

---

## Phase 1: OpenClaw Gateway + 2D Hooks

### Overview
Get the OpenClaw gateway running as a systemd service. Initialize config. Fix 2D template hooks.

### Changes Required:

#### 1. OpenClaw systemd service
**File**: `/etc/systemd/system/openclaw-gateway.service` (new, manual)
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
EnvironmentFile=/home/deploy/creative-mode/harness/.env
KillMode=process

[Install]
WantedBy=multi-user.target
```

Note: `ExecStart` command may need adjustment based on Phase 0 verification.

#### 2. OpenClaw setup script
**File**: `harness/scripts/setup-openclaw.sh` (new)

Initializes `$OPENCLAW_HOME/openclaw.json` with Discord adapter config and empty agents/bindings. Idempotent (skips if file exists).

Config structure (verified from research):
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
  "agents": { "list": [] },
  "bindings": []
}
```

#### 3. Update harness-run.sh
**File**: `scripts/harness-run.sh` — add after Rust PATH:
```bash
export OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
```

#### 4. Fix 2D template hooks
Copy from `templates/3d/.claude/` to `templates/2d/.claude/`:
- `templates/2d/.claude/settings.json` (new)
- `templates/2d/.claude/hooks/on-stop.sh` (new)
- `templates/2d/.claude/hooks/on-tool-use.sh` (new)
- `templates/2d/.claude/hooks/on-notification.sh` (new)

Hooks are template-agnostic (POST events using `CM_WORLD_ID`, `CM_CHECKPOINT_ID` env vars).

#### 5. Add Node.js to flake.nix (if not done in Phase 0)
**File**: `flake.nix` — add `nodejs_22` to packages list.

### Success Criteria:

#### Automated Verification:
- [ ] `sudo systemctl start openclaw-gateway` starts without error
- [ ] `journalctl -u openclaw-gateway` shows gateway listening on 18789
- [ ] `cat $OPENCLAW_HOME/openclaw.json` has Discord adapter config
- [ ] `ls templates/2d/.claude/hooks/` shows all 3 hook scripts
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] `curl http://localhost:8080/health` returns `{"status":"ok"}`

#### Manual Verification:
- [ ] Gateway survives restart: `sudo systemctl restart openclaw-gateway`

---

## Phase 2: DB Schema + Site→Harness Bridge

### Overview
Add mayor + instrumentation tables. Create world-hatched webhook. Wire site to call it after hatching.

### Changes Required:

#### 1. Database migration
**File**: `harness/internal/db/migrations/004_mayor_and_instrumentation.sql` (new)

```sql
-- Mayor identity (world columns)
ALTER TABLE worlds ADD COLUMN mayor_name TEXT;
ALTER TABLE worlds ADD COLUMN mayor_personality TEXT;
ALTER TABLE worlds ADD COLUMN mayor_secret TEXT;
ALTER TABLE worlds ADD COLUMN discord_channel_id TEXT;
ALTER TABLE worlds ADD COLUMN openclaw_agent_id TEXT;

-- Discord message mirror
CREATE TABLE IF NOT EXISTS mayor_messages (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    discord_message_id TEXT UNIQUE,
    author_type TEXT NOT NULL,  -- 'user', 'mayor', 'system'
    author_name TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_messages_world ON mayor_messages(world_id, created_at);

-- Activity log
CREATE TABLE IF NOT EXISTS mayor_activity (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    activity_type TEXT NOT NULL,
    detail TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_activity_world ON mayor_activity(world_id, created_at);

-- Build delegations
CREATE TABLE IF NOT EXISTS mayor_builds (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'building',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_seconds INTEGER,
    error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_mayor_builds_world ON mayor_builds(world_id, started_at);

-- Session tracking
CREATE TABLE IF NOT EXISTS mayor_sessions (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    session_key TEXT NOT NULL,
    message_count INTEGER DEFAULT 0,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- World invites
CREATE TABLE IF NOT EXISTS world_invites (
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    invited_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (world_id, user_id)
);
```

#### 2. Register migration
**File**: `harness/internal/db/db.go` — add to `migrationFiles` and `bootstrapExistingMigrations`

Pattern: check `pragma_table_info('worlds')` for `mayor_name` column (same pattern as migration 003 at line 153-161 of `db.go`).

#### 3. SQL queries
**File**: `harness/internal/db/queries/worlds.sql` — add:
- `UpdateWorldMayor` — set mayor_name, mayor_secret, discord_channel_id, openclaw_agent_id
- `GetWorldByDiscordChannel` — lookup by discord_channel_id
- `GetWorldsWithDiscordChannels` — all worlds with channels (for startup discovery)

**New files**: `mayor_messages.sql`, `mayor_activity.sql`, `mayor_builds.sql`, `mayor_sessions.sql`, `world_invites.sql`

Use explicit column lists (not `SELECT *`) matching the existing codebase pattern.

#### 4. sqlc config
**File**: `harness/sqlc.yaml` — add renames for all new columns:
```yaml
mayor_name: "MayorName"
mayor_personality: "MayorPersonality"
mayor_secret: "MayorSecret"
discord_channel_id: "DiscordChannelID"
openclaw_agent_id: "OpenClawAgentID"
discord_message_id: "DiscordMessageID"
author_type: "AuthorType"
author_name: "AuthorName"
activity_type: "ActivityType"
session_key: "SessionKey"
message_count: "MessageCount"
first_seen_at: "FirstSeenAt"
last_active_at: "LastActiveAt"
started_at: "StartedAt"
completed_at: "CompletedAt"
duration_seconds: "DurationSeconds"
error_message: "ErrorMessage"
invited_by: "InvitedBy"
```

#### 5. World-hatched webhook endpoint
**File**: `harness/internal/server/mayor_api.go` (new)

```go
// POST /api/world-hatched
// Auth: hookSecretMiddleware() (same as /api/claude-event — no args, reads CM_HOOK_SECRET from env)
// Body: {"discord_channel_id", "world_name", "mayor_name", "creator_discord_id", "creator_username"}
func (s *Server) handleWorldHatched(c echo.Context) error
```

Register route in `server.go` alongside `/api/claude-event`:
```go
e.POST("/api/world-hatched", s.handleWorldHatched, hookSecretMiddleware())
```

#### 6. Site webhook call
**File**: `site/internal/mayor/handler.go` — in `hatchWorld()`, after `PinOnboardingData`, POST to `{HARNESS_URL}/api/world-hatched` with `X-Hook-Secret` header. Non-blocking (fire and forget via goroutine). Log errors but don't fail the user flow.

### Success Criteria:

#### Automated Verification:
- [ ] Harness starts without DB errors: `just vps-build && sudo systemctl restart creative-mode && journalctl -u creative-mode --no-pager -n 20`
- [ ] `sqlc generate` succeeds: `cd harness && sqlc generate`
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] All new tables exist: `sqlite3 data/creative-mode.db ".tables"` shows mayor_messages, mayor_activity, etc.
- [ ] Webhook responds: `curl -s -X POST -H "X-Hook-Secret: $CM_HOOK_SECRET" -H "Content-Type: application/json" -d '{"discord_channel_id":"test","world_name":"test","mayor_name":"test","creator_discord_id":"test","creator_username":"test"}' http://localhost:8080/api/world-hatched` returns 202

#### Manual Verification:
- [ ] Site `hatchWorld()` calls webhook (check harness logs after hatching a world)

---

## Phase 3: Mayor Agent Provisioning

### Overview
When world-hatched fires, provision an OpenClaw agent with workspace files generated from the onboarding conversation.

### Changes Required:

#### 1. Mayor manager
**File**: `harness/internal/mayor/mayor.go` (new)

```go
type Manager struct {
    openclawHome  string
    openclawBin   string  // path to openclaw CLI binary
    harnessURL    string
    discordClient *worldchannel.Client
    db            *db.DB
    logger        *slog.Logger
}

func (m *Manager) ProvisionAgent(ctx context.Context, worldID, worldName, discordChannelID string) error
```

`ProvisionAgent` flow:
1. `ReadOnboardingData(channelID)` via `pkg/worldchannel/onboarding.go:100`
2. Generate 32-byte hex `mayor_secret`
3. Create OpenClaw agent via CLI: `openclaw agents add ...` (flags verified in Phase 0)
4. Write workspace files: SOUL.md, AGENTS.md, IDENTITY.md, USER.md, skills/
5. Read-modify-write bindings: `openclaw config get bindings` → append → `openclaw config set bindings`
   - Note: `config set` does FULL REPLACE (research finding), must read existing first
6. Update world in DB: `UpdateWorldMayor()`
7. Log `agent_provisioned` to `mayor_activity`

#### 2. SOUL.md generation
**File**: `harness/internal/mayor/soul.go` (new)

Generated from `OnboardingData.Messages` (the full conversation). Template:
```
# Soul
You are **{MayorName}**, the mayor of **{WorldName}**.

## Your Origin
You were born from a conversation with your world's creator:
{formatted onboarding conversation}

## World Vision
{WorldSummary}

## Core Traits
- You genuinely care about your world
- You remember past conversations and build on them
- You have opinions about design — share them
- You're collaborative, not authoritative
```

#### 3. AGENTS.md, IDENTITY.md, USER.md, skills/
**Files**: `harness/internal/mayor/agents.go`, `identity.go`, `user.go`, `skills.go` (all new)

AGENTS.md: Understand → Plan → Build → Verify → Save → Report workflow. Includes checkpoint verification with playwright-cli, general vs world-specific knowledge, contribute-learning instructions.

Skills use YAML frontmatter format (verified from research):
```markdown
---
name: world-build
description: Trigger a build to modify the world
metadata: |
  {"skillKey": "world-build", "emoji": "🔨"}
---
# World Build
POST {HarnessURL}/api/mayor/build -H "X-Mayor-Secret: {secret}" -d '{"prompt": "..."}'
```

#### 4. OpenClaw CLI integration
**File**: `harness/internal/mayor/openclaw.go` (new)

Wrappers for `exec.Command`:
- `createAgentViaCLI(agentID, workspaceDir)` → `openclaw agents add ...`
- `bindAgentToDiscord(agentID, channelID)` → read-modify-write bindings
- `deleteAgent(agentID)` → `openclaw agents delete ...`

#### 5. Wire into harness startup
**File**: `harness/main.go` — if `DISCORD_BOT_TOKEN` set, create `worldchannel.Client` (reusing existing `pkg/worldchannel`), create `mayor.Manager`, set on server.

Pattern: graceful degradation — if token missing, `MayorManager` is nil, harness works without mayors.

#### 6. Complete world-hatched handler
**File**: `harness/internal/server/mayor_api.go` — wire to call `MayorManager.ProvisionAgent`.

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] `ls $OPENCLAW_HOME/workspaces/world-<id>/` shows SOUL.md, AGENTS.md, IDENTITY.md, USER.md, skills/
- [ ] `cat $OPENCLAW_HOME/workspaces/world-<id>/SOUL.md` contains onboarding conversation
- [ ] `openclaw agents list` shows the provisioned agent

#### Manual Verification:
- [ ] Site hatch → webhook → agent provisioned (end-to-end)
- [ ] Agent bound to Discord channel (check OpenClaw config)
- [ ] Mayor responds in Discord when user messages

---

## Phase 4: Build Pipeline + Instrumentation

### Overview
Connect mayors to the existing build pipeline. Create mayor API. Instrument everything.

### Changes Required:

#### 1. Mayor auth middleware
**File**: `harness/internal/server/server.go` — validates `X-Mayor-Secret` against per-world secrets in DB.

```go
func (s *Server) mayorAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        secret := c.Request().Header.Get("X-Mayor-Secret")
        if secret == "" {
            return echo.NewHTTPError(http.StatusUnauthorized, "missing mayor secret")
        }
        world, err := s.DB.GetWorldByMayorSecret(c.Request().Context(), secret)
        if err != nil {
            return echo.NewHTTPError(http.StatusForbidden, "invalid mayor secret")
        }
        c.Set("mayor_world", &world)
        return next(c)
    }
}
```

#### 2. Mayor API handlers
**File**: `harness/internal/server/mayor_api.go` — add:
- `POST /api/mayor/build` — auth via X-Mayor-Secret, delegates to `orchestrator.HandlePrompt()`
- `GET /api/mayor/status` — returns checkpoint + server info
- `POST /api/mayor/contribute-learning` — creates GitHub PR via `gh` CLI

Routes registered with mayor auth middleware:
```go
mayor := e.Group("/api/mayor")
mayor.Use(s.mayorAuthMiddleware)
mayor.POST("/build", s.handleMayorBuild)
mayor.GET("/status", s.handleMayorStatus)
mayor.POST("/contribute-learning", s.handleContributeLearning)
```

#### 3. Build events → Discord
**File**: `harness/internal/claude/claude.go` — in `BuildCheckpoint()`, after completion/failure:
```go
if world.DiscordChannelID.Valid && s.MayorManager != nil {
    msg := fmt.Sprintf("[BUILD COMPLETE] Checkpoint `%s` — %s", cpID, workSummary)
    go s.MayorManager.PostToDiscord(world.DiscordChannelID.String, msg)
}
```

#### 4. Learning PR creation
**File**: `harness/internal/mayor/learning.go` (new) — uses `gh pr create` via `exec.Command`. Rate limited: 1 PR per mayor per hour.

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] `curl -s -X POST -H "X-Mayor-Secret: <valid>" http://localhost:8080/api/mayor/status` returns world/checkpoint info
- [ ] `curl -s -X POST -H "X-Mayor-Secret: <valid>" -H "Content-Type: application/json" -d '{"prompt":"test"}' http://localhost:8080/api/mayor/build` triggers a build

#### Manual Verification:
- [ ] Build completion appears in Discord as `[BUILD COMPLETE]`
- [ ] `mayor_builds` and `mayor_activity` tables have entries after build
- [ ] Learning PR creation works (test with `gh pr list`)

---

## Phase 5: Discord Listener + Chat UI + Prompt Routing

### Overview
Mirror Discord messages to SQLite. Render conversation in harness UI. Route browser prompts through Discord.

### Changes Required:

#### 1. Discord listener
**File**: `harness/internal/discord/listener.go` (new)

Uses `discordgo` Gateway WebSocket (separate session from `worldchannel.Client` which is REST-only). Loads `channelMap` from `GetWorldsWithDiscordChannels()` on startup. On `MESSAGE_CREATE`: classifies, inserts to `mayor_messages`, publishes to EventBus.

**Important**: Use a separate `discordgo.Session` from `worldchannel.Client`. The listener calls `session.Open()` for Gateway; `worldchannel.Client` never calls `Open()` (REST-only).

Message classification:
```go
func classifyMessage(msg *discordgo.MessageCreate, botUserID string) string {
    if msg.Author.ID != botUserID {
        return "user"
    }
    if strings.HasPrefix(msg.Content, "[BUILD") || strings.HasPrefix(msg.Content, "[SYSTEM") {
        return "system"
    }
    return "mayor"
}
```

#### 2. Mayor chat component
**File**: `harness/views/world/mayor_chat.templ` (new) — Datastar SSE-updated conversation. Styles for user/mayor/system messages.

#### 3. Add chat to world overlay
**File**: `harness/views/world/overlay.templ` — "Mayor" tab alongside existing chat tabs. Shows `mayor_messages` for the world.

#### 4. Browser prompt routing
**File**: `harness/internal/server/server.go` — in `handlePrompt`: if world has Discord channel + healthy gateway → post to Discord instead of direct pipeline. Fallback to direct if unhealthy.

Gateway health: `MayorManager.IsGatewayHealthy()` pings `http://localhost:18789/health` with 2s timeout.

#### 5. Go dependency
`go get github.com/bwmarrin/discordgo` (already transitively available via `pkg/worldchannel`).

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] `go get github.com/bwmarrin/discordgo` resolves

#### Manual Verification:
- [ ] Discord messages appear in browser chat (mayor_chat component)
- [ ] Browser prompt posts to Discord channel
- [ ] Mayor responds in Discord, response appears in browser
- [ ] Build events visible in both Discord and browser
- [ ] `[BUILD COMPLETE]` and `[SYSTEM ...]` messages classified correctly

---

## Phase 6: President Agent

### Overview
Provision the president agent. Set up workspace, skills, HEARTBEAT.md for autonomous monitoring.

### Changes Required:

#### 1. President manager
**File**: `harness/internal/president/president.go` (new)

```go
type Manager struct {
    openclawHome      string
    openclawBin       string
    harnessURL        string
    presidentSecret   string
    db                *db.DB
    logger            *slog.Logger
}

func (m *Manager) Provision() error   // one-time setup
func (m *Manager) IsProvisioned() bool
```

`Provision()`:
1. Create OpenClaw agent `president` with workspace at `$OPENCLAW_HOME/workspaces/president/`
2. Write SOUL.md, AGENTS.md, IDENTITY.md, USER.md, HEARTBEAT.md, TOOLS.md
3. Write skills: `repo-build/SKILL.md`, `mayor-status/SKILL.md`, `template-update/SKILL.md`, `deploy/SKILL.md`
4. Bind to #creative-mode-dev channel

Called from `harness/main.go` on startup if `DISCORD_PRESIDENT_CHANNEL_ID` is set and not already provisioned.

#### 2. President workspace files

**SOUL.md**:
```
# Soul
You are the **President** of Creative Mode — the highest-level agent.

## Your Role
You oversee all mayor agents and maintain the repository infrastructure.
You work at the repo root: harness/, templates/, scripts/, and documentation.

## Safety Rules
| Tier | Scope | Action |
|------|-------|--------|
| Autonomous | templates/ CLAUDE.md, hook scripts, scripts/ | Commit + deploy |
| Autonomous | MEMORY.md, thoughts/ | Commit |
| PR Required | harness/ code changes | Create branch + PR |
| PR Required | flake.nix, DB migrations | Create branch + PR |
| Forbidden | .env files, force-push, deleting worlds | Never |
```

**AGENTS.md** — Two modes:

*Heartbeat mode*: Query `mayor-status`, check failed builds, look for patterns, fix templates autonomously or PR harness changes.

*Reactive mode*: Respond to maintainer messages in #creative-mode-dev.

**HEARTBEAT.md**:
```
# Heartbeat
Run every 30 minutes.

1. Check mayor-status for all worlds
2. Review builds failed since last heartbeat
3. If pattern (>2 worlds, same error): diagnose and fix
4. Check stale mayors (no activity 24h with pending messages)
5. Update MEMORY.md with observations
```

#### 3. President API handlers
**File**: `harness/internal/server/president_api.go` (new)

```go
presidentGroup := e.Group("/api/president")
presidentGroup.Use(presidentAuthMiddleware())  // validates X-President-Secret
presidentGroup.GET("/mayor-status", s.handlePresidentMayorStatus)
presidentGroup.POST("/repo-build", s.handlePresidentRepoBuild)
presidentGroup.POST("/template-update", s.handlePresidentTemplateUpdate)
presidentGroup.POST("/deploy", s.handlePresidentDeploy)
```

- `mayor-status`: Queries `mayor_activity`, `mayor_builds`, `mayor_sessions` + OpenClaw agent status. Returns all worlds with recent activity.
- `repo-build`: Spawns tmux session `cm-president-<ts>` at repo root, runs `just check`.
- `template-update`: Spawns Claude Code session at repo root with given prompt.
- `deploy`: Runs `just vps-build`, restarts service.

#### 4. President skills
Skill SKILL.md files that instruct the president how to call harness API:

- `repo-build`: `curl -X POST -H "X-President-Secret: $PRESIDENT_SECRET" $HARNESS_URL/api/president/repo-build`
- `mayor-status`: `curl -H "X-President-Secret: $PRESIDENT_SECRET" $HARNESS_URL/api/president/mayor-status`
- `template-update`: `curl -X POST ... -d '{"prompt": "..."}'`
- `deploy`: `curl -X POST ... $HARNESS_URL/api/president/deploy`

#### 5. Wire into main.go
**File**: `harness/main.go` — if `DISCORD_PRESIDENT_CHANNEL_ID` set, create president manager, provision if needed.

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] `openclaw agents list` shows `president` agent
- [ ] `ls $OPENCLAW_HOME/workspaces/president/` shows SOUL.md, AGENTS.md, HEARTBEAT.md, skills/
- [ ] `curl -s -H "X-President-Secret: $PRESIDENT_SECRET" http://localhost:8080/api/president/mayor-status` returns JSON

#### Manual Verification:
- [ ] President responds in #creative-mode-dev when mentioned
- [ ] President heartbeat runs (check logs every 30 min)
- [ ] President can spawn Claude Code at repo root
- [ ] Safe template change: autonomous commit + deploy
- [ ] Harness code change: creates PR, posts to #creative-mode-dev

---

## Phase 7: Mayor Dashboard

### Overview
Dedicated `/mayor/:worldID` page with full observability.

### Changes Required:

#### 1. OpenClaw CLI query wrappers
**File**: `harness/internal/mayor/openclaw_query.go` (new) — `GatewayStatus()`, `ListSessions()`, `AgentStatus()`

#### 2. Dashboard page + handlers
**Files**: `harness/internal/server/mayor_dashboard.go` (new), `harness/views/mayor/dashboard.templ` (new), `memory.templ`, `sessions.templ`, `builds.templ`, `activity.templ`

Routes:
```go
approved.GET("/mayor/:worldID", s.handleMayorDashboard)
approved.GET("/mayor/:worldID/events", s.handleMayorDashboardSSE)
approved.GET("/mayor/:worldID/file", s.handleMayorFileRead)
approved.PUT("/mayor/:worldID/file", s.handleMayorFileSave)
approved.GET("/mayor/:worldID/sessions", s.handleMayorSessions)
approved.GET("/mayor/:worldID/builds", s.handleMayorBuilds)
approved.GET("/mayor/:worldID/activity", s.handleMayorActivity)
```

File editing: allowlist (SOUL.md, MEMORY.md, AGENTS.md editable; skills read-only). Path traversal prevention via `filepath.Clean` + prefix check (same pattern as `handleSharedAssets` in `server.go:534-566`).

#### 3. Dashboard templ views
- **dashboard.templ**: Tab layout (Memory, Sessions, Builds, Activity)
- **memory.templ**: File viewer/editor for workspace files with allowlist
- **sessions.templ**: OpenClaw session list with message counts
- **builds.templ**: Build history table with status, duration, errors
- **activity.templ**: Chronological activity timeline

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `just vps-build` succeeds (from `harness/`)
- [ ] Path traversal rejected: `curl "http://localhost:8080/mayor/testworld/file?path=../../.env"` returns 400

#### Manual Verification:
- [ ] `/mayor/:worldID` dashboard loads with all tabs
- [ ] Memory tab: SOUL.md editable, skills read-only
- [ ] Builds tab: build history with status displayed
- [ ] Activity tab: chronological timeline renders
- [ ] SSE updates arrive in real-time

---

## File Inventory

### New Files (33)

| File | Phase |
|------|-------|
| `harness/scripts/setup-openclaw.sh` | 1 |
| `templates/2d/.claude/settings.json` | 1 |
| `templates/2d/.claude/hooks/on-stop.sh` | 1 |
| `templates/2d/.claude/hooks/on-tool-use.sh` | 1 |
| `templates/2d/.claude/hooks/on-notification.sh` | 1 |
| `harness/internal/db/migrations/004_mayor_and_instrumentation.sql` | 2 |
| `harness/internal/db/queries/mayor_messages.sql` | 2 |
| `harness/internal/db/queries/mayor_activity.sql` | 2 |
| `harness/internal/db/queries/mayor_builds.sql` | 2 |
| `harness/internal/db/queries/mayor_sessions.sql` | 2 |
| `harness/internal/db/queries/world_invites.sql` | 2 |
| `harness/internal/server/mayor_api.go` | 2-4 |
| `harness/internal/mayor/mayor.go` | 3 |
| `harness/internal/mayor/soul.go` | 3 |
| `harness/internal/mayor/agents.go` | 3 |
| `harness/internal/mayor/identity.go` | 3 |
| `harness/internal/mayor/user.go` | 3 |
| `harness/internal/mayor/skills.go` | 3 |
| `harness/internal/mayor/openclaw.go` | 3 |
| `harness/internal/mayor/learning.go` | 4 |
| `harness/internal/discord/listener.go` | 5 |
| `harness/views/world/mayor_chat.templ` | 5 |
| `harness/internal/president/president.go` | 6 |
| `harness/internal/president/soul.go` | 6 |
| `harness/internal/president/agents.go` | 6 |
| `harness/internal/president/identity.go` | 6 |
| `harness/internal/president/heartbeat.go` | 6 |
| `harness/internal/president/skills.go` | 6 |
| `harness/internal/server/president_api.go` | 6 |
| `harness/internal/mayor/openclaw_query.go` | 7 |
| `harness/internal/server/mayor_dashboard.go` | 7 |
| `harness/views/mayor/dashboard.templ` | 7 |
| `harness/views/mayor/activity.templ` | 7 |

### Modified Files (11)

| File | Phase |
|------|-------|
| `flake.nix` | 0 |
| `scripts/harness-run.sh` | 1 |
| `harness/internal/db/db.go` | 2 |
| `harness/internal/db/queries/worlds.sql` | 2 |
| `harness/sqlc.yaml` | 2 |
| `harness/internal/server/server.go` | 2-7 |
| `harness/main.go` | 3-6 |
| `harness/internal/claude/claude.go` | 4 |
| `harness/views/world/overlay.templ` | 5-7 |
| `harness/go.mod` / `go.sum` | 5 |
| `site/internal/mayor/handler.go` | 2 |

---

## Testing Strategy

### Unit Tests:
- Mayor manager provisioning logic
- Message classification (user/mayor/system)
- SOUL.md generation from onboarding data
- Path traversal prevention in dashboard file access

### Integration Tests:
- World-hatched webhook → agent provisioning → OpenClaw agent exists
- Mayor build API → orchestrator pipeline → build completes
- Discord listener → message mirrored to SQLite → EventBus broadcast

### Manual Testing Steps:
1. Complete "Meet the Mayor" onboarding on site
2. Verify Discord channel created with onboarding data pinned
3. Verify harness receives webhook and provisions OpenClaw agent
4. Message mayor in Discord → verify response with personality
5. Ask mayor to build something → verify build pipeline triggers
6. Check browser overlay shows Discord conversation
7. Verify president heartbeat fires and reports status
8. Navigate to `/mayor/:worldID` dashboard and verify all tabs

## Performance Considerations

- Discord listener uses a single Gateway WebSocket connection (not per-world)
- Mayor messages table indexed by `(world_id, created_at)` for efficient queries
- OpenClaw CLI calls are async (goroutines) to avoid blocking HTTP handlers
- Gateway health check cached with 30s TTL to avoid per-request pings
- Build events to Discord are fire-and-forget goroutines

## Migration Notes

- Migration 004 uses `ALTER TABLE` for world columns (SQLite limitation: no IF NOT EXISTS for ALTER)
- Bootstrap check uses `pragma_table_info('worlds')` for `mayor_name` column
- Existing worlds are unaffected (new columns are nullable)
- No data migration needed — mayor columns are populated on first world hatch

## Dependencies

| Dependency | Status |
|------------|--------|
| Rust toolchain | **Not installed** — Phase 0 |
| Node.js 22+ | Already in `flake.nix` |
| OpenClaw source | **Not cloned** — Phase 0 |
| `discordgo` | Available (used by `pkg/worldchannel`) |
| Discord guild | **Needs creation** — Phase 0 |
| Discord bot | **Exists** (shared with site) — needs MESSAGE_CONTENT intent |
| `ANTHROPIC_API_KEY` | Already configured |
| `GITHUB_TOKEN` | **Not configured** — Phase 4 |
| `pkg/worldchannel` | Already in `go.mod` |

## References

- OpenClaw research: `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md`
- Previous plans: `thoughts/CoreyCole/plans/2026-02-15_07-49-26_world-mayors-vps-plan.md`
- Reviews: `thoughts/CoreyCole/reviews/2026-02-15_18-19-42_world-mayors-vps-plan_review.md`
- Worldchannel package: `pkg/worldchannel/`
- Site mayor handler: `site/internal/mayor/handler.go`
- Harness server: `harness/internal/server/server.go`
- Claude orchestrator: `harness/internal/claude/claude.go`
