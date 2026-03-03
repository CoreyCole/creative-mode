---
date: 2026-03-02T18:50:41Z
researcher: CoreyCole
git_commit: 670b5e3a0657cec9e5b4e0f631c1fb5d311342cb
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Session Edge Cases: Context Exhaustion, Implementation Looping, and Recovery Paths"
tags: [research, codebase, swarm, orchestrator, edge-cases, context-limit, session-management, recovery]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
---

# Research: Swarm Session Edge Cases

**Date**: 2026-03-02T18:50:41Z
**Researcher**: CoreyCole
**Git Commit**: 670b5e3a0657cec9e5b4e0f631c1fb5d311342cb
**Branch**: feature/agent-swarm
**Repository**: creative-mode

## Research Question

What happens when Claude Code runs out of context before issuing the stop hook? How is implementation looping handled? Are handoff documents being created when context windows get large?

## Summary

The swarm orchestrator has a **critical gap**: the `context_limit` result path exists in the state machine and would correctly resume phases without incrementing attempts, but **no skill instructs Claude to detect context pressure or emit this result**. A sentinel file mechanism exists server-side (written on 2nd compaction event) but no skill reads it. In practice, context exhaustion is treated as a crash (`infra_failure`), which has only 2 retries before terminal failure. Additionally, there is no proactive handoff creation — skills only write handoffs reactively ("if interrupted"), and there are no instructions to wrap up early when context is running low.

## Detailed Findings

### 1. The Context Limit Gap — Dead Code Path

The system has two disconnected halves:

**Server-side infrastructure (fully built):**

| Component | Location | What It Does |
|-----------|----------|-------------|
| `ContextPressure` tracker | `swarmorch/hooks.go:227-276` | Counts `PreCompact` hook events per session |
| Sentinel file | `/tmp/swarm-context-pressure-{sessionID}` | Written on 2nd compaction (`pressureThreshold = 2`) |
| `EventSwarmContextPressure` | `server/swarm_hooks.go:184` | Published to EventBus on threshold (visible on dashboard) |
| `ResultContextLimit` enum | `swarm/enums.go:51` | First-class result type |
| State machine handler | `swarm/statemachine.go:62-64` | Resumes same phase, no attempt increment |

**Client-side instructions (completely missing):**

| What's Needed | Status |
|--------------|--------|
| Skill instruction to check sentinel file | **MISSING** — no SKILL.md references it |
| Instruction for when to write `context_limit` | **MISSING** — only listed as a valid value, never explained |
| Proactive handoff creation | **MISSING** — skills only say "if interrupted" |
| `CM_SWARM_SESSION_ID` used for sentinel check | **MISSING** — env var exists but never connected to sentinel |

The conventions skill (`swarm-conventions/SKILL.md:74`) lists `context_limit` as a valid RESULT value but provides zero guidance on when or how to report it.

### 2. What Actually Happens Today When Context Runs Out

The real-world failure sequence:

```
1. Claude Code session working on implement phase
2. Context window fills → auto-compaction fires (PreCompact hook → count=1)
3. More work → second compaction (PreCompact hook → count=2, sentinel written)
4. More work → Claude Code hits hard context limit
5. Claude Code terminates without writing RESULT file
6. The `; exit` in tmux command kills the tmux session
7. watchSession() detects tmux death via 30s fallback (manager.go:496-524)
8. FireCrashRecovery Discord alert sent
9. handleSessionComplete() called → ParseResultFile() → file missing → ResultInfraFailure
10. State machine: infra_failure + attempt < 2 → retry same phase (attempt++)
11. New session spawned with attempt=1, fresh context
12. If it fails again: attempt=2 → PhaseFailed (terminal)
```

**The sentinel file was written at step 3 but never read by anyone.** The `context_limit` state machine path at step 9 is never reached because no code converts the crash into a `context_limit` result.

### 3. Session Lifecycle — Complete Flow

#### Spawning (`manager.go:326-445`)

