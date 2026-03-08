---
date: 2026-03-08
reviewer: CoreyCole
source_plan: thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md
source_research: thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md
branch: feat/agent-primitives
type: staff_review_summary
---

# Agent Primitives v3 — Staff Engineering Review Summary

## What This System Does

A swarm of LLM agents (pi-mono + gpt-5.3-codex) investigates codebases and produces research documents and code change plans. Users trigger tasks from a browser dashboard and watch agents work in real time.

**Scope**: Only Primitive 1 (Research) and Primitive 2 (Code Change Plan) — document generation only, no code changes applied.

## System Architecture (One Diagram)

```
Browser ──SSE──→ Datastar (Span lifecycle events)
                     ↑
                  EventBus ("swarm" topic)
                     ↑
               ┌─────┴──────┐
               │  Temporal   │
               │  Workflows  │──→ SQLite (swarm_tasks, swarm_spans, swarm_artifacts)
               └─────┬──────┘
                     │
              ┌──────┴──────┐
              │  Go Activity │  ←── bidirectional JSONL stdin/stdout
              │  runAgent()  │
              └──────┬──────┘
                     │
              ┌──────┴──────┐
              │  Node.js     │  ←── pi-mono agent (gpt-5.3-codex)
              │  subprocess  │      read-only file tools + ask_orchestrator + submit_artifact
              └─────────────┘
```

## 8 Key Implementation Decisions

### 1. Temporal as durable scheduler, NOT state machine

State lives in SQLite. Temporal provides retry, fan-out, and crash recovery. No workflow state is stored in Temporal's history — avoids non-deterministic replay bugs that killed the v1 attempt (350+ files, collapsed under scope).

**Trade-off**: Extra DB writes on every state transition. Acceptable at our scale.

### 2. Bidirectional JSONL protocol (not fire-and-forget)

Agents are conversational — they can ask the Go orchestrator follow-up questions via `ask_orchestrator` tool, blocking until Go responds. The workflow step only completes when the agent calls `submit_artifact` with a schema-valid output.

**Why it matters**: Agents that get stuck can ask for help instead of hallucinating. Go can load skill files, grep the codebase, and feed context back without the agent burning tokens re-reading files.

**Risk**: A chatty agent could loop on questions. Mitigated by 10-minute `StartToCloseTimeout` + 3 retry max.

### 3. Hierarchical spans (Langfuse data model) instead of flat events

The original v3 design had a flat `swarm_events` table with 7 domain-specific event types (`agent.tool_call`, `research.questions_generated`, etc.), each requiring custom SSE handler code and templ components.

**New design**: Single `swarm_spans` table with `parent_span_id` for tree nesting. Six span types: `workflow`, `stage`, `agent`, `tool_call`, `llm_call`, `question`. Three SSE events (`span.started`, `span.completed`, `span.failed`) drive one polymorphic `SpanRow` component.

**Why Langfuse's model**: Adding new span types (e.g., `llm_call` when pi-mono exposes generation events) requires zero SSE handler changes. The tree structure shows causality (which agent made which tool calls) rather than just chronology.

**What we skip from Langfuse**: ClickHouse analytics, cost/token tracking, LLM-as-a-Judge eval, prompt management, multi-tenant. All deferred to Phase 6.

### 4. Custom SSE observability instead of self-hosting Langfuse

Langfuse self-hosted requires PostgreSQL + ClickHouse + Redis + S3 (~$3-4k/mo). No Go SDK — we'd use REST or OTel directly. Cloud tier is $249/mo or 1M free spans.

**Our advantage**: Real-time streaming. Langfuse ingests asynchronously and shows traces after-the-fact. Our dashboard shows agents working *live* — tool calls appearing as they happen, parallel agents progressing simultaneously. This is a debugger, not a log viewer.

**Our disadvantage**: No analytics, no cross-trace aggregation, no scoring. Acceptable for v1 — we're debugging agent behavior, not running production evals.

### 5. Dual-write spans: SQLite + EventBus

Every span lifecycle event (create, complete, fail) writes to `swarm_spans` in SQLite AND publishes to `EventBus("swarm")`. SQLite provides persistence for page-load rendering of completed traces. EventBus provides live streaming for in-progress traces.

**Why not EventBus-only**: Browser refresh would lose all trace history. The mayor dashboard has this same problem — it re-queries DB on any event.

**Why not SQLite-only with polling**: Defeats the purpose of SSE. Mayor dashboard proved the EventBus → SSE pattern works.

### 6. Pi-mono agents with `DirectRunner` (no sandbox v1)

Agents run as plain `node` subprocesses via `exec.CommandContext`. Pi-mono's `createReadOnlyTools(cwd)` has no path traversal protection — absolute paths and `../../` work.

