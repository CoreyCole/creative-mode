# Agent Swarm Phase 4 Completion — Implementation Plan

## Overview

Complete the remaining v5 Phase 4 deliverables: Temporal workflow engine, hook system, metrics/health/alerts observability, learning digest loop, structured logging, per-session JSONL logs, dashboard enhancements, and president skill integration. Builds on the existing goroutine-based orchestrator (Phases 1-3) and dashboard (Phase 4A — already shipped).

## Current State Analysis

**Already built (Phases 1-3 + dashboard):**
- DB schema: 9 tables (workflows, sessions, events, milestones, tickets, ticket_comments, learnings, learning_digests, config)
- State machine: `DetermineNextPhase` with full phase transitions (`harness/internal/swarm/statemachine.go`)
- Orchestrator: `Manager` with tmux spawning, 15s poll-based `watchSession`, result file parsing, workflow advancement (`harness/internal/swarmorch/manager.go`)
- Learning capture: 4 capture functions + `GetLearningContext` (`harness/internal/swarm/learnings.go`)
- Handoffs: `ResolveHandoffPath` + formatting (`harness/internal/swarm/handoffs.go`)
- Result parsing: `ParseResultFile` (`harness/internal/swarm/result.go`)
- 6 Claude Code skills: swarm-research, swarm-code-plan, swarm-plan-review, swarm-code, swarm-code-verify, swarm-code-pr
- API: start/status/cancel endpoints (`harness/internal/server/swarm_api.go`)
- Dashboard: page + SSE + detail + cancel (`harness/internal/server/swarm_dashboard.go`, `harness/views/swarm/dashboard.templ`)
- 51 passing tests

**VPS specs:** ARM64 Linux, 31GB RAM, Nix. Temporal NOT installed.

### Key Discoveries:
- Manager already passes env vars: `CM_SWARM_TICKET_ID`, `WORKFLOW_ID`, `SESSION_ID`, `PHASE`, `ATTEMPT`, `RESULT_PATH`, `HANDOFF_PATH`, `LEARNING_CONTEXT_PATH`, `HARNESS_URL`, `HOOK_SECRET`, `BRANCH`
- Sessions named `cm-swarm-{ticketID}-{phase}`, spawned via `tmux new-session`
- Skills invoked via `tmux send-keys` with `claude --dangerously-skip-permissions --input-file /tmp/swarm-prompt-{sessionID}.txt`
- `watchSession` goroutine polls tmux every 15s — hook system will replace this with event-driven completion
- `captureLearnings` already called from `handleSessionComplete` — routes to the 4 capture functions based on result + phase
- Enum names: `swarm.StatusComplete` (not Completed), `swarm.MilestoneStatusPassed`/`MilestoneStatusFailed`

## Desired End State

After this plan is complete:
1. Temporal orchestrates all swarm workflows with durable execution, automatic retries, and UI visibility
2. Hook system provides real-time session lifecycle events (start, tool use, compaction, stop, crash recovery)
3. Metrics endpoint returns completion rates, phase durations, retry rates, token costs
4. Health endpoint shows system capacity, active workflows, alerts
5. Discord alerts fire for terminal failures, stalls, and crash recovery
6. Daily learning digests auto-generate with pattern detection and action items
7. Dashboard shows learnings, metrics cards, and live tool activity
8. Per-session JSONL logs enable historical inspection
9. All swarm log lines include correlation IDs (ticket, workflow, session, phase)

**Verification:** `just check` passes, all existing tests pass, `GET /api/swarm/health` returns healthy status, Temporal UI at `127.0.0.1:8233` shows workflow history.

## What We're NOT Doing

- Project workflow skills (swarm-project-plan, swarm-project-review, swarm-project-verify) — deferred to Phase 5
- Temporal Cloud migration — using `temporal server start-dev` for v1
- Custom Temporal TLS/auth — localhost-only, accessed via SSH tunnel/Tailscale
- Dashboard handoff chain visualization (complex UI) — deferred; show handoff paths in session detail instead
- Skill improvement PR flow via `ContributeSkillImprovement` — deferred to Phase 5 (requires president integration testing)

## Implementation Approach

Break into 7 sub-phases (4A-4G), each independently deployable and testable. The existing goroutine-based manager continues functioning through 4A-4F. Phase 4G introduces Temporal behind a feature flag (`CM_SWARM_TEMPORAL=true`), preserving the goroutine path as fallback.

