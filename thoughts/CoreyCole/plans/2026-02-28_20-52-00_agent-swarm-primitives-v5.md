# Agent Swarm Primitives v5 — Implementation Plan

## Context

The v4 plan (`thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`) designed a complete agent swarm orchestrator: 14 Claude Code skills, a Temporal-driven SQLite state machine, hook-based session completion, and a Datastar dashboard. Two reviews (v4 review + focused Temporal/self-improvement review) found the orchestration mechanically sound but identified a fundamental gap: **the system has zero mechanisms to learn from its own outputs**. Skills are static, failures repeat, and there's no connection to the existing mayor/president learning infrastructure.

v5 adds a **continuous learning layer** and a **handoff system** on top of v4's orchestration. The learning layer captures learnings in SQLite at every decision point (plan revisions, verification failures, terminal failures, successes), surfaces them to future sessions, and generates periodic digests that the president/lead FDE uses to improve skills, adjust config, and contribute template fixes. The handoff system ensures continuous context across session boundaries — every session writes a handoff document as its last act, and the next session reads it as its first.

**Key principles**:
- Intelligence lives in Claude Code sessions. Orchestration is deterministic Go code.
- *Learning* is structured data capture + deterministic pattern detection + Claude Code sessions that act on the patterns.
- *Handoffs* are the primary inter-session context transfer mechanism. RESULT comments become a BLUF (Bottom Line Up Front) summary with a link to the detailed handoff document.

**Companion document**: `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md` — operational walkthrough of the Temporal setup and context passing mechanics. Includes full code workflow example (7 phases), retry flow, context window limit continuation, and CompletionRegistry pattern details. Read alongside this plan for the "how it runs" perspective.

## What v5 Adds to v4

v4 remains the authoritative plan for orchestration, Temporal setup, skills, and the base schema. v5 adds:

1. **`swarm_learnings` table** — captures plan issues, code bugs, success patterns, post-mortems, conventions
2. **`swarm_learning_digests` table** — periodic summaries for the president/lead FDE
3. **Automatic capture** at 4 state machine decision points
4. **Learning context injection** into every Claude Code session via env var + file
5. **Relevance scoring** with time decay, reference boosting, and auto-archival
6. **Daily digest generation** with deterministic pattern detection and action items
7. **Skill improvement PR flow** reusing the existing `ContributeLearning()` pattern
8. **President integration** via a `swarm-learnings` skill
9. **Handoff documents** at every session boundary for continuous context
10. **`thoughts/swarm/` directory structure** organized by document type with ticket IDs in filenames
11. **BLUF RESULT comments** — thin Linear comments linking to detailed handoff docs
12. **Fixes for v4 review issues**: transaction isolation in `ReadTicketQueue`, hook directory handling

## Database Schema Additions

Add to `harness/internal/db/migrations/006_swarm_tables.sql` (alongside the 7 v4 tables):

### `swarm_learnings`

```sql
CREATE TABLE swarm_learnings (
    id                 TEXT PRIMARY KEY,
    source_workflow_id TEXT REFERENCES swarm_workflows(id),
    source_session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id          TEXT NOT NULL,
    category           TEXT NOT NULL CHECK(category IN (
        'plan_issue', 'code_bug', 'pattern', 'post_mortem', 'convention'
    )),
    phase              TEXT,
    severity           TEXT NOT NULL DEFAULT 'info' CHECK(severity IN (
        'critical', 'warning', 'info'
    )),
    title              TEXT NOT NULL,
    content            TEXT NOT NULL,
    doc_path           TEXT,
    tags               TEXT,                              -- JSON array
    relevance_score    REAL NOT NULL DEFAULT 1.0,
    referenced_count   INTEGER NOT NULL DEFAULT 0,
    archived_at        TEXT,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_swarm_learnings_category ON swarm_learnings(category, archived_at);
CREATE INDEX idx_swarm_learnings_ticket ON swarm_learnings(ticket_id);
CREATE INDEX idx_swarm_learnings_relevance ON swarm_learnings(relevance_score DESC)
    WHERE archived_at IS NULL;
```

### `swarm_learning_digests`

```sql
CREATE TABLE swarm_learning_digests (
    id              TEXT PRIMARY KEY,
    digest_type     TEXT NOT NULL CHECK(digest_type IN ('daily', 'weekly', 'ad_hoc')),
    period_start    TEXT NOT NULL,
    period_end      TEXT NOT NULL,
    learning_count  INTEGER NOT NULL,
    summary         TEXT NOT NULL,
    action_items    TEXT,                                  -- JSON array
    doc_path        TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### New Enum Types (add to `harness/internal/swarm/enums.go`)

```go
type LearningCategory string
const (
    LearningPlanIssue  LearningCategory = "plan_issue"
    LearningCodeBug    LearningCategory = "code_bug"
    LearningPattern    LearningCategory = "pattern"
    LearningPostMortem LearningCategory = "post_mortem"
    LearningConvention LearningCategory = "convention"
)

type LearningSeverity string
const (
    SeverityCritical LearningSeverity = "critical"
    SeverityWarning  LearningSeverity = "warning"
    SeverityInfo     LearningSeverity = "info"
)
```

### sqlc.yaml Overrides (add to existing overrides)

```yaml
  - column: "swarm_learnings.category"
    go_type:
      import: "creative-mode/harness/internal/swarm"
      type: "LearningCategory"
  - column: "swarm_learnings.severity"
    go_type:
      import: "creative-mode/harness/internal/swarm"
      type: "LearningSeverity"
```

## Handoff System

Handoffs are the primary mechanism for continuous context across session boundaries. Every Claude Code session in the swarm is ephemeral — when it ends, all implicit context (files read, approaches considered, mental model of the codebase) evaporates. Handoff documents capture the *working state*: what was being worked on, what was tried, what was rejected, and what the next agent needs to know.

### BLUF RESULT Comments

RESULT comments on Linear become thin summaries linking to the detailed handoff:

```
RESULT: success
Phase: implement
Handoff: thoughts/swarm/handoffs-code/2026-03-01_15-45-00_CM-123_implement_complete.md

