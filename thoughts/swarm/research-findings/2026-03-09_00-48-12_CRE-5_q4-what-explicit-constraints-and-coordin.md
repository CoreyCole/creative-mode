---
question: What explicit constraints and coordination rules govern mayor build triggering (per-world serialization, concurrency limits, WASM memory limits, retry/cancel behavior, and interaction with other orchestration systems like Swarm/Temporal)?
confidence: high
filesReferenced:
  - harness/internal/server/mayor_api.go
  - harness/internal/claude/claude.go
  - harness/internal/world/manager.go
  - harness/internal/world/rate_limit.go
  - harness/internal/builder/builder.go
  - harness/internal/swarmorch/workflows.go
  - harness/internal/swarmorch/manager.go
  - harness/agents/skills/build-system.md
  - harness/agents/skills/swarm-conventions.md
  - harness/agents/skills/temporal-conventions.md
---

Mayor-triggered builds are initiated through `POST /api/mayor/build` with `X-Mayor-Secret` auth, resolved to a world, then routed to `Orchestrator.HandlePrompt(...)` using that world’s latest ready checkpoint as source (or latest checkpoint if none ready) (`harness/internal/server/mayor_api.go:113`, `harness/internal/server/mayor_api.go:131`, `harness/internal/server/mayor_api.go:146`, `harness/internal/server/mayor_api.go:160`).

## Explicit triggering and checkpoint-state coordination

- `HandlePrompt` always forks a new checkpoint first via `world.Manager.ForkCheckpoint`, and forked checkpoints are inserted as `status=building` before Claude/tmux execution proceeds (`harness/internal/claude/claude.go:73`, `harness/internal/world/manager.go:225`, `harness/internal/world/manager.go:273`).
- Build execution is asynchronous from the request path: `HandlePrompt` returns accepted checkpoint info immediately; subsequent build completion is hook/event-driven (`harness/internal/claude/claude.go:70`, `harness/internal/server/mayor_api.go:168`).
- Actual compile/build runs in `BuildCheckpoint(worldID, cpID)` when Claude session stop events arrive; it transitions checkpoint status to `failed` on errors or `ready` on success (`harness/internal/claude/claude.go:127`, `harness/internal/claude/claude.go:164`, `harness/internal/claude/claude.go:191`).

## Concurrency and serialization constraints

- The explicit enforced limiter for mayor/user prompt triggering is user-scoped, not world-scoped: `RateLimiter.Check` enforces a 30s cooldown per user and blocks when `CountActiveBuilds(...) > 0` for that user (`harness/internal/world/rate_limit.go:12`, `harness/internal/world/rate_limit.go:35`, `harness/internal/world/rate_limit.go:56`, `harness/internal/world/rate_limit.go:68`).
- That limiter is invoked inside `ForkCheckpoint`, so every mayor build trigger passes through it (`harness/internal/world/manager.go:230`).
- No dedicated per-world mutex/queue is defined in the mayor build path itself; coordination is primarily by checkpoint status transitions and user active-build gating in rate limiter + DB state (`harness/internal/server/mayor_api.go:146`, `harness/internal/world/manager.go:273`, `harness/internal/claude/claude.go:337`).
- On successful 3D build, game server coordination is world-scoped: old servers for the world are stopped except the new checkpoint via `StopByWorldExcept(worldID, cpID)` before connecting the new server (`harness/internal/claude/claude.go:209`, `harness/internal/claude/claude.go:210`, `harness/internal/claude/claude.go:212`).

## WASM/memory-related explicit limits

- Build-system docs state an explicit memory constraint: each `wasm-bindgen` invocation uses ~5 GB RAM on a 10 GB VPS; therefore only one template build at a time to avoid OOM (`harness/agents/skills/build-system.md`, section “WASM Build Constraint”).
- In world runtime dev serving, trunk serve for template worlds is explicitly single-instance by policy: `EnsureTemplateTrunkServe` stops all other trunk serves before starting one (“only one at a time due to memory”) (`harness/internal/world/manager.go:595`, `harness/internal/world/manager.go:604`).
- Builder timeouts are explicit: incremental 5 min, initial 15 min; timeout produces specific build failure errors (`harness/internal/builder/builder.go:21`, `harness/internal/builder/builder.go:44`, `harness/internal/builder/builder.go:96`, `harness/internal/builder/builder.go:129`).

## Retry / cancel behavior in mayor build pipeline

- Mayor build API path has no explicit retry loop; if `HandlePrompt` or build fails, failure is returned/logged and checkpoint status is marked failed where applicable (`harness/internal/server/mayor_api.go:160`, `harness/internal/server/mayor_api.go:162`, `harness/internal/claude/claude.go:115`, `harness/internal/claude/claude.go:167`, `harness/internal/claude/claude.go:340`).
- Tmux/session coordination includes cleanup but not automatic retry: failed session creation or prompt send kills/disconnects and marks checkpoint failed (`harness/internal/claude/claude.go:108`, `harness/internal/claude/claude.go:117`).
- Orchestrator has orphan reaping logic that kills stale Claude tmux sessions when checkpoint status is no longer `building` (`harness/internal/claude/claude.go:283`, `harness/internal/claude/claude.go:327`).
- Cancel semantics shown in this codebase are implemented for Swarm Temporal tasks (`CancelTask`) rather than mayor build checkpoints; swarm cancel endpoints route to Temporal workflow cancellation (`harness/internal/swarmorch/manager.go:164`, `harness/internal/server/swarm_api.go:158`).

## Interaction boundaries with Swarm/Temporal orchestration

- Swarm/Temporal orchestration is a separate subsystem for agent tasks (`ResearchWorkflow`, `CodeChangePlanWorkflow`) on task queue `swarm-agents`; it is not the execution engine used by `/api/mayor/build` (`harness/agents/skills/temporal-conventions.md`, “Workflows” and “Task Queue”; `harness/agents/skills/swarm-conventions.md`, “Temporal Integration”).
- Mayor builds go through Claude Orchestrator + world manager + builder/tmux path, while Swarm tasks go through Temporal workflows/activities and support workflow cancellation/retries per Temporal activity options (`harness/internal/server/mayor_api.go:160`, `harness/internal/claude/claude.go:73`; `harness/agents/skills/temporal-conventions.md`, “Activity Options”, “Disconnected Context for Cleanup”).
- Therefore, explicit coordination between systems is architectural separation: mayor build triggering does not call Swarm workflow APIs; Swarm cancellation and retry policies apply to swarm tasks, not mayor checkpoint builds.
