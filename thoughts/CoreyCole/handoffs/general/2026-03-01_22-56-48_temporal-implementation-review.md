---
date: 2026-03-01T22:56:48-08:00
researcher: CoreyCole
git_commit: a6d9bff89ed34869df78af98b0ecfd4f0a5155d2
branch: feature/agent-swarm
repository: creative-mode
topic: "Temporal Integration Activation & Goroutine Fallback Removal"
tags: [implementation, temporal, swarm, infrastructure, review]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Review Temporal Implementation

## Task(s)

**All completed:**

1. **Infrastructure: Install Temporal dev server** — Added `temporal-cli` to Nix flake, created `temporal-dev.service` systemd unit (SQLite-backed, port 7233/8233, namespace `swarm`), updated `creative-mode.service` to depend on it, added `CM_SWARM_TEMPORAL=true` to `.env`.

2. **Code cleanup: Remove goroutine polling fallback** — The swarm orchestrator had a dual-mode design: Temporal (dormant) + goroutine-based polling (active). Now Temporal is the sole orchestration path. Removed `StartMaintenance()`/`StopMaintenance()`/`maintenanceLoop()`, simplified `StartWorkflow`/`advanceWorkflow`/`advanceFromGate`/`RejectGate` by removing goroutine spawn branches, added nil-safe `triggerHeartbeat()` helper.

3. **main.go wiring** — Made Temporal initialization unconditional (fatal on failure), moved `RecoverWorkflows()` after Temporal setup, removed maintenance calls, simplified shutdown.

4. **Verification** — All swarmorch/swarm tests pass, linter clean, Temporal dev server running, heartbeat schedule active, creative-mode service started cleanly with 3 Temporal workers.

## Critical References

- `harness/internal/swarmorch/temporal.go` — Temporal runtime, client, workers, heartbeat schedule
- `harness/internal/swarmorch/workflows.go` + `activities.go` — Temporal workflow/activity definitions (unchanged but now exercised)
- `harness/CLAUDE.md` — Updated Temporal docs (search for "Temporal")

## Recent changes

- `flake.nix:22` — Added `temporal-cli` to Nix packages
- `harness/main.go:295-316` — Made Temporal init unconditional, moved RecoverWorkflows after
- `harness/main.go:401-404` — Simplified shutdown (removed StopMaintenance, unconditional temporalRuntime.Stop)
- `harness/internal/swarmorch/manager.go:1775-1782` — Added `triggerHeartbeat()` nil-safe helper
- `harness/internal/swarmorch/manager.go:227-229` — Simplified StartWorkflow (triggerHeartbeat + return)
- `harness/internal/swarmorch/manager.go:925-926` — Simplified advanceWorkflow (triggerHeartbeat only)
- `harness/internal/swarmorch/manager.go:1651-1653` — Simplified RejectGate (triggerHeartbeat + return)
- `harness/internal/swarmorch/manager.go:1736-1738` — Simplified advanceFromGate (triggerHeartbeat + return)
- `harness/internal/swarmorch/manager.go:271-275` — Removed RecoverWorkflows early-return guard
- `harness/internal/swarmorch/manager.go` — Deleted StartMaintenance/StopMaintenance/maintenanceLoop (~55 lines), maintenance constants, maintenanceCancel field
- `harness/internal/swarmorch/temporal.go:39-42` — Removed `TemporalEnabled()` function, removed unused `os` import
- `harness/internal/swarmorch/project.go:202-204` — Made startProjectOrchestrator call unconditional
- `harness/internal/swarmorch/project.go:298` — Simplified nil guard (removed `.client` check, kept `temporalRuntime == nil`)

## Learnings

