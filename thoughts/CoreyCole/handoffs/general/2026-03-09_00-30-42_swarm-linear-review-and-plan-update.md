---
date: 2026-03-09T00:30:42-07:00
researcher: CoreyCole
git_commit: d23b4e73e36134887666c4418def657f13a38a3a
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Linear Integration — Review Fixes + Phase 3 Plan Update"
tags: [swarm, linear, temporal, review, implementation]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Linear Integration — Review + Plan Update

## Task(s)

### Commit Phases 0-6 — COMPLETED
All unstaged work from previous sessions (Phases 0, 1, 2, 4, 5, 6) committed as `d23b4e7` on `feat/agent-primitives`. 60 files, 3980 insertions, 940 deletions. Go compiles via `just vps-build`.

### Staff eng review of Phases 0-6 — COMPLETED
Ran 6 parallel codebase-analyzer subagents to independently verify the implementation. Found 1 critical bug, 4 concerns, 3 questions. Review document written.

### Update plan with Phase 2.5 + revised Phase 3 — COMPLETED
Inserted Phase 2.5 (review fixes) and consolidated Phase 3 (post-processor + ticket context + full wiring) into the combined plan. Old Phases 4-6 removed (already done or absorbed).

### Implementation of Phase 2.5 and Phase 3 — NOT STARTED

Working from: `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md`

## Critical References

- `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — the updated combined plan with Phase 2.5 + revised Phase 3
- `thoughts/CoreyCole/reviews/2026-03-09_00-22-17_linear-integration-plan_review.md` — staff eng review with all findings

## Recent changes

No code changes this session. The only commit was packaging prior sessions' work:

- `d23b4e7` — committed all Phases 0-6 work (agent prompt improvements, Linear CLI wrapper, Temporal activities, workflow wiring, artifact serving, manager init, type-safe enums, dashboard overhaul)

## Learnings

### Critical bug: `artifactURL` nil pointer dereference
`workflows.go:221-229` — `artifactURL(a, artifactID)` accesses `a.config.HarnessURL` where `a` is the nil `*SwarmActivities` pointer (standard Temporal activity-reference pattern). Called at `workflows.go:525` and `workflows.go:784`. Will panic at runtime whenever `PersistArtifact` returns a non-empty ID. Fix: add `HarnessURL` to workflow input structs and make `artifactURL` a pure function taking `(harnessURL, artifactID string)`.

### Three Linear activities have zero callers
`FetchLinearTicket` (`activities.go:440`), `CreateLinearFollowup` (`activities.go:504`), `SearchLinearIssues` (`activities.go:534`) are registered but never called from any workflow. These are needed for Phase 3 (ticket context injection, follow-up creation).

### Agent `tags` field is orphaned
All agents output `tags:` in YAML frontmatter but no Go struct has a `Tags` field. Data is silently dropped by the lenient JSON fallback in `unmarshalArtifact`. Also missing from `yamlMultiKeyRe`. Decision: remove tags from prompts (no consumer exists yet).

### Path traversal check inconsistency
`swarm_dashboard.go:483` uses `strings.HasPrefix(absPath, absRoot)` without appending a path separator — differs from `handleWASMArtifacts` in the same file which does append `string(os.PathSeparator)`. Not exploitable (DB-sourced paths) but should be fixed for consistency.

### Hardcoded `linear-cli` path
`main.go:529` hardcodes `/home/deploy/.cargo/bin/linear-cli` instead of using `exec.LookPath` or env var. Setup script uses `${LINEAR_CLI:-linear-cli}`. Should match.

### Temporal `var a *SwarmActivities` pattern
The nil pointer `a` is safe for Temporal method-value references (`a.SomeActivity` passed to `workflow.ExecuteActivity`) because Temporal resolves by registered name. But direct field access like `a.config.HarnessURL` panics. Only `artifactURL` has this bug — all other uses are activity references.

## Artifacts

- `thoughts/CoreyCole/reviews/2026-03-09_00-22-17_linear-integration-plan_review.md` — staff eng review
- `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — updated plan with Phase 2.5 + revised Phase 3

## Action Items & Next Steps

1. **Implement Phase 2.5** — 4 targeted fixes:
   - **2.5a**: Fix `artifactURL` nil deref — add `HarnessURL` to `ResearchWorkflowInput` and `CodeChangePlanWorkflowInput`, make `artifactURL` a pure function, thread from `SwarmManager` and child workflow calls
   - **2.5b**: Remove orphaned `tags` from 4 agent prompts (research-agent.js, research-synthesizer.js, specialist-planner.js, plan-synthesizer.js)
   - **2.5c**: Add path separator to artifact handler traversal check in `swarm_dashboard.go`
   - **2.5d**: Replace hardcoded `linear-cli` path with `exec.LookPath` + `LINEAR_CLI` env var in `main.go`

2. **Implement Phase 3** — Post-processor agent + full Linear lifecycle:
   - Create `harness/agents/linear-context-processor.js`
   - Add Go types: `LinearContextInput`, `LinearContextOutput`, `FollowupTicket`
   - Add `RunLinearContextProcessor` activity + `validateLinearContextOutput`
   - Add `TicketContext` field to agent input types
   - Wire `FetchLinearTicket` at workflow start for context injection
   - Wire post-processor after research/plan completion
   - Wire `SearchLinearIssues` + `CreateLinearFollowup` for follow-up creation
   - Replace hardcoded comment templates with post-processor output

3. **Manual testing** — with and without `ticketID`, verify full Linear lifecycle

4. **Push branch** when ready for review

## Other Notes

### Phase 2.5 is small and mechanical
All 4 fixes are straightforward — no design decisions needed. ~30 min total. Should be committed separately before Phase 3.

### Phase 3 is the last implementation phase
After Phase 3, the Linear integration is feature-complete for research and code_change_plan primitives. Implementation/verification loops (the "Max's Bolt" loop from the agent primitives flowchart) are future work.

### `SwarmConfig` already has `HarnessURL`
The field exists at `activities.go:34` and is set from `HARNESS_URL` env at `main.go:536`. The fix is only about threading it into workflow inputs so workflow code can access it without dereferencing the nil activity struct.

### `SwarmManager` already has `config SwarmConfig`
`manager.go:44` — `config` field is accessible in `StartResearch` and `StartCodePlan`, so adding `HarnessURL: m.config.HarnessURL` to workflow inputs is a one-line change per starter method.
