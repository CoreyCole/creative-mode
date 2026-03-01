# Agent Swarm Primitives v4 — Implementation Plan

## Overview

Build a general-purpose agent swarm system through 14 composable Claude Code skills and a Temporal-driven SQLite state machine. The swarm turns high-level goals into deployed software through research, planning, implementation, verification, and PR creation — all tracked in Linear and observable via a real-time Datastar dashboard.

**Architecture**: Short-lived Temporal workflows + SQLite state machine + EventBus SSE dashboard. No long-running workflows. The heartbeat (scheduled every 2 min) reads state from SQLite, determines next actions, and spawns fire-and-forget child workflows. All orchestration logic is deterministic Go code — zero LLM cost for scheduling. Intelligence lives in the Claude Code worker sessions.

**Key decisions (v4 changes from v3)**:
- **Hook-based session completion** — skills fire `POST /api/swarm/session-complete` via on-stop hook, not tmux polling. Consistent with existing harness pattern (`POST /api/claude-event`).
- **Child workflows, not activity-spawned workflows** — `HeartbeatWorkflow` uses `workflow.ExecuteChildWorkflow()` with `PARENT_CLOSE_POLICY_ABANDON` for fire-and-forget session spawning. No Temporal anti-patterns.
- **Typed Go enums via sqlc overrides** — sqlc for SQLite does not auto-generate enums from CHECK constraints. Custom Go enum types defined in `harness/internal/swarm/enums.go`, mapped to columns via `sqlc.yaml` `overrides`. Compile-time type safety + database-level CHECK validation.
- **Explicit user-invoked workflows** — no auto-classification. User explicitly calls `/swarm-research`, `/swarm-code "..."`, or `/swarm-project "..."`. OpenClaw can direct routing in a future iteration.
- **Full restart path** — `swarm_workflows.previous_workflow_id` enables creating new attempts that reference prior failed/rejected work as context.
- **State machine unit tests** — `statemachine_test.go` in Phase 1 with table-driven tests for every transition.
- **Graphite `gt` for PR stacking** — `pkgs.graphite-cli` added to `flake.nix` (available in nixpkgs v1.7.18). PRs created via `gt create`.
- **Schedule idempotency** — try-create with `ErrScheduleAlreadyRunning` fallback on harness restart.

## Current State Analysis

### What Exists
- **38 linear-cli skills** installed (`.agents/skills/linear-*/SKILL.md`) — full Linear CRUD, search, relations, projects, sprints
- **Workflow commands** working: `/create_plan`, `/implement_plan`, `/validate_plan`, `/research_codebase`, `/create_handoff`, `/resume_handoff`, `/describe_pr`, `/create_spec`, `/tkt`
- **Claude Code orchestrator** (`harness/internal/claude/`) — hook-driven tmux session management, `POST /api/claude-event` completion signal, orphaned session reaping
- **Tmux session management** (`harness/internal/tmux/`) — `Session` struct with `Create`, `SendPrompt`, `Kill`, `IsAlive`
- **EventBus** (`harness/internal/events/`) — `Subscribe(key)` takes any string (not just worldIDs), `Publish(key, event)` with non-blocking 100-buffer channels
- **President agent pattern** (`harness/internal/president/`) — secret-based auth middleware, API endpoints
- **Hook-based completion pattern** — on-stop hook POSTs to harness, handler dispatches via `event` field, side effects in goroutines
- **Skill directory pattern** proven by `playwright-cli` — YAML frontmatter, `references/` sub-files
- **Linear team** `CM` configured
- **SQLite + sqlc** (`harness/internal/db/`) — 5 existing migrations, hardcoded `migrationFiles` list in `db.go:93-99`, embedded via `//go:embed`
- **Nix flake** (`flake.nix`) — `temporal-cli` is available in nixpkgs as `pkgs.temporal-cli` (v1.5.1)

### What's Missing
- No swarm skills or primitives
- No Linear conventions for agent workflow (labels, comment format, ticket structure)
- No orchestration beyond president/mayors
- No swarm-specific DB tables or state tracking
- No Linear data mirrored locally
- No admin dashboard for swarm observability

## Desired End State

A fully operational agent swarm where:
1. **Research, code changes, and projects** are explicitly requested by the user — Linear is the source of truth
2. **Projects** decompose into tickets with dependency graphs, milestones, and parallelism analysis
3. **Code changes** follow: research → plan → review loop → implement → verify loop → PR → human review
4. **The heartbeat** drives everything: polls Linear, reads SQLite state, spawns child workflows, detects stalls, reaps dead sessions
5. **The dashboard** shows real-time swarm activity: workflows, phases, decisions, stuck indicators, capacity
6. **Human gates** are explicit and minimal: workflow type selection, project kickoff, PR merge
7. **Dry-run** works on all primitives for safe iteration
8. **Full restart** — failed/rejected work can be referenced by new attempts

### Verification
1. `/swarm-setup` creates all labels idempotently (second run = no-op)
2. `/swarm-research "how does auth work"` → creates ticket + research doc + Linear comments
3. `/swarm-code "add /version endpoint"` → implements from an approved plan
4. `/swarm-project "build notification system"` → project + ticket hierarchy + dependency graph
5. `/swarm-resume CM-XXX` → reads comment history, continues from last phase
6. `/swarm-status` → formatted dashboard with workflow states + capacity
7. `GET /api/swarm/health` → returns active sessions + capacity from SQLite
8. `POST /api/swarm/spawn` → starts SessionWorkflow via Temporal child workflow
9. `POST /api/swarm/session-complete` → hook-driven completion signal from Claude Code sessions
10. Dashboard at `/swarm` shows live event stream, active workflows, decision points

## What We're NOT Doing

- Building a custom workflow engine (Linear IS the workflow engine for tickets; SQLite + Temporal for orchestration)
- Long-running Temporal workflows (all workflows are short-lived; state machine in SQLite)
- LLM-based orchestration (heartbeat is deterministic Go code; LLM reasoning is only in Claude Code sessions)
- Auto-classifying ideas (user explicitly chooses research/code/project; OpenClaw routing is future work)
- Auto-merging PRs (human always reviews and merges)
- Slack integration (Discord-only; Slack acknowledged as future)
- Custom UI beyond the admin dashboard (Linear UI + CLI + dashboard is sufficient)
- Nested skill loading (flat skills only, composition via state machine)
- Webhooks for Linear sync v1 (polling via heartbeat; webhook endpoint stubbed for future)
- Custom PR tooling beyond Graphite (Graphite `gt` handles stacking natively)

---

## Naming Conventions

### Temporal Workflows

| Workflow | Duration | Purpose |
|----------|----------|---------|
| `SessionWorkflow` | Minutes–hours | Generic: run one Claude Code skill in tmux, wait for hook completion |
| `HeartbeatWorkflow` | Seconds | Scheduled: state machine driver, sync, reap, stall detect, spawns child SessionWorkflows |

### Temporal Activities

| Activity | Queue | Purpose |
|----------|-------|---------|
| `RunClaudeSession` | general/verify | Spawn tmux session with skill, wait for hook-based completion signal, record result |
| `SyncLinearState` | ops | Poll Linear API, upsert `swarm_tickets` + `swarm_ticket_comments` |
| `ReadTicketQueue` | ops | State machine: read SQLite, determine next phase — returns spawn decisions (does NOT spawn workflows) |
| `ReapSessions` | ops | Kill orphaned `cm-swarm-*` tmux sessions |
| `DetectStalls` | ops | Flag stuck workflows, post HEARTBEAT comments |
| `MarkTicketFailed` | ops | Terminal failure: update Linear status + SQLite + log event |

### Temporal Task Queues

| Queue | Concurrency | Purpose |
|-------|-------------|---------|
| `swarm-general` | 3 | Claude Code sessions (research, plan, implement, PR) |
| `swarm-verify` | 1 | Code verification — OOM prevention (only one `just check` at a time) |
| `swarm-ops` | 1 | Heartbeat maintenance activities |

### Workflow IDs & Tmux Sessions

- **Workflow ID**: `swarm-{agentIdx}-{ticketID}` (e.g., `swarm-0-CM-123`)
- **Tmux session**: `cm-swarm-{agentIdx}-{ticketID}` (e.g., `cm-swarm-0-CM-123`)
- **Agent index**: 0 through `max_sessions - 1`, assigned from first available slot

### API Routes

| Route | Method | Auth | Purpose |
|-------|--------|------|---------|
| `/api/swarm/health` | GET | `X-Swarm-Secret` | Status + capacity |
| `/api/swarm/spawn` | POST | `X-Swarm-Secret` | Create workflow + trigger first phase |
| `/api/swarm/workflow/:id` | GET | `X-Swarm-Secret` | Workflow detail |
| `/api/swarm/cancel/:id` | POST | `X-Swarm-Secret` | Cancel workflow + kill tmux session |
| `/api/swarm/session-complete` | POST | `X-Hook-Secret` | Hook-driven completion signal from Claude Code sessions |
| `/swarm` | GET | Admin session | Dashboard page |
| `/swarm/events` | GET | Admin session | SSE stream for dashboard |

### Go Package

`harness/internal/swarm/` with files:
- `workflows.go` — `SessionWorkflow`, `HeartbeatWorkflow`
- `activities.go` — all activity implementations
- `worker.go` — worker setup, queue registration
- `store.go` — sqlc-generated SQLite operations
- `config.go` — `SwarmConfig` struct, JSON marshal/unmarshal
- `linear.go` — Linear API sync logic
- `statemachine.go` — phase transition logic, next-phase determination, Go const enums
- `statemachine_test.go` — table-driven tests for every transition
- `completion.go` — hook-based session completion: channel registry, signal handler

---

## Database Schema

All tables use CHECK constraints for database-level validation. Go-side enum types are defined in `harness/internal/swarm/enums.go` and mapped to columns via `sqlc.yaml` `overrides`, giving compile-time type safety. sqlc for SQLite does not auto-generate enums from CHECK constraints, but the `overrides` mechanism achieves the same result.

### `swarm_config`

