---
date: 2026-03-01T12:34:32-08:00
researcher: CoreyCole
git_commit: 60fec80
branch: feature/agent-swarm
repository: creative-mode
topic: "Agent Swarm Phase 4G: Temporal Integration"
tags: [implementation, swarm, temporal, orchestration, workflows, activities]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Phase 4G Temporal Integration Complete

## Task(s)

Implemented Phase 4G (Temporal Integration) of the Agent Swarm Phase 4 completion plan. This is the final sub-phase of Phase 4.

| Sub-Phase | Description | Status |
|-----------|-------------|--------|
| **4A: Structured Logging + JSONL** | SessionLog wrapper, per-session JSONL files, log API endpoint | **Completed** (prior session) |
| **4B: Hook System + Completion Registry** | CompletionRegistry, StartRegistry, WriteHooksConfig, 6 hook endpoints | **Completed** (prior session) |
| **4C: Metrics + Health** | SQL aggregation with cache, health endpoint, session status | **Completed** (prior session) |
| **4D: Alerts + Learning Digest Loop** | Discord alerts, daily digest, relevance decay, periodic tickers | **Completed** (prior session) |
| **4E: Dashboard Enhancements** | Metrics/Learnings tabs, live tool activity feed | **Completed** (prior session) |
| **4F: President Skill Integration** | swarm-learnings skill for president | **Completed** (prior session) |
| **4G: Temporal Integration** | Feature-flagged Temporal workflow engine | **Completed** (this session) |

**Phase 4 is now fully complete.**

The v5 plan has **Phase 5: Integration Testing & Documentation** remaining as the final phase.

Working from:
- Full plan: `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md` (Phase 4G section)
- v5 master plan: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`

## Critical References

1. **v5 master plan**: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — Phase 5 spec at lines 874-888.
2. **Phase 4 completion plan**: `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md` — Phase 4G detailed spec.
3. **Previous handoff (Phase 4E+4F)**: `thoughts/CoreyCole/handoffs/general/2026-03-01_03-30-00_swarm-phase4ef-dashboard-president.md`

## Recent changes

All on branch `feature/agent-swarm`, uncommitted (staged and ready):

**New files:**
- `scripts/setup-temporal.sh` — Installs Temporal CLI, creates systemd service with SQLite persistence at `data/temporal.db`, binds to 127.0.0.1:7233, UI on 8233, creates `swarm` namespace. Idempotent.
- `harness/internal/swarmorch/temporal.go:1-241` — `TemporalEnabled()` reads `CM_SWARM_TEMPORAL` env var. `TemporalRuntime` struct holds client, 3 workers, schedule handle. `NewTemporalClient()` connects to Temporal with `swarm` namespace. `StartRuntime()` creates workers for 3 queues with concurrency limits, registers workflows/activities, creates heartbeat schedule (2min interval, overlap skip). `TriggerHeartbeat()` for immediate execution. `Stop()` for graceful shutdown. `temporalLogger` bridges slog→Temporal log interface.
- `harness/internal/swarmorch/workflows.go:1-130` — `HeartbeatWorkflow` runs 5 maintenance activities sequentially (DetectStalls, ReapSessions, DecayLearnings, GenerateDigest, ReadTicketQueue), then spawns `SessionWorkflow` child workflows with fire-and-forget (PARENT_CLOSE_POLICY_ABANDON). Verify phases routed to `swarm-verify` queue. `SessionWorkflow` wraps `RunClaudeSession` activity with 65min timeout, 2min heartbeat. Activity failures return `ResultInfraFailure` cleanly.
- `harness/internal/swarmorch/activities.go:1-210` — `Activities` struct wrapping `*Manager`. `RunClaudeSession` calls `mgr.spawnSession()` then polls DB every 15s for completion, heartbeating to Temporal. `ReadTicketQueue` finds running workflows needing sessions. `DetectStalls`, `ReapSessions` (kills orphaned tmux sessions), `DecayLearnings`, `GenerateDigest` wrap existing manager/swarm functions.
- `harness/internal/swarmorch/workflows_test.go:1-180` — 6 tests using Temporal's `WorkflowTestSuite`: HeartbeatWorkflow calls all activities, spawns children for pending work, routes verify to verify queue, maintenance failures don't block workflow. SessionWorkflow returns activity result, handles activity failure with InfraFailure.

**Modified files:**
- `harness/internal/swarmorch/manager.go` — Added `temporalRuntime *TemporalRuntime` field (line ~68). Added `SetTemporalRuntime()` method. Modified `StartWorkflow()` to trigger heartbeat instead of spawning directly when Temporal enabled. Modified `advanceWorkflow()` to trigger heartbeat instead of spawning next session. Modified `RecoverWorkflows()` to early-return when Temporal enabled (heartbeat handles recovery). Modified `StartMaintenance()` to skip goroutine-based maintenance when Temporal enabled.
- `harness/main.go:272-297` — After swarm manager setup, conditionally creates Temporal client and runtime when `CM_SWARM_TEMPORAL=true`. Uses `log.Fatalf` for fail-fast on connection failure (no fallback to goroutine mode). Shutdown handler calls `temporalRuntime.Stop()`.
- `harness/go.mod` — Added `go.temporal.io/sdk v1.40.0` and `go.temporal.io/api v1.62.2` plus transitive dependencies.

## Learnings

- **Temporal SDK enum types are in `go.temporal.io/api/enums/v1`**, not in the SDK client package. `ScheduleOverlapPolicy` → `enumspb.SCHEDULE_OVERLAP_POLICY_SKIP`, `ParentClosePolicy` → `enumspb.PARENT_CLOSE_POLICY_ABANDON`. The SDK's `client` and `workflow` packages reference these proto enums.
- **Temporal test suite requires activity registration**: When using string-based activity names in workflows (e.g., `workflow.ExecuteActivity(ctx, "RunClaudeSession", ...)`), the test environment needs `env.RegisterActivity(&Activities{})` before `OnActivity` mocks will work. Without registration, it panics.
- **Temporal test child workflow mocking**: `env.OnWorkflow(SessionWorkflow, mock.Anything, mock.Anything)` needs two `mock.Anything` args (context + params), not just one.
- **gocritic `exitAfterDefer`**: `os.Exit` and `log.Fatalf` after `defer` statements trigger this lint. Only one `//nolint:gocritic` is needed per function scope — subsequent calls don't re-trigger the warning.
- **Temporal Schedule API**: `ScheduleClient().Create()` returns `AlreadyExists` error if schedule exists. Pattern: try Create, on error fall back to `GetHandle().Update()` with `DoUpdate` callback.
- **Task queue design**: `swarm-verify` at concurrency 1 prevents OOM from parallel `just check` runs (each WASM build uses ~5GB RAM on the 10GB VPS).
- **Design decision**: `StartWorkflow()` creates DB records then triggers heartbeat — heartbeat is the single source of truth for session spawning. `advanceWorkflow()` updates DB phase but does NOT spawn next session directly — avoids starting workflows from within activities.

