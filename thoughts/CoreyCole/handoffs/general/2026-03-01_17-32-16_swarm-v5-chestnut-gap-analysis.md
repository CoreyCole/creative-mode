---
date: 2026-03-01T17:32:16-08:00
researcher: CoreyCole
git_commit: 09d4c4126371bd322c7d8a14e3e3a365573dfd98
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm v5 Plan + Chestnut Flowchart Gap Analysis"
tags: [review, swarm, gap-analysis, chestnut, v5-plan, simplify]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Implementation Review vs v5 Plan + Chestnut Flowchart

## Task(s)

1. **Full gap analysis: implementation vs v5 plan + Chestnut flowchart** — Complete. Systematically compared every feature in the v5 plan and every section of the Chestnut Agent Primitives HTML flowchart against the actual implementation. Produced a detailed review document with coverage scores (~85% v5, ~90% Chestnut).

2. **Save Chestnut flowchart** — Complete. Saved the HTML flowchart (provided inline by user) to `thoughts/swarm/chestnut-agent-primitives-flowchart.html`.

3. **`/simplify` review of all uncommitted changes** — Complete. Ran 3 parallel review agents (code reuse, code quality, efficiency). Found 1 issue: `GateAction` enum missing `Valid()` method. Fixed.

4. **Commit all changes** — Not done. User has not yet requested a commit.

Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-03-01_17-04-39_swarm-review-and-docs.md`

## Critical References

1. **Gap analysis document**: `thoughts/CoreyCole/reviews/2026-03-02_swarm-implementation-vs-v5-and-chestnut-review.md` — The primary output. Contains full feature-by-feature comparison, coverage scores, priority action items, and simplification opportunities.
2. **v5 plan**: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — The authoritative implementation spec for the swarm system.
3. **Chestnut flowchart**: `thoughts/swarm/chestnut-agent-primitives-flowchart.html` — The target architecture visualization with 7 primitives, 3 workflow types, and design principles.

## Recent changes

All changes are uncommitted on `feature/agent-swarm` (building on the previous session's uncommitted changes):

- `harness/internal/swarm/enums.go:171-179` — Added `Valid()` method to `GateAction` enum (was the only enum in the file without one)

From previous session (still uncommitted):
- `harness/internal/db/queries/swarm.sql:183-184` — Bug fix: removed `review_feedback = NULL` from `ClearSwarmWorkflowGate`
- `harness/internal/swarmorch/manager.go` — `requireAwaitingReview()`, `completeWorkflow()` helpers, reordered gate operations, string→enum constants
- `harness/internal/server/swarm_dashboard.go` — `fetchWorkflowDetailData()`, `reviewerFromContext()` helpers
- `harness/internal/swarm/enums.go:163-169` — `GateAction` typed enum
- `harness/views/swarm/dashboard.templ:508` — Uses `GateActionApprove` constant
- `CLAUDE.md` — Swarm orchestrator section, updated agent hierarchy
- `harness/CLAUDE.md` — 10 new sections of swarm documentation

## Learnings

1. **Implementation is ~85% complete against v5 plan**. Core orchestration is solid. The gaps are mostly in the "learning feedback loop" — president integration, automated skill improvement PRs, and cost tracking.

2. **Significant Chestnut routing divergence**: The flowchart specifies that human review rejection should route "Back to Plan Revision" (re-enter the plan loop). The implementation routes to `PhaseImplement` (re-implement only). This means a human reviewer who wants architectural changes can only get re-implementation, not re-planning. Fix: add a `revision_target` parameter to `RejectGate` allowing choice between `implement` and `code_plan`.

3. **Token cost tracking is wired but not flowing**: The `swarm_sessions.total_tokens` column exists in the schema, but the Stop hook is HTTP-only (not a shell command). v5 specifies the Stop hook should be a shell script that runs `tmux capture-pane` to extract the token count before POSTing. This is the only hook that needs to be a command type.

4. **Relevance decay is simplified**: v5 spec has `newScore = min((decayFactor * severityFactor) + referenceBoost, 1.0)` with age-based decay. Implementation uses flat `score * 0.95`. Simpler but critical learnings decay at the same rate as info learnings.

5. **No retrospective files**: `captureTerminalFailure` writes a DB learning record only, not the `thoughts/swarm/retrospectives/` markdown file specified in v5.

6. **`/simplify` on this codebase found only 1 issue** (the missing `Valid()` method). The previous session's simplification pass was thorough — the code is clean.

7. **Chestnut flowchart sections 6+7 (orchestration heartbeats)** are the least implemented area. The maintenance loop does stall detection and learning decay, but doesn't autonomously reprompt stalled agents or handle cross-project dependencies.

## Artifacts

- `thoughts/CoreyCole/reviews/2026-03-02_swarm-implementation-vs-v5-and-chestnut-review.md` — Full gap analysis document (NEW)
- `thoughts/swarm/chestnut-agent-primitives-flowchart.html` — Saved Chestnut flowchart (NEW)
- `harness/internal/swarm/enums.go:171-179` — Added `GateAction.Valid()` method

## Action Items & Next Steps

The next agent should:

1. **Summarize the implementation and compare it to the Chestnut Agent Primitives flowchart** — The gap analysis is already written at `thoughts/CoreyCole/reviews/2026-03-02_swarm-implementation-vs-v5-and-chestnut-review.md`. The next agent should read this review and produce a concise summary for the user, focusing on:
   - What's working well (the ~90% that aligns)
   - The key routing divergence (human review rejection → implement vs code_plan)
   - The 3 tiers of gaps (high/medium/low priority)
   - Whether the design principles from the flowchart are met

2. **Optionally commit all changes** — 9 files modified, all uncommitted on `feature/agent-swarm`. Includes bug fix, refactors, docs, gap analysis, flowchart save, and `Valid()` fix.

3. **Consider implementing the high-priority gap** — Human review rejection routing. Add `revision_target` parameter to `RejectGate` and a dropdown to the dashboard reject form.

## Other Notes

- The v5 plan version lineage (v1→v5) is at `thoughts/CoreyCole/plans/2026-02-28_*_agent-swarm-primitives*.md`. Implementation phases (1-5) handoffs are at `thoughts/CoreyCole/handoffs/general/2026-02-28_*` through `2026-03-01_*`.
- The Chestnut flowchart was provided inline as HTML by the user. It's now saved at `thoughts/swarm/chestnut-agent-primitives-flowchart.html` but can also be viewed in the browser.
- All tests pass: the previous session confirmed `go test ./internal/swarm/... ./internal/swarmorch/...` and `just check` are clean.
- The `swarm/` vs `swarmorch/` package split is an intentional improvement over the v5 plan (which put everything in `swarm/`). Pure domain types with no I/O in `swarm/`, orchestrator with DB/HTTP in `swarmorch/`.
