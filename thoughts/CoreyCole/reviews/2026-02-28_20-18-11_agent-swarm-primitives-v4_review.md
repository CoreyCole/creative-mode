---
date: 2026-02-28T20:18:11-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 633082c8a0e87df2f9acada8a258bce46d52cbf3
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md
status: complete
type: plan_review
---

# Plan Review: Agent Swarm Primitives v4

### Summary

The v4 plan represents a mature architectural evolution — the decision history from v1 through v4 shows clear learning, and the core choices (short-lived Temporal workflows, SQLite state machine, hook-based completion, zero-LLM orchestration) are sound. However, three critical issues must be resolved before implementation: the existing tmux reaper will kill swarm sessions, two enum types are missing from the plan, and Temporal workflow ID collisions will occur on retry/restart.

### Critical Issues (Must Address Before Implementation)

1. **Existing tmux reaper will kill swarm sessions**
   - Problem: `ReapOrphanedSessions` in `harness/internal/claude/claude.go:293-337` scans all `cm-*` tmux sessions (excluding only `cm-server-*` and `cm-trunk-*`), extracts a `cpID` by splitting on hyphens (`parts := strings.SplitN(line, "-", 3)`), and kills any session whose `cpID` doesn't exist in the checkpoints table. A swarm session named `cm-swarm-0-CM-123` would split into `["cm", "swarm", "0-CM-123"]`, yielding `cpID = "0-CM-123"`. This doesn't exist in the checkpoints table, so the reaper kills it. The reaper runs every 5 minutes (`main.go:224`).
   - Risk: Every swarm session will be killed within 5 minutes of creation, before it can complete any work.
   - Suggestion: Add `strings.HasPrefix(line, "cm-swarm-")` to the skip list at `claude.go:312-315`. This is a one-line change but absolutely required. Include it in Phase 4 changes or even Phase 1 as a prerequisite.

2. **Missing `EventType` and `MilestoneStatus` enum types**
   - Problem: The `sqlc.yaml` overrides section (plan lines 370-373) maps `swarm_events.event_type` to `swarm.EventType` and `swarm_project_milestones.status` to `swarm.MilestoneStatus`. But the `enums.go` section (plan lines 289-351) only defines `Phase`, `SessionResult`, `WorkflowStatus`, and `WorkflowType`. Neither `EventType` nor `MilestoneStatus` are defined.
   - Risk: `sqlc generate` will produce code referencing types that don't exist → compile error. This blocks Phase 1 completion since sqlc overrides are part of Phase 1.
   - Suggestion: Add these two enum types to `enums.go`:
     ```go
     type EventType string
     const (
         EventWorkflowStarted    EventType = "workflow_started"
         EventWorkflowCompleted  EventType = "workflow_completed"
         // ... all 16 event types from the CHECK constraint
     )

     type MilestoneStatus string
     const (
         MilestoneStatusPending MilestoneStatus = "pending"
         MilestoneStatusPassed  MilestoneStatus = "passed"
         MilestoneStatusFailed  MilestoneStatus = "failed"
     )
     ```
     Both need `Valid()` methods consistent with the other enum types.

3. **Temporal Workflow ID collision on retry/restart**
   - Problem: The workflow ID format is `swarm-{agentIdx}-{ticketID}` (e.g., `swarm-0-CM-123`). When `ReadTicketQueue` processes a workflow for the same ticket after a failure, it calls `a.findAvailableSlot()` which may return the same agent index. `workflow.ExecuteChildWorkflow` with the same workflow ID will fail because Temporal retains completed/failed workflow IDs for a retention period (default 24h in dev server). The plan's `previous_workflow_id` mechanism creates a new SQLite row, but the Temporal workflow ID generation doesn't incorporate the attempt number.
   - Risk: Retry after failure silently fails — Temporal rejects the duplicate workflow ID, the error is logged, but the workflow is stuck forever because the heartbeat keeps trying to spawn a child with the same ID.
   - Suggestion: Append the attempt number to the Temporal workflow ID: `swarm-{agentIdx}-{ticketID}-{attempt}` (e.g., `swarm-0-CM-123-2`). This makes each attempt unique. Update the tmux session naming to match. Alternatively, use a UUID suffix, but the attempt number is more debuggable.

