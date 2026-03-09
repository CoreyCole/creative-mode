---
date: 2026-03-09T00:17:36-07:00
researcher: CoreyCole
git_commit: d326eaa4de0ccc775dd84389ec3ae04ee321dfa9
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Linear Integration + Agent Prompt Improvements Implementation"
tags: [swarm, linear, prompts, agent-design, temporal, implementation]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Linear Integration Implementation (Phases 0-6)

## Task(s)

### Phase 0: Agent prompt improvements — COMPLETED
5 HumanLayer-inspired changes to agent prompts and Go types. All edits applied, Go compiles, JS parses.

### Phase 1: Linear CLI wrapper + label setup — COMPLETED
Created `harness/internal/linear/` Go package wrapping `linear-cli` binary. Created and ran `harness/scripts/setup-linear.sh` — 7 labels now exist in Linear (3 type labels, 4 swarm stage labels).

### Phase 2: Temporal activities for Linear operations — COMPLETED
7 new Linear activities on `SwarmActivities`. All no-op when `LinearClient` is nil or `ticketID` is empty.

### Phase 3: Post-processor agent — NOT STARTED
The `linear-context-processor.js` agent that validates scope claims and creates follow-up tickets was deferred. This is the only remaining phase from the plan.

### Phase 4: Wire into workflows — COMPLETED
Both `ResearchWorkflow` and `CodeChangePlanWorkflow` now call Linear activities at stage boundaries. Research standalone: In Progress + labels at start, comment + artifact link + Done at end. Code change plan: In Progress at start, research comment + planning label swap after research, plan comment + artifact link + In Review at end. All Linear activities are fire-and-forget (non-fatal on error).

### Phase 5: Artifact URL serving — COMPLETED
New route `GET /swarm/artifacts/:id/view` serves artifact files. New `GetSwarmArtifact` sqlc query. `PersistArtifact` now returns artifact ID for URL construction. Path traversal protection included.

### Phase 6: Manager initialization — COMPLETED
Linear client initialized from `LINEAR_TEAM_KEY` env var in `main.go`. `HarnessURL` passed through `SwarmConfig` for artifact link URLs.

Working from: `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` (combined plan) and `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md` (Phase 0 detail).

## Critical References

