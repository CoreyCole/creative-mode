---
date: 2026-03-07T16:22:48-08:00
researcher: CoreyCole
git_commit: 2ecd88292a8c69fa2c76a59cbcf93150469b8793
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives v3 — Conversational Agents Plan Design"
tags: [implementation, strategy, swarm, temporal, pi-mono, agent-primitives, conversational-protocol]
status: in_progress
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives v3 — Conversational Agents Plan Design

## Task(s)

**Designing an agent swarm system on top of Temporal, pi-mono, and SQLite** — focusing on Primitive 1 (Research) and Primitive 2 (Code Change Plan). Status: **plan v3 written, continuing to refine**.

- **v3 plan created** (`thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`) — this is the primary artifact. Major upgrade from v2 with three architectural shifts.
- **v2 plan superseded** (`thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md`) — still useful for context but v3 is authoritative.
- **No code written yet** — this session was entirely design/planning + deep research into pi-mono APIs.
- The user wants to continue refining the plan before implementation begins.

### Three Architectural Shifts (v2 → v3)

1. **Conversational agents** — agents are NOT fire-and-forget subprocesses. They communicate with the Go orchestrator via bidirectional JSONL over stdin/stdout. Agents can ask follow-up questions via `ask_orchestrator` tool, and must submit validated output via `submit_artifact` tool. The workflow step only completes when a valid artifact is submitted.

2. **Lightweight system prompts + discoverable skills** — instead of baking domain knowledge into each agent's system prompt, skills live on disk at `harness/agents/skills/*.md`. Agents discover and load relevant skills using `read_file`. Adding new domain knowledge = dropping a file.

3. **Production pi-mono tools** — use `createReadOnlyTools(cwd)` from `@mariozechner/pi-coding-agent` instead of custom file tools. Returns `[readTool, grepTool, findTool, lsTool]` all sandboxed to cwd with built-in truncation.

## Critical References

1. **v3 Plan (authoritative)**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — READ THIS FIRST, it's the complete design document.
2. **Previous v1-v5 plans from old branch**: All exist at `thoughts/CoreyCole/plans/2026-02-28_*_agent-swarm-primitives-v*.md` — the old `feature/agent-swarm` branch reached ~85% implementation of v5 before being abandoned for scope. Key reusable patterns documented in v3.
3. **Old branch reference code**: `git show feature/agent-swarm:harness/internal/swarm/` — state machine, enums, handoffs, result parsing. Use `git show feature/agent-swarm:<path>` to pull reference.

## Recent Changes

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — new v3 plan document (entire file)

## Learnings

### Pi-Mono API Surface (Verified Against v0.54.0 at `/opt/openclaw/`)