### Concerns (Should Address)

1. **sqlc overrides format inconsistency**
   - Observation: The plan uses the plain string format `go_type: "creative-mode/harness/internal/swarm.Phase"` for overrides. The existing `sqlc.yaml` (lines 56-62) uses the structured format `go_type: {import: "time", type: "Time"}`. The plain string format is noted as potentially deprecated in sqlc docs.
   - Suggestion: Use the structured format for consistency and forward-compatibility:
     ```yaml
     - column: "swarm_workflows.phase"
       go_type:
         import: "creative-mode/harness/internal/swarm"
         type: "Phase"
     ```

2. **`SetMaxOpenConns(1)` with concurrent Temporal activities**
   - Observation: The database is initialized with `sqlDB.SetMaxOpenConns(1)` at `db.go:33`. The plan introduces up to 5 concurrent activities (`swarm-general` concurrency 3, `swarm-verify` concurrency 1, `swarm-ops` concurrency 1) that all write to SQLite. With a single connection and WAL mode, writers will serialize on the `busy_timeout=5000` mutex. This isn't a crash risk, but activities could block for up to 5 seconds waiting for the DB lock.
   - Suggestion: This is fine for v1 — SQLite with WAL and busy_timeout handles concurrent writers correctly, just serially. Note it as a known limitation. If activity latency becomes an issue, consider bumping `SetMaxOpenConns` to 2-3 (WAL mode supports concurrent readers with one writer).

3. **CompletionRegistry is process-local — harness restart gap**
   - Observation: The `CompletionRegistry` uses in-memory Go channels. If the harness process restarts while a swarm session is running: (a) the session completes, (b) the on-stop hook fires and retries `POST /api/swarm/session-complete` up to 5 times over 10 seconds, (c) if the harness isn't back up within 10s, the hook gives up, (d) the tmux health check fallback catches it at the next 30s tick. But there's a subtle issue: the `RunClaudeSession` activity is running inside a Temporal worker. If the harness restarts, the Temporal worker also restarts, and the activity will be retried by Temporal. The retried activity creates a new completion channel — but the session already ran, the tmux session is dead, so the tmux health check immediately fires and reads the RESULT comment. This is actually fine.
   - Suggestion: Document this recovery path explicitly in the code comments. The sequence is: harness restart → Temporal retries `RunClaudeSession` → new channel registered → tmux session is dead → 30s health check fires → reads RESULT comment → returns result. It works, but only because the tmux session name is deterministic and the RESULT comment persists in Linear.

4. **No bootstrap step for migration 006 in `bootstrapExistingMigrations`**
   - Observation: The existing `db.go:135-186` has a `bootstrapExistingMigrations` method that handles the case where migrations ran before tracking was introduced (pre-migration-table schema). When adding migration 006, you should add a corresponding bootstrap check for the case where swarm tables somehow exist without tracking (e.g., if someone manually ran the SQL).
   - Suggestion: Add a bootstrap check for `swarm_config` table existence, similar to the existing checks for `worlds`, `template_type` column, etc. This is defensive and follows the established pattern.

5. **Graphite CLI version discrepancy**
   - Observation: The plan claims `pkgs.graphite-cli` v1.7.18. Research confirms the package exists in nixpkgs but the current version is **v1.7.2**, not v1.7.18.
   - Suggestion: Correct the version number in the plan. This doesn't affect functionality — `gt create` is available in v1.7.2 — but the plan should be accurate.