Summary: Added /version endpoint. Modified 3 files. Discovered auth middleware
needs to exclude the new route — documented in handoff for verify phase.
```

### Handoff Trigger Points

Six points in the swarm workflow where handoff documents are created:

#### 1. Context Window Limit (mid-phase)
The session is running out of context window room mid-work. Must hand off to a fresh session that continues the same phase. This is the most critical handoff because it's *unplanned* — the agent didn't finish its work.
- **Directory**: `thoughts/swarm/handoffs-{phase}/`
- **Detail suffix**: `context-limit`

#### 2. Phase Completion (every session)
Each session's last act before exiting writes a handoff. Even on success. The next phase's session starts cold — it reads the plan doc or research doc, but doesn't know *why* certain decisions were made, which files were most important, or what gotchas were discovered. Phase-completion handoffs are the glue between research→plan→review→implement→verify→PR.
- **Directory**: `thoughts/swarm/handoffs-{phase}/`
- **Detail suffix**: `complete`

#### 3. Retry Transitions (plan_review→code_plan, verify→implement)
When plan_review says "revise" or verify says "logic_failure", the retry session needs more than "it failed." It needs:
- What was tried and why it seemed right
- The exact failure context (beyond the RESULT comment)
- Which approaches were already ruled out
- Files that were modified and their state
- **Directory**: `thoughts/swarm/handoffs-plan-reviews/` or `thoughts/swarm/handoffs-code-reviews/`
- **Detail suffix**: `revise-v{N}` or `logic-failure-a{N}`

#### 4. Terminal Failure → Full Restart
v5 captures a retrospective here, but the handoff is forward-looking. When a human decides to restart (`--previous CM-XXX`), the new workflow's research phase reads the handoff, not just the retrospective.
- **Directory**: `thoughts/swarm/retrospectives/`
- **Detail suffix**: `terminal-failure`

#### 5. Project → Child Workflow Spawn
When a project workflow decomposes into child code workflows, each child gets a ticket + title. But the project agent built up deep context about how the pieces fit together, dependencies between tickets, architectural constraints. A handoff per child ticket captures that project-level context for the child.
- **Directory**: `thoughts/swarm/handoffs-project/`
- **Detail suffix**: `spawn-{childTicketID}`

#### 6. Cross-Workflow Discovery (rare)
When one workflow discovers something that affects another in-flight workflow (e.g., "I just refactored module X which workflow B is also touching"). More of a broadcast than a traditional handoff. Written proactively by the discovering agent via `POST /api/swarm/learnings`.
- **Directory**: `thoughts/swarm/handoffs-code/` (on the affected ticket)
- **Detail suffix**: `cross-workflow-{sourceTicketID}`

### Handoff Document Template

**Note**: In standalone mode (Phase 2, no Temporal), `workflow_id` and `session_id` will be empty. Skills should populate whatever env vars are available and leave the rest blank. The template works for both standalone and orchestrated invocation.

```markdown
---
ticket_id: CM-123
phase: implement
result: success | logic_failure | context_limit | ...
attempt: 1
workflow_id: swarm-0-CM-123-a1              # empty in standalone mode
session_id:                                  # empty in standalone mode
timestamp: 2026-03-01T15:45:00-08:00
previous_handoff: thoughts/swarm/handoffs-code/2026-03-01_14-30-00_CM-123_code-plan_complete.md
---

# Handoff: CM-123 — {phase} {result}

## BLUF
One-paragraph summary of what happened and what the next session needs to do.

## What Was Done
- Files modified and why
- Key decisions made
- Approaches tried

## What Was NOT Done
- Remaining work in this phase
- Items deferred to next phase

## Key Files
- `path/to/file.go:123` — why this file matters
- `path/to/other.go` — what was changed

## Gotchas & Discoveries
- Things the next agent needs to know
- Patterns discovered
- Codebase conventions learned

## Next Steps
1. Explicit instructions for the next session
2. What to do first
3. What to watch out for
```

### Handoff Consumption

Each swarm skill reads the most recent handoff for its ticket at session start. The handoff path is passed via `CM_SWARM_HANDOFF_PATH` env var (set by the orchestrator before spawning the session, or empty for standalone invocation). Skills include this in their preamble:

```
Before starting work:
1. Read $CM_SWARM_HANDOFF_PATH if it exists — this is the previous session's context.
2. Read $CM_SWARM_LEARNING_CONTEXT_PATH if it exists — these are cross-ticket learnings.
```

### Handoff Path Resolution

The orchestrator (`RunClaudeSession` activity) resolves the most recent handoff for the ticket before spawning:

```go
func (a *Activities) resolveHandoffPath(ticketID string) string {
    // Glob across ALL handoff directories: thoughts/swarm/handoffs-*/*_{ticketID}_*.md
    // Return the most recent by timestamp prefix (regardless of which phase directory it's in)
    // This is correct because the "previous handoff" for an implement session may be
    // from handoffs-plan-reviews/ (plan review approval), not from handoffs-code/.
    // Each session needs the most recent handoff for the ticket, period.
    // Returns "" if no handoff exists (first session for this ticket)
}
```

## Automatic Capture Points

Four capture functions in `harness/internal/swarm/learnings.go`, called from the state machine:

### 1. `CapturePlanIssue` — when `plan_review` returns "revise"

Called from `ReadTicketQueue` when `DetermineNextPhase` cycles back from `plan_review` → `code_plan`. Parses the RESULT comment's `detail` JSON to extract the specific issues the reviewer found. Severity escalates to `critical` if attempt >= 2 (repeated plan failures).

### 2. `CaptureCodeBug` — when `verify` returns "logic_failure"

Called from `ReadTicketQueue` when `DetermineNextPhase` cycles back from `verify` → `implement`. Parses the RESULT comment's `checks` array to extract which checks failed and why. Tags are extracted from failure details (e.g., "go-mod", "trunk", "sqlc").

### 3. `CaptureTerminalFailure` — when workflow reaches terminal failure

Called from `markFailed`. Builds a retrospective from the full session history for the workflow. Writes a `thoughts/swarm/retrospectives/{timestamp}_{ticketID}_terminal-failure.md` document. Always `severity: critical`.

### 4. `CaptureSuccessPattern` — when workflow reaches "done"

Called from `markDone`. Records what worked — clean runs (no retries) are `info`, recovered successes (had retries) are `warning` (worth learning the recovery path from).

### Integration in `ReadTicketQueue`

```go
// In the workflow processing loop, after DetermineNextPhase:
if lastSession != nil && lastSession.Phase == PhasePlanReview &&
    nextPhase == PhaseCodePlan && shouldRetry {
    a.CapturePlanIssue(ctx, wf, *lastSession, lastSession.Detail)
}
if lastSession != nil && lastSession.Phase == PhaseVerify &&
    lastSession.Result == ResultLogicFailure &&
    nextPhase == PhaseImplement && shouldRetry {
    a.CaptureCodeBug(ctx, wf, *lastSession, lastSession.Detail)
}
```

`markFailed` calls `CaptureTerminalFailure`. `markDone` calls `CaptureSuccessPattern`. Both are non-fatal — learning capture failures are logged but don't block the state machine.

## Learning Consumption

### Pre-Session Context Injection

`GetLearningContext(ctx, params SessionParams) (string, error)` assembles relevant learnings before each Claude Code session:

1. **Phase-specific** — top 5 learnings for the upcoming phase (by relevance score)
2. **Critical** — top 3 critical learnings across all phases (deduped)
3. **Ticket-specific** — all learnings for this ticket (for retries/restarts)

Output is written to `/tmp/swarm-learning-context-{ticketID}.md`. The path is passed via `CM_SWARM_LEARNING_CONTEXT_PATH` env var. Each referenced learning gets its `referenced_count` incremented (feeds relevance scoring). Writing to `/tmp/` avoids gitignore concerns — this is an ephemeral per-session file, not a persistent artifact.

### Skill-Side Consumption

Standard preamble added to `swarm-conventions/SKILL.md`:

```
Before starting work, read $CM_SWARM_LEARNING_CONTEXT_PATH if it exists.
These are past learnings relevant to your task — use them to avoid repeating known issues.
```

Each skill references this convention. No per-skill changes needed.

### Manual Learning Creation

Skills can POST to `POST /api/swarm/learnings` to record conventions or patterns discovered during work. This lets Claude Code sessions contribute learnings proactively, not just at state machine transitions.

## Learning Feedback Loop

### Relevance Decay

`DecayLearningRelevance` runs from the heartbeat but **skips if <1 hour since last run** (scores change on a daily timescale — running the formula every 2 minutes is wasteful). Tracks last run time in `swarm_config` or a simple file. Formula:

```
newScore = min((decayFactor * severityFactor) + referenceBoost, 1.0)
  where:
    decayFactor = 1.0 / (1.0 + ageDays/30.0)      -- halves every 30 days
    severityFactor = 2.0 (critical), 1.5 (warning), 1.0 (info)
    referenceBoost = min(referencedCount * 0.1, 0.5) -- capped at +0.5
