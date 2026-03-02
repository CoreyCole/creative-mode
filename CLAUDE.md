# Creative Mode

Multiplayer creative sandbox — Go harness server + Bevy/WASM game client.

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `harness/` | Go server (Echo + SQLite + Datastar + templ) — see `harness/CLAUDE.md` |
| `templates/3d/` | 3D Bevy/Lightyear game template — see `templates/3d/CLAUDE.md` |
| `templates/2d/` | 2D Bevy room-based template — see `templates/2d/CLAUDE.md` |
| `templates/boardgame/` | Board game (Checkers) Bevy/WASM template — see `templates/boardgame/CLAUDE.md` |
| `scripts/` | Build, format, setup, and infrastructure bootstrap scripts |
| `site/`    | Marketing site + onboarding (Echo + templ) — see `site/CLAUDE.md` |
| `pkg/`     | Shared Go packages: `worldchannel` (Discord channels), `mayorchat` (onboarding chat), `markdown` (renderer), `imagegen` (image generation) |
| `context/` | Reference code (gitignored) |
| `thoughts/` | Plans, reviews, and notes |
| `thoughts/swarm/` | Swarm agent handoffs, plans, research, retrospectives, and learning digests |

## Agent System

Creative Mode uses a hierarchical AI agent system: **mayors** (per-world), a **president** (global), and the **swarm orchestrator** (ticket-driven).

### Mayors

Every world gets a mayor — an OpenClaw agent with a personality, evolving memory, and build skills. Mayors chat with users in Discord and trigger builds via the harness API.

**Flow**: Site onboarding → Discord channel created → `POST /api/world-hatched` webhook → harness provisions OpenClaw agent → mayor responds in Discord → can trigger builds via `POST /api/mayor/build`.

**Discord listener**: A separate discordgo Gateway session mirrors all messages from world channels into `mayor_messages` in SQLite, publishes `EventMayorMessage` to the EventBus, and streams them to the browser via SSE.

**Mayor Dashboard**: `/mayor/:worldID` — view builds, activity, messages, sessions, and read/edit workspace files (SOUL.md, MEMORY.md, AGENTS.md).

### President

One president agent oversees all mayors and the repo. It can query all mayor statuses, run `just check`, spawn Claude Code sessions for template updates, and trigger deploys. Auto-provisions on startup when `PRESIDENT_SECRET` and `DISCORD_PRESIDENT_CHANNEL_ID` env vars are set (currently disabled in production).

### Swarm Orchestrator

The swarm is a multi-phase AI orchestration system that takes Linear tickets through research → planning → implementation → verification → PR → human review. Unlike mayors (per-world, chat-driven), the swarm handles repo-level feature work autonomously.

**Architecture**: `internal/swarm/` (pure domain: enums, state machine, env config) + `internal/swarmorch/` (orchestrator: Manager, health, metrics, hooks, integrations).

**Workflow types**: `research` (research only), `code` (full implementation), `project` (decomposes into child research + code workflows).

**Code workflow phases**: `research` → `code_plan` → `plan_review` → `implement` → `verify` → `pr` → `human_review` → `done`

**Human review gates**: Every code workflow pauses at `human_review` after PR creation. Configurable gates at `plan_review` and `project_review` (opt-in). Humans approve/reject via dashboard (`/swarm/:id`) or API (`POST /api/swarm/gate/:id/approve|reject`).

**Integrations**: Linear (ticket status/comments), Graphite (branch stacking), Discord (alerts), Temporal (optional workflow engine).

**Swarm Dashboard**: `/swarm` — workflows table, metrics/health, events log, learnings. `/swarm/:id` — workflow detail with phase timeline, sessions, gate review panel.

See `harness/CLAUDE.md` for full API routes, configuration, and architecture details.

### Architecture: Agent Hierarchy