**Sequencing:** 4A → 4B → 4C → 4D → 4E → 4F → 4G

---

## Phase 4A: Structured Logging + Per-Session JSONL

### Overview
Add correlation IDs to all swarm log calls and per-session JSONL log files. Foundation for all subsequent observability.

### Changes Required:

#### 1. Session logger helper
**File**: `harness/internal/swarmorch/sessionlog.go` (NEW)

Create a `SessionLog` type that wraps `*slog.Logger` with swarm correlation fields:
```go
type SessionLog struct {
    logger *slog.Logger
}

func NewSessionLog(base *slog.Logger, ticketID, workflowID, sessionID string, phase swarm.Phase) *SessionLog
func (l *SessionLog) Info(msg string, args ...any)
func (l *SessionLog) Warn(msg string, args ...any)
func (l *SessionLog) Error(msg string, args ...any)
```

Every log call auto-includes `subsystem=swarm`, `ticket_id`, `workflow_id`, `session_id`, `phase`.

#### 2. JSONL log writer
**File**: `harness/internal/swarmorch/jsonllog.go` (NEW)

Per-session append-only JSONL log at `data/swarm/logs/{ticketID}/{sessionID}.jsonl`:
```go
type JSONLWriter struct {
    file *os.File
}

func NewJSONLWriter(logsDir, ticketID, sessionID string) (*JSONLWriter, error)
func (w *JSONLWriter) Write(event map[string]any) error
func (w *JSONLWriter) Close() error
```

Each line: `{"ts":"...","event":"...","session":"...","ticket":"...","detail":{...}}`

#### 3. Wire into manager
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

- Replace bare `m.logger.Info/Error/Warn` calls in swarm code paths with `SessionLog`
- Create JSONL writer in `spawnSession`, pass to `watchSession`, close on completion
- Add `CM_SWARM_LOG_DIR` env var to tmux session

#### 4. Session log API endpoint
**File**: `harness/internal/server/swarm_api.go` (MODIFY)

Add `GET /api/swarm/session/:id/log` — reads and returns JSONL file for a session. Requires looking up session → ticketID from DB.

**File**: `harness/internal/server/server.go` (MODIFY)

Register the new route under `hookSecretMiddleware` group.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `go test ./internal/swarm/... ./internal/swarmorch/...` — all 51 tests pass
- [ ] New `sessionlog_test.go` verifies correlation fields in log output
- [ ] `GET /api/swarm/session/{id}/log` returns 200 with JSONL content (or 404 if no log)

#### Manual Verification:
- [ ] Start a workflow, verify `journalctl -u creative-mode | grep swarm` shows ticket_id + workflow_id + session_id
- [ ] After session completes, `data/swarm/logs/{ticketID}/{sessionID}.jsonl` exists with entries

---

## Phase 4B: Hook System + Completion Registry

### Overview
Replace tmux polling with event-driven completion via Claude Code hooks. Adds real-time session lifecycle events.

### Changes Required:

#### 1. Completion and Start registries
**File**: `harness/internal/swarmorch/registry.go` (NEW)

```go
type CompletionRegistry struct {
    mu       sync.RWMutex
    channels map[string]chan SessionResult
}

func (r *CompletionRegistry) Register(sessionID string) chan SessionResult
func (r *CompletionRegistry) Signal(sessionID string, result SessionResult) bool
func (r *CompletionRegistry) Unregister(sessionID string)

type StartRegistry struct {
    mu       sync.RWMutex
    channels map[string]chan struct{}
}

func (r *StartRegistry) Register(sessionID string) chan struct{}
func (r *StartRegistry) Signal(sessionID string) bool
func (r *StartRegistry) Unregister(sessionID string)
```

#### 2. Hook config writer
**File**: `harness/internal/swarmorch/hooks.go` (NEW)

`WriteHooksConfig(sessionID, ticketID, harnessURL, hookSecret) (hooksDir string, err error)`:
- Creates `/tmp/swarm-hooks-{sessionID}/hooks.json` with 6 hooks:
  - SessionStart → HTTP `POST /api/swarm/hook/session-started`
  - PreToolUse (Bash) → HTTP `POST /api/swarm/hook/pre-tool-use`
  - PostToolUse → HTTP `POST /api/swarm/hook/post-tool-use`
  - PreCompact → HTTP `POST /api/swarm/hook/pre-compact`
  - Stop → Command hook (`on-stop.sh` — captures tmux token count, POSTs to `/api/swarm/session-complete`)
  - SessionEnd → HTTP `POST /api/swarm/hook/session-ended`
