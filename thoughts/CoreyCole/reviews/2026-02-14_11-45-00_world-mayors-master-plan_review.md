---
date: 2026-02-14T11:45:00-08:00
reviewer: Claude (Staff Eng Review)
git_commit: b03f9326b6c7a09d54be4f4e7bff29e391b81afc
branch: main
plan_reviewed: thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md
prior_reviews:
  - thoughts/CoreyCole/reviews/2026-02-14_03-15-25_meet-the-mayor-site-page_review.md
  - thoughts/CoreyCole/reviews/2026-02-14_09-27-22_meet-the-mayor-implementation_review.md
status: complete
type: plan_review
---

# Thorough Review: World Mayors Master Plan

## 1. Summary

The master plan is a comprehensive, well-structured 6-phase design for replacing the direct "inner Claude" pipeline with persistent OpenClaw AI agents. However, it has a **fundamental architecture mismatch**: it was written entirely for the `harness/` monolith (Discord OAuth in `harness/internal/auth/`, multi-step form in `harness/views/lobby/`, mayor chat in the harness overlay), but significant implementation work happened in a **separate `site/` server** — Discord OAuth (`site/internal/auth/`), conversational onboarding (`site/internal/mayor/`), and Discord channel bootstrapping (`pkg/worldchannel/`). Phases 1-6 are unimplemented in harness, while Phases 2-3 are partially realized in site/. The core design — OpenClaw orchestration, Discord-as-bus, SOUL.md/AGENTS.md templates, contribute-learning feedback loop — is sound and should be preserved. The recently added sections (checkpoint verification, general vs world-specific knowledge, contribute-learning skill) are high quality. Resolution of the architecture split is the single prerequisite before any implementation can begin.

## 2. Critical Issue: Architecture Divergence (Must Resolve Before Implementation)

The plan targets `harness/` exclusively. The actual implementation built `site/` as a separate Go server for the onboarding flow. This creates a split where neither server has everything:

| Component | Plan Location | Actual Location | Status |
|-----------|---------------|-----------------|--------|
| Discord OAuth | `harness/internal/auth/` (Phase 2) | `site/internal/auth/auth.go:27-30` | Implemented in site, not harness |
| Mayor onboarding | `harness/views/lobby/lobby.templ` (Phase 2, multi-step form) | `site/internal/mayor/handler.go` (conversational chat) | Superseded — site uses conversation, not form |
| System prompt | Not specified (form fields → SOUL.md) | `site/internal/mayor/prompt.go:10-57` | Implemented in site |
| Discord channel creation | `harness/internal/mayor/discord.go` (Phase 3) | `pkg/worldchannel/client.go:28-46` | Implemented in pkg, usable by both |
| Onboarding persistence | Not specified | `pkg/worldchannel/onboarding.go:50-96` (PinOnboardingData) | Implemented — plan doesn't know about it |
| Onboarding read-back | Not specified | `pkg/worldchannel/onboarding.go:100-138` (ReadOnboardingData) | Implemented — this IS the bridge |
| Agent provisioning | `harness/internal/mayor/mayor.go` (Phase 3) | Not implemented anywhere | Still needed |
| Build pipeline | `harness/internal/claude/claude.go` | `harness/internal/claude/claude.go` | Exists, untouched |
| Mayor dashboard | `harness/views/mayor/` (Phase 6) | Not implemented | Still needed |

### Resolution Options

**Option A (Recommended): Site handles acquisition + onboarding; harness reads pinned data to bootstrap agents.**

The `ReadOnboardingData` function (`pkg/worldchannel/onboarding.go:100-138`) already exists as the bridge. When a world is "hatched" on site/, the onboarding conversation is pinned as JSON in the Discord channel. The harness calls `ReadOnboardingData(channelID)` to retrieve the conversation and generates SOUL.md/AGENTS.md from it. The harness never needs Discord OAuth or an onboarding form.

- **Pros**: Clean separation (site = marketing/onboarding, harness = game server/build pipeline). No changes to either server's auth system. The bridge already exists.
- **Cons**: Requires a trigger mechanism — how does harness know a new world was hatched? Options: (a) harness polls for new channels, (b) site POSTs to a harness webhook, (c) harness discovers channels when users first visit.
- **Work needed**: Define the trigger, implement SOUL.md generation from `OnboardingData` instead of form fields, wire `ReadOnboardingData` into harness startup/discovery.

**Option B: Merge site into harness.**

Move `site/internal/auth/`, `site/internal/mayor/`, and the site pages into the harness. Single server, single auth system.