```
President (global, optional)
├── Oversees all mayors and the repo
├── Skills: mayor-status, repo-build, template-update, deploy
├── Channel: DISCORD_PRESIDENT_CHANNEL_ID
└── Auto-provisions on startup if env vars set

Swarm Orchestrator (ticket-driven)
├── Takes Linear tickets through multi-phase workflows
├── Phases: research → plan → review → implement → verify → PR → human_review
├── Human gates: workflows pause for approval at configurable checkpoints
├── Skills: swarm-research, swarm-code-plan, swarm-plan-review, swarm-code,
│           swarm-code-verify, swarm-code-pr, swarm-project-decompose/plan/review/verify
├── Dashboard: /swarm (all workflows), /swarm/:id (detail + gate actions)
├── Integrations: Linear, Graphite, Discord alerts, Temporal (optional)
└── Sessions: Claude Code in tmux, hooks POST to /api/swarm/hook/*

Mayors (per-world)
├── OpenClaw agent with personality from onboarding
├── Workspace: {OPENCLAW_HOME}/workspaces/world-{worldID}/
│   ├── SOUL.md, AGENTS.md, IDENTITY.md, USER.md, MEMORY.md
│   └── skills/ (world-build, world-status, contribute-learning)
├── Discord channel: one per world (private)
└── Triggers Claude Code builds via POST /api/mayor/build

Claude Code (per-build session)
├── Runs in tmux: cm-{worldID}-{cpID}
├── Guided by templates/*/CLAUDE.md + MEMORY.md
├── Hook scripts POST events to /api/claude-event
└── Pipeline: ForkCheckpoint → edit → BuildCheckpoint → deploy
```

### Single Bot Architecture

The codebase uses ONE `DISCORD_BOT_TOKEN` for all Discord operations via separate `discordgo.Session` instances:
- **REST** (`pkg/worldchannel.Client`): Channel creation, welcome messages, pinning onboarding data
- **Gateway** (`internal/discord.Listener`): Real-time message mirroring from Discord → SQLite → EventBus → SSE
- **Mayor init** (`internal/mayor.Manager`): Creates `worldchannel.Client` for Discord API operations

### Environment Variables (Agent System)

| Variable | Required For | Purpose |
|----------|-------------|---------|
| `DISCORD_BOT_TOKEN` | Mayors + Discord listener | Bot auth (shared with site) |
| `DISCORD_GUILD_ID` | Mayors | Discord server ID |
| `DISCORD_WORLDS_CATEGORY_ID` | Mayors | Category for world channels |
| `DISCORD_PRESIDENT_CHANNEL_ID` | President | #creative-mode-dev channel |
| `PRESIDENT_SECRET` | President | Auth for `/api/president/*` |
| `CM_HOOK_SECRET` | Site→Harness webhook | Shared secret for `/api/world-hatched` |
| `ANTHROPIC_API_KEY` | All agents | Claude API for OpenClaw agents + Claude Code builds + swarm sessions |
| `OPENCLAW_HOME` | Mayors + President | Data dir (default: `data/openclaw`) |
| `LINEAR_API_KEY` | Swarm | Linear API for ticket status updates and comments |
| `GRAPHITE_TOKEN` | Swarm | Graphite CLI auth for branch stacking |
| `DISCORD_SWARM_CHANNEL_ID` | Swarm | Discord channel for swarm alerts |

### OpenClaw

OpenClaw is installed at `/opt/openclaw/`, CLI at `/opt/openclaw/node_modules/.bin/openclaw`. Setup script: `harness/scripts/setup-openclaw.sh`.

## Running the Server

### VPS (production) — Nix + systemd

The harness runs via `air` (hot-reload) under systemd — `scripts/harness-run.sh` sets up PATH for Nix, Rust, and Go tools, then `exec air`. Air rebuilds to `/tmp/harness` on file changes. Nix provides build/runtime deps, Rust is installed system-wide via rustup.

| Command | Purpose |
|---------|---------|
| `just vps-build` | Build harness binary (templ + tailwind + go build) |
| `just vps-deploy` | Pull + build + restart systemd service |
| `just vps-logs` | Stream service logs (journalctl) |
| `just vps-status` | Check service status |

All commands run from `harness/`.

### macOS (local dev) — Docker

**On macOS, always run the harness in Docker, never directly on the host.**

Running `go run .` on the host skips `DEV_MODE=true` and killing it can destroy tmux sessions that manage game servers. The Docker container bind-mounts the project root, so host-side cargo/go builds corrupt incremental builds. Use Docker:

| Command | Purpose |
|---------|---------|
| `just live` | Docker + host file watcher + Tailwind (recommended for dev) |
| `just up` | Docker container only |
| `just down` | Stop Docker container |

All commands run from `harness/`.

### Deployment Topology: Site + Harness

The system runs on two servers connected via Tailscale:

| Server | Runs | Access |
|--------|------|--------|
| **EC2** (Ubuntu) | Marketing site (`site/`) — native Go binary under systemd | Public: `creative-mode.ai` → Route 53 → Caddy:443 → localhost:3000 |
| **VPS** (Nix) | Harness (`harness/`) — `air` hot-reload under systemd | Internal: `100.x.x.x:8080` via Tailscale |

The site creates Discord channels during onboarding, then fires `POST /api/world-hatched` to the harness (via Tailscale) to provision the mayor agent. Both servers share `DISCORD_BOT_TOKEN` and `CM_HOOK_SECRET`.

