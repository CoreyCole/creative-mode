---
date: 2026-02-15T07:39:39+0000
reviewer: Claude (Staff Eng Review — VPS Context)
git_commit: a098b6c5afccb72da664511ba101b36a7a490122
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md
prior_review: thoughts/CoreyCole/reviews/2026-02-14_11-45-00_world-mayors-master-plan_review.md
status: complete
type: plan_review
context: VPS (Ubuntu, Nix + systemd, native binary — NOT Docker)
---

# Plan Review: World Mayors Master Plan (VPS Edition)

### Summary

The prior review (2026-02-14) identified 10+ issues including the architecture split between site/ and harness/. This review focuses on a **new critical issue the prior review couldn't have known about**: the VPS was migrated from Docker to Nix + systemd (commit `8ada159`), which fundamentally changes Phase 1 and affects the entire plan's infrastructure assumptions. Additionally, Rust/Cargo/Trunk are not yet installed on the VPS, meaning game worlds cannot be built yet. The core design (OpenClaw orchestration, Discord-as-bus, SOUL.md/AGENTS.md templates, instrumentation) remains sound — the issue is entirely about *how* to deploy it.

### Prior Review Status

The 2026-02-14 review identified these issues. Status update for each:

| Prior Issue | Status | Notes |
|-------------|--------|-------|
| Architecture split (site/ vs harness/) | **Still open** | site/ exists with Discord OAuth + onboarding. Must resolve before Phase 2+ |
| 2D template no `.claude/` hooks | **Still open** | Confirmed: `templates/2d/.claude/` does not exist |
| Success criteria run `go build` on host | **Obsolete** | On VPS, building directly IS the intended workflow. `just vps-build` does this |
| IDENTITY.md/USER.md not specified | **Still open** | Plan still says "same as original plan" without content |
| `GITHUB_TOKEN` missing from env | **Still open** | Not in harness `.env` file |
| `CreateLearningPR` hand-waved | **Still open** | Steps 3-5 are still comments |
| openclaw.json structure not shown | **Still open** | No JSON schema in plan |
| Two bots vs one bot mismatch | **Still open** | site/ uses one bot, plan specifies two |

---

### Critical Issues (Must Address Before Implementation)

#### 1. Phase 1 Is Written for Docker — VPS Uses Nix + systemd

**Problem**: Phase 1 is entirely Docker-centric:
- Modifies `harness/Dockerfile` to add Node.js + OpenClaw (lines 155-174)
- Modifies `harness/scripts/dev-entrypoint.sh` to start OpenClaw gateway (lines 177-187)
- Modifies `harness/docker-compose.yml` for ports + env vars (lines 189-210)

But the VPS harness now runs as a **native binary under systemd** (commit `8ada159`). The harness starts via `scripts/harness-run.sh` → `air` → `go build` → `./harness`. There's no Docker container in the picture.

**Current VPS deployment chain**:
```
systemd (creative-mode.service)
  → scripts/harness-run.sh (sources Nix, sets PATH)
    → air (live-reload)
      → go build → /tmp/harness (native binary)
```

**Risk**: If Phase 1 is implemented as-written, all changes go to files that aren't used on VPS. The Docker files still exist but aren't part of the production deployment path.

**Suggestion**: Rewrite Phase 1 for VPS native deployment:
1. Add `nodejs_22` and `pnpm` to `flake.nix` packages (or install via apt)
2. Clone/install OpenClaw to a known path (e.g., `/opt/openclaw` or `~/openclaw`)
3. Add OpenClaw gateway startup to `scripts/harness-run.sh` (before `exec air`)
4. Add Discord + OpenClaw env vars to `harness/.env`
5. Keep the Docker changes too (for macOS local dev), but note they're secondary

#### 2. Rust Toolchain Not Installed on VPS

**Problem**: The plan assumes a working build pipeline (ForkCheckpoint → Claude Code → Trunk build → WASM). But this VPS has **no Rust installation**:
- `rustup`, `cargo`, `trunk`, `wasm-bindgen-cli` are all missing
- `harness-run.sh` references `RUSTUP_HOME=/usr/local/rustup` and `CARGO_HOME=/usr/local/cargo` but those paths are empty
- This means **no worlds can be built** — not just mayor-initiated builds, but ANY builds

**Risk**: All of Phases 3-5 depend on the build pipeline working. Without Rust, the mayor can accept prompts but cannot actually build anything.

**Suggestion**: Before implementing the mayors plan, install the Rust toolchain:
```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
rustup target add wasm32-unknown-unknown
cargo install trunk wasm-bindgen-cli
```
This is a prerequisite for the plan, not part of it — but it should be explicitly called out.

