---
date: 2026-03-08T21:47:59-07:00
researcher: CoreyCole
git_commit: 31ade4ce4492a9fba703e31ec70bb7ecab100ae2
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Agent Prompt Review + HumanLayer Insights"
tags: [swarm, prompts, agent-design, context-injection, humanlayer]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Prompt Review + HumanLayer Insights

## Task(s)

### 1. Swarm System Review — Bugs, Quality, Context Injection (COMPLETED)
Implemented a 3-phase plan to fix bugs, inject deterministic project context, and expand search scope.

### 2. Context/Prompt Coherence Audit (COMPLETED)
Reviewed all 6 agent scripts and the agent-factory to ensure injected project context doesn't confuse agents. Fixed 5 issues (tool name mismatch, redundant discovery instructions, synthesis agents getting file-tool docs they can't use).

### 3. HumanLayer Prompt Analysis (COMPLETED — findings only, no implementation yet)
Analyzed `context/humanlayer/.claude/agents/` and `.claude/commands/` for patterns we can adopt. Key findings documented below in Learnings.

## Critical References
- `harness/agents/lib/agent-factory.js` — central agent factory, controls what context each agent receives
- `harness/agents/lib/prompts.js` — shared `selfReflection()` prompt fragment used by 4 of 6 agents
- `context/humanlayer/.claude/commands/research_codebase.md` — their research orchestration (richest comparison point)

## Recent changes

Phase 1 (bug fixes):
- `harness/internal/swarmorch/workflows.go:627` — changed `research.Synthesize.Summary` to `.Document` so specialist planners get full research
- `harness/agents/skills/temporal-conventions.md` — complete rewrite removing stale "Go SDK not integrated" content

Phase 2 (context injection):
- `harness/internal/swarmorch/types.go:20` — added `ProjectContext` field to `StartMessage`
- `harness/internal/swarmorch/context.go` — NEW file: `loadProjectContext()` reads CLAUDE.md + skill frontmatter
- `harness/internal/swarmorch/agent.go:43` — added `projectContext` to `runAgentParams`, wired to StartMessage
- `harness/internal/swarmorch/activities.go:44` — cached `projectContext` on `SwarmActivities`, loaded at construction
- `harness/agents/lib/agent-factory.js:26` — gated context injection: `(startMsg.projectContext && withFileTools)` — synthesis agents skip it

Phase 3 (skills + search scope):
- `harness/agents/skills/swarm-conventions.md` — NEW: documents JSONL protocol, agent scripts, output format
- `harness/internal/swarmorch/agent.go:507-545` — expanded grep to search `templates/` dir + `.rs` files

Prompt coherence fixes:
- `harness/internal/swarmorch/context.go:39` — fixed tool name: `read_file` → `read`
- `harness/agents/lib/prompts.js:11-15` — rewrote `selfReflection()` to leverage injected context instead of redundant search_context discovery
- `harness/agents/research-questions.js:15` — removed instruction to use search_context for project structure (already injected)
- `harness/agents/specialist-planner.js:10` — changed "search for conventions" to "read the skill file from the manifest"

## Learnings

### HumanLayer Patterns Worth Adopting (not yet implemented)

**1. "Documentarian, Not Critic" constraint for research agents**
Every HumanLayer research agent has a CRITICAL block:
```
- DO NOT suggest improvements or changes
- DO NOT critique the implementation
- ONLY describe what exists, how it works, and how components interact
```
Our `research-agent.js` and `research-questions.js` lack this. Research agents may waste tool calls suggesting improvements instead of documenting. Only planning agents should suggest changes.

**Where to add**: `harness/agents/research-agent.js:5` (system prompt), `harness/agents/research-questions.js:5` (system prompt)

**2. Automated vs Manual verification separation in plans**
HumanLayer plan template explicitly separates:
- `Automated Verification`: commands that can be run (`make test`, `go test`)
- `Manual Verification`: human testing steps (UI, performance, edge cases)
Our `specialist-planner.js:27-29` has flat `verificationChecks` with no distinction.

**Where to add**: `harness/agents/specialist-planner.js` output format section, `harness/internal/swarmorch/types.go` PlannerOutput struct (split `VerificationChecks []string` into two fields or add a structured type)

**3. "What We're NOT Doing" scope section in plans**
HumanLayer's plan template includes explicit out-of-scope items to prevent scope creep. Our plan-synthesizer doesn't produce this.