1. Idempotency check — skip if active session exists for current phase
2. DB record creation with 8-char UUID, session name `cm-swarm-{ticketID}-{phase}`
3. Environment assembly (`buildEnv()` at `manager.go:1014-1129`):
   - `CM_SWARM_RESULT_PATH` = `/tmp/swarm-result-{sessionID}.txt`
   - `CM_SWARM_HANDOFF_PATH` = most recent handoff matching `thoughts/swarm/handoffs-*/*_{ticketID}_*.md`
   - `CM_SWARM_LEARNING_CONTEXT_PATH` = relevant historical learnings
   - `CM_SWARM_REVIEW_FEEDBACK` = if following a gate rejection
4. Hooks config generation — 6 HTTP hooks to `http://localhost:8080/api/swarm/hook/*`
5. Registry registration — buffered channels for start/completion signals
6. Tmux session creation with all env vars as `-e K=V`
7. Skill prompt: `claude --dangerously-skip-permissions --settings {path} /{skill} ; exit`
8. Watcher goroutine launched

#### Monitoring (`manager.go:449-526`)

- **Phase 1**: Wait up to 30s for `SessionStart` hook signal
- **Phase 2**: Block on completion channel with 30s tmux fallback polling
- **Primary signal**: `Stop` hook → reads result file → `SignalCompletion()`
- **Backup signal**: `SessionEnd` hook → reads result file → `SignalCompletion()` (defaults to `infra_failure` if no result)
- **Fallback**: tmux death detected → `FireCrashRecovery` alert → `handleSessionComplete()`

#### Completion (`manager.go:530-625`)

1. Double-fire guard (skip if `CompletedAt` already set)
2. Parse result file (defaults to `ResultInfraFailure` on any error)
3. Capture tokens from tmux pane via `swarm-capture-tokens.sh`
4. Update DB with result, duration, tokens
5. Capture learnings based on result type
6. Call `advanceWorkflow()` for state machine transition

### 4. Implementation Looping

#### Current loop mechanisms:

| Loop | Trigger | Phase Transition | Max Iterations |
|------|---------|-----------------|----------------|
| Verify → Implement | `logic_failure` from verify | `verify` → `implement` | `MaxVerifyRetries` = 3 |
| Plan Review → Code Plan | `logic_failure` from review | `plan_review` → `code_plan` | `MaxPlanRevisions` = 3 |
| Project Review → Project Plan | `logic_failure` from review | `project_review` → `project_plan` | 3, then human gate escalation |
| Project Verify retry | `logic_failure` from verify | `project_verify` → `project_verify` | `MaxProjectVerifyRetries` = 5 |
| Context limit continuation | `context_limit` from any phase | same phase → same phase | **UNLIMITED** (dead code) |
| Infra failure retry | `infra_failure` from any phase | same phase → same phase (attempt++) | `maxInfraRetries` = 2 |

#### What's missing for implementation looping:

- **No progress checkpointing**: The implement phase has no instructions to save incremental progress
- **No step tracking across sessions**: If session 1 completes steps 1-3 of a 10-step plan and crashes, session 2 has to figure out what was done from the handoff document (if one was written)
- **No proactive handoff trigger**: Skills only create handoffs at the end or "if interrupted" — never mid-execution based on context usage
- **No iteration cap for context_limit**: If the code path worked, a session could loop indefinitely. Needs a `MaxContextContinuations` limit.

### 5. The `check.sh` Stop Hook Interaction

Two Stop hooks fire when a swarm session ends:

1. **Project-level** (`.claude/settings.json`): `scripts/check.sh` runs `just check` (format + generate + lint) with 120s timeout
2. **Per-session HTTP** (generated `settings.json`): POST to `/api/swarm/hook/session-complete`

**Edge case**: If Claude is near context limit and `check.sh` fails (exit 2), Claude must fix lint/build issues before it can stop. This burns more context. In the worst case:
- `check.sh` fails → Claude tries to fix → runs out of context mid-fix → crash → `infra_failure`
- The useful work from the session may not have been saved to a handoff document

### 6. Recovery and Failure Paths

#### Harness restart recovery (`manager.go:290-329`)

| Scenario | Action |
|----------|--------|
| No session found for workflow | Skip; `SpawnPendingSessions` will catch it in next 2-min heartbeat |
| Session completed | Skip (already advanced) |
| Session open, tmux alive | Re-create watcher goroutine with fresh registry channels |
| Session open, tmux dead | Call `handleSessionComplete()` immediately (crash recovery) |

