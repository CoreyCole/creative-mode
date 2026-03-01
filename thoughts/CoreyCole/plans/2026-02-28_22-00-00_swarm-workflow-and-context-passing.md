# Swarm Workflow: Temporal Setup & Context Passing Between Sessions

## Overview

The swarm turns high-level goals into deployed software through a pipeline of Claude Code sessions, each handling one phase (research, plan, review, implement, verify, PR). **Orchestration is deterministic Go code** — zero LLM cost for scheduling. **Intelligence lives in Claude Code sessions**. **Context lives in handoff documents** — the glue that keeps sessions connected.

## Temporal Architecture

### Two Workflow Types

```
HeartbeatWorkflow (scheduled every 2 min, runs for seconds)
├── SyncLinearState         — poll Linear API, upsert swarm_tickets
├── ReadTicketQueue         — state machine: read SQLite, determine next phases
├── ReapSessions            — kill orphaned cm-swarm-* tmux sessions
├── DetectStalls            — flag stuck workflows, post HEARTBEAT comments
├── DecayLearningRelevance  — time-decay relevance scores
├── GenerateDigest          — daily learning digest if >24h since last
└── Spawns child SessionWorkflows (fire-and-forget)

SessionWorkflow (runs minutes to hours)
└── RunClaudeSession        — one Claude Code skill in tmux, waits for hook completion
```

**HeartbeatWorkflow** is the driver. It's short-lived — runs all maintenance activities, reads the SQLite state machine, determines what to do next, spawns child `SessionWorkflow`s, and exits. Scheduled to repeat every 2 minutes.

**SessionWorkflow** wraps one Claude Code session. It creates a tmux session, waits for the on-stop hook to fire, and returns the outcome. Each workflow maps to one phase of one ticket.

### Task Queues

| Queue | Concurrency | Purpose |
|-------|-------------|---------|
| `swarm-general` | 3 | Claude Code sessions (research, plan, implement, PR) |
| `swarm-verify` | 1 | Code verification — only one `just check` at a time (OOM prevention) |
| `swarm-ops` | 1 | Heartbeat maintenance activities |

### Workflow IDs

```
Temporal workflow ID: swarm-{agentIdx}-{ticketID}-a{attempt}
                      e.g. swarm-0-CM-123-a1

Tmux session name:    cm-swarm-{agentIdx}-{ticketID}-a{attempt}
                      e.g. cm-swarm-0-CM-123-a1
```

The `cm-swarm-` prefix ensures the existing reaper (`ReapOrphanedSessions`) skips these sessions.

### State Machine (SQLite, not Temporal)

All state lives in SQLite, not Temporal. Workflows are short-lived — they read state, act, and exit. This avoids the biggest class of Temporal bugs (non-deterministic replay, signal races, workflow versioning).

```
Code Workflow Phases:
research → code_plan → plan_review → implement → verify → pr → done

Project Workflow Phases:
research → project_plan → project_review → done (spawns child code workflows)
                                         → project_verify (milestone checks)
```

**Retry loops:**
```
plan_review + revise (< max)    → code_plan  (attempt++)
plan_review + revise (>= max)   → failed
verify + logic_failure (< max)  → implement  (attempt++)
verify + logic_failure (>= max) → failed
any + infra_failure (< 2)       → same phase (retry)
any + infra_failure (>= 2)      → failed
any + timeout                   → failed
```

### Session Completion: Hook-Based

```
Claude Code session (in tmux)
  ├── Does work (research, plan, implement, etc.)
  ├── Writes handoff document to thoughts/swarm/
  ├── Writes BLUF RESULT comment to Linear (with handoff link)
  └── Exits → on-stop hook fires
       └── POSTs to /api/swarm/session-complete
            with {session_id, workflow_id, ticket_id}

Harness handler:
  ├── Reads RESULT comment from Linear
  ├── Updates swarm_sessions in SQLite
  ├── Publishes session_completed to EventBus
  └── Signals the waiting RunClaudeSession activity via completion channel
```

The `CompletionRegistry` holds per-session channels. `RunClaudeSession` registers a channel before spawning tmux, then blocks on it. The handler signals the channel when the hook fires.

**Safety net**: Every 30 seconds, `RunClaudeSession` heartbeats to Temporal and checks if the tmux session is still alive. If it died without the hook firing, it reads the RESULT comment directly as a fallback.

---

## Context Passing Between Sessions

Every Claude Code session is ephemeral. When it ends, all implicit context — files read, approaches considered, mental model of the codebase — evaporates. Three mechanisms preserve context across session boundaries:

### 1. Handoff Documents (Primary)

The main context transfer mechanism. Every session writes a handoff as its last act. The next session reads it as its first act.