## Skills

### `playwright-cli` — Autonomous Browser Debugging

The `.claude/skills/playwright-cli/` skill enables browser automation for debugging the harness UI.

**Setup**: `just setup-playwright` (installs CLI + generates skill)

**Quick reference**:
```bash
playwright-cli open http://localhost:8080 --headed --persistent  # launch browser (reuses session)
playwright-cli snapshot                      # get element refs
playwright-cli click e15                     # interact by ref
playwright-cli screenshot                    # capture to .playwright-cli/
playwright-cli console error                 # check JS errors
playwright-cli network                       # inspect requests
playwright-cli close                         # clean up
```

**Important flags** (CLI-only, not supported in config file):
- `--headed` — opens a visible browser window (config `headless: false` is ignored)
- `--persistent` — stores cookies/profile in `~/Library/Caches/ms-playwright/daemon/`. Session cookie lasts 7 days, so after one manual OAuth login, future sessions are authenticated automatically.

**Config**: `playwright-cli.json` at project root (`.playwright-cli/` output, 30s nav timeout for Docker cold starts).

### E2E Testing Tips

**Workflow per page**: `snapshot` → interact → `console error` → `screenshot` → read outputs. Always check console errors after navigation to catch regressions early.

**Datastar + Playwright interop**: Playwright's `fill` and `keyboard.type()` do NOT update Datastar signal bindings (`data-bind-*`). To test form submission, use `run-code` with `page.evaluate` to call `fetch()` directly:
```bash
playwright-cli run-code "async page => { await page.evaluate(async () => { await fetch('/api/chat', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({chat_text: 'test'})}); }); }"
```

**Verifying SSE connections**: `playwright-cli network` shows active SSE streams as `[GET] /events => [200] OK`. Check this after page load to confirm `data-init` attributes are working.

**Cookie management for auth testing**: Use `cookie-delete session` to simulate logged-out state, then navigate to verify middleware redirects. With `--persistent`, Discord OAuth auto-completes on re-login, so you can't easily observe the login page after middleware redirects — delete cookies and navigate to `/` directly instead.

**Reading snapshots**: Element refs like `[ref=e15]` are stable within a snapshot but change between snapshots. Always `snapshot` before interacting. The `[active]` annotation on elements indicates CSS active state (e.g., selected tab).

**Screenshots are images**: `playwright-cli screenshot` saves a PNG. Use the Read tool on the PNG path to view it — Claude Code is multimodal and can visually inspect screenshots.

## WASM Build Constraints

Each `wasm-bindgen` invocation uses ~5 GB RAM. The VPS has 10 GB, so only one template build can run at a time — two simultaneous builds will OOM. The build pipeline (`internal/builder/`) serializes builds per-world, but be aware if manually triggering builds.

## Build & Check

**macOS only: NEVER run `cargo build/clippy/check`, `go build`, `templ generate`, or `just generate` directly on the host.** The Docker container bind-mounts the project root, so host-side cargo writes to the same `target/` directories that trunk uses inside Docker, corrupting incremental builds and crashing the WASM server. These commands are denied in `.claude/settings.json`.

**On VPS**, building directly is the intended workflow — use `just vps-build` from `harness/`.

**Always use `just check` from the project root** (uses isolated `CARGO_TARGET_DIR` to avoid conflicts on macOS):

```bash
just check          # verify Go + Rust + WASM all compile
just fmt            # format all code
just setup          # run setup (currently only runs setup-playwright; scripts/setup.sh is a placeholder)
```

## Debug CLI

Query world game state from the terminal. Handles auth, endpoint routing, and output formatting for both 2D and 3D worlds.

| Command | Purpose |
|---------|---------|
| `just debug <world> status` | World status (template type, build, server) |
| `just debug <world> room` | 2D: current room + hotspots |
| `just debug <world> dialog` | 2D: dialog visibility + text |
| `just debug <world> click <id>` | 2D: trigger hotspot by ID |
| `just debug <world> query <comp...>` | 3D: server ECS query |
| `just debug <world> resources` | 3D: list server resources |
| `just debug <world> components <entity>` | 3D: list components on entity |
| `just debug <world> list` | List queryable types (client) |
| `just debug <world> resource <name>` | Query a resource by name (client) |
| `just debug <world> client-query <comp...>` | Query components (client) |
| `just debug <world> client '<json>'` | Raw client debug query |
| `just debug <world> server '<json>'` | Raw server BRP query |

Cookie is auto-extracted from `playwright-cli` (requires `--persistent` session).
Override with `COOKIE=<value> just debug ...`.