- Creates `/tmp/swarm-hooks-{sessionID}/on-stop.sh` with token capture script

PreToolUse deny patterns:
```go
var swarmDenyPatterns = []*regexp.Regexp{
    regexp.MustCompile(`cargo\s+(build|clippy|check)`),
    regexp.MustCompile(`go\s+build`),
    regexp.MustCompile(`templ\s+generate`),
    regexp.MustCompile(`just\s+generate`),
}
```

#### 3. Context pressure tracker
**File**: `harness/internal/swarmorch/hooks.go` (same file)

In-memory tracker for compact events per session:
```go
type ContextPressure struct {
    mu     sync.RWMutex
    counts map[string]int // sessionID → compact count
}

func (cp *ContextPressure) Increment(sessionID string) int
func (cp *ContextPressure) Get(sessionID string) int
func (cp *ContextPressure) Remove(sessionID string)
```

Second PreCompact sets `context_pressure=true` via a sentinel file at `/tmp/swarm-context-pressure-{sessionID}` (simpler than API — skills just check file existence).

#### 4. Hook endpoint handlers
**File**: `harness/internal/server/swarm_hooks.go` (NEW)

6 handlers:
- `handleSwarmHookSessionStarted` — signals StartRegistry
- `handleSwarmHookPreToolUse` — checks deny list, returns `{permissionDecision: "deny"}` if matched
- `handleSwarmHookPostToolUse` — publishes tool event to EventBus, writes to JSONL log
- `handleSwarmHookPreCompact` — increments context pressure, writes sentinel file on 2nd
- `handleSwarmSessionComplete` — parses result, signals CompletionRegistry
- `handleSwarmHookSessionEnded` — crash backup: if no Stop fired, signals CompletionRegistry with infra_failure

All authenticated via `hookSecretMiddleware`.

#### 5. Wire hooks into manager
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

- Add `completionReg *CompletionRegistry`, `startReg *StartRegistry`, `ctxPressure *ContextPressure` to Manager
- In `spawnSession`: call `WriteHooksConfig`, pass `CLAUDE_HOOKS_DIR` env var to tmux
- In `watchSession`: replace 15s tmux polling with:
  1. Wait for StartRegistry signal (30s timeout → infra_failure)
  2. Block on CompletionRegistry signal (with 30s tmux health check fallback)
  3. On completion: parse result, call `handleSessionComplete`
- Keep tmux health check as fallback (if hooks fail, detect dead session and read result file)

#### 6. New event types
**File**: `harness/internal/events/types.go` (MODIFY)

Add:
```go
EventSwarmToolUse         = "swarm.tool_use"
EventSwarmContextPressure = "swarm.context_pressure"
```

#### 7. Route registration
**File**: `harness/internal/server/server.go` (MODIFY)

Register 6 hook endpoints under the `swarmGroup` (hookSecretMiddleware).

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `registry_test.go` — concurrent register/signal/unregister, timeout behavior
- [ ] `hooks_test.go` — WriteHooksConfig generates valid JSON, deny patterns match correctly, context pressure state transitions
- [ ] All 51 existing tests pass

#### Manual Verification:
- [ ] Start a workflow, verify SessionStart hook fires within 5s
- [ ] Verify PostToolUse events appear in JSONL log
- [ ] Verify denied commands are blocked (check JSONL for deny events)
- [ ] Kill a tmux session without Stop — verify SessionEnd triggers infra_failure recovery

---

## Phase 4C: Metrics + Health Endpoint

### Overview
Aggregate observability — answer "is the swarm effective?" with data.

### Changes Required:

#### 1. Metrics aggregation
**File**: `harness/internal/swarmorch/metrics.go` (NEW)

SQL aggregation queries with 60s in-memory cache:
```go
type SwarmMetrics struct {
    Period    string
    Workflows WorkflowMetrics  // total, completed, failed, in_progress, completion_rate
    Phases    map[string]PhaseMetrics  // avg_duration_min, count, revise_rate/retry_rate
    Retries   RetryMetrics  // plan_revisions, verify_retries, infra_retries
    Learnings LearningMetrics  // total, by_category
    Cost      CostMetrics  // total_session_minutes, total_tokens, tokens_by_phase
}

func (m *Manager) GetMetrics(ctx context.Context, period string) (*SwarmMetrics, error)
```