```sql
CREATE TABLE swarm_config (
    id     TEXT PRIMARY KEY DEFAULT 'default',
    config TEXT NOT NULL DEFAULT '{}'  -- JSON blob
);
```

```go
type SwarmConfig struct {
    MaxSessions      int `json:"max_sessions"`       // default 4
    HeartbeatSeconds int `json:"heartbeat_seconds"`   // default 120
    StallMinutes     int `json:"stall_minutes"`       // default 45
    MaxPlanRevisions int `json:"max_plan_revisions"`  // default 3
    MaxVerifyRetries int `json:"max_verify_retries"`  // default 3
    RetryBackoffSecs int `json:"retry_backoff_secs"`  // default 30
}
```

### `swarm_workflows`

```sql
CREATE TABLE swarm_workflows (
    id                   TEXT PRIMARY KEY,  -- matches Temporal workflow ID: swarm-{idx}-{ticket}
    ticket_id            TEXT NOT NULL,
    workflow_type        TEXT NOT NULL CHECK(workflow_type IN ('research', 'code', 'project')),
    phase                TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_plan', 'project_review', 'project_verify',
        'done', 'failed'
    )),
    status               TEXT NOT NULL CHECK(status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    agent_index          INTEGER NOT NULL,
    attempt              INTEGER NOT NULL DEFAULT 1,
    previous_workflow_id TEXT,              -- references prior attempt for "full restart" context
    branch_name          TEXT,              -- git branch for this workflow
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### `swarm_sessions`

```sql
CREATE TABLE swarm_sessions (
    id            TEXT PRIMARY KEY,
    workflow_id   TEXT NOT NULL REFERENCES swarm_workflows(id),
    session_name  TEXT NOT NULL,  -- tmux session name: cm-swarm-{idx}-{ticket}
    skill         TEXT NOT NULL,  -- skill invoked: swarm-research, swarm-code, etc.
    phase         TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_plan', 'project_review', 'project_verify'
    )),
    result        TEXT CHECK(result IN ('success', 'logic_failure', 'infra_failure', 'timeout')),
    detail        TEXT,           -- JSON: error message, verdict, exit code, etc.
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at  TEXT
);
```

### `swarm_events`

```sql
CREATE TABLE swarm_events (
    id          TEXT PRIMARY KEY,
    workflow_id TEXT REFERENCES swarm_workflows(id),
    session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id   TEXT NOT NULL,
    event_type  TEXT NOT NULL CHECK(event_type IN (
        'workflow_started', 'workflow_completed', 'workflow_failed', 'workflow_cancelled',
        'phase_started', 'phase_completed',
        'session_spawned', 'session_completed',
        'plan_review_verdict', 'verify_result',
        'milestone_passed', 'milestone_failed',
        'retry_triggered',
        'stall_detected', 'session_reaped',
        'ticket_synced',
        'terminal_failure'
    )),
    phase       TEXT,   -- which phase this event relates to
    detail      TEXT,   -- JSON: verdict, error, attempt number, summary, etc.
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### `swarm_project_milestones`

```sql
CREATE TABLE swarm_project_milestones (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    project_id   TEXT,          -- Linear project ID
    name         TEXT NOT NULL,
    criteria     TEXT NOT NULL,  -- JSON: array of verification checks
    status       TEXT NOT NULL CHECK(status IN ('pending', 'passed', 'failed')),
    verified_at  TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### `swarm_tickets`

```sql
CREATE TABLE swarm_tickets (
    id           TEXT PRIMARY KEY,  -- Linear issue UUID
    identifier   TEXT NOT NULL,     -- CM-123
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,     -- Linear workflow state name
    priority     INTEGER,
    assignee     TEXT,
    labels       TEXT,              -- JSON array of label names
    parent_id    TEXT,              -- parent issue ID
    project_id   TEXT,              -- Linear project ID
    url          TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    synced_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### `swarm_ticket_comments`

```sql
CREATE TABLE swarm_ticket_comments (
    id         TEXT PRIMARY KEY,  -- Linear comment UUID
    ticket_id  TEXT NOT NULL REFERENCES swarm_tickets(id),
    body       TEXT NOT NULL,
    author     TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    synced_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
```

---

## State Machine

The state machine lives in `statemachine.go`. Typed enum constants are defined in `enums.go` and mapped to SQLite columns via `sqlc.yaml` `overrides`. The `ReadTicketQueue` activity calls the state machine for each running workflow and returns spawn decisions to `HeartbeatWorkflow`. All transitions write to `swarm_workflows`, `swarm_events`, and publish to the EventBus.

### Go Enum Types (`harness/internal/swarm/enums.go`)

```go
package swarm

// Phase is a typed enum for workflow phases. Mapped to swarm_workflows.phase
// and swarm_sessions.phase via sqlc.yaml overrides.
type Phase string

const (
    PhaseResearch       Phase = "research"
    PhaseCodePlan       Phase = "code_plan"
    PhasePlanReview     Phase = "plan_review"
    PhaseImplement      Phase = "implement"
    PhaseVerify         Phase = "verify"
    PhasePR             Phase = "pr"
    PhaseProjectPlan    Phase = "project_plan"
    PhaseProjectReview  Phase = "project_review"
    PhaseProjectVerify  Phase = "project_verify"
    PhaseDone           Phase = "done"
    PhaseFailed         Phase = "failed"
)

func (p Phase) Valid() bool {
    switch p {
    case PhaseResearch, PhaseCodePlan, PhasePlanReview, PhaseImplement, PhaseVerify, PhasePR,
        PhaseProjectPlan, PhaseProjectReview, PhaseProjectVerify, PhaseDone, PhaseFailed:
        return true
    }
    return false
}

// SessionResult is a typed enum for session outcomes. Mapped to swarm_sessions.result
// via sqlc.yaml overrides.
type SessionResult string

const (
    ResultSuccess      SessionResult = "success"
    ResultLogicFailure SessionResult = "logic_failure"
    ResultInfraFailure SessionResult = "infra_failure"
    ResultTimeout      SessionResult = "timeout"
)

// WorkflowStatus is a typed enum for workflow status. Mapped to swarm_workflows.status
// via sqlc.yaml overrides.
type WorkflowStatus string

const (
    StatusPending   WorkflowStatus = "pending"
    StatusRunning   WorkflowStatus = "running"
    StatusCompleted WorkflowStatus = "completed"
    StatusFailed    WorkflowStatus = "failed"
    StatusCancelled WorkflowStatus = "cancelled"
)

// WorkflowType is a typed enum for workflow types. Mapped to swarm_workflows.workflow_type
// via sqlc.yaml overrides.
type WorkflowType string

const (
    WorkflowTypeResearch WorkflowType = "research"
    WorkflowTypeCode     WorkflowType = "code"
    WorkflowTypeProject  WorkflowType = "project"
)
```

### sqlc.yaml Overrides

Add to the existing `overrides` list in `harness/sqlc.yaml`:

```yaml
overrides:
  # ... existing overrides ...
  - column: "swarm_workflows.phase"
    go_type: "creative-mode/harness/internal/swarm.Phase"
  - column: "swarm_workflows.status"
    go_type: "creative-mode/harness/internal/swarm.WorkflowStatus"
  - column: "swarm_workflows.workflow_type"
    go_type: "creative-mode/harness/internal/swarm.WorkflowType"
  - column: "swarm_sessions.phase"
    go_type: "creative-mode/harness/internal/swarm.Phase"
  - column: "swarm_sessions.result"
    go_type: "creative-mode/harness/internal/swarm.SessionResult"
  - column: "swarm_events.event_type"
    go_type: "creative-mode/harness/internal/swarm.EventType"
  - column: "swarm_project_milestones.status"
    go_type: "creative-mode/harness/internal/swarm.MilestoneStatus"
```

This gives **compile-time type safety** — sqlc-generated query functions accept and return `swarm.Phase`, `swarm.WorkflowStatus`, etc. instead of plain `string`. Passing wrong values is a compile error.

### Phase Transitions

```
CURRENT PHASE    + RESULT/VERDICT           → NEXT PHASE
─────────────────────────────────────────────────────────
research         + success                  → code_plan
code_plan        + success                  → plan_review
plan_review      + approve                  → implement
plan_review      + revise (attempts < max)  → code_plan  (attempt++)
plan_review      + revise (attempts >= max) → failed
implement        + success                  → verify
verify           + success                  → pr
verify           + logic_failure (< max)    → implement  (attempt++)
verify           + logic_failure (>= max)   → failed
pr               + success                  → done
any              + infra_failure (< 2)      → same phase (retry)
any              + infra_failure (>= 2)     → failed
any              + timeout                  → failed
```

### Plan Revision Cycle

When `plan_review` returns `revise`:
1. The review doc is written to `thoughts/{user}/reviews/{timestamp}_{slug}_review.md`
2. The state machine sets phase back to `code_plan` with incremented attempt
3. The heartbeat spawns a new child `SessionWorkflow` with `/swarm-code-plan`
4. The `swarm-code-plan` skill reads the previous plan + review doc and produces a versioned plan (v2, v3...)
5. The plan doc path: `thoughts/{user}/plans/{timestamp}_{slug}.md` (v1), `{slug}_v2.md`, `{slug}_v3.md`

### Implement/Verify Loop

When `verify` returns `logic_failure`:
1. The state machine sets phase back to `implement` with incremented attempt
2. After `retry_backoff_secs` (default 30s), the heartbeat spawns a new child `SessionWorkflow` with `/swarm-code`
3. The `swarm-code` skill reads the plan + previous verification failure context
4. After implementation, the state machine automatically advances to `verify`

### Terminal Failure

When any phase exceeds max attempts or encounters an unrecoverable error:
1. `MarkTicketFailed` activity runs:
   - Updates `swarm_workflows` → `status=failed, phase=failed`
   - Posts `TERMINAL_FAILURE:` comment on Linear ticket with failure summary
   - Updates Linear ticket status (e.g., "Blocked" or adds `needs-human` label)
   - Logs `terminal_failure` event with full context
2. Dashboard highlights the failed workflow

### Full Restart Path

When a human reviews a PR and decides a full restart is needed:
1. Close/reject the existing PR
2. User invokes `/swarm-code "same goal" --previous CM-XXX` (or creates a new ticket via Linear UI)
3. The new workflow's `previous_workflow_id` references the old one
4. Skills can read the old workflow's artifacts (research doc, plan, code) as context for the new attempt
5. The old workflow remains in `done` or `failed` state (not modified)

### Project Workflow Transitions

```
CURRENT PHASE       + RESULT/VERDICT           → NEXT PHASE
──────────────────────────────────────────────────────────────
research            + success                  → project_plan
project_plan        + success                  → project_review
project_review      + approve                  → done (spawns child CodeWorkflows)
project_review      + revise (attempts < max)  → project_plan (attempt++)
project_review      + revise (attempts >= max) → failed
project_verify      + all milestones passed    → done
project_verify      + milestones failed        → (spawns remediation tickets)
```

Project verification is triggered by the heartbeat when child workflows complete milestones, not as a sequential phase in the project workflow itself.

---

## Session Completion Model (v4 — Hook-Based)

**Why not polling**: The v3 plan proposed polling `tmux has-session` in a loop. This creates a race condition — the tmux session disappears before the RESULT comment is written to Linear. The existing codebase (`harness/internal/claude/claude.go`) uses hook-driven completion where `.claude/hooks/on-stop.sh` POSTs to the harness *after* Claude Code finishes its output. v4 follows this proven pattern.

### Architecture

```
Claude Code session (tmux)
  └─ Finishes work, writes RESULT comment to Linear
  └─ on-stop.sh fires (Claude Code hook)
     └─ Reads CM_SWARM_SESSION_ID, CM_SWARM_WORKFLOW_ID from tmux env
     └─ POSTs to CM_HARNESS_URL/api/swarm/session-complete
        with {session_id, workflow_id, ticket_id}

Harness handler: handleSwarmSessionComplete
  └─ Reads RESULT comment from Linear (or sentinel file)
  └─ Updates swarm_sessions (result, detail, completed_at)
  └─ Publishes session_completed event to EventBus
  └─ Signals the waiting RunClaudeSession activity via completion channel
```

### Completion Channel Registry

```go
// completion.go — manages per-session completion signals

type CompletionRegistry struct {
    mu       sync.Mutex
    channels map[string]chan SessionResult // keyed by session ID
}

func (r *CompletionRegistry) Register(sessionID string) <-chan SessionResult {
    r.mu.Lock()
    defer r.mu.Unlock()
    ch := make(chan SessionResult, 1)
    r.channels[sessionID] = ch
    return ch
}

func (r *CompletionRegistry) Signal(sessionID string, result SessionResult) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if ch, ok := r.channels[sessionID]; ok {
        ch <- result
        delete(r.channels, sessionID)
    }
}

func (r *CompletionRegistry) Deregister(sessionID string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.channels, sessionID)
}
```

### RunClaudeSession Activity (v4)

```go
func (a *Activities) RunClaudeSession(ctx context.Context, params SessionParams) (SessionResult, error) {
    sessionID := generateID()
    sessionName := fmt.Sprintf("cm-%s", params.WorkflowID)

    // 1. Register completion channel BEFORE spawning
    completionCh := a.completionRegistry.Register(sessionID)
    defer a.completionRegistry.Deregister(sessionID)

    // 2. Create DB record
    a.store.CreateSession(ctx, sessionID, params.WorkflowID, sessionName, params.Skill, params.Phase)
    a.publishEvent(params, "session_spawned", params.Phase, nil)

    // 3. Create tmux session with swarm-specific env vars
    extraEnv := []string{
        fmt.Sprintf("CM_SWARM_SESSION_ID=%s", sessionID),
        fmt.Sprintf("CM_SWARM_WORKFLOW_ID=%s", params.WorkflowID),
        fmt.Sprintf("CM_SWARM_TICKET_ID=%s", params.TicketID),
    }
    createSwarmTmuxSession(sessionName, a.repoRoot, params.Skill, params.TicketID, extraEnv)

    // 4. Wait for hook-based completion signal OR timeout/cancellation
    //    Activity heartbeat keeps Temporal informed we're still alive
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case result := <-completionCh:
            // Hook fired — session complete
            a.store.CompleteSession(ctx, sessionID, result.Status, result.Detail)
            a.publishEvent(params, "session_completed", params.Phase, result)
            return result, nil

        case <-ticker.C:
            // Heartbeat to Temporal (keeps activity alive, enables recovery)
            activity.RecordHeartbeat(ctx, sessionName)

            // Safety check: if tmux session died without hook firing
            if !tmuxHasSession(sessionName) {
                // Session crashed — read RESULT comment as fallback
                result := a.parseResultComment(params.TicketID, params.Phase)
                if result.Status == "" {
                    result = SessionResult{Status: ResultInfraFailure, Detail: "session crashed without RESULT comment"}
                }
                a.store.CompleteSession(ctx, sessionID, result.Status, result.Detail)
                a.publishEvent(params, "session_completed", params.Phase, result)
                return result, nil
            }

        case <-ctx.Done():
            // Workflow cancelled or activity timed out
            killTmuxSession(sessionName)
            return SessionResult{Status: ResultTimeout}, ctx.Err()
        }
    }
}
```

### Swarm On-Stop Hook

Each swarm tmux session gets a hook that fires on Claude Code exit. The hook script is injected at session creation time (written to a temp dir, referenced in `.claude/hooks/`).

```bash
#!/usr/bin/env bash
# swarm-on-stop.sh — fires when Claude Code exits in a swarm session
set -euo pipefail

HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"
SESSION_ID="${CM_SWARM_SESSION_ID}"
WORKFLOW_ID="${CM_SWARM_WORKFLOW_ID}"
TICKET_ID="${CM_SWARM_TICKET_ID}"
HOOK_SECRET="${CM_HOOK_SECRET:-}"

PAYLOAD=$(jq -n \
  --arg sid "$SESSION_ID" \
  --arg wid "$WORKFLOW_ID" \
  --arg tid "$TICKET_ID" \
  '{session_id: $sid, workflow_id: $wid, ticket_id: $tid}')

HEADER=""
if [ -n "$HOOK_SECRET" ]; then
  HEADER="-H X-Hook-Secret: $HOOK_SECRET"
fi

# Retry up to 5 times with 2s backoff (same pattern as existing on-stop.sh)
for i in 1 2 3 4 5; do
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "$HARNESS_URL/api/swarm/session-complete" \
    -H "Content-Type: application/json" \
    $HEADER \
    -d "$PAYLOAD" 2>/dev/null || echo "000")
  if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
    exit 0
  fi
  sleep 2
done
```

### Handler

```go
// POST /api/swarm/session-complete — called by swarm on-stop hook
func (s *Server) handleSwarmSessionComplete(c echo.Context) error {
    var req struct {
        SessionID  string `json:"session_id"`
        WorkflowID string `json:"workflow_id"`
        TicketID   string `json:"ticket_id"`
    }
    if err := c.Bind(&req); err != nil {
        return c.NoContent(http.StatusBadRequest)
    }

    // Read RESULT comment from Linear to get the session outcome
    result := s.SwarmActivities.ParseResultComment(req.TicketID, "" /* latest */)
    if result.Status == "" {
        result = SessionResult{Status: ResultSuccess, Detail: "no RESULT comment found"}
    }

    // Signal the waiting RunClaudeSession activity
    s.CompletionRegistry.Signal(req.SessionID, result)

    // Publish to EventBus for dashboard
    if s.EventBus != nil {
        s.EventBus.Publish("swarm", map[string]any{
            "event":       "swarm.session_completed",
            "session_id":  req.SessionID,
            "workflow_id": req.WorkflowID,
            "ticket_id":   req.TicketID,
            "result":      result.Status,
        })
    }

    return c.NoContent(http.StatusOK)
}
```

---

## Skill Directory Structure

```
.claude/skills/
  swarm-conventions/
    SKILL.md                              # Shared reference (~150 lines)
    templates/
      ticket-description.md               # Ticket footer template
      research-doc.md                     # Research doc template
      plan-doc.md                         # Plan doc template
  swarm-setup/SKILL.md                    # One-time label setup (~60 lines)
  swarm-research/SKILL.md                 # Research primitive (~120 lines)
  swarm-code-plan/SKILL.md               # Code change plan (~120 lines)
  swarm-code/SKILL.md                    # Implementation only (~100 lines)
  swarm-code-verify/SKILL.md             # Code verification (~100 lines)
  swarm-code-pr/SKILL.md                 # Create PR (~100 lines)
  swarm-plan-review/SKILL.md             # Review code plan → review doc (~120 lines)
  swarm-project/SKILL.md                 # Project decomposition (~150 lines)
  swarm-project-plan/SKILL.md            # Project plan + dependency graph (~130 lines)
  swarm-project-review/SKILL.md          # Review project plan (~120 lines)
  swarm-project-verify/SKILL.md          # Milestone verification (~100 lines)
  swarm-status/SKILL.md                  # Status query (~80 lines)
  swarm-resume/SKILL.md                  # Resume from ticket history (~120 lines)
```

14 skill directories + 3 templates = **17 skill files**.

---

## Phase 1: Foundation (Conventions, Setup, Templates, Schema, State Machine Tests)

### Overview
Create the shared conventions, one-time setup, document templates, database schema, and state machine with unit tests. Establishes the dry-run convention, label taxonomy, all SQLite tables, and the tested state machine.

### Changes Required

#### 1. Database Migration
**File**: `harness/internal/db/migrations/006_swarm_tables.sql`

Create all 7 swarm tables:
- `swarm_config` — with default row inserted
- `swarm_workflows` — includes `previous_workflow_id` and `branch_name` columns
- `swarm_sessions`
- `swarm_events`
- `swarm_project_milestones`
- `swarm_tickets`
- `swarm_ticket_comments`

All CHECK constraints as specified in the Database Schema section above.

**File**: `harness/internal/db/db.go` — add `"migrations/006_swarm_tables.sql"` to the `migrationFiles` slice at line 93-99.

#### 2. sqlc Queries
**File**: `harness/internal/db/queries/swarm.sql`

Queries for all 7 tables:
- CRUD for `swarm_workflows` (get by ID, get by ticket, list running, update phase/status, get by previous_workflow_id)
- CRUD for `swarm_sessions` (create, complete with result, list by workflow)
- Insert + list for `swarm_events` (with filters by workflow, event_type, time range)
- CRUD for `swarm_project_milestones`
- Get/update for `swarm_config`
- Upsert for `swarm_tickets` and `swarm_ticket_comments`
- Dashboard queries: running workflows with latest session, recent events, capacity count

#### 3. Enum Types
**File**: `harness/internal/swarm/enums.go` (~80 lines)

Typed Go enum types for `Phase`, `SessionResult`, `WorkflowStatus`, `WorkflowType`, `EventType`, `MilestoneStatus` (as shown in State Machine section above). Each type has a `Valid()` method.

#### 4. sqlc.yaml Overrides
**File**: `harness/sqlc.yaml` — add overrides mapping swarm columns to typed enums (as shown in State Machine section above).

After adding overrides, run `sqlc generate` to regenerate query code with typed enums.

#### 5. State Machine
**File**: `harness/internal/swarm/statemachine.go` (~120 lines)

Contains:
- `DetermineNextPhase(wf, lastSession, config) (Phase, bool)` — the core state machine function (uses typed `Phase` enum)
- `SkillForPhase(phase Phase) string` — maps phase to skill name
- `firstPhaseForType(workflowType WorkflowType) Phase` — returns starting phase per workflow type

#### 6. State Machine Tests
**File**: `harness/internal/swarm/statemachine_test.go` (~200 lines)

Table-driven tests covering every row in the phase transition table:

```go
func TestDetermineNextPhase(t *testing.T) {
    config := DefaultConfig

    tests := []struct {
        name        string
        wf          SwarmWorkflow
        lastSession *SwarmSession
        wantPhase   string
        wantRetry   bool
    }{
        {name: "new code workflow starts at research", wf: SwarmWorkflow{WorkflowType: "code"}, lastSession: nil, wantPhase: PhaseResearch, wantRetry: false},
        {name: "research success → code_plan", wf: SwarmWorkflow{WorkflowType: "code"}, lastSession: &SwarmSession{Phase: PhaseResearch, Result: ResultSuccess}, wantPhase: PhaseCodePlan, wantRetry: false},
        {name: "code_plan success → plan_review", ...},
        {name: "plan_review approve → implement", ...},
        {name: "plan_review revise under max → code_plan retry", ...},
        {name: "plan_review revise at max → failed", ...},
        {name: "implement success → verify", ...},
        {name: "verify success → pr", ...},
        {name: "verify logic_failure under max → implement retry", ...},
        {name: "verify logic_failure at max → failed", ...},
        {name: "pr success → done", ...},
        {name: "infra_failure under 2 → same phase retry", ...},
        {name: "infra_failure at 2 → failed", ...},
        {name: "timeout → failed", ...},
        // Project workflow transitions
        {name: "new project workflow starts at research", ...},
        {name: "research success (project) → project_plan", ...},
        {name: "project_plan success → project_review", ...},
        {name: "project_review approve → done", ...},
        {name: "project_review revise → project_plan retry", ...},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            phase, retry := DetermineNextPhase(tt.wf, tt.lastSession, config)
            if phase != tt.wantPhase || retry != tt.wantRetry {
                t.Errorf("got (%q, %v), want (%q, %v)", phase, retry, tt.wantPhase, tt.wantRetry)
            }
        })
    }
}

func TestSkillForPhase(t *testing.T) {
    // Verify every phase maps to a non-empty skill
    phases := []string{PhaseResearch, PhaseCodePlan, PhasePlanReview, PhaseImplement, PhaseVerify, PhasePR, PhaseProjectPlan, PhaseProjectReview, PhaseProjectVerify}
    for _, p := range phases {
        if skill := SkillForPhase(p); skill == "" {
            t.Errorf("SkillForPhase(%q) returned empty string", p)
        }
    }
}
```

#### 7. Conventions Reference
**File**: `.claude/skills/swarm-conventions/SKILL.md` (~150 lines)

```yaml
---
name: swarm-conventions
description: Reference for swarm agent conventions — labels, ticket format, comment format, doc templates. Not an action primitive. Load when creating/updating swarm-tracked tickets.
allowed-tools: Bash, Read
---
```

Content:
- **Linear team**: `CM`
- **Labels** with colors: `swarm:research` (#3B82F6), `swarm:code` (#10B981), `swarm:verification` (#EAB308), `swarm:project` (#8B5CF6), `swarm:plan` (#F97316), `swarm:orchestration` (#EF4444), plus `type:bug`, `type:feature`, `type:refactor`, `type:prototype`
- **Ticket footer**: Structured YAML block — `swarm_type`, `parent_ticket`, `research_path`, `plan_path`, `pr_url`, `previous_attempt`, `dependencies`
- **Comment prefixes** (parseable by resume): `RESEARCH:`, `PLAN:`, `PLAN-REVIEW:`, `IMPL:`, `VERIFY:`, `PR:`, `REVISION:`, `RESTART:`, `HEARTBEAT:`, `RESUME:`, `TERMINAL_FAILURE:`, `RESULT:`
- **Result comment format**: `RESULT: {"status": "success|logic_failure|infra_failure|timeout", "phase": "...", "artifacts": [...]}`
- **Lifecycle states**: Triage → Backlog → Todo → In Progress → In Review → Done
- **Doc paths**: `thoughts/{git_user}/research/{timestamp}_{slug}.md`, `thoughts/{git_user}/plans/{timestamp}_{slug}.md`
- **Plan versioning**: `{slug}.md` (v1), `{slug}_v2.md`, `{slug}_v3.md`, with review docs at `{slug}_review.md`, `{slug}_v2_review.md`
- **Dry-run convention**: All primitives accept `--dry-run`. Print `[DRY-RUN]` prefix per action. linear-cli native `--dry-run`.
- **Rate limits**: 1500 req/hr. Batch sequentially (linear-cli handles 429 retry).
- **Error handling**: exit 3 → stop, `linear-cli config doctor`. exit 4 → wait 60s, retry once. Mid-execution → write RESULT comment on ticket, keep In Progress.

#### 8. Templates
**File**: `.claude/skills/swarm-conventions/templates/ticket-description.md` (~30 lines)
Structured footer template with swarm metadata fields.

**File**: `.claude/skills/swarm-conventions/templates/research-doc.md` (~25 lines)
YAML frontmatter: `linear_ticket`, `date`, `author`, `topic`. Sections: Summary, Key Findings, Open Questions, Files Referenced, Next Steps.

**File**: `.claude/skills/swarm-conventions/templates/plan-doc.md` (~30 lines)
YAML frontmatter: `linear_ticket`, `linear_project`, `date`, `author`, `version`. Sections: Goal, Success Criteria, Phases, File Inventory, Dependencies.

#### 9. Setup Primitive
**File**: `.claude/skills/swarm-setup/SKILL.md` (~60 lines)

```yaml
---
name: swarm-setup
description: One-time setup for agent swarm — creates Linear labels, verifies CLI auth. Run once before using other swarm primitives.
allowed-tools: Bash
---
```

Steps:
1. `linear-cli config doctor` — verify auth
2. Check-then-create for each label: `linear-cli l list --output json --compact` → check existence → `linear-cli l create "name" --color "#hex"` only if missing
3. `linear-cli st list -t CM --output json` — verify workflow states
4. Report summary; supports `--dry-run`

### Success Criteria

#### Automated:
- [ ] Migration runs without error
- [ ] `sqlc generate` succeeds
- [ ] All 7 tables exist with correct CHECK constraints
- [ ] Default config row exists in `swarm_config`
- [ ] `go test ./internal/swarm/...` passes — all state machine transitions verified
- [ ] `ls .claude/skills/swarm-conventions/SKILL.md` exists
- [ ] `ls .claude/skills/swarm-conventions/templates/` shows 3 files
- [ ] `ls .claude/skills/swarm-setup/SKILL.md` exists
- [ ] Migration file listed in `db.go:93-99` migrationFiles slice

#### Manual:
- [ ] `/swarm-setup` creates all labels in Linear (verify in Linear UI)
- [ ] `/swarm-setup` run twice = no errors (idempotent)
- [ ] `/swarm-setup --dry-run` prints labels without creating them

---

## Phase 2: Core Skills (Research, Code-Plan, Code, Code-Verify, Code-PR, Plan-Review)

### Overview
Create the six skills that form the code change lifecycle. Each is flat, self-contained, and writes structured RESULT comments for the state machine.

### Changes Required

#### 1. Research Primitive
**File**: `.claude/skills/swarm-research/SKILL.md` (~120 lines)

```yaml
---
name: swarm-research
description: Deep research on a topic or ticket with Linear tracking. Creates research doc in thoughts/ and updates ticket with structured comments.
allowed-tools: Bash, Read, Glob, Grep, Agent, WebSearch, WebFetch
---
```

Process:
1. Parse input: topic string or ticket ID. Optional `--dry-run`.
2. Get or create ticket: `linear-cli i create "[RESEARCH] <topic>" -t CM -l swarm:research --id-only`
3. Start work: `linear-cli i update $TICKET -s "In Progress"`, comment `RESEARCH: Starting`
4. Research: Agent subagents (codebase-analyzer, web-search-researcher) in parallel
5. Write doc: `thoughts/{user}/research/{timestamp}_{slug}.md` with `linear_ticket` frontmatter
6. Update Linear: comment `RESEARCH: <summary>` + `RESULT: {"status": "success", "phase": "research", "artifacts": ["<doc_path>"]}`
7. Output: doc path + ticket ID

#### 2. Code Change Plan
**File**: `.claude/skills/swarm-code-plan/SKILL.md` (~120 lines)

```yaml
---
name: swarm-code-plan
description: Create or revise an implementation plan for a code change ticket. Reads research docs and any previous plan + review for context. Outputs a versioned plan document.
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
---
```

Process:
1. Parse input: ticket ID. Check for previous plan version + review doc in Linear comments.
2. If first plan: use `/create_plan` with research context → write `{slug}.md`
3. If revision: read previous plan + review doc → produce `{slug}_v{N}.md` addressing review feedback
4. Comment `PLAN: Created at <path> (version {N})`
5. Comment `RESULT: {"status": "success", "phase": "code_plan", "artifacts": ["<plan_path>"], "version": N}`

#### 3. Plan Review
**File**: `.claude/skills/swarm-plan-review/SKILL.md` (~120 lines)

```yaml
---
name: swarm-plan-review
description: Review a code change plan for completeness, feasibility, and edge cases. Outputs a review document and a verdict (approve/revise). Agent-only — no human approval needed.
allowed-tools: Bash, Read, Glob, Grep, Agent
---
```

Process:
1. Parse input: ticket ID. Read latest plan from Linear comments.
2. Spawn Agent subagent to review plan: file existence, pattern consistency, edge cases, feasibility
3. Write review doc: `thoughts/{user}/reviews/{timestamp}_{slug}_v{N}_review.md`
4. Verdict: APPROVE or REVISE with specific issues
5. Comment `PLAN-REVIEW: {verdict}. Review at <path>`
6. Comment `RESULT: {"status": "success", "phase": "plan_review", "verdict": "approve|revise", "artifacts": ["<review_path>"]}`

#### 4. Implementation
**File**: `.claude/skills/swarm-code/SKILL.md` (~100 lines)

```yaml
---
name: swarm-code
description: Implement code changes based on an approved plan. Reads plan document, writes code, commits. Implementation only — yields to swarm-code-verify for verification.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent
---
```

Process:
1. Parse input: ticket ID. Optional `--previous CM-XXX` for full restart context. Read approved plan from Linear comments.
2. If retry after verification failure: read previous VERIFY comment for failure context.
3. If full restart (`--previous`): read old workflow's artifacts for additional context.
4. Implement changes following the plan.
5. Comment `IMPL: Changes applied. Files modified: <list>`
6. Comment `RESULT: {"status": "success", "phase": "implement", "artifacts": ["<list of files>"]}`

#### 5. Code Verification
**File**: `.claude/skills/swarm-code-verify/SKILL.md` (~100 lines)

```yaml
---
name: swarm-code-verify
description: Verify code changes compile and pass tests. Runs plan-defined checks and just check. Reports structured PASS/FAIL result.
allowed-tools: Bash, Read, Glob, Grep
---
```

Process:
1. Parse input: ticket ID. Read plan to extract verification criteria.
2. Run automated checks from plan (unit tests, integration tests, specific assertions).
3. Run `just check` (compilation, clippy, formatting).
4. Comment `VERIFY: {PASS|FAIL}. Details: <summary>`
5. Comment `RESULT: {"status": "success|logic_failure", "phase": "verify", "pass": true|false, "checks": [...]}`

Note: This skill runs on the `swarm-verify` queue (concurrency 1) to prevent OOM from concurrent `just check` invocations.

#### 6. Create PR
**File**: `.claude/skills/swarm-code-pr/SKILL.md` (~100 lines)

```yaml
---
name: swarm-code-pr
description: Create a pull request for verified code changes. Branches, commits, pushes, and creates a PR linked to the Linear ticket via Graphite stacking.
allowed-tools: Bash, Read, Glob, Grep, Agent
---
```

Process:
1. Parse input: ticket ID. Read plan + verification summary from Linear comments.
2. Create branch, stage changes, commit with descriptive message.
3. Push and create PR via Graphite: `gt create --title "..." --body "..."` (positions in stack automatically).
4. Link to Linear: include ticket ID in PR description.
5. Update Linear: `linear-cli i update $TICKET -s "In Review"`
6. Comment `PR: <url>`
7. Comment `RESULT: {"status": "success", "phase": "pr", "artifacts": ["<pr_url>"]}`

### Success Criteria

#### Automated:
- [ ] All 6 SKILL.md files exist under `.claude/skills/swarm-*/`
- [ ] Each has valid YAML frontmatter with `name`, `description`, `allowed-tools`
- [ ] No skill file references another skill's SKILL.md path

#### Manual:
- [ ] `/swarm-research "how does session auth work"` → ticket + doc + comments
- [ ] `/swarm-code-plan` with a ticket → plan doc + Linear comment
- [ ] `/swarm-plan-review` with a plan → review doc + verdict
- [ ] `/swarm-code` with an approved plan → code changes + Linear comment
- [ ] `/swarm-code-verify` → PASS/FAIL with structured result
- [ ] `/swarm-code-pr` → PR created and linked
- [ ] `--dry-run` prints actions without executing on all six

---

## Phase 3: Project & Support Skills (Project, Project-Plan, Project-Review, Project-Verify, Status, Resume)

### Overview
Create the project lifecycle skills and operational utilities. These complete the full skill set.

### Changes Required

#### 1. Project Decomposition
**File**: `.claude/skills/swarm-project/SKILL.md` (~150 lines)

```yaml
---
name: swarm-project
description: Decompose a high-level goal into a Linear project with tracked workstreams and research questions. Creates project + parent tickets.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent, WebSearch
---
```

Process:
1. Parse input: goal or project ID. Optional `--dry-run`.
2. Create Linear project: `linear-cli p create "<name>" -t CM --status planned --id-only`
3. Research: 2-5 research questions → child research tickets → Agent subagents (parallel)
4. Create parent tickets per workstream with scope descriptions.
5. Output: project ID, research ticket IDs, parent ticket IDs.

#### 2. Project Plan
**File**: `.claude/skills/swarm-project-plan/SKILL.md` (~130 lines)

```yaml
---
name: swarm-project-plan
description: Create a project-level plan with dependency graph, parallelism analysis, and milestone definitions. Reads research results from child tickets.
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
---
```

Process:
1. Parse input: project ID. Read all research docs from child tickets.
2. Decompose into 2-7 workstreams with scope, dependencies, complexity estimates.
3. Create child tickets per task. Set blocking relations (`linear-cli rel add $A -r blocks $B`).
4. Define milestones with verifiable criteria.
5. Write plan: `thoughts/{user}/plans/{timestamp}_{slug}.md` with:
   - Dependency graph (ASCII)
   - Parallelism analysis (what can run concurrently)
   - Milestone definitions with verification criteria
   - PR ordering
6. Comment on project tickets with plan reference.

#### 3. Project Review
**File**: `.claude/skills/swarm-project-review/SKILL.md` (~120 lines)

```yaml
---
name: swarm-project-review
description: Review a project plan for completeness — dependency graph, milestone definitions, parallelism analysis, scope coverage. Outputs review doc and verdict.
allowed-tools: Bash, Read, Glob, Grep, Agent
---
```

Process:
1. Read project plan from Linear comments.
2. Agent subagent reviews:
   - Dependency graph completeness (no orphan tickets, no circular deps)
   - Milestone criteria are verifiable (not vague)
   - Parallelism analysis is accurate
   - Scope fully covers the original goal
   - Workstream estimates are reasonable
3. Write review doc. Verdict: APPROVE or REVISE.
4. **Human gate (project kickoff)**: Present plan summary for approval.

#### 4. Project Verification
**File**: `.claude/skills/swarm-project-verify/SKILL.md` (~100 lines)

```yaml
---
name: swarm-project-verify
description: Verify project milestones by running defined verification criteria. Records results in swarm_project_milestones.
allowed-tools: Bash, Read, Glob, Grep
---
```

Process:
1. Parse input: project ID or milestone ID.
2. Read milestone criteria from `swarm_project_milestones`.
3. Run each verification check.
4. Record results: update milestone status to `passed` or `failed`.
5. If all milestones passed: project complete.
6. If milestones failed: identify remediation needed, create new tickets if necessary.

#### 5. Status Dashboard (CLI)
**File**: `.claude/skills/swarm-status/SKILL.md` (~80 lines)

```yaml
---
name: swarm-status
description: Display swarm status dashboard — active workflows, capacity, recent decisions, stuck indicators. Read-only, no side effects.
allowed-tools: Bash, Read
---
```

Process:
1. Query `/api/swarm/health` for capacity and active workflows.
2. Query Linear for ticket statuses.
3. Format dashboard: active projects table, in-progress tickets with phases, capacity gauge, recent events.

#### 6. Resume from Ticket History
**File**: `.claude/skills/swarm-resume/SKILL.md` (~120 lines)

```yaml
---
name: swarm-resume
description: Resume work on a ticket by reading its Linear comment history and swarm_sessions to reconstruct state. Picks up where the last session left off.
allowed-tools: Bash, Read, Glob, Grep, Write, Edit, Agent
---
```

Process:
1. `linear-cli i get $TICKET --output json` — ticket details
2. `linear-cli cm list $TICKET --output json` — full comment history
3. Parse RESULT comments to determine last completed phase and result
4. Read referenced docs (research, plan, review) from paths in comments
5. Comment `RESUME: Resuming from <phase>. Context: <summary>`
6. Continue from appropriate phase based on workflow type + last result

### Success Criteria

#### Automated:
- [ ] All 14 skill SKILL.md files exist under `.claude/skills/swarm-*/`
- [ ] Each has valid YAML frontmatter

#### Manual:
- [ ] `/swarm-project "improve error handling"` → project + parent tickets
- [ ] `/swarm-project-plan` → dependency graph + milestones
- [ ] `/swarm-project-review` → review doc + verdict
- [ ] `/swarm-project-verify` → milestone pass/fail
- [ ] `/swarm-status` → formatted dashboard
- [ ] `/swarm-resume CM-XXX` → correctly identifies last phase and continues

---

## Phase 4: Temporal + Completion Hooks + Dashboard

### Overview
Create the Temporal infrastructure, hook-based completion model, state machine driver, API endpoints, Linear sync, and admin dashboard. This phase brings the skills to life as an automated swarm.

### Infrastructure

#### Temporal Server
**Install via Nix**: Add `temporal-cli` and `graphite-cli` to `flake.nix` packages (both available in nixpkgs: `pkgs.temporal-cli` v1.5.1, `pkgs.graphite-cli` v1.7.18).

**File**: `flake.nix` — add `temporal-cli` and `graphite-cli` to the `paths` list in `packages.default` and `devShells.default`. Note: `graphite-cli` has `license = lib.licenses.unfree`, so add `config.allowUnfree = true` or a targeted predicate to the nixpkgs instantiation.

**Systemd service** — add step to `scripts/vps-bootstrap.sh`:
```ini
[Unit]
Description=Temporal Server (SQLite)
After=network.target
Before=creative-mode.service

[Service]
Type=simple
User=deploy
ExecStart=/home/deploy/.nix-profile/bin/temporal server start-dev \
    --db-filename /home/deploy/creative-mode/data/temporal.db \
    --port 7233 --ui-port 8233 --headless \
    --namespace swarm \
    --log-format json --log-level warn
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**Go SDK**: Add `go.temporal.io/sdk` to `harness/go.mod`.

### Changes Required

#### 1. Completion Channel Registry
**File**: `harness/internal/swarm/completion.go` (~60 lines)

The `CompletionRegistry` manages per-session completion signals (see Session Completion Model section above). Thread-safe map of session ID → buffered channel. Used by `RunClaudeSession` activity (registers + waits) and `handleSwarmSessionComplete` handler (signals).

#### 2. Workflows
**File**: `harness/internal/swarm/workflows.go` (~120 lines)

```go
// SessionWorkflow — generic, short-lived: run one Claude Code skill session, wait for hook completion
func SessionWorkflow(ctx workflow.Context, params SessionParams) (SessionResult, error) {
    // Determine queue based on phase
    queue := "swarm-general"
    if params.Phase == PhaseVerify {
        queue = "swarm-verify"
    }

    actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        TaskQueue:           queue,
        StartToCloseTimeout: 2 * time.Hour,
        HeartbeatTimeout:    2 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1}, // no auto-retry; state machine handles retries
    })

    var result SessionResult
    err := workflow.ExecuteActivity(actCtx, RunClaudeSession, params).Get(ctx, &result)
    return result, err
}

