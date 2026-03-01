---
date: 2026-02-28T20:36:52-08:00
researcher: CoreyCole
git_commit: 633082c8a0e87df2f9acada8a258bce46d52cbf3
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v4 — Review & Plan Fixes"
tags: [implementation, strategy, agent-swarm, temporal, sqlc, plan-review]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v4 — Review & Plan Fixes

## Task(s)

1. **v4 Plan Review** — COMPLETED. Performed staff engineer review of `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`. Found 3 critical issues, 8 concerns, 4 questions. Review saved to `thoughts/CoreyCole/reviews/2026-02-28_20-18-11_agent-swarm-primitives-v4_review.md`.

2. **v4 Plan Fixes (v4.1)** — COMPLETED. Updated the v4 plan to resolve all 3 critical issues plus key concerns. Changes tracked in Decision History as "v4.1 (review fixes)".

3. **Implementation** — PLANNED. No code has been written. The plan is ready for implementation starting with Phase 1 (Foundation).

## Critical References

- **v4 Plan (authoritative, updated with v4.1 fixes)**: `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`
- **v4 Review**: `thoughts/CoreyCole/reviews/2026-02-28_20-18-11_agent-swarm-primitives-v4_review.md`
- **Chestnut Flowchart (design reference)**: `~/Downloads/chestnut-agent-primitives-flowchart.html`

## Recent Changes

No code changes — only plan document updates:
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` — updated with v4.1 fixes
- `thoughts/CoreyCole/reviews/2026-02-28_20-18-11_agent-swarm-primitives-v4_review.md` — new review document

## Learnings

### Critical Issues Found & Resolved in v4.1

1. **Existing tmux reaper kills swarm sessions** (`harness/internal/claude/claude.go:293-337`): `ReapOrphanedSessions` matches `cm-*` sessions (excluding only `cm-server-*` and `cm-trunk-*`), splits on hyphens to extract a `cpID`, and kills any session whose `cpID` isn't in the checkpoints table. A swarm session `cm-swarm-0-CM-123-a1` splits into `["cm", "swarm", "0-CM-123-a1"]`, yielding phantom `cpID = "0-CM-123-a1"` → reaper kills it within 5 minutes. **Fix**: Add `strings.HasPrefix(line, "cm-swarm-")` to skip list at `claude.go:312-315`.

2. **Temporal Workflow ID collisions on retry**: Format `swarm-{idx}-{ticket}` produces the same ID when retrying a failed workflow with the same agent slot. Temporal rejects duplicate IDs within its retention period (24h default). **Fix**: Changed to `swarm-{idx}-{ticket}-a{attempt}` format.

3. **Missing enum types**: `EventType` (17 constants) and `MilestoneStatus` (3 constants) were referenced in sqlc.yaml overrides but not defined in the `enums.go` section. Would cause compile errors after `sqlc generate`. **Fix**: Added both types with `Valid()` methods.

### Additional Fixes Applied

4. **`SessionResult` name collision**: Used for both a typed string enum (DB column type) and a struct (Temporal activity return value). Go doesn't allow two types with the same name in a package. **Fix**: Renamed the struct to `SessionOutcome`, updated all references in `CompletionRegistry`, `RunClaudeSession`, `SessionWorkflow`, and `handleSwarmSessionComplete`.

5. **sqlc overrides format**: Plan used plain string `go_type: "module/path.Type"` but existing `sqlc.yaml` uses structured `go_type: {import: "...", type: "..."}`. Plain format is potentially deprecated. **Fix**: Switched all overrides to structured format.

6. **`SessionParams`/`SessionOutcome` used `string` not typed enums**: Undermined compile-time safety from sqlc overrides. **Fix**: Changed `SessionParams.Phase` to typed `Phase`, `SessionOutcome.Result` to typed `SessionResult`.

7. **Hook injection mechanism was unclear**: Plan said hooks are "injected at session creation time" but didn't explain how. **Fix**: Clarified that `createSwarmTmuxSession` writes the hook script to a workspace-local `.claude/hooks/` directory. No conflict with template hooks since swarm sessions run from repo root, not template directories.

8. **Graphite CLI version**: Plan claimed v1.7.18 but nixpkgs has v1.7.2. **Fix**: Corrected.

### Verified Claims

- **`temporal-cli`** is in nixpkgs as `pkgs.temporal-cli` v1.5.1 — confirmed
- **`graphite-cli`** is in nixpkgs as `pkgs.graphite-cli` v1.7.2 (unfree license) — confirmed
- **`ErrScheduleAlreadyRunning`** exists as sentinel error in `go.temporal.io/sdk/temporal` — confirmed, use `errors.Is()`
- **`PARENT_CLOSE_POLICY_ABANDON`** works correctly for short-lived parents — confirmed: child continues running after parent completes (not just cancellation). Must wait for `GetChildWorkflowExecution().Get()` before parent returns.
- **sqlc structured overrides** for local module types work: `go_type: {import: "creative-mode/harness/internal/swarm", type: "Phase"}`
- **`SetMaxOpenConns(1)`** with concurrent Temporal activities is fine for v1 — WAL mode handles concurrent writers serially via `busy_timeout=5000`

### Open Questions from Review (Not Yet Answered)

1. Is there a timing guarantee that the RESULT comment exists in Linear when the on-stop hook fires? The hook fires when Claude Code exits, which may be before post-exit cleanup (like `linear-cli` writing the RESULT comment) completes.
2. How does `ReadTicketQueue` relate the SQLite `swarm_workflows.id` to the Temporal workflow ID? Are they the same or different? The v4.1 update clarifies they should be the same, but the spawn endpoint must generate the ID in the `swarm-{idx}-{ticket}-a{attempt}` format.
3. Does Temporal correctly route child `SessionWorkflow`s to `swarm-general` queue when spawned by `HeartbeatWorkflow` running on `swarm-ops`? (Should be yes — `ChildWorkflowOptions.TaskQueue` controls routing.)
4. How does the swarm on-stop hook coexist with existing template hooks if a swarm session runs from within a template directory? (v4.1 clarifies swarm sessions run from repo root, avoiding conflict.)

## Artifacts

- `thoughts/CoreyCole/reviews/2026-02-28_20-18-11_agent-swarm-primitives-v4_review.md` — v4 review (3 critical, 8 concerns, 4 questions)
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` — v4 plan (updated with v4.1 fixes, ~1900 lines)
- `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md` — v3 review (informed v4)
- `thoughts/CoreyCole/plans/2026-02-28_17-30-00_agent-swarm-primitives-v3.md` — v3 plan (superseded)
- `thoughts/CoreyCole/handoffs/general/2026-02-28_20-09-07_agent-swarm-primitives-v4-plan-review.md` — previous handoff

