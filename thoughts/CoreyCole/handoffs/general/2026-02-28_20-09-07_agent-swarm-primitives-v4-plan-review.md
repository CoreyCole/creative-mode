---
date: 2026-02-28T20:09:07-08:00
researcher: CoreyCole
git_commit: 2c68eddfcea832d3ce6c15456fe0ab9cd2f82b04
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v4 — Plan Review & Continued Refinement"
tags: [implementation, strategy, agent-swarm, temporal, linear-cli, skills, orchestration, sqlc, graphite]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v4 — Plan Review & Continued Refinement

## Task(s)

1. **v3 Plan Review** — COMPLETED. Reviewed `thoughts/CoreyCole/plans/2026-02-28_17-30-00_agent-swarm-primitives-v3.md` as a staff engineer reviewer. Identified 3 critical issues, 8 concerns, 4 questions. Review saved to `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md`.

2. **v4 Plan Creation** — COMPLETED. Created `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` addressing all v3 critical issues plus user-directed changes.

3. **v4 Plan Refinement** — COMPLETED. Updated v4 plan with two user-requested changes:
   - **Typed Go enums via sqlc overrides** instead of manual Go const blocks. Custom enum types in `enums.go` mapped to SQLite columns via `sqlc.yaml` `overrides` for compile-time type safety.
   - **Graphite `gt` CLI** for PR stacking instead of `gh pr create`. Available in nixpkgs as `pkgs.graphite-cli` v1.7.18.

4. **v4 Plan Review** — PLANNED. The user wants to continue reviewing the v4 plan in the next session. No implementation has started.

## Critical References

- **v4 Plan (authoritative)**: `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`
- **v3 Review (issues that informed v4)**: `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md`
- **Chestnut Flowchart (design reference)**: `~/Downloads/chestnut-agent-primitives-flowchart.html` — HTML visualization of the agent primitives architecture showing two-level orchestration model, task classification, code change lifecycle, and project lifecycle

## Recent Changes

No code changes — this session was plan review and creation only. Documents created/modified:
- `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md` (new)
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` (new, then refined)

## Learnings

### Architecture Decisions (v4)

1. **Hook-based session completion, not polling**: The existing harness uses `.claude/hooks/on-stop.sh` → `POST /api/claude-event` for session completion, not tmux polling. The v3 plan proposed polling `tmux has-session` which creates a race condition (tmux dies before RESULT comment is written). v4 uses a `CompletionRegistry` (per-session Go channels) + `POST /api/swarm/session-complete` endpoint following the existing hook pattern. Tmux health check is a 30s fallback for crashed sessions. See `harness/internal/claude/claude.go:139-253` and `harness/internal/server/server.go:688-711` for existing pattern.

2. **Child workflows from HeartbeatWorkflow, not activity-spawned**: Starting workflows from within Temporal activities is an anti-pattern (retries cause duplicates, no parent-child lifecycle management, lost observability). v4 refactors: `ReadTicketQueue` activity returns spawn decisions (list of `SessionParams`), then `HeartbeatWorkflow` calls `workflow.ExecuteChildWorkflow()` with `PARENT_CLOSE_POLICY_ABANDON` for fire-and-forget.

3. **sqlc does NOT generate enums from SQLite CHECK constraints**: Verified by examining sqlc source (`internal/engine/sqlite/convert.go`). The SQLite parser only extracts column name, type, and NOT NULL. However, sqlc's `overrides` in `sqlc.yaml` can map columns to custom Go types. v4 defines typed enums in `harness/internal/swarm/enums.go` (`Phase`, `SessionResult`, `WorkflowStatus`, `WorkflowType`) and maps them via overrides for compile-time safety.

4. **Explicit user invocation, no auto-classification**: User explicitly calls `/swarm-research`, `/swarm-code`, or `/swarm-project`. The Chestnut flowchart's "Task Classification & Routing" step is intentionally deferred — OpenClaw can handle routing in a future layer above the swarm system.

5. **Graphite `gt` available in nixpkgs**: `pkgs.graphite-cli` v1.7.18, binary is `gt`. License is `unfree` in nixpkgs, requires `config.allowUnfree = true`. Use `gt create --title "..." --body "..."` for PR stacking.

6. **`temporal-cli` available in nixpkgs**: `pkgs.temporal-cli` v1.5.1. Run dev server with `temporal server start-dev --db-filename /path/temporal.db`. Schedule idempotency: `errors.Is(err, temporal.ErrScheduleAlreadyRunning)` on restart.

### Infrastructure Facts Verified

- **EventBus `Subscribe("swarm")` works today** — `Subscribe` takes any string key, not validated as world ID (`harness/internal/events/bus.go:65`)
- **Migration files must be manually added** to hardcoded list at `harness/internal/db/db.go:93-99` — not auto-discovered
- **Next migration number is 006** (existing: 001-005)
- **No `*_test.go` files exist** in the entire harness codebase
- **`flake.nix` is Linux-only**: `aarch64-linux` and `x86_64-linux` (no macOS/darwin)
- **Go version**: 1.24.3 (`harness/go.mod`)

## Artifacts

- `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md` — v3 review (3 critical, 8 concerns, 4 questions)
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` — v4 plan (authoritative, ~1500 lines)
- `thoughts/CoreyCole/plans/2026-02-28_17-30-00_agent-swarm-primitives-v3.md` — v3 plan (superseded by v4)
- `thoughts/CoreyCole/handoffs/general/2026-02-28_16-53-51_agent-swarm-primitives-v2-temporal-update.md` — v2 handoff (decision history)
- `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md` — v1 review

