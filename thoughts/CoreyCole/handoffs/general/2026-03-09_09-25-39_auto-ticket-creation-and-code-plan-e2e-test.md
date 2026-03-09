---
date: 2026-03-09T09:25:39-07:00
researcher: CoreyCole
git_commit: 703d56ed241b458cd6abf296d47fed5f6ced9fdb
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Auto-Ticket Creation + Code Plan E2E Test"
tags: [swarm, linear, temporal, dashboard, implementation]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Auto-Ticket Creation + Code Plan E2E Test + Dashboard Polish

## Task(s)

### Auto-create Linear tickets when ticketID not provided — COMPLETED
Implemented auto-ticket creation so every swarm task is tracked in Linear. When `ticketID` is omitted from `POST /api/swarm/tasks/*`, the server creates a Linear issue from `requestText` with appropriate labels and passes it through the workflow.

### Code change plan E2E test — COMPLETED
Ran a full `CodeChangePlanWorkflow` end-to-end for CRE-29 (auto-created). The workflow planned implementation of remaining agent primitives from the flowchart. All 15 agents completed successfully in ~5 min, $1.42 cost.

### Swarm dashboard polish — NOT STARTED
User has noticed a bug on the swarm dashboard at `https://claude-2.tailcdc985.ts.net/swarm`. This is the next task.

## Critical References

- `thoughts/swarm/agent-primitives-flowchart.html` — full architecture flowchart of all agent primitives
- `thoughts/CoreyCole/handoffs/general/2026-03-09_00-55-31_swarm-linear-phase-2.5-3-tested-and-pushed.md` — prior handoff with full branch context
- PR #2: https://github.com/CoreyCole/creative-mode/pull/2 — PR for the full `feat/agent-primitives` branch

## Recent changes

**Not yet committed** — 3 files modified:

- `harness/main.go:500-520` — Extracted `initLinearClient()` from `initSwarmManager()` so Linear client is shared between swarm manager and server. `initSwarmManager` now takes `linearClient` as a parameter.
- `harness/main.go:338-352` — Call site: `initLinearClient(logger)` before `initSwarmManager`, then `srv.LinearClient = linearClient` on the server.
- `harness/internal/server/server.go:35,61` — Added `linear` import and `LinearClient *linear.Client` field to `Server` struct.
- `harness/internal/server/swarm_api.go:43-64` — Auto-create Linear ticket in `startSwarmTask()` when `req.TicketID == ""` and `s.LinearClient != nil`. Uses `labelsForPrimitive()` to select labels.
- `harness/internal/server/swarm_api.go:110` — Response now includes `ticketID` in the 202 JSON.
- `harness/internal/server/swarm_api.go:318-327` — New `labelsForPrimitive()` helper maps `PrimitiveType` to Linear label sets.

## Learnings

### `source .env` doesn't work in curl one-liners
The `CM_HOOK_SECRET` env var needs to be read directly. The `source` command in bash one-liners with the Bash tool doesn't always export vars correctly. Use the literal secret value or `grep` it from `.env`.

### API response field names are PascalCase
The swarm API returns Go-style PascalCase JSON (from sqlc): `.task.Status`, `.task.LinearIssueID`, `.spans[].SpanType`, `.spans[].Name`. NOT camelCase. This is because sqlc generates Go structs with PascalCase tags.

### Code plan workflow runs child research workflow
`CodeChangePlanWorkflow` embeds a `ResearchWorkflow` as a child workflow (ID: `swarm-research-child-{taskID}`). The child handles question generation → parallel research → synthesis. The parent then runs planning: orchestrator → specialist planners → plan synthesizer. Two `linear-context-processor.js` runs: one after research, one after planning.

### Workflow metrics
CRE-29 code plan: 15 agents, 59 LLM calls, 84 tool calls, 567K tokens, $1.42 cost, ~5 min duration.

## Artifacts

- `harness/internal/server/swarm_api.go` — auto-ticket creation + `labelsForPrimitive()`
- `harness/internal/server/server.go` — `LinearClient` field on Server
- `harness/main.go` — `initLinearClient()` extracted, shared with server
- `thoughts/swarm/project-plans/2026-03-09_01-07-15_CRE-29_implement-the-remaining-agent-primitives.md` — 9-phase plan doc (output of the code plan workflow)
- `thoughts/swarm/research/2026-03-09_01-07-17_CRE-29_implement-the-remaining-agent-primitives.md` — research doc (output)

## Action Items & Next Steps

1. **Commit the auto-ticket-creation changes** — 3 files, ~30 insertions. All passing `just check`.

2. **Polish the swarm dashboard** — User has noticed a bug at `https://claude-2.tailcdc985.ts.net/swarm`. Need to investigate:
   - Dashboard code: `harness/internal/server/swarm_dashboard.go`
   - Dashboard templates: `harness/views/swarm/dashboard.templ`
   - SSE events: `handleSwarmDashboardSSE` in `swarm_dashboard.go`

3. **Review CRE-29 plan doc** — The 9-phase plan at `thoughts/swarm/project-plans/2026-03-09_01-07-15_CRE-29_implement-the-remaining-agent-primitives.md` should be reviewed before implementation begins.

## Other Notes

### Swarm dashboard URL
`https://claude-2.tailcdc985.ts.net/swarm` — the harness on the Tailscale VPS. Requires auth (Discord OAuth or dev login).

### CRE-29 was auto-created
The test run created CRE-29 automatically — this is a real Linear ticket with structured comments from both post-processor runs, plus research/plan artifact links.

### Existing test tickets
CRE-26, CRE-27, CRE-28 were created during prior testing (follow-ups from CRE-5 research). CRE-29 is from this session's code plan test. May want to clean up or keep for reference.

### The 9-phase plan summary
1. Data Model & Lifecycle Foundation (DB schema)
2. Temporal Workflow Primitives (7 new workflows)
3. Task Classification & Routing (unified endpoint + classifier agent)
4. Plan Revision Loop (human gate + versioned plans)
5. Implementation & Verification Loop ("Max's Bolt")
6. PR Creation (GitHub wrapper + drafter agents)
7. Human PR Review Gate (merge/revision/restart)
8. Project Workflow (decomposition + DAG + Graphite)
9. Docs & Rollout (staged enablement)
