---
date: 2026-02-15T23:42:38+0000
researcher: CoreyCole
git_commit: 326c63a0f31e965a7dc6ea892462ea2731d5a9c3
branch: main
repository: creative-mode
topic: "Documentation Audit: Agent Hierarchy (President → Mayors → Claude Code)"
tags: [research, documentation, agents, president, mayors, claude-code, architecture]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
---

# Research: Documentation Audit — Agent Hierarchy

**Date**: 2026-02-15T23:42:38 UTC
**Researcher**: CoreyCole
**Git Commit**: 326c63a
**Branch**: main
**Repository**: creative-mode

## Research Question

Audit all CLAUDE.md files against the actual codebase to identify inaccuracies, gaps, and areas needing improvement — with a focus on the president → mayors → Claude Code agent hierarchy and providing helpful details for engineers and mayor agents managing user worlds.

## Summary

The documentation is **largely accurate** for the harness (which has been recently updated) but has several gaps and inaccuracies across the site, root, and deployment docs. The biggest issues are: (1) root CLAUDE.md's env var table references `DISCORD_BOT_TOKEN` but the master plan assumes two bots while codebase uses one, (2) site CLAUDE.md omits three internal packages, (3) missing documentation for WASM build memory constraints, and (4) the master plan is still Docker-centric in Phase 1 despite VPS migration to Nix+systemd.

## Detailed Findings

### 1. Root CLAUDE.md — Accuracy vs. Codebase

#### Correct
- Project structure table: all directories exist and match descriptions
- Agent system description: mayors, president, OpenClaw setup — all implemented
- Mayor flow (site → Discord → webhook → harness → OpenClaw) — verified in code
- Discord listener description — matches `internal/discord/listener.go`
- Mayor Dashboard at `/mayor/:worldID` — routes and templates exist
- President auto-provisions on startup when env vars set — verified in `main.go:371-402`
- Build & Check section: `just check`, `just fmt`, `just setup` all work
- Debug CLI section: matches `scripts/debug.sh`
- Playwright-cli section: config and usage patterns accurate
- VPS commands (`vps-build`, `vps-deploy`, `vps-logs`, `vps-status`): all exist in `harness/justfile`
- macOS Docker commands (`live`, `up`, `down`): all exist

#### Inaccurate or Misleading
| Issue | Details | Fix |
|-------|---------|-----|
| Env var table says `DISCORD_BOT_TOKEN` for "Mayors + Discord listener" | Code uses a single `DISCORD_BOT_TOKEN` for everything (worldchannel REST + listener gateway + mayor init). This is correct — but the master plan originally proposed two bots. Root CLAUDE.md is correct. | Clarify in docs that this is ONE bot doing multiple things, not two bots |
| "VPS — runs as a native binary under systemd" | Actually runs via `air` (hot-reload) under systemd. `harness-run.sh` → `air` → builds to `/tmp/harness`. Not a bare binary. | Update to: "runs via air hot-reload under systemd" |
| `OPENCLAW_HOME` default documented as `data/openclaw` | `harness-run.sh` hardcodes `OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw`. The Go code falls back to `{dataDir}/openclaw` if env not set. Both are correct for different contexts. | No change needed, but worth noting VPS always uses the explicit path |

#### Missing from Root CLAUDE.md
| Gap | Why It Matters |
|-----|---------------|
| No mention of WASM build memory constraints | `wasm-bindgen` uses ~5 GB RAM per template. Multiple simultaneous builds OOM the 10 GB VPS. Engineers need to know this. |
| No mention of the site ↔ harness webhook bridge | The site fires `POST /api/world-hatched` to the harness to trigger mayor provisioning. This is a critical integration point. |
| No `pkg/worldchannel` documentation | Shared package used by both site and harness for Discord channel operations. Engineers need to know it exists. |
| `scripts/setup.sh` is a placeholder | `just setup` only runs `just setup-playwright` effectively. The "run setup" line in docs is misleading. |
| No env var example file for harness | `harness/.env.example` is referenced in VPS bootstrap but doesn't exist in the repo. The bootstrap script creates `.env` interactively. |
| No mention of `internal/builder/` | CLAUDE.md mentions `internal/claude/` but the build package was renamed to `internal/builder/` (commit 326c63a) |

