---
date: 2026-03-08T00:10:46-08:00
researcher: CoreyCole
git_commit: 834866c0f22a54fef5b2c2ce97425156065a6b77
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives Branch — Full Review"
tags: [review, swarm, temporal, agent-primitives, dashboard]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Full Review of feat/agent-primitives Branch

## Task(s)

**Full branch review requested.** The `feat/agent-primitives` branch is ready for a comprehensive review before merge to `main`. This branch adds a complete swarm agent orchestration system: Temporal workflows, JS agent scripts, a dashboard UI, and supporting infrastructure.

### What the branch introduces (high-level)

1. **Swarm DB schema** — 5 new tables (`swarm_tasks`, `swarm_spans`, `swarm_artifacts`, `swarm_research_questions`, `swarm_task_messages`) with recursive CTE span tree queries
2. **Temporal workflow orchestration** (`internal/swarmorch/`) — research and code_change_plan workflows, agent process runner with heartbeat/timeout, span-based observability
3. **JS agent scripts** (`harness/agents/`) — pi-ai agent framework scripts with shared libs (protocol, orchestrator tools, search context, Codex auth)
4. **Swarm dashboard** (`/swarm`) — tabbed UI (Chat, Agents, Spans, Artifacts) with SSE live updates and user chat
5. **Swarm API** — REST endpoints for programmatic task management (protected by hook secret)
6. **Claude Code config** — hooks, custom agents, skills (Linear integration, planning, handoffs)

### Status: All committed and pushed. Working tree clean.

## Critical References

- `harness/CLAUDE.md` — Updated with Swarm API, Swarm Dashboard, and Dev Login Testing sections
- `CLAUDE.md` — Updated agent hierarchy diagram to include Swarm
- `thoughts/swarm/plans/2026-03-08_agent-primitives-v3-final.md` — The most complete plan document

## Recent Changes

All changes are on `feat/agent-primitives` branch. Key commits (newest first):

- `834866c` — Shared skill definitions and lock file (88 files)
- `13d2545` — Claude Code config: hooks, agents, skills, settings (50 files)
- `08b4f7b` — Agent JS libs: heartbeat protocol, Codex auth, model provider fix (4 files)
- `ae033ef` — Swarm orchestration: Temporal workflows, agent runner, API handlers (10 files, +1908)
- `67d6dc3` — Swarm dashboard tabbed UI with chat (12 files, +3082)
- `8e544f5` — Agent primitives Phase 2: JS agent scripts, shared libraries, skill files, Go types
- `93e0184` — Agent primitives Phase 1: swarm DB schema, SQLC queries, event types

## Learnings

### Architecture decisions

- **Temporal for orchestration**: Workflows run in namespace `swarm` on `temporal-dev.service` (systemd, SQLite-backed). Env var `CM_SWARM_TEMPORAL=true` gates initialization. Service dependency: `creative-mode.service` Requires+After `temporal-dev.service`.
- **JS agents run as child processes**: `swarmorch/agent.go` spawns Node.js scripts via `exec.CommandContext`, communicates over stdin/stdout with a line-delimited JSON protocol (types: `init`, `event`, `result`, `heartbeat`, `question`).
- **Heartbeat for liveness**: Agents emit `{"type":"heartbeat"}` every 15s via `protocol.js:startHeartbeat()`. The Go runner uses this to detect stalls (no output for configurable timeout).
- **Span-to-chat conversion**: `dashboard.templ:spansToChatMessages()` walks spans and extracts workflow/agent/question events into readable chat messages, interleaved with DB messages by timestamp.

### Key patterns

- **Datastar signals for tabs**: `active_tab` signal with `data-show`/`data-class` pattern (reused from mayor dashboard)
- **SSE append for chat**: `datastar.WithModeAppend()` appends new message bubbles without re-rendering the full chat log
- **Dev login for testing**: `POST /dev/auth/login` with `username=test&role=admin` form data, then use cookie jar for authenticated requests. Datastar POST signals are flat JSON (no `datastar` wrapper).

### Gotchas discovered

- **Hook URL**: Must use `http://localhost:8080` — Claude Code blocks private/link-local IPs (Tailscale `100.x.x.x`). Set via `HARNESS_HOOK_URL` in `.env`.
- **Migrations**: Must be manually added to `migrationFiles` slice in `db.go` — not auto-discovered.
- **Playwright not installed on VPS**: `playwright-cli` commands fail with missing Chromium. Use curl + dev login for testing.

## Artifacts