- **Pros**: No coordination protocol needed. One deployment.
- **Cons**: Large merge effort. The site's Discord OAuth would conflict with harness's GitHub OAuth. The conversational onboarding (streaming Claude chat) is architecturally different from the harness's form-based patterns. Risk of destabilizing the working harness.

**Option C: Keep both servers, share state via database.**

Add a shared SQLite database (or use Discord as the database via pinned messages, which is already implemented). Both servers read/write world state.

- **Pros**: Keeps servers independent. Already partially implemented via Discord pins.
- **Cons**: Eventual consistency issues. Two servers managing the same Discord channels. Harder to reason about state ownership.

**Recommendation**: Option A. The bridge (`ReadOnboardingData`) already exists. The site handles the user-facing onboarding flow well. The harness should focus on what it does best: build pipeline, game servers, and the mayor dashboard. The trigger mechanism is the only missing piece.

## 3. Phase-by-Phase Analysis

### Phase 1: OpenClaw + Discord in Docker

**Status**: Not started. Still valid.

**What's still needed**: Everything — Dockerfile changes, Node.js/pnpm/OpenClaw installation, entrypoint modifications, docker-compose port/env updates, setup script.

**What's superseded**: Nothing — Phase 1 is infrastructure and doesn't overlap with site/.

**Confirmed blocker**: 2D template has no `.claude/` directory at all (verified: `templates/2d/.claude/` does not exist). The plan correctly identifies this at line 224-226 but understates it — without hooks, the entire build pipeline is non-functional for 2D worlds, not just mayors.

**Missing detail**: `playwright-cli` is not in the Dockerfile. The AGENTS.md template (line 784-816) instructs the mayor to use `playwright-cli console error` and `playwright-cli screenshot` for checkpoint verification. If playwright-cli isn't installed in the Docker image, these verification steps will fail silently.

**Recommendation**: Valid as-is. Add playwright-cli installation to the Dockerfile changes. Pin OpenClaw to a specific commit hash instead of `--depth 1` from main (see Risk Assessment).

### Phase 2: Discord OAuth + Mayor Personality + DB Schema

**Status**: Largely superseded by site/ implementation.

**What's superseded**:
- Discord OAuth — fully implemented in `site/internal/auth/auth.go:27-30` with `Config{ClientID, ClientSecret, RedirectURI}`, session management, and cookie handling
- Multi-step personality form — replaced by conversational onboarding in `site/internal/mayor/handler.go:46-265`, which collects the same data (world setting, gameplay, world name, mayor name) through natural conversation instead of form fields
- Discord channel creation — implemented in `pkg/worldchannel/client.go:28-46` with proper permission overwrites

**What's still needed**:
- DB migration for instrumentation tables (`mayor_activity`, `mayor_builds`, `mayor_sessions`, `mayor_messages`) — these don't exist in site/ and are still valuable
- `mayor_name` and `discord_channel_id` columns on the worlds table — needed for harness integration
- sqlc config updates for new columns

**What should be dropped**:
- The entire multi-step wizard (Steps 1-4, lines 519-602) — the conversational approach in site/ is strictly better and already implemented
- Discord OAuth in harness — keep GitHub OAuth for harness, let site/ handle Discord OAuth
- `HandleDevLogin` Discord ID changes — harness dev login should continue using GitHub IDs

**Recommendation**: Reduce Phase 2 to "DB schema + instrumentation only." Keep the migration for instrumentation tables and world columns. Drop OAuth and form changes entirely.

### Phase 3: Mayor Agent Provisioning + Discord Channel

**Status**: Partially done — channel creation implemented, agent provisioning still needed.

**What's superseded**:
- Discord channel creation (`harness/internal/mayor/discord.go`, line 898-906) — already implemented in `pkg/worldchannel/client.go` and used by site/
- Permission overwrites, invite/revoke — implemented in `pkg/worldchannel/channel.go`
- Channel name sanitization — implemented in `pkg/worldchannel/`

**What's still needed**:
- OpenClaw agent provisioning (`ProvisionAgent` at line 685-692) — the core of this phase
- SOUL.md generation — needs adaptation. The plan generates from `MayorPersonality` struct (form fields). With Option A, it should generate from `OnboardingData` (conversation + extracted world/mayor names)
- AGENTS.md generation — still valid, template is high quality (lines 756-887)
- Skill definitions (world-build, world-status, contribute-learning) — still needed
- OpenClaw CLI integration (agents add, config set bindings) — still needed

