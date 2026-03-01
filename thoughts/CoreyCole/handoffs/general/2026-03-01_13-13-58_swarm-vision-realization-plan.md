---
date: 2026-03-01T13:13:58-08:00
researcher: CoreyCole
git_commit: 9596859
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Vision Realization — Full Agent Primitives Plan"
tags: [implementation, swarm, plan, temporal, linear, graphite, orchestration, project-skills]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Vision Realization Plan Complete

## Task(s)

| Task | Status |
|------|--------|
| Resume from Phase 4G Temporal Integration handoff | **Completed** |
| Analyze HTML flowchart of "Chestnut Agent Primitives" vision | **Completed** |
| Gap analysis: current state vs flowchart vision | **Completed** |
| Write comprehensive 9-phase implementation plan (Phases 5A-5I) | **Completed** |
| **Next: Implement Phase 5A (Commit & Stabilize)** | **Planned** |

Working from:
- Phase 4G handoff: `thoughts/CoreyCole/handoffs/general/2026-03-01_12-34-32_swarm-phase4g-temporal-integration.md`
- New plan: `thoughts/CoreyCole/plans/2026-03-01_21-08-34_swarm-vision-realization.md`
- v5 master plan: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`

## Critical References

1. **Swarm Vision Realization Plan** (the plan to implement next): `thoughts/CoreyCole/plans/2026-03-01_21-08-34_swarm-vision-realization.md`
2. **Phase 4G Temporal Handoff** (current state of uncommitted code): `thoughts/CoreyCole/handoffs/general/2026-03-01_12-34-32_swarm-phase4g-temporal-integration.md`
3. **v5 Master Plan** (Phase 5 spec at lines 874-888): `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`

## Recent changes

No code changes this session — this was a planning session. The plan document was written to `thoughts/CoreyCole/plans/2026-03-01_21-08-34_swarm-vision-realization.md`.

**Uncommitted code from prior session (Phase 4G) still needs committing:**
- `harness/go.mod`, `harness/go.sum` (modified — Temporal SDK dependency)
- `harness/internal/swarmorch/manager.go` (modified — Temporal branching in StartWorkflow, advanceWorkflow, RecoverWorkflows, StartMaintenance)
- `harness/main.go:272-297` (modified — Temporal client/runtime wiring)
- `harness/internal/swarmorch/temporal.go` (new — TemporalRuntime, client, workers, schedule)
- `harness/internal/swarmorch/workflows.go` (new — HeartbeatWorkflow, SessionWorkflow)
- `harness/internal/swarmorch/activities.go` (new — 6 activities wrapping manager functions)
- `harness/internal/swarmorch/workflows_test.go` (new — 6 Temporal test suite tests)
- `scripts/setup-temporal.sh` (new — idempotent Temporal server setup)

## Learnings

- **3 project skills are missing**: `swarm-project-plan`, `swarm-project-review`, `swarm-project-verify` are mapped in `statemachine.go:196-201` but have no SKILL.md files. The DB schema + state machine transitions already support them.
- **`linear-cli` is not installed** on the VPS — needed before Linear integration (Phase 5D).
- **`gt` (Graphite CLI) is not installed** on the VPS — needed before Graphite integration (Phase 5E).
- **`previous_workflow_id` column exists** in `swarm_workflows` table (`006_swarm_tables.sql:21`) but is not populated by `StartWorkflow()` or passed to sessions as env vars.
- **`swarm_project_milestones` table exists** (`006_swarm_tables.sql:73-82`) with milestone tracking fields, but no Go code creates or checks milestones.
- **`swarm_tickets.parent_id` and `project_id` columns exist** (`006_swarm_tables.sql:87-88`) but are unused in orchestrator code.
- **`PhaseProjectVerify` has no max retry limit** (`statemachine.go:162-169`) — retries indefinitely on `logic_failure`.
- **Linear integration is currently CLI-only from skills** — skills shell out to `linear-cli`. There is no Go-level Linear API client.
- **All 8 existing skills follow the same pattern**: preamble (read handoff + learnings + conventions) → phase-specific work → write handoff → write RESULT file.

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-01_21-08-34_swarm-vision-realization.md` — **The full 9-phase plan** (5A through 5I)
- `thoughts/CoreyCole/handoffs/general/2026-03-01_12-34-32_swarm-phase4g-temporal-integration.md` — Prior handoff documenting all Phase 4G uncommitted changes

## Action Items & Next Steps

### Immediate: Phase 5A — Commit & Stabilize
1. **Commit Phase 4G changes** — All files listed above are uncommitted on `feature/agent-swarm`. Commit with a message describing Phase 4G Temporal integration.
2. **Run `scripts/setup-temporal.sh` on VPS** — Install Temporal server, create systemd service, create `swarm` namespace.
3. **Test with `CM_SWARM_TEMPORAL=false`** — Verify goroutine-based behavior is unchanged.
4. **Test with `CM_SWARM_TEMPORAL=true`** — Verify workers connect, heartbeat schedule appears in Temporal UI at `:8233`.

### Then: Phase 5B — Project Skills
5. Write `swarm-project-plan` SKILL.md — Project plan with ticket decomposition, dependency graph, milestones, Graphite stack plan.
6. Write `swarm-project-review` SKILL.md — Reviews project plan against 7-criterion checklist.
7. Write `swarm-project-verify` SKILL.md — Verifies project milestones after all child tickets complete.

### Subsequent phases (5C → 5I) are detailed in the plan document.

**Critical dependency chain:** 5A → 5B → 5D (Linear) → 5F (dependency graphs) → 5G (two-level orchestration)

## Other Notes

- **Flowchart defines 7 primitives**: (1) Question, (2) Code Change, (2.1) Verification, (3) Project, (4) Parent Tickets, (5) Project Plan & Dependency Graph, (6) Project Orchestrator Heartbeat, (7) Lead FDE Heartbeat.
- **Design principles from flowchart**: Don't block on human input, agents answer own questions first, Linear is source of truth, Slack only for critical escalation.
- **The existing president agent (OpenClaw-based)** remains separate — the Lead FDE heartbeat (Phase 5G) is a Temporal workflow, not an OpenClaw agent replacement.
- **Feature flags**: `CM_SWARM_TEMPORAL=true` is required for orchestration features. Linear and Graphite are optional — system degrades gracefully without them.
- **Prior plan evolution**: v1→v2→v3→v4→v5 plans exist in `thoughts/CoreyCole/plans/` with reviews at each stage. The v5 plan is the canonical reference.
- **`thoughts/swarm/` directories are scaffolded** (plans, research, project-plans, retrospectives, digests, various handoff dirs) but contain only `.gitkeep` files — no swarm workflows have run yet to populate them.