### Code (harness/)
- `harness/internal/db/migrations/006_swarm_tables.sql` — Core schema (tasks, spans, artifacts, research questions)
- `harness/internal/db/migrations/007_swarm_messages.sql` — Chat messages table
- `harness/internal/db/queries/swarm.sql` — All SQLC queries (CRUD + recursive CTE span tree)
- `harness/internal/swarmorch/manager.go` — SwarmManager: Temporal client, workflow starters
- `harness/internal/swarmorch/workflows.go` — Research and code plan workflow definitions
- `harness/internal/swarmorch/activities.go` — Temporal activities (run agent, record spans)
- `harness/internal/swarmorch/agent.go` — Agent process runner (spawn, protocol, heartbeat)
- `harness/internal/swarmorch/helpers.go` — Shared helpers (context building, file search)
- `harness/internal/swarmorch/runner.go` — Runner interface
- `harness/internal/swarmorch/types.go` — Shared types (AgentTask, AgentResult, etc.)
- `harness/internal/server/swarm_api.go` — REST API handlers
- `harness/internal/server/swarm_dashboard.go` — Dashboard handlers + SSE + chat POST
- `harness/views/swarm/dashboard.templ` — Tabbed dashboard UI (Chat, Agents, Spans, Artifacts)
- `harness/main.go:322-344` — SwarmManager initialization
- `harness/main.go:351` — SwarmManager wired to server
- `harness/main.go:363-366` — SwarmManager shutdown

### Code (agents/)
- `harness/agents/lib/protocol.js` — Stdin/stdout line-JSON protocol + heartbeat
- `harness/agents/lib/agent-factory.js` — Shared agent bootstrapping (model, tools, prompt)
- `harness/agents/lib/codex-auth.js` — Codex OAuth token management
- `harness/agents/lib/codex-login.js` — Codex login flow

### Config
- `.claude/settings.json` — Migrated deny rules to hook-based enforcement
- `.claude/hooks/recommend-just-check.sh` — Pre-tool-use hook for build commands
- `.claude/agents/` — 9 custom agent definitions
- `.claude/skills/` — Planning, handoff, Linear integration skills

### Documentation
- `harness/CLAUDE.md` — Swarm API, Swarm Dashboard, Dev Login Testing sections
- `CLAUDE.md` — Swarm in agent hierarchy diagram
- `thoughts/swarm/` — Research docs, plans, handoffs (extensive)

## Action Items & Next Steps

1. **Full code review of the branch** — Focus areas:
   - `internal/swarmorch/` — Temporal workflow correctness, error handling, activity idempotency
   - `internal/swarmorch/agent.go` — Process lifecycle, timeout handling, resource cleanup
   - `internal/server/swarm_api.go` + `swarm_dashboard.go` — Auth, input validation, SSE correctness
   - `views/swarm/dashboard.templ` — Chat message building logic, XSS safety (span JSON rendering)
   - `harness/agents/lib/` — Protocol robustness, auth token handling
2. **Test coverage** — No tests exist yet for swarmorch package
3. **Consider**: Should `.agents/`, `.pi/`, `skills/`, `skills-lock.json` be gitignored or committed? They're tooling config that may vary per developer.
4. **Consider**: The `swarmorch/types.go` file exists but wasn't in my session's diff — verify it's consistent with the rest of the package.

## Other Notes

### Branch scope
The branch has 226 changed files (+27,076 lines) total, but most of that is `thoughts/` research docs. Core code changes are ~10,139 lines in `harness/`.

### Testing the dashboard
```bash
# Dev login
curl -s -c /tmp/cookies.txt -X POST http://localhost:8080/dev/auth/login -d 'username=test&role=admin' -o /dev/null

# View dashboard
curl -s -b /tmp/cookies.txt http://localhost:8080/swarm

# Send chat message
curl -s -b /tmp/cookies.txt -X POST http://localhost:8080/swarm/chat \
  -H "Content-Type: application/json" \
  -d '{"chat_input":"hello","selected_task_id":"<task-id>"}'
```

### Temporal service
```bash
# Check Temporal is running
sudo systemctl status temporal-dev
# List workflows
temporal workflow list --namespace swarm
# List schedules
temporal schedule list --namespace swarm
```

### Key environment variables for swarm
- `CM_SWARM_TEMPORAL=true` — Enable swarm manager
- `HARNESS_HOOK_URL=http://localhost:8080` — Hook callback URL
- `CM_HOOK_SECRET` — Shared secret for API auth
- `ANTHROPIC_API_KEY` — For agent LLM calls
