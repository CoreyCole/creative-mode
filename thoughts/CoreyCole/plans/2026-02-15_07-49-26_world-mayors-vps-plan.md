# World Mayors — VPS Implementation Plan

> **Supersedes:**
> - `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md`
>
> **Reviews incorporated:**
> - `thoughts/CoreyCole/reviews/2026-02-14_11-45-00_world-mayors-master-plan_review.md`
> - `thoughts/CoreyCole/reviews/2026-02-15_07-39-39_world-mayors-master-plan_review.md`

## Overview

Replace the direct "inner Claude" pipeline with **world mayors** — persistent OpenClaw AI agents, one per world, that orchestrate all world modifications through structured conversation. Users interact with their mayor from **Discord** (accessible from any phone) or from the **harness UI** (which mirrors the Discord conversation).

This plan is written for the **VPS deployment** (Ubuntu, Nix + systemd, native binaries) — not Docker.

## Deployment Architecture

| Service | Host | URL | Runtime |
|---------|------|-----|---------|
| **Harness** | Tailnet VM | `https://claude-2.tailcdc985.ts.net/` | Go binary under systemd (`creative-mode.service`) |
| **Site** | EC2 | `https://creative-mode.ai/` | Go binary under systemd (`creative-mode-site.service`) |
| **OpenClaw Gateway** | Tailnet VM | `localhost:18789` | Node.js under systemd (`openclaw-gateway.service`) — NEW |

**Site handles**: Discord OAuth, mayor onboarding conversation, Discord channel creation, onboarding data pinning.

**Harness handles**: Game worlds, build pipeline, Claude Code sessions, game servers, mayor agent provisioning, Discord message mirroring, mayor dashboard.

**Bridge**: Site pins `OnboardingData` as JSON in Discord channel. Harness reads it via `ReadOnboardingData` (`pkg/worldchannel/onboarding.go`).

## Single Discord Bot

One bot (`DISCORD_BOT_TOKEN`) handles everything — infrastructure (channel creation, build events, message mirroring) and AI persona (OpenClaw mayor responses). The site already uses this token for channel creation during onboarding.

**Message classification** in the Discord listener uses content format, not author identity:
- Content starts with `[BUILD` or `[SYSTEM` → `author_type = "system"`
- Bot message in response to OpenClaw routing → `author_type = "mayor"` (OpenClaw tags its messages)
- Otherwise → `author_type = "user"`

## What Exists (Verified)

| Component | Location | Status |
|-----------|----------|--------|
| Inner Claude pipeline | `harness/internal/claude/claude.go` | Working |
| tmux session management | `harness/internal/tmux/session.go` | Working |
| Hook scripts (3D) | `templates/3d/.claude/hooks/` | Working |
| Hook scripts (2D) | `templates/2d/.claude/` | **Missing** |
| World creation | `harness/internal/world/manager.go` | Working (`name, description, userID, templateType`) |
| GitHub OAuth | `harness/internal/auth/auth.go` | Working |
| Discord channel creation | `pkg/worldchannel/channel.go` | Working |
| Onboarding data bridge | `pkg/worldchannel/onboarding.go` | Working (`PinOnboardingData` / `ReadOnboardingData`) |
| Welcome message | `pkg/worldchannel/welcome.go` | Working |
| Event bus | `harness/internal/events/bus.go` | Working |
| SSE patterns | `harness/internal/server/events.go` | Working |
| DB migrations | `harness/internal/db/db.go` | Manual, 3 migrations applied |
| Nix dev shell | `flake.nix` | Working (Go, tmux, jq, sqlite, just, etc.) |
| systemd harness service | `/etc/systemd/system/creative-mode.service` | Working |
| Harness startup script | `scripts/harness-run.sh` | Working (Nix + air) |
| Node.js / pnpm | — | **Not installed** |
| Rust toolchain | — | **Not installed** |
| Discord bot token | `harness/.env` | **Not configured** |

## What We're NOT Doing

- **Discord OAuth in harness** — site handles Discord auth; harness keeps GitHub OAuth
- **Onboarding form in harness** — site handles onboarding conversation; harness reads pinned data
- **Docker changes as primary** — VPS uses native binaries; Docker files are for macOS dev only
- **Replacing Claude Code** — mayors orchestrate Claude Code, they don't replace it
- **Per-user mayors** — one mayor per world
- **Voice interaction** — text only
- **Mayor-to-mayor communication** — worlds are independent

---

## Phase 0: Prerequisites

### Overview
Install system dependencies and create Discord infrastructure. Everything in this phase is manual setup, not code.

### 0.1 Install Rust Toolchain

```bash
# System-wide Rust (matches harness-run.sh RUSTUP_HOME/CARGO_HOME paths)
sudo mkdir -p /usr/local/rustup /usr/local/cargo
sudo RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
  sh -c 'curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable'
sudo chmod -R a+r /usr/local/rustup /usr/local/cargo

# Add to PATH for current session
export PATH="/usr/local/cargo/bin:$PATH"

# Install tools
cargo install trunk wasm-bindgen-cli@0.2.108 cargo-watch
rustup target add wasm32-unknown-unknown
```