```

Learnings older than 60 days with relevance < 0.1 are auto-archived (soft delete).

### Daily Digest Generation

`GenerateDigest` runs from the heartbeat, checks if last digest is >24h old:

1. Queries all learnings since last digest
2. Groups by category (post_mortem > code_bug > plan_issue > convention > pattern)
3. Runs deterministic pattern detection:
   - Same tag appears in >=2 code bugs → suggest updating `swarm-code-verify` SKILL.md
   - >=2 plan issues → suggest updating `swarm-code-plan` SKILL.md
   - Any post-mortems → suggest reviewing for systemic SwarmConfig changes
4. Writes digest to `thoughts/swarm/digests/{date}_digest.md`
5. Stores in `swarm_learning_digests` table

### President/Lead FDE Integration (Autonomous with PR Gate)

New `swarm-learnings` president skill (add to `harness/internal/president/skills.go`):

- `GET /api/swarm/learnings?since=24h` — recent learnings
- `GET /api/swarm/learnings?category=code_bug&top=10` — filtered queries
- `GET /api/swarm/learnings/digest/latest` — latest digest with action items

**Autonomy model**: The president acts autonomously on digests but all changes go through PR review:

1. President reads the digest during its heartbeat cycle
2. When action items suggest skill updates (e.g., "Repeated 'go-mod' bugs — update swarm-code-verify SKILL.md"), the president uses its existing `template-update` skill to spawn a Claude Code session
3. That session reads the digest + relevant learnings, modifies the SKILL.md file(s), and calls `ContributeSkillImprovement` which creates a PR
4. Human reviews and merges the PR — this is the safety gate

This reuses the existing president infrastructure (`POST /api/president/template-update`) and the mayor PR pattern (`ContributeLearning()`). No new agent or manual intervention needed for the detection→proposal step, but humans always approve the actual change.

### Skill Improvement PR Flow

`ContributeSkillImprovement` in `harness/internal/swarm/learnings.go` mirrors `ContributeLearning()` from `harness/internal/mayor/learning.go:14`:

- Creates a **git worktree** in `/tmp/swarm-skill-pr-{timestamp}/` (avoids the crash fragility of `ContributeLearning` which operates on the main repo checkout — if killed mid-sequence, the repo is left on a stale branch)
- Creates branch `swarm/{ticketID}/skill-improvement-{timestamp}` in the worktree
- Commits the modified SKILL.md
- Pushes and creates PR via `gh pr create`
- Cleans up the worktree
- Human reviews and merges

This is the same PR-based human gate used by mayors. Template improvements from both systems go through the same review pipeline. The worktree approach is safer than `ContributeLearning`'s direct-checkout pattern — a future improvement should migrate `ContributeLearning` to worktrees as well.

## Thoughts Directory Convention

All swarm documents live under `thoughts/swarm/`, organized by document type. Ticket IDs are in filenames, not directory paths. This enables globbing across all documents for a ticket (`thoughts/swarm/*/*{ticketID}*`) or all documents of a type.

```
thoughts/swarm/
  handoffs-research/       # Handoffs from research phase sessions
  handoffs-code/           # Handoffs from implement phase sessions
  handoffs-plan-reviews/   # Plan review feedback + context for next plan attempt
  handoffs-code-reviews/   # Verify phase feedback + context for retry
  handoffs-project/        # Project-level handoffs (decomposition, child spawns)
  handoffs-project-reviews/# Project plan review feedback
  plans/                   # Code plans (v1, v2, v3...)
  research/                # Research phase outputs
  project-plans/           # Project decomposition plans
  retrospectives/          # Terminal failure post-mortems
  digests/                 # Daily/weekly learning digests
```

### File Naming Convention

```
{timestamp}_{ticketID}_{detail}.md

# Examples:
handoffs-code/2026-03-01_14-30-00_CM-123_implement_complete.md
handoffs-code/2026-03-01_15-45-00_CM-123_implement_context-limit.md
handoffs-plan-reviews/2026-03-01_15-00-00_CM-123_revise-v1.md
handoffs-code-reviews/2026-03-01_16-20-00_CM-123_logic-failure-a2.md
plans/2026-03-01_14-45-00_CM-123_add-version-endpoint_v1.md
plans/2026-03-01_15-30-00_CM-123_add-version-endpoint_v2.md
research/2026-03-01_13-00-00_CM-123_auth-system.md
retrospectives/2026-03-01_17-00-00_CM-123_terminal-failure.md
digests/2026-03-01_daily.md
handoffs-project/2026-03-01_14-00-00_CM-100_spawn-CM-123.md
```

### Cross-Ticket Discovery

An agent working on CM-123 can find all its context:
```bash
ls thoughts/swarm/*/**CM-123**
```

Human-initiated plans/reviews continue to use the existing per-user convention:
```
thoughts/{git_user}/plans/{timestamp}_{slug}.md
thoughts/{git_user}/reviews/{timestamp}_{slug}_review.md
```

## v4 Review Fixes Incorporated

### 1. Transaction Isolation in `ReadTicketQueue`

Wrap the entire read-modify-write cycle in `BEGIN IMMEDIATE`. Add `WithTx(func(tx) error) error` to the store interface. SQLite IMMEDIATE mode acquires a write lock at BEGIN, serializing concurrent heartbeats.

### 2. Hook Directory Handling

Instead of writing `.claude/hooks/on-stop.sh` at the repo root (which could conflict with existing project-level hooks), write the swarm hook to a temp directory and pass the path via `--hooks-dir` CLI flag or `CLAUDE_HOOKS_DIR` env var. If neither is available, chain with existing hooks.

### 3. EventBus Subscription Key

Document that `Subscribe("swarm")` uses a synthetic key. Add a comment to the EventBus explaining that keys are arbitrary strings, not strictly world IDs.

### 4. Reaper Relationship

Two separate reapers run independently:
- **Existing `ReapOrphanedSessions`** (`harness/internal/claude/claude.go:293-337`) — handles `cm-{worldID}-{cpID}` build sessions. Must be modified to **skip** `cm-swarm-*` sessions (check prefix, not just existence).
- **Swarm `ReapSessions`** — heartbeat maintenance activity that kills orphaned `cm-swarm-*` tmux sessions. Only reaps swarm sessions where the corresponding `swarm_sessions` row shows no active Temporal workflow.

The `cm-swarm-` prefix is the discriminator. The existing reaper must learn to ignore it; the swarm reaper must only touch it.

## Swarm Hook System

The v4 plan only uses the **Stop** hook. v5 adds 5 more hooks for full lifecycle observability and deterministic behavior. All hooks POST to the harness via HTTP, authenticated with `CM_HOOK_SECRET`.

### Hook Configuration

Swarm sessions use a dedicated hooks config written to a temp directory before tmux spawn. The `--hooks-dir` flag (or `CLAUDE_HOOKS_DIR` env var) points Claude Code to this directory instead of the repo-root `.claude/hooks/`. This avoids conflicts with the project-level Stop hook (`scripts/check.sh`).

```
/tmp/swarm-hooks-{sessionID}/
  hooks.json          # Hook configuration (SessionStart, PreToolUse, PostToolUse, PreCompact, Stop, SessionEnd)
  on-stop.sh          # Stop hook shell script — captures token count from tmux before POSTing