// HeartbeatWorkflow — scheduled every N seconds, short-lived
// Reads state, determines next actions, spawns fire-and-forget child SessionWorkflows
func HeartbeatWorkflow(ctx workflow.Context) error {
    logger := workflow.GetLogger(ctx)

    opsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        TaskQueue:           "swarm-ops",
        StartToCloseTimeout: 2 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
    })

    // 1. Sync Linear state (prerequisite for queue processing)
    if err := workflow.ExecuteActivity(opsCtx, SyncLinearState).Get(ctx, nil); err != nil {
        logger.Error("SyncLinearState failed", "error", err)
        // Continue — stale data is better than no processing
    }

    // 2. Read ticket queue — returns spawn decisions (does NOT spawn workflows)
    var spawnDecisions []SessionParams
    if err := workflow.ExecuteActivity(opsCtx, ReadTicketQueue).Get(ctx, &spawnDecisions); err != nil {
        logger.Error("ReadTicketQueue failed", "error", err)
        // Fall through to maintenance activities
        spawnDecisions = nil
    }

    // 3. Spawn child SessionWorkflows (fire-and-forget)
    for _, params := range spawnDecisions {
        cwo := workflow.ChildWorkflowOptions{
            WorkflowID:        params.WorkflowID,
            TaskQueue:         "swarm-general",
            ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON, // child outlives parent
        }
        childCtx := workflow.WithChildOptions(ctx, cwo)
        future := workflow.ExecuteChildWorkflow(childCtx, SessionWorkflow, params)

        // Wait only for child to START (not complete) — fire-and-forget
        var childWE workflow.Execution
        if err := future.GetChildWorkflowExecution().Get(ctx, &childWE); err != nil {
            logger.Error("Failed to start child workflow", "workflowID", params.WorkflowID, "error", err)
        }
    }

    // 4. Maintenance activities (independent, errors logged but not fatal)
    if err := workflow.ExecuteActivity(opsCtx, ReapSessions).Get(ctx, nil); err != nil {
        logger.Error("ReapSessions failed", "error", err)
    }
    if err := workflow.ExecuteActivity(opsCtx, DetectStalls).Get(ctx, nil); err != nil {
        logger.Error("DetectStalls failed", "error", err)
    }

    return nil
}
```

**SessionParams**:
```go
type SessionParams struct {
    WorkflowID string // swarm-{idx}-{ticket}
    TicketID   string // CM-123
    Skill      string // swarm-research, swarm-code, etc.
    Phase      string // research, code_plan, plan_review, etc.
    AgentIndex int
}
```

**SessionResult**:
```go
type SessionResult struct {
    Status  string // success, logic_failure, infra_failure, timeout
    Phase   string // phase that completed
    Detail  string // JSON: verdict, error, artifacts, etc.
}
```

#### 3. Activities
**File**: `harness/internal/swarm/activities.go` (~250 lines)

```go
type Activities struct {
    store              *SwarmStore   // sqlc-generated
    completionRegistry *CompletionRegistry
    eventBus           *events.EventBus
    repoRoot           string
    logger             *slog.Logger
}

