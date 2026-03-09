# Research Document

## End-to-end trigger architecture and control points

Mayor build triggering enters at `POST /api/mayor/build` under `/api/mayor`, with group-level middleware enforcing mayor auth before handler execution (`harness/internal/server/server.go:149-154`, `harness/internal/server/server.go:150-153`). The route chain is consistent across findings: **route registration → `mayorAuthMiddleware` → `handleMayorBuild` → `Orchestrator.HandlePrompt`** (`harness/internal/server/mayor_api.go:81-101`, `harness/internal/server/mayor_api.go:114-170`, `harness/internal/server/mayor_api.go:162-170`).

Operationally, mayor agents are instructed to invoke this endpoint by generated skill content (`world-build` curl example) that includes `X-Mayor-Secret` and prompt JSON (`harness/internal/mayor/skills.go:19-43`).

## Authorization, ownership, and request validation

The auth model is world-scoped and secret-bound: middleware requires `X-Mayor-Secret`, resolves `GetWorldByMayorSecret`, then stores `mayor_world` on request context (`harness/internal/server/mayor_api.go:83-103`). Handler logic does not accept world ID from client payload; it binds only `prompt` and derives world identity exclusively from authenticated context (`harness/internal/server/mayor_api.go:123-130`, `harness/internal/server/mayor_api.go:135`, `harness/internal/server/mayor_api.go:160`).

Failure semantics are explicit and granular:

- 401 missing secret (`harness/internal/server/mayor_api.go:86-89`)
- 403 invalid secret (`harness/internal/server/mayor_api.go:92-99`)
- 401 missing world in context (`harness/internal/server/mayor_api.go:108-110`)
- 400 missing/invalid prompt (`harness/internal/server/mayor_api.go:123-130`)
- 503 no orchestrator (`harness/internal/server/mayor_api.go:132-137`)
- 404 no checkpoint data (`harness/internal/server/mayor_api.go:140-143`)
- 500 orchestration/build start failure (`harness/internal/server/mayor_api.go:160-167`)

Accepted triggers return `202` with `{status:"building", checkpoint_id, world_id}` (`harness/internal/server/mayor_api.go:170-174`).

## Checkpoint selection, queueing, and execution model

Before orchestration, source checkpoint is selected from `GetCheckpointTree(world_id)`: newest `ready` first, otherwise latest checkpoint fallback (`harness/internal/server/mayor_api.go:137-153`). `HandlePrompt` then forks a new checkpoint via manager logic that creates directory state, copies files/cache, writes DB record with `status=building`, links parent/prompt/creator, and updates user position (`harness/internal/world/manager.go`). This `building` value acts as the effective queued/in-progress marker (`harness/internal/world/manager.go`, `harness/internal/claude/claude.go`).

Execution runs in tmux sessions named `cm-{worldID}-{cpID}` (`harness/internal/tmux/session.go`). Orchestrator injects environment (`CM_WORLD_ID`, `CM_CHECKPOINT_ID`, `CM_HARNESS_URL`, `CM_LOG_DIR`, optional hook/env values), writes prompt file, and launches Claude (`harness/internal/tmux/session.go`, `harness/internal/claude/claude.go`).

Template behavior is runtime-conditional, not template-bootstrap-based at trigger time: orchestrator reads `world.template_type`, defaults to 3D on lookup failure, and only 3D starts dev server with game-related env vars (`harness/internal/claude/claude.go`).

## Event-driven completion and lifecycle cleanup

Build completion is decoupled from trigger acceptance. Hook events sent to `/api/claude-event` are published to EventBus; when `claude.session_stopped` is seen, server launches `BuildCheckpoint` asynchronously (`harness/internal/server/server.go:707-727`). `BuildCheckpoint` handles teardown, compile/build, DB status transitions (ready/failed), summary extraction, and 3D production server restart behavior (`harness/internal/claude/claude.go`).

