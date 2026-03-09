# Implementation Plan

## Phase 1: Define status semantics and add SwarmManager/server status surfaces

**Goal:** Eliminate ambiguous `nil` manager interpretation and create a truthful, lock-safe status source before changing API output.

### Tasks

- In `harness/main.go`, capture Temporal enablement intent and init outcome during swarm manager setup:
  - `SwarmTemporalEnabled bool` from `CM_SWARM_TEMPORAL == "true"`
  - `SwarmTemporalInitError string` when initialization fails
- Extend `harness/internal/server/server.go` `Server` struct to carry:
  - existing `SwarmManager *swarmorch.SwarmManager`
  - `SwarmTemporalEnabled`
  - `SwarmTemporalInitError`
- In `harness/internal/swarmorch/manager.go` (or `swarm_manager.go` depending on actual file), add a read-only lifecycle snapshot accessor, e.g. `Status()` / `HealthSnapshot()` with lock-safe state.
- Minimum snapshot fields should support non-misleading reporting:
  - config/startup inference: `configured` or `enabled`, `startupSucceeded` / `worker_state`
  - lifecycle: `started`, optional `running` (only if semantically reliable), `last_error`, timestamps (`started_at`, `stopped_at` as available)
  - confidence marker: `health_confidence: startup_only` (or equivalent)
- Track lifecycle transitions in manager start/stop/error paths (non-blocking for HTTP reads).

### Commit boundary

- Commit only status plumbing and manager/server state surfaces; no `/health` payload changes yet.

### Verification

- `go test ./harness/internal/swarmorch/...`
- `go test ./harness/internal/server -run Health -v` (or specific server unit tests covering new server fields)

### Risks addressed

- Resolves disabled-vs-init-failed ambiguity.
- Prevents false precision by encoding confidence explicitly.

______________________________________________________________________

## Phase 2: Implement additive /health response contract

**Goal:** Preserve legacy behavior while exposing new Temporal/Swarm metadata as additive fields.

### Tasks

- Update `handleHealth` in `harness/internal/server/server.go`:
  - Keep route and method unchanged: `GET /health`
  - Keep HTTP response code always `200` for liveness
  - Keep top-level JSON compatibility: `"status": "ok"`
- Add nested additive health object(s), using stable names and explicit semantics. Preferred merged shape:
  - `swarm.status` (or `temporal.worker_state` if chosen as canonical; pick one primary naming scheme and keep consistent)
  - include status details from manager snapshot and server init intent/error
  - return `unknown`/`disabled`/`init_failed`/`initialized` semantics rather than hard `healthy=true` claims when runtime probes do not exist
- If `SwarmManager == nil`, classify deterministically using server init fields:
  - disabled when feature not enabled
  - init_failed when enabled but init error exists
- Ensure additive-only change policy: do not remove/rename existing top-level keys.

### Commit boundary

- Commit `/health` response wiring and schema mapping only.

### Verification

- `go test ./harness/internal/server -run Health -v`
- `curl -i http://localhost:<harness-port>/health` (must remain 200)
- `curl -s http://localhost:<harness-port>/health | jq .` (verify `status: ok` plus additive fields)

### Risks addressed

- Avoids breaking existing scripts/monitors that assume HTTP success and/or top-level `status`.
- Prevents latency regressions by avoiding synchronous heavy Temporal probes in this phase.

______________________________________________________________________

## Phase 3: Tests and compatibility verification

**Goal:** Lock backward compatibility and validate new status semantics across startup modes.

### Tasks

- Add/extend tests in:
  - `harness/internal/server/server_health_test.go` (or `server_test.go` if that is existing convention)
  - `harness/internal/swarmorch/manager_test.go`
- Required server test cases:
  - legacy invariant: always HTTP 200 + top-level `status == "ok"`
  - feature disabled: returns additive state indicating disabled/unknown as designed
  - init failed: enabled + nil manager + init error => init_failed (or chosen enum)
  - initialized: enabled + manager present => initialized/startup_succeeded
  - degraded/unknown snapshot still does not change HTTP status/top-level status
- Manager tests:
  - lifecycle snapshot after successful start
  - stop transition state/timestamps
  - error capture reflected in snapshot
- Compatibility checks for downstream monitor/scripts:
  - `go test ./site/internal/monitor -run HarnessHealth -v`
  - validate scripts relying on HTTP code still pass unchanged

### Commit boundary

- Commit tests and compatibility fixes only (no contract redesign).

### Verification

- `cd harness && go test ./internal/server ./internal/swarmorch`
- `cd harness && go test ./...`
- `go test ./site/internal/monitor -run HarnessHealth -v`

### Cross-domain concern flags

- If any consumer parses strict JSON schema, coordinate additive-field tolerance before deployment.

______________________________________________________________________

## Phase 4: Documentation, rollout, and follow-up readiness path

**Goal:** Communicate semantics clearly and stage future stricter health behavior safely.

### Tasks

- Update docs/runbooks:
  - `harness/E2E_PLAYBOOK.md` health section to describe additive fields and compatibility guarantees
  - clarify `/health` is liveness, component subsection is informational with confidence level
- Add release notes/change log guidance:
  - top-level contract unchanged
  - new fields are best-effort unless/until runtime probes are implemented
- Optional follow-up proposal (separate PR): introduce `/ready` for non-2xx readiness semantics tied to stronger runtime checks.

### Commit boundary

- Commit docs + rollout notes independently from code.

### Verification

- Manual smoke matrix:
  - `CM_SWARM_TEMPORAL=false` => disabled semantics
  - `CM_SWARM_TEMPORAL=true` with Temporal down => init_failed semantics
  - `CM_SWARM_TEMPORAL=true` with Temporal up => initialized semantics

______________________________________________________________________

## Consolidated Risks and Mitigations

- **Risk:** Breaking existing monitors/scripts by changing `/health` status behavior.\
  **Mitigation:** Preserve HTTP 200 and top-level `status: ok`; additive-only payload changes.
- **Risk:** Misleading operators with strong runtime claims from startup-only data.\
  **Mitigation:** Encode `health_confidence`/`startup_only`, use `unknown` where certainty is low, avoid premature `healthy=true` semantics.
- **Risk:** `SwarmManager` lifecycle visibility insufficient for reliable runtime status.\
  **Mitigation:** Add explicit lifecycle tracking now; defer true runtime probes to follow-up.
- **Risk:** Health endpoint latency increase from synchronous Temporal checks.\
  **Mitigation:** Keep phase scoped to local snapshot inference; only add probes later with timeout budgets.

## Dependency Graph (resolved)

1. Manager/server status surfaces (Phase 1) **must precede** endpoint schema wiring (Phase 2).
1. Endpoint behavior (Phase 2) **must precede** compatibility and regression assertions (Phase 3).
1. Verified behavior (Phase 3) **must precede** rollout communication and consumer guidance (Phase 4).