Period parsing: `24h`, `7d`, `30d`, `all` → SQL `datetime('now', '-24 hours')` etc.

#### 2. Health endpoint
**File**: `harness/internal/swarmorch/health.go` (NEW)

```go
type SwarmHealth struct {
    Status           string  // healthy, degraded, unhealthy
    Capacity         CapacityInfo
    ActiveWorkflows  []ActiveWorkflowInfo
    RecentCompletions []CompletionInfo
    Alerts           []AlertInfo
    MetricsSummary   MetricsSummary
}

func (m *Manager) GetHealth(ctx context.Context) (*SwarmHealth, error)
```

Status logic:
- `healthy` — no workflows stalled, active sessions < max
- `degraded` — stalls detected OR capacity at max
- `unhealthy` — terminal failures with no workflows progressing

#### 3. API endpoints
**File**: `harness/internal/server/swarm_api.go` (MODIFY)

- `GET /api/swarm/metrics?period=24h` — returns SwarmMetrics JSON
- `GET /api/swarm/health` — returns SwarmHealth JSON
- `GET /api/swarm/session/:id/status` — returns session status + context_pressure flag

Register under both `hookSecretMiddleware` (for skills) AND `approved` group (for dashboard).

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `metrics_test.go` — aggregation queries return correct counts with test data
- [ ] `GET /api/swarm/health` returns valid JSON with status field
- [ ] `GET /api/swarm/metrics?period=all` returns valid JSON

#### Manual Verification:
- [ ] After running workflows, metrics show correct completion rates and token counts
- [ ] Health endpoint reflects actual active sessions and capacity

---

## Phase 4D: Alerts + Learning Digest Loop

### Overview
Close the learning feedback loop with daily digests and operational alerting via Discord.

### Changes Required:

#### 1. Discord alerts
**File**: `harness/internal/swarmorch/alerts.go` (NEW)

```go
type AlertManager struct {
    discord    *worldchannel.Client
    channelID  string
    dedup      map[string]time.Time  // alertKey → lastFired (1hr dedup)
    mu         sync.Mutex
}

func (a *AlertManager) FireTerminalFailure(ticketID, reason, docPath string)
func (a *AlertManager) FireCrashRecovery(ticketID string, phase swarm.Phase)
func (a *AlertManager) FireStallDetected(ticketID string, phase swarm.Phase, minutes int)
func (a *AlertManager) FireHighRetryRate(rate float64)
```

All fire-and-forget via goroutine. Posts to `DISCORD_PRESIDENT_CHANNEL_ID`. Dedup: same alert type + workflow_id skipped within 1-hour window.

Wire into manager: `markFailed` calls `FireTerminalFailure`, `DetectStalls` calls `FireStallDetected`, SessionEnd-without-Stop calls `FireCrashRecovery`.

#### 2. Learning relevance decay
**File**: `harness/internal/swarm/learnings.go` (MODIFY)

Add `DecayLearningRelevance(ctx, db)`:
- Skip if <1 hour since last run (track in swarm_config or in-memory)
- Formula: `newScore = min((1.0/(1.0+ageDays/30.0) * severityFactor) + referenceBoost, 1.0)`
- Auto-archive learnings >60 days old with relevance < 0.1

#### 3. Digest generation
**File**: `harness/internal/swarm/digest.go` (NEW)

```go
func GenerateDigest(ctx context.Context, db *db.DB) error
```

1. Query learnings since last digest
2. Group by category (post_mortem > code_bug > plan_issue > convention > pattern)
3. Deterministic pattern detection:
   - Same tag in ≥2 code bugs → "Update swarm-code-verify SKILL.md"
   - ≥2 plan issues → "Update swarm-code-plan SKILL.md"
   - Any post-mortems → "Review SwarmConfig changes"
4. Write to `thoughts/swarm/digests/{date}_digest.md`
5. Store in `swarm_learning_digests` table

#### 4. Heartbeat integration
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

Add periodic maintenance to the manager (runs alongside session watching):
- Every 2 minutes: `DetectStalls`, `ReapSessions`
- Every hour: `DecayLearningRelevance`
- Every 24 hours: `GenerateDigest`

Use a simple ticker-based approach (Temporal will replace this in 4G).

#### 5. Learning API endpoints
**File**: `harness/internal/server/swarm_api.go` (MODIFY)