// RunClaudeSession — spawn tmux, wait for hook-based completion signal
// See "Session Completion Model" section for full implementation

// SyncLinearState — poll Linear for swarm-labeled tickets
func (a *Activities) SyncLinearState(ctx context.Context) error {
    // 1. Query: linear-cli i list -t CM -l "swarm:research" --output json (repeat per label)
    // 2. For each issue: upsert into swarm_tickets
    // 3. For active workflows: linear-cli cm list $TICKET --output json
    // 4. Upsert into swarm_ticket_comments
    // 5. Detect changes, publish ticket_synced events
    ...
}

// ReadTicketQueue — the state machine reader (returns decisions, does NOT spawn workflows)
func (a *Activities) ReadTicketQueue(ctx context.Context) ([]SessionParams, error) {
    config := a.store.GetConfig(ctx)
    workflows := a.store.GetRunningWorkflows(ctx)
    activeCount := a.store.CountActiveSessions(ctx)

    var spawns []SessionParams
    for _, wf := range workflows {
        if a.hasActiveSession(ctx, wf.ID) {
            continue // session still running
        }

        // Determine next phase from state machine
        lastSession := a.store.GetLatestSession(ctx, wf.ID)
        nextPhase, shouldRetry := DetermineNextPhase(wf, lastSession, config)

        if nextPhase == PhaseFailed {
            a.markFailed(ctx, wf, lastSession)
            continue
        }
        if nextPhase == PhaseDone {
            a.markDone(ctx, wf)
            continue
        }

        // Check retry backoff
        if shouldRetry && lastSession != nil {
            elapsed := time.Since(lastSession.CompletedAt)
            if elapsed < time.Duration(config.RetryBackoffSecs)*time.Second {
                continue // wait for backoff
            }
        }

        // Check global capacity
        if activeCount+len(spawns) >= config.MaxSessions {
            continue // at capacity
        }

        // Build spawn decision
        agentIdx := a.findAvailableSlot(ctx, config.MaxSessions)
        workflowID := fmt.Sprintf("swarm-%d-%s", agentIdx, wf.TicketID)

        a.store.UpdateWorkflowPhase(ctx, wf.ID, nextPhase, wf.Attempt)
        a.publishEvent(wf, "phase_started", nextPhase, nil)

        spawns = append(spawns, SessionParams{
            WorkflowID: workflowID,
            TicketID:   wf.TicketID,
            Skill:      SkillForPhase(nextPhase),
            Phase:      nextPhase,
            AgentIndex: agentIdx,
        })
    }
    return spawns, nil
}