#### 3. Node.js Not Installed on VPS

**Problem**: OpenClaw requires Node.js 22+ and pnpm. Neither is installed on the VPS. The plan's Dockerfile approach (`curl -fsSL https://deb.nodesource.com/setup_22.x | bash -`) doesn't apply to the Nix-managed VPS environment.

**Risk**: Phase 1 cannot complete — OpenClaw gateway won't start.

**Suggestion**: Either:
- Add `nodejs_22` and `nodePackages.pnpm` to `flake.nix` dev shell packages, OR
- Install via apt outside Nix (simpler but less reproducible)

#### 4. No Discord Env Vars Configured

**Problem**: The harness `.env` currently has: `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `HARNESS_URL`, `CM_HOOK_SECRET`. None of the Discord env vars exist:
- `DISCORD_CLIENT_ID` / `DISCORD_CLIENT_SECRET` (OAuth)
- `DISCORD_MAYOR_BOT_TOKEN` / `DISCORD_HARNESS_BOT_TOKEN` (bots)
- `DISCORD_GUILD_ID` (server)

**Risk**: Even with code implemented, nothing Discord-related will work until these are configured. The plan doesn't include a "create Discord bots/app" prerequisite step.

**Suggestion**: Add a "Phase 0: Prerequisites" that includes:
1. Create Discord server (guild)
2. Create Discord OAuth application in Developer Portal
3. Create bot application(s) in Developer Portal — resolve one-bot vs two-bot question
4. Configure redirect URIs
5. Add env vars to `harness/.env`
6. Install Rust toolchain
7. Install Node.js + pnpm

---

### Concerns (Should Address)

#### 1. `context/openclaw/` Directory Missing on VPS

**Observation**: The plan's "Key Discoveries" section (lines 32-50) references `context/openclaw/` extensively for verified implementation details (workspace files, config behavior, gateway hot-reload, HTTP hooks API). This directory doesn't exist on the VPS — it's gitignored and was only on the macOS dev machine.

**Suggestion**: The implementation will need to reference the OpenClaw source directly. Either clone `context/openclaw/` to this VPS or rely on the plan's documented findings (which are detailed enough to work from).

#### 2. `playwright-cli` Not in Plan's VPS Setup

**Observation**: The AGENTS.md template (lines 783-816) instructs mayors to use `playwright-cli` for checkpoint verification. The prior review flagged this as missing from Docker. On VPS, playwright-cli would also need to be installed separately. The VPS has no browser runtime for headed mode.

**Suggestion**: On VPS, playwright-cli needs Chromium. Options:
- Install via `npx playwright install chromium` (after Node.js is available)
- Run headless only (no `--headed` flag on VPS)
- Consider if playwright verification is even feasible on a headless VPS — it should be (headless Chromium works), but test early

#### 3. systemd Service Needs Additional Env Vars

**Observation**: The systemd service uses `EnvironmentFile=/home/deploy/creative-mode/harness/.env`. All new env vars must go in this file. The plan's docker-compose.yml env var list (lines 200-209) should be adapted as an `.env` file template.

**Suggestion**: Create a `.env.example` update that documents all required vars:
```
# Existing
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=
GEMINI_API_KEY=
ANTHROPIC_API_KEY=
HARNESS_URL=
CM_HOOK_SECRET=
# Discord (new)
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=
DISCORD_MAYOR_BOT_TOKEN=
DISCORD_HARNESS_BOT_TOKEN=
DISCORD_GUILD_ID=
# GitHub API (for learning PRs)
GITHUB_TOKEN=
# OpenClaw
OPENCLAW_HOME=
```

#### 4. OpenClaw Gateway Lifecycle Under systemd

**Observation**: The plan starts the OpenClaw gateway as a background process in the entrypoint (`node src/gateway/server.js &`). Under systemd + air, this needs careful thought:
- `air` restarts the harness binary on code changes — should the OpenClaw gateway restart too?
- If the gateway crashes, who restarts it? systemd only monitors the main process (air)
- The gateway should probably be a separate systemd service

**Suggestion**: Create a separate `openclaw-gateway.service` systemd unit instead of embedding it in the harness startup script. This gives independent lifecycle management, logs via journalctl, and automatic restart on crash.

#### 5. `harness-run.sh` PATH Setup for OpenClaw

**Observation**: The current `harness-run.sh` sets up PATH for Nix, Rust, Claude Code, and Go tools. OpenClaw (Node.js CLI) would need to be added to PATH here too, since tmux sessions spawned by the harness inherit this environment.

**Suggestion**: Add to `harness-run.sh`:
```bash
# OpenClaw + Node.js
export OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
export PATH="/path/to/openclaw/node_modules/.bin:$PATH"
```

---

### Questions (Need Clarification)

1. **One bot or two bots?** The plan specifies two Discord bots (mayor + harness). The site/ implementation uses one. Which approach for VPS? Two bots means two tokens to manage but cleaner separation. One bot is simpler. Decision needed before Phase 0 Discord setup.

2. **OpenClaw as separate service or embedded?** Should the OpenClaw gateway run as its own systemd service (recommended) or be spawned from `harness-run.sh`? A separate service is more resilient but adds deployment complexity.

3. **Resolve architecture split first?** The prior review's #1 critical issue — site/ handles onboarding, harness/ handles builds. Is the plan to use Option A (site/ onboards, harness reads pinned data) on this VPS? If so, is site/ deployed here too?

4. **Is site/ deployed on this VPS?** The VPS has `site/` in the repo but is there a systemd service running it? If not, the harness needs its own onboarding flow (the plan's multi-step form), making the "superseded by site/" argument from the prior review irrelevant.

5. **Rust toolchain installation**: Who/when installs it? Is this a blocker for starting mayor work, or can Phases 1-2 proceed without it?

---

### Suggestions (Nice to Have)

1. **Use Nix for OpenClaw dependencies**: Instead of `apt-get install nodejs`, add `nodejs_22` and `nodePackages.pnpm` to `flake.nix`. This keeps the environment reproducible and self-documenting.

2. **OpenClaw gateway healthcheck in systemd**: If running as a separate service, add `ExecStartPost=/usr/bin/curl -sf http://localhost:18789/health` (or equivalent) for startup verification.