- **Two-step init is required**: Manager is created first, then Temporal runtime (which needs Manager for Activities), then runtime is wired back into Manager via `SetTemporalRuntime()`. Can't avoid this circular dependency.
- **`triggerHeartbeat()` nil-safety is essential for tests**: Tests create Manager without Temporal. The helper avoids panics in test paths that call StartWorkflow/advanceWorkflow.
- **`startProjectOrchestrator` still needs a nil guard**: Unlike the other functions that use `triggerHeartbeat()`, this one directly calls `m.temporalRuntime.client.ExecuteWorkflow()`. Tests would panic without it.
- **Nix profile `--refresh` didn't work**: Had to `nix profile remove` + `nix profile install` to pick up the new `temporal-cli` package.
- **`log.Fatalf` nolint placement**: `gocritic`'s `exitAfterDefer` triggers on the `log.Fatalf` line, not the closing paren. The `//nolint:gocritic` comment must be on the same line as `log.Fatalf(`. For multiline calls, use a preceding `//nolint:gocritic` comment line instead.
- **Second `log.Fatalf` doesn't trigger exitAfterDefer**: When `temporalClient.Close()` precedes it, the linter doesn't flag it (possibly because the Close() call already "handled" the defer concern).

## Artifacts

- `flake.nix` — Updated with temporal-cli
- `harness/main.go` — Temporal mandatory, no maintenance loop
- `harness/internal/swarmorch/manager.go` — Simplified orchestration paths
- `harness/internal/swarmorch/temporal.go` — Removed TemporalEnabled()
- `harness/internal/swarmorch/project.go` — Removed conditional guards
- `harness/CLAUDE.md` — Updated Temporal docs
- `/home/deploy/.claude/projects/-home-deploy-creative-mode/memory/MEMORY.md` — Added Temporal Server section
- `/etc/systemd/system/temporal-dev.service` — New systemd unit
- `/etc/systemd/system/creative-mode.service` — Updated with Requires=temporal-dev

## Action Items & Next Steps

The next agent should **review the Temporal implementation** for correctness and completeness:

1. **Review `workflows.go` and `activities.go`** — These files define the Temporal workflows (`HeartbeatWorkflow`, `LeadFDEWorkflow`, `SessionWorkflow`, `ProjectOrchestratorWorkflow`) and their activities. They were written earlier and are now exercised for the first time. Verify they correctly orchestrate the swarm phases.

2. **Verify heartbeat behavior** — The `LeadFDEWorkflow` runs every 2 minutes via schedule. It should detect workflows needing sessions and spawn them. Check that the activity implementations in `activities.go` match the removed goroutine logic.

3. **Check `SessionWorkflow`** — This replaces the goroutine-spawned `spawnSession` path. Verify it correctly handles session lifecycle (spawn, watch, complete, advance).

4. **Verify `ProjectOrchestratorWorkflow`** — This manages project child ticket lifecycle. It was gated behind `if m.temporalRuntime != nil` — now always called. Ensure it handles wave progression and completion correctly.

5. **Test with a real workflow** — Start a test workflow via `POST /api/swarm/start` or `POST /api/swarm/create-project` and observe the Temporal UI (port 8233) to verify workflow execution, session spawning, and phase advancement all work end-to-end.

6. **Consider edge cases** — What happens if Temporal goes down mid-workflow? The `Restart=on-failure` on temporal-dev should bring it back, and Temporal's durable execution should resume workflows. But this hasn't been tested.

## Other Notes

- **Temporal UI**: Accessible at `http://localhost:8233` — shows workflows, schedules, and execution history
- **Temporal CLI commands**: `temporal workflow list --namespace swarm`, `temporal schedule list --namespace swarm`, `temporal schedule describe --namespace swarm --schedule-id swarm-heartbeat`
- **Key files for the reviewer**: `harness/internal/swarmorch/workflows.go`, `harness/internal/swarmorch/activities.go`, `harness/internal/swarmorch/temporal.go`
- **Pre-existing test failures** (not related to this change): `views/*` packages have `non-constant format string` build errors, `internal/linear` TestHTTPError times out due to rate limiting mock
- **Running sessions survived**: The harness restart correctly recovered live swarm sessions (CRE-13 research) via `RecoverWorkflows`