### 2. Harness CLAUDE.md — Accuracy vs. Codebase

#### Correct
- Architecture overview, stack description, key packages table — all accurate
- Data flow diagram — matches code
- Auth middleware chain — matches `internal/auth/middleware.go`
- Mayor API routes, auth, handlers — all exist as documented
- President API routes — all exist as documented
- Mayor Dashboard routes — all exist as documented
- Discord Listener description — matches `internal/discord/listener.go`
- OpenClaw Integration section — matches `internal/mayor/manager.go`
- Build Notifications — `OnBuildComplete` callback wired in `main.go:269-283`
- EventBus — matches `internal/events/bus.go`
- DB Queries — all listed queries exist
- Templ patterns, Datastar patterns — all accurate
- Game server tmux patterns — accurate
- SSE patterns — accurate

#### Inaccurate
| Issue | Details |
|-------|---------|
| Key Packages lists `internal/claude/` as "Claude Code orchestrator" | This package exists but the BUILD functionality was moved to `internal/builder/builder.go`. `internal/claude/` handles memory.go and claude.go (session management), not building. |
| Missing packages from table | `internal/logging/` (structured logger), `internal/gemini/` (image generation), `internal/builder/` (build pipeline), `internal/tmux/` (session management) — all exist but not in the table |

### 3. Site CLAUDE.md — Accuracy vs. Codebase

#### Correct
- Meet the Mayor conversation flow — accurate
- Personality rules (anti-sycophantic) — matches `internal/mayor/prompt.go`
- Scripted fallback — matches `internal/mayor/scripted.go`
- Conversation persistence via pinned Discord messages — matches `pkg/worldchannel/onboarding.go`
- World hatching webhook — matches `internal/mayor/handler.go:377-414`
- Key constraints (ReadSignals before NewSSE, greeting dedup, rate limiting, mayor name uniqueness) — all accurate
- Datastar integration patterns — accurate
- Running section (Docker and EC2 deployment) — accurate

#### Inaccurate or Missing
| Issue | Details | Fix |
|-------|---------|-----|
| Architecture table missing `internal/db/` | SQLite persistence package added in commit f666372. Has schema creation, WAL mode setup. | Add to architecture table |
| Architecture table missing `internal/webhook/` | GitHub push webhook handler for self-rebuild (`POST /webhook/github`) | Add to architecture table |
| Architecture table missing `internal/ui/` | Shared templ components (tooltip, utils — anchor, signals, expressions, tailwind_merge) | Add to architecture table |
| `CM_HOOK_SECRET` documented as "required env var in site.env" | Neither `site.env.example` nor `.env.example` includes it. Code reads it from env and silently omits the header if unset. | Add `CM_HOOK_SECRET` to both env examples, or document that it's optional |
| Missing env vars: `SITE_DB_PATH`, `DEV_MODE`, `WEBHOOK_SECRET` | All read at runtime but not documented in CLAUDE.md | Add to env section |
| Docker compose missing `HARNESS_URL` | Local dev won't wire up the harness webhook, so world hatching won't work locally | Document this limitation or add to docker-compose.yml |
| Claude model not documented | Uses `anthropic.ModelClaudeSonnet4_5_20250929` in `client.go:14` | Document in CLAUDE.md |

### 4. Templates CLAUDE.md (3D and 2D)

#### 3D Template (`templates/3d/CLAUDE.md`)
- **Accurate**: Architecture, protocol constants, building instructions, debug system, dev server, key patterns, mayor context
- **Missing**: No mention of `internal/builder/` package rename