#### Orphan reaping (`activities.go:217-258`)

Every 2 minutes, `LeadFDEWorkflow` → `ReapSessions` finds tmux sessions with `cm-swarm-` prefix that have no matching active DB record, and kills them.

#### Stall detection (`manager.go:1942-1967`)

Workflows with `updated_at` older than 45 minutes (and not in `awaiting_review` status) trigger `FireStallDetected` Discord alert. Deduped to once per hour per ticket.

**Note**: Hook events (tool use, compaction) do NOT update `updated_at` on the workflow record — only phase transitions and status changes do. A long-running session that is actively working but hasn't transitioned phases will trigger a stall alert after 45 minutes.

#### Temporal timeouts

| Timeout | Value | Effect |
|---------|-------|--------|
| `StartToCloseTimeout` | 65 minutes | Temporal cancels the activity → `infra_failure` |
| `HeartbeatTimeout` | 2 minutes | Activity heartbeats every 15s; missing 8 consecutive → cancel |
| `maintenanceActivityTimeout` | 30 seconds | Per-activity timeout in `LeadFDEWorkflow` |

### 7. Race Condition Analysis

#### Guards that exist:

| Guard | Location | Protects Against |
|-------|----------|-----------------|
| `spawnSession` idempotency | `manager.go:334-343` | Double-spawning for same phase |
| `m.mu` mutex | `manager.go:741-742` | Concurrent `advanceWorkflow` calls |
| `CompletedAt` double-fire | `manager.go:582-584` | Processing same completion twice |
| Buffered completion channel | `registry.go:36` | Second signal blocking |

#### Remaining race window:

**TOCTOU in spawnSession**: Both `advanceWorkflow` (under `m.mu`) and `SpawnPendingSessions` (on `swarm-ops` queue) can call `spawnSession` concurrently. The idempotency check reads `GetLatestSwarmSession` but there's no DB-level uniqueness constraint on `(workflow_id, phase, completed_at IS NULL)`. Both callers could pass the check and create duplicate sessions. In practice this is unlikely because `SpawnPendingSessions` runs on a single-concurrency queue and `advanceWorkflow` holds `m.mu`, but the theoretical window exists.

### 8. Handoff Document Architecture

#### How handoffs flow between phases:

```
Session N writes handoff → thoughts/swarm/handoffs-{phase}/{timestamp}_{ticketID}_{slug}.md
                                    ↓
buildEnv() calls ResolveHandoffPath() → globs thoughts/swarm/handoffs-*/*_{ticketID}_*.md
                                    ↓
Most recent file by filename sort → set as CM_SWARM_HANDOFF_PATH
                                    ↓
Session N+1 reads this file at preamble time (per conventions protocol)
```

#### Handoff directory mapping (`handoffs.go:135-154`):

| Phase | Directory |
|-------|-----------|
| `research` | `handoffs-research` |
| `code_plan`, `implement`, `pr` | `handoffs-code` |
| `plan_review` | `handoffs-plan-reviews` |
| `verify` | `handoffs-code-reviews` |
| `project_decompose`, `project_plan`, `project_verify` | `handoffs-project` |
| `project_review` | `handoffs-project-reviews` |

#### What skills are told about handoffs:

Every skill says "write handoff as the last step before RESULT" and "if interrupted, write partial handoff." The conventions skill provides a template (BLUF, What Was Done, What Was NOT Done, Key Files, Gotchas, Next Steps).

**Gap**: No skill says "if you detect context pressure, save your progress NOW and write a handoff." The handoff is always the last thing before the RESULT file — so if the session crashes before reaching that point, no handoff is written.

## Recommended Fixes (Priority Order)

### Fix 1: Smart crash classification (HIGH IMPACT, LOW EFFORT)

In `handleSessionComplete()`, when `ParseResultFile` returns `infra_failure` (file missing/unparseable), check if context pressure was >= `pressureThreshold`. If so, reclassify as `context_limit`.