// ReapSessions — kill orphaned cm-swarm-* tmux sessions
func (a *Activities) ReapSessions(ctx context.Context) error { ... }

// DetectStalls — flag workflows stuck in same phase too long
func (a *Activities) DetectStalls(ctx context.Context) error { ... }

// MarkTicketFailed — terminal failure handling
func (a *Activities) MarkTicketFailed(ctx context.Context, workflowID string, reason string) error { ... }
```

#### 4. Worker Setup
**File**: `harness/internal/swarm/worker.go` (~80 lines)

```go
func SetupWorkers(tc client.Client, activities *Activities) (general, verify, ops worker.Worker) {
    general = worker.New(tc, "swarm-general", worker.Options{
        MaxConcurrentActivityExecutionSize: 3,
    })
    general.RegisterWorkflow(SessionWorkflow)
    general.RegisterActivity(activities)

    verify = worker.New(tc, "swarm-verify", worker.Options{
        MaxConcurrentActivityExecutionSize: 1,
    })
    verify.RegisterWorkflow(SessionWorkflow) // SessionWorkflow can dispatch to verify queue
    verify.RegisterActivity(activities)

    ops = worker.New(tc, "swarm-ops", worker.Options{
        MaxConcurrentActivityExecutionSize: 1,
    })
    ops.RegisterWorkflow(HeartbeatWorkflow)
    ops.RegisterActivity(activities)

    return
}
```

#### 5. Config
**File**: `harness/internal/swarm/config.go` (~60 lines)

```go
var DefaultConfig = SwarmConfig{
    MaxSessions:      4,
    HeartbeatSeconds: 120,
    StallMinutes:     45,
    MaxPlanRevisions: 3,
    MaxVerifyRetries: 3,
    RetryBackoffSecs: 30,
}
```

`MaxSessions` is the global cap enforced in `ReadTicketQueue`. The worker concurrency limits (3 general + 1 verify) are a safety net — the state machine should never exceed `MaxSessions` total.

#### 6. Linear Sync
**File**: `harness/internal/swarm/linear.go` (~120 lines)

```go
// SyncLinearState polls Linear for all swarm-labeled tickets and syncs to SQLite.
func (a *Activities) SyncLinearState(ctx context.Context) error {
    // Query per specific label (linear-cli does not support wildcard label matching):
    //   linear-cli i list -t CM -l "swarm:research" --output json
    //   linear-cli i list -t CM -l "swarm:code" --output json
    //   linear-cli i list -t CM -l "swarm:project" --output json
    //   ... etc
    // Merge results, upsert into swarm_tickets
    // For active workflows: linear-cli cm list $TICKET --output json
    // Upsert into swarm_ticket_comments
    // Detect changes, publish ticket_synced events
    ...
}
```

#### 7. API Endpoints
**File**: `harness/internal/server/swarm_api.go` (~250 lines)

Auth: `swarmAuthMiddleware` validates `X-Swarm-Secret` header (same pattern as `presidentAuthMiddleware`).

```go
// GET /api/swarm/health — status + capacity from SQLite
func (s *Server) handleSwarmHealth(c echo.Context) error { ... }

