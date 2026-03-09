# Research Document

## End-to-end trigger and execution architecture

Mayor-initiated builds enter through `POST /api/mayor/build` on the Mayor route group, guarded by mayor auth middleware (`harness/internal/server/server.go:149-153`, `harness/internal/server/mayor_api.go:112-114`). The middleware reads `X-Mayor-Secret`, rejects missing secrets with `401`, invalid secrets with `403`, resolves the world via `GetWorldByMayorSecret`, and stores `mayor_world` in request context (`harness/internal/server/mayor_api.go:83-100`).

After auth, the handler binds `{ "prompt": string }`, requires non-empty prompt, chooses a source checkpoint by preferring latest `ready` then latest fallback, and calls `Orchestrator.HandlePrompt(...)`, returning `202 Accepted` with `checkpoint_id`/`world_id` quickly (async semantics) (`harness/internal/server/mayor_api.go:121-127`, `harness/internal/server/mayor_api.go:137-153`, `harness/internal/server/mayor_api.go:162`, `harness/internal/server/mayor_api.go:172-176`).

`HandlePrompt` then forks a checkpoint, updates memory context, initializes template/runtime-specific env, starts tmux Claude session, and sends prompt via `claude --input-file` (`harness/internal/claude/claude.go:73`; `harness/internal/world/manager.go:225,273`; `harness/internal/tmux/session.go` session/create/send flow). Hook events arrive via `POST /api/claude-event`, are immediately published to the world EventBus, and `claude.session_stopped` triggers `BuildCheckpoint(worldID, cpID)` asynchronously (`harness/internal/server/server.go:707,723,727`).

## State transitions, persistence, and UI propagation

The canonical lifecycle is checkpoint-driven: create as `building`, then terminal `ready`/`failed` through `UpdateCheckpointStatus`, with artifact updates (`wasm_path`, `server_port`, summary fields) on success paths (`harness/internal/db/queries/checkpoints.sql:1,13,16,20,23`; `harness/internal/claude/claude.go:174,201,206,225`).

During execution, orchestrator persists lifecycle messages (`build.started`, `build.failed`, `build.completed`) and publishes world events, while build outputs/log streams are written to disk JSONL and later served via logs endpoint rather than embedded in SSE (`harness/internal/claude/claude.go:167,180,242`; `harness/internal/builder/builder.go:65,226`; `harness/internal/server/server.go:593`).

UI status is event-mapped: `claude.tool_use.pre -> editing`, `claude.session_stopped -> compiling`, `build.completed -> ready`, `build.failed -> failed`, plus notifications/reload behavior (`harness/internal/server/events.go:249,254,259,309,322`). This cleanly separates fast status signaling from heavier log retrieval.

## Coordination constraints and operational rules

A key explicit limiter exists, but it is user-scoped (`CountActiveBuilds` on `created_by` and cooldown), invoked inside `ForkCheckpoint` for mayor-triggered builds (`harness/internal/world/rate_limit.go:12,35,56,68`; `harness/internal/world/manager.go:230`; `harness/internal/db/queries/checkpoints.sql:35`). There is no explicit per-world queue/mutex in the mayor path itself; coordination is mostly implicit via DB status transitions and downstream runtime replacement (e.g., `StopByWorldExcept` for 3D server handoff) (`harness/internal/claude/claude.go:209-212`).

Resource constraints are explicit in docs/runtime code: WASM memory pressure motivates serialized template build behavior, and trunk serve enforces single-instance policy due to memory (`harness/agents/skills/build-system.md` WASM constraint; `harness/internal/world/manager.go:595,604`). Build timeouts are explicit (incremental vs initial) (`harness/internal/builder/builder.go:21,44,96,129`).

Retry/cancel behavior is limited in the mayor path: no explicit retry loop, with failures resulting in failed checkpoints and cleanup; cancel semantics are clearly implemented in Swarm/Temporal flows, not mayor checkpoint builds (`harness/internal/claude/claude.go:107,117,349`; `harness/internal/swarmorch/manager.go:164`; `harness/internal/server/swarm_api.go:158`).

## Data model coverage and boundary with mayor-specific tables

Research identifies dual modeling: checkpoints as canonical build lifecycle plus mayor-specific tables for delegation/session tracking. `checkpoints` and `prompt_history` are actively tied to fork/build transitions (`harness/internal/db/migrations/001_initial.sql:20,40`; `harness/internal/world/manager.go:279`).

`mayor_builds` and `mayor_sessions` schema and queries exist (`CreateMayorBuild`, `UpdateMayorBuildStatus`, `UpsertMayorSession`, read models) (`harness/internal/db/migrations/004_mayor_and_instrumentation.sql:25,39`; `harness/internal/db/queries/mayor_builds.sql:1,5,11,15`; `harness/internal/db/queries/mayor_sessions.sql:1,9`).

Potentially important gap: findings strongly document these tables and lifecycle intent, but the concrete `/api/mayor/build -> HandlePrompt` flow evidence emphasizes checkpoint/orchestrator operations and does not explicitly show mayor_builds writes in that runtime path. This is a traceability gap in the findings set (not necessarily a code defect).

## Contradictions, tensions, and improvement opportunities

- **No direct factual contradictions found** across high-confidence findings.
- **Design tension 1 (coordination semantics):** trigger selection and operations are world-centric, but hard gating is user-centric (`created_by` active-build check). This may permit or block concurrency in ways that do not align with world-level resource constraints (`harness/internal/server/mayor_api.go:137-160`; `harness/internal/world/rate_limit.go:56`; `harness/agents/skills/build-system.md`).
- **Design tension 2 (status vs logs):** SSE provides fast state transitions, but detailed logs are separate file retrieval. This is robust but can feel fragmented without stronger correlation UX (`harness/internal/server/events.go`; `harness/internal/server/server.go:593`; `harness/internal/builder/builder.go:226`).
- **Design tension 3 (lifecycle observability):** mayor-specific lifecycle tables are present, but end-to-end mayor trigger findings center on checkpoints/messages/events. Aligning which store is authoritative for mayor build tracking would improve clarity (`harness/internal/db/queries/mayor_builds.sql`; `harness/internal/server/mayor_api.go`; `harness/internal/claude/claude.go`).

## Gaps / low-confidence areas

All provided findings are marked **high confidence**. Remaining gaps are about **coverage linkage** rather than confidence:

1. Whether `mayor_builds`/`mayor_sessions` are invoked on the hot path of `/api/mayor/build` is not explicitly demonstrated in the cited request-flow snippets.
1. No explicit per-world queue/serialization primitive was identified in mayor triggering despite world-scoped runtime resource constraints.
1. Retry policy for mayor builds appears absent/implicit compared with Swarm Temporal workflows; explicit policy definition is not shown in mayor path evidence.