- `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — the combined plan document with all phases
- `thoughts/CoreyCole/handoffs/general/2026-03-08_23-57-11_swarm-linear-integration-and-prompt-improvements.md` — prior handoff with design decisions and learnings

## Recent changes

### Phase 0 — Prompt improvements
- `harness/agents/research-agent.js` — added CRITICAL documentarian block + tags in output frontmatter
- `harness/agents/research-questions.js` — added CRITICAL factual questions constraint
- `harness/agents/specialist-planner.js` — split `verificationChecks` to `automatedVerification`/`manualVerification`, added tags, scope exclusion instruction
- `harness/agents/plan-synthesizer.js` — preserve verification split, "What We're NOT Doing" instruction, tags
- `harness/agents/research-synthesizer.js` — tags in output frontmatter
- `harness/agents/lib/prompts.js` — added step 4 "Think deeply about underlying patterns" to `selfReflection()`
- `harness/internal/swarmorch/types.go:138-145` — `PlannerOutput` now has `AutomatedVerification`/`ManualVerification` instead of `VerificationChecks`
- `harness/internal/swarmorch/artifact.go:27` — `yamlMultiKeyRe` updated to `automatedVerification|manualVerification`
- `harness/internal/swarmorch/artifact.go:288-291` — validation checks both verification fields

### Phase 1 — Linear CLI wrapper
- `harness/internal/linear/cli.go` — NEW: Go wrapper around `linear-cli` with methods for get/update/comment/attach/create/search/relate
- `harness/internal/linear/types.go` — NEW: Issue, State, Labels, Label, Relation, CreateOpts, SearchResult types
- `harness/scripts/setup-linear.sh` — NEW: creates 7 labels in Linear (already run)

### Phase 2 — Temporal activities
- `harness/internal/swarmorch/activities.go` — added `LinearClient *linear.Client` field, 7 new activity methods (FetchLinearTicket, UpdateLinearStatus, UpdateLinearLabels, AddLinearComment, LinkArtifactToLinear, CreateLinearFollowup, SearchLinearIssues), `HarnessURL` on SwarmConfig, `PersistArtifact` now returns artifact ID

### Phase 4 — Workflow wiring
- `harness/internal/swarmorch/workflows.go` — added `runLinearActivity` helper, `artifactURL` helper, Linear activities at stage boundaries in both workflows
- `harness/internal/server/swarm_api.go` — `workflowStarter` type updated to include `ticketID`, request struct accepts `ticketID`, passes to workflow
- `harness/internal/server/swarm_dashboard.go` — `new_task_ticket` signal, `handleSwarmArtifactView` handler, ticket ID passed to workflow starters

### Phase 5 — Artifact serving
- `harness/internal/db/queries/swarm.sql` — added `GetSwarmArtifact` query
- `harness/internal/db/sqlc/swarm.sql.go` — regenerated
- `harness/internal/server/server.go` — registered `GET /swarm/artifacts/:id/view` route

### Phase 6 — Manager init
- `harness/main.go` — Linear client creation from `LINEAR_TEAM_KEY`, `HarnessURL` from env, passed to `NewSwarmManager`
- `harness/internal/swarmorch/manager.go` — `NewSwarmManager` accepts `*linear.Client`, `StartResearch`/`StartCodePlan` accept `ticketID`
- `harness/views/swarm/dashboard.templ` — `NewTaskTicket` field in signals, ticket input field in form

## Learnings

### `PersistArtifact` return type change
Changed from `error` to `(string, error)` to return the artifact ID for building Linear attachment URLs. All callers in workflows updated to capture the ID.

### `FetchLinearTicket` returns value not pointer
The `nilnil` linter forbids returning `nil, nil` for pointer return types. Changed to return `linear.Issue` by value instead of `*linear.Issue`.

### `runLinearActivity` must not be generic
Temporal's `workflow.ExecuteActivity` uses reflection to dispatch, so a generic wrapper with `[T any]` fails type inference. The helper uses `any` for `activityFn` and variadic `args`.

### Linear labels are set via `-l` flag (not `--labels`)
`linear-cli issues update CRE-15 -l "type:research" -l "swarm:research"` — the `-l` flag can be repeated. Our `UpdateLabels` preallocates the args slice.

### Labels created in Linear
7 labels now exist: `type:research`, `type:code-change`, `type:project`, `swarm:research`, `swarm:planning`, `swarm:implementing`, `swarm:verifying`. Setup script is idempotent (errors on duplicates are swallowed with `|| true`).

## Artifacts

- `harness/internal/linear/cli.go` — Linear CLI wrapper
- `harness/internal/linear/types.go` — Linear types
- `harness/scripts/setup-linear.sh` — label creation script
- `harness/internal/db/queries/swarm.sql` — added GetSwarmArtifact query
- `harness/views/swarm/dashboard.templ` — ticket field in form
- All modified files listed in "Recent changes" above

## Action Items & Next Steps

1. **All changes are unstaged** — commit when ready. The working tree has modifications from this session plus prior Phase 1-3 work on the `feat/agent-primitives` branch.

2. **Implement Phase 3 (post-processor agent)** — `harness/agents/linear-context-processor.js` + types + validation. This agent reads artifacts and ticket data, validates scope claims, writes structured comments, and recommends follow-up tickets. See plan Phase 3 section for exact design.

3. **Manual testing** — Start a research task WITH `ticketID` and verify Linear status/label/comment lifecycle. Start one WITHOUT `ticketID` to verify no-op behavior.

4. **Fix stale MEMORY.md entries** — Remove/update claims about `IsLinearIdentifier`, gate endpoints, `swarm_config` table that don't exist on this branch (noted in prior handoff).

5. **Artifact URL serving needs `HARNESS_URL`** — Ensure the env var is set so Linear attachment links are clickable absolute URLs (currently falls back to relative paths).

## Other Notes

### Environment variables needed
- `LINEAR_TEAM_KEY=CRE` — already in `.env`
- `LINEAR_API_KEY` — already in `.env`
- `HARNESS_URL` — needed for absolute artifact URLs in Linear attachments
- `CM_SWARM_TEMPORAL=true` — already in `.env`

### linear-cli workspace
Already configured: `~/.config/linear-cli/config.toml` has workspace `creative-mode`. No setup needed for future sessions.

### Design decision: no Linear ops in child workflows
When `ResearchWorkflow` runs as a child of `CodeChangePlanWorkflow`, all Linear activities are skipped (`isChild` guard). The parent workflow handles Linear lifecycle for the full code change plan flow.

### Design decision: fire-and-forget Linear activities
All Linear calls go through `runLinearActivity()` which logs warnings but never fails the workflow. This ensures Linear API issues don't block agent work.