Orphan cleanup is session-name-based: reaper targets `cm-{worldID}-{cpID}`, excludes server/trunk sessions, and kills sessions whose checkpoints are missing or not `building` (`harness/internal/claude/claude.go`).

## Persistence model and data linkage

Findings show a dual persistence path:

1. **Core build state in `checkpoints`** (status, logs, summaries, artifacts, timing, server metadata) (`harness/internal/db/migrations/001_initial.sql:24-43`, `harness/internal/db/queries/checkpoints.sql`).
1. **Mayor-trigger lifecycle in `mayor_builds`** (prompt/status/start-complete timestamps/duration/error) (`harness/internal/db/migrations/004_mayor_and_instrumentation.sql:31-42`, `harness/internal/db/queries/mayor_builds.sql:1-19`).

World/mayor linkage is consistently world-anchored: `worlds.id` is key, mayor identity is via `worlds.mayor_secret`, and both `checkpoints.world_id` and `mayor_builds.world_id` reference world records (`harness/internal/db/migrations/004_mayor_and_instrumentation.sql:1-5`, `harness/internal/server/mayor_api.go:79-102`, `harness/internal/db/migrations/001_initial.sql:40`, `harness/internal/db/migrations/004_mayor_and_instrumentation.sql:33`).

## User-facing propagation surfaces and payloads

Event vocabulary includes build and Claude lifecycle plus mayor messaging (`harness/internal/events/types.go:3-16`). Two payload patterns coexist:

- Hook/build events: forwarded largely as arbitrary JSON maps from `/api/claude-event` (`harness/internal/server/server.go:709-727`).
- Discord-mirrored mayor chat: normalized map with `event`, `worldID`, `author_type`, `author_name`, `content`, `message_id` (`harness/internal/discord/listener.go:145-153`).

World-page SSE consumes event types directly and mutates UI state (`build_status`, checkpoint, chat notices, iframe reload scripts) (`harness/internal/server/events.go:260-349`). Mayor dashboard SSE, by contrast, treats any event as cache invalidation and re-queries DB to rerender builds/activity/messages tabs (`harness/internal/server/mayor_dashboard.go:73-118`, `harness/views/mayor/dashboard.templ:66-70`).

## Improvement opportunities observed in current state

- **Status consistency across channels:** Build state is represented in multiple places (`checkpoints.status`, `mayor_builds.status`, SSE `build_status` values). Aligning these semantics appears important to avoid drift (`harness/internal/db/queries/checkpoints.sql`, `harness/internal/db/queries/mayor_builds.sql`, `harness/internal/server/events.go:248-327`).
- **Event payload standardization:** One path forwards arbitrary event maps while another emits normalized payloads; harmonization could reduce consumer branching and ambiguity (`harness/internal/server/server.go:709-727`, `harness/internal/discord/listener.go:145-153`).
- **Attribution clarity:** `userID` for mayor-triggered builds is derived from `w.CreatedBy` with empty fallback; this may produce ambiguous creator attribution in some cases (`harness/internal/server/mayor_api.go:155-160`).
- **Failure visibility consistency:** API-level failures are explicit, but asynchronous failures occur later via build events and DB state; ensuring consistent surfacing across API response, SSE, and dashboard appears to be a key operational quality area (`harness/internal/server/mayor_api.go:160-174`, `harness/internal/server/events.go:307-325`, `harness/internal/server/mayor_dashboard.go:73-118`).

## Contradictions, confidence, and gaps

No direct contradictions were present across findings; accounts were mutually reinforcing on auth flow, orchestration entry, and event propagation.

Low-confidence gaps were not reported in the inputs (all marked high confidence), but these areas remain unspecified in the findings set:

- Explicit call-site evidence showing where `CreateMayorBuild` / `UpdateMayorBuildStatus` are invoked in runtime code paths.
- Any retry/backoff semantics for failed orchestration start or failed downstream build events.
- Any idempotency guard for duplicate mayor trigger submissions with identical prompt/time window.