- `GET /api/swarm/learnings?category=&phase=&since=&top=&search=` — filtered query
- `POST /api/swarm/learnings` — create learning from skill session
- `GET /api/swarm/learnings/digest/latest` — latest digest with action items

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `digest_test.go` — pattern detection identifies repeated bugs, generates correct action items
- [ ] `DecayLearningRelevance` test — old learnings decay, critical learnings decay slower, referenced get boosted
- [ ] Learning API: POST creates record, GET returns filtered results

#### Manual Verification:
- [ ] Trigger a terminal failure → Discord alert fires in president channel within 30s
- [ ] After 24h (or manual trigger), digest generated at `thoughts/swarm/digests/`
- [ ] Alert dedup: same failure doesn't spam Discord within 1 hour

---

## Phase 4E: Dashboard Enhancements

### Overview
Surface metrics, learnings, and live tool activity in the existing dashboard.

### Changes Required:

#### 1. Extend dashboard data
**File**: `harness/views/swarm/dashboard.templ` (MODIFY)

Add to `DashboardData`:
```go
type DashboardData struct {
    Workflows []sqlc.ListAllSwarmWorkflowsRow
    Events    []sqlc.SwarmEvent
    Metrics   *swarmorch.SwarmMetrics  // NEW
    Health    *swarmorch.SwarmHealth   // NEW
}
```

New components:
- `MetricsCards(metrics)` — completion rate, avg duration, retry rate, token cost
- `HealthStatus(health)` — capacity bar, status badge, active alerts
- `LearningsSection(learnings)` — recent learnings with category/severity badges

Add "Metrics" tab and "Learnings" tab to the existing tab bar.

#### 2. Live tool activity in SSE
**File**: `harness/internal/server/swarm_dashboard.go` (MODIFY)

In SSE handler, also handle `EventSwarmToolUse` events:
- Append tool use entry to a live activity feed component
- Show file being edited, command being run, etc.

#### 3. Workflow detail enhancements
**File**: `harness/views/swarm/dashboard.templ` (MODIFY)

In `WorkflowPage`:
- Show handoff path for each session (from DB or result file)
- Show context_pressure indicator on active sessions
- Show token count per session (from `swarm_sessions.total_tokens`)

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] Templ generates without errors
- [ ] Dashboard page handler fetches metrics + health data

#### Manual Verification:
- [ ] Navigate to `/swarm` — metrics cards show real data
- [ ] Health status reflects actual system state
- [ ] Tool activity appears live during active sessions
- [ ] Learnings tab shows recent learnings with badges

---

## Phase 4F: President Skill Integration

### Overview
Give the president agent access to swarm learnings and health data.

### Changes Required:

#### 1. Add swarm-learnings skill
**File**: `harness/internal/president/skills.go` (MODIFY)

Add `"swarm-learnings"` to the skills map. The SKILL.md provides curl commands:
```
GET /api/swarm/learnings?category=critical&limit=10
GET /api/swarm/health
GET /api/swarm/metrics?period=24h
GET /api/swarm/learnings/digest/latest
```

Auth: uses `CM_HOOK_SECRET` header (same as other swarm API calls).

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] President skill directory contains SKILL.md with correct curl commands

#### Manual Verification:
- [ ] President agent can query learnings API and receive results

---

## Phase 4G: Temporal Integration

### Overview
Replace goroutine-based orchestration with Temporal for durable execution, automatic retries, and workflow visibility. Feature-flagged behind `CM_SWARM_TEMPORAL=true`.

### Changes Required:

#### 1. Install Temporal on VPS
**File**: `scripts/setup-temporal.sh` (NEW)

- Install Temporal CLI via Nix or direct binary
- Create systemd service at `/etc/systemd/system/temporal.service`:
  ```ini
  [Service]
  ExecStart=temporal server start-dev \
      --db-filename /home/deploy/creative-mode/data/temporal.db \
      --ip 127.0.0.1 \
      --ui-ip 127.0.0.1 \
      --ui-port 8233
  ```
- Enable and start service
- Create `swarm` namespace

#### 2. Temporal client package
**File**: `harness/internal/swarmorch/temporal.go` (NEW)

Temporal client initialization + worker setup:
```go
func NewTemporalClient(addr string) (client.Client, error)
func StartWorkers(c client.Client, mgr *Manager) error
```