### 0.2 Install Node.js 22 + pnpm

Add to `flake.nix`:

```nix
packages = with pkgs; [
  # ... existing packages ...
  nodejs_22
  nodePackages.pnpm
];
```

Then `cd /home/deploy/creative-mode && direnv reload`.

Alternatively, install via apt if Nix complications arise:
```bash
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo bash -
sudo apt-get install -y nodejs
sudo npm install -g pnpm@latest
```

### 0.3 Install + Build OpenClaw

```bash
sudo git clone --depth 1 https://github.com/openclaw/openclaw.git /opt/openclaw
cd /opt/openclaw
sudo pnpm install --frozen-lockfile
sudo pnpm build
# Skip pnpm ui:build — gateway doesn't need the UI
sudo chown -R deploy:deploy /opt/openclaw
```

### 0.4 Create Discord Infrastructure

In the [Discord Developer Portal](https://discord.com/developers/applications):

1. **Discord server (guild)** — Create or use existing. Note the guild ID.
2. **Create "Worlds" category** — In the guild, create a channel category called "Worlds". Note its ID.
3. **Bot permissions** — Ensure the existing bot has MESSAGE_CONTENT intent enabled (needed for OpenClaw to read messages). Verify permissions: Send Messages, Read Message History, View Channels, Manage Messages.

### 0.5 Configure Environment Variables

**File**: `harness/.env` — Add:

```bash
# Discord (single bot — shared with site and OpenClaw)
DISCORD_BOT_TOKEN=<bot token>
DISCORD_GUILD_ID=<guild ID>
DISCORD_WORLDS_CATEGORY_ID=<worlds category ID>

# OpenClaw
OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw

# GitHub API (for learning PRs — optional, Phase 4)
# GITHUB_TOKEN=<token with repo push access>
```

### 0.6 Install playwright-cli

```bash
npx playwright install chromium
# Verify
npx playwright --version
```

If the `playwright-cli` wrapper script is needed for the mayor skill:
```bash
just setup-playwright  # if this exists, or install manually
```

### Success Criteria
- [ ] `cargo --version` works from deploy user
- [ ] `trunk --version` works
- [ ] `node --version` shows v22+
- [ ] `pnpm --version` works
- [ ] `/opt/openclaw` exists with built gateway
- [ ] `harness/.env` has all Discord + OpenClaw vars
- [ ] Bot is in the Discord guild with MESSAGE_CONTENT intent
- [ ] "Worlds" category exists in the guild

---

## Phase 1: OpenClaw Gateway + 2D Hooks

### Overview
Get the OpenClaw gateway running as a systemd service. Create OpenClaw home directory with Discord adapter config. Fix 2D template hooks.

### Changes Required

#### 1. OpenClaw systemd service
**File**: `/etc/systemd/system/openclaw-gateway.service` (new, created via bootstrap or manually)

```ini
[Unit]
Description=OpenClaw Gateway
After=network.target

[Service]
Type=simple
User=deploy
WorkingDirectory=/opt/openclaw
ExecStart=/usr/bin/node src/gateway/server.js
Restart=on-failure
RestartSec=5
EnvironmentFile=/home/deploy/creative-mode/harness/.env

[Install]
WantedBy=multi-user.target
```

#### 2. OpenClaw setup script
**File**: `harness/scripts/setup-openclaw.sh` (new)

Initializes `$OPENCLAW_HOME/openclaw.json` with the Discord adapter. Run once after env vars are configured.

```bash
#!/bin/bash
set -euo pipefail

OPENCLAW_HOME="${OPENCLAW_HOME:-/home/deploy/creative-mode/data/openclaw}"
mkdir -p "$OPENCLAW_HOME"

if [ -f "$OPENCLAW_HOME/openclaw.json" ]; then
    echo "OpenClaw already initialized at $OPENCLAW_HOME/openclaw.json"
    exit 0
fi

BOT_TOKEN="${DISCORD_BOT_TOKEN:-}"
if [ -z "$BOT_TOKEN" ]; then
    echo "WARNING: DISCORD_BOT_TOKEN not set. Creating config without Discord adapter."
    cat > "$OPENCLAW_HOME/openclaw.json" << 'EOF'
{
  "channels": {},
  "agents": [],
  "bindings": []
}
EOF
else
    cat > "$OPENCLAW_HOME/openclaw.json" << EOF
{
  "channels": {
    "discord": {
      "kind": "discord",
      "token": "$BOT_TOKEN"
    }
  },
  "agents": [],
  "bindings": []
}
EOF
fi

echo "Initialized OpenClaw at $OPENCLAW_HOME/openclaw.json"
```

#### 3. Update harness-run.sh
**File**: `scripts/harness-run.sh`

Add OpenClaw + Node.js to PATH:

```bash
# OpenClaw home (for CLI commands)
export OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw

# Node.js (for OpenClaw CLI — available via Nix or system)
# (No change needed if nodejs is in the Nix flake — direnv handles it)
```

#### 4. Fix 2D template hooks
**Files** (all new):
- `templates/2d/.claude/settings.json`
- `templates/2d/.claude/hooks/on-stop.sh`
- `templates/2d/.claude/hooks/on-tool-use.sh`
- `templates/2d/.claude/hooks/on-notification.sh`

Copy from `templates/3d/.claude/`. The hooks are template-agnostic — they POST events to the harness using env vars (`CM_WORLD_ID`, `CM_CHECKPOINT_ID`, etc.) that are set per-session by the orchestrator.

#### 5. Update flake.nix (if not using apt for Node.js)
**File**: `flake.nix`

```nix
packages = with pkgs; [
  # ... existing ...
  nodejs_22
  # pnpm via npm or corepack
];
```

### Success Criteria
- [ ] `sudo systemctl start openclaw-gateway` starts without error
- [ ] `journalctl -u openclaw-gateway` shows gateway listening on port 18789
- [ ] `curl -sf http://localhost:18789/health` returns OK (if health endpoint exists)
- [ ] `$OPENCLAW_HOME/openclaw.json` exists with Discord adapter
- [ ] `templates/2d/.claude/hooks/` has all 3 hook scripts
- [ ] `just vps-build` still succeeds (no regression)
- [ ] Existing harness still works: `curl http://localhost:8080` returns lobby

---

## Phase 2: DB Schema + World Discovery

### Overview
Add mayor and instrumentation tables to SQLite. Add a discovery mechanism for the harness to find worlds hatched on the site. No OAuth changes — the harness keeps GitHub OAuth.

### Changes Required

#### 1. Database Migration
**File**: `harness/internal/db/migrations/004_mayor_and_instrumentation.sql` (new)

```sql
-- Mayor identity + personality (world columns)
ALTER TABLE worlds ADD COLUMN mayor_name TEXT;
ALTER TABLE worlds ADD COLUMN mayor_personality TEXT;     -- from onboarding conversation
ALTER TABLE worlds ADD COLUMN mayor_secret TEXT;          -- per-world auth for mayor API
ALTER TABLE worlds ADD COLUMN discord_channel_id TEXT;
ALTER TABLE worlds ADD COLUMN openclaw_agent_id TEXT;

-- Discord message mirror for browser UI
CREATE TABLE IF NOT EXISTS mayor_messages (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    discord_message_id TEXT UNIQUE,
    discord_thread_id TEXT,
    author_type TEXT NOT NULL,  -- 'user', 'mayor', 'system'
    author_name TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_messages_world ON mayor_messages(world_id, created_at);

-- World channel access
CREATE TABLE IF NOT EXISTS world_invites (
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    invited_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (world_id, user_id)
);

-- Mayor activity log
CREATE TABLE IF NOT EXISTS mayor_activity (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    activity_type TEXT NOT NULL,
    detail TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_activity_world ON mayor_activity(world_id, created_at);
CREATE INDEX IF NOT EXISTS idx_mayor_activity_type ON mayor_activity(world_id, activity_type);

-- Mayor build delegations
CREATE TABLE IF NOT EXISTS mayor_builds (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT,
    prompt TEXT NOT NULL,
    original_request TEXT,
    status TEXT NOT NULL DEFAULT 'building',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_seconds INTEGER,
    error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_mayor_builds_world ON mayor_builds(world_id, started_at);

-- Mayor sessions (OpenClaw session tracking)
CREATE TABLE IF NOT EXISTS mayor_sessions (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    session_key TEXT NOT NULL,
    label TEXT,
    model TEXT,
    message_count INTEGER DEFAULT 0,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_sessions_world ON mayor_sessions(world_id, last_active_at);
```

Note: Using `IF NOT EXISTS` / `IF NOT EXISTS` for idempotency. No `users` table recreation — harness keeps GitHub auth unchanged.

#### 2. Register migration
**File**: `harness/internal/db/db.go`

Add to `migrationFiles`:
```go
migrationFiles := []string{
    "migrations/001_initial.sql",
    "migrations/002_cascades_indexes.sql",
    "migrations/003_template_type.sql",
    "migrations/004_mayor_and_instrumentation.sql",
}
```

Add bootstrap check for the `mayor_name` column:
```go
// 004: adds mayor columns to worlds.
var hasMayorName int
_ = d.db.QueryRowContext(ctx,
    "SELECT COUNT(*) FROM pragma_table_info('worlds') WHERE name='mayor_name'",
).Scan(&hasMayorName)
if hasMayorName > 0 {
    _, _ = d.db.ExecContext(ctx,
        "INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
        "migrations/004_mayor_and_instrumentation.sql")
}
```

#### 3. SQL queries
**File**: `harness/internal/db/queries/worlds.sql` — Add:

```sql
-- name: UpdateWorldMayor :exec
UPDATE worlds SET mayor_name = ?, mayor_personality = ?, mayor_secret = ?,
    discord_channel_id = ?, openclaw_agent_id = ?
WHERE id = ?;

-- name: GetWorldByMayorSecret :one
SELECT * FROM worlds WHERE mayor_secret = ?;

-- name: GetWorldByDiscordChannel :one
SELECT * FROM worlds WHERE discord_channel_id = ?;

-- name: GetWorldsWithDiscordChannels :many
SELECT id, discord_channel_id, mayor_name FROM worlds
WHERE discord_channel_id IS NOT NULL;
```

**File**: `harness/internal/db/queries/mayor_messages.sql` (new)
**File**: `harness/internal/db/queries/mayor_activity.sql` (new)
**File**: `harness/internal/db/queries/mayor_builds.sql` (new)
**File**: `harness/internal/db/queries/mayor_sessions.sql` (new)
**File**: `harness/internal/db/queries/world_invites.sql` (new)

(Same queries as original plan — InsertMayorMessage, GetMayorMessages, InsertMayorActivity, GetMayorActivity, InsertMayorBuild, UpdateMayorBuildStatus, UpsertMayorSession, etc.)

#### 4. sqlc config updates
**File**: `harness/sqlc.yaml` — Add to `rename`:

```yaml
mayor_name: "MayorName"
mayor_personality: "MayorPersonality"
mayor_secret: "MayorSecret"
discord_channel_id: "DiscordChannelID"
openclaw_agent_id: "OpenClawAgentID"
discord_message_id: "DiscordMessageID"
discord_thread_id: "DiscordThreadID"
author_type: "AuthorType"
author_name: "AuthorName"
activity_type: "ActivityType"
original_request: "OriginalRequest"
error_message: "ErrorMessage"
duration_seconds: "DurationSeconds"
completed_at: "CompletedAt"
started_at: "StartedAt"
session_key: "SessionKey"
last_active_at: "LastActiveAt"
first_seen_at: "FirstSeenAt"
message_count: "MessageCount"
invited_by: "InvitedBy"
```

#### 5. World discovery endpoint
**File**: `harness/internal/server/mayor_api.go` (new)

The site needs a way to tell the harness "a new world was hatched." Add a webhook endpoint:

```go
// POST /api/world-hatched — called by site when a world is hatched
// Body: {"discord_channel_id": "...", "world_name": "...", "mayor_name": "..."}
// Auth: CM_HOOK_SECRET header (same pattern as /api/claude-event)
func (s *Server) handleWorldHatched(c echo.Context) error {
    // Validate hook secret
    // Read request
    // Call ReadOnboardingData to get full onboarding conversation
    // Create world in DB (or link to existing)
    // Trigger mayor provisioning (Phase 3)
    return c.JSON(http.StatusAccepted, map[string]string{"status": "accepted"})
}
```

Register route:
```go
e.POST("/api/world-hatched", s.handleWorldHatched, hookSecretMiddleware(hookSecret))
```

The site calls this after hatching a world. The harness can also discover worlds on startup by scanning Discord channels with `GetWorldsWithDiscordChannels`.

### Success Criteria
- [ ] Migration applies: harness starts without DB errors
- [ ] `just generate && just vps-build` succeeds
- [ ] All new tables exist in SQLite
- [ ] `POST /api/world-hatched` endpoint responds (even if provisioning isn't wired yet)

---

## Phase 3: Mayor Agent Provisioning

### Overview
When a world is hatched (via webhook from site), provision an OpenClaw agent with workspace files generated from the onboarding conversation. Bind the agent to the world's Discord channel.

### Changes Required

#### 1. Mayor package
**File**: `harness/internal/mayor/mayor.go` (new)

```go
type Manager struct {
    openclawHome    string
    openclawBin     string     // path to openclaw CLI
    harnessURL      string
    discordClient   *worldchannel.Client
    logger          *slog.Logger
}

func NewManager(openclawHome, harnessURL string, discordClient *worldchannel.Client,
    logger *slog.Logger) (*Manager, error)

// ProvisionAgent creates an OpenClaw agent for a world.
// Reads onboarding data from Discord, generates workspace files, binds to channel.
func (m *Manager) ProvisionAgent(ctx context.Context, db *db.DB,
    worldID, worldName, discordChannelID string) error
```

`ProvisionAgent` flow:
1. Read onboarding conversation via `discordClient.ReadOnboardingData(channelID)`
2. Extract mayor name, world name, summary, personality from conversation
3. Generate 32-byte hex mayor secret
4. Create OpenClaw agent via CLI: `openclaw agents add --non-interactive --json`
5. Write workspace files (SOUL.md, AGENTS.md, IDENTITY.md, USER.md, skills/)
6. Bind agent to Discord channel via `openclaw config set bindings` (read-modify-write)
7. Update world record in DB with `mayor_name`, `mayor_secret`, `openclaw_agent_id`
8. Log `session_created` to `mayor_activity`

#### 2. SOUL.md generation from onboarding conversation
**File**: `harness/internal/mayor/soul.go` (new)

Instead of generating from form fields, the SOUL.md is generated from the full onboarding conversation. The conversation IS the personality — it captures the user's vision, the mayor's emerging voice, and the creative direction.

```go
func GenerateSoulMD(data *worldchannel.OnboardingData) string {
    // Template uses:
    // - data.Mayor.Name
    // - data.World.Name
    // - data.World.Summary
    // - data.Messages (the full onboarding conversation as context)
}
```

Template structure:
```
# Soul

You are **{MayorName}**, the mayor of **{WorldName}**.

## Your Origin

You were born from a conversation with your world's creator. Here's how
you came to be — this conversation shaped your personality, values, and
vision for the world:

{Formatted onboarding conversation}

## World Vision

{WorldSummary}

## Core Traits
- You genuinely care about your world and the people building it
- You remember past conversations and build on them
- You celebrate successes and help troubleshoot failures
- You have opinions about design and aesthetics — share them
- You're collaborative, not authoritative — you guide, suggest, discuss

## Communication Style
- Address users by name when you know it
- Reference previous builds and decisions
- Be concise in chat but thorough when explaining plans
- You're not a generic assistant, you're {MayorName}
```

#### 3. AGENTS.md generation
**File**: `harness/internal/mayor/agents.go` (new)

Same structured workflow template as original plan (Understand → Plan → Build → Verify → Save → Report). Includes checkpoint verification with playwright-cli, general vs world-specific knowledge taxonomy, and contribute-learning instructions.

(Content from original plan lines 756-887 — unchanged, well-reviewed.)

#### 4. IDENTITY.md generation
**File**: `harness/internal/mayor/identity.go` (new)

```go
const identityTemplate = `# Identity

**Name**: {{.MayorName}}
**Role**: Mayor of {{.WorldName}}
**Template**: {{.TemplateType}} world

You are an AI agent responsible for orchestrating all modifications to
{{.WorldName}}. You work through conversation with the world's builders,
translating their ideas into concrete build prompts.
`
```

#### 5. USER.md generation
**File**: `harness/internal/mayor/user.go` (new)

```go
const userTemplate = `# User Context

## World
- **Name**: {{.WorldName}}
- **Type**: {{.TemplateType}}
- **Summary**: {{.WorldSummary}}
- **Created by**: {{.CreatorUsername}} (Discord: <@{{.CreatorDiscordID}}>)

## Harness
- **URL**: {{.HarnessURL}}
- **Build API**: POST {{.HarnessURL}}/api/mayor/build
- **Status API**: GET {{.HarnessURL}}/api/mayor/status?world_id={{.WorldID}}
`
```

#### 6. Skill definitions
**File**: `harness/internal/mayor/skills.go` (new)

Three skills: `world-build`, `world-status`, `contribute-learning`.

Each is a SKILL.md with YAML frontmatter and curl commands using `X-Mayor-Secret` auth.

(Content from original plan — unchanged.)

#### 7. OpenClaw CLI integration
**File**: `harness/internal/mayor/openclaw.go` (new)

```go
func (m *Manager) createAgentViaCLI(agentID, workspaceDir string) error
    // openclaw agents add --non-interactive --json --workspace {dir}
    // Sets OPENCLAW_HOME env var

func (m *Manager) bindAgentToDiscord(agentID, discordChannelID string) error
    // Read-modify-write on bindings:
    // 1. openclaw config get bindings --json
    // 2. Append new binding
    // 3. openclaw config set bindings --json '{...}'

func (m *Manager) deleteAgent(agentID string) error
    // openclaw agents delete --force {agentID}
```

#### 8. Wire into harness startup
**File**: `harness/main.go`

```go
// Set up mayor manager (optional — requires DISCORD_BOT_TOKEN + OPENCLAW_HOME).
var mayorManager *mayor.Manager
if botToken := os.Getenv("DISCORD_BOT_TOKEN"); botToken != "" {
    wcClient, err := worldchannel.NewClient(worldchannel.Config{
        BotToken:         botToken,
        GuildID:          os.Getenv("DISCORD_GUILD_ID"),
        WorldsCategoryID: os.Getenv("DISCORD_WORLDS_CATEGORY_ID"),
    }, logger)
    if err != nil {
        logger.Error("failed to create Discord client", "error", err)
    } else {
        mayorManager, err = mayor.NewManager(
            os.Getenv("OPENCLAW_HOME"),
            baseURL,
            wcClient,
            logger,
        )
        if err != nil {
            logger.Error("failed to create mayor manager", "error", err)
        }
    }
}

// Wire into server
srv.MayorManager = mayorManager
```

#### 9. Complete world-hatched handler
**File**: `harness/internal/server/mayor_api.go`

Wire the `handleWorldHatched` endpoint from Phase 2 to call `MayorManager.ProvisionAgent`.

### Success Criteria
- [ ] `just generate && just vps-build` succeeds
- [ ] Creating a world via site → webhook fires → agent provisioned
- [ ] `$OPENCLAW_HOME/workspaces/{agentID}/` has SOUL.md, AGENTS.md, IDENTITY.md, USER.md, skills/
- [ ] SOUL.md contains the onboarding conversation
- [ ] `openclaw agents list --json` shows the new agent
- [ ] Agent is bound to the Discord channel
- [ ] Mayor responds in Discord when user messages the channel

---

## Phase 4: Build Pipeline + Instrumentation

### Overview
Connect the existing build pipeline to Discord. Create the mayor build API. Instrument every touchpoint to SQLite.

### Changes Required

#### 1. Mayor auth middleware
**File**: `harness/internal/server/server.go`

```go
func (s *Server) mayorAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    // Validates X-Mayor-Secret header against per-world secrets
    // Sets c.Set("mayor_world", &world)
}
```

#### 2. Mayor build handler
**File**: `harness/internal/server/mayor_api.go`

```go
// POST /api/mayor/build — called by the mayor's world-build skill
func (s *Server) handleMayorBuild(c echo.Context) error {
    // Auth via X-Mayor-Secret
    // Log to mayor_builds + mayor_activity
    // Delegate to existing orchestrator.HandlePrompt
}

// GET /api/mayor/status — called by the mayor's world-status skill
func (s *Server) handleMayorStatus(c echo.Context) error {
    // Return: current checkpoint, build status, server info
}
```

#### 3. Build events → Discord + instrumentation
**File**: `harness/internal/claude/claude.go`

After build completes/fails in `BuildCheckpoint`:

```go
if world.DiscordChannelID.Valid && o.mayorManager != nil {
    var msg string
    if buildErr == nil {
        msg = fmt.Sprintf("[BUILD COMPLETE] Checkpoint `%s` — %s", cpID, workSummary)
    } else {
        msg = fmt.Sprintf("[BUILD FAILED] Checkpoint `%s`: %s", cpID, buildErr.Error())
    }
    go o.mayorManager.PostToDiscord(world.DiscordChannelID.String, msg)
}

// Log to mayor_builds and mayor_activity tables
```

#### 4. Contribute-learning handler + PR creation
**File**: `harness/internal/server/mayor_api.go`
**File**: `harness/internal/mayor/learning.go` (new)

```go
// POST /api/mayor/contribute-learning — submit a PR
func (s *Server) handleContributeLearning(c echo.Context) error

// CreateLearningPR uses the GitHub API to create a branch, commit, and PR.
func (m *Manager) CreateLearningPR(mayorName, worldName, targetFile, section, learning, description string) (prURL string, err error)
```

Implementation uses `gh` CLI for simplicity (available on VPS):
```bash
# From Go via exec.Command:
git checkout -b mayor/{name}/{timestamp}
# apply edit to target file
git add {targetFile}
git commit -m "[Mayor: {name}] {description}"
git push origin mayor/{name}/{timestamp}
gh pr create --title "[Mayor: {name}] {description}" --body "..."
```

Rate limit: 1 PR per mayor per hour (tracked in `mayor_activity`).

#### 5. Route registration
**File**: `harness/internal/server/server.go`

```go
// Mayor API — X-Mayor-Secret auth
mayor := e.Group("/api/mayor")
mayor.Use(s.mayorAuthMiddleware)
mayor.POST("/build", s.handleMayorBuild)
mayor.GET("/status", s.handleMayorStatus)
mayor.POST("/contribute-learning", s.handleContributeLearning)
```

### Success Criteria
- [ ] `just generate && just vps-build` succeeds
- [ ] `curl -X POST /api/mayor/build -H "X-Mayor-Secret: ..."` triggers build pipeline
- [ ] Build completion appears in Discord with `[BUILD COMPLETE]` prefix
- [ ] `mayor_builds` and `mayor_activity` tables have entries
- [ ] Contribute-learning creates a real PR on GitHub

---

## Phase 5: Discord Listener + Chat + Prompt Routing

### Overview
Add a `discordgo` Gateway listener to mirror all Discord messages to SQLite. Render the conversation in the world overlay. Route browser prompts through Discord.

### Changes Required

#### 1. Discord listener package
**File**: `harness/internal/discord/listener.go` (new)

`discordgo`-based Gateway WebSocket listener using the bot token. Watches all world channels, mirrors to SQLite, pushes SSE events.

```go
type Listener struct {
    session    *discordgo.Session
    db         *db.DB
    eventBus   *events.EventBus
    mu         sync.RWMutex
    channelMap map[string]string  // discord_channel_id → world_id
    botUserID  string             // our own bot's user ID (to detect bot messages)
    logger     *slog.Logger
}

func NewListener(botToken string, db *db.DB,
    eventBus *events.EventBus, logger *slog.Logger) (*Listener, error)
func (l *Listener) Start() error  // loads channel map from DB, opens Gateway
func (l *Listener) Stop() error
func (l *Listener) RegisterChannel(discordChannelID, worldID string)
```

Message classification (single bot — all bot messages come from our bot):
- Bot message + content starts with `[BUILD` or `[SYSTEM` → `author_type = "system"`
- Bot message + otherwise → `author_type = "mayor"` (OpenClaw responses)
- Not a bot → `author_type = "user"`

#### 2. Wire listener into harness startup
**File**: `harness/main.go`

Start if `DISCORD_BOT_TOKEN` is set.

#### 3. Mayor chat component
**File**: `harness/views/world/mayor_chat.templ` (new)

Renders mayor conversation. Distinct styles for user/mayor/system messages. SSE-updated via append.

#### 4. Add chat to world overlay
**File**: `harness/views/world/overlay.templ`

Add a "Mayor" tab to the existing chat tab system (alongside Global, World, Lineage, Assets). The mayor tab shows the Discord conversation mirrored from `mayor_messages`.

#### 5. Route browser prompts through Discord
**File**: `harness/internal/server/server.go` — `handlePrompt`

If world has Discord channel + mayor + healthy gateway → post user's message to Discord (it becomes part of the conversation, OpenClaw picks it up). Fall back to direct pipeline if gateway unhealthy.

Gateway health check: `MayorManager.IsGatewayHealthy()` — pings OpenClaw gateway HTTP endpoint with 2s timeout.

#### 6. Go dependency
Add `github.com/bwmarrin/discordgo` to `harness/go.mod`.

(Note: `pkg/worldchannel` already uses discordgo, so it's transitively available. But the harness should depend on it directly for the listener.)

### Success Criteria
- [ ] `just generate && just vps-build` succeeds
- [ ] Open world in browser → see conversation history from Discord
- [ ] Submit prompt from browser → message appears in Discord channel
- [ ] Mayor responds in Discord → response appears in browser chat
- [ ] Build events appear in both Discord and browser chat

---

## Phase 6: Mayor Dashboard

### Overview
Dedicated `/mayor/:worldID` page with full observability: memory inspector, sessions, builds, activity timeline. OpenClaw integration via CLI wrappers.

### Changes Required

(Same as original plan Phase 6 — the dashboard design is deployment-agnostic.)

#### Key components:
1. **OpenClaw CLI wrappers** (`harness/internal/mayor/openclaw_query.go`) — `GatewayStatus()`, `ListSessions()`, `SessionPreview()`, `AgentStatus()`
2. **Dashboard page** (`harness/views/mayor/dashboard.templ`) — full-width layout at `/mayor/:worldID`
3. **Memory Inspector** — browse/edit workspace files (SOUL.md, MEMORY.md, AGENTS.md editable; skills read-only)
4. **Sessions section** — OpenClaw sessions with preview
5. **Builds section** — from `mayor_builds` table
6. **Activity Timeline** — from `mayor_activity` table
7. **Dashboard handlers** (`harness/internal/server/mayor_dashboard.go`) — SSE endpoints, file read/write with allowlist + path traversal checks

#### Routes:
```go
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

### Success Criteria
- [ ] Navigate to `/mayor/:worldID` → dashboard loads
- [ ] Memory tab: SOUL.md editable, skills read-only
- [ ] Sessions tab: OpenClaw sessions visible
- [ ] Builds tab: build history with status
- [ ] Activity tab: chronological timeline
- [ ] Path traversal rejected for `../../etc/passwd`

---

## Implementation Sequence

```
Phase 0: Prerequisites (manual)
  ├── Install Rust toolchain
  ├── Install Node.js + pnpm
  ├── Install + build OpenClaw
  ├── Configure Discord bot (enable MESSAGE_CONTENT intent)
  └── Configure env vars

Phase 1: OpenClaw Gateway + 2D Hooks
  ├── systemd service for OpenClaw gateway
  ├── OpenClaw setup script
  ├── harness-run.sh PATH updates
  └── 2D template hooks

Phase 2: DB Schema + World Discovery
  ├── Migration (mayor + instrumentation tables)
  ├── SQL queries + sqlc config
  └── World-hatched webhook endpoint

Phase 3: Mayor Agent Provisioning
  ├── Mayor package (provisioning from onboarding data)
  ├── Workspace files (SOUL.md, AGENTS.md, IDENTITY.md, USER.md, skills)
  ├── OpenClaw CLI integration
  └── Wire into harness startup + world-hatched handler

Phase 4: Build Pipeline + Instrumentation
  ├── Mayor auth middleware
  ├── Build API + status API
  ├── Build events → Discord
  ├── Contribute-learning + PR creation
  └── Full instrumentation logging

Phase 5: Discord Listener + Chat + Prompt Routing
  ├── discordgo Gateway listener
  ├── Mayor chat component + overlay tab
  ├── Browser prompt → Discord routing
  └── Health check + fallback

Phase 6: Mayor Dashboard
  ├── OpenClaw CLI wrappers
  ├── Dashboard page + sections
  └── File editor with security
```

Each phase builds on the previous and is independently testable.

---

## File Inventory

### New files

| File | Phase | Purpose |
|------|-------|---------|
| `harness/scripts/setup-openclaw.sh` | 1 | Initialize OpenClaw config |
| `templates/2d/.claude/settings.json` | 1 | 2D template Claude settings |
| `templates/2d/.claude/hooks/*.sh` | 1 | 2D template build hooks |
| `harness/internal/db/migrations/004_mayor_and_instrumentation.sql` | 2 | Schema migration |
| `harness/internal/db/queries/mayor_messages.sql` | 2 | Message queries |
| `harness/internal/db/queries/mayor_activity.sql` | 2 | Activity log queries |
| `harness/internal/db/queries/mayor_builds.sql` | 2 | Build history queries |
| `harness/internal/db/queries/mayor_sessions.sql` | 2 | Session tracking queries |
| `harness/internal/db/queries/world_invites.sql` | 2 | Invite queries |
| `harness/internal/server/mayor_api.go` | 2-4 | World-hatched webhook + mayor API |
| `harness/internal/mayor/mayor.go` | 3 | Manager + provisioning |
| `harness/internal/mayor/soul.go` | 3 | SOUL.md from onboarding data |
| `harness/internal/mayor/agents.go` | 3 | AGENTS.md template |
| `harness/internal/mayor/identity.go` | 3 | IDENTITY.md template |
| `harness/internal/mayor/user.go` | 3 | USER.md template |
| `harness/internal/mayor/skills.go` | 3 | Skill SKILL.md templates |
| `harness/internal/mayor/openclaw.go` | 3 | CLI integration (agent CRUD) |
| `harness/internal/mayor/learning.go` | 4 | Learning PR creation |
| `harness/internal/discord/listener.go` | 5 | discordgo Gateway listener |
| `harness/views/world/mayor_chat.templ` | 5 | Mayor chat component |
| `harness/internal/mayor/openclaw_query.go` | 6 | CLI wrappers (dashboard queries) |
| `harness/internal/server/mayor_dashboard.go` | 6 | Dashboard handlers |
| `harness/views/mayor/dashboard.templ` | 6 | Dashboard page layout |
| `harness/views/mayor/memory.templ` | 6 | Memory inspector section |
| `harness/views/mayor/sessions.templ` | 6 | Sessions section |
| `harness/views/mayor/builds.templ` | 6 | Builds section |
| `harness/views/mayor/activity.templ` | 6 | Activity timeline section |

### Modified files

| File | Phase | Changes |
|------|-------|---------|
| `flake.nix` | 0 | Add nodejs_22 |
| `scripts/harness-run.sh` | 1 | Add OPENCLAW_HOME to env |
| `harness/internal/db/db.go` | 2 | Register migration 004 |
| `harness/sqlc.yaml` | 2 | Column renames |
| `harness/internal/db/queries/worlds.sql` | 2 | Mayor fields + channel |
| `harness/internal/server/server.go` | 2-6 | Routes, middleware |
| `harness/main.go` | 3-5 | Mayor manager, Discord listener |
| `harness/internal/claude/claude.go` | 4 | Build→Discord events + instrumentation |
| `harness/views/world/overlay.templ` | 5-6 | Mayor chat tab + dashboard link |
| `harness/go.mod` / `go.sum` | 5 | discordgo dependency |

### Systemd services (created manually, not in repo)

| File | Phase | Purpose |
|------|-------|---------|
| `/etc/systemd/system/openclaw-gateway.service` | 1 | OpenClaw gateway |

---

## Key Differences from Original Plan

| Aspect | Original Plan | This Plan |
|--------|--------------|-----------|
| **Deployment** | Docker (Dockerfile, docker-compose, entrypoint) | Nix + systemd (native binaries) |
| **OpenClaw runtime** | Background process in Docker entrypoint | Separate systemd service |
| **Auth** | Discord OAuth replaces GitHub in harness | Harness keeps GitHub OAuth; site handles Discord OAuth |
| **Onboarding** | Multi-step form in harness lobby | Site handles via conversation; harness reads pinned data |
| **SOUL.md source** | Form fields (MayorPersonality struct) | Onboarding conversation (OnboardingData) |
| **Channel creation** | Harness creates channels in Phase 3 | Site already creates channels; harness discovers them |
| **Site↔Harness bridge** | Not addressed | World-hatched webhook + ReadOnboardingData |
| **Discord bots** | Two bots (ambiguous env vars) | Single bot (`DISCORD_BOT_TOKEN`) for infrastructure + OpenClaw |
| **Prerequisites** | Assumed available | Explicit Phase 0 (Rust, Node.js, Discord setup) |
| **Users table** | Recreated with discord_id | Unchanged (GitHub auth preserved) |

---

## Dependencies

| Dependency | Where | Status |
|------------|-------|--------|
| Rust toolchain | VPS system-wide | **Not installed** — Phase 0 |
| Node.js 22+ | VPS via Nix or apt | **Not installed** — Phase 0 |
| pnpm | VPS | **Not installed** — Phase 0 |
| OpenClaw source | `/opt/openclaw` | **Not cloned** — Phase 0 |
| `openclaw` CLI | PATH | **Not available** — Phase 0 |
| `discordgo` | Go module | Available (used by `pkg/worldchannel`) |
| `google/uuid` | Go module | Already in `go.mod` |
| Discord guild | Developer Portal | **Not created** — Phase 0 |
| Discord bot | Developer Portal | **Exists** (shared with site) — needs MESSAGE_CONTENT intent |
| `ANTHROPIC_API_KEY` | `harness/.env` | Already configured |
| `GITHUB_TOKEN` | `harness/.env` | **Not configured** — Phase 4 |
| `pkg/worldchannel` | Go module (local replace) | Already in `go.mod` |
