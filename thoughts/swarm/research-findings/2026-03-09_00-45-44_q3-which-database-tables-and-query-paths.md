---
question: Which database tables and query paths persist mayor build triggers, build metadata, and status updates, and how are these records linked to worlds and mayors?
confidence: high
filesReferenced:
  - harness/internal/db/migrations/001_initial.sql
  - harness/internal/db/migrations/004_mayor_and_instrumentation.sql
  - harness/internal/db/queries/mayor_builds.sql
  - harness/internal/db/queries/checkpoints.sql
  - harness/internal/server/mayor_api.go
  - harness/internal/server/events.go
---

Mayor-triggered build persistence is split across **world identity fields**, **mayor_builds**, and **checkpoints**, with world linkage centered on `world_id` and mayor linkage centered on `worlds.mayor_secret` authentication.

- `004_mayor_and_instrumentation.sql:1-5` adds mayor identity columns on `worlds` (`mayor_name`, `mayor_personality`, `mayor_secret`, `discord_channel_id`, `openclaw_agent_id`). This is how a mayor is bound to a specific world record.
- `mayor_api.go:79-102` authenticates `X-Mayor-Secret` by querying `GetWorldByMayorSecret(...)`; the resolved world is stored in request context as `mayor_world`. This is the runtime join point between mayor credentials and `world_id`.

**Build trigger persistence path (mayor → DB):**

- Build trigger endpoint is `POST /api/mayor/build` (`mayor_api.go:112-174`). It resolves the authenticated world, validates prompt, selects source checkpoint from `GetCheckpointTree(world_id)`, then calls orchestrator `HandlePrompt(worldID, sourceCPID, prompt, userID)`.
- `004_mayor_and_instrumentation.sql:31-41` defines `mayor_builds` with columns for trigger and lifecycle metadata: `id`, `world_id`, `checkpoint_id`, `prompt`, `status`, `started_at`, `completed_at`, `duration_seconds`, `error_message`.
- SQLC query path for this table is in `mayor_builds.sql`:
  - `CreateMayorBuild` inserts trigger records (`mayor_builds.sql:1-4`).
  - `UpdateMayorBuildStatus` writes terminal status, completion timestamp, computed duration, and error (`mayor_builds.sql:6-11`).
  - `GetMayorBuilds` fetches per-world build history (`mayor_builds.sql:13-15`).
  - `GetRecentMayorBuildsAllWorlds` supports cross-world recent build views (`mayor_builds.sql:17-19`).
- `004_mayor_and_instrumentation.sql:42` adds `idx_mayor_builds_world(world_id, started_at)` for world-scoped build-history reads.

**Build metadata + status persistence path (checkpoint pipeline):**

- Core build state lives in `checkpoints` (`001_initial.sql:24-43`) with `world_id`, `status`, `build_log`, `work_summary`, `files_changed`, `build_duration_ms`, `wasm_path`, `server_port`, etc.
- SQLC query path in `checkpoints.sql`:
  - `CreateCheckpoint` creates build units tied to `world_id` and parent checkpoint (`checkpoints.sql:1-4`).
  - `UpdateCheckpointStatus` stores status transitions + build log (`checkpoints.sql:11-12`).
  - `UpdateCheckpointSummary` stores post-build metadata (`work_summary`, `files_changed`, `build_duration_ms`) (`checkpoints.sql:14-15`).
  - `GetCheckpointTree` returns world-specific checkpoint/build history (`checkpoints.sql:24-30`).
  - `CountActiveBuilds` checks active `building` checkpoints by `created_by` (`checkpoints.sql:35-36`).
- In mayor build handling, source selection is based on checkpoint statuses from `GetCheckpointTree(world_id)` (`mayor_api.go:133-152`), preferring latest `ready` checkpoint.

**Status update propagation path (DB-backed status to client signals):**

- World SSE handling maps build lifecycle events to UI `build_status` signals (`events.go:248-327`):
  - tool use pre → `editing`
  - Claude stop → `compiling`
  - build completed → `ready` (+ checkpoint id)
  - build failed → `failed`
  - rate-limited → `rate_limited`
- These SSE statuses are event-driven view updates; persisted status fields are in `checkpoints.status` and `mayor_builds.status` (schema + queries above).

**How records are linked to worlds and mayors:**

- `worlds.id` is the anchor key.
- `mayor_builds.world_id -> worlds.id` foreign key (`004_mayor_and_instrumentation.sql:33`).
- `checkpoints.world_id -> worlds.id` foreign key (`001_initial.sql:40`).
- Mayor identity/auth is world-scoped via `worlds.mayor_secret` and middleware lookup (`mayor_api.go:79-102`), so a mayor-triggered build request is always resolved to one `world_id` before checkpoint/build operations.
