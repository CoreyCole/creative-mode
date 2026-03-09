---
date: 2026-03-08T22:01:41-07:00
researcher: CoreyCole
git_commit: 84cc6f33838b5e21a90b51babd6cd825112e6f2c
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Agent Prompt Improvements — HumanLayer-Inspired Patterns"
tags: [swarm, prompts, agent-design, humanlayer, verification, tags, documentarian]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Prompt HumanLayer Improvements

## Task(s)

### Plan Creation — COMPLETED
Created implementation plan for 5 HumanLayer-inspired improvements to swarm agent prompts and types. Plan reviewed and finalized, ready for implementation.

Working from:
- Prior handoff: `thoughts/CoreyCole/handoffs/general/2026-03-08_21-47-59_swarm-prompt-review-humanlayer-insights.md` (Phase 1-3 bug fixes + context injection already done, HumanLayer findings documented but not yet implemented)
- HumanLayer research: `thoughts/CoreyCole/research/2026-03-08_21-51-08_humanlayer-commands-agents-skills.md`

### Implementation — NOT YET STARTED
The 5 changes are ready to implement per the plan. No code was written in this session.

## Critical References
- **Implementation plan**: `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md` — the complete plan with exact code snippets for each change
- **HumanLayer agent definitions**: `context/humanlayer/.claude/agents/` — source of the documentarian pattern (especially `codebase-analyzer.md` for the full CRITICAL block format)
- **HumanLayer plan template**: `context/humanlayer/.claude/commands/create_plan.md:225-240` — source of automated/manual verification split format

## Recent changes

No code changes in this session. All changes are from the prior handoff's Phase 1-3 work (already in working tree as unstaged modifications).

## Learnings

### Documentarian constraint should be adapted per agent role
HumanLayer applies the full "documentarian, not critic" CRITICAL block to 3 of their 6 agents (codebase-analyzer, codebase-locator, codebase-pattern-finder). Their question decomposer and synthesizers do NOT get it. For our agents:
- `research-agent.js` — full CRITICAL block (reads code, should only document)
- `research-questions.js` — lighter variant: "decompose into factual questions, do not suggest improvements" (it's a decomposer, not a documenter)

### Verification split requires Go struct change + validation update
`PlannerOutput` struct at `harness/internal/swarmorch/types.go:138-145` has `VerificationChecks []string`. Must split to `AutomatedVerification []string` + `ManualVerification []string`. Also update validation at `harness/internal/swarmorch/artifact.go:288` to check `len(a.AutomatedVerification) == 0 && len(a.ManualVerification) == 0`. No other Go code references `VerificationChecks` directly beyond these two locations plus the JS output format in `specialist-planner.js:27-29`.

### "What We're NOT Doing" should be added at specialist level too
The prior handoff only mentioned `plan-synthesizer.js`, but each specialist should flag out-of-scope items for their domain. The synthesizer then merges these into a unified section.

### Tag vocabulary should be constrained
Rather than letting agents invent arbitrary tags, suggest: `database`, `api`, `temporal`, `ui`, `bevy`, `wasm`, `discord`, `auth`, `migration`, `config`, `build`, `testing`.

### selfReflection() is the right insertion point for thinking prompt
`harness/agents/lib/prompts.js:10-16` — add step 4 "Think deeply about underlying patterns, connections, and architectural implications" between "use search_context" and "proceed with {verb}". Used by 4 of 6 agents.

### Context flow architecture (verified)
Per-agent context matrix — which agents get what:

| Agent | Project Context | File Tools | search_context | selfReflection |
|-------|:-:|:-:|:-:|:-:|
| research-questions | Yes | Yes | Yes | Yes |
| research-agent | Yes | Yes | Yes | Yes |
| research-synthesizer | No | No | No | No |
| plan-orchestrator | Yes | Yes | Yes | Yes |
| specialist-planner | Yes | Yes | Yes | Yes |
| plan-synthesizer | No | No | No | No |

Gating logic at `harness/agents/lib/agent-factory.js:23-29`: `(startMsg.projectContext && withFileTools)` controls whether project context is prepended to system prompt.

## Artifacts
- `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md` — implementation plan (this session's output)
- `thoughts/CoreyCole/handoffs/general/2026-03-08_21-47-59_swarm-prompt-review-humanlayer-insights.md` — prior handoff with Phase 1-3 work + HumanLayer findings
- `thoughts/CoreyCole/research/2026-03-08_21-51-08_humanlayer-commands-agents-skills.md` — HumanLayer architecture research

## Action Items & Next Steps

1. **Implement all 5 changes** per the plan at `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md`. The plan has exact code snippets for each file. Changes are independent — no ordering constraints except Go must compile.

   Files to modify:
   - `harness/agents/research-agent.js` — add documentarian CRITICAL block + tags in output format
   - `harness/agents/research-questions.js` — add lighter "factual questions only" constraint
   - `harness/agents/specialist-planner.js` — split verificationChecks → automated/manual + tags + scope exclusions
   - `harness/agents/plan-synthesizer.js` — preserve verification split + "What We're NOT Doing" + tags
   - `harness/agents/research-synthesizer.js` — add tags in output format
   - `harness/agents/lib/prompts.js` — add thinking step to selfReflection()
   - `harness/internal/swarmorch/types.go` — split PlannerOutput.VerificationChecks into two fields
   - `harness/internal/swarmorch/artifact.go` — update validation for new fields

2. **Verify Go compiles**: `cd harness && go build ./...`

3. **Run end-to-end verification**: Start a research and a code_change_plan workflow to verify outputs have tags, no improvement suggestions in research, and verification split in plans.

## Other Notes

### HumanLayer patterns NOT adopted (and why)
- **Agent specialization split** (locator/analyzer/pattern-finder with different tool access) — deferred. Our single `research-agent.js` handles all roles. Worth revisiting if research quality remains an issue.
- **Interactive human-in-the-loop** — our agents are fully autonomous, making the documentarian constraint and scope exclusions even more critical (no human to course-correct).
- **"ultrathink" keyword** — HumanLayer's branding. We use "Think deeply about underlying patterns, connections, and architectural implications" instead.
- **`color` field in agent frontmatter** — cosmetic, not relevant to our JSONL-based system.

### Key architectural difference
HumanLayer agents are interactive (present understanding → get buy-in → research → present options → get approval at every step). Our swarm agents are fully autonomous. This means stronger self-correction and scope constraints are needed since there's no human to course-correct mid-workflow.
