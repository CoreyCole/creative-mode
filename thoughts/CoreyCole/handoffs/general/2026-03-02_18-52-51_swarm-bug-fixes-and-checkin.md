---
date: 2026-03-02T18:52:51-08:00
researcher: CoreyCole
git_commit: 33b6a6c2f97705088e986e1ef0cc0999b5843de2
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Bug Fixes and Progress Check-in"
tags: [swarm, bug-fixes, linear, hook-url, ticket-type]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Bug Fixes and Check-in

## Task(s)

### Fix Linear child ticket ID parsing (completed)
Synthetic child ticket IDs (`CRE-8-1`, `CRE-8-r1`) were failing `parseIdentifier` in the Linear client, causing false error alerts and silently skipping all Linear operations for child workflows. Added `linear.IsLinearIdentifier()` guard to all four Linear API call sites in the manager.

### Fix buildProjectGraph title-based type heuristic (completed)
`buildProjectGraph` was inferring workflow type from `strings.Contains(title, "research")` — fragile and wrong for tickets with ambiguous titles. Added `ticket_type` column to `swarm_tickets` (migration 012), set explicitly at creation time, and used in `buildProjectGraph` instead of the heuristic.

### Fix hook URL for Claude Code sessions (completed)
All 4 research sessions completed their work but were stuck at idle prompt because the session-complete hook was POSTing to `claude-2.tailcdc985.ts.net` (Tailscale DNS → `100.x.x.x` private IP), which Claude Code blocks. Added `HARNESS_HOOK_URL=http://localhost:8080` env var separate from `HARNESS_URL`, and passed it to the swarm Manager.

### Recover stuck sessions and advance workflows (completed)
Manually triggered session-complete hooks for 4 stuck research sessions via `POST /api/swarm/hook/session-complete`. All 4 workflows advanced from `research` → `code_plan` and spawned new sessions with correct localhost hook URLs.

## Critical References
- Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-03-02_16-59-23_swarm-project-launch-and-vps-fixes.md`
- CRE-8 project plan: `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md`

## Recent changes

- `harness/internal/linear/client.go:575-590` — Added `IsLinearIdentifier()` function (validates `TEAM-NUMBER` format)
- `harness/internal/linear/client_test.go:172-197` — Tests for `IsLinearIdentifier`
- `harness/internal/swarmorch/manager.go:1442,1480,1314,1950` — Added `linear.IsLinearIdentifier(ticketID)` guard to `linearComment`, `linearUpdateStatus`, `resolveTicketDescription`, `classifyTicket`
- `harness/internal/swarmorch/manager.go:157,210` — Added `TicketType` field to `UpsertSwarmTicket` calls
- `harness/internal/swarmorch/project.go:84,121,699,723` — Set `TicketType` on all child ticket upserts (`pt.Type` for plan children, `WorkflowTypeResearch` for decompose children)
- `harness/internal/swarmorch/project.go:562-575` — `buildProjectGraph` now reads `t.TicketType` from DB instead of title heuristic
- `harness/internal/db/migrations/012_ticket_type.sql` — `ALTER TABLE swarm_tickets ADD COLUMN ticket_type TEXT NOT NULL DEFAULT 'code'`
- `harness/internal/db/db.go:117` — Registered migration 012
- `harness/internal/db/queries/swarm.sql` — Added `ticket_type` to INSERT and all SELECTs
- `harness/internal/db/queries/swarm_dependencies.sql` — Added `ticket_type` to SELECTs
- `harness/main.go:139-142,249` — Added `HARNESS_HOOK_URL` env var support, passed `hookURL` to swarm Manager
- `harness/.env:28-30` — Added `HARNESS_HOOK_URL=http://localhost:8080`

## Learnings

### HARNESS_URL vs HARNESS_HOOK_URL separation
`HARNESS_URL` is the Tailscale Serve URL for browser access (enables HSTS, secure cookies). But Claude Code sessions run on the same machine and need `http://localhost:8080` for hooks because Claude blocks all private/link-local IPs. These must be separate env vars. The `harnessURL` field in the Manager is used for both hooks config and `CM_HARNESS_URL` env var — both are correct with localhost since sessions are local.

### Recovering stuck sessions
When sessions complete but hooks fail, the session sits at idle prompt indefinitely. Recovery: `POST /api/swarm/hook/session-complete` with `X-Hook-Secret` and `X-Swarm-Session` headers. The heartbeat's `ReapSessions` activity will then clean up orphaned tmux sessions.