## Artifacts

- `scripts/setup-temporal.sh` — Temporal server setup script
- `harness/internal/swarmorch/temporal.go` — Runtime, client, workers, schedule
- `harness/internal/swarmorch/workflows.go` — HeartbeatWorkflow, SessionWorkflow
- `harness/internal/swarmorch/activities.go` — 6 activities wrapping existing manager functions
- `harness/internal/swarmorch/workflows_test.go` — 6 workflow tests
- `harness/internal/swarmorch/manager.go` — Modified with Temporal branching
- `harness/main.go` — Modified with Temporal wiring
- `harness/go.mod` / `harness/go.sum` — Temporal SDK dependency

## Action Items & Next Steps

### Phase 5: Integration Testing & Documentation (the final phase)

Per the v5 plan (`thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md:874-888`), Phase 5 includes:

1. **Learning capture verification** — Test at each state machine transition that learnings are captured correctly (plan_issue on plan review revise, code_bug on verify failure, pattern on success, post_mortem on terminal failure).

2. **Handoff creation/consumption verification** — Test that every session boundary writes a handoff document and the next session reads `CM_SWARM_HANDOFF_PATH`.

3. **Handoff chain continuity test** — Full workflow research→plan→review→implement→verify→PR, verify each phase reads previous handoff.

4. **Context window limit handoff test** — Simulate mid-phase context exhaustion, verify continuation with new session reading handoff.

5. **Digest generation test** — Verify digest generated after simulated 24h period.

6. **End-to-end test** — Verify failure→learning captured→handoff written→retry reads both→success.

7. **Hook integration tests**:
   - SessionStart: fail fast (infra_failure) if no SessionStart within 30s
   - PreToolUse: denied commands return exit 2
   - PostToolUse: tool events appear on dashboard SSE
   - PreCompact: context_pressure flag set after 2nd compact
   - SessionEnd: crash recovery — kill tmux without Stop, verify SessionEnd triggers completion
   - Full lifecycle ordering and dedup

8. **Documentation** — Update CLAUDE.md with Temporal env vars (`CM_SWARM_TEMPORAL`, `TEMPORAL_ADDRESS`), update project structure, document task queues and operational procedures.

### Pre-Phase 5 Cleanup

- **Commit Phase 4G changes** — All changes are uncommitted on `feature/agent-swarm`.
- **Run `scripts/setup-temporal.sh` on VPS** — Install Temporal for production use.
- **Test with `CM_SWARM_TEMPORAL=true`** — Verify workers connect, heartbeat schedule visible in Temporal UI at :8233, full workflow lifecycle works.
- **Test with `CM_SWARM_TEMPORAL=false`** — Verify existing goroutine-based behavior is unchanged (regression test).

## Other Notes

- **Task queue architecture**: `swarm-general` (concurrency 3) for research/plan/review/implement/PR sessions; `swarm-verify` (concurrency 1) for verify-only to prevent OOM; `swarm-ops` (concurrency 1) for heartbeat workflow + maintenance activities.
- **No ContinueAsNew**: The plan originally mentioned ContinueAsNew for HeartbeatWorkflow, but the implementation uses a Temporal Schedule (2min interval) that fires fresh HeartbeatWorkflow executions. This is cleaner — each heartbeat is a short-lived workflow that exits after one pass.
- **Feature flag**: `CM_SWARM_TEMPORAL=true` enables Temporal mode. Default is goroutine mode. When Temporal is enabled and unreachable at startup, the harness fatally exits (no mixed mode).
- **99+ tests pass**: `go test ./internal/swarm/... ./internal/swarmorch/...` — all existing tests plus 6 new workflow tests pass. `just check` is lint-clean.
- **`SetAlertManager` + `StartMaintenance` already wired**: The previous handoff listed this as outstanding, but it was already done in `main.go:248-273`.