6. **No transaction isolation in `ReadTicketQueue`**
   - Observation: `ReadTicketQueue` reads `GetRunningWorkflows`, `CountActiveSessions`, `GetLatestSession`, then writes `UpdateWorkflowPhase` — all as separate queries without a transaction. If two heartbeats somehow overlap (e.g., one takes >2 minutes due to a slow Linear API call in `SyncLinearState`), both could read the same workflow state and spawn duplicate sessions.
   - Suggestion: Wrap `ReadTicketQueue` in a transaction (`BEGIN IMMEDIATE`). SQLite's `IMMEDIATE` mode acquires a write lock at `BEGIN`, preventing concurrent writers. This is a simple defensive measure that matches the single-writer SQLite model.

7. **`SessionResult` struct vs `SessionResult` enum name collision**
   - Observation: The plan defines `SessionResult` as both a typed string enum (in `enums.go`, line 319) AND as a struct (in the `RunClaudeSession` activity, lines 1273-1278: `type SessionResult struct { Status string; Phase string; Detail string }`). These have the same name but different types. Go will not allow two types with the same name in the same package.
   - Suggestion: Rename the struct to `SessionOutcome` or `CompletionResult`, keeping `SessionResult` for the enum (since it maps to the `swarm_sessions.result` column via sqlc overrides).

8. **`SessionParams.Phase` and `SessionResult.Status` use `string` not typed enums**
   - Observation: The `SessionParams` struct (plan line 1263) has `Phase string` and the `SessionResult` struct (plan line 1274) has `Status string`. These should use the typed `Phase` and `SessionResult` (or `WorkflowStatus`) enums instead of bare strings — otherwise the compile-time safety from sqlc overrides is undermined by the Temporal activity interface using plain strings.
   - Suggestion: Change `SessionParams.Phase` to `Phase` type and the completion result status to use the `SessionResult` enum type. This ensures type safety flows through the entire system, not just the DB layer.

### Questions (Need Clarification)

1. The `handleSwarmSessionComplete` handler (plan lines 612-643) reads the RESULT comment from Linear via `ParseResultComment`. But what if the Claude Code session wrote the RESULT comment *after* the on-stop hook fired? The hook fires when Claude Code exits, which may be before all post-exit cleanup (like writing the RESULT comment to Linear via `linear-cli`) completes. Is there a timing guarantee that the RESULT comment exists when the hook fires?

2. The `ReadTicketQueue` activity generates `workflowID := fmt.Sprintf("swarm-%d-%s", agentIdx, wf.TicketID)` and passes it to the child `SessionWorkflow`. But this `workflowID` is different from `wf.ID` (the SQLite workflow row ID). Which one is the Temporal workflow ID? Are they the same? The plan seems to conflate the SQLite `swarm_workflows.id` with the Temporal workflow ID — if they're meant to be the same, the spawn endpoint must generate the ID in the same format.

3. How does the `swarm-ops` worker handle the case where `HeartbeatWorkflow` spawns child `SessionWorkflow`s on the `swarm-general` queue, but the `swarm-general` worker must also register `SessionWorkflow`? The plan's `SetupWorkers` (lines 1376-1397) shows `SessionWorkflow` registered on both `general` and `verify` workers, but the `HeartbeatWorkflow` runs on `ops`. When the heartbeat calls `workflow.ExecuteChildWorkflow(childCtx, SessionWorkflow, params)` with `TaskQueue: "swarm-general"` in the child options, does Temporal route the child to the correct queue even though the parent runs on a different queue?

4. The plan says skills fire `POST /api/swarm/session-complete` via on-stop hook. But the swarm on-stop hook (plan lines 572-606) is described as "injected at session creation time (written to a temp dir, referenced in `.claude/hooks/`)". How exactly is this hook injected? The existing tmux `Session.Create` method doesn't set up hook files — it only passes env vars. Does the swarm need to write a hook file to the session's working directory before launching Claude Code? If so, how does this interact with the existing `templates/*/.claude/hooks/on-stop.sh`?

### Suggestions (Nice to Have)

1. **Add a `swarm_workflows.temporal_workflow_id` column** — Separate the SQLite row ID from the Temporal workflow ID. This eliminates confusion about ID formats and makes retry/restart cleaner. The SQLite ID can be a UUID, while the Temporal ID follows the `swarm-{idx}-{ticket}-{attempt}` format.

