# Swarm Human Gates & Post-PR Lifecycle Implementation Plan

## Overview

The swarm agent system is fully autonomous — no human gates exist anywhere in the workflow. This plan adds:
1. **Human gates** at plan review and project review (configurable)
2. **Post-PR lifecycle** — a `human_review` phase after PR creation where humans approve/reject before the workflow completes

Currently, `PhasePR` success → `PhaseDone` (workflow ends). There is no mechanism to pause, wait for human input, or route rejection feedback back to earlier phases.

## Current State Analysis

The swarm orchestrator (`harness/internal/swarmorch/manager.go`) manages workflows through a state machine (`harness/internal/swarm/statemachine.go`). Key facts:

- **State machine** (`statemachine.go:79-183`): `DetermineNextPhase()` maps phase+result → next phase. `PhasePR` success → `PhaseDone`.
- **Manager advancement** (`manager.go:570-805`): `advanceWorkflow()` reads transition, updates DB, spawns next session or marks terminal.
- **Enums** (`enums.go`): Phases include `research`, `code_plan`, `plan_review`, `implement`, `verify`, `pr`, `project_plan`, `project_review`, `project_verify`, `done`, `failed`. Statuses: `pending`, `running`, `completed`, `failed`, `canceled`.
- **Config** (`statemachine.go:15-22`): `SwarmConfig` has `MaxSessions`, `HeartbeatSeconds`, `StallMinutes`, `MaxPlanRevisions`, `MaxVerifyRetries`, `RetryBackoffSecs`.
- **DB schema** (`migrations/006_swarm_tables.sql`): CHECK constraints on `phase` and `status` columns.
- **Events** (`events/types.go`): EventBus constants for swarm lifecycle events.
- **Alerts** (`alerts.go`): Discord alerts with 1-hour dedup.
- **Health** (`health.go`): `queryActiveWorkflows` only queries `status = 'running'`.
- **CancelWorkflow** (`manager.go:203-238`): Only accepts `running` or `pending` status.
- **Dashboard** (`views/swarm/dashboard.templ`): Workflow detail page with status badge, cancel button, sessions, milestones, events.
- **API routes** (`server.go:165-218`): Swarm API at `/api/swarm/*` (hookSecret auth), dashboard at `/swarm/*` (approved user auth).

### Key Discoveries:
- `advanceWorkflow()` is mutex-protected and re-reads workflow under lock (`manager.go:575-584`)
- `buildEnv()` assembles `SwarmEnv` struct → `ToMap()` via reflection (`env.go:45-68`)
- `SkillForPhase()` maps phases to skill names; terminal phases return `""` (`statemachine.go:186-211`)
- `spawnSession()` errors on empty skill (`manager.go:296-298`)
- `linearUpdateStatus()` uses Linear status constants like `StatusInReview` (`linear/client.go:26`)
- `toNullString()` helper in `learnings.go:210`, `nowUTC()` in `project.go:449`

## Desired End State