// POST /api/swarm/spawn — create workflow + trigger first phase via heartbeat
func (s *Server) handleSwarmSpawn(c echo.Context) error {
    // Parse ticket ID + workflow type from request
    // Check capacity
    // Create swarm_workflows row (with optional previous_workflow_id)
    // The heartbeat will pick this up and spawn the first SessionWorkflow
    // Return 202 with workflow ID
    ...
}

// GET /api/swarm/workflow/:id — workflow detail
func (s *Server) handleSwarmWorkflow(c echo.Context) error { ... }

// POST /api/swarm/cancel/:id — cancel workflow + kill tmux
func (s *Server) handleSwarmCancel(c echo.Context) error { ... }

// POST /api/swarm/session-complete — hook-driven completion signal
// (Protected by hookSecretMiddleware, same as /api/claude-event)
// See "Session Completion Model" section for full implementation
func (s *Server) handleSwarmSessionComplete(c echo.Context) error { ... }
```

#### 8. Dashboard
**File**: `harness/views/swarm/dashboard.templ` (~200 lines)

Admin-only page at `/swarm`. Sections:

| Section | Component | Update Trigger |
|---------|-----------|----------------|
| **Capacity Gauge** | X / max_sessions slots used | `session_spawned`, `session_completed` |
| **Active Workflows** | Table: ticket, type, phase, agent index, duration, attempt | `phase_started`, `workflow_completed` |
| **Live Event Stream** | Reverse-chronological event list | Any event |
| **Decision Points** | Highlighted: plan_review verdicts, verify results | `plan_review_verdict`, `verify_result` |
| **Stuck Indicators** | Workflows in same phase > stall_minutes | `stall_detected` |
| **Recent Completions** | Last 10 completed/failed workflows | `workflow_completed`, `workflow_failed` |

**File**: `harness/internal/server/swarm_dashboard.go` (~150 lines)

```go
// GET /swarm — admin dashboard page
func (s *Server) handleSwarmDashboard(c echo.Context) error { ... }