```

The `hooks.json` uses HTTP hook type pointing at the harness:

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "http", "url": "$CM_HARNESS_URL/api/swarm/hook/session-started", "headers": {"X-Hook-Secret": "$CM_HOOK_SECRET"}, "allowedEnvVars": ["CM_HOOK_SECRET", "CM_HARNESS_URL", "CM_SWARM_SESSION_ID"], "timeout": 10 }] }],
    "PreToolUse": [{ "matcher": "Bash", "hooks": [{ "type": "http", "url": "$CM_HARNESS_URL/api/swarm/hook/pre-tool-use", "headers": {"X-Hook-Secret": "$CM_HOOK_SECRET"}, "allowedEnvVars": ["CM_HOOK_SECRET", "CM_HARNESS_URL", "CM_SWARM_SESSION_ID"], "timeout": 5 }] }],
    "PostToolUse": [{ "hooks": [{ "type": "http", "url": "$CM_HARNESS_URL/api/swarm/hook/post-tool-use", "headers": {"X-Hook-Secret": "$CM_HOOK_SECRET"}, "allowedEnvVars": ["CM_HOOK_SECRET", "CM_HARNESS_URL", "CM_SWARM_SESSION_ID"], "timeout": 5 }] }],
    "PreCompact": [{ "hooks": [{ "type": "http", "url": "$CM_HARNESS_URL/api/swarm/hook/pre-compact", "headers": {"X-Hook-Secret": "$CM_HOOK_SECRET"}, "allowedEnvVars": ["CM_HOOK_SECRET", "CM_HARNESS_URL", "CM_SWARM_SESSION_ID"], "timeout": 5 }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "/tmp/swarm-hooks-SESSION_ID/on-stop.sh", "timeout": 30 }] }],
    "SessionEnd": [{ "hooks": [{ "type": "http", "url": "$CM_HARNESS_URL/api/swarm/hook/session-ended", "headers": {"X-Hook-Secret": "$CM_HOOK_SECRET"}, "allowedEnvVars": ["CM_HOOK_SECRET", "CM_HARNESS_URL", "CM_SWARM_SESSION_ID"], "timeout": 10 }] }]
  }
}
```

### Hook Endpoints

