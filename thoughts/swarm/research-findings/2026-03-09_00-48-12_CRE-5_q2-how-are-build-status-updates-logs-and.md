---
question: How are build status updates, logs, and completion/failure signals propagated from Claude Code hooks into persisted state and real-time UI streams?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/server/events.go
  - harness/internal/events/types.go
  - harness/internal/events/bus.go
  - harness/internal/claude/claude.go
  - harness/internal/builder/builder.go
  - harness/internal/db/queries/checkpoints.sql
  - harness/agents/skills/api-conventions.md
  - harness/agents/skills/agent-hierarchy.md
  - harness/agents/skills/swarm-conventions.md
---

Claude Code hook events enter via `POST /api/claude-event` (hook-secret protected), decoded as generic JSON, and keyed by `worldID`, `cpID`, and `event` (`handleClaudeEvent`) (`harness/internal/server/server.go:707`). That handler immediately publishes the raw hook payload to the world-scoped EventBus channel, which is what live world SSE streams are subscribed to (`harness/internal/server/server.go:723`, `harness/internal/events/bus.go:78`, `harness/internal/server/events.go:66`).

For the specific hook event `claude.session_stopped`, the same handler asynchronously triggers orchestrator build execution (`BuildCheckpoint`) for that world/checkpoint (`harness/internal/server/server.go:727`). In the world SSE event switch, this same event is rendered as a signal transition to `build_status=compiling` (`harness/internal/server/events.go:254`).

Build lifecycle persistence happens in `Orchestrator.BuildCheckpoint`:

- On start, it posts a persisted message (`messages` table) via `createAndPublishMessage(...build.started...)` (`harness/internal/claude/claude.go:167`, `harness/internal/claude/claude.go:253`).
- On build failure, it updates checkpoint row status + `build_log` text through `UpdateCheckpointStatus(status, build_log)` (`harness/internal/claude/claude.go:175`, `harness/internal/db/queries/checkpoints.sql:12`), persists a `build.failed` message, and publishes a world event `{event: build.failed, error: ...}` (`harness/internal/claude/claude.go:180`, `harness/internal/claude/claude.go:183`).
- On success, it runs post-build metadata extraction, updates checkpoint status to `ready`, persists wasm/server metadata, persists `build.completed` message, and publishes world event `{event: build.completed, cpID, worldName, serverPort}` (`harness/internal/claude/claude.go:197`, `harness/internal/claude/claude.go:202`, `harness/internal/claude/claude.go:239`, `harness/internal/claude/claude.go:242`).

Real-time UI propagation is handled by world SSE subscribers (`/world/:worldID/events`):

- `claude.tool_use.pre` => `build_status=editing`
- `claude.session_stopped` => `build_status=compiling`
- `build.completed` => `build_status=ready`, `current_checkpoint_id=cpID`, plus chat notification and iframe reload script
- `build.failed` => `build_status=failed`, plus chat failure notification
- `claude.rate_limited` => `build_status=rate_limited` (`harness/internal/server/events.go:249`, `harness/internal/server/events.go:254`, `harness/internal/server/events.go:259`, `harness/internal/server/events.go:309`, `harness/internal/server/events.go:322`).

Logs are written to disk during build execution, not through SSE payloads:

- Build command stdout/stderr are structured into JSONL (`build.jsonl`) via `jsonlLineWriter` with fields like `ts,event,worldID,cpID,line` (`harness/internal/builder/builder.go:65`, `harness/internal/builder/builder.go:226`).
- Post-build file-change extraction reads Claude tool-use log file `claude.jsonl` (`harness/internal/builder/builder.go:162`, `harness/internal/builder/builder.go:178`).
- UI/API log access is via `GET /world/:worldID/checkpoint/:cpID/logs/:logType`, serving `build`, `claude`, or `game-server` logs from `data/logs/worlds/<world>/<cp>/` with extension fallback (`harness/internal/server/server.go:593`).

Event typing is centralized in `internal/events/types.go`, where Claude hook and build outcome constants (`claude.tool_use.pre`, `claude.session_stopped`, `build.completed`, `build.failed`, `claude.rate_limited`) align the hook ingress, orchestrator publishes, and SSE switch handling (`harness/internal/events/types.go:7`).

So the end-to-end path is: hook POST -> world EventBus publish (immediate UI status transitions) -> optional build trigger on session stop -> DB/message/checkpoint persistence in orchestrator -> outcome world events -> SSE signal/chat/script updates -> log files served through checkpoint log endpoint.