- Workflows can be configured to pause at `plan_review`, `project_review`, and after `pr` (always)
- Gated workflows enter `awaiting_review` status and wait indefinitely for human action
- Humans approve (advance) or reject (send back with feedback) via API and dashboard UI
- Rejection feedback is passed to the next Claude session as `CM_SWARM_REVIEW_FEEDBACK`
- Discord alerts fire when a gate is reached
- Linear status updates to "In Review" at gates
- The heartbeat naturally skips `awaiting_review` workflows (they're not `running`)

## What We're NOT Doing

- Cross-project dependencies
- Code change sub-type routing (feature/bugfix/prototype/refactor)
- Temporal signals (unnecessary — DB status-based gating is simpler and works in both Temporal and goroutine modes)
- Auto-approve timeouts (wait indefinitely)

---

## Phase 1: DB Migration & Types

### Overview
Add database support for the new `human_review` phase, `awaiting_review` status, gate-related event types, and the `swarm_gate_reviews` audit table.

### Changes Required:

#### 1. Migration `008_human_gates.sql`
**File**: `harness/internal/db/migrations/008_human_gates.sql` (NEW)

Recreate `swarm_workflows` with:
- New CHECK values: `phase` adds `human_review`; `status` adds `awaiting_review`
- New columns: `gate_phase TEXT`, `review_feedback TEXT`

Recreate `swarm_events` with new event types: `gate_reached`, `gate_approved`, `gate_rejected`

New table `swarm_gate_reviews`:
```sql
CREATE TABLE swarm_gate_reviews (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    gate_phase   TEXT NOT NULL,
    action       TEXT NOT NULL CHECK(action IN ('approve', 'reject')),
    feedback     TEXT,
    reviewer     TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
```

#### 2. New sqlc queries
**File**: `harness/internal/db/queries/swarm.sql` (append)
- `UpdateSwarmWorkflowGate :exec` — set status=awaiting_review, gate_phase
- `ClearSwarmWorkflowGate :exec` — set status=running, clear gate_phase/review_feedback
- `UpdateSwarmWorkflowReviewFeedback :exec` — store rejection feedback
- `ListAwaitingReviewSwarmWorkflows :many` — list gated workflows

**File**: `harness/internal/db/queries/swarm_gate_reviews.sql` (NEW)
- `CreateSwarmGateReview :exec`
- `ListSwarmGateReviewsByWorkflow :many`

#### 3. Regenerate sqlc
Run `cd harness && sqlc generate`

#### 4. sqlc.yaml overrides
Add type overrides for new columns:
- `gate_phase` → nullable string
- `review_feedback` → nullable string
- `swarm_gate_reviews.action` → string

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go run . &` starts without errors (migration applies)
- [ ] `cd harness && sqlc generate` succeeds
- [ ] `cd harness && go build ./...` compiles

---

## Phase 2: Enum & State Machine Changes

### Overview
Add `PhaseHumanReview`, `StatusAwaitingReview`, gate event types, config flags, state machine transition `PhasePR` → `PhaseHumanReview`, and gate helper functions.

### Changes Required:

#### 1. New enums
**File**: `harness/internal/swarm/enums.go`

```go
PhaseHumanReview Phase = "human_review"  // + add to Valid()
StatusAwaitingReview WorkflowStatus = "awaiting_review"  // + add to Valid()
EventGateReached EventType = "gate_reached"  // + add to Valid()
EventGateApproved EventType = "gate_approved"  // + add to Valid()
EventGateRejected EventType = "gate_rejected"  // + add to Valid()
```

#### 2. Config extension
**File**: `harness/internal/swarm/statemachine.go`

Add to `SwarmConfig`:
```go
GatePlanReview    bool `json:"gatePlanReview"`
GateProjectReview bool `json:"gateProjectReview"`
```
Defaults: `false` (gates disabled — opt-in)

#### 3. State machine changes
**File**: `harness/internal/swarm/statemachine.go`

- `PhasePR` success → `PhaseHumanReview` (instead of `PhaseDone`)
- New case `PhaseHumanReview` success → `PhaseDone`
- `SkillForPhase(PhaseHumanReview)` returns `""` (no Claude session)

#### 4. Gate helper functions
**File**: `harness/internal/swarm/statemachine.go`

```go
// IsGatedTransition returns true if the phase+result+config combination should
// pause for human review instead of advancing automatically.
func IsGatedTransition(phase Phase, result SessionResult, config SwarmConfig) bool {
    if result != ResultSuccess {
        return false
    }
    switch phase {
    case PhasePlanReview:
        return config.GatePlanReview
    case PhaseProjectReview:
        return config.GateProjectReview
    default:
        return false
    }
}

// GateRejectionTarget returns the phase to send the workflow back to when a gate is rejected.
func GateRejectionTarget(gatePhase Phase) (Phase, bool) {
    switch gatePhase {
    case PhasePlanReview:
        return PhaseCodePlan, true
    case PhaseProjectReview:
        return PhaseProjectPlan, true
    case PhasePR, PhaseHumanReview:
        return PhaseImplement, true
    default:
        return "", false
    }
}
```

#### 5. New EventBus constants
**File**: `harness/internal/events/types.go`

```go
EventSwarmGateReached  = "swarm.gate_reached"
EventSwarmGateApproved = "swarm.gate_approved"
EventSwarmGateRejected = "swarm.gate_rejected"
```

#### 6. New event structs
**File**: `harness/internal/swarmorch/events.go`

```go
// GateReachedEvent is published when a workflow enters a human review gate.
type GateReachedEvent struct {
    Event      string `json:"event"`
    WorkflowID string `json:"workflow_id"`   //nolint:tagliatelle // EventBus compat
    TicketID   string `json:"ticket_id"`     //nolint:tagliatelle // EventBus compat
    GatePhase  string `json:"gate_phase"`    //nolint:tagliatelle // EventBus compat
}

// GateReviewedEvent is published when a human approves or rejects at a gate.
type GateReviewedEvent struct {
    Event      string `json:"event"`
    WorkflowID string `json:"workflow_id"`   //nolint:tagliatelle // EventBus compat
    TicketID   string `json:"ticket_id"`     //nolint:tagliatelle // EventBus compat
    GatePhase  string `json:"gate_phase"`    //nolint:tagliatelle // EventBus compat
    Action     string `json:"action"`
    Reviewer   string `json:"reviewer"`
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./...` compiles
- [ ] `cd harness && go test ./internal/swarm/...` passes
- [ ] State machine test: `PhasePR` success → `PhaseHumanReview`
- [ ] State machine test: `PhaseHumanReview` success → `PhaseDone`

---

## Phase 3: Manager Gate Logic

### Overview
Add gate interception in `advanceWorkflow()`, new `enterGate`/`ApproveGate`/`RejectGate` methods, environment passthrough, and alert extension.

### Changes Required:

#### 1. `advanceWorkflow()` gate interception
**File**: `harness/internal/swarmorch/manager.go`

Before computing transition, check `IsGatedTransition()`. If gated, call `enterGate()` and return.

After computing transition, if `NextPhase == PhaseHumanReview`, update phase and call `enterGate()`.

#### 2. New methods on Manager

**`enterGate(ctx, wf, gatePhase)`**:
- `UpdateSwarmWorkflowGate` (sets status=awaiting_review, gate_phase)
- `emitEvent` with EventGateReached
- Publish `GateReachedEvent` to EventBus
- `alertMgr.FireGateReached(ticketID, phase)`
- `linearComment` + `linearUpdateStatus(InReview)`

**`ApproveGate(ctx, workflowID, reviewer) error`**:
- Validate status is `awaiting_review`
- Create gate review audit record
- Clear gate (set status=running)
- Emit EventGateApproved + Linear comment
- Route by gate_phase:
  - `plan_review` → `implement` (advance phase, spawn session)
  - `project_review` → `project_verify` (spawn children)
  - `pr`/`human_review` → `done` (mark complete)

**`RejectGate(ctx, workflowID, reviewer, feedback) error`**:
- Validate status is `awaiting_review`
- Create gate review audit record
- Store feedback on workflow (`UpdateSwarmWorkflowReviewFeedback`)
- Clear gate, set phase to rejection target (increment attempt)
- Emit EventGateRejected + Linear comment
- Spawn next session (at rejection target phase)

#### 3. Environment passthrough
**File**: `harness/internal/swarm/env.go`

Add `ReviewFeedback` field to `SwarmEnv`:
```go
ReviewFeedback string `envconfig:"CM_SWARM_REVIEW_FEEDBACK"`
```

In `buildEnv()` (`manager.go`), pass `wf.ReviewFeedback` if set (the new nullable column on `swarm_workflows`).

#### 4. CancelWorkflow update
**File**: `harness/internal/swarmorch/manager.go`

Accept `awaiting_review` status as cancellable:
```go
if wf.Status != swarm.StatusRunning && wf.Status != swarm.StatusPending && wf.Status != swarm.StatusAwaitingReview {
    return fmt.Errorf(...)
}
```

#### 5. Alert extension
**File**: `harness/internal/swarmorch/alerts.go`

Add `FireGateReached(ticketID string, phase swarm.Phase)`:
```go
func (a *AlertManager) FireGateReached(ticketID string, phase swarm.Phase) {
    key := fmt.Sprintf("gate:%s:%s", ticketID, phase)
    msg := fmt.Sprintf(
        "**[SWARM] Gate Reached**\nTicket: `%s`\nPhase: `%s`\nHuman review required — approve or reject in the dashboard.",
        ticketID, phase,
    )
    a.fireAsync(key, msg)
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./...` compiles
- [ ] `cd harness && go test ./internal/swarmorch/...` passes

---

## Phase 4: API Endpoints

### Overview
Add HTTP handlers for approve/reject/list-gated operations, both via hookSecret API and dashboard auth.

### Changes Required:

#### 1. New handlers
**File**: `harness/internal/server/swarm_api.go` (append)

- `handleSwarmApproveGate(c echo.Context) error` — POST, validates workflow is awaiting_review, calls `manager.ApproveGate()`
- `handleSwarmRejectGate(c echo.Context) error` — POST, requires `feedback` field, calls `manager.RejectGate()`
- `handleSwarmListGated(c echo.Context) error` — GET, returns list of awaiting_review workflows

#### 2. Dashboard handlers
**File**: `harness/internal/server/swarm_dashboard.go` (append)

- `handleSwarmDashboardApprove` — POST `/swarm/:id/approve`, approved user auth, reviewer from session user
- `handleSwarmDashboardReject` — POST `/swarm/:id/reject`, approved user auth, reads feedback from signals/form

#### 3. Route registration
**File**: `harness/internal/server/server.go`

API routes (hookSecret auth):
```go
swarmGroup.POST("/gate/:id/approve", s.handleSwarmApproveGate)
swarmGroup.POST("/gate/:id/reject", s.handleSwarmRejectGate)
swarmGroup.GET("/gate/pending", s.handleSwarmListGated)
```

Dashboard routes (approved user auth):
```go
approved.POST("/swarm/:id/approve", s.handleSwarmDashboardApprove)
approved.POST("/swarm/:id/reject", s.handleSwarmDashboardReject)
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./...` compiles
- [ ] `curl -X POST localhost:8080/api/swarm/gate/test/approve` returns appropriate error (401 or 404)

---

## Phase 5: Dashboard UI

### Overview
Add visual indicators and interactive controls for human gates in the swarm dashboard.

### Changes Required:

#### 1. Status badge
**File**: `harness/views/swarm/dashboard.templ`

Add purple `awaiting_review` badge styling to `statusClass()`:
```go
case swarm.StatusAwaitingReview:
    return "bg-purple-900/50 text-purple-300"
```

#### 2. Gate review panel
On workflow detail page (`WorkflowPage`), when `status == awaiting_review`:
- Show gate phase and "Awaiting Human Review" banner
- Approve button (POST to `/swarm/:id/approve`)
- Reject textarea + button (POST to `/swarm/:id/reject`)

#### 3. Gate review history
**File**: `harness/views/swarm/dashboard.templ`

Add `GateReviews` to `WorkflowDetailData` and render a timeline section showing approve/reject actions from `swarm_gate_reviews`.

#### 4. SSE updates
**File**: `harness/internal/server/swarm_dashboard.go`

In `handleSwarmDashboardSSE`, handle `swarm.gate_reached`, `swarm.gate_approved`, `swarm.gate_rejected` events to refresh the dashboard (they already have the `swarm.` prefix so they'll be caught by the existing `strings.HasPrefix(eventType, "swarm.")` check — the tab refresh logic applies).

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./...` compiles

#### Manual Verification:
- [ ] Navigate to `/swarm/:id` for a gated workflow, see approve/reject buttons
- [ ] Purple badge displays for `awaiting_review` status

---

## Phase 6: Health & Stall Detection

### Overview
Ensure `awaiting_review` workflows appear in health output but are not flagged as stalled.

### Changes Required:

#### 1. Health endpoint update
**File**: `harness/internal/swarmorch/health.go`

`queryActiveWorkflows`: include `awaiting_review` workflows in the query but mark them with a separate indicator (not stalled):
```sql
WHERE status IN ('running', 'awaiting_review')
```

Add `AwaitingReview bool` field to `ActiveWorkflowInfo`. Set it for awaiting_review workflows. In `determineHealthStatus()`, exclude awaiting_review workflows from stall counting.

#### 2. Metrics
**File**: `harness/internal/swarmorch/metrics.go`

Count `awaiting_review` workflows separately in the workflow totals.

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go test ./internal/swarmorch/...` passes
- [ ] `cd harness && go build ./...` compiles

---

## Phase 7: Tests

### Overview
Add comprehensive tests for all new functionality.

### Changes Required:

#### 1. State machine tests
**File**: `harness/internal/swarm/statemachine_test.go`

New test cases:
- `PhasePR` success → `PhaseHumanReview`
- `PhaseHumanReview` success → `PhaseDone`
- `IsGatedTransition` respects config flags (enabled/disabled for plan_review, project_review)
- `GateRejectionTarget` returns correct targets for each gate phase

#### 2. Manager tests
**File**: `harness/internal/swarmorch/manager_test.go` (or new gate_test.go)

- `ApproveGate`: validates status, advances correctly per gate phase
- `RejectGate`: validates feedback required, sends to correct phase, increments attempt
- `enterGate`: sets correct status and gate_phase
- Double-approve returns error
- Approve after cancel returns error

#### 3. API tests
- Approve/reject endpoints with valid/invalid workflow IDs
- Reject without feedback returns 400

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go test ./internal/swarm/... ./internal/swarmorch/...` all pass
- [ ] `just check` passes

---

## Edge Cases

- **In-flight workflows**: Unaffected — new columns are nullable, new status only entered after migration + config enable
- **Double approve/reject**: Mutex-protected, second call returns error (status check fails)
- **Cancel during gate**: Allowed — CancelWorkflow accepts awaiting_review
- **Full restart on rejection**: Human can reject (→ implement) OR cancel + start new workflow with previousWorkflowID (existing path)
- **Heartbeat**: `ListRunningSwarmWorkflows` only returns `status='running'`, so awaiting_review is naturally skipped
- **No skill for PhaseHumanReview**: `SkillForPhase` returns `""`, and `spawnSession` errors on empty skill — but we never call `spawnSession` for human_review (we call `enterGate` instead)

## Key Files

| File | Changes |
|------|---------|
| `harness/internal/db/migrations/008_human_gates.sql` | NEW: migration |
| `harness/internal/db/queries/swarm.sql` | Append gate queries |
| `harness/internal/db/queries/swarm_gate_reviews.sql` | NEW: review audit queries |
| `harness/internal/swarm/enums.go` | New phase, status, event types |
| `harness/internal/swarm/statemachine.go` | PR→human_review, gate config, helper functions |
| `harness/internal/swarm/env.go` | ReviewFeedback field |
| `harness/internal/swarmorch/manager.go` | enterGate, ApproveGate, RejectGate, advanceWorkflow changes |
| `harness/internal/swarmorch/events.go` | GateReachedEvent, GateReviewedEvent |
| `harness/internal/swarmorch/alerts.go` | FireGateReached |
| `harness/internal/events/types.go` | Gate event constants |
| `harness/internal/server/swarm_api.go` | Approve/reject/list handlers |
| `harness/internal/server/server.go` | Route registration |
| `harness/views/swarm/dashboard.templ` | Dashboard gate UI |
| `harness/internal/swarmorch/health.go` | Include awaiting_review in health |

## References

- State machine: `harness/internal/swarm/statemachine.go`
- Manager: `harness/internal/swarmorch/manager.go`
- DB schema: `harness/internal/db/migrations/006_swarm_tables.sql`
- Dashboard: `harness/views/swarm/dashboard.templ`
- Server routes: `harness/internal/server/server.go:165-218`
