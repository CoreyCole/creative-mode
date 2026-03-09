---
question: What internal state and lifecycle hooks exist in swarmorch.SwarmManager (startup, running, stop, errors), and what additional state would be needed to expose a reliable worker health signal?
confidence: high
filesReferenced:
  - harness/internal/swarmorch/manager.go
  - harness/main.go
---

`swarmorch.SwarmManager` currently has very minimal internal lifecycle state and no explicit health model.

- `harness/internal/swarmorch/manager.go:29` — internal state is limited to handles/config: `client`, `worker`, `activities`, `repoRoot`, `logger`.
- `harness/internal/swarmorch/manager.go:39` — startup lifecycle is entirely inside `NewSwarmManager`: dial Temporal client, build runner/activities, register workflows/activities, then `w.Start()`.
- `harness/internal/swarmorch/manager.go:71-77` — startup failure hooks exist only as returned errors:
  - Temporal dial failure returns `dial temporal: ...`
  - worker start failure closes client then returns `start temporal worker: ...`
- `harness/internal/swarmorch/manager.go:79-86` — successful startup is only observable via log (`"swarm temporal worker started"`) and non-nil returned manager.
- `harness/internal/swarmorch/manager.go:90-149` — running behavior exposes task APIs (`StartResearch`, `StartCodePlan`, `CancelTask`) but no manager-level running/healthy checks; errors are per-call return values from `ExecuteWorkflow`/`CancelWorkflow`.
- `harness/internal/swarmorch/manager.go:157-161` — stop hook is `Stop()`, which unconditionally calls `worker.Stop()`, `client.Close()`, and logs `"swarm temporal worker stopped"`; no stopped flag, no idempotency guard, no error channel/return.
- `harness/main.go:334-367` and `harness/main.go:526-539` — process lifecycle integration: manager is optional (`CM_SWARM_TEMPORAL=true`), created once during boot, and stopped on app shutdown if non-nil.

## What is missing for a reliable worker health signal

Current implementation has construction-time success/failure, but no persistent health state after startup. To expose reliable health, `SwarmManager` would need explicit, queryable runtime state such as:

1. **Lifecycle state machine**

   - e.g. `initialized | starting | running | stopping | stopped | degraded | failed`.
   - Updated atomically at key transitions (`New...` start path, successful `w.Start`, `Stop`, fatal runtime faults).

1. **Last-known error + timestamp**

   - Store `lastError`, `lastErrorAt`, and optionally `errorCount` for both startup and runtime operation failures.
   - Include failures from `StartResearch`/`StartCodePlan`/`CancelTask` (currently only returned to caller).

1. **Heartbeat / liveness timestamp**

   - A periodically refreshed `lastHealthyAt` based on an active check (e.g., Temporal service reachability and/or lightweight workflow service call).
   - Without this, “started once” can be stale while dependencies are down.

1. **Dependency health facets**

   - Separate indicators for Temporal client connectivity vs worker loop availability.
   - A single boolean “healthy” should be derived from component checks, not only boot success.

1. **Stop semantics tracking**

   - `stopRequestedAt`, `stoppedAt`, and idempotent `Stop` guard so health endpoints can distinguish intentional shutdown from crash/failure.

1. **Concurrency-safe status accessor**

   - A `Status()` method returning a snapshot struct (state, healthy bool, timestamps, last error, counters), protected via mutex/atomic values.

In short: the current hooks cover **startup success/failure and manual stop**, but not continuous runtime health. Reliable worker health requires explicit manager-owned lifecycle/error/timestamp state plus active probing and a stable read API.