## Action Items & Next Steps

1. **Optionally review v4.1 one more time** — The plan has been updated with all critical fixes. A quick pass to verify consistency across the ~1900-line document may be worthwhile, or skip and proceed to implementation.

2. **Begin Phase 1 implementation** — Foundation (conventions, setup, templates, schema, state machine tests). Key deliverables:
   - `harness/internal/db/migrations/006_swarm_tables.sql` — 7 tables with CHECK constraints
   - `harness/internal/db/db.go` — add migration to `migrationFiles` slice
   - `harness/internal/db/queries/swarm.sql` — CRUD queries for all 7 tables
   - `harness/internal/swarm/enums.go` — 6 typed enum types (~150 lines)
   - `harness/sqlc.yaml` — add 7 overrides in structured format
   - `harness/internal/swarm/statemachine.go` — phase transitions (~120 lines)
   - `harness/internal/swarm/statemachine_test.go` — table-driven tests (~200 lines)
   - `.claude/skills/swarm-conventions/SKILL.md` + 3 templates
   - `.claude/skills/swarm-setup/SKILL.md`

3. **Phase 2** — Core skills (research, code-plan, code, code-verify, code-pr, plan-review) — 6 SKILL.md files

4. **Phase 3** — Project & support skills (project, project-plan, project-review, project-verify, status, resume) — 6 SKILL.md files

5. **Phase 4** — Temporal + completion hooks + dashboard + API. This phase includes the critical `claude.go` reaper fix. Key deliverables: `completion.go`, `workflows.go`, `activities.go`, `worker.go`, `config.go`, `linear.go`, `swarm_api.go`, `swarm_dashboard.go`, `dashboard.templ`, and modifications to `claude.go`, `main.go`, `server.go`, `go.mod`, `flake.nix`.

6. **Phase 5** — Integration testing & CLAUDE.md documentation

## Other Notes

### Key Codebase Entry Points
- Existing reaper (MUST modify): `harness/internal/claude/claude.go:293-337` — `ReapOrphanedSessions`, skip list at line 312-315
- Hook completion pattern: `harness/internal/claude/claude.go:138-253` and `harness/internal/server/server.go:686-711`
- Hook scripts: `templates/3d/.claude/hooks/on-stop.sh` (5-retry POST pattern, same pattern for swarm)
- EventBus: `harness/internal/events/bus.go` — `Subscribe("swarm")` for dashboard SSE
- Tmux sessions: `harness/internal/tmux/session.go` — `Session` struct, env var injection
- Migration registration: `harness/internal/db/db.go:93-99` — hardcoded `migrationFiles` slice
- sqlc config: `harness/sqlc.yaml` — existing overrides at lines 55-62 (use structured format)
- President auth pattern: `harness/internal/server/president_api.go:20-36` — `X-President-Secret` header (model for `X-Swarm-Secret`)
- Main wiring: `harness/main.go` — all component initialization, add `initSwarm()` function
- Server struct: `harness/internal/server/server.go:49-66` — add `SwarmStore`, `CompletionRegistry` fields
- Go module: `harness/go.mod` — Go 1.24.3, add `go.temporal.io/sdk`
- Nix packages: `flake.nix` — Linux-only (aarch64 + x86_64), add `temporal-cli` + `graphite-cli`
- No `*_test.go` files exist in the harness — `statemachine_test.go` will be the first

### Document Evolution
v1 plan → v1 review → v2 plan → v2 handoff (Temporal decision) → v3 plan → v3 review → v4 plan → **v4 review → v4.1 fixes** (current)