**Acceptable because**: Agents are our code (not user-submitted), already have `OPENAI_API_KEY` in env, and the codebase is what they're supposed to read. The `AgentRunner` interface abstracts command construction so `BwrapRunner` (bubblewrap sandbox, verified working) slots in later without touching JSONL protocol or activity logic.

**Production path**: Container isolation (ECS/K8s) replaces bwrap — container IS the sandbox.

### 7. Compression at every pipeline boundary

Each stage summarizes, never passes raw file contents forward:

```
User Question (~100 tokens)
  → Question Generator → 3-5 sub-questions (~500 tokens)
  → Research Agents (parallel) → compressed findings per agent (~1-2K each)
  → Synthesizer → research document (~3-5K tokens)
  → Plan Orchestrator → specialist assignments (~500 tokens)
  → Specialist Planners (parallel) → plan sections (~2-4K each)
  → Plan Synthesizer → unified plan document (~5-10K tokens)
```

Agents produce summaries with `file:line` references, not raw code. This prevents context window blowup and keeps token costs bounded.

### 8. Span data truncated at 4KB

Tool args and results (especially `read_file` output) can be large. `truncateJSON()` caps `input_json`/`output_json` in `swarm_spans` at 4KB. Full data remains in agent subprocess stdout JSONL logs at `{dataDir}/logs/` for post-hoc debugging.

**Trade-off**: Dashboard I/O preview is lossy for large file reads. Acceptable — the span tree shows *what* was read and *how long* it took, which is what you need for observability. Full content is a click away in log files.

## Implementation Phases

| Phase | Scope | Key Risk |
|-------|-------|----------|
| **1. Foundation** | Migration 006 (swarm_spans), SQLC, Temporal SDK, agent libs, skill files, event types | Migration correctness — recursive CTE for span tree query |
| **2. Agent Scripts** | 6 JS scripts tested standalone with manual JSON | Prompt quality — agents may not produce valid artifacts |
| **3. Temporal + Spans** | Workflows, activities, `runAgent()` with span instrumentation, dual-write helpers | JSONL protocol edge cases (agent crash mid-span, orphaned tool spans) |
| **4. HTTP API + SSE** | Swarm API, SSE handler, route registration | SSE connection lifetime — EventBus drops events if subscriber is slow |
| **5. Dashboard** | Three-view trace detail (tree, timeline, log), task list | templ component complexity — span tree rendering with live updates |
| **6. Future** | Bwrap sandbox, cost tracking, scoring, DAG visualization | — |

## Things I'd Push Back On (Review Questions)

1. **`answerQuestion` v1 is keyword matching** — this is the weakest link. If agents ask questions the keyword matcher can't answer, they'll either waste tool calls re-investigating or submit low-quality artifacts. Consider: should `answerQuestion` just run another agent subprocess? That's the "spawn another agent" future mentioned in resolved question #9, but it might need to be v1.

2. **No tool call caps** — the plan says "monitor agent behavior via SSE dashboard." But if an agent enters a pathological grep/read loop, it burns tokens for 10 minutes before timeout. Consider: a soft cap (log warning at 50 tool calls, kill at 100) is cheap and prevents runaway API spend.

3. **Single `swarm-agents` task queue** — all 6 agent types share one queue with concurrency 4. A CodeChangePlanWorkflow runs ResearchWorkflow as a child, which fans out 3-5 research agents. If a user submits 2 tasks simultaneously, that's 6-10 agents competing for 4 slots. Consider: is 4 the right concurrency for the VPS (31GB RAM, ~200MB per Node.js process)?

4. **`swarm_spans.input_json` / `output_json` are TEXT** — storing JSON in SQLite TEXT columns works but prevents efficient queries like "find all spans where tool=grep and args contained 'EventBus'". If analytics become important, consider a `metadata_json` field with indexed extracted keys, or accept that analytics queries will be slow table scans.

5. **Three remaining open questions** — system prompt text, skill file content, and Temporal heartbeat pattern are all unresolved. Prompts and skills are the entire agent behavior — they can't be deferred much longer.

## Files That Will Be Created/Modified

**New files** (~20):
- `harness/db/migrations/006_swarm_tables.sql`
- `harness/db/queries/swarm.sql` (SQLC)
- `harness/internal/temporal/{client,activities,workflows,worker}.go`
- `harness/internal/server/{swarm_api,swarm_sse,swarm_dashboard}.go`
- `harness/views/swarm/{dashboard,task_detail,span_row,timeline,components}.templ`
- `harness/agents/{package.json,lib/*.js,*.js,skills/*.md}`
- `harness/internal/events/types.go` (modified — add 5 event constants)

**Modified files** (~5):
- `harness/internal/db/db.go` (register migration 006)
- `harness/internal/server/server.go` (register routes)
- `harness/main.go` (Temporal client init, worker start)
- `harness/go.mod` / `harness/go.sum` (Temporal SDK)
- `flake.nix` (temporal-cli)
