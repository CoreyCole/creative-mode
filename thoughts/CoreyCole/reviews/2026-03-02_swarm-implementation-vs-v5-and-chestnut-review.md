# Swarm Implementation Review: v5 Plan + Chestnut Flowchart Gap Analysis

**Date**: 2026-03-02
**Branch**: `feature/agent-swarm`
**Reviewer**: Claude (automated)
**References**:
- v5 Plan: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- Chestnut Flowchart: `thoughts/swarm/chestnut-agent-primitives-flowchart.html`
- Human Gates Plan: `thoughts/CoreyCole/plans/2026-03-02_00-16-57_swarm-human-gates-post-pr-lifecycle.md`

---

## Executive Summary

The swarm implementation is **substantially complete** against the v5 plan (~85% feature coverage). The core orchestration — state machine, skills, hooks, handoffs, learnings, project decomposition, human gates, dashboard, alerts, metrics, and health — all work. The Chestnut flowchart's architecture is faithfully represented with one significant routing divergence.

**Key gaps**: token cost tracking, president integration, automated skill improvement PRs, and the Chestnut-specified human review → plan revision routing. **Key divergences**: simplified relevance decay, no retrospective file writing, HTTP-only hooks (no shell Stop hook for token capture).

---

## 1. v5 Plan Compliance

### Fully Implemented ✅

| v5 Feature | Implementation Location | Notes |
|---|---|---|
| `swarm_learnings` table | `migrations/006_swarm_tables.sql` | Schema matches spec |
| `swarm_learning_digests` table | Same migration | Schema matches spec |
| `LearningCategory` / `LearningSeverity` enums | `swarm/enums.go:189-229` | All 5 categories + 3 severities |
| 4 capture functions | `swarmorch/learnings.go:44-89` | plan_issue, code_bug, terminal_failure, success_pattern |
| Learning context injection | `swarmorch/manager.go:897-916` | Phase + critical + ticket learnings → temp file → env var |
| Reference count boosting | `db/queries/swarm_learnings.sql:29-32` | `+0.1` per reference |
| Auto-archival | `db/queries/swarm_learnings.sql:42-47` | `<0.1` score + `>60 days` |
| Daily digest generation | `swarmorch/digest.go:28-95` | Deterministic pattern detection + action items |
| Handoff system (all 6 trigger points) | `swarm/handoffs.go` + all 9 skills | Directory structure, naming, resolution |
| Handoff consumption via env var | `swarmorch/manager.go:891-895` | `CM_SWARM_HANDOFF_PATH` |
| BLUF RESULT file format | `swarm/result.go:28-91` | `RESULT:`, `Phase:`, `Handoff:`, `Summary:` |
| 6 HTTP hooks | `swarmorch/hooks.go:61-174` | SessionStart, PreToolUse, PostToolUse, PreCompact, Stop, SessionEnd |
| PreToolUse deny list | `swarmorch/hooks.go:22-38` | 4 patterns: cargo, go build, templ, just generate |
| Context pressure (sentinel file) | `swarmorch/hooks.go:185-231` | Threshold=2, writes `/tmp/swarm-context-pressure-{sid}` |
| CompletionRegistry + StartRegistry | `swarmorch/registry.go` | Buffered channels, thread-safe |
| Per-session JSONL logs | `swarmorch/jsonllog.go` | Auto-timestamped, thread-safe |
| Structured logging with correlation IDs | `swarmorch/sessionlog.go` | subsystem, ticket, workflow, session, phase |
| Aggregate metrics with 60s cache | `swarmorch/metrics.go` | Periods: 24h, 7d, 30d, all |
| Discord alerting with dedup | `swarmorch/alerts.go` | 1-hour window, 4 alert types |
| `/api/swarm/health` | `server/swarm_api.go` | Capacity, workflows, stalls, completions |
| `/api/swarm/metrics` | `server/swarm_api.go` | Completion rate, phase durations, retries |
| `POST /api/swarm/learnings` | `server/swarm_api.go:298-354` | Manual learning creation from skills |
| All 9 skills + conventions | `.claude/skills/swarm-*/SKILL.md` | Preamble, handoff, RESULT protocol |
| State machine (all transitions) | `swarm/statemachine.go:43-190` | Code, research, project workflows |
| Retry loops | `statemachine.go:110-139` | MaxPlanRevisions=3, MaxVerifyRetries=3 |
| Ticket classification | `swarm/classify.go:13-32` | YAML footer > project keywords > research keywords > code default |
| Dependency graph + waves | `swarm/dependencies.go` | Kahn's topological sort, ReadyTickets, AllComplete |
| Project child spawning | `swarmorch/project.go:41-205` | Parse plan → create tickets → start wave 1 |
| Human review gates | `swarmorch/manager.go:1218-1466` | enterGate, ApproveGate, RejectGate |
| Gate audit trail | `swarm_gate_reviews` table | Reviewer, action, feedback, timestamp |
| `thoughts/swarm/` directory structure | 11 subdirectories with `.gitkeep` | Matches v5 spec exactly |
| Existing reaper skips `cm-swarm-*` | `claude/claude.go:311-316` | Explicit prefix check |
| `swarm_sessions.total_tokens` column | `migrations/006_swarm_tables.sql:42` | Nullable INTEGER exists |
| Temporal dual-mode | `swarmorch/temporal.go` + `workflows.go` | Goroutine default, Temporal opt-in |
| `thoughts/swarm/` growth strategy | `.gitkeep` files committed | Documents accumulate in git |

