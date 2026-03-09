---
date: 2026-03-08T23:57:11-07:00
researcher: CoreyCole
git_commit: e0d3167b36d876bc71654e10bbb34bae5b58c0b7
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Linear Integration + Agent Prompt Improvements"
tags: [swarm, linear, prompts, agent-design, humanlayer, temporal, implementation]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Linear Integration + Agent Prompt Improvements

## Task(s)

### Combined plan creation — COMPLETED
Created a unified implementation plan combining two efforts:

1. **Agent prompt improvements (Phase 0)** — 5 HumanLayer-inspired changes to agent prompts and Go types. Code snippets ready, no dependencies. Originally planned in a prior session.
2. **Linear integration (Phases 1-6)** — Full Linear tracking for swarm Temporal workflows: status pushes, label management, structured comments, artifact linking, follow-up ticket creation via post-processor agent.

Both plans are finalized in a single document. No code was written in this session — purely design and planning.

### Implementation — NOT YET STARTED
All 7 phases (0-6) are ready to implement per the combined plan.

## Critical References

- **Combined implementation plan**: `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — the complete plan with Phase 0 (prompts) + Phases 1-6 (Linear)
- **Prompt improvements detail plan**: `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md` — exact code snippets for Phase 0 changes
- **Agent primitives flowchart**: `thoughts/swarm/agent-primitives-flowchart.html` — the full vision for task types, lifecycle, and orchestration that informed the Linear state design

## Recent changes

No code changes in this session. The working tree has unstaged modifications from prior Phase 1-3 work (bug fixes, context injection, prompt coherence) on the `feat/agent-primitives` branch.

## Learnings

### No Linear integration exists in the current codebase
Despite MEMORY.md claiming `IsLinearIdentifier()` guards, Linear comment posting, and `swarm_config` with `gateProjectReview` — none of this exists on the `feat/agent-primitives` branch. `LINEAR_API_KEY` and `LINEAR_TEAM_KEY=CRE` are in `.env` but no code reads them. The `linear_issue_id` column on `swarm_tasks` is always NULL. The `LinearIssueID` field on workflow inputs is always empty string. MEMORY.md is stale on these points.

### Gates don't exist either
MEMORY.md mentions gate approve/reject endpoints (`POST /api/swarm/gate/<id>/approve`) but these routes are not registered. All workflow stages chain sequentially with no pause points. The plan was designed to work without gates.

### Labels > custom workflow states
We decided against creating custom Linear workflow states (like "Research In Progress", "Plan In Progress") because as the swarm handles more task types (research, code changes, projects, implementation, verification loops), per-stage states would become unwieldy. Instead, keep the default states (Backlog, Todo, In Progress, In Review, Done, Canceled, Duplicate) and use **labels** to track what the swarm is currently doing within "In Progress": `swarm:research`, `swarm:planning`, `swarm:implementing`, `swarm:verifying`.

### Research tickets vs code change tickets — relations, not types
Standalone research tickets go `Todo → In Progress → Done`. Code change tickets embed their own research as a child workflow. When a code change needs deeper research, it uses `blocked-by` relations to separate research tickets. The post-processor creates follow-up research tickets with `blocked-by` (prerequisite) or `relates-to` (tangential) relations. This scales without special ticket type logic.

### Comments are a knowledge ledger, not status updates
Linear comments should contain: key context/decisions, mistakes and learnings, validated out-of-scope items. NOT "research started" / "plan complete" — that information belongs in status/label changes.

### linear-cli is the integration layer
The `linear-cli` Rust binary (v0.3.14) is already installed at `/home/deploy/.cargo/bin/linear-cli`. Workspace `creative-mode` was configured during this session (`linear-cli config workspace-add creative-mode $LINEAR_API_KEY`). It covers all needed operations: `i get`, `i update -s`, `i update -l`, `cm create`, `att create`, `i create`, `s issues`, `rel add`. No Go Linear SDK needed — activities shell out via `exec.CommandContext`.

### Current CRE team Linear states (discovered this session)
Backlog (backlog), Todo (unstarted), In Progress (started), In Review (started), Done (completed), Canceled (canceled), Duplicate (canceled). No labels exist yet. These defaults are kept as-is per the labels-over-states design decision.

### selfReflection() insertion point for thinking prompt
`harness/agents/lib/prompts.js:10-16` — add step 4 between "use search_context" and "proceed with {verb}". Used by 4 of 6 agents (the two synthesizers don't use it since they have `withFileTools: false`).

### Verification split requires yamlMultiKeyRe update
`harness/internal/swarmorch/artifact.go:27` — the regex that fixes LLM multi-key YAML output includes `verificationChecks`. When splitting to `automatedVerification|manualVerification`, this regex must be updated too or artifact parsing will break.

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md` — combined plan (this session's primary output)
- `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md` — Phase 0 detail plan with exact code snippets (prior session)
- `thoughts/CoreyCole/handoffs/general/2026-03-08_22-01-41_swarm-prompt-humanlayer-improvements.md` — prior handoff that this session resumed from
- `thoughts/CoreyCole/research/2026-03-08_22-02-53_humanlayer-linear-context-threading.md` — HumanLayer Linear context threading research
- `thoughts/CoreyCole/research/2026-03-08_21-51-08_humanlayer-commands-agents-skills.md` — HumanLayer agent architecture research

## Action Items & Next Steps

1. **Fix stale MEMORY.md entries** — Remove/update claims about `IsLinearIdentifier`, gate endpoints, `swarm_config` table, and Linear comment posting that don't exist on this branch.

2. **Implement Phase 0 (prompt improvements)** — 5 independent changes, exact code in the detail plan. Start here — no dependencies, quick win, improves agent output quality for all subsequent work.
   - Files: `research-agent.js`, `research-questions.js`, `specialist-planner.js`, `plan-synthesizer.js`, `research-synthesizer.js`, `lib/prompts.js`, `types.go`, `artifact.go`

3. **Implement Phase 1 (Linear CLI wrapper + labels)** — Create `harness/internal/linear/cli.go` + `types.go`. Create `harness/scripts/setup-linear.sh` and run it to create labels in Linear.

4. **Implement Phase 2 (Temporal activities)** — 7 new activities on `SwarmActivities`. All no-op when ticket ID is empty.

5. **Implement Phase 3 (post-processor agent)** — New `harness/agents/linear-context-processor.js` + types + validation.

6. **Implement Phase 4 (wire into workflows)** — Accept `ticket_id` from API, insert Linear activities at stage boundaries in both workflows.

7. **Implement Phase 5 (artifact URL serving)** — New route `GET /swarm/artifacts/:id/view` so Linear attachment links are clickable.

8. **Implement Phase 6 (manager initialization)** — Initialize Linear client in `main.go`, pass to SwarmActivities.

9. **Verify** — `cd harness && go build ./...`, then manual test with and without `ticket_id`.

## Other Notes

### linear-cli workspace already configured
The workspace `creative-mode` was added during this session. Future sessions don't need to re-run `linear-cli config workspace-add`. The API key is read from the workspace config at `~/.config/linear-cli/config.toml`.

### linear-cli skills available at `.agents/skills/linear-*/SKILL.md`
29 skill files from `Finesssee/linear-cli` are installed. Key ones: `linear-list`, `linear-create`, `linear-update`, `linear-comments`, `linear-attachments`, `linear-search`, `linear-statuses`, `linear-api`, `linear-relations`. These document all CLI flags and patterns.

### Context flow architecture (from prior session, still valid)
Per-agent context matrix — which agents get projectContext, fileTools, search_context, selfReflection:
- research-questions, research-agent, plan-orchestrator, specialist-planner: all four
- research-synthesizer, plan-synthesizer: none (withFileTools: false)
- Gating logic: `agent-factory.js:23-29` — `(startMsg.projectContext && withFileTools)` controls injection

### Relationship to agent primitives flowchart
The flowchart at `thoughts/swarm/agent-primitives-flowchart.html` shows the full vision including implementation, verification loops, plan revision cycles, and project decomposition. This plan only covers the research and planning primitives (types 1 and 2 from the flowchart). Implementation/verification (the "Max's Bolt" loop) and project orchestration (type 3) are future work.

### Post-processor design rationale
The post-processor is a separate agent (not inline in the synthesizer) because: (a) synthesizers shouldn't self-validate their own scope claims, (b) the post-processor needs to search Linear for duplicates which requires tool access the synthesizers don't have (`withFileTools: false`), (c) it keeps the comment generation decoupled from artifact generation so either can change independently.