#### 2D Template (`templates/2d/CLAUDE.md`)
- **Accurate**: Architecture, room JSON schema, building instructions, debug system, key patterns, mayor context
- **Critical gap**: `templates/2d/.claude/` directory does NOT exist. The 2D template has no Claude Code hooks, meaning the build pipeline won't work for 2D worlds. This is called out in the master plan (Phase 1) and both reviews but remains unresolved.

### 5. Master Plan Status

The master plan (`thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md`) has been reviewed three times:

| Review | Key Finding |
|--------|-------------|
| 2026-02-14_11-45-00 | 10+ issues including architecture split (site/ vs harness/), missing 2D hooks |
| 2026-02-15_07-39-39 | VPS migration from Docker to Nix+systemd invalidates Phase 1 |
| 2026-02-15_18-19-42 | Harness already uses Discord OAuth (not GitHub) — invalidates bridge design assumptions |

A **VPS-specific plan** was written (`2026-02-15_07-49-26_world-mayors-vps-plan.md`) and the latest plan superseding everything is `2026-02-15_18-43-12_world-agents-president-mayors.md`.

**Key resolved decisions from reviews:**
- Single bot (not two) — confirmed by codebase
- Both site and harness use Discord OAuth — confirmed
- VPS uses Nix + systemd, not Docker — confirmed
- OpenClaw gateway should be a separate systemd service
- `nodejs_22` already in `flake.nix`

**Key unresolved items:**
- 2D template `.claude/` hooks still missing
- OpenClaw CLI API unverified on VPS (not installed yet)
- `CM_HOOK_SECRET` commented out in harness `.env`
- `DEV_MODE=true` on production VPS (security concern)
- WASM build memory issue (5 GB per `wasm-bindgen`) not addressed

### 6. Deployment Documentation Gaps

| Gap | Details |
|-----|---------|
| No checked-in systemd service file for harness | Generated by `vps-bootstrap.sh` at runtime. Site service IS checked in. |
| No `.env.example` for harness | Bootstrap creates `.env` interactively. Engineers can't see what vars are needed without reading the script. |
| `air` in production undocumented | Root CLAUDE.md says "native binary under systemd" but it's actually `air` managing the binary lifecycle. |
| No documentation of WASM memory constraints | Multiple trunk instances cause OOM on 10 GB VPS. Critical for anyone running worlds. |
| `scripts/setup.sh` is a no-op | Just prints "not yet implemented". `just setup` only runs playwright setup. |

## Code References

- `harness/main.go:330-402` — Mayor + President initialization, single bot token pattern
- `harness/internal/mayor/manager.go` — `NewManager`, `ProvisionFromWebhook`, `PostToDiscord`
- `harness/internal/president/manager.go` — `NewManager`, `Provision`
- `harness/internal/discord/listener.go` — Discord Gateway listener
- `harness/internal/server/server.go` — Route registration, middleware
- `harness/internal/server/mayor_api.go` — Mayor API handlers
- `harness/internal/server/president_api.go` — President API handlers
- `harness/internal/server/mayor_dashboard.go` — Dashboard handlers
- `harness/internal/builder/builder.go` — Build pipeline (renamed from `internal/build/`)
- `pkg/worldchannel/` — Shared Discord channel management (used by both site and harness)
- `scripts/harness-run.sh:28` — `OPENCLAW_HOME` path
- `site/internal/mayor/handler.go:377-414` — World hatching webhook
- `site/internal/mayor/client.go:14` — Claude model selection

## Architecture Insights

### Actual Agent Hierarchy (as implemented)

```
President (global, optional)
├── Oversees all mayors and the repo
├── Skills: mayor-status, repo-build, template-update, deploy
├── Channel: DISCORD_PRESIDENT_CHANNEL_ID
├── Auth: PRESIDENT_SECRET
└── Auto-provisions on startup if env vars set

Mayors (per-world)
├── OpenClaw agent with personality from onboarding
├── Workspace: {OPENCLAW_HOME}/workspaces/world-{worldID}/
│   ├── SOUL.md (personality, tone, aesthetic, lore)
│   ├── AGENTS.md (structured workflow)
│   ├── IDENTITY.md, USER.md
│   └── skills/ (world-build, world-status, contribute-learning)
├── Discord channel: one per world (private)
├── Auth: X-Mayor-Secret header
└── Triggers Claude Code builds via POST /api/mayor/build

Claude Code (per-build session)
├── Runs in tmux: cm-{worldID}-{cpID}
├── Guided by templates/*/CLAUDE.md + MEMORY.md
├── Hook scripts POST events to /api/claude-event
└── Pipeline: ForkCheckpoint → edit → BuildCheckpoint → deploy
```