**What needs adaptation**:
- `IDENTITY.md` and `USER.md` templates are referenced at line 890-895 as "same as original plan" — but there IS no original plan content for these. The superseded plan (`2026-02-13_10-20-05`) may have had them, but this plan doesn't include their templates. They need to be specified.
- SOUL.md generation should consume `OnboardingData.Messages` (the full conversation) plus extracted names/summary, not `MayorPersonality` struct fields

**Recommendation**: Keep agent provisioning and workspace file generation. Drop channel creation (already done). Adapt SOUL.md generation to work from onboarding conversation data. Specify IDENTITY.md and USER.md templates.

### Phase 4: Build Pipeline + Instrumentation

**Status**: Not started. Still valid.

**What's still needed**: Everything — mayor auth middleware, build API, build→Discord events, instrumentation logging, contribute-learning handler and PR creation.

**Hand-waved details**: `CreateLearningPR` (lines 1177-1204) shows steps 1-2 (read file, apply learning) but steps 3-5 (create branch via GitHub API, create commit, create PR) are comments with `// 3. Create branch via GitHub API.` No implementation. The GitHub API for creating branches and commits via the Contents API is non-trivial (requires getting the current tree SHA, creating a blob, creating a new tree, creating a commit, and updating the ref).

**`applyLearning` concern**: The function (line 1207-1211) finds a `##` or `###` header and appends below it. But CLAUDE.md files use table-based sections (e.g., the "Common Build Issues" section in `templates/2d/CLAUDE.md` is a markdown table). Appending a new table row requires finding the table's end, not just the section header. If the section uses prose instead of tables, appending works. This ambiguity needs clarification.

**`GITHUB_TOKEN` env var**: Listed in the Dependencies table (line 1930) but NOT in the docker-compose.yml env vars section (lines 193-209). The token is needed for the GitHub API calls but would never be available at runtime.

**Recommendation**: Valid as-is, but `CreateLearningPR` needs a real implementation (not comments). Add `GITHUB_TOKEN` to docker-compose.yml. Document `applyLearning` behavior for both table and prose sections.

### Phase 5: Discord Listener + Chat + Prompt Routing

**Status**: Not started. Still valid.

**Missing details**:
- No health check or fallback mechanism defined. The plan says "Fall back to direct pipeline if gateway unhealthy" (line 1372) but doesn't define how health is checked or what "unhealthy" means (timeout? error response? process exit?)
- Chat tab integration unclear — the plan adds a `MayorChat` component (line 1360-1362) but the harness already has a 4-tab chat system (`harness/views/chat/chat.templ` with Global, World, Lineage, Assets tabs). Does mayor chat replace the World tab? Become a 5th tab? Replace the entire chat panel? This is unspecified.
- The `discordgo` dependency is already in use via `pkg/worldchannel/` (which uses `github.com/bwmarrin/discordgo`). The plan lists it as a new dependency (line 1380) but it's already transitively available.

**Recommendation**: Valid. Define the health check mechanism (suggest: `IsGatewayHealthy()` pings the OpenClaw gateway HTTP endpoint with a 2s timeout). Clarify chat tab integration.

### Phase 6: Mayor Dashboard

**Status**: Not started. Still valid.

**Session sync staleness**: The dashboard queries OpenClaw sessions via CLI wrappers (`ListSessions`, `SessionPreview`). These are point-in-time snapshots. The dashboard SSE connection patches data on initial load but doesn't re-poll. A user viewing the dashboard for 30 minutes sees stale session data unless they click "Refresh." This trade-off should be documented. Consider adding a periodic poll (every 30s) for the sessions section since it's the most dynamic.

**Recommendation**: Valid. Document the staleness trade-off. Consider adding auto-refresh for the sessions tab.

## 4. Recently Added Sections Review

### Checkpoint Verification (AGENTS.md, lines 783-816)

**Assessment**: Excellent addition. The verification workflow (Step 4: `playwright-cli console error` → `playwright-cli screenshot` → visual inspection → save or rollback) is well-designed and addresses a real gap — mayors previously had no way to verify builds before saving checkpoints.

**Issues**:
1. `playwright-cli` must be installed in the Docker image. The current Dockerfile has no playwright-cli installation. The plan's Phase 1 Dockerfile changes (lines 157-174) add Node.js and OpenClaw but not playwright-cli.
2. The mayor agent running through OpenClaw uses Claude as its LLM. For screenshot-based visual inspection, the model must be multimodal. The plan doesn't specify which Claude model OpenClaw should use, but the verification step requires vision capabilities (Sonnet 4.5 or Opus 4.6, not Haiku).
3. The instruction "If the world URL is not yet open, use `playwright-cli open` to navigate first" (line 816) assumes the mayor knows the world's URL. The world-status skill provides this, creating a dependency: the mayor must call world-status before verification. This implicit dependency should be made explicit in the workflow.

