---
date: 2026-03-02T16:59:23-08:00
researcher: CoreyCole
git_commit: 60edd4cfd0dafaed729dbe500a24c75c94204031
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Project Launch + VPS Performance Fixes"
tags: [swarm, infrastructure, project-workflow, bug-fixes]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Project Launch + VPS Performance Fixes

## Task(s)

### VPS Performance Audit Fixes (completed)
Implemented remaining items from the VPS performance audit handoff (`thoughts/CoreyCole/handoffs/general/2026-03-02_15-19-18_vps-performance-audit-fixes.md`):
- Disabled Docker/containerd on VPS (~150 MB RAM freed)
- Fixed corrupt zsh history
- Added `Requires=temporal-dev.service` to systemd unit
- Added SSE heartbeat to mayor dashboard
- Added `ctx.Done()` to `watchSession` (which uncovered critical bugs, see below)

### Swarm Project Launch Bugs (completed — code fixes)
Approving the CRE-8 tech debt project gate exposed several critical bugs:
1. **watchSession used HTTP request context** — goroutines died when the approval HTTP response completed. Fixed with a Manager lifecycle context (`m.ctx`/`m.cancel`).
2. **Missing migration** — `011_prompt_versions_and_tokens.sql` existed but wasn't registered in `db.go`'s `migrationFiles` slice. Caused `no such column: input_tokens` errors that triggered false `infra_failure` results.
3. **No capacity throttling** — `SpawnProjectWorkflows` and `AdvanceProject` spawned ALL ready tickets at once, overwhelming the system. Added `maxSessions` cap.
4. **Research re-spawning** — Decompose-phase research children (CRE-8-r1..r5) were re-spawned because they had different ticket IDs than the original research workflows (CRE-9..CRE-13). Added `completedChildTickets` filtering.

### CRE-8 Tech Debt Project (in progress — swarm running)
The CRE-8 project plan was approved and 4 of 12 code tickets are actively running research phase:
- **Running**: CRE-8-1 (secret middleware), CRE-8-2 (HTTP hardening), CRE-8-3 (DB improvements), CRE-8-4 (pkg tests)
- **Queued**: CRE-8-5 through CRE-8-12 (CI, Rust templates, Go file splits, discordauth extraction)
- **Orchestrator**: `project-orch-e8c72519` Temporal workflow polls every 2 min to advance waves

## Critical References
- Project plan: `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md`
- Swarm orchestrator: `harness/internal/swarmorch/manager.go` (Manager struct, lifecycle ctx, watchSession)
- Project workflow logic: `harness/internal/swarmorch/project.go` (SpawnProjectWorkflows, advanceProjectVerify)

## Recent changes

- `harness/internal/swarmorch/manager.go:48-61` — Added `ctx`/`cancel` lifecycle context fields to Manager struct
- `harness/internal/swarmorch/manager.go:100-103` — Initialize lifecycle ctx in NewManager
- `harness/internal/swarmorch/manager.go:123-125` — Added `Shutdown()` method that cancels lifecycle ctx
- `harness/internal/swarmorch/manager.go:497-500,530-533` — watchSession uses `m.ctx.Done()` instead of passed-in ctx
- `harness/internal/swarmorch/project.go:186-232` — SpawnProjectWorkflows now filters completed children and caps at maxSessions
- `harness/internal/swarmorch/activities.go:309-312` — AdvanceProject caps new workflow starts at maxSessions
- `harness/internal/db/db.go:116` — Registered `011_prompt_versions_and_tokens.sql` migration
- `harness/main.go:414` — Wired `swarmManager.Shutdown()` in graceful shutdown
- `harness/internal/server/mayor_dashboard.go:83-85,117-120` — Added SSE heartbeat ticker
- `scripts/vps-bootstrap.sh:537-538` — Added `After=temporal-dev.service` and `Requires=temporal-dev.service`

## Learnings

### Context propagation hazard in goroutines
When spawning goroutines from HTTP handlers, NEVER pass the request context to long-lived goroutines. The request context is canceled when the HTTP response completes. Use a server/manager-level lifecycle context instead. This pattern appears in `spawnSession` → `watchSession` and could exist in other places.

### Migration registration is manual
Migrations in `harness/internal/db/db.go` must be manually added to the `migrationFiles` slice. The `011_prompt_versions_and_tokens.sql` file existed on disk but wasn't registered, causing silent column-missing errors. Always verify new migrations are in the slice.

### Child ticket ID format breaks Linear API
Project child tickets get identifiers like `CRE-8-1` (parent-number format). The Linear client's `parseIdentifier` expects `TEAM-NUMBER` format, so `CRE-8-1` fails to parse. The Linear API errors are non-fatal (logged but don't block workflows), but Linear comments/status updates are silently skipped for all child tickets.