3. **Consolidate env var naming**: Whether one bot or two, establish a single naming convention. The plan uses `DISCORD_MAYOR_BOT_TOKEN` / `DISCORD_HARNESS_BOT_TOKEN`. The site uses `DISCORD_BOT_TOKEN`. Pick one and document it.

4. **Add a migration path document**: The plan's migration notes (lines 1910-1912) are thin. Document: existing worlds get no mayor, existing users keep GitHub auth, first Rust install enables builds, OpenClaw install enables mayors.

---

### What's Good

The prior review's positive findings all still hold. Additionally:

- **AGENTS.md checkpoint verification workflow** (lines 783-816) — The Understand → Plan → Build → Verify → Save → Report flow with playwright-cli verification is the standout design element. It prevents mayors from saving broken checkpoints.

- **General vs world-specific knowledge taxonomy** (lines 833-878) — The contribute-learning feedback loop where mayors can PR improvements to CLAUDE.md files is a genuinely novel idea for AI agent knowledge sharing.

- **Instrumentation from day one** (Phase 2 DB schema) — Having `mayor_activity`, `mayor_builds`, `mayor_sessions` tables ready before any mayor runs means the dashboard has data from the start.

- **Non-fatal provisioning** (line 933-935) — "World still works without mayor" is exactly the right resilience pattern.

- **SOUL.md template richness** (lines 700-747) — Personality, tone, aesthetic, lore, examples — gives each mayor distinct character from first interaction.

- **File inventory tables** (lines 1934-1991) — Complete new/modified file lists make implementation planning concrete.

---

### Recommended Next Steps

1. **Answer the architecture questions**: One bot or two? Site/ deployed here? OpenClaw as separate service? These decisions unblock everything.

2. **Install prerequisites on VPS**: Rust toolchain + Node.js 22 + pnpm. Without these, nothing can be built or run.

3. **Rewrite Phase 1 for Nix + systemd**: Replace Docker-centric changes with:
   - `flake.nix` additions (Node.js, pnpm)
   - `harness-run.sh` PATH updates
   - Separate `openclaw-gateway.service` systemd unit
   - `.env` additions for Discord + OpenClaw vars

4. **Create Discord infrastructure**: Set up Discord server, OAuth app, bot(s) in Developer Portal. Add env vars to `.env`.

5. **Fix 2D template hooks**: Copy `templates/3d/.claude/` to `templates/2d/.claude/`. This is independent and unblocks 2D world builds.

6. **Fill in plan gaps from prior review**: IDENTITY.md/USER.md templates, `CreateLearningPR` implementation, openclaw.json schema, health check mechanism.

7. **Resolve site/ ↔ harness/ coordination**: Decide on Option A (site onboards, harness reads pinned data) or implement onboarding directly in harness.