### Implemented but Divergent 🟡

| v5 Spec | Implementation | Impact |
|---|---|---|
| **Relevance decay**: `min((1/(1+ageDays/30)) * severityFactor + refBoost, 1.0)` | Simple `score * 0.95` per invocation | Lower: critical learnings decay at same rate as info. Reference boosts are separate (+0.1 per ref). Simpler but less nuanced. |
| **Stop hook as command** (shell script to capture tmux token count) | All 6 hooks are HTTP type | Medium: Token count never captured. `total_tokens` column stays NULL. Cost tracking doesn't work. |
| **JSONL log path**: `data/swarm/logs/{ticketID}/{sessionID}.jsonl` | `data/logs/swarm/logs/{ticketID}/{sessionID}.jsonl` | Cosmetic: Extra `logs/` prefix. Functional. |
| **Learning context file**: `/tmp/swarm-learning-context-{ticketID}.md` | `/tmp/swarm-learning-{sessionID}.txt` | Better: Session ID avoids collisions for concurrent same-ticket runs. |
| **Code location**: learnings/handoffs/hooks in `internal/swarm/` | Moved to `internal/swarmorch/` | Better: Separates pure domain (swarm/) from I/O (swarmorch/). |
| **Transaction isolation**: `WithTx()` in ReadTicketQueue | Manager mutex `m.mu` in advanceWorkflow | Equivalent: Mutex serializes all state transitions. Different mechanism, same guarantee. |
| **Human review rejection → code_plan** (Chestnut flowchart) | Rejection → implement | Significant: See Chestnut section below. |

### Not Implemented ❌

| v5 Feature | Priority | Complexity | Notes |
|---|---|---|---|
| **Token cost tracking** (Stop hook captures tmux status line tokens) | Medium | Medium | Stop hook would need to be command type, not HTTP. Requires shell script to run `tmux capture-pane` before POST. |
| **President `swarm-learnings` skill** | Low | Low | President reads digest, queries learnings API. Foundation exists (APIs are built). |
| **`ContributeSkillImprovement` PR flow** | Low | Medium | Digest generates action items but doesn't trigger automated PRs. President would need to act on digest. |
| **Complex relevance decay formula** | Low | Low | Current simple decay works. Could upgrade later if learning quality degrades. |
| **Retrospective file writing** | Low | Low | `captureTerminalFailure` writes DB record only, not `thoughts/swarm/retrospectives/` file. |
| **High retry rate alert** (>50% in 24h) | Low | Low | Only terminal, crash, stall, and gate alerts exist. |
| **`GET /api/swarm/session/{id}/status`** for context pressure | N/A | N/A | v5 suggested this; implementation chose sentinel file (the "simpler alternative" from v5). Correct choice. |

---

## 2. Chestnut Flowchart Compliance

### Section 1: Task Classification & Routing ✅

| Flowchart | Implementation |
|---|---|
| Idea → Task Classification | `classify.go` keyword + YAML footer |
| Question (1) → Research Agent → Research Doc → Human Review | `WorkflowTypeResearch` → research → done. No human review gate (research just completes). |
| Code Change (2) → Full lifecycle | `WorkflowTypeCode` → 8-phase pipeline |
| Project (3) → Decomposition | `WorkflowTypeProject` → research → project_plan → project_review → project_verify |

**Gap**: Research workflow has no human review gate. Flowchart shows "Human Review" after research doc. Implementation goes directly to done. Low priority — research is informational only.

