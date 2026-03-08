---
date: 2026-03-08T01:29:01-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 773d305ee138bceac6e3b412f627dbf37bc5374f
branch: feat/agent-primitives
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md
status: complete
type: plan_review
---

# Plan Review: Agent Primitives — System Prompts & Skill Files

### Summary

Solid plan with thorough domain knowledge captured in skill files and well-structured system prompts. The skill files are impressively accurate against the current codebase (12/13 file:line references verified). Two critical issues around fragility and a schema mismatch need addressing before implementation.

### Critical Issues (Must Address Before Implementation)

1. **`temporal-conventions.md` describes patterns that don't exist yet**
   - Problem: The skill file documents Temporal workflow patterns, activity patterns, task queues, and worker setup — but zero Temporal code exists in the harness (verified: `harness/internal/temporal/` doesn't exist, `go.temporal.io/sdk` not in `go.mod`). Research agents loading this skill will find no matching code when they try to verify claims.
   - Risk: Research agents will report "low confidence" findings or hallucinate connections because the skill describes target architecture, not current state. Specialist planners will reference Temporal patterns that they can't verify with `grep`/`read`.
   - Suggestion: Either (a) defer `temporal-conventions.md` to Phase 2 when Go code is being written (it's only useful for the `temporal` specialist planner, which won't run until Temporal code exists), or (b) add a clear header: `> NOTE: This documents TARGET patterns for the swarm system. This code does not exist yet — it will be created following these conventions.` so agents know to treat it as spec, not discoverable code.

2. **Plan Orchestrator tool selection is fragile — depends on array index**
   - Problem: The plan says the plan orchestrator should get only `read`: `createReadOnlyTools(cwd)[0]`. I verified the current return order in pi-coding-agent v0.54.0 (`/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/tools/index.js:44-46`): `[createReadTool, createGrepTool, createFindTool, createLsTool]`. This is correct today, but it's an undocumented implementation detail.
   - Risk: A pi-mono version bump (or even a patch) could reorder the array, silently giving the plan orchestrator `grep` instead of `read`. The agent would fail to read the research doc — a runtime error with no compile-time safety.
   - Suggestion: Use named destructuring or filter by tool name: `const readTool = createReadOnlyTools(cwd).find(t => t.name === 'read')` or import `createReadTool` directly from `@mariozechner/pi-coding-agent/dist/core/tools/index.js` (it's exported). This is a one-line change that eliminates the fragility.

3. **Artifact schema for Question Generator conflicts with v3 plan**
   - Problem: The system prompts plan defines the Question Generator artifact as `{ questions: string[] }`. The v3 plan's `GenerateResearchQuestions` activity returns `([]string, error)` — just a string array. But the agent's `submit_artifact` tool wraps artifacts in `{ type: 'result', data: artifact }`, so Go receives `{ type: 'result', data: { questions: [...] } }`. The Go activity would need to unmarshal `.data.questions`, not `.data` directly.
   - Risk: Runtime JSON unmarshaling error on first run. The Go side expects `[]string` but receives `{questions: []string}`.
   - Suggestion: Either (a) change the Go activity to expect the nested structure `GenerateQuestionsResult{Questions: []string}`, or (b) change the artifact schema to just `string[]` (the submit_artifact tool's parameters ARE the artifact). Recommend (a) since it's more explicit and self-documenting. This applies to ALL 6 agent artifact schemas — verify each one matches what the Go activity will unmarshal.

### Concerns (Should Address)

1. **Skill files will go stale immediately after Phase 1 implementation**
   - Observation: `database-conventions.md` lists 5 migrations (001-005) and 12 query files. Phase 1 of the v3 plan adds migration 006 and new SQLC queries. `api-conventions.md` references `server.go:107-229` for route registration — adding swarm routes will shift these line numbers. `project-structure.md` lists directories that don't include `harness/internal/temporal/` or `harness/agents/`.
   - Suggestion: Add a section to the plan addressing skill file maintenance. Options: (a) treat skill files as "initial bootstrap" docs that get a maintenance pass after each implementation phase, (b) keep line numbers out of skill files (reference function names instead: `RegisterRoutes() in server.go`), (c) add `last_verified: YYYY-MM-DD` frontmatter to each skill file. I recommend (b) — function/method names are more stable than line numbers.

2. **2000-character limit on research agent findings may be too tight**
   - Observation: The research agent prompt says "Keep findings under 2000 characters." A thorough investigation of a question like "How does the EventBus work?" would reference `bus.go`, `types.go`, `events.go`, plus 2-3 consumer files. With file:line citations for each (~50 chars each × 6 files = 300 chars for refs alone), that leaves ~1700 chars for actual findings. That's roughly 250-300 words.
   - Suggestion: Increase to 3000 chars, or make it configurable per-task. The synthesizer's 3-5K target leaves room. Alternatively, just make it advisory ("aim for under 2000 characters") rather than a hard rule ("keep under").

3. **Skill files duplicate CLAUDE.md content**
   - Observation: `project-structure.md` significantly overlaps with the root `CLAUDE.md`'s "Project Structure" section. `build-system.md` overlaps with "Running the Server" and "Build & Check" sections. `agent-hierarchy.md` overlaps with "Agent System" section.
   - Suggestion: This is intentional (agents can't see CLAUDE.md, only skill files), but document the relationship in a comment at the top of each skill file: `<!-- Derived from CLAUDE.md — if project structure changes, update both. -->` This makes the maintenance burden explicit.

4. **Research Synthesizer and Plan Synthesizer have no verification capability**
   - Observation: These agents get only `submit_artifact` — no file tools. They must trust that input findings/plans are accurate. If a research agent hallucinates a file:line reference, the synthesizer will propagate it faithfully.
   - Suggestion: This is an acceptable design tradeoff (compression at every boundary), but add a note in the synthesizer prompt: "If findings seem contradictory or reference files that conflict with each other, note the discrepancy rather than silently resolving it." This makes the failure mode visible.

5. **`ask_orchestrator` absent from Question Generator and Synthesizers**
   - Observation: The v3 plan says every agent gets `ask_orchestrator` + `submit_artifact`. But the system prompts plan gives the Question Generator "Tools: None" and synthesizers "Tools: `submit_artifact` only". Since `agent-factory.js` always adds both custom tools, these agents WILL have `ask_orchestrator` available even though the prompts don't mention it.
   - Suggestion: Either (a) have `agent-factory.js` conditionally add `ask_orchestrator` based on a flag, or (b) mention it in the prompt as available-but-discouraged: "You have ask_orchestrator available but should not need it — work only with the provided input." Option (b) is simpler and prevents confusion if the agent tries to use an undescribed tool.

### Questions (Need Clarification)

1. When skill files go stale after implementation phases, who updates them? Is this manual, or should there be a swarm task type for "verify skill file accuracy"?

2. The plan says system prompts are Go string constants in `prompts.go`. But the v3 plan's `agent-factory.js` also accepts `systemPrompt` as a parameter and falls back to `startMsg.systemPrompt`. Which is authoritative — the JS-side default or the Go-side constant sent in the start message? The plan should clarify that `prompts.go` constants are sent via the JSONL start message and override any JS defaults.

3. Should the plan orchestrator have `ask_orchestrator` available? The v3 plan architecture says it's a tool available to every agent, but the system prompts plan explicitly restricts it to just `read` + `submit_artifact`. If the plan orchestrator can't classify a domain confidently, what's the fallback?

4. The Question Generator prompt says "Do NOT exceed the max_questions limit" — but what if the topic genuinely needs more sub-questions? Is 5 a hard cap, or should the prompt allow the agent to explain why it needs more?

### Suggestions (Nice to Have)

1. **Add a `SKILL_VERSION` or `last_verified` field to skill frontmatter** — makes staleness visible and trackable.

2. **Consider using function/method names instead of line numbers in skill files** — `RegisterRoutes() in server.go` is more stable than `server.go:107-229`. Line numbers are great for research agent output (ephemeral), less great for semi-permanent reference docs.

3. **Add a "common pitfalls" section to skill files** — e.g., `database-conventions.md` could warn about the manual `migrationFiles` slice requirement more prominently (it's buried in the migration pattern section). These are the things that trip up both humans and agents.

4. **Consider a brief `README.md` in `harness/agents/skills/`** — listing all skills and their intended audiences. Agents do `ls` first, and a README would help them decide which skills to load without reading all 7.

### What's Good

- **Separation of concerns is clean**: Skills = domain facts, Prompts = behavioral instructions, Artifacts = typed outputs. Each layer has a clear purpose.
- **Compression-by-design**: Every agent produces summaries with file:line references, never raw contents. This keeps context budgets manageable.
- **Skill files are impressively thorough and accurate**: Database conventions include transaction patterns, the SQLC workflow, and the manual migration gotcha. API conventions cover all 4 auth patterns. The codebase analyzer verified 12/13 file:line references are correct.
- **System prompts are appropriately concise**: ~1-2K tokens each. They tell the agent what to do, not how the codebase works (that's the skill files' job).
- **The "What We're NOT Doing" section is clear**: Good scope discipline — content authoring only, no running code.
- **Discoverable skill pattern is elegant**: `ls harness/agents/skills/` → `read` relevant ones. Adding domain knowledge = dropping a file. No agent code changes.

### Recommended Next Steps

1. **Address Critical Issue #3 first** — verify all 6 artifact schemas match what the Go activities expect. This is a design-level mismatch that would cause runtime failures.
2. **Address Critical Issue #1** — either defer `temporal-conventions.md` or add the "target patterns" header.
3. **Address Critical Issue #2** — switch from array index to named lookup for the plan orchestrator's read tool.
4. **Address Concern #1** — decide on a line-number-vs-function-name strategy for skill files before writing them.
5. **Implement Phase 1** (skill files) — these are the simplest deliverable and provide immediate value for Phase 2.
6. **Implement Phase 2** (agent scripts + Go constants) — with the schema alignment from step 1.