- **Model names are different than expected**: `getModel('openai-codex', 'gpt-5.3-codex')` — NOT `codex-5.3`. Available under `openai-codex` provider: `gpt-5.1`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`, `gpt-5.2`, `gpt-5.2-codex`, `gpt-5.3-codex`, `gpt-5.3-codex-spark`.
- **`createReadOnlyTools(cwd)`** from `@mariozechner/pi-coding-agent` returns `[readTool, grepTool, findTool, lsTool]` — production tools with built-in truncation, sandboxed to cwd. No custom `lib/tools.js` needed. See `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/sdk.d.ts`.
- **`Agent` class** from `@mariozechner/pi-agent-core`: supports multi-turn conversations, `prompt()` / `continue()` / `steer()` / `followUp()`. Tool execution is synchronous from agent's perspective — a tool's async `execute()` can block arbitrarily (perfect for `ask_orchestrator`).
- **`AgentTool` interface**: `{ name, label, description, parameters: TypeBox schema, execute: async (id, params, signal?, onUpdate?) => AgentToolResult }` where `AgentToolResult = { content: [{type: "text", text: "..."}], details: any }`.
- **`Agent.subscribe(fn)`**: receives `AgentEvent` — includes `tool_execution_start` (with toolName, args) and `tool_execution_end`. We stream these to Go for SSE dashboard.
- **`transformContext`**: optional hook on Agent constructor that can prune messages before LLM calls. Safety valve for context window pressure.
- **Installed packages**: `pi-agent-core`, `pi-ai`, `pi-coding-agent`, `pi-tui` — all at v0.54.0.
- **ExtensionAPI** (`registerTool`, `registerCommand`, etc.) is for extending interactive pi/OpenClaw sessions — NOT applicable to our subprocess agents. We use the raw `Agent` class directly.

### Bidirectional JSONL Protocol Design

- The `ask_orchestrator` tool works because pi-agent-core's tool `execute` is async — it can block waiting for stdin. From the agent's perspective, it's just another tool call. From Go's perspective, it's reading a question line from stdout and writing an answer line to stdin.
- The `submit_artifact` tool validates the artifact schema before accepting. If invalid, it returns an error to the agent which retries. Only valid submissions cause the subprocess to write a `{"type":"result",...}` line and exit.
- Go's `runAgent()` method runs a `bufio.Scanner` loop reading stdout JSONL lines, dispatching `event` → EventBus, `question` → answerQuestion(), `result` → return.

### Context Management Principles

- **Compress at every boundary**: each agent returns summaries with file:line references, never raw file contents.
- **Skills on disk, not in system prompts**: agents load `harness/agents/skills/*.md` as needed via `read_file`.
- **No tool call caps**: monitor via SSE dashboard. Tune system prompts if agents are pathological.
- **Go controls what each agent sees**: Go marshals exactly what the agent needs as the initial task JSON.
- **CLAUDE.md should be lightweight**: reference skills rather than embedding all domain knowledge.

### Old Branch (`feature/agent-swarm`) Key Patterns

- **State machine in SQLite, not Temporal** — `DetermineNextPhase()` at `harness/internal/swarm/statemachine.go` is deterministic Go code. Temporal is a durable scheduler only.
- **Hook-based completion** — `CompletionRegistry` with per-session channels, not tmux polling.
- **Handoff documents** — primary context transfer between sessions at `thoughts/swarm/handoffs-*/`.
- **Typed enums** — `Phase`, `SessionResult`, `WorkflowStatus`, `WorkflowType`, `EventType`, `GateAction`, etc. at `harness/internal/swarm/enums.go`.
- **Two task queues** — `swarm-general` (concurrency 3) + `swarm-verify` (concurrency 1) for OOM prevention.
- **Workflow IDs** — `swarm-{agentIdx}-{ticketID}` for observability.
- **v5 continuous learning** — plan at `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`. Not in scope for v3 but informs future design.

## Artifacts

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — **THE v3 PLAN** (read this)
- `thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md` — v2 plan (superseded by v3)
- `thoughts/CoreyCole/handoffs/general/2026-03-07_15-31-10_agent-primitives-v2-plan-design.md` — v2 handoff

## Action Items & Next Steps

1. **Continue refining the v3 plan** — the user wants to keep iterating before implementation. Open questions from the plan:
   - How smart should `answerQuestion` be? v1: keyword matching + skill loading. Future: spawn another agent.
   - What if `submit_artifact` never gets called? Need subprocess timeout.
   - `OPENAI_API_KEY` sourcing — `.env` as env var passed to subprocess?
   - Node.js binary path — system node? nix node?
   - Parallel agent limits — 5 research agents simultaneously OK for VPS RAM?
   - `createReadOnlyTools(cwd)` path traversal safety — need to verify.
   - Error propagation when agent subprocess crashes.

2. **Consider these refinement areas**:
   - Exact system prompt text for each agent (currently described conceptually, not written)
   - Skill file content — what goes in `database-conventions.md`, `api-conventions.md`, etc.
   - Agent-to-agent context: when the synthesizer receives findings, should it also get the sub-questions for structure?
   - Temporal activity heartbeat pattern during long `ask_orchestrator` waits
   - Dashboard templ component design for live tool activity

3. **When ready to implement**, follow the 5 phases in the v3 plan:
   - Phase 1: Foundation (DB, Temporal SDK, agent libs, skills)
   - Phase 2: Agent Scripts (6 scripts + standalone testing)
   - Phase 3: Temporal Workflows + Activities
   - Phase 4: HTTP API + SSE
   - Phase 5: Dashboard

## Other Notes

### Key Codebase Entry Points

- `harness/internal/events/bus.go` — EventBus to extend with swarm events
- `harness/internal/events/types.go` — event type constants
- `harness/internal/server/server.go:107-230` — `RegisterRoutes()` where swarm routes go
- `harness/internal/server/mayor_dashboard.go` — SSE dashboard pattern to follow
- `harness/internal/db/db.go` — migration registration (`migrationFiles` slice)
- `harness/views/mayor/dashboard.templ` — templ dashboard pattern to follow
- `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/agent.d.ts` — Agent class types
- `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/sdk.d.ts` — createReadOnlyTools, createAgentSession exports
- `/opt/openclaw/node_modules/@mariozechner/pi-ai/dist/models.generated.js` — available models (use `node --input-type=module -e "import { MODELS } from '...'; console.log(Object.keys(MODELS['openai-codex']))"`)

### User-Provided Pi-Mono Extension Examples

The user provided 4 examples of pi-mono extensions using the `ExtensionAPI` pattern (`registerTool`, `registerCommand`, `appendEntry`, `on()`). These are for interactive sessions (TUI) and don't apply to our subprocess agents. However, the tool definition pattern (TypeBox schemas, `execute` returning `{content, details}`) is the same — see the `AgentTool` interface we use.

### Branch Context

- Working on `feat/agent-primitives` branch
- Old implementation lives on `feature/agent-swarm` (local + remote)
- Use `git show feature/agent-swarm:<path>` to pull reference code
- All v1-v5 plan documents exist on both branches

### Document Evolution

v1 plan → v1 review → v2 plan → v2 handoff → v3 plan → **v3 handoff (this document)**

Prior chain (old branch): v1 → v2 → v3 → v4 → v5 (with reviews at each step). See `thoughts/CoreyCole/plans/2026-02-28_*` for full history.