### Section 2: Code Change Full Lifecycle ✅ (one routing divergence)

| Flowchart Element | Implementation | Status |
|---|---|---|
| Create Linear Ticket | `StartWorkflow()` with ticket ID | ✅ |
| Optional Previous Attempt Reference | `CM_SWARM_PREVIOUS_*` env vars | ✅ |
| Research Doc | `swarm-research` skill → `thoughts/swarm/research/` | ✅ |
| Plan Revision Loop (v1→v2→v3…) | `MaxPlanRevisions=3`, plan_review → code_plan retry | ✅ |
| Plan Review → Agent Review | `swarm-plan-review` skill (8-criterion checklist) | ✅ |
| Plan Decision → Human (configurable) | `GatePlanReview` config flag | ✅ |
| Implement & Verify Loop ("Max's Bolt") | `MaxVerifyRetries=3`, verify → implement retry | ✅ |
| Verification categories (unit, integration, E2E, manual Playwright) | `swarm-code-verify` skill runs all 5 phases | ✅ |
| Pull Request (linked to Linear, Graphite stack) | `swarm-code-pr` skill with Graphite support | ✅ |
| Human Review (always) | `PhaseHumanReview` after PR → `enterGate()` | ✅ |
| **Merge outcome** | `ApproveGate` at human_review → `completeWorkflow()` | ✅ |
| **Full Restart outcome** | Cancel + new workflow with `previousWorkflowID` | ✅ |
| **Revision needed outcome** | See below | 🔴 |

**Significant Routing Divergence**:

The Chestnut flowchart shows three human review outcomes:
1. ✅ Merge → Done
2. 🔴 **Revision needed → "Back to Plan Revision — Same ticket, enter plan loop with review feedback"**
3. ✅ Full Restart → New ticket referencing previous

The implementation routes `RejectGate(PhaseHumanReview)` → `PhaseImplement` (re-implement), NOT back to the plan revision loop. The Chestnut flowchart explicitly says revision should re-enter the plan loop, suggesting the human reviewer wants architectural changes, not just implementation fixes.

**Recommendation**: Add a `revision_target` parameter to `RejectGate` allowing the human to choose:
- `"implement"` — minor implementation fixes (current behavior)
- `"code_plan"` — re-plan from scratch with feedback (flowchart behavior)

This would require `GateRejectionTarget` to accept an optional override, and the dashboard reject form to offer a dropdown.

### Section 3: Project → Plan → Execute ✅

| Flowchart Element | Implementation | Status |
|---|---|---|
| Project → Linear Project | `WorkflowTypeProject` + ticket in Linear | ✅ |
| Break down into Research + Parent Tickets | `swarm-project-plan` decomposes into child tickets | ✅ |
| Dependency Graph (parallel/sequential/independent) | `DependencyGraph`, `ComputeWaves()` | ✅ |
| Graphite Stack Plan | Skill generates stack plan, `CM_SWARM_STACK_PARENT/ORDER` | ✅ |
| Project Verification Checkpoints | `swarm-project-verify` runs milestone checks | ✅ |

### Section 5.1: Project Plan Verification ✅

| Flowchart Element | Implementation | Status |
|---|---|---|
| Agent Review | `swarm-project-review` (7-criterion checklist) | ✅ |
| Agent Summarizes | Review skill posts verdict + rationale | ✅ |
| Human Review (configurable) | `GateProjectReview` config flag | ✅ |
| Changes needed → Revised Dependency Graph | logic_failure → project_plan retry | ✅ |

### Sections 6+7: Orchestration Heartbeats 🟡

| Flowchart Element | Implementation | Status |
|---|---|---|
| **Project Orchestrator (6)**: Check on project, keep it moving | `CheckProjectProgress()` every 2 min, `advanceProject()` | ✅ |
| Comment on tickets | Linear comments on phase transitions | ✅ |
| Create new research/code/parent tickets | `SpawnProjectChildren()` creates and starts child workflows | ✅ |
| Reprompt agents | Stall detection fires Discord alert but doesn't reprompt | 🟡 |
| Slack only if critical | Discord used instead (project convention) | ✅ (equiv) |
| **Lead FDE (7)**: Check on each Project Lead FDE | `StartMaintenance()` checks running projects | 🟡 Partial |
| Reprompt stalled leads | Not implemented — stalls alert humans | ❌ |
| Cross-project dependencies | Not implemented — projects are independent | ❌ |
| Escalate critical blockers | Discord alerts for terminal failures | ✅ |
| **OpenClaw**: Human interface to Lead FDE | President agent exists but no swarm skills | 🟡 |