## Action Items & Next Steps

1. **Review the v4 plan** — Read `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` and perform a staff engineer review (use `/review_plan` skill). Focus areas:
   - Hook-based completion model: is the `CompletionRegistry` + fallback health check robust enough?
   - sqlc overrides: verify the `go_type` import path format works with sqlc's override mechanism for local module types
   - Temporal schedule + child workflow pattern: does `PARENT_CLOSE_POLICY_ABANDON` work correctly for short-lived parent (HeartbeatWorkflow) with long-lived children (SessionWorkflow)?
   - Linear sync: does `linear-cli i list -t CM -l "swarm:research"` work per-label, or does it need a different flag?
   - Missing `EventType` and `MilestoneStatus` enum types referenced in sqlc.yaml overrides but not defined in the enums.go section

2. **After review is approved** — Begin implementation starting with Phase 1 (Foundation: migration, enums, sqlc overrides, state machine + tests, conventions, setup skill)

## Other Notes

### Key Codebase Entry Points
- Hook completion pattern: `harness/internal/claude/claude.go:73-135` (HandlePrompt), `harness/internal/server/server.go:688-711` (handleClaudeEvent)
- Hook scripts: `templates/3d/.claude/hooks/on-stop.sh` (5-retry POST pattern)
- EventBus: `harness/internal/events/bus.go` (Subscribe/Publish with any string key)
- Tmux sessions: `harness/internal/tmux/session.go` (Session struct, env vars: CM_HARNESS_URL, CM_HOOK_SECRET)
- President auth: `harness/internal/server/president_api.go:20-36` (X-President-Secret header pattern)
- SSE + Datastar: `harness/internal/server/events.go:56-114` (handleWorldSSE with select loop)
- Migration registration: `harness/internal/db/db.go:93-99` (hardcoded migrationFiles slice)
- sqlc config: `harness/sqlc.yaml` (engine: sqlite, existing overrides at lines 55-62)

### Document Evolution
v1 plan → v1 review → v2 plan → v2 handoff (Temporal decision) → v3 plan → v3 review → **v4 plan** (current)

### Chestnut Flowchart Divergence
The v4 plan intentionally simplifies the Chestnut flowchart's two-level orchestration (Lead FDE + Project Orchestrators) into a single HeartbeatWorkflow + deterministic state machine. This is documented in the plan's "Intentional Chestnut Flowchart Divergence" section. The future path is OpenClaw as a layer above that handles idea routing and ambiguous situations.
