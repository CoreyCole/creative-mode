---
question: Where and how is SwarmManager (Temporal client/worker) initialized, attached to the HTTP server, and optionally disabled via environment flags, and what does that imply for reporting worker status in health responses?
confidence: high
filesReferenced:
  - harness/main.go
  - harness/internal/server/server.go
  - harness/internal/server/swarm_api.go
  - harness/internal/swarmorch/manager.go
---

SwarmManager is created in `main()` via `initSwarmManager(...)` and then injected into the HTTP `Server` struct before route registration (`harness/main.go:334-349`). The initializer is explicitly feature-gated: if `CM_SWARM_TEMPORAL != "true"`, it returns `nil` immediately (`harness/main.go:503-505`), so Temporal is fully disabled by env flag without erroring startup.

When enabled, initialization resolves repo/agents paths, reads optional config env (`CM_SWARM_TOOL_CALL_LIMIT`, `CM_SWARM_MODEL`), and calls `swarmorch.NewSwarmManager(...)` (`harness/main.go:512-531`). `NewSwarmManager` dials Temporal at fixed host/namespace (`localhost:7233`, `swarm`), registers workflows/activities, and **starts** the worker before returning (`harness/internal/swarmorch/manager.go:16-21`, `34-78`). If dial/start fails, it returns error; caller logs and falls back to `nil` manager (`harness/main.go:526-535`).

Attachment to HTTP is simple dependency injection: `srv.SwarmManager = swarmManager` (`harness/main.go:348`). Route registration always mounts swarm endpoints, but handlers guard runtime availability by checking `s.SwarmManager == nil` and returning 503 "swarm manager not configured" (`harness/internal/server/swarm_api.go:97-101`, `106-110`, `157-161`). So API presence does not imply worker availability.

Lifecycle: on process shutdown, main checks non-nil and calls `swarmManager.Stop()` (`harness/main.go:360-362`), which stops worker + closes client (`harness/internal/swarmorch/manager.go:153-157`).

Health implication: current `/health` is static `{status:"ok"}` with HTTP 200 and has no swarm field (`harness/internal/server/server.go:332-335`). Because SwarmManager can be nil by design (feature off or init failure), health reporting should distinguish at least:

- feature disabled (`CM_SWARM_TEMPORAL` false) vs
- enabled but unavailable (init failed / nil unexpectedly) vs
- enabled and initialized.

A nil check alone is ambiguous unless combined with env intent; otherwise disabled-by-config could be misreported as unhealthy. Also, no explicit runtime "is worker running" flag exists on `SwarmManager` beyond successful construction/start and eventual `Stop()`, so any health extension is currently limited to configuration/initialization state unless additional manager state is added.
