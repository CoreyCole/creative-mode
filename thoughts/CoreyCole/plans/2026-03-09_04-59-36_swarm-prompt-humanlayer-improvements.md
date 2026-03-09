# Swarm Agent Prompt Improvements — HumanLayer-Inspired Patterns

## Overview

Apply 5 targeted improvements to swarm agent prompts and types, inspired by patterns from HumanLayer's agent architecture. These changes improve research quality (documentarian constraint), plan actionability (verification split, scope exclusions), output searchability (tags), and agent reasoning (deeper thinking prompt).

All changes are on the `feat/agent-primitives` branch, building on the Phase 1-3 work (bug fixes, context injection, prompt coherence) already in the working tree.

## Current State Analysis

The swarm agent system has 6 JS agent scripts running under a Go orchestrator via JSONL protocol:
- 4 "file-tool" agents: `research-questions.js`, `research-agent.js`, `plan-orchestrator.js`, `specialist-planner.js`
- 2 "synthesis" agents: `research-synthesizer.js`, `plan-synthesizer.js`

System prompt assembly: `[projectContext] --- [selfReflection()] + [role-specific instructions] + [output format]`

### Key Discoveries:
- `harness/agents/lib/prompts.js:10-16` — `selfReflection()` is procedural ("review docs, load skills, search, proceed") but lacks a thinking/reflection step
- `harness/agents/research-agent.js:5` — no constraint preventing the agent from suggesting improvements instead of documenting
- `harness/agents/specialist-planner.js:27-29` — flat `verificationChecks` with no automated/manual distinction
- `harness/internal/swarmorch/types.go:142` — `VerificationChecks []string` is a flat slice
- `harness/internal/swarmorch/artifact.go:288` — validation only checks `len(a.VerificationChecks) == 0`
- `harness/agents/plan-synthesizer.js:6-14` — no "What We're NOT Doing" instruction
- No agent output format includes `tags` in YAML frontmatter

## Desired End State

After implementation:
1. Research agents document what exists without suggesting changes
2. Plans separate runnable commands from human testing steps
3. Plans include explicit scope exclusions
4. All agent outputs include searchable tags in frontmatter
5. Agents think deeply about patterns before proceeding

### Verification:
- All 6 agent scripts parse and run (no syntax errors)
- Go code compiles: `cd harness && go build ./...`
- A test research workflow produces output with tags and no improvement suggestions
- A test plan workflow produces output with separated verification and scope exclusions

## What We're NOT Doing

- **Agent specialization split** (locator vs analyzer vs pattern-finder) — deferred; our single `research-agent.js` handles all three roles for now
- **Tool restriction tiers** — all file-tool agents keep the same tool set
- **Interactive human-in-the-loop** — our agents remain fully autonomous
- **Output format migration** — existing artifacts in `thoughts/swarm/` are not retroactively updated

## Implementation Approach

All 5 changes are independent prompt/type edits with no cross-dependencies. They can be implemented in a single phase. The only ordering constraint is that the Go struct change (verification split) must compile before we can verify.

## Phase 1: Prompt and Type Improvements

### Overview
Apply all 5 changes across JS agent scripts, the shared prompt fragment, and the Go PlannerOutput struct.

### Changes Required:

#### 1. Documentarian Constraint — Research Agents
**Files**: `harness/agents/research-agent.js`, `harness/agents/research-questions.js`

**`research-agent.js`** — Add CRITICAL block after the role description (line 5), before `selfReflection()`:

```javascript
systemPrompt: `You are a codebase researcher. Your job is to investigate a single focused question by reading source code and producing compressed findings.

CRITICAL — You are a documentarian, not a critic:
- DO NOT suggest improvements or changes to the codebase
- DO NOT critique the implementation or identify problems
- DO NOT propose future enhancements or refactoring
- ONLY describe what exists, how it works, and how components interact
- Your job is to document facts with file:line references, not to evaluate

${selfReflection('your investigation')}
```

**`research-questions.js`** — Add a lighter constraint after the role description (line 5). This agent decomposes questions, not documents code, so the constraint focuses on question quality:

```javascript
systemPrompt: `You are a research decomposer. Your job is to break down a codebase question into 2-5 focused sub-questions that can be investigated in parallel.

CRITICAL — Decompose into factual questions only:
- Each sub-question must ask WHAT exists or HOW something works
- DO NOT include questions that suggest improvements or evaluate quality
- DO NOT frame questions around "what's wrong" or "what could be better"
- Sub-questions should lead to documentation of current state, not criticism

${selfReflection('your investigation')}
```

#### 2. Split Verification Checks — Specialist Planner + Types + Synthesizer
**Files**: `harness/internal/swarmorch/types.go`, `harness/internal/swarmorch/artifact.go`, `harness/agents/specialist-planner.js`, `harness/agents/plan-synthesizer.js`

**`types.go:138-145`** — Replace `VerificationChecks` with two fields:

