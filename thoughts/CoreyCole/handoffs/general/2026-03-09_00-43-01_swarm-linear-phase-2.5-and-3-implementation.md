---
date: 2026-03-09T00:43:01-07:00
researcher: CoreyCole
git_commit: 061dd6f201de529eb8a0b45993c20951883991c4
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Linear Integration — Phase 2.5 + Phase 3 Implementation"
tags: [swarm, linear, temporal, implementation]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Linear Integration — Phase 2.5 + Phase 3 Implementation

## Task(s)

### Phase 2.5: Review fixes — COMPLETED
All 4 targeted fixes from the staff eng review implemented and passing `just check`.

### Phase 3: Post-processor agent + ticket context + full wiring — COMPLETED
Linear context processor agent created, Go types added, activity wired, ticket context injection into all agent inputs, post-processor replacing hardcoded comments, follow-up ticket creation with dedup. All compiling and passing lint.

### Manual testing — NOT STARTED
No runtime testing has been done. All changes are unstaged on `feat/agent-primitives`.

Working from: `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md`

## Critical References

- `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — the combined plan with Phase 2.5 + Phase 3
- `thoughts/CoreyCole/reviews/2026-03-09_00-22-17_linear-integration-plan_review.md` — staff eng review

## Recent changes

### Phase 2.5 changes
- `harness/internal/swarmorch/workflows.go:223-228` — `artifactURL` is now a pure function `(harnessURL, artifactID string)` instead of dereferencing nil `*SwarmActivities`
- `harness/internal/swarmorch/workflows.go:31-37,40-46` — `HarnessURL` and `TicketContext` added to both workflow input structs
- `harness/internal/swarmorch/manager.go:30-36` — `harnessURL` field on `SwarmManager`, threaded into workflow inputs at lines 108,145
- `harness/agents/research-agent.js:32-34` — `tags:` block removed from output format
- `harness/agents/research-synthesizer.js:23-25` — `tags:` block and guidance removed
- `harness/agents/specialist-planner.js:25-27` — `tags:` block and guidance removed
- `harness/agents/plan-synthesizer.js:24-26` — `tags:` block and guidance removed
- `harness/internal/server/swarm_dashboard.go:485` — `filepath.Separator` appended to `absRoot` in path traversal check
- `harness/main.go:529-539` — `exec.LookPath` + `LINEAR_CLI` env var replaces hardcoded path

### Phase 3 changes
- `harness/agents/linear-context-processor.js` — NEW: post-processor agent (validates scope, produces structured comments + follow-up recommendations)
- `harness/internal/swarmorch/types.go:108-131` — NEW: `LinearContextInput`, `LinearContextOutput`, `FollowupTicket` types
- `harness/internal/swarmorch/types.go` — `TicketContext string` field added to `GenerateQuestionsInput`, `ResearchAgentInput`, `ClassifyInput`, `SpecialistInput`
- `harness/internal/swarmorch/activities.go:503-522` — NEW: `RunLinearContextProcessor` activity using `runAgentActivity` pattern
- `harness/internal/swarmorch/artifact.go:296-313` — NEW: `validateLinearContextOutput` validation function
- `harness/internal/swarmorch/artifact.go:29-30` — Added `comment|followups|title|description|relation` to `yamlMultiKeyRe`
- `harness/internal/swarmorch/workflows.go:248-292` — NEW: `fetchTicketContext` and `formatTicketContext` helpers
- `harness/internal/swarmorch/workflows.go:297-392` — NEW: `runPostProcessor` helper (runs agent, posts comment, creates follow-ups with dedup)
- `harness/internal/swarmorch/workflows.go` — Ticket context fetched at workflow start and injected into `GenerateQuestionsInput`, `ResearchAgentInput`, `ClassifyInput`, `SpecialistInput`
- `harness/internal/swarmorch/workflows.go` — Hardcoded `AddLinearComment` calls replaced with `runPostProcessor` in both workflows (3 call sites: standalone research, code plan research stage, code plan plan stage)

## Learnings

### `runPostProcessor` is non-fatal by design
The post-processor uses `runLinearActivity` for comment posting (fire-and-forget) and has a `fallbackComment` field that gets used if the post-processor agent itself fails. This means a post-processor failure won't break the workflow — it just falls back to the old hardcoded comment template.

### `TicketContext` threading pattern
Rather than fetching the ticket inside `runResearchSteps`, the ticket context is fetched once at the workflow level and passed down via `ResearchWorkflowInput.TicketContext`. For child workflows in `CodeChangePlanWorkflow`, the parent passes its fetched context to the child via the input struct. This avoids redundant API calls.

### `yamlMultiKeyRe` needs updating for new agent outputs
The `linear-context-processor.js` agent outputs `comment`, `followups`, `title`, `description`, `relation` fields. These were added to `yamlMultiKeyRe` so the multi-key YAML line fixer handles them correctly.

### Lint requirements
- `errchkjson`: `json.Marshal` return errors must be checked (not `_` ignored)
- `prealloc`: Slices that will be appended to in a for-range must be preallocated with `make([]T, 0, len(source))`

## Artifacts

- `harness/agents/linear-context-processor.js` — new post-processor agent
- `harness/internal/swarmorch/types.go` — LinearContextInput/Output/FollowupTicket types + TicketContext on agent inputs
- `harness/internal/swarmorch/activities.go` — RunLinearContextProcessor activity
- `harness/internal/swarmorch/artifact.go` — validateLinearContextOutput + yamlMultiKeyRe update
- `harness/internal/swarmorch/workflows.go` — fetchTicketContext, formatTicketContext, runPostProcessor, full wiring

## Action Items & Next Steps

1. **Commit all changes** — 12 files (11 modified + 1 new), 308 insertions, 86 deletions. All passing `just check`.

2. **Manual testing with Linear ticket** — Start a research task with `ticket_id` set to a real CRE-* ticket. Verify:
   - Ticket context appears in agent inputs (check agent JSONL logs for `ticketContext` field)
   - Post-processor runs and posts structured comment to Linear
   - Follow-up tickets created with correct relations (if post-processor recommends any)
   - Artifact URL links work in Linear

3. **Manual testing without Linear ticket** — Start a task with no `ticket_id`. Verify all Linear activities no-op gracefully, no errors.

4. **Push branch** — `feat/agent-primitives` for review.

5. **Future work** — Implementation/verification loops (the "Max's Bolt" loop from agent primitives flowchart) are not part of this Linear integration and are deferred.

## Other Notes

### Changes are unstaged
All 12 files are modified but not staged or committed. The previous commit is `d23b4e7` (Phases 0-6 work). These Phase 2.5 + Phase 3 changes should be committed as a separate commit.

### The `linear` package import in workflows.go
This was added for `linear.Issue` and `linear.SearchResult` types used by `fetchTicketContext` and `runPostProcessor`. The import is clean — no circular dependency.

### `encoding/json` import in workflows.go
Added for `json.Marshal` in `runPostProcessor` to serialize the fresh ticket data for the post-processor agent input. This is a side-effect-free operation (marshaling a struct to JSON bytes), so it's safe in Temporal workflow code.

### Post-processor agent script location
`harness/agents/linear-context-processor.js` follows the same `runAgent` pattern as all other agents. Uses `withFileTools: true` and `withSearchContext: true` so it can read the codebase if needed for scope validation.