**Where handoffs are written:**

| Trigger | Directory | When |
|---------|-----------|------|
| Research complete | `thoughts/swarm/handoffs-research/` | Session finished researching |
| Code plan complete | `thoughts/swarm/handoffs-code/` | Plan written, ready for review |
| Plan review verdict | `thoughts/swarm/handoffs-plan-reviews/` | Review done, feedback for planner |
| Implementation complete | `thoughts/swarm/handoffs-code/` | Code written, ready for verify |
| Verify verdict | `thoughts/swarm/handoffs-code-reviews/` | Checks run, feedback for implementer |
| PR created | `thoughts/swarm/handoffs-code/` | PR done, workflow complete |
| Context window limit | `thoughts/swarm/handoffs-{phase}/` | Mid-phase, session out of room |
| Terminal failure | `thoughts/swarm/retrospectives/` | Workflow failed, post-mortem |
| Project → child spawn | `thoughts/swarm/handoffs-project/` | Project context for each child ticket |

**Filename convention:**
```
{timestamp}_{ticketID}_{detail}.md
e.g. 2026-03-01_14-30-00_CM-123_implement_complete.md
     2026-03-01_15-45-00_CM-123_implement_context-limit.md
```

**How the next session gets the handoff:**

The orchestrator (`RunClaudeSession`) resolves the most recent handoff for the ticket before spawning:

```go
// Glob thoughts/swarm/handoffs-*/*_{ticketID}_*.md
// Sort by timestamp prefix, return most recent
handoffPath := a.resolveHandoffPath(ticketID, phase)
```

The path is passed via env var:
```
CM_SWARM_HANDOFF_PATH=thoughts/swarm/handoffs-plan-reviews/2026-03-01_15-00-00_CM-123_revise-v1.md
```

Every skill's preamble says:
```
Before starting work, read $CM_SWARM_HANDOFF_PATH if it exists.
```

### 2. Learning Context (Cross-Ticket Intelligence)

Handoffs transfer context *within* a ticket's workflow. Learnings transfer intelligence *across* tickets.

**Capture points** (automatic, from state machine):
- `plan_review` returns "revise" → `CapturePlanIssue` (what the planner got wrong)
- `verify` returns "logic_failure" → `CaptureCodeBug` (what check failed and why)
- Terminal failure → `CaptureTerminalFailure` (full post-mortem)
- Workflow done → `CaptureSuccessPattern` (what worked)

**Consumption**: Before each session, `GetLearningContext()` assembles:
- Top 5 phase-specific learnings (by relevance score)
- Top 3 critical learnings across all phases
- All learnings for this specific ticket

Written to `.swarm-learning-context.md`, path passed via:
```
CM_SWARM_LEARNING_CONTEXT_PATH=.swarm-learning-context.md
```

**Feedback loop**: Referenced learnings get relevance boosted. Old learnings decay. Daily digests surface patterns for skill improvement PRs.

### 3. Linear Comments (BLUF Summaries)

Thin structured comments on the Linear ticket. No longer the primary context mechanism — that's handoffs. RESULT comments are a BLUF (Bottom Line Up Front) with a link:

```
RESULT: success
Phase: implement
Handoff: thoughts/swarm/handoffs-code/2026-03-01_15-45-00_CM-123_implement_complete.md

Summary: Added /version endpoint. Modified 3 files.
```

Other comment types (`PLAN:`, `VERIFY:`, `HEARTBEAT:`, etc.) remain for Linear UI readability and `swarm-resume` skill parsing.

### 4. Artifact Documents (Plans, Research)

Research docs, plan docs, and review docs are standalone artifacts that live in `thoughts/swarm/`:

```
thoughts/swarm/research/2026-03-01_13-00-00_CM-123_auth-system.md
thoughts/swarm/plans/2026-03-01_14-45-00_CM-123_add-version-endpoint_v1.md
thoughts/swarm/plans/2026-03-01_15-30-00_CM-123_add-version-endpoint_v2.md
```

These are referenced from handoffs and Linear comments. The plan doc path is passed to the implement skill so it knows what to build. The research doc path is passed to the plan skill so it knows what was discovered.

---

## Full Context Flow: Code Workflow Example