### General vs World-Specific Knowledge (AGENTS.md, lines 833-878)

**Assessment**: Excellent design. The distinction between world-specific knowledge (save to MEMORY.md) and general Creative Mode knowledge (contribute as a PR) is clear, well-exemplified, and solves a real problem — preventing knowledge silos where one mayor discovers a Bevy gotcha but no other mayor benefits.

**No issues found.** The examples are concrete, the target file mapping is correct, and the "if unsure, it's world-specific" heuristic is a good default.

### Contribute-Learning Skill (lines 1220-1283)

**Assessment**: Sound design. The skill template, target file allowlist, rate limiting (1 PR/mayor/hour), and example usage are well-specified.

**Issues**:
1. The `applyLearning` function needs to handle markdown tables (many CLAUDE.md sections use tables, not prose). See Phase 4 analysis.
2. `GITHUB_TOKEN` is missing from the docker-compose.yml environment block. Without it, `CreateLearningPR` will fail at runtime with no token for the GitHub API.
3. The PR body template (line 1199) uses `{learning}` but should include the full context of where it was added (diff or before/after) for easier review.

## 5. Internal Consistency Issues

### 1. Two bots vs one bot

The plan specifies **two Discord bots** (line 62): a Mayor bot (OpenClaw persona) and a Harness bot (listener, channel management). The site/ implementation uses **one bot** (`DISCORD_BOT_TOKEN` at `site/main.go:54`). The `pkg/worldchannel` package also uses one bot (`BotToken` at `pkg/worldchannel/client.go:12`). The plan's env vars are `DISCORD_MAYOR_BOT_TOKEN` and `DISCORD_HARNESS_BOT_TOKEN` (lines 204-205), but the implementation uses `DISCORD_BOT_TOKEN`.

**Resolution needed**: The two-bot architecture may still be the right call for production (clean persona separation), but the current implementation uses one bot. The plan should be updated to acknowledge the current single-bot state and clarify whether to keep one bot or migrate to two.

### 2. Env var naming mismatch

