---
question: What is the end-to-end execution flow after /api/mayor/build is accepted, including checkpoint creation, Claude Code session startup, tmux process orchestration, and event hook wiring?
confidence: high
filesReferenced:
  - harness/internal/server/mayor_api.go
  - harness/internal/claude/claude.go
  - harness/internal/world/manager.go
  - harness/internal/tmux/session.go
  - harness/internal/server/server.go
  - harness/internal/server/events.go
  - harness/internal/events/events.go
---

`POST /api/mayor/build` is registered under `/api/mayor` with `mayorAuthMiddleware`, which validates `X-Mayor-Secret`, resolves the world via DB lookup, and stores it in context (`internal/server/server.go`, route registration; `internal/server/mayor_api.go`, auth middleware + `requireMayorWorld`).

After auth, `handleMayorBuild` binds `{prompt}`, finds a source checkpoint by scanning the world’s checkpoint tree and preferring the most recent `ready` checkpoint (falling back to the latest checkpoint if none are ready), chooses `userID` from `world.created_by` when present, then calls `Orchestrator.HandlePrompt(...)` (`internal/server/mayor_api.go: handleMayorBuild flow`). On success it immediately returns `202 Accepted` with `{status:"building", checkpoint_id, world_id}`.

`Orchestrator.HandlePrompt` performs the asynchronous pipeline setup (`internal/claude/claude.go`):

1. Fork checkpoint via `worldManager.ForkCheckpoint`.
1. Append prompt context into the new checkpoint `MEMORY.md` via `updateMemory`.
1. Resolve template type from world record (defaulting to 3D if lookup fails).
1. For 3D templates, start a dev game server via `GameServers.ConnectDev` and capture `CM_GAME_PORT`/`CM_BRP_PORT` as extra env vars.
1. Create a Claude tmux session and inject `CM_*` variables.
1. Send the prompt to Claude CLI using `--input-file`.

Checkpoint creation details come from `ForkCheckpoint` (`internal/world/manager.go`):

- Rate limiter check runs first.
- Source checkpoint directory is copied to a new `{dataDir}/worlds/{worldID}/{newCPID}` directory (excluding top-level `target/`).
- Build cache cloning (`target/`) is attempted (warning-only on failure).
- Transaction inserts checkpoint with:
  - `parent_checkpoint_id = sourceCPID`
  - `prompt = provided prompt`
  - `status = building`
  - `dir_path = newDir`
  - `created_by = userID` (if non-empty)
- Transaction also inserts `prompt_history` and updates `user_positions`.

Tmux orchestration is encapsulated in `tmux.Session` (`internal/tmux/session.go`):

- Session name format: `cm-{worldID}-{cpID}`.
- `Create(...)` runs `tmux new-session -d` with:
  - `-c <checkpointDir>`
  - env: `CM_WORLD_ID`, `CM_CHECKPOINT_ID`, `CM_HARNESS_URL`, `CM_LOG_DIR`
  - env: `CM_HOOK_SECRET` propagated from process env when set
  - plus any extra env passed by orchestrator (e.g., dev ports)
- Log directory is created at `{logsDir}/worlds/{worldID}/{cpID}`.
- `SendPrompt(...)` writes `.claude-prompt.txt` in the checkpoint dir, then sends: `claude --dangerously-skip-permissions --input-file <promptFile>` via `tmux send-keys`.

Hook wiring and build trigger path (`internal/server/server.go`):

- `/api/claude-event` is protected by `hookSecretMiddleware` (checks `X-Hook-Secret` against `CM_HOOK_SECRET` when configured).
- Hook payload is decoded as generic JSON map containing fields like `worldID`, `cpID`, `event`.
- Server publishes every hook event to world-scoped EventBus channel.
- When `event == claude.session_stopped`, server starts `go Orchestrator.BuildCheckpoint(worldID, cpID)`.

`BuildCheckpoint` execution (`internal/claude/claude.go`):

- Disconnects dev server for that specific checkpoint.
- Kills Claude tmux session `cm-{worldID}-{cpID}`.
- Loads checkpoint/world data.
- Emits/persists `build.started` message via `createAndPublishMessage`.
- Runs builder `Build(&cp, isInitial, templateType)`.
  - On failure: sets checkpoint `failed` + `build_log`, emits `build.failed`, publishes world event with error, calls `OnBuildComplete` callback (if configured).
  - On success: runs `PostBuild`, updates checkpoint `ready`, persists wasm path if present, and for 3D starts production game server after stopping older world servers (`StopByWorldExcept` then `Connect`), storing `server_port`.
- Emits/persists `build.completed` and publishes world event containing `worldID`, `cpID`, `worldName`, and optional `serverPort`; calls `OnBuildComplete` on success when configured.

UI/event propagation (`internal/server/events.go`): world SSE listeners react to the published event stream:

- `claude.tool_use.pre` => `build_status: editing`
- `claude.session_stopped` => `build_status: compiling`
- `build.completed` => `build_status: ready`, `current_checkpoint_id` update, chat notification, iframe reload (with `server_port` for 3D, without for client-only)
- `build.failed` => `build_status: failed` + failure notification

Overall chain after accepted mayor build is: `/api/mayor/build` accepted → source checkpoint chosen → forked checkpoint in `building` state + prompt history/user position → tmux Claude session (`cm-world-cp`) started with CM env + hook secret → Claude hook events posted to `/api/claude-event` → `claude.session_stopped` triggers `BuildCheckpoint` → build status persisted + events published + runtime server switch (3D) → SSE updates world UI state and iframe target.
