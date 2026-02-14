---
date: 2026-02-13T16:39:46-08:00
researcher: CoreyCole
git_commit: 406c5b52a00520bd58f54c43919e073739048b2c
branch: main
repository: creative-mode
topic: "World Mayors Master Plan — Consolidated Implementation Strategy"
tags: [implementation, strategy, openclaw, mayor, dashboard, discord, planning]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: World Mayors Master Plan Creation

## Task(s)

### Completed
1. **Resumed from dashboard design handoff** — Read and analyzed `thoughts/CoreyCole/handoffs/general/2026-02-13_15-25-28_mayor-dashboard-design.md` and both linked plan documents in full (~2600 lines total).

2. **Created consolidated master plan** — Wrote a single comprehensive 6-phase master plan that supersedes the two older plans:
   - Supersedes: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` (OpenClaw World Mayors, ~2130 lines)
   - Supersedes: `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` (Prompt Attenuation + Memory Inspector)

3. **Gathered user design decisions** — Clarified 6 key questions with the user:
   - Prompt attenuation: Mayor handles it naturally through conversational workflow (not Gemini, not a separate "Suggest" button)
   - Scope: Everything + dashboard in one plan
   - Discord: Primary interface (unchanged)
   - OpenClaw API: CLI wrappers via `exec.Command` (not WebSocket client)
   - Dashboard UI: Dedicated `/mayor/:worldID` page (not a chat panel tab)
   - Instrumentation: Full instrumentation from day one (not deferred)

### Planned (Next Session)
4. **Thorough review of the master plan** — User wants a comprehensive review pass before implementation begins. This should verify:
   - All content from the two superseded plans is captured or intentionally dropped
   - No gaps in the phase dependencies
   - Multi-step personality gathering flow is complete
   - Instrumentation tables cover all touchpoints
   - Dashboard sections are well-specified
   - Success criteria are specific and testable

## Critical References

- **Master plan (to be reviewed)**: `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md`
- **Superseded plan 1**: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — the original ~2130-line 5-phase plan with extensive OpenClaw research
- **Superseded plan 2**: `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — prompt attenuation + memory inspector

## Recent changes

No code changes. This session was planning only.

- `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md` — new master plan document (~900 lines)

## Learnings

### What Changed vs the Older Plans
- **Dropped Gemini prompt attenuation**: The `SuggestionTracker`, async callback pattern, `ImageGenSuggesting`/`ImageGenSuggested` fragments, `prompt-enhance` skill, and the "Suggest" button are all gone. The mayor enhances prompts naturally through its conversational workflow (Step 2: Plan in AGENTS.md).
- **Mayor tab → dedicated page**: Instead of adding a "Mayor" tab to the chat panel (5th tab alongside Global/World/Lineage/Assets), the dashboard is a full-width page at `/mayor/:worldID`.
- **Rich personality gathering**: The simple world creation form (name + description + template_type) becomes a 4-step Datastar wizard gathering: name, description, template → mayor name, personality, tone → aesthetic sensibility, backstory, example phrases → review & create. Six new DB columns on `worlds` table.
- **Three instrumentation tables from day one**: `mayor_activity` (all actions), `mayor_builds` (build delegations), `mayor_sessions` (OpenClaw sessions). Populated at every touchpoint, not deferred.
- **CLI wrappers for OpenClaw API**: Dashboard queries (sessions, previews, gateway health) use `exec.Command` to call `openclaw status --json`, `openclaw sessions list --json`, etc. Not WebSocket client.

### Plan Structure
The 6 phases build on each other:
1. Docker infrastructure (Node.js + OpenClaw)
2. Auth migration + DB schema (Discord OAuth + all tables including instrumentation)
3. Agent provisioning (workspace files + Discord channel)
4. Build pipeline + instrumentation (mayor API + build→Discord events + logging)
5. Discord listener + chat + prompt routing (discordgo + browser UI)
6. Mayor dashboard (dedicated page with 4 sections)

### Key OpenClaw Facts Carried Forward
- `MEMORY.md` is NOT auto-created by OpenClaw — created on first agent memory write
- `openclaw config set` does FULL REPLACE — must use read-modify-write for bindings
- Per-channel routing uses `peer` match with highest priority in route resolution
- Config hot-reload via chokidar — no gateway restart needed for agent/binding changes
- HTTP hooks API is fire-and-forget — no response body
- Session management available via `sessions.list`, `sessions.preview`, etc.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md` — the master plan (primary artifact, to be reviewed next session)
- `thoughts/CoreyCole/handoffs/general/2026-02-13_15-25-28_mayor-dashboard-design.md` — the handoff this session resumed from

## Action Items & Next Steps

1. **Thorough review of the master plan** — The user specifically requested this as the next step. The review should:
   - Read the master plan in full: `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md`
   - Cross-reference against both superseded plans to ensure nothing important was lost
   - Verify phase dependencies (each phase should be independently testable)
   - Check that the multi-step wizard flow is complete (signals, form steps, handler)
   - Verify instrumentation coverage (every touchpoint has a `mayor_activity` insert)
   - Ensure dashboard handlers cover all sections (memory, sessions, builds, activity)
   - Check for missing SQL queries, missing route registrations, or missing file references
   - Look for inconsistencies between the architecture section and the phase details
   - Validate success criteria are specific enough for automated and manual verification

2. **After review, begin implementation** — Starting with Phase 1 (Docker infrastructure) unless the review reveals issues that need resolution first.

## Other Notes

### Current Codebase State (relevant to the plan)
- `harness/views/world/signals.go` — 19 signals in `OverlaySignals`, no mayor-related signals yet
- `harness/views/chat/chat.templ` — 4 tabs (Global, World, Lineage, Assets)
- `harness/internal/server/imagegen.go` — established SSE pattern (ReadSignals → NewSSE → patch fragments)
- `harness/internal/server/server.go` — Server struct has no OpenClaw/mayor fields
- `harness/views/lobby/lobby.templ:50-66` — simple world creation form (to be replaced with wizard)

### Files Modified in Working Tree (not committed)
From git status at session start:
- `harness/CLAUDE.md`, `harness/internal/gemini/gemini.go`, `harness/internal/server/imagegen.go`, `harness/internal/server/server.go`, `harness/views/chat/chat.templ`, `harness/views/imagegen/expressions.go`, `harness/views/imagegen/imagegen.templ`, `harness/views/world/signals.go`
- New: `harness/views/imagegen/asset_tree.templ`, `harness/views/imagegen/types.go`
- These are unrelated to the mayor plan — likely in-progress image gen work.