```go
// In handleSessionComplete(), after ParseResultFile:
if result.Result == swarm.ResultInfraFailure {
    if m.ctxPressure.Get(sessionID) >= pressureThreshold {
        result.Result = swarm.ResultContextLimit
        result.Summary = "context exhaustion (reclassified from crash)"
    }
}
```

This is the highest-leverage fix — it converts context crashes from "2 retries then terminal failure" to "unlimited continuations with handoff bridging."

### Fix 2: Add context pressure awareness to skills (HIGH IMPACT, MEDIUM EFFORT)

Update `swarm-conventions/SKILL.md` to add a "Context Pressure Protocol":

```markdown
## Context Pressure Protocol

The orchestrator writes a sentinel file when your session is running low on context:
  `/tmp/swarm-context-pressure-$CM_SWARM_SESSION_ID`

**Check for this file after completing each major step** (e.g., after each plan step in implement,
after each section in research). If the file exists:

1. Immediately write a handoff document with your progress so far
2. Write RESULT: context_limit to $CM_SWARM_RESULT_PATH
3. The orchestrator will spawn a fresh session that reads your handoff
```

Update `swarm-code/SKILL.md` specifically to check after each plan step.

### Fix 3: Cap context_limit iterations (MEDIUM IMPACT, LOW EFFORT)

Add `MaxContextContinuations = 5` to the state machine config. After 5 context_limit results on the same phase, transition to `PhaseFailed`. This prevents infinite loops.

```go
if lastResult == ResultContextLimit {
    if attempt >= config.MaxContextContinuations {
        return Transition{NextPhase: PhaseFailed, Failed: true}
    }
    return Transition{NextPhase: currentPhase}
}
```

**Note**: This requires context_limit to increment attempts (unlike current design). Alternative: track context continuations separately from attempts.

### Fix 4: Pre-stop handoff safety net (MEDIUM IMPACT, LOW EFFORT)

Add a `PreCompact` hook behavior that, on the 3rd compaction (pressure >= 3), writes a minimal handoff from the JSONL log. This ensures some progress is captured even if the skill never writes one.

### Fix 5: Stall detection should check hook activity (LOW IMPACT, LOW EFFORT)

Currently stall detection only checks `updated_at` on the workflow record, which is only updated on phase transitions. A session actively using tools (firing `PostToolUse` hooks every few seconds) will still trigger a stall alert after 45 minutes if it hasn't transitioned phases. Consider updating a `last_hook_at` timestamp and checking that in stall detection.

## Code References

- `harness/internal/swarm/statemachine.go:62-64` — `ResultContextLimit` handler (resumes same phase)
- `harness/internal/swarm/statemachine.go:72-78` — `ResultInfraFailure` handler (max 2 retries)
- `harness/internal/swarm/result.go:18-91` — `ParseResultFile` (defaults to `infra_failure` on any error)
- `harness/internal/swarm/enums.go:43-65` — Result type constants
- `harness/internal/swarmorch/hooks.go:227-276` — `ContextPressure` struct and sentinel file logic
- `harness/internal/swarmorch/manager.go:326-445` — `spawnSession()` full sequence
- `harness/internal/swarmorch/manager.go:449-526` — `watchSession()` monitoring loop
- `harness/internal/swarmorch/manager.go:530-625` — `handleSessionComplete()` completion handler
- `harness/internal/swarmorch/manager.go:736-1020` — `advanceWorkflow()` state routing
- `harness/internal/swarmorch/manager.go:1014-1129` — `buildEnv()` environment assembly
- `harness/internal/swarmorch/manager.go:1236-1288` — tmux session creation and skill prompt
- `harness/internal/swarmorch/manager.go:290-329` — `RecoverWorkflows()` restart recovery
- `harness/internal/swarmorch/manager.go:1942-1967` — `detectAndAlertStalls()` 45-min threshold
- `harness/internal/swarmorch/activities.go:22-109` — `RunClaudeSession` Temporal activity
- `harness/internal/swarmorch/activities.go:173-208` — `SpawnPendingSessions` catch-all
- `harness/internal/swarmorch/activities.go:217-258` — `ReapSessions` orphan cleanup
- `harness/internal/swarmorch/workflows.go:173-225` — `LeadFDEWorkflow` 2-min maintenance
- `harness/internal/swarmorch/workflows.go:13-62` — `SessionWorkflow` with 65-min timeout
- `harness/internal/swarmorch/alerts.go:13` — `alertDedupWindow = time.Hour`
- `harness/internal/server/swarm_hooks.go:155-189` — `handleSwarmHookPreCompact`
- `harness/internal/server/swarm_hooks.go:193-223` — `handleSwarmHookSessionComplete` (Stop hook)
- `harness/internal/server/swarm_hooks.go:228-265` — `handleSwarmHookSessionEnded` (crash backup)
- `harness/internal/swarm/handoffs.go:22-46` — `ResolveHandoffPath()` glob resolution
- `harness/internal/swarm/handoffs.go:135-163` — `HandoffDir()` phase-to-directory mapping
- `.claude/skills/swarm-conventions/SKILL.md:74` — `context_limit` listed but unexplained
- `.claude/skills/swarm-code/SKILL.md:88` — "If interrupted" reactive handoff
- `.claude/settings.json:27-37` — Project-level `check.sh` Stop hook