### Single Bot Architecture

The codebase uses ONE `DISCORD_BOT_TOKEN` for three distinct purposes:
1. **REST operations** (`pkg/worldchannel.Client`): Channel creation, welcome messages, pinning onboarding data, mayor name uniqueness checks
2. **Gateway listener** (`internal/discord.Listener`): Real-time message mirroring from Discord → SQLite → EventBus → SSE
3. **Mayor manager init** (`internal/mayor.Manager`): Creates `worldchannel.Client` for Discord API operations

These use two separate `discordgo.Session` instances (REST-only for worldchannel, Gateway for listener).

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md` — Original 6-phase plan (Docker-centric)
- `thoughts/CoreyCole/reviews/2026-02-15_07-39-39_world-mayors-master-plan_review.md` — VPS edition review
- `thoughts/CoreyCole/reviews/2026-02-15_18-19-42_world-mayors-vps-plan_review.md` — Latest review, identifies Discord OAuth misconception
- `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` — Latest plan incorporating president concept
- `thoughts/CoreyCole/handoffs/general/2026-02-15_20-35-20_wasm-build-memory-optimization.md` — Critical WASM memory issue

## Recommended Documentation Updates

### Priority 1: Fix Inaccuracies

1. **Root CLAUDE.md**: Change "runs as a native binary under systemd" to "runs via air hot-reload under systemd"
2. **Root CLAUDE.md**: Add note about `internal/builder/` (renamed from `internal/build/`)
3. **Harness CLAUDE.md**: Update Key Packages table to include `internal/builder/`, `internal/logging/`, `internal/gemini/`, `internal/tmux/`
4. **Site CLAUDE.md**: Add `internal/db/`, `internal/webhook/`, `internal/ui/` to architecture table
5. **Site CLAUDE.md**: Add `CM_HOOK_SECRET` to env examples or document as optional

### Priority 2: Fill Gaps

6. **Root CLAUDE.md**: Add "WASM Build Constraints" section warning about 5 GB `wasm-bindgen` memory usage
7. **Root CLAUDE.md**: Add `pkg/worldchannel` to Project Structure table with description
8. **Root CLAUDE.md**: Document site ↔ harness webhook bridge (`POST /api/world-hatched`)
9. **Root CLAUDE.md**: Note that `scripts/setup.sh` is a placeholder
10. **Harness**: Create `.env.example` with all required/optional env vars documented
11. **2D Template**: Create `templates/2d/.claude/` hooks (copy from 3D template)

### Priority 3: Improve for Engineers & Mayors

12. **Root CLAUDE.md**: Add "Architecture: Agent Hierarchy" section with the diagram above
13. **Root CLAUDE.md**: Document single-bot vs two-bot decision explicitly
14. **Root CLAUDE.md**: Add "Deployment: Site ↔ Harness" section explaining the EC2 + VPS topology
15. **Templates CLAUDE.md**: Document that mayor-triggered builds use the same pipeline (already partially done)
16. **Site CLAUDE.md**: Document the Claude model used for onboarding

## Open Questions

1. Should the master plan be updated in place or should the latest VPS plan (`2026-02-15_18-43-12`) be treated as authoritative?
2. Should `DEV_MODE=true` be disabled on the VPS before adding mayor functionality?
3. Is the 2D `.claude/` hooks fix blocked on anything, or can it be done immediately?
4. Should there be a separate "Operations Guide" document for VPS management?
