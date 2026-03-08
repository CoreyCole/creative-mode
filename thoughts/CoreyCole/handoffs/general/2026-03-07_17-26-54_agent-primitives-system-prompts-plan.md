---
date: 2026-03-08T01:26:54-08:00
researcher: CoreyCole
git_commit: 2417134078cd956170fdc391a641b2d0b459da69
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives — System Prompts & Skill Files Plan"
tags: [implementation, strategy, swarm, agent-primitives, pi-mono, system-prompts, skills]
status: in_progress
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives — System Prompts & Skill Files Plan

## Task(s)

**Writing a plan for the 6 agent system prompts + 7 skill files.** Status: **plan written, awaiting review**.

- Resumed from previous handoff (`2026-03-07_16-58-03_agent-primitives-v3-refinement-sandboxing.md`)
- Conducted deep research into three systems to inform prompt design:
  1. **Claude Code skills/agents** (`.claude/skills/`, `.claude/agents/`) — role definitions, do/don't rules, output schemas, sub-agent delegation patterns
  2. **Pi-mono Agent architecture** (`/opt/openclaw/node_modules/@mariozechner/pi-agent-core/`) — Agent class API, tree context system, sequential tool execution, compaction, event types, skill discovery
  3. **Harness codebase patterns** — EventBus, server/routes, DB/migrations, templ/Datastar, mayor build system
- Produced a complete plan with near-final content for all 13 files
- **No code written** — plan review is the next step

## Critical References

1. **System prompts & skill files plan (this session's output)**: `thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md`
2. **v3 plan (authoritative architecture)**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
3. **Previous handoff (full context chain)**: `thoughts/CoreyCole/handoffs/general/2026-03-07_16-58-03_agent-primitives-v3-refinement-sandboxing.md`

## Recent changes

No code changes this session. One new plan document created:
- `thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md` — complete plan with all 13 files' content

## Learnings

### Pi-mono Tree Context System
- Sessions stored as append-only JSONL files with `id`/`parentId` pointers forming a tree structure
- Context management is a two-stage pipeline: `transformContext` (AgentMessage[] pruning) → `convertToLlm` (type mapping to LLM Message[])
- Compaction is automatic — triggers when `contextTokens > contextWindow - reserveTokens` (default reserve: 16384, keep recent: 20000)
- Branch summaries injected as user messages wrapped in `<summary>` tags
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/agent.js`, `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/session-manager.js`

### Pi-mono Agent API (verified)
- `Agent` constructor accepts `AgentOptions` with defaults: model=gemini-2.5-flash-lite, steeringMode="one-at-a-time", transport="sse"
- Key methods: `setSystemPrompt(v)`, `setModel(m)`, `setTools(t)`, `prompt(input, images?)`, `subscribe(fn)`, `abort()`, `waitForIdle()`
- Tool execution is **sequential** with steering checks between each tool — if steering message arrives, remaining tools are skipped
- `AgentEvent` types: `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_execution_start`, `tool_execution_update`, `tool_execution_end`
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/agent.js:107-403`

### Pi-mono Skill Discovery Pattern
- Skills formatted as XML `<available_skills>` in system prompt: `<skill><name>...</name><description>...</description><location>...</location></skill>`
- Discovery: root-level `.md` files + recursive `SKILL.md` in subdirs
- Gating via metadata: required bins, env vars, OS filter
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/skills.js`

### Pi-mono System Prompt Construction
- `buildSystemPrompt()` is layered: base prompt → context files → skills → timestamp/cwd
- Tool-aware guidelines — only included when the tool is available (prevents contradictions)
- Custom prompt support: if `customPrompt` provided, uses it directly with appended context/skills
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/system-prompt.js:17-136`

### Key Difference: Our Agents vs Pi-mono Sessions
- Pi-mono sessions are interactive, long-lived, with compaction and branching
- Our agents are **subprocesses**: one task → one artifact → exit via JSONL stdin/stdout
- No sub-agent spawning, no interactive iteration, no session persistence
- Skills discovered via `ls` + `read` file tools (not XML injection in system prompt)
- System prompts should be concise (~1-2K tokens) vs pi-mono's multi-section construction

### Claude Code Skill/Agent Patterns
- Skills define rich role personas, explicit do/don't rules, output schemas, file:line references
- Agents split into locate-then-analyze pairs: `codebase-locator` (Grep/Glob/LS only) → `codebase-analyzer` (adds Read)
- Tool restriction as context narrowing — locators can't Read, analyzers can
- All skills enforce "read files FULLY" and "read before spawning sub-agents"
- Source: `.claude/skills/create_plan.md`, `.claude/agents/codebase-analyzer.md`, etc.

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md` — the plan to review

## Action Items & Next Steps

1. **Review the plan** using `/review_plan thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md`
   - Key review areas: skill file accuracy (do file:line references match?), prompt tone/directiveness, missing domain knowledge, artifact schema completeness
2. **Address review feedback** — iterate on prompt text and skill content
3. **Implement Phase 1** (skill files) — create `harness/agents/skills/` dir and write 7 .md files
4. **Implement Phase 2** (agent scripts) — write 6 .js files with system prompts and artifact schemas, plus `harness/internal/swarmorch/prompts.go` for Go constants
5. **Continue to Phase 1 of v3 plan** — foundation (migration, SQLC, Temporal SDK, shared JS libs)

## Other Notes

### Document Evolution Chain
v1 → v1 review → v2 → v2 handoff → v3 → v3 handoff → v3 refinement → v3 refinement handoff → **system prompts plan (this session)** → review (next)

### Plan Design Decisions
- **Prompts as Go constants** (not .md files) — short, tightly coupled to JS artifact schemas, rarely change
- **Plan orchestrator gets only `read` tool** — extracts `createReadOnlyTools(cwd)[0]`, not full suite
- **Domain-to-skill mapping in prompt text** — specialist planner prompt says "database → database-conventions.md"
- **No tool call caps** — soft limits ("keep under N chars") instead; monitor via SSE dashboard
- **Skill files are pure reference** — domain facts only, no behavioral instructions

### VPS State
- Temporal dev server running (7233/8233, namespace `swarm`)
- bubblewrap + temporal-cli in flake.nix (verified installed)
- No `OPENAI_API_KEY` in `.env` yet
- No swarm Go code exists on this branch