| Hook Event | Endpoint | Purpose | Blocking? |
|------------|----------|---------|-----------|
| **SessionStart** | `POST /api/swarm/hook/session-started` | Confirms Claude Code initialized. `RunClaudeSession` waits up to 30s for this before failing fast. | No |
| **PreToolUse (Bash)** | `POST /api/swarm/hook/pre-tool-use` | Enforces denied commands server-side. Returns exit 2 (block) for `cargo build`, `go build`, `templ generate`, etc. Same deny list as `.claude/settings.json` but enforced even when using a separate hooks dir. | Yes (exit 2 blocks) |
| **PostToolUse** | `POST /api/swarm/hook/post-tool-use` | Live progress tracking. Publishes tool activity to EventBus → SSE → dashboard. Fire-and-forget (5s timeout, failures logged but don't block). | No |
| **PreCompact** | `POST /api/swarm/hook/pre-compact` | Context pressure detection. First PreCompact is informational. Second PreCompact for same session sets `context_pressure=true` in `swarm_sessions`, which skills can check via `GET /api/swarm/session/{id}/status` to trigger orderly handoff. | No |
| **Stop** | `POST /api/swarm/session-complete` | Primary completion signal. Signals `CompletionRegistry` to unblock `RunClaudeSession` activity. Retries handled by HTTP hook config. | No |
| **SessionEnd** | `POST /api/swarm/hook/session-ended` | Crash detection backup. If Stop already fired, this is a no-op. If Stop didn't fire (Claude crashed), this signals completion with `result=infra_failure`. Catches crashes that the 30s tmux health check would otherwise need to detect. | No |

### PreToolUse Deny List

The PreToolUse handler checks the `tool_input.command` field against the same patterns denied in `.claude/settings.json`:

```go
var swarmDenyPatterns = []string{
    `cargo\s+(build|clippy|check)`,
    `go\s+build`,
    `templ\s+generate`,
    `just\s+generate`,
}
```

If matched, the handler returns a JSON response with `permissionDecision: "deny"` and a reason explaining why. This is defense-in-depth — the settings.json deny rules are the primary gate, but swarm sessions use a separate hooks dir and may not inherit project-level settings.

### Context Pressure Flow

```
Session running normally
  ↓
PreCompact fires (auto-compaction triggered by context window filling)
  → POST /api/swarm/hook/pre-compact
  → Handler: compact_count++ for this session in memory
  → If compact_count == 1: log, no action
  → If compact_count >= 2: set context_pressure=true in swarm_sessions
  ↓
Skill checks context pressure (via env var or API):
  → Reads CM_SWARM_SESSION_ID, queries GET /api/swarm/session/{id}/status
  → If context_pressure=true: write handoff, exit gracefully
  → State machine: result=context_limit → same phase, no attempt increment
```

**Alternative (simpler)**: Instead of polling an API, the PreCompact hook could write a sentinel file (e.g., `/tmp/swarm-context-pressure-{sessionID}`) that the skill checks with a simple file existence test. This avoids the API round-trip.

### RunClaudeSession Activity — Hook Integration

```go
func (a *Activities) RunClaudeSession(ctx context.Context, params SessionParams) (SessionResult, error) {
    // 1. Write hooks.json to /tmp/swarm-hooks-{sessionID}/
    // 2. Register completion channel in CompletionRegistry
    // 3. Register session-started channel in StartRegistry
    // 4. Spawn tmux session with CLAUDE_HOOKS_DIR=/tmp/swarm-hooks-{sessionID}/
    // 5. Wait for session-started signal (30s timeout → fail fast with infra_failure)
    // 6. Enter main wait loop:
    //    - Block on completion channel (Stop or SessionEnd hook)
    //    - Every 30s: heartbeat to Temporal + check tmux alive
    //    - If tmux dead and no signal: read RESULT comment as fallback
    // 7. Clean up: remove /tmp/swarm-hooks-{sessionID}/
}
```

## API Endpoints

### Learning Endpoints

| Route | Method | Auth | Purpose |
|-------|--------|------|---------|
| `/api/swarm/learnings` | GET | `X-Swarm-Secret` | Query learnings (filters: category, phase, ticket, since, top, search) |
| `/api/swarm/learnings` | POST | `X-Hook-Secret` | Create learning from skill session |
| `/api/swarm/learnings/digest/latest` | GET | `X-Swarm-Secret` | Latest digest with action items |

### Hook Endpoints

| Route | Method | Auth | Purpose |
|-------|--------|------|---------|
| `/api/swarm/hook/session-started` | POST | `X-Hook-Secret` | SessionStart confirmation |
| `/api/swarm/hook/pre-tool-use` | POST | `X-Hook-Secret` | PreToolUse deny enforcement |
| `/api/swarm/hook/post-tool-use` | POST | `X-Hook-Secret` | PostToolUse progress tracking |
| `/api/swarm/hook/pre-compact` | POST | `X-Hook-Secret` | PreCompact context pressure detection |
| `/api/swarm/session-complete` | POST | `X-Hook-Secret` | Stop completion signal (from v4) |
| `/api/swarm/hook/session-ended` | POST | `X-Hook-Secret` | SessionEnd crash backup |

### Observability Endpoints

| Route | Method | Auth | Purpose |
|-------|--------|------|---------|
| `/api/swarm/health` | GET | `X-Swarm-Secret` | Full system health (capacity, Temporal, active workflows, alerts, metrics summary) |
| `/api/swarm/metrics` | GET | `X-Swarm-Secret` | Aggregate metrics (`?period=24h\|7d\|30d\|all`) — completion rate, phase durations, retry rates, cost estimate |
| `/api/swarm/session/{id}/status` | GET | `X-Swarm-Secret` | Session status including `context_pressure` flag |
| `/api/swarm/session/{id}/log` | GET | `X-Swarm-Secret` | Per-session JSONL log for historical inspection |

## Observability

### 1. Aggregate Metrics (`/api/swarm/metrics`)

The `swarm_events` table records every state transition but has no aggregation layer. Without metrics, an operator can't answer "is the swarm effective?" Add `GET /api/swarm/metrics` returning:

```json
{
  "period": "24h",
  "workflows": {
    "total": 12,
    "completed": 8,
    "failed": 2,
    "in_progress": 2,
    "completion_rate": 0.80
  },
  "phases": {
    "research":    { "avg_duration_min": 5.2,  "count": 10 },
    "code_plan":   { "avg_duration_min": 8.1,  "count": 9 },
    "plan_review": { "avg_duration_min": 4.3,  "count": 9, "revise_rate": 0.22 },
    "implement":   { "avg_duration_min": 15.7, "count": 8 },
    "verify":      { "avg_duration_min": 3.5,  "count": 10, "retry_rate": 0.30 },
    "pr":          { "avg_duration_min": 2.1,  "count": 8 }
  },
  "retries": {
    "plan_revisions": 2,
    "verify_retries": 3,
    "infra_retries": 1
  },
  "learnings": {
    "total": 15,
    "by_category": { "code_bug": 5, "plan_issue": 3, "pattern": 4, "convention": 2, "post_mortem": 1 }
  },
  "cost": {
    "total_session_minutes": 187,
    "total_tokens": 3200000,
    "tokens_by_phase": {
      "research": 450000,
      "code_plan": 620000,
      "plan_review": 380000,
      "implement": 1100000,
      "verify": 350000,
      "pr": 300000
    }
  }
}
```

Computed from `swarm_events` + `swarm_sessions` + `swarm_workflows` + `swarm_learnings`. Query supports `?period=24h|7d|30d|all`. Returned in the dashboard header and via API.

Implementation: `harness/internal/swarm/metrics.go` — SQL aggregation queries, no materialized views needed at this scale. Cached in memory for 60s to avoid repeated computation on dashboard refreshes.

### 2. Structured Logging with Correlation IDs

Every log line in the swarm code path includes correlation fields so `journalctl -u creative-mode | grep CM-123` shows the full timeline:

```go
logger := slog.With(
    "subsystem", "swarm",
    "ticket_id", ticketID,
    "workflow_id", workflowID,
    "session_id", sessionID,
    "phase", phase,
)
```

**Log levels**:
| Level | What |
|-------|------|
| `INFO` | State transitions, session spawn/complete, phase changes, handoff written |
| `WARN` | Retries, context pressure detected, learning capture failures, stall detection |
| `ERROR` | Terminal failures, crash recovery (SessionEnd without Stop), hook delivery failures, infra failures |

Every hook handler logs at `INFO` with the session's correlation fields. The `ReadTicketQueue` state machine loop logs each spawn decision. `RunClaudeSession` logs the full lifecycle: spawn → SessionStart received → (tool events) → Stop received → result.

### 3. `/api/swarm/health` Response Schema

```json
{
  "status": "healthy",
  "capacity": {
    "max_sessions": 4,
    "active_sessions": 2,
    "available": 2
  },
  "temporal": {
    "connected": true,
    "last_heartbeat": "2026-03-01T15:45:00Z",
    "heartbeat_interval_sec": 120
  },
  "active_workflows": [
    {
      "workflow_id": "swarm-CM-123-a1",
      "ticket_id": "CM-123",
      "ticket_title": "Add /version endpoint",
      "phase": "implement",
      "status": "running",
      "session_id": "abc-123",
      "started_at": "2026-03-01T15:30:00Z",
      "duration_min": 15,
      "context_pressure": false,
      "last_tool_event": {
        "tool_name": "Edit",
        "file_path": "harness/internal/server/server.go",
        "timestamp": "2026-03-01T15:44:30Z"
      }
    }
  ],
  "recent_completions": [
    {
      "workflow_id": "swarm-CM-122-a1",
      "ticket_id": "CM-122",
      "result": "done",
      "completed_at": "2026-03-01T15:20:00Z",
      "total_duration_min": 45,
      "phases_completed": 7,
      "retries": 1
    }
  ],
  "alerts": [
    {
      "type": "terminal_failure",
      "workflow_id": "swarm-CM-121-a1",
      "ticket_id": "CM-121",
      "message": "Failed after 3 verify retries",
      "timestamp": "2026-03-01T14:00:00Z"
    }
  ],
  "metrics_summary": {
    "completion_rate_24h": 0.80,
    "avg_workflow_duration_min": 42,
    "retry_rate_24h": 0.25
  }
}
```

`status` is `"healthy"` when Temporal is connected and no workflows are stalled. `"degraded"` when Temporal is disconnected or stalls detected. `"unhealthy"` when active terminal failures exist and no workflows are progressing.

### 4. Alerting via Discord

Terminal failures and high-severity events post to the president's Discord channel (`DISCORD_PRESIDENT_CHANNEL_ID`). Reuses the existing `worldchannel.Client` for Discord API calls.

**Alert triggers:**
| Trigger | Severity | Message |
|---------|----------|---------|
| Terminal failure | Critical | `🔴 Swarm: CM-123 failed — {reason}. Retrospective: {doc_path}` |
| Crash recovery (SessionEnd without Stop) | Warning | `⚠️ Swarm: CM-123 session crashed during {phase}. Auto-recovering.` |
| Stall detected (>stall_minutes) | Warning | `⚠️ Swarm: CM-123 stalled in {phase} for {N} minutes.` |
| High retry rate (>50% in last 24h) | Warning | `⚠️ Swarm: Retry rate {N}% in last 24h. Check digest for patterns.` |

Implementation: `harness/internal/swarm/alerts.go` — called from `markFailed`, `DetectStalls`, and a periodic check in the heartbeat. Posts are fire-and-forget via goroutine. Requires `DISCORD_BOT_TOKEN` and `DISCORD_PRESIDENT_CHANNEL_ID` (already in env for the president).

**Alert dedup**: Each alert type + workflow_id is deduped within a 1-hour window to prevent spam during retry loops.

### 5. Per-Session JSONL Logs

Mirror the existing template hook pattern. Each swarm session writes structured JSON lines to a local log file alongside POSTing to the harness. If the harness is down, the local log survives.

```
data/swarm/logs/{ticketID}/{sessionID}.jsonl
```

Each hook event appends a line:
```json
{"ts":"2026-03-01T15:44:30Z","event":"tool_use.post","tool":"Edit","file":"server.go","session":"abc-123","ticket":"CM-123"}
```

The `WriteHooksConfig` function sets `CM_SWARM_LOG_DIR` in the tmux environment. Each HTTP hook handler also appends to the local log before POSTing. The dashboard can serve these logs via `GET /api/swarm/session/{id}/log` for historical inspection.

### 6. Session Inspection

For running sessions, the dashboard shows:
- **Last tool event** from PostToolUse (what file was just edited, what command ran)
- **Context pressure** from PreCompact (boolean flag)
- **Session duration** (computed from SessionStart timestamp)
- **Phase + attempt** from `swarm_sessions` row

For completed sessions:
- **JSONL log** via `GET /api/swarm/session/{id}/log`
- **Handoff document** link
- **RESULT comment** on Linear

This is sufficient for v1. Deeper inspection (files read, current context window usage) would require Claude Code API support that doesn't exist yet.

### 7. Temporal UI Enabled

Remove `--headless` from the Temporal systemd service. The Temporal web UI runs on port 8233 by default. Bind to `127.0.0.1:8233` (same as the server on port 7233) — accessible via SSH tunnel or Tailscale.

```ini
# /etc/systemd/system/temporal.service
ExecStart=/nix/store/.../temporal server start-dev \
    --db-filename /home/deploy/creative-mode/data/temporal.db \
    --ip 127.0.0.1 \
    --ui-ip 127.0.0.1 \
    --ui-port 8233
```

The Temporal UI shows workflow execution history, pending activities, task queue backlog, and retry status. Invaluable for debugging stuck workflows without reading SQLite directly.

### 8. Cost Tracking (Exact Token Count)

Claude Code displays the total token count in the tmux status line (e.g., `125685 tokens`). On the Stop hook, **before killing the tmux session**, capture it:

```bash
tmux capture-pane -t $CM_SWARM_TMUX_SESSION -p | grep -oE '[0-9,]+ tokens' | tail -1
```

This gives the exact session token usage. The Stop hook handler (`session-complete`) receives the token count in the POST body and stores it:

```sql
ALTER TABLE swarm_sessions ADD COLUMN duration_sec INTEGER;
ALTER TABLE swarm_sessions ADD COLUMN total_tokens INTEGER;
```

**Implementation**: The `WriteHooksConfig` function generates a Stop hook script (not just HTTP — a command hook that captures tmux first, then POSTs). The script:

1. Captures the tmux pane output
2. Extracts the token count via regex
3. POSTs to `/api/swarm/session-complete` with `total_tokens` in the body
4. The handler stores it in `swarm_sessions.total_tokens`

```bash
#!/bin/bash
# Swarm Stop hook — capture tokens before session dies
TOKENS=$(tmux capture-pane -t "$CM_SWARM_TMUX_SESSION" -p 2>/dev/null | grep -oE '[0-9,]+ tokens' | tail -1 | tr -d ', tokens')
EVENT_JSON=$(cat)
# Merge token count into the event payload and POST
echo "$EVENT_JSON" | jq --arg t "${TOKENS:-0}" '. + {total_tokens: ($t | tonumber)}' | \
  curl -s -X POST "$CM_HARNESS_URL/api/swarm/session-complete" \
    -H "Content-Type: application/json" \
    -H "X-Hook-Secret: $CM_HOOK_SECRET" \
    -d @-
```

**Note**: This means the Stop hook must be a **command** hook (shell script), not an HTTP hook, since it needs to run `tmux capture-pane` locally before POSTing. Update the `hooks.json` accordingly — Stop uses `type: "command"` while the other 5 hooks remain `type: "http"`.

`GET /api/swarm/metrics` includes exact token totals per period. Dashboard shows per-workflow and cumulative token usage.

## New Files (v5 additions beyond v4)

| File | Phase | ~Lines | Purpose |
|------|-------|--------|---------|
| `harness/internal/swarm/learnings.go` | 2 | 250 | Capture functions, GetLearningContext, createLearning helper |
| `harness/internal/swarm/handoffs.go` | 2 | 200 | WriteHandoff, resolveHandoffPath, handoff template rendering |
| `harness/internal/swarm/hooks.go` | 4 | 200 | WriteHooksConfig, hook endpoint handlers, deny list, context pressure tracker |
| `harness/internal/swarm/hooks_test.go` | 4 | 100 | PreToolUse deny list tests, context pressure state machine |
| `harness/internal/swarm/completion_test.go` | 4 | 100 | CompletionRegistry + StartRegistry tests |
| `harness/internal/swarm/digest.go` | 4 | 150 | GenerateDigest, DecayLearningRelevance, detectActionItems, ContributeSkillImprovement |
| `harness/internal/swarm/metrics.go` | 4 | 150 | Aggregate metrics queries, 60s cache, cost estimation |
| `harness/internal/swarm/alerts.go` | 4 | 100 | Discord alerting for terminal failures, stalls, crash recovery, high retry rates |
| `harness/internal/db/queries/swarm_learnings.sql` | 1 | 80 | CRUD queries for learnings + digests |

## Modified Files (v5 additions beyond v4 modifications)

| File | Phase | Change |
|------|-------|--------|
| `harness/internal/db/migrations/006_swarm_tables.sql` | 1 | Add `swarm_learnings` + `swarm_learning_digests` tables |
| `harness/internal/swarm/enums.go` | 1 | Add `LearningCategory`, `LearningSeverity` types |
| `harness/sqlc.yaml` | 1 | Add 2 overrides for learning columns |
| `harness/internal/swarm/activities.go` | 4 | Call capture functions + write handoffs from RunClaudeSession, ReadTicketQueue, markFailed, markDone |
| `harness/internal/swarm/workflows.go` | 4 | Add DecayLearningRelevance + GenerateDigest to HeartbeatWorkflow |
| `harness/internal/server/swarm_api.go` | 4 | Add learning query + creation endpoints + 6 hook endpoints + metrics + health + session log |
| `harness/internal/president/skills.go` | 4 | Add `swarm-learnings` skill |
| `harness/internal/swarm/activities.go` | 4 | Add structured slog fields (ticket_id, workflow_id, session_id, phase) to all log calls |
| `harness/internal/db/migrations/006_swarm_tables.sql` | 1 | Add `duration_sec` column to `swarm_sessions` |
| `.claude/skills/swarm-conventions/SKILL.md` | 2 | Add handoff + learning context consumption preamble |

## Phased Delivery

### Phase 1: Foundation (v4 + learning schema)
- All v4 Phase 1 deliverables (migration, enums, sqlc, state machine, tests, conventions, setup)
- **Plus**: `swarm_learnings` + `swarm_learning_digests` tables in the same migration
- **Plus**: `LearningCategory`, `LearningSeverity` enums + sqlc overrides + queries
- **Plus**: Transaction isolation via `WithTx` on the store interface
- **Plus**: Create `thoughts/swarm/` directory structure (11 subdirectories) — `WriteHandoff` should also `os.MkdirAll` defensively
- **Plus**: Add `thoughts/swarm/` to CLAUDE.md project structure table
- **Plus**: Add standalone `research` workflow type transition: `research` + success → `done` (not `code_plan` or `project_plan`)

### Phase 2: Core Skills (v4 + learning capture + consumption + handoffs)
- All v4 Phase 2 deliverables (6 core skills)
- **Plus**: `learnings.go` with 4 capture functions + `GetLearningContext`
- **Plus**: `handoffs.go` with `WriteHandoff`, `resolveHandoffPath`, handoff template
- **Plus**: Handoff + learning context consumption preamble in `swarm-conventions/SKILL.md`
- **Plus**: `POST /api/swarm/learnings` endpoint for manual learning creation
- **Plus**: Every skill writes a handoff document as its last act before exiting

### Phase 3: Project & Support Skills (v4)
- All v4 Phase 3 deliverables (6 project/support skills)
- No learning-specific additions (project skills use the same conventions)

### Phase 4: Temporal + Hooks + Dashboard + Learning Loop + Handoff Wiring
- All v4 Phase 4 deliverables (Temporal, completion hooks, dashboard, API)
- **Plus**: Capture function calls wired into `ReadTicketQueue`, `markFailed`, `markDone`
- **Plus**: `RunClaudeSession` resolves handoff path + passes via `CM_SWARM_HANDOFF_PATH` env var
- **Plus**: `DecayLearningRelevance` + `GenerateDigest` in HeartbeatWorkflow
- **Plus**: Learning query endpoints (`GET /api/swarm/learnings`, digest)
- **Plus**: `digest.go` with pattern detection + `ContributeSkillImprovement`
- **Plus**: President `swarm-learnings` skill
- **Plus**: Dashboard learnings section (recent learnings, digest summary)
- **Plus**: Dashboard handoff timeline (handoff chain visualization per workflow)
- **Plus**: `hooks.go` — `WriteHooksConfig` (generates `/tmp/swarm-hooks-{sessionID}/hooks.json`), 6 hook endpoint handlers, PreToolUse deny list, context pressure tracker
- **Plus**: `hooks_test.go` — deny list pattern matching, context pressure state transitions
- **Plus**: `completion_test.go` — CompletionRegistry + StartRegistry tests
- **Plus**: `RunClaudeSession` updated: write hooks config → spawn with `CLAUDE_HOOKS_DIR` → wait for SessionStart (30s) → main loop → SessionEnd fallback
- **Plus**: 6 hook routes registered in `swarm_api.go`
- **Plus**: Dashboard live tool activity via PostToolUse → EventBus → SSE
- **Plus**: `metrics.go` — aggregate metrics with 60s cache, cost estimation from session duration
- **Plus**: `alerts.go` — Discord notifications for terminal failures, stalls, crash recovery, high retry rate
- **Plus**: Structured logging with correlation IDs (ticket_id, workflow_id, session_id, phase) on all swarm slog calls
- **Plus**: Per-session JSONL logs at `data/swarm/logs/{ticketID}/{sessionID}.jsonl`
- **Plus**: `/api/swarm/health` with full schema (capacity, Temporal status, active workflows, alerts, metrics summary)
- **Plus**: `/api/swarm/metrics?period=24h` (completion rate, phase durations, retry rates, cost estimate)
- **Plus**: `/api/swarm/session/{id}/log` for historical JSONL inspection
- **Plus**: Temporal UI enabled (remove `--headless`, bind UI to `127.0.0.1:8233`)

### Phase 5: Integration Testing & Documentation
- All v4 Phase 5 deliverables
- **Plus**: Learning capture verification at each state machine transition
- **Plus**: Handoff creation verification at each session boundary
- **Plus**: Handoff chain continuity test (research→plan→review→implement→verify→PR, verify each reads previous handoff)
- **Plus**: Context window limit handoff test (simulate mid-phase handoff, verify continuation)
- **Plus**: Digest generation test (verify after simulated 24h)
- **Plus**: End-to-end: verify failure → learning captured → handoff written → retry reads handoff + learning context → success
- **Plus**: Hook integration tests:
  - SessionStart: verify `RunClaudeSession` fails fast (infra_failure) if no SessionStart within 30s
  - PreToolUse: verify denied commands return exit 2 and are blocked
  - PostToolUse: verify tool events appear on dashboard SSE stream
  - PreCompact: verify context_pressure flag set after 2nd compact, skill can read it
  - SessionEnd: verify crash recovery — kill tmux without Stop firing, verify SessionEnd triggers completion
  - Full lifecycle: SessionStart → PostToolUse events → PreCompact → Stop → SessionEnd (verify ordering and dedup)

## Verification

### Schema & State Machine
1. **Schema**: Migration runs, 9 tables exist (7 from v4 + 2 learning tables), default config row
2. **State machine tests**: `go test ./internal/swarm/...` — all transitions + learning capture

### Learning Capture
3. **Plan issue**: Simulate plan_review revise → verify `swarm_learnings` row with category=plan_issue
4. **Code bug**: Simulate verify logic_failure → verify row with category=code_bug
5. **Terminal failure**: Simulate terminal failure → verify row with category=post_mortem + `thoughts/swarm/retrospectives/` doc
6. **Success pattern**: Simulate workflow done → verify row with category=pattern
7. **Context injection**: Verify `GetLearningContext` returns relevant learnings, increments `referenced_count`
8. **Relevance decay**: Verify old learnings decay, critical learnings decay slower, referenced learnings get boosted

### Handoff System
9. **Phase completion handoff**: Every session writes a handoff to `thoughts/swarm/handoffs-{phase}/` on exit
10. **BLUF RESULT comment**: RESULT comment on Linear includes handoff path link
11. **Handoff consumption**: Next session reads `CM_SWARM_HANDOFF_PATH` at startup, gets previous context
12. **Handoff resolution**: `resolveHandoffPath` correctly globs across all `handoffs-*` directories and returns most recent handoff for the ticket
13. **Context limit handoff**: Simulate mid-phase context window limit → handoff written → new session continues same phase
14. **Retry handoff chain**: plan_review revise → handoff in `handoffs-plan-reviews/` → next code_plan reads it
15. **Verify retry chain**: verify logic_failure → handoff in `handoffs-code-reviews/` → next implement reads it
16. **Full handoff chain**: research→plan→review→implement→verify→PR, each session reads previous handoff

### Learning Loop & Integration
17. **Digest**: Verify digest generated after 24h, action items detected from repeated bugs
18. **API**: `GET /api/swarm/learnings?category=code_bug` returns filtered results
19. **E2E**: Full workflow with intentional verify failure → learning captured → handoff written → retry reads both → success
20. **President**: Verify `swarm-learnings` skill queries learnings API correctly

### Observability
21. **Metrics**: `GET /api/swarm/metrics?period=24h` returns completion rate, phase durations, retry rates, cost estimate
22. **Health**: `GET /api/swarm/health` returns capacity, Temporal status, active workflows with last tool event, alerts
23. **Structured logs**: Every swarm log line includes ticket_id + workflow_id + session_id + phase — verify with `journalctl | grep CM-123`
24. **JSONL logs**: Session JSONL written to `data/swarm/logs/` and retrievable via `GET /api/swarm/session/{id}/log`
25. **Alerts**: Terminal failure → Discord notification in president channel within 30s
26. **Alert dedup**: Same alert type + workflow_id doesn't spam within 1-hour window
27. **Temporal UI**: Accessible at `127.0.0.1:8233`, shows workflow history and task queue backlog
28. **Cost tracking**: `total_tokens` captured from tmux status line on Stop hook, `duration_sec` computed on completion, metrics endpoint includes exact token totals

## Resolved Questions (from v4 reviews)

1. **~~`findAvailableSlot`~~** → **Use UUIDs, not slot indices.** Session IDs are UUIDs. No slot allocation needed. `max_sessions` is enforced by counting active sessions (`SELECT COUNT(*) FROM swarm_sessions WHERE completed_at IS NULL`). If count >= max, skip spawn. The `agent_index` column in `swarm_sessions` is removed — it was only needed for the slot model. Tmux session names use `cm-swarm-{ticketID}-a{attempt}` (no index).

2. **RESULT comment timing** → **Verified by design + defensive retry.** `linear-cli` is synchronous — the RESULT comment is written before Claude exits, before Stop fires. The `session-complete` handler includes one defensive retry (wait 2s, re-read) as a safety net. Documented as a tested guarantee.

3. **`completion_test.go`** → **Added to Phase 4 file inventory.** Tests CompletionRegistry + StartRegistry.

4. **Temporal `start-dev` in production** → **Bind to `127.0.0.1`, UI enabled.** Add `--ip 127.0.0.1 --ui-ip 127.0.0.1 --ui-port 8233` to the systemd `ExecStart`. UI accessible via SSH tunnel or Tailscale. Document as known v1 limitation: single-node, no TLS, no auth. Acceptable for single-VPS with <10 concurrent workflows.

## Open Questions (deferred to implementation)

1. **Temporal recovery path** — Document explicit scenarios: (a) harness restart mid-session, (b) Temporal restart mid-workflow, (c) both restart simultaneously. Trace event sequence for each and verify convergence. Phase 5 deliverable.

2. **`thoughts/swarm/` growth** — Files are committed to git (like `thoughts/CoreyCole/`). They will grow over time. Consider periodic archival (move old handoffs to `thoughts/swarm/archive/`). Not blocking for v1.