```go
type PlannerOutput struct {
	Domain                string   `json:"domain"`
	PlanSection           string   `json:"planSection"`
	FilesAffected         []string `json:"filesAffected"`
	AutomatedVerification []string `json:"automatedVerification"`
	ManualVerification    []string `json:"manualVerification"`
	Risks                 []string `json:"risks"`
	Dependencies          []string `json:"dependencies"`
}
```

**`artifact.go:288`** — Update validation to check the new fields:

```go
if len(a.AutomatedVerification) == 0 && len(a.ManualVerification) == 0 {
    return errors.New("must include at least one verification check (automated or manual)")
}
```

**`specialist-planner.js:27-29`** — Update YAML frontmatter output format:

```yaml
automatedVerification:
  - "just check"
  - "go test ./..."
manualVerification:
  - "Verify the UI renders correctly"
  - "Test edge case: empty input"
```

**`plan-synthesizer.js:12-13`** — Add instruction to preserve the automated/manual distinction:

Add to guidelines: `- Preserve the automated vs manual verification distinction from each specialist`

#### 3. "What We're NOT Doing" Scope Section
**File**: `harness/agents/plan-synthesizer.js`

Add to output format guidelines:

```
- Include a "What We're NOT Doing" section listing explicit out-of-scope items to prevent scope creep
```

Also add to `specialist-planner.js` guidelines so each specialist flags out-of-scope items for their domain, which the synthesizer can merge.

#### 4. Tags in Output Frontmatter
**Files**: `harness/agents/research-agent.js`, `harness/agents/research-synthesizer.js`, `harness/agents/specialist-planner.js`, `harness/agents/plan-synthesizer.js`

Add `tags` field to the YAML frontmatter in each agent's output format spec.

**`research-agent.js`** — Add to frontmatter:
```yaml
tags:
  - "relevant-tag-1"
  - "relevant-tag-2"
```
With instruction: `Choose 2-5 tags from: database, api, temporal, ui, bevy, wasm, discord, auth, migration, config, build, testing, or other relevant terms.`

**`research-synthesizer.js`** — Add to frontmatter:
```yaml
tags:
  - "relevant-tag-1"
```
With same tag vocabulary instruction.

**`specialist-planner.js`** — Add to frontmatter:
```yaml
tags:
  - "relevant-tag-1"
```

**`plan-synthesizer.js`** — Add to frontmatter:
```yaml
tags:
  - "relevant-tag-1"
```

#### 5. Deeper Thinking Prompt
**File**: `harness/agents/lib/prompts.js`

Update `selfReflection()` to add a thinking step between reviewing context and proceeding:

```javascript
export function selfReflection(verb) {
  return `Before starting work:
1. Review the project documentation and skills manifest already provided in your context
2. Use read to load the full content of any skills relevant to your task (paths listed in the manifest)
3. Use search_context if you need to find specific source files beyond what's in the documentation
4. Think deeply about underlying patterns, connections, and architectural implications before proceeding
5. Then proceed with ${verb}`;
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Go compiles: `cd harness && go build ./...`
- [ ] No JS syntax errors: each agent script can be parsed by Node
- [ ] All 6 agent scripts contain valid template literal syntax

#### Manual Verification:
- [ ] Start a research workflow via `/api/swarm/tasks/research` and verify:
  - Output contains `tags:` in frontmatter
  - Research findings document facts without suggesting improvements
- [ ] Start a code_change_plan workflow via `/api/swarm/tasks/code-change-plan` and verify:
  - Specialist plans have `automatedVerification` and `manualVerification` (not `verificationChecks`)
  - Final plan includes "What We're NOT Doing" section
  - Output contains `tags:` in frontmatter

---

## Testing Strategy

### Unit Tests:
- No unit tests needed — these are prompt/type changes
- Go compilation is the primary automated check

### Manual Testing Steps:
1. Start a research task: `curl -X POST http://localhost:8080/api/swarm/tasks/research -H "X-Hook-Secret: $CM_HOOK_SECRET" -H "Content-Type: application/json" -d '{"request_text":"How does the checkpoint system work?"}'`
2. Check research output in `thoughts/swarm/research/` for tags and documentarian behavior
3. Start a plan task: `curl -X POST http://localhost:8080/api/swarm/tasks/code-change-plan -H "X-Hook-Secret: $CM_HOOK_SECRET" -H "Content-Type: application/json" -d '{"request_text":"Add a health check endpoint"}'`
4. Check plan output in `thoughts/swarm/project-plans/` for verification split and scope exclusions

## References

- Handoff: `thoughts/CoreyCole/handoffs/general/2026-03-08_21-47-59_swarm-prompt-review-humanlayer-insights.md`
- HumanLayer research: `thoughts/CoreyCole/research/2026-03-08_21-51-08_humanlayer-commands-agents-skills.md`
- HumanLayer agent definitions: `context/humanlayer/.claude/agents/` (documentarian pattern source)
- HumanLayer plan template: `context/humanlayer/.claude/commands/create_plan.md` (verification split source)
- Previous prompt plan: `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md`
