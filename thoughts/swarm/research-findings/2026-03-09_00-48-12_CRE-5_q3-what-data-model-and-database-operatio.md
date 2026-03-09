---
question: What data model and database operations represent mayor-triggered builds (requests, checkpoints, sessions, artifacts, and statuses), and how do queries/migrations encode lifecycle transitions?
confidence: high
filesReferenced:
  - harness/agents/skills/database-conventions.md
  - harness/agents/skills/agent-hierarchy.md
  - harness/internal/db/db.go
  - harness/internal/db/migrations/001_initial.sql
  - harness/internal/db/migrations/004_mayor_and_instrumentation.sql
  - harness/internal/db/queries/mayor_builds.sql
  - harness/internal/db/queries/checkpoints.sql
  - harness/internal/db/queries/mayor_sessions.sql
  - harness/internal/server/mayor_api.go
  - harness/internal/claude/claude.go
  - harness/internal/world/manager.go
---

Mayor-triggered builds are represented across two main DB entities: `checkpoints` (core build state) and `mayor_builds` (delegation/request tracking), with `mayor_sessions` capturing interaction continuity.

## Schema model for mayor-triggered build lifecycle

- `checkpoints` is the canonical build object (created in initial schema) with lineage and build/runtime fields: `parent_checkpoint_id`, `prompt`, `status`, `build_log`, `work_summary`, `files_changed`, `build_duration_ms`, `dir_path`, `wasm_path`, `server_port`, `created_by`, `created_at` (`harness/internal/db/migrations/001_initial.sql:20`).
- `mayor_builds` adds mayor-specific build delegation/request metadata: `id`, `world_id`, optional `checkpoint_id`, `prompt`, `status` defaulting to `building`, timing fields (`started_at`, `completed_at`, `duration_seconds`), and `error_message` (`harness/internal/db/migrations/004_mayor_and_instrumentation.sql:25`).
- `mayor_sessions` tracks mayor conversational/build sessions at world scope using `session_key`, incrementing `message_count`, and first/last activity timestamps (`harness/internal/db/migrations/004_mayor_and_instrumentation.sql:39`).
- `prompt_history` stores prompt-to-checkpoint request history (`checkpoint_id`, `world_id`, `user_id`, `prompt_text`) and is written when checkpoints are forked (`harness/internal/db/migrations/001_initial.sql:40`, `harness/internal/world/manager.go:279`).

## Request entrypoint and request→checkpoint creation

- Mayor build requests enter via `POST /api/mayor/build` with `X-Mayor-Secret` auth; secret resolves to world via `GetWorldByMayorSecret` (`harness/internal/server/mayor_api.go:73`, `harness/internal/server/mayor_api.go:115`).
- Handler selects a source checkpoint by preferring most recent `ready`, else most recent overall (`harness/internal/server/mayor_api.go:137`).
- It calls orchestrator `HandlePrompt(worldID, sourceCPID, prompt, userID)` to start lifecycle (`harness/internal/server/mayor_api.go:156`).
- `HandlePrompt` forks a new checkpoint via `world.Manager.ForkCheckpoint`, which inserts a new `checkpoints` row with:
  - `parent_checkpoint_id = sourceCPID`
  - `prompt = mayor prompt`
  - `status = building`
  - `dir_path` for new workspace copy
  - `created_by` (world creator when available) (`harness/internal/world/manager.go:268`).

## Encoded checkpoint lifecycle transitions

`checkpoints.sql` query set encodes lifecycle updates as explicit status and artifact mutations:

- `CreateCheckpoint` creates new nodes in lifecycle graph (`harness/internal/db/queries/checkpoints.sql:1`).
- `UpdateCheckpointStatus` mutates status and optional `build_log` (`harness/internal/db/queries/checkpoints.sql:13`).
- `UpdateCheckpointSummary` persists post-build artifact metadata (`work_summary`, `files_changed`, `build_duration_ms`) (`harness/internal/db/queries/checkpoints.sql:16`).
- `UpdateCheckpointWasmPath` and `UpdateCheckpointServerPort` persist deploy/runtime artifact locations (`harness/internal/db/queries/checkpoints.sql:23`, `harness/internal/db/queries/checkpoints.sql:20`).
- `CountActiveBuilds` defines “active” as rows where `created_by = ?` and `status='building'` (`harness/internal/db/queries/checkpoints.sql:35`).

Runtime transition execution:

- On orchestration failures before/around session setup, checkpoint is marked `failed` with reason in `build_log` (`harness/internal/claude/claude.go:107`, `harness/internal/claude/claude.go:349`).
- On build start/complete path:
  - build failure → `status=failed`, `build_log=error` (`harness/internal/claude/claude.go:174`)
  - build success → `status=ready`, then optional `wasm_path` and `server_port` updates (`harness/internal/claude/claude.go:201`, `harness/internal/claude/claude.go:206`, `harness/internal/claude/claude.go:225`).
- Session reaper uses checkpoint status as authoritative lifecycle guard: tmux sessions are reaped when associated checkpoint status is no longer `building` (`harness/internal/claude/claude.go:334`).

## Mayor build request/status table operations

`mayor_builds.sql` defines request-level lifecycle operations:

- `CreateMayorBuild` inserts request with initial status (`harness/internal/db/queries/mayor_builds.sql:1`).
- `UpdateMayorBuildStatus` sets final status, stamps `completed_at=CURRENT_TIMESTAMP`, computes `duration_seconds` from `started_at`, and stores `error_message` (`harness/internal/db/queries/mayor_builds.sql:5`).
- Read models: `GetMayorBuilds(world_id, limit)` and cross-world `GetRecentMayorBuildsAllWorlds(limit)` sorted by `started_at DESC` (`harness/internal/db/queries/mayor_builds.sql:11`, `harness/internal/db/queries/mayor_builds.sql:15`).

## Mayor session operations

- `UpsertMayorSession` uses insert-or-update semantics on `id`; each call increments `message_count` and advances `last_active_at` (`harness/internal/db/queries/mayor_sessions.sql:1`).
- `GetMayorSessions` provides world-scoped recency ordering (`last_active_at DESC`) (`harness/internal/db/queries/mayor_sessions.sql:9`).

## Migration and query registration mechanics affecting lifecycle encoding

- Migration files are embedded and applied in fixed ordered list in `runMigrations`; mayor build/session tables are introduced by `004_mayor_and_instrumentation.sql` (`harness/internal/db/db.go:81`, `harness/internal/db/db.go:85`).
- Bootstrap logic marks 004 as applied when `worlds.mayor_name` exists, anchoring lifecycle schema detection for mayor features (`harness/internal/db/db.go:155`).
- DB wrapper sets WAL mode and single writer connection, shaping how these lifecycle updates execute under concurrent activity (`harness/internal/db/db.go:29`, `harness/internal/db/db.go:36`).

Overall, lifecycle is encoded as: mayor request/auth → checkpoint fork (`building`) → async orchestration/build hooks → checkpoint terminal state (`ready`/`failed`) plus artifact fields (`wasm_path`, `server_port`, summaries), with parallel mayor-level request/session tables (`mayor_builds`, `mayor_sessions`) providing delegation tracking and activity state.