### sqlc regeneration with new columns
Adding a column to `swarm_tickets` requires: (1) migration SQL, (2) register in `db.go` migrationFiles, (3) add to INSERT and all SELECT queries in `.sql` files, (4) `sqlc generate`, (5) update all callers of `UpsertSwarmTicketParams` to set the new field, (6) update test schema in `manager_test.go`.

## Artifacts

- `harness/internal/db/migrations/012_ticket_type.sql` — New migration
- `harness/internal/linear/client.go:575-590` — `IsLinearIdentifier` function
- `harness/.env:28-30` — `HARNESS_HOOK_URL` env var
- Swarm-generated code plans (from active sessions):
  - `thoughts/swarm/plans/2026-03-03_02-05-06_CRE-8-4_pkg-pure-function-tests_v1.md`
  - `thoughts/swarm/plans/2026-03-03_02-05-13_CRE-8-1_secret-middleware-hardening_v1.md`
  - `thoughts/swarm/plans/2026-03-03_02-05-32_CRE-8-3_database-improvements_v1.md`
  - `thoughts/swarm/plans/2026-03-03_02-05-38_CRE-8-2_http-hardening_v1.md`

## Action Items & Next Steps

1. **Monitor code_plan sessions**: 4 sessions are actively running `code_plan` phase for CRE-8-1 through CRE-8-4. Check with `tmux capture-pane -t cm-swarm-CRE-8-1-code_plan -p | tail -20`. When they complete, the orchestrator should auto-advance to `plan_review` → `implement` (plan review gate is disabled: `gatePlanReview: false`).

2. **Watch for stuck sessions (hook URL)**: The new sessions use `http://localhost:8080` hooks. If any still get stuck, check `/tmp/swarm-hooks-<session_id>/settings.json` to verify the URL. Recovery: `POST /api/swarm/hook/session-complete` with appropriate headers.

3. **Review human_review gates**: Each code ticket will eventually pause at `human_review` after PR creation. Monitor via `GET /api/swarm/gate/pending` or the `/swarm` dashboard.

4. **Wave 2 tickets**: CRE-8-5 through CRE-8-12 are queued. As Wave 1 tickets (1-4) complete, the Temporal project orchestrator (`project-orch-e8c72519`, polls every 2 min) will spawn Wave 2 tickets up to `maxSessions: 4`.

5. **Known remaining issues from prior handoff**:
   - Research re-spawn root cause at plan level — mitigated by `ticket_type` column but decompose-phase children still share `project_id` with plan children. A future cleanup could filter by `ticket_type` in `ListSwarmTicketsByProject`.
   - Capacity check in `spawnSession` — defense-in-depth, not critical.

## Other Notes

### Swarm monitoring commands
```bash
# Health
curl -s localhost:8080/api/swarm/health -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)" | python3 -m json.tool

# Session output
tmux capture-pane -t cm-swarm-CRE-8-1-code_plan -p | tail -20

# DB status
sqlite3 data/creative-mode.db "SELECT id, ticket_id, phase, status FROM swarm_workflows WHERE status NOT IN ('cancelled','canceled') AND ticket_id LIKE 'CRE-8%';"

# Temporal orchestrator
temporal workflow list --namespace swarm | head -5

# Pending gates
curl -s localhost:8080/api/swarm/gate/pending -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)"

# Recover stuck session
curl -s -X POST localhost:8080/api/swarm/hook/session-complete -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)" -H "X-Swarm-Session: <session_id>"
```

### Current CRE-8 project state
- **Parent**: `e8c72519` — `project_verify` phase, running (Temporal orchestrator polls every 2 min)
- **Wave 1 (running)**: CRE-8-1 (secret middleware), CRE-8-2 (HTTP hardening), CRE-8-3 (DB improvements), CRE-8-4 (pkg tests) — all in `code_plan` phase
- **Wave 1 (queued)**: CRE-8-5 (CI), CRE-8-6 (2D constants), CRE-8-7 (3D split), CRE-8-8 (boardgame) — will start when capacity frees up
- **Wave 2**: CRE-8-9 (split create.go), CRE-8-10 (split project.go), CRE-8-12 (extract discordauth) — after Wave 1
- **Wave 3**: CRE-8-11 (split manager.go) — depends on CRE-8-10