### Decompose vs Plan ticket identity mismatch
The decompose phase creates research children with Linear-assigned identifiers (CRE-9, CRE-10, etc.). The plan phase creates code children with internal identifiers (CRE-8-1, CRE-8-2, etc.). If the plan also references research topics, those get NEW ticket entries (CRE-8-r1, etc.) that don't match the existing research workflows. The fix was to delete the research entries from the project graph and use `completedChildTickets` filtering.

### DB cleanup for stuck workflows
Useful commands for cleaning up stuck swarm state:
```bash
# Check running workflows
sqlite3 data/creative-mode.db "SELECT id, ticket_id, phase, status FROM swarm_workflows WHERE status = 'running';"
# Cancel orphaned sessions
sqlite3 data/creative-mode.db "UPDATE swarm_sessions SET completed_at = datetime('now'), result = 'infra_failure' WHERE completed_at IS NULL;"
# Reset a workflow to a gate
sqlite3 data/creative-mode.db "UPDATE swarm_workflows SET phase = 'project_review', status = 'awaiting_review', gate_phase = 'project_review' WHERE id = '<id>';"
# Cancel Temporal orchestrator
temporal workflow cancel --workflow-id project-orch-<id> --namespace swarm
```

## Artifacts

- `thoughts/CoreyCole/handoffs/general/2026-03-02_16-59-23_swarm-project-launch-and-vps-fixes.md` — This handoff
- `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md` — CRE-8 project plan (12 tickets, 3 waves)
- `thoughts/swarm/research/2026-03-02_06-00-19_CRE-8_tech-debt-audit.md` — CRE-8 parent research
- `thoughts/swarm/retrospectives/2026-03-03-CRE-8-*.md` — Retrospectives from failed first-run attempts
- `thoughts/swarm/digests/2026-03-03_digest.md` — Daily learning digest

## Action Items & Next Steps

1. **Monitor swarm progress**: Check `tmux list-sessions | grep cm-swarm` and `curl localhost:8080/api/swarm/health -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)"` to see how research sessions are progressing. As sessions complete, the orchestrator should advance them through `code_plan → implement → verify → pr → human_review`.

2. **Review human_review gates**: Each code ticket will pause at `human_review` after PR creation. Monitor via `/swarm` dashboard or `GET /api/swarm/gate/pending`.

3. **Fix Linear child ticket ID parsing** (known issue): Child ticket IDs like `CRE-8-1` fail Linear's `parseIdentifier`. Either:
   - Change `CreateProjectTicketsFromPlan` to actually create Linear tickets first and use the real identifier, or
   - Make `parseIdentifier` handle the `TEAM-PARENT-CHILD` format
   - Location: `harness/internal/linear/client.go:575` (`parseIdentifier`), `harness/internal/swarmorch/project.go:82` (identifier generation)

4. **Fix research re-spawn at plan level**: The real fix is to not include decompose-phase research children in the plan's project graph. Currently we deleted them from `swarm_tickets` manually. Consider adding a `ticket_type` column or filtering by workflow phase. Location: `harness/internal/swarmorch/project.go:538` (`buildProjectGraph`)

5. **Add capacity check to StartWorkflow/spawnSession**: Currently capacity is only checked at the caller level (SpawnProjectWorkflows, AdvanceProject). A defense-in-depth approach would add a capacity gate to `spawnSession` itself. Location: `harness/internal/swarmorch/manager.go:347` (`spawnSession`)

6. **VPS performance**: Docker is disabled, zsh history fixed, Temporal systemd dependency added. Run `systemctl status docker containerd` to verify they stay disabled across reboots.

## Other Notes

### Swarm workflow status tracking
- **Swarm dashboard**: `https://<tailscale-dns>/swarm` shows all workflows, events, metrics
- **Workflow detail**: `https://<tailscale-dns>/swarm/<id>` shows phase timeline, sessions, gate review panel
- **Health endpoint**: `GET /api/swarm/health` — shows capacity, active workflows, stall detection
- **Gate actions**: `POST /api/swarm/gate/<id>/approve` or `/reject` (needs `X-Hook-Secret` header)

### Temporal monitoring
- **Workflow list**: `temporal workflow list --namespace swarm`
- **Schedule list**: `temporal schedule list --namespace swarm`
- **UI dashboard**: `http://localhost:8233` (Temporal Web UI)
- **Project orchestrator**: `project-orch-e8c72519` — polls every 2 min, calls `AdvanceProject` activity

### Swarm config
Current config (check with `sqlite3 data/creative-mode.db "SELECT config FROM swarm_config WHERE id = 'default';"`:
- `maxSessions: 4` — concurrent Claude Code sessions
- `gateProjectReview: true` — human gate at project review
- `gatePlanReview: false` — no gate at individual plan review (code tickets auto-advance)
- `maxPlanRevisions: 3`, `maxVerifyRetries: 3`