## Architecture Insights

1. **Layered completion detection**: The system uses 3 tiers — hook-driven (primary), tmux polling every 30s (fallback), and Temporal heartbeat every 2min (catch-all). This is robust but the classification logic at each tier defaults to `infra_failure`, losing context about WHY the session ended.

2. **Optimistic session spawning**: `spawnSession` uses a DB-level idempotency check (read-then-write) but no database constraint. The `m.mu` mutex protects `advanceWorkflow` but not `spawnSession` itself. The TOCTOU window is narrow but exists.

3. **Fire-and-forget integrations**: All Linear and Discord API calls are non-blocking. Failures never block workflow progression. This is correct for resilience but means integration failures are silent unless you check the error channel alerts.

4. **Handoff-as-context-bridge**: The entire inter-session continuity model relies on skills writing handoff documents. If a session crashes without writing one, the next session starts with stale context from a previous phase's handoff. This is the weakest link in the architecture.

## Historical Context

- `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md` — Original design for context passing between sessions
- `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — v5 plan includes handoff format, token tracking
- `thoughts/CoreyCole/reviews/2026-03-02_swarm-implementation-vs-v5-and-chestnut-review.md` — Gap analysis identifying missing error handling
- `thoughts/CoreyCole/research/2026-03-01_23-00-00_overstory-additional-improvements.md` — Safety guards research
- `thoughts/CoreyCole/handoffs/general/2026-03-02_10-18-34_phase-ef-event-observability-doctor.md` — Event observability and health check implementation
- `thoughts/swarm/retrospectives/2026-03-02-CRE-5-research.md` — First real-world workflow run retrospective

## Open Questions

1. **Should `context_limit` increment attempts?** Current design says no (unlimited continuations). But without a cap, a session that makes zero progress per context window would loop forever. Need either: (a) a separate `context_continuations` counter with its own max, or (b) require the handoff to show meaningful progress (hard to enforce).

2. **Can the PreCompact hook inject instructions?** If the hook response could modify Claude's system prompt (like PreToolUse can deny commands), we could inject "you are running low on context, wrap up now" without changing SKILL.md files. Need to check if PreCompact supports response-based behavior modification in Claude Code.

3. **Should we track `last_hook_at` for stall detection?** The 45-minute stall threshold checks `updated_at` which only changes on phase transitions. A session actively working for 60 minutes on implement (firing PostToolUse hooks constantly) triggers a false-positive stall alert. Should we add a `last_hook_at` column or is the stall alert acceptable as a "check on this" signal?

4. **What about the verify→implement loop with context pressure?** If a verification session fails (logic_failure) and loops back to implement, the new implement session gets a fresh context window. But what if the implement session itself hits context pressure, writes `context_limit`, and restarts? The attempt counter is shared between the verify→implement loop (`MaxVerifyRetries`) and context continuations. Need to verify these don't interfere.

5. **Race condition in spawnSession**: Should we add a DB-level uniqueness constraint on `(workflow_id, phase)` where `completed_at IS NULL`? Or wrap `spawnSession` in the `m.mu` mutex?