**Analysis**: The orchestration layer is functional but lacks the "autonomous recovery" aspects. When things stall, it alerts humans rather than autonomously reprompting. This aligns with the current architecture philosophy where the orchestrator is deterministic Go code and intelligence lives in Claude Code sessions — autonomous reprompting would need a mechanism to spawn a "diagnosis" session, which doesn't exist yet.

### Design Principles

| Principle | Status | Notes |
|---|---|---|
| Don't wait/stop for human input | 🟡 | Human gates do wait, but this is intentional. Skills themselves don't wait. |
| Context changes → put tickets back in progress | ✅ | Rejection re-enters pipeline with feedback |
| Agents answer own questions first | ✅ | Research and plan phases are autonomous |
| Defer to humans for company context & trade-offs | ✅ | Human gates at plan review, project review, and PR |
| Linear is source of truth | ✅ | Ticket status synced, comments posted at every transition |
| Slack only for critical human input | ✅ | Discord used (project equiv), only for alerts |

---

## 3. Simplification Opportunities

### Already Simplified (previous session)
- `GateAction` typed enum replacing raw strings
- `requireAwaitingReview()` helper (deduplicates gate preamble)
- `completeWorkflow()` helper (deduplicates ~30 lines)
- `fetchWorkflowDetailData()` helper (deduplicates 5-query fetching)
- `reviewerFromContext()` helper (deduplicates reviewer extraction)

### Remaining Opportunities

1. **`advanceWorkflow` complexity**: At ~230 lines (571-798), this is the most complex method. The gate interception at two levels (configurable before state machine, always-on after) adds cognitive load. Consider extracting `handleTerminalDone()`, `handleTerminalFailed()`, and `handleNonTerminalAdvance()` helper methods to flatten the nesting.

2. **Project verify infinite retry**: `PhaseProjectVerify` has no `maxRetries` limit (`statemachine.go:168-175`). A stuck project_verify will retry indefinitely. Should either add a limit or document this as intentional.

3. **Relevance decay invocation frequency**: v5 explicitly says "skip if <1 hour since last run." The maintenance loop runs decay every hour (via `decayInterval`), but there's no guard against double-runs if the loop timing drifts or Temporal triggers extra heartbeats.

---

## 4. Priority Action Items

### High Priority
1. **Human review rejection routing** — Add option to reject back to `code_plan` (not just `implement`) per Chestnut flowchart

### Medium Priority
2. **Token cost tracking** — Convert Stop hook to command type, capture `tmux capture-pane` tokens
3. **Retrospective file writing** — `captureTerminalFailure` should also write `thoughts/swarm/retrospectives/` markdown

### Low Priority
4. **President swarm-learnings skill** — Enable president to query/act on digest
5. **ContributeSkillImprovement** — Automated PR for skill updates from digest action items
6. **High retry rate alert** — Add the >50% threshold alert
7. **Complex relevance decay** — Upgrade from flat 0.95 to severity-weighted formula
8. **Autonomous reprompting** — Stall detection spawns diagnosis session instead of just alerting

### Won't Do (Intentional Divergences)
- Session context pressure API (`GET /api/swarm/session/{id}/status`) — sentinel file is simpler and works
- Learning context keyed by ticket ID — session ID is better for concurrency
- Code in `internal/swarm/` vs `internal/swarmorch/` — current split is cleaner

---

## 5. Implementation Completeness Score

| Area | v5 Coverage | Chestnut Coverage |
|---|---|---|
| State Machine | 100% | 95% (rejection routing) |
| Skills (9) | 100% | 100% |
| Handoff System | 100% | 100% |
| Learning System | 85% (no retrospective files, simple decay) | N/A |
| Hooks | 90% (no token capture) | N/A |
| Dashboard | 100% | N/A |
| Alerts | 85% (no high retry rate) | N/A |
| Metrics/Health | 100% | N/A |
| Project Workflows | 100% | 100% |
| Human Gates | 95% (rejection target fixed) | 90% (no rejection routing choice) |
| Orchestration Heartbeats | 80% (no reprompt, no cross-project) | 70% |
| President Integration | 0% | 30% (exists but no swarm skills) |
| Cost Tracking | 10% (column exists, no data flow) | N/A |
| **Overall** | **~85%** | **~90%** |