```
User: /swarm-code "add /version endpoint"

1. RESEARCH SESSION
   Reads: SKILL.md, ticket description
   Writes: thoughts/swarm/research/..._CM-123_version-endpoint.md
           thoughts/swarm/handoffs-research/..._CM-123_research_complete.md
           Linear RESULT comment (BLUF + handoff link)
   Hook fires → session-complete → state machine advances

2. Heartbeat fires (2 min later)
   ReadTicketQueue: CM-123 is in code_plan, no active session → spawn

3. CODE_PLAN SESSION
   Reads: CM_SWARM_HANDOFF_PATH (research handoff)
          CM_SWARM_LEARNING_CONTEXT_PATH (cross-ticket learnings)
          Research doc (from handoff's "Key Files" section)
   Writes: thoughts/swarm/plans/..._CM-123_add-version-endpoint_v1.md
           thoughts/swarm/handoffs-code/..._CM-123_code-plan_complete.md
           Linear PLAN: comment

4. PLAN_REVIEW SESSION
   Reads: CM_SWARM_HANDOFF_PATH (code plan handoff)
          Plan doc v1
   Writes: thoughts/swarm/handoffs-plan-reviews/..._CM-123_approve.md
           Linear PLAN-REVIEW: comment
   Verdict: approve → advance to implement

5. IMPLEMENT SESSION
   Reads: CM_SWARM_HANDOFF_PATH (plan review handoff)
          Plan doc v1
          Learning context
   Does: Writes code, runs builds
   Writes: thoughts/swarm/handoffs-code/..._CM-123_implement_complete.md
           Linear IMPL: comment

6. VERIFY SESSION
   Reads: CM_SWARM_HANDOFF_PATH (implement handoff)
   Does: Runs `just check`, reads test output
   Verdict: success → advance to PR
   Writes: thoughts/swarm/handoffs-code-reviews/..._CM-123_verify_success.md

7. PR SESSION
   Reads: CM_SWARM_HANDOFF_PATH (verify handoff)
          Plan doc, research doc (for PR description)
   Does: Creates branch, commits, `gt create`
   Writes: thoughts/swarm/handoffs-code/..._CM-123_pr_complete.md
           Linear PR: comment with PR URL

→ Workflow marked done. CaptureSuccessPattern records what worked.
```

### Retry Example: Verify Fails

```
6. VERIFY SESSION (attempt 1)
   Reads: CM_SWARM_HANDOFF_PATH (implement handoff)
   Does: Runs `just check` → go-mod error
   Verdict: logic_failure
   Writes: thoughts/swarm/handoffs-code-reviews/..._CM-123_logic-failure-a1.md
   Hook fires → CaptureCodeBug records the go-mod failure

7. Heartbeat: verify logic_failure, attempts < max → implement retry

8. IMPLEMENT SESSION (attempt 2)
   Reads: CM_SWARM_HANDOFF_PATH (code review handoff — knows exactly what failed)
          CM_SWARM_LEARNING_CONTEXT_PATH (may include past go-mod bugs from other tickets)
          Plan doc
   Does: Fixes go-mod issue, re-runs build
   Writes: thoughts/swarm/handoffs-code/..._CM-123_implement_complete-a2.md
```

### Context Window Limit Example

```
5. IMPLEMENT SESSION (hits context limit mid-work)
   Has modified 2 of 5 files, is halfway through the plan
   Writes: thoughts/swarm/handoffs-code/..._CM-123_implement_context-limit.md
           Contains: files modified so far, remaining work, current approach, gotchas
   Exits gracefully

   Heartbeat: session completed with special "context_limit" result → same phase, no attempt increment

   NEW IMPLEMENT SESSION (continuation)
   Reads: CM_SWARM_HANDOFF_PATH (context limit handoff — picks up exactly where previous left off)
   Continues from step 3 of 5 in the plan
```

---

## Environment Variables Set Per Session

The orchestrator sets these before spawning each tmux session:

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_SESSION_ID` | Session ID for completion hook |
| `CM_SWARM_WORKFLOW_ID` | Workflow ID for state machine |
| `CM_SWARM_TICKET_ID` | Linear ticket identifier |
| `CM_SWARM_HANDOFF_PATH` | Path to most recent handoff for this ticket |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Path to assembled learning context file |
| `CM_HARNESS_URL` | Harness API URL for hook POST |
| `CM_HOOK_SECRET` | Auth for session-complete endpoint |

## thoughts/swarm/ Directory

```
thoughts/swarm/
  handoffs-research/          # Research phase handoffs
  handoffs-code/              # Code plan, implement, PR handoffs
  handoffs-plan-reviews/      # Plan review feedback + context
  handoffs-code-reviews/      # Verify phase feedback + context
  handoffs-project/           # Project-level handoffs
  handoffs-project-reviews/   # Project plan review feedback
  plans/                      # Code plans (v1, v2, v3...)
  research/                   # Research phase outputs
  project-plans/              # Project decomposition plans
  retrospectives/             # Terminal failure post-mortems
  digests/                    # Daily/weekly learning digests
```

All files named: `{timestamp}_{ticketID}_{detail}.md`

Find all context for a ticket: `ls thoughts/swarm/*/**CM-123**`
