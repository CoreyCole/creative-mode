# Research Document

## Summary

The harness health endpoint is currently a stable liveness probe (`GET /health` -> `200` + `{"status":"ok"}`), and existing clients mostly gate on HTTP status rather than JSON body content (`harness/internal/server/server.go:141`, `harness/internal/server/server.go:303-304`; `site/internal/monitor/handler.go:565-568`; `scripts/debug.sh:173`; `scripts/vps-bootstrap.sh:1069-1071`). The safest design for adding Temporal worker status is additive fields while preserving top-level `status: "ok"` and 200 semantics for liveness compatibility. However, there is a structural gap: `SwarmManager` does not currently expose continuous runtime health state, only construction/start failure and stop hooks, so any worker-health signal is limited unless manager state is extended (`harness/internal/swarmorch/manager.go:29-29`, `39-39`, `71-77`, `157-161`).

## Existing health contract and compatibility boundaries

The endpoint contract is explicit and minimal: `/health` is publicly registered as `GET` and handled by `handleHealth` returning `c.JSON(http.StatusOK, map[string]string{"status":"ok"})` (`harness/internal/server/server.go:141`, `harness/internal/server/server.go:303-304`; also reported as `339-341` in one finding). This establishes a de facto liveness contract of process-up/reachable rather than deep dependency readiness.

Known callers reinforce that contract:

- Status monitor polls harness health and marks ok on `resp.StatusCode == 200`; body is not decoded (`site/internal/monitor/handler.go:548`, `201`, `246`, `565-568`).
- Debug and bootstrap scripts use curl success/http-code only (`scripts/debug.sh:173`; `scripts/vps-bootstrap.sh:1069-1071`).

Implication: additive JSON fields are broadly backward-compatible, but changing non-2xx behavior for Temporal degradation would alter current operational workflows.

## Temporal/Swarm lifecycle realities that shape what can be reported

SwarmManager is optional and env-gated. In `main`, `initSwarmManager(...)` returns `nil` immediately unless `CM_SWARM_TEMPORAL == "true"` (`harness/main.go:503-505`). If enabled, startup creates the manager and worker via `swarmorch.NewSwarmManager(...)`; failures are logged and system falls back to `nil` manager rather than aborting startup (`harness/main.go:512-531`, `526-535`; `harness/internal/swarmorch/manager.go:34-78`). The server receives `srv.SwarmManager = swarmManager` regardless, and swarm routes self-guard with 503 when manager is nil (`harness/main.go:348`; `harness/internal/server/swarm_api.go:97-101`, `106-110`, `157-161`).

This yields three distinct interpretive states for health payload design:

1. Temporal feature intentionally disabled by config.
1. Temporal enabled but manager unavailable (init failure/fallback nil).
1. Temporal enabled and initialized.

A simple nil check cannot distinguish (1) vs (2) without including env intent.

## Preferred response semantics and naming patterns

Repo patterns suggest a two-layer model:

- Machine health endpoints remain minimal (`status: "ok"`, HTTP-based semantics) (`site/internal/webhook/handler.go:37-40`; `site/main.go:171-179`; `site/internal/monitor/handler.go:522-557`).
- Richer component status models use normalized status vocabulary `ok|error|checking` in monitoring UI paths (`site/pages/status_fragments.templ:5-13`; `site/internal/monitor/handler.go:283-311`, `523-557`).

For detailed JSON payloads, conventions favor snake_case keys and explicit booleans/counters (`site/internal/monitor/handler.go:560-575`; `site/pages/types.go:38-48`). This supports additive fields like a nested temporal object while preserving root compatibility.

## Gaps and low-confidence areas

High-confidence findings agree on compatibility and current behavior, but there is a capability gap for “worker status” fidelity:

- `SwarmManager` has no explicit lifecycle/health state beyond startup and stop handles (`harness/internal/swarmorch/manager.go:29`, `39`, `71-77`, `157-161`).
- No built-in `Status()` accessor, heartbeat, last error timestamp, or runtime connectivity probe is present.

Therefore, any immediate `/health` worker signal is likely limited to configuration/initialization inference (high confidence) rather than true ongoing worker health (low confidence if interpreted as runtime guarantee).

## Contradictions or reference variance

No material behavioral contradiction was found. Minor reference variance exists for the current health-return line numbers (`harness/internal/server/server.go:303-304` vs `339-341`), but both findings report the same effective behavior: fixed `200` with `{"status":"ok"}`.

## Consolidated synthesis for implementation direction

- Preserve `GET /health`, HTTP 200 liveness behavior, and top-level `status: "ok"` compatibility (`harness/internal/server/server.go:141`, `303-304`; `site/internal/monitor/handler.go:565-568`; scripts refs above).
- Add Temporal worker information as additive fields (prefer snake_case naming conventions from status payload patterns).
- If signaling Temporal problems, avoid reclassifying liveness as non-2xx unless intentionally changing monitor/debug/bootstrap semantics.
- Explicitly model disabled-vs-unavailable distinctions if reporting Temporal state, because nil manager alone is ambiguous (`harness/main.go:503-505`, `526-535`).
- Flag that current manager internals do not support reliable runtime health without new state/lifecycle instrumentation (`harness/internal/swarmorch/manager.go:29-29`, `90-149`, `157-161`).