// GET /swarm/events — SSE stream
func (s *Server) handleSwarmSSE(c echo.Context) error {
    sse := datastar.NewSSE(c.Response().Writer, c.Request())
    ch := s.EventBus.Subscribe("swarm")
    defer s.EventBus.Unsubscribe("swarm", ch)
    heartbeat := time.NewTicker(30 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
        case event := <-ch:
            // Patch appropriate dashboard section based on event type
            ...
        case <-heartbeat.C:
            sse.MarshalAndPatchSignals(map[string]any{}) // Keepalive
        case <-c.Request().Context().Done():
            return nil
        }
    }
}
```

#### 9. Wiring
**File**: `harness/main.go` — additions:

```go
// Init Temporal client (guarded by SWARM_SECRET + TEMPORAL_ADDRESS env vars)
func initSwarm(ctx context.Context, database *db.DB, eventBus *events.EventBus, logger *slog.Logger) {
    swarmSecret := os.Getenv("SWARM_SECRET")
    if swarmSecret == "" {
        logger.Info("Swarm disabled (SWARM_SECRET not set)")
        return
    }

    temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
    if temporalAddr == "" {
        temporalAddr = "localhost:7233"
    }

    tc, err := client.Dial(client.Options{HostPort: temporalAddr, Namespace: "swarm"})
    // ... error handling ...

    store := swarm.NewStore(database)
    completionRegistry := swarm.NewCompletionRegistry()
    activities := swarm.NewActivities(store, completionRegistry, eventBus, repoRoot, logger)
    generalW, verifyW, opsW := swarm.SetupWorkers(tc, activities)

    generalW.Start()
    verifyW.Start()
    opsW.Start()

    // Create heartbeat schedule (idempotent — handles already-exists on restart)
    _, err = tc.ScheduleClient().Create(ctx, client.ScheduleOptions{
        ID: "swarm-heartbeat",
        Spec: client.ScheduleSpec{
            Intervals: []client.ScheduleIntervalSpec{{Every: 2 * time.Minute}},
        },
        Action: &client.ScheduleWorkflowAction{
            Workflow:  swarm.HeartbeatWorkflow,
            TaskQueue: "swarm-ops",
        },
    })
    if err != nil {
        if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
            logger.Info("Swarm heartbeat schedule already exists")
        } else {
            logger.Error("Failed to create heartbeat schedule", "error", err)
        }
    }

    // Graceful shutdown
    go func() {
        <-ctx.Done()
        generalW.Stop()
        verifyW.Stop()
        opsW.Stop()
        tc.Close()
    }()
}
```

**File**: `harness/internal/server/server.go` — add `SwarmStore`, `CompletionRegistry` fields. Register `/api/swarm` route group with `swarmAuthMiddleware`. Register `/api/swarm/session-complete` with `hookSecretMiddleware`. Register `/swarm` dashboard routes with admin middleware.

#### 10. Infrastructure Files
**File**: `flake.nix` — add `temporal-cli` to packages.
**File**: `scripts/vps-bootstrap.sh` — add step for Temporal systemd service + SQLite DB path.
**File**: `scripts/harness-run.sh` — add `TEMPORAL_ADDRESS` default export.

#### 11. Environment Variables

| Variable | Purpose |
|----------|---------|
| `SWARM_SECRET` | Auth for `/api/swarm/*` endpoints |
| `TEMPORAL_ADDRESS` | Temporal server address (default `localhost:7233`) |

### Success Criteria

#### Automated:
- [ ] `just check` passes (Go compilation with `go.temporal.io/sdk`)
- [ ] `go test ./internal/swarm/...` passes (state machine + completion registry tests)
- [ ] `ls harness/internal/swarm/` shows all 8 Go files (including `completion.go` and `statemachine_test.go`)
- [ ] `ls harness/internal/server/swarm_api.go` and `swarm_dashboard.go` exist
- [ ] `ls harness/views/swarm/dashboard.templ` exists
- [ ] `temporal-cli` available in Nix profile
- [ ] All migrations run, 7 swarm tables exist

#### Manual:
- [ ] Temporal server starts via systemd, web UI accessible on :8233
- [ ] Workers connect on harness boot (log message)
- [ ] HeartbeatWorkflow runs every 2 min (visible in Temporal UI)
- [ ] `POST /api/swarm/spawn` creates workflow; heartbeat picks it up and spawns child SessionWorkflow
- [ ] Session completion hook fires → `POST /api/swarm/session-complete` → activity unblocks
- [ ] Workflow ID format: `swarm-{agentIdx}-{ticketID}`
- [ ] 5th spawn (when max_sessions=4) returns 429
- [ ] Verification sessions queue (only 1 at a time on swarm-verify)
- [ ] Dashboard at `/swarm` shows live data
- [ ] State machine correctly advances phases
- [ ] Stalled workflows get flagged within 2 heartbeat ticks
- [ ] Terminal failures update Linear + SQLite + dashboard
- [ ] Schedule survives harness restart (idempotent create)
- [ ] Crashed session (no hook) detected by tmux health check fallback

---

## Phase 5: Integration Testing & CLAUDE.md

### Overview
End-to-end testing and documentation.

### Changes Required

#### 1. CLAUDE.md Update
**File**: `CLAUDE.md`

Add "## Agent Swarm System" section:
- Skills table (all 14)
- Explicit invocation: user calls `/swarm-research`, `/swarm-code`, or `/swarm-project` (no auto-classification)
- Human gates (project kickoff, PR merge)
- Full restart: `--previous CM-XXX` for referencing prior attempts
- Dry-run documentation
- API reference
- State machine transitions
- Dashboard access
- Config reference
- Future: OpenClaw routing for automatic workflow type selection

#### 2. Error Handling Documentation
Each skill includes standardized error handling:
- linear-cli exit code 3 → "Auth error. Run `linear-cli config doctor`"
- linear-cli exit code 4 → wait 60s, retry once
- Mid-execution failure → write RESULT comment on ticket, keep In Progress
- Crash recovery → `/swarm-resume` reads comment history + `swarm_sessions` DB
- Hook failure → tmux health check fallback detects crashed sessions within 30s

### Success Criteria

#### Automated:
- [ ] All 14 SKILL.md + 3 templates = 17 skill files exist
- [ ] CLAUDE.md contains "Agent Swarm System" section
- [ ] `just check` passes
- [ ] `go test ./internal/swarm/...` passes

#### Manual:
- [ ] `/swarm-setup --dry-run` prints labels without creating
- [ ] `/swarm-research "session auth"` → ticket + doc + comments
- [ ] `/swarm-code-plan` → plan doc linked to ticket
- [ ] `/swarm-plan-review` → review doc + verdict
- [ ] `/swarm-code` → implements changes from approved plan
- [ ] `/swarm-code-verify` → PASS/FAIL
- [ ] `/swarm-code-pr` → PR created and linked
- [ ] Full code lifecycle end-to-end via swarm spawn API
- [ ] `/swarm-resume CM-XXX` → reconstructs state and continues
- [ ] `/swarm-project "improve error handling"` → project + hierarchy
- [ ] HeartbeatWorkflow auto-advances phases (verify in dashboard + Temporal UI)
- [ ] Plan revision cycle: plan → review (revise) → plan v2 → review (approve)
- [ ] Implement/verify retry: implement → verify (fail) → implement → verify (pass)
- [ ] Terminal failure: max retries → ticket marked failed in Linear + dashboard
- [ ] Full restart: `/swarm-code "same goal" --previous CM-XXX` → new workflow with context from old

---

## Testing Strategy

### Automated Tests:
1. `statemachine_test.go` — table-driven tests for every phase transition (Phase 1)
2. `completion_test.go` — CompletionRegistry concurrent register/signal/deregister (Phase 4)

### Dry-Run Testing (no side effects):
1. `/swarm-setup --dry-run` → prints labels it would create
2. `/swarm-research "auth system" --dry-run` → prints ticket + doc actions
3. `/swarm-code-plan --dry-run` → prints plan creation actions
4. All 14 skills support `--dry-run`

### Live Skill Testing (one skill at a time):
1. `/swarm-setup` → verify labels in Linear UI
2. `/swarm-research "how does session auth work"` → ticket + doc + comments
3. `/swarm-code-plan` on a researched ticket → plan doc
4. `/swarm-plan-review` → review doc + verdict
5. `/swarm-code` on approved plan → code changes
6. `/swarm-code-verify` → PASS/FAIL result
7. `/swarm-code-pr` → PR created

### State Machine Testing:
1. `go test ./internal/swarm/...` — automated unit tests
2. Manually create `swarm_workflows` rows in various states
3. Run `ReadTicketQueue` and verify correct spawn decisions
4. Test retry backoff timing
5. Test terminal failure at max attempts

### Temporal + Hook Completion Testing:
1. `POST /api/swarm/spawn` → verify HeartbeatWorkflow spawns child SessionWorkflow
2. Session completes → on-stop hook fires → `POST /api/swarm/session-complete` → activity unblocks
3. Dashboard shows workflow in real-time
4. Spawn 4 workflows, attempt 5th → 429
5. Cancel a workflow → tmux cleaned up, dashboard updated
6. Kill harness mid-workflow → restart → heartbeat picks up where it left off
7. Session crashes without hook → tmux health check detects within 30s → infra_failure
8. Verify only 1 `swarm-code-verify` runs at a time (swarm-verify queue)

### End-to-End:
1. `POST /api/swarm/spawn` with a code ticket → full lifecycle through dashboard
2. Watch state machine advance: research → code_plan → plan_review → implement → verify → pr → done
3. Force a verification failure → observe retry in dashboard
4. Force a plan revision → observe v2 creation in dashboard
5. `/swarm-project "add rate limiting"` → project + child workflows → milestone verification
6. Stall a workflow → observe heartbeat detection in dashboard
7. Full restart test: reject PR → `/swarm-code "same goal" --previous CM-XXX` → new workflow references old

---

## Performance Considerations

- **linear-cli calls**: ~1500 req/hr budget. HeartbeatWorkflow sync: query per label (6 labels × ~1 call = ~6 calls per tick at 2-min interval = ~180 req/hr). Project decomposition (20 tickets + 15 relations + 20 comments = ~55 requests) well within limits.
- **WASM builds**: ~5 GB RAM each. `swarm-verify` queue (concurrency 1) ensures only one verification/build at a time — prevents OOM.
- **Claude Code sessions**: `swarm-general` (concurrency 3) + `swarm-verify` (concurrency 1) = max 4 concurrent, conservative for 10 GB VPS. `MaxSessions` config enforced in `ReadTicketQueue` as the authoritative global cap.
- **Temporal server**: ~200 MB RAM with SQLite backend at idle. Negligible CPU for short-lived workflows.
- **Resource budget**: Temporal (~200 MB) + harness + workers (~250 MB) + up to 3 Claude sessions (~1.5 GB) + 1 verification (~1-5 GB) = ~3-7 GB peak. Fits in 10 GB with headroom.
- **Dashboard SSE**: One EventBus channel ("swarm"), one SSE connection per admin viewer. 30s keepalive heartbeat. Negligible overhead.
- **SQLite writes**: All swarm tables are low-write (events at most every few seconds). No WAL contention concerns.
- **Zero LLM API cost** for orchestration. All Anthropic API spend goes to Claude Code worker sessions.
- **Completion hook latency**: Hook fires immediately after Claude Code exits. 5-retry with 2s backoff = max 10s delay on network issues. Tmux health check fallback at 30s interval catches crashed sessions.

---

## File Inventory

### New Files (29)

| File | Phase | ~Lines |
|------|-------|--------|
| `harness/internal/db/migrations/006_swarm_tables.sql` | 1 | 100 |
| `harness/internal/db/queries/swarm.sql` | 1 | 200 |
| `harness/internal/swarm/enums.go` | 1 | 80 |
| `harness/internal/swarm/statemachine.go` | 1 | 120 |
| `harness/internal/swarm/statemachine_test.go` | 1 | 200 |
| `.claude/skills/swarm-conventions/SKILL.md` | 1 | 150 |
| `.claude/skills/swarm-conventions/templates/ticket-description.md` | 1 | 30 |
| `.claude/skills/swarm-conventions/templates/research-doc.md` | 1 | 25 |
| `.claude/skills/swarm-conventions/templates/plan-doc.md` | 1 | 30 |
| `.claude/skills/swarm-setup/SKILL.md` | 1 | 60 |
| `.claude/skills/swarm-research/SKILL.md` | 2 | 120 |
| `.claude/skills/swarm-code-plan/SKILL.md` | 2 | 120 |
| `.claude/skills/swarm-code/SKILL.md` | 2 | 100 |
| `.claude/skills/swarm-code-verify/SKILL.md` | 2 | 100 |
| `.claude/skills/swarm-code-pr/SKILL.md` | 2 | 100 |
| `.claude/skills/swarm-plan-review/SKILL.md` | 2 | 120 |
| `.claude/skills/swarm-project/SKILL.md` | 3 | 150 |
| `.claude/skills/swarm-project-plan/SKILL.md` | 3 | 130 |
| `.claude/skills/swarm-project-review/SKILL.md` | 3 | 120 |
| `.claude/skills/swarm-project-verify/SKILL.md` | 3 | 100 |
| `.claude/skills/swarm-status/SKILL.md` | 3 | 80 |
| `.claude/skills/swarm-resume/SKILL.md` | 3 | 120 |
| `harness/internal/swarm/workflows.go` | 4 | 120 |
| `harness/internal/swarm/activities.go` | 4 | 250 |
| `harness/internal/swarm/worker.go` | 4 | 80 |
| `harness/internal/swarm/config.go` | 4 | 60 |
| `harness/internal/swarm/linear.go` | 4 | 120 |
| `harness/internal/swarm/completion.go` | 4 | 60 |
| `harness/internal/server/swarm_api.go` | 4 | 250 |
| `harness/internal/server/swarm_dashboard.go` | 4 | 150 |
| `harness/views/swarm/dashboard.templ` | 4 | 200 |

### Modified Files (9)

| File | Phase | Change |
|------|-------|--------|
| `harness/internal/db/db.go` | 1 | Add `006_swarm_tables.sql` to `migrationFiles` slice |
| `harness/sqlc.yaml` | 1 | Add `overrides` mapping swarm columns to typed Go enum types |
| `harness/internal/db/queries/` | 1 | sqlc query file for swarm tables |
| `CLAUDE.md` | 5 | Add Agent Swarm System section |
| `harness/main.go` | 4 | Wire Temporal client + workers + heartbeat schedule + completion registry |
| `harness/internal/server/server.go` | 4 | Add SwarmStore, CompletionRegistry fields, routes |
| `harness/go.mod` | 4 | Add `go.temporal.io/sdk` |
| `flake.nix` | 4 | Add `temporal-cli` + `graphite-cli` to packages |
| `scripts/vps-bootstrap.sh` | 4 | Add Temporal systemd service step |

---

## Decision History

| Version | Key Change | Why |
|---------|-----------|-----|
| v1 | OpenClaw heartbeat for Lead FDE | Original Chestnut flowchart design |
| v2 | Temporal workflows, long-running CodeChangeWorkflow | Need queue management, durable state, observability |
| v3 | Short-lived workflows + SQLite state machine | No long-running workflows; state queryable in SQLite; dashboard-driven observability; separate skills per operation |
| v4 | Hook-based completion, child workflows, typed enums via sqlc overrides, Graphite `gt`, explicit user invocation | Fix polling race condition (use proven hook pattern); fix Temporal anti-pattern (child workflows instead of activity-spawned); compile-time type safety via sqlc overrides + custom Go enum types; Graphite CLI for PR stacking (in nixpkgs); simplify by removing auto-classification (user explicitly chooses workflow type; OpenClaw routing is future) |

### Intentional Chestnut Flowchart Divergence

The Chestnut flowchart describes a two-level orchestration model: OpenClaw as human interface → Lead FDE heartbeat (7️⃣) → Project Orchestrators (6️⃣). The v4 plan intentionally simplifies this to a single `HeartbeatWorkflow` + deterministic Go state machine:

- **What's kept**: The full task lifecycle (research → plan → review → implement → verify → PR), plan revision loops, implement/verify loops, full restart path, project decomposition, milestones.
- **What's simplified**: Two-level orchestration (Lead FDE + Project Orchestrators) → single heartbeat. LLM-based routing → deterministic state machine. OpenClaw integration → explicit user invocation.
- **Why**: Zero LLM cost for orchestration. Deterministic behavior is easier to test and debug. The state machine handles 95% of cases. The remaining 5% (ambiguous situations requiring LLM reasoning) are better addressed in a future OpenClaw integration layer that sits *above* the current architecture.
- **Future path**: OpenClaw agent receives ideas via Discord, uses Linear skills to classify and invoke `/swarm-research`, `/swarm-code`, or `/swarm-project`. The swarm system doesn't need to change — OpenClaw just becomes the human interface layer that the flowchart envisions.

---

## References

- v3 plan: `thoughts/CoreyCole/plans/2026-02-28_17-30-00_agent-swarm-primitives-v3.md`
- v3 review: `thoughts/CoreyCole/reviews/2026-02-28_18-42-59_agent-swarm-primitives-v3_review.md`
- v2 handoff: `thoughts/CoreyCole/handoffs/general/2026-02-28_16-53-51_agent-swarm-primitives-v2-temporal-update.md`
- v1 review: `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md`
- Chestnut flowchart: `~/Downloads/chestnut-agent-primitives-flowchart.html`
- Existing hook pattern: `harness/internal/claude/claude.go`, `harness/internal/server/server.go:688-711`
- Temporal Go SDK: `go.temporal.io/sdk` — child workflows, schedules, activity heartbeats
- `temporal-cli` in nixpkgs: `pkgs.temporal-cli` v1.5.1
