---
question: How does the system create, queue, and execute mayor-triggered build sessions (including checkpoint creation, tmux session naming, and template workspace selection)?
confidence: high
filesReferenced:
  - harness/internal/server/mayor_api.go
  - harness/internal/server/server.go
  - harness/internal/claude/claude.go
  - harness/internal/tmux/session.go
  - harness/internal/world/manager.go
  - harness/agents/skills/agent-hierarchy.md
---

Mayor-triggered builds start at `POST /api/mayor/build`, protected by `X-Mayor-Secret` lookup against `worlds.mayor_secret`; middleware stores the authenticated world on context (`harness/internal/server/mayor_api.go`). The handler requires a JSON `prompt`, fetches the world checkpoint tree, and selects a source checkpoint by scanning from newest to oldest for the latest `ready` checkpoint, falling back to the last checkpoint if none are ready; it then calls `Orchestrator.HandlePrompt(worldID, sourceCPID, prompt, userID)` and returns `202 building` with the new checkpoint ID (`harness/internal/server/mayor_api.go`).

Checkpoint creation/queueing is implemented by `world.Manager.ForkCheckpoint`, which is called from `HandlePrompt`. It applies per-user rate limiting, loads source checkpoint, creates a new checkpoint ID/dir under `data/worlds/{worldID}/{newCPID}`, copies source files (excluding `target/`), clones build cache, inserts checkpoint with `status=building`, parent link, prompt, and created_by, writes prompt history, updates user position, and commits transaction (`harness/internal/world/manager.go`). This `building` status is the queued/in-progress state used later by reapers and build completion logic (`harness/internal/world/manager.go`, `harness/internal/claude/claude.go`).

Execution session naming is centralized in `tmux.NewSession`: Claude sessions are named `cm-{worldID}-{cpID}` (`harness/internal/tmux/session.go`). `Orchestrator.HandlePrompt` creates this session in the checkpoint dir, injecting `CM_WORLD_ID`, `CM_CHECKPOINT_ID`, `CM_HARNESS_URL`, `CM_LOG_DIR` (plus `CM_HOOK_SECRET` if present and optional extra env). It writes prompt text to `.claude-prompt.txt` and starts `claude --dangerously-skip-permissions --input-file <file>` via tmux send-keys (`harness/internal/tmux/session.go`, `harness/internal/claude/claude.go`).

Template workspace selection for mayor-triggered edits comes from checkpoint forking (copying the world’s existing checkpoint directory), not from template-world startup paths. The orchestrator reads `world.template_type` to choose runtime behavior: defaults to 3D if lookup fails; for 3D it starts a dev game server (`ConnectDev`) and passes `CM_GAME_PORT`/`CM_BRP_PORT` into the Claude tmux env; for 2D/boardgame it skips dev server startup (`harness/internal/claude/claude.go`).

Build execution is event-driven: hook scripts post to `/api/claude-event`; when `event == claude.session_stopped`, server launches `go Orchestrator.BuildCheckpoint(worldID, cpID)` (`harness/internal/server/server.go`). `BuildCheckpoint` disconnects the dev server for that checkpoint, kills Claude tmux session `cm-{worldID}-{cpID}`, loads checkpoint/template type, emits build-started message/event, runs builder (`Build(cp, isInitial, templateType)`), marks failed or ready in DB, runs post-build summary extraction, and for 3D starts production game server after stopping previous world servers except current cp (`harness/internal/claude/claude.go`).

Session lifecycle/reaping: `ReapOrphanedSessions` lists tmux sessions and targets names matching `cm-{worldID}-{cpID}` while explicitly excluding `cm-server-*` and `cm-trunk-*`; if checkpoint missing or not `building`, it kills that tmux session (`harness/internal/claude/claude.go`).