2. **Add `completion_test.go` in Phase 4** — The plan mentions testing the `CompletionRegistry` but doesn't list it in the file inventory. A simple test for concurrent register/signal/deregister would catch channel leak bugs.

3. **Consider a dead letter mechanism for hook failures** — If the on-stop hook fails all 5 retries (harness down >10s), the completion signal is permanently lost. The tmux fallback catches this at 30s, but only if the Temporal activity is still running. If both the harness AND the activity crashed, the session is orphaned. A dead letter file (e.g., `.swarm-completion-{sessionID}.json` written by the hook as a fallback) would allow recovery on harness restart.

4. **Add `swarm-` to the tmux session prefix constants** — Define `swarmSessionPrefix = "cm-swarm-"` alongside the existing `claudeSessionParts` constant in `claude.go` for clarity and grep-ability.

### What's Good

- **Decisive architectural evolution with clear learning curve**: v1 (OpenClaw heartbeat) → v2 (Temporal long-running) → v3 (short-lived + SQLite) → v4 (hook-based + child workflows). Each iteration fixes real problems found in the previous review.
- **Zero LLM orchestration cost**: All scheduling/routing is deterministic Go code. This is a significant cost advantage and makes the system predictable and testable.
- **Hook-based completion model is proven**: The v4 plan correctly identifies the existing hook pattern (`on-stop.sh → POST /api/claude-event → BuildCheckpoint`) and adapts it for swarm sessions. This is the right approach — consistent with the codebase and avoids the polling race condition from v3.
- **CompletionRegistry + tmux health check fallback**: Elegant dual-path design. The happy path is fast (hook fires immediately), the fallback catches crashes within 30s. Neither path alone is sufficient, but together they're robust.
- **State machine with typed enums and unit tests**: The phase transition table is explicit, the typed enums via sqlc overrides provide compile-time safety, and the table-driven tests in Phase 1 are the single highest-value test investment.
- **Child workflows with `PARENT_CLOSE_POLICY_ABANDON`**: Verified — children survive parent completion. This correctly enables the fire-and-forget pattern where the heartbeat spawns work and returns quickly.
- **Phased delivery with standalone skills**: Phases 1-3 deliver value without Temporal. Skills work standalone via CLI invocation, and the state machine + Temporal just orchestrate them. This reduces risk and allows incremental validation.
- **Comprehensive file inventory**: The 29 new files + 9 modified files table makes the scope clear and reviewable. The line estimates are reasonable.
- **Full restart path with `previous_workflow_id`**: Addresses the Chestnut flowchart's restart requirement without overcomplicating the state machine.
- **Intentional Chestnut divergence is well-documented**: The "Intentional Chestnut Flowchart Divergence" section explicitly acknowledges what's simplified and why. This prevents future confusion about design intent.

### Recommended Next Steps

1. **Resolve Critical Issue #1** — Add `cm-swarm-` to the existing reaper's skip list in `claude.go`. This is a prerequisite for any swarm work.

2. **Add missing `EventType` and `MilestoneStatus` enums** to the `enums.go` section of the plan. Include `Valid()` methods.

3. **Fix Temporal workflow ID collision** — Append attempt number or use a separate `temporal_workflow_id` column.

4. **Resolve the `SessionResult` name collision** — Rename either the enum or the struct.

5. **Switch sqlc overrides to structured format** — Use `go_type: {import: "...", type: "..."}` for consistency with existing overrides.

6. **Clarify hook injection mechanism** — Document how the swarm on-stop hook is written to the session's working directory and how it coexists with existing template hooks.

7. **Wrap `ReadTicketQueue` in a transaction** — Simple defensive measure against overlapping heartbeats.

8. **After resolving all issues** — Begin Phase 1 implementation (foundation: migration, enums, sqlc overrides, state machine + tests, conventions, setup skill).
