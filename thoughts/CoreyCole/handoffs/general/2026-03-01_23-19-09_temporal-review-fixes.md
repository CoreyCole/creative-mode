---
date: 2026-03-01T23:19:09-08:00
researcher: CoreyCole
git_commit: 278720426358bca4984b3e73801befa1b83d5f8c
branch: feature/agent-swarm
repository: creative-mode
topic: "Temporal Implementation Review & Bug Fixes"
tags: [implementation, temporal, swarm, review, bug-fix]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Temporal Review Fixes & Secret Redaction

## Task(s)

1. **Review Temporal implementation for correctness** — **Completed.** Resumed from `2026-03-01_22-56-48_temporal-implementation-review.md`. Reviewed `workflows.go`, `activities.go`, `temporal.go`, `manager.go`, `project.go`, and `main.go` for correctness after the goroutine fallback removal and Temporal activation.

2. **Fix critical bugs found during review** — **Completed.** Found and fixed 4 issues:
   - **Infinite recursion in `triggerHeartbeat()`** — `m.triggerHeartbeat()` called itself instead of `m.temporalRuntime.TriggerHeartbeat()`. Stack overflow on any workflow state change.
   - **Dead `HeartbeatWorkflow`** — Superseded by `LeadFDEWorkflow` but still registered on workers. Removed function + 4 tests + worker registrations.
   - **Redundant `CheckProjectProgress` in `LeadFDEWorkflow`** — Stale goroutine-mode fallback. `ProjectOrchestratorWorkflow` handles advancement now. Could cause double-advancement.
   - **Unnecessary worker registrations** — `LeadFDEWorkflow`, `HeartbeatWorkflow`, `ProjectOrchestratorWorkflow` registered on `QueueGeneral` but only run on `QueueOps`.

3. **Redact leaked secrets from git history** — **Completed.** Used `git filter-branch --tree-filter` to remove a Linear API key and hook secret from `thoughts/CoreyCole/handoffs/general/2026-03-01_19-28-43_swarm-first-run-infra-fixes.md` across all 13 commits. Force pushed.

## Critical References

- `harness/internal/swarmorch/temporal.go` — Temporal runtime, client, workers, heartbeat schedule
- `harness/internal/swarmorch/workflows.go` — Temporal workflow definitions (LeadFDEWorkflow, SessionWorkflow, ProjectOrchestratorWorkflow)
- `harness/internal/swarmorch/activities.go` — Activity implementations

## Recent changes

- `harness/internal/swarmorch/manager.go:1744-1750` — Fixed `triggerHeartbeat()` to call `m.temporalRuntime.TriggerHeartbeat()` instead of itself
- `harness/internal/swarmorch/workflows.go` — Removed dead `HeartbeatWorkflow` (~65 lines), removed `CheckProjectProgress` call from `LeadFDEWorkflow`
- `harness/internal/swarmorch/temporal.go:74-95` — Cleaned up worker registrations: removed `HeartbeatWorkflow`/`LeadFDEWorkflow`/`ProjectOrchestratorWorkflow` from general worker, removed `HeartbeatWorkflow` from ops worker
- `harness/internal/swarmorch/workflows_test.go` — Removed 5 `HeartbeatWorkflow` tests, removed `CheckProjectProgress` mocks from 2 `LeadFDEWorkflow` tests
- `thoughts/CoreyCole/handoffs/general/2026-03-01_19-28-43_swarm-first-run-infra-fixes.md:89,97-98` — Redacted secrets (hook secret + Linear API key) and rewrote git history

## Learnings

- **`triggerHeartbeat()` was a latent crash**: The previous session wrote `m.triggerHeartbeat()` instead of `m.temporalRuntime.TriggerHeartbeat()`, creating infinite recursion. The nil check made it appear safe but actually enabled the recursion when Temporal was set.
- **`HeartbeatWorkflow` vs `LeadFDEWorkflow`**: `LeadFDEWorkflow` is the active one (used by the heartbeat schedule). `HeartbeatWorkflow` was the original version without project health checks — superseded but never removed.
- **Worker registrations matter for task queues**: Registering a workflow on a worker for a queue it never runs on wastes resources. `LeadFDEWorkflow` and `ProjectOrchestratorWorkflow` only run on `QueueOps`.
- **`git filter-branch --tree-filter`**: Works for rewriting specific files across commits. Must stash changes first. Creates backup refs under `refs/original/` that should be cleaned up. Use `FILTER_BRANCH_SQUELCH_WARNING=1` to suppress the deprecation warning.

## Artifacts

- `harness/internal/swarmorch/manager.go` — Fixed triggerHeartbeat, all goroutine fallback removed
- `harness/internal/swarmorch/workflows.go` — HeartbeatWorkflow removed, LeadFDEWorkflow simplified
- `harness/internal/swarmorch/workflows_test.go` — Tests updated to match
- `harness/internal/swarmorch/temporal.go` — Clean worker registrations

## Action Items & Next Steps

The Temporal implementation is now reviewed, fixed, and pushed. Next steps from the original handoff (`2026-03-01_22-56-48_temporal-implementation-review.md`):

1. **Test with a real workflow** — Start a test workflow via `POST /api/swarm/start` and observe the Temporal UI (port 8233) to verify workflow execution, session spawning, and phase advancement all work end-to-end. This is the first real test since the goroutine fallback was removed.

2. **Consider edge cases** — What happens if Temporal goes down mid-workflow? The `Restart=on-failure` on `temporal-dev.service` should bring it back, and Temporal's durable execution should resume workflows. Not yet tested.

3. **The `CheckProjectProgress` activity** (`activities.go:220-225`) still exists but is no longer called by any workflow. It can be removed if desired, or kept as a utility for manual debugging.

4. **`heartbeatInterval` naming** — Used for both the schedule interval (2min) and `SessionWorkflow`'s `HeartbeatTimeout`. A dedicated `sessionHeartbeatTimeout` constant would be clearer but is cosmetic.

5. **Untracked `thoughts/` files** — Many swarm research/handoff files are untracked. Consider committing them in a batch.

## Other Notes

- **Branch**: `feature/agent-swarm` — history was rewritten via `filter-branch` and force-pushed.
- **All checks pass**: `just check` green (Go fmt, templ fmt, cargo fmt, golangci-lint, clippy)
- **All tests pass**: `go test ./internal/swarmorch/` and `go test ./internal/swarm/` both pass
- **Pre-existing test issues** (not related): `views/*` packages have `non-constant format string` build errors, `internal/linear` TestHTTPError times out
- **Temporal UI**: `http://localhost:8233` — verify workflows, schedules, execution history
- **Temporal CLI**: `temporal workflow list --namespace swarm`, `temporal schedule list --namespace swarm`
