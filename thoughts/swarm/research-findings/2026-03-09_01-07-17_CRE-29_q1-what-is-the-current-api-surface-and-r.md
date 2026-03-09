---
question: What is the current API surface and request-routing path for swarm tasks (including endpoint handlers, task-type selection, DB record creation, and SSE/UI update plumbing), and where would automatic task classification and routing hook into that flow?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/server/swarm_api.go
  - harness/internal/server/swarm_dashboard.go
  - harness/internal/db/queries/swarm.sql
---

Swarm task entry points are split between a hook-authenticated JSON API and an approved-user dashboard surface.

## API surface and route registration

- `server.go:167-174` registers hook-secret-protected Swarm API routes under `/api/swarm`:
  - `GET /tasks` → `handleSwarmListTasks`
  - `POST /tasks/research` → `handleSwarmStartResearch`
  - `POST /tasks/code-change-plan` → `handleSwarmStartCodePlan`
  - `GET /tasks/:taskID` → `handleSwarmGetTask`
  - `GET /tasks/:taskID/spans` → `handleSwarmGetTaskSpans`
  - `GET /tasks/:taskID/metrics` → `handleSwarmGetTaskMetrics`
  - `POST /tasks/:taskID/cancel` → `handleSwarmCancelTask`
- `server.go:203-208` registers approved-user dashboard routes:
  - `GET /swarm` page render
  - `GET /swarm/events` SSE stream
  - `POST /swarm/start` Datastar form start
  - `POST /swarm/cancel` Datastar cancel
  - `POST /swarm/chat` task chat message
  - `GET /swarm/artifacts/:id/view` artifact file serve

## Request-routing path for starting tasks (API)

- `swarm_api.go:20-114` centralizes task startup in `startSwarmTask`.
- Handler selection is explicit by endpoint:
  - `handleSwarmStartResearch` passes `PrimitiveTypeResearch` and `SwarmManager.StartResearch` (`swarm_api.go:117-128`)
  - `handleSwarmStartCodePlan` passes `PrimitiveTypeCodeChangePlan` and `SwarmManager.StartCodePlan` (`swarm_api.go:130-144`)
- Request body bind/validation expects `{requestText, ticketID}` and requires non-empty `requestText` (`swarm_api.go:30-39`).
- If `ticketID` is empty and `LinearClient` is configured, Linear issue auto-creation is performed with labels derived by primitive type (`swarm_api.go:41-61`, `swarm_api.go:319-327`).
- DB creation flow:
  - New short task ID (`uuid[:8]`) and RFC3339 timestamps (`swarm_api.go:63-65`)
  - Insert `swarm_tasks` row in `pending` status (`swarm_api.go:67-76`; SQL in `db/queries/swarm.sql:4-6`)
- Workflow launch and persistence:
  - Call selected starter (`swarm_api.go:82`)
  - On startup error, update task status to `failed` (`swarm_api.go:86-104`; SQL in `db/queries/swarm.sql:8`)
  - On success, persist `workflow_id` (`swarm_api.go:106-114`; SQL in `db/queries/swarm.sql:11`)
  - Return `202` JSON `{taskID, workflowID, ticketID}` (`swarm_api.go:116-120`)

## Request-routing path for starting tasks (Dashboard)

- `handleSwarmStartTaskDashboard` receives Datastar signals `new_task_type`, `new_task_text`, `new_task_ticket` (`swarm_dashboard.go:217-223`).
- Task-type selection is done from `new_task_type` cast to `PrimitiveType`; only `research` and `code-change-plan` are accepted, else defaults to `research` (`swarm_dashboard.go:234-239`).
- DB create/start/update sequence mirrors API path:
  - `CreateSwarmTask` pending insert (`swarm_dashboard.go:247-256`)
  - Branch to `StartResearch` or `StartCodePlan` (`swarm_dashboard.go:267-281`)
  - On workflow start failure: mark `failed` (`swarm_dashboard.go:283-305`)
  - Persist workflow ID (`swarm_dashboard.go:313-321`)

## Read/list/detail/cancel plumbing

- List tasks: `ListSwarmTasks` (`swarm_api.go:210-218`; SQL `swarm.sql:19-20`).
- Get task detail: combines task + artifacts + spans (`swarm_api.go:147-174`; SQL `swarm.sql:15`, `50`, `91`).
- Spans endpoint: `GetSwarmSpanTree` only (`swarm_api.go:221-233`; SQL `swarm.sql:91-92`).
- Metrics endpoint: computes aggregate values from span metadata, adds task status/type (`swarm_api.go:236-263`, `266-316`).
- Cancel endpoint: resolves task → checks stored workflow ID → `SwarmManager.CancelTask` (`swarm_api.go:177-207`).

## SSE/UI update plumbing

- Dashboard SSE stream subscribes to EventBus topic `"swarm"` and refreshes sidebar/detail on each event (`swarm_dashboard.go:125-176`).
- Heartbeat every 30s also refreshes sidebar and running/pending detail panes (`swarm_dashboard.go:20`, `128-197`).
- UI patch helpers:
  - Sidebar patch target `#swarm-sidebar` (`swarm_dashboard.go:88-104`)
  - Detail patch target `#swarm-detail` (`swarm_dashboard.go:107-118`)
- Selected task behavior uses `firstActiveTaskID` (running/pending preferred, then first) for what detail pane is shown during updates (`swarm_dashboard.go:81-86`, `141`, `164`).
- Start-task dashboard POST returns SSE response that clears form signals, sets selected task + active tab, patches sidebar/detail immediately (`swarm_dashboard.go:331-376`).
- Chat POST inserts `swarm_task_messages`, appends a chat bubble in response SSE, and publishes `"swarm"` event so other clients refresh (`swarm_dashboard.go:492-556`; SQL `swarm.sql:116-121`).

## Where automatic task classification/routing would hook in this existing flow

Current routing decision points are the places where primitive type is chosen before `CreateSwarmTask` + workflow start:

- API path: before calling `startSwarmTask`, currently split into two dedicated endpoints (`/tasks/research`, `/tasks/code-change-plan`) that hardcode primitive type + starter (`swarm_api.go:117-144`).
- Dashboard path: inside `handleSwarmStartTaskDashboard`, where `new_task_type` is normalized/defaulted and then branched to `StartResearch` vs `StartCodePlan` (`swarm_dashboard.go:234-239`, `267-281`).

In the present structure, classification/routing logic would integrate at those primitive-type selection points, prior to `CreateSwarmTask` insertion and before starter dispatch, because the chosen primitive type is persisted in `swarm_tasks.primitive_type` and drives both workflow start function and Linear labeling (`swarm_api.go:67-71`, `319-327`; `swarm_dashboard.go:247-251`).