| Plan env var | Implementation env var | Used in |
|-------------|----------------------|---------|
| `DISCORD_MAYOR_BOT_TOKEN` | (doesn't exist) | Plan lines 204, 221 |
| `DISCORD_HARNESS_BOT_TOKEN` | (doesn't exist) | Plan lines 205, 1357 |
| (not referenced) | `DISCORD_BOT_TOKEN` | `site/main.go:54`, `site/docker-compose.yml:18` |

### 3. IDENTITY.md and USER.md templates not specified

Line 895 says: "Same as original plan — `IDENTITY.md` (name + role), `USER.md` (world context)." But the "original plan" (`2026-02-13_10-20-05_openclaw-world-mayors.md`) is superseded and not included. This plan does not contain the actual templates for IDENTITY.md or USER.md. Implementers would have to guess the content.

### 4. openclaw.json scaffold structure not shown

The setup script (`harness/scripts/setup-openclaw.sh`, line 219) is described as initializing `$OPENCLAW_HOME/openclaw.json` with the Discord adapter, but the actual JSON structure is never shown. The research document (`thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md:114`) has a partial example, but the plan itself doesn't include it.

### 5. Success criteria reference `just generate && go build` — violates project build rules

Six success criteria blocks (lines 234, 631, 982, 1300, 1385, 1841) use:

```
cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint
```

Per CLAUDE.md and project memory: **NEVER run `go build` directly on the host.** This corrupts Docker's trunk builds via the shared bind-mount `target/` directory. The success criteria should use `just check` (which runs `scripts/check.sh` with `CARGO_TARGET_DIR=/tmp/cm-check-target` to isolate host builds from Docker).

## 6. Missing Details

1. **IDENTITY.md template content** — Referenced as "same as original plan" but no content provided. Needs: agent name, role description, world association.

2. **USER.md template content** — Referenced as "same as original plan" but no content provided. Needs: world context, creator info, template type.

3. **CreateLearningPR steps 3-5** — Lines 1193-1201 are comments (`// 3. Create branch via GitHub API.`, `// 4. Create commit...`, `// 5. Create PR.`). The GitHub Contents API workflow for creating a branch+commit+PR is non-trivial and should be specified.

4. **openclaw.json initial structure** — The plan references `setup-openclaw.sh` creating this file but never shows the JSON schema. At minimum: `{ "channels": { "discord": { "kind": "discord", "token": "..." } }, "agents": [], "bindings": [] }`.

5. **Health check mechanism for OpenClaw gateway** — Phase 5 says "fall back to direct pipeline if gateway unhealthy" but doesn't define the health endpoint, timeout, or what constitutes "unhealthy."

6. **Session sync frequency for dashboard** — Phase 6 SSE connection patches session data on initial load but has no refresh interval. Stale data after 30+ minutes.

7. **Existing user migration path** — Line 1911 says existing users "have `github_id` but no `discord_id`. Must re-authenticate via Discord on next login." But with Option A (keep GitHub OAuth in harness), existing users don't need Discord auth in the harness at all. This migration note assumes Phase 2's Discord OAuth in harness, which is superseded.

8. **Site/harness coordination protocol** — How does harness discover worlds hatched on site/? Options: webhook, polling, on-demand discovery. Not specified anywhere.

9. **playwright-cli in Docker** — The AGENTS.md template requires playwright-cli for checkpoint verification, but the Dockerfile changes don't install it.

10. **`GITHUB_TOKEN` env var in docker-compose.yml** — Listed in Dependencies (line 1930) but absent from the docker-compose.yml environment block (lines 193-209).

## 7. Risk Assessment

### High Risk

- **Architecture mismatch** — The plan cannot be implemented as-written because it targets harness/ while the onboarding flow lives in site/. Must resolve before starting Phase 2+.
- **No bridge between site/ and harness/** — The `ReadOnboardingData` function exists but nothing calls it. No trigger mechanism for harness to discover new worlds from site/.

### Medium Risk

- **OpenClaw version not pinned** — The Dockerfile clones from `main` (`git clone --depth 1`, line 167). If OpenClaw makes a breaking change, the Docker build breaks without warning. Should pin to a specific commit hash.
- **Docker image size increase** — Adding Node.js 22 + pnpm + OpenClaw (with dependencies) adds approximately 500MB-1GB to the image. The plan notes this at line 1912 ("first deploy after Phase 1 will be slow") but doesn't quantify the permanent size increase or discuss mitigation (multi-stage build, skipping UI build).
- **`just generate && go build` in success criteria** — If implementers follow the plan's success criteria literally, they'll corrupt Docker builds. Six occurrences.

### Low Risk

- **Core design is sound** — The OpenClaw integration, Discord-as-bus architecture, SOUL.md/AGENTS.md templates, and instrumentation tables are well-designed and don't need fundamental changes.
- **SOUL.md template quality** — The personality template (lines 700-747) incorporates all personality dimensions and gives the mayor a strong identity from day one.
- **AGENTS.md workflow quality** — The structured workflow (Understand → Plan → Build → Verify → Save → Report) with checkpoint verification is the right level of process for an AI agent.

## 8. Revised Phase Sequence

To account for the architecture divergence, the implementation sequence should be revised:

**Phase 0: Architecture Reconciliation** (NEW)
- Choose resolution option (A recommended)
- Define site→harness coordination protocol (webhook, polling, or on-demand)
- Document data flow: site hatches world → pins conversation → harness discovers → provisions agent
- Verify `ReadOnboardingData` contract matches `PinOnboardingData` output (it does — both use `OnboardingData` struct from `pkg/worldchannel/onboarding.go:19-25`)

**Phase 1: OpenClaw + Docker + 2D Hooks** (unchanged, plus playwright-cli)
- Add Node.js, pnpm, OpenClaw to Dockerfile
- Add playwright-cli to Dockerfile
- Pin OpenClaw to specific commit
- Create 2D template `.claude/` hooks
- Start OpenClaw gateway in entrypoint

**Phase 2: DB Schema Only** (revised — drop OAuth and form)
- Migration for instrumentation tables (`mayor_activity`, `mayor_builds`, `mayor_sessions`, `mayor_messages`)
- Add `mayor_name`, `discord_channel_id` columns to worlds table
- sqlc config and query updates
- No Discord OAuth changes (keep GitHub OAuth in harness)
- No multi-step form (site/ handles onboarding)

**Phase 3: Agent Provisioning from Onboarding Data** (revised — drop channel creation)
- Implement `ProvisionAgent` that reads `OnboardingData` via `ReadOnboardingData`
- Generate SOUL.md from conversation data (not form fields)
- Generate AGENTS.md, IDENTITY.md, USER.md, skills
- OpenClaw CLI integration (agents add, config set bindings)
- Wire into harness discovery mechanism (from Phase 0)

**Phase 4: Build Pipeline + Instrumentation** (unchanged)
- Mayor auth middleware, build API, build→Discord events
- Contribute-learning handler with real `CreateLearningPR` implementation
- Add `GITHUB_TOKEN` to docker-compose.yml

**Phase 5: Discord Listener + Chat + Prompt Routing** (unchanged)
- `discordgo` Gateway listener
- Chat component integration (clarify: new tab vs replacement)
- Health check mechanism for OpenClaw gateway
- Fallback to direct pipeline

**Phase 6: Mayor Dashboard** (unchanged)
- OpenClaw CLI wrappers
- Dashboard page with memory, sessions, builds, activity sections
- Auto-refresh for sessions tab

## 9. What's Good

The plan has significant strengths that should be preserved:

1. **Discord-as-bus architecture** — Using Discord as the single communication bus (lines 109-139) is elegant. All messages flow through Discord channels, avoiding custom message queues. OpenClaw listens natively, and the harness mirrors to SQLite for the browser UI. This means the mayor "just works" on mobile via Discord without any custom app.

2. **AGENTS.md structured workflow** — The 6-step workflow (Understand → Plan → Build → Verify → Save → Report, lines 767-800) with explicit checkpoint save philosophy (lines 803-816) is exactly the right level of process. It prevents the mayor from spamming builds while encouraging thoughtful iteration.

3. **Checkpoint verification with playwright-cli** — The recently added verification step (lines 783-816) catches a real gap. Visual inspection + console error checking before saving a checkpoint ensures mayors don't save broken states.

4. **General vs world-specific knowledge** — The knowledge taxonomy (lines 833-878) with concrete examples and the "if unsure, it's world-specific" heuristic solves the knowledge silo problem elegantly.

5. **Contribute-learning feedback loop** — The skill design (lines 1220-1283) with file allowlist, rate limiting, and structured PR format creates a safe way for AI agents to improve shared documentation. The allowlist (`templates/3d/CLAUDE.md`, `templates/2d/CLAUDE.md`, `harness/CLAUDE.md`, `CLAUDE.md`) limits blast radius.

6. **Instrumentation from day one** — Creating `mayor_activity`, `mayor_builds`, and `mayor_sessions` tables in Phase 2 (lines 308-349) means the dashboard in Phase 6 has historical data from the start. No retroactive instrumentation needed.

7. **Non-fatal provisioning** — The `ProvisionAgent` error handling (lines 933-935) treats provisioning failure as non-fatal: "world still works without mayor." This is the correct resilience pattern.

8. **File allowlist security** — The dashboard's `editableFiles` and `readOnlyFiles` maps (lines 1758-1767) plus path traversal checks prevent the mayor dashboard from becoming an arbitrary file read/write endpoint.

9. **Rich SOUL.md template** — The template (lines 700-747) incorporates personality, tone, aesthetic sensibility, origin story, and example phrases — giving each mayor a distinct voice from the first interaction.

10. **Detailed file inventory** — The new files (lines 1934-1966) and modified files (lines 1968-1991) tables provide a complete implementation map. This level of specificity is rare and valuable for planning.

## 10. Recommended Next Steps

1. **Resolve the architecture split** — Choose Option A (recommended), B, or C. This unblocks all other work. Define the site→harness coordination protocol.

2. **Fix the 6 known bugs in site/** — The implementation review (`thoughts/CoreyCole/reviews/2026-02-14_09-27-22_meet-the-mayor-implementation_review.md`) identified 6 bugs including duplicate greetings on refresh, WORLD_READY marker saved in conversation history, and stream error handling. Fix these before relying on site/ for production onboarding.

3. **Add 2D template hooks** — The 2D template has no `.claude/` directory. This is a confirmed blocker for 2D worlds regardless of the mayor system. Can be done independently.

4. **Pin OpenClaw version** — Change `git clone --depth 1` to `git clone --depth 1 --branch <tag-or-commit>` in the Dockerfile to prevent surprise breakage.

5. **Fill in missing details** — Write IDENTITY.md and USER.md templates. Implement `CreateLearningPR` steps 3-5. Show openclaw.json structure. Define health check mechanism.

6. **Fix success criteria** — Replace all 6 occurrences of `just generate && go build ./...` with `just check`. The current success criteria would corrupt Docker builds if followed literally.