**Where to add**: `harness/agents/plan-synthesizer.js:6-14` (add to output format guidelines)

**4. Deeper thinking prompts**
HumanLayer uses "Take time to ultrathink about the underlying patterns, connections, and architectural implications." Our `selfReflection()` is procedural but doesn't prompt for deeper analysis.

**Where to add**: `harness/agents/lib/prompts.js` — add a thinking step between reviewing context and proceeding

**5. Tags in output frontmatter**
HumanLayer output docs include `tags: [research, codebase, relevant-tags]`. Our research/plan outputs lack tags. Adding them would improve searchability of `thoughts/swarm/` artifacts.

**Where to add**: Output format sections in `research-agent.js`, `research-synthesizer.js`, `specialist-planner.js`, `plan-synthesizer.js`

**6. Agent specialization (Locator vs Analyzer vs Pattern-finder)**
HumanLayer separates three research roles with different tool access:
- Locator: find WHERE (Grep, Glob, LS — no Read)
- Analyzer: understand HOW (Read, Grep, Glob, LS)
- Pattern-finder: show EXAMPLES (Grep, Glob, Read, LS)
We use one `research-agent.js` for all three. Consider splitting if research quality remains an issue.

### Context Flow Architecture (verified working)

Per-agent context matrix:
| Agent | Project Context | File Tools | search_context | selfReflection |
|-------|:-:|:-:|:-:|:-:|
| research-questions | Yes | Yes | Yes | Yes |
| research-agent | Yes | Yes | Yes | Yes |
| research-synthesizer | No | No | No | No |
| plan-orchestrator | Yes | Yes | Yes | Yes |
| specialist-planner | Yes | Yes | Yes | Yes |
| plan-synthesizer | No | No | No | No |

System prompt assembly: `[projectContext] --- [selfReflection()] + [role-specific instructions] + [output format]`

## Artifacts
- `harness/internal/swarmorch/context.go` — NEW: project context loading
- `harness/agents/skills/temporal-conventions.md` — rewritten
- `harness/agents/skills/swarm-conventions.md` — NEW: swarm system documentation
- `thoughts/CoreyCole/handoffs/general/2026-03-09_swarm-review-bugs-context.md` — earlier handoff (Phase 1-3 only)

## Action Items & Next Steps

1. **Add "documentarian" constraint to research agents** — Add the "DO NOT suggest improvements, ONLY document what exists" block to `research-agent.js:5` and `research-questions.js:5` system prompts. This is the highest-impact change from HumanLayer patterns.

2. **Split verificationChecks into automated/manual** — Update `specialist-planner.js` output format to require separate `automatedVerification` and `manualVerification` arrays. Update `PlannerOutput` struct in `types.go` accordingly. Update `plan-synthesizer.js` to preserve the distinction.

3. **Add "What We're NOT Doing" section to plan output** — Add instruction to `plan-synthesizer.js` system prompt to include explicit scope exclusions.

4. **Add tags to output frontmatter** — Update research and plan agent output format specs to include `tags: [relevant, tags]` in YAML frontmatter. This helps `thoughts-locator`-style search later.

5. **Add deeper thinking prompt** — Update `selfReflection()` in `prompts.js` to include "Think deeply about patterns, connections, and architectural implications before proceeding."

6. **Run end-to-end verification** — Start a research workflow and a code_change_plan workflow to verify the context injection and prompt changes produce better output. Compare with pre-change artifacts in `thoughts/swarm/`.

## Other Notes

### HumanLayer Reference Files (in context/)
- Agent definitions: `context/humanlayer/.claude/agents/` (6 agents with YAML frontmatter + detailed markdown)
- Command definitions: `context/humanlayer/.claude/commands/` (27 slash commands)
- Their agent frontmatter format: `name`, `description`, `tools`, `model`, `color` (we use `name`, `description`, `tags`, `last_verified`)
- Their plan template: `context/humanlayer/.claude/commands/create_plan.md:182-277` — gold standard for plan structure
- Their handoff template: `context/humanlayer/.claude/commands/create_handoff.md` — structured with Task status, Learnings, Artifacts, Action Items

### Key Architectural Difference
HumanLayer's agents are interactive (human-in-the-loop at every step — present understanding → get buy-in → research → present options → get approval). Our swarm agents are fully autonomous (no human interaction during workflow). This means we need stronger self-correction and scope constraints since there's no human to course-correct.