Three task queues:
- `swarm-general` (concurrency 3) — research, plan, implement, PR
- `swarm-verify` (concurrency 1) — plan_review, verify (exclusive repo access)
- `swarm-ops` (concurrency 1) — heartbeat maintenance

#### 3. Workflow definitions
**File**: `harness/internal/swarmorch/workflows.go` (NEW)

`HeartbeatWorkflow` — runs every 2 min via ContinueAsNew:
```go
func HeartbeatWorkflow(ctx workflow.Context) error {
    // Execute maintenance activities
    // ContinueAsNew after each iteration
}
```

`SessionWorkflow` — wraps one Claude Code session:
```go
func SessionWorkflow(ctx workflow.Context, params SessionParams) (swarm.SessionResult, error) {
    // Execute RunClaudeSession activity with 60min timeout, 2min heartbeat
}
```

#### 4. Activity definitions
**File**: `harness/internal/swarmorch/activities.go` (NEW)

Wrap existing manager methods as Temporal activities:
- `RunClaudeSession` — spawn tmux, wait for hook completion, return result
- `ReadTicketQueue` — state machine: determine next phases, spawn child SessionWorkflows
- `DetectStalls` — flag stuck workflows
- `ReapSessions` — kill orphaned tmux sessions
- `DecayLearnings` — relevance decay
- `GenerateDigest` — daily digest

#### 5. Feature flag in manager
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

- Add `temporalClient client.Client` field
- `StartWorkflow`: if `CM_SWARM_TEMPORAL=true`, call `temporalClient.ExecuteWorkflow` instead of direct `spawnSession`
- `RecoverWorkflows`: no-op when Temporal enabled (Temporal handles durability)
- Heartbeat tickers: no-op when Temporal enabled (HeartbeatWorkflow handles it)

#### 6. Wire into main.go
**File**: `harness/main.go` (MODIFY)

Conditionally initialize Temporal client and start workers based on `CM_SWARM_TEMPORAL` env var.

#### 7. Go module dependency
**File**: `harness/go.mod` (MODIFY)

Add `go.temporal.io/sdk`.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] With `CM_SWARM_TEMPORAL=false` (default): system behaves exactly as before
- [ ] All 51 existing tests pass (they test the goroutine path)
- [ ] Temporal-specific unit tests with mock environment pass
- [ ] `temporal server` process running: `systemctl status temporal`

#### Manual Verification:
- [ ] With `CM_SWARM_TEMPORAL=true`: start a workflow → appears in Temporal UI at `127.0.0.1:8233`
- [ ] Workflow progresses through phases visible in Temporal history
- [ ] Kill harness mid-session → restart → Temporal resumes workflow
- [ ] Session completes via hooks, not tmux polling

---

## Testing Strategy

### Unit Tests:
- `registry_test.go` — CompletionRegistry + StartRegistry concurrent behavior
- `hooks_test.go` — WriteHooksConfig JSON generation, deny pattern matching, context pressure
- `metrics_test.go` — aggregation queries with seeded test data
- `digest_test.go` — pattern detection, action item generation
- `sessionlog_test.go` — correlation field inclusion

### Integration Tests:
- Hook lifecycle: SessionStart → PostToolUse → PreCompact → Stop → SessionEnd ordering
- Learning capture → digest generation → action items
- Metrics accuracy after workflow completion

### Manual Testing Steps:
1. Start workflow via API, watch dashboard SSE updates
2. Verify hook events in JSONL log
3. Check metrics after workflow completes
4. Trigger terminal failure, verify Discord alert
5. Verify Temporal UI shows workflow history (4G only)

## Performance Considerations

- Metrics cache (60s TTL) prevents repeated aggregation on dashboard refreshes
- Relevance decay runs at most once per hour
- Digest generation runs at most once per 24 hours
- JSONL writes are append-only, no locking needed
- Discord alerts are fire-and-forget goroutines with 1hr dedup

## References

- v5 plan: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- Context passing plan: `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md`
- Existing orchestrator: `harness/internal/swarmorch/manager.go`
- Existing state machine: `harness/internal/swarm/statemachine.go`
- Existing learnings: `harness/internal/swarm/learnings.go`
- Dashboard: `harness/internal/server/swarm_dashboard.go`, `harness/views/swarm/dashboard.templ`
- Event types: `harness/internal/events/types.go`
- Phase 4 dashboard handoff: `thoughts/CoreyCole/handoffs/general/2026-03-01_01-07-27_swarm-dashboard-phase4.md`
