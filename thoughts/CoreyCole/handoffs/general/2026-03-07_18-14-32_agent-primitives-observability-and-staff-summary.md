---
date: 2026-03-08T02:14:32-08:00
researcher: CoreyCole
git_commit: 9ff08b87c7c03e1ddc48825343a14462be902c14
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives v3 — Observability Design + Staff Review Summary"
tags: [implementation, strategy, swarm, agent-primitives, observability, langfuse, sse, spans, staff-review]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives — Observability Design Added to v3 Plan + Staff Summary Written

## Task(s)

1. **Incorporate Langfuse-inspired observability into the v3 plan** — Status: **complete**. Replaced the flat `swarm_events` table with a hierarchical `swarm_spans` table (parent-child nesting), redesigned SSE events from 7 domain-specific types to 3 span lifecycle events, added three dashboard views (tree, timeline, event log), and updated all dependent sections (JSONL protocol, `runAgent()`, implementation phases, resolved questions).

2. **Write staff engineering review summary** — Status: **complete**. Distilled the 1500-line v3 plan into a concise review document with 8 key decisions (with trade-offs), 5 pushback items, implementation phases with risks, and a file manifest.

3. **Final staff eng review of the plan** — Status: **next step**. The summary document is ready for review. The next session should conduct the actual staff-level review against the updated plan.

## Critical References

1. **v3 plan (authoritative, updated with observability)**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
2. **Staff review summary**: `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md`
3. **Langfuse research**: `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md`

## Recent changes

No code changes. Three document updates:

- Updated `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`:
  - Replaced `swarm_events` table with `swarm_spans` table (lines ~678-710)
  - Added new "Observability Design (Langfuse-Inspired)" section (lines ~926-1113)
  - Replaced "Dashboard" section with "Dashboard (Langfuse-Inspired Views)" section (lines ~1115-1232)
  - Updated SSE Events section to span-based events (lines ~878-925)
  - Updated JSONL protocol to include `toolCallId` on tool events (lines ~103-112)
  - Updated `lib/protocol.js` `sendEvent` signature (line ~772)
  - Updated `lib/agent-factory.js` to forward `toolCallId` in events (lines ~863-870)
  - Updated implementation phases with span instrumentation details (lines ~1402-1460)
  - Added resolved questions 11-13 for observability decisions (lines ~1502-1514)
  - Updated resolved question 8 to reference spans (line ~1487-1488)
- Created `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md` — new file

## Learnings

### Langfuse Data Model Insight
Langfuse's core insight is that a single polymorphic observations table with `parentObservationId` can represent any agent execution topology. We adopted this as `swarm_spans` with `parent_span_id`. The six span types (`workflow`, `stage`, `agent`, `tool_call`, `llm_call`, `question`) cover the full research/plan pipeline. Adding new types later requires zero SSE handler changes — the `SpanRow` templ component is polymorphic.

### SSE Advantage Over Langfuse
Langfuse ingests events asynchronously via a processing pipeline (API → S3 → BullMQ → Worker → ClickHouse). Traces are viewed after-the-fact. Our EventBus → Datastar SSE path shows spans appearing in real time. This is a debugger experience, not a log viewer. This is the key differentiator that justifies building custom observability instead of self-hosting Langfuse.

### Dual-Write Pattern
Every span lifecycle event (create, complete, fail) must write to BOTH SQLite (persistence for page-load rendering) AND EventBus (live streaming for in-progress traces). The mayor dashboard already uses this pattern for builds/activity/messages — re-query DB on any event, patch tabs via SSE.

### `toolCallId` Is Essential
Pi-mono's `AgentEvent` type includes `toolCallId` on `tool_execution_start` events (verified at `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/types.d.ts:161-177`). This ID correlates start/end pairs into spans with accurate duration tracking. The original v3 JSONL protocol didn't forward it — now it does.

### Span Truncation Trade-off
Tool args and results (especially `read_file`) can be many KB. Storing full data in `swarm_spans` would bloat SQLite and make SSE sluggish. 4KB truncation limit is a pragmatic trade-off — the span tree shows *what* was read and *how long*, which is what you need for observability. Full data stays in JSONL log files.

## Artifacts

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — **the updated v3 plan** (1514 lines, updated with observability design)
- `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md` — **staff review summary** (new, ~170 lines)
- `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md` — Langfuse research (unchanged, from prior session)
- `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md` — system prompts & skills plan (unchanged, from prior session)
- `thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md` — prior staff review (unchanged)

## Action Items & Next Steps

1. **Conduct final staff eng review of the updated v3 plan** — use the summary at `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md` as a starting point, then verify claims against the full plan. The summary already identifies 5 pushback items to investigate:
   - `answerQuestion` v1 weakness (keyword matching may be insufficient)
   - No tool call caps (agents could burn tokens for 10 minutes)
   - Task queue concurrency (4 slots for potentially 10 agents)
   - TEXT column analytics limitations on `swarm_spans`
   - 3 remaining open questions (prompts, skills, heartbeat)

2. **Verify the observability sections are internally consistent** — the plan was updated incrementally across multiple edits. Check that:
   - The `swarm_spans` schema matches the span types referenced in `runAgent()` code
   - The SSE event names in the event types section match what's used in the SSE handler code
   - The JSONL protocol changes (`toolCallId`, `data` field rename from `args`) are reflected in both `protocol.js` and the Go-side parsing
   - The implementation phases reference the correct files/sections

3. **After review approval, begin Phase 1 implementation** — migration 006, SQLC queries, Temporal SDK, agent libs, skill files, event types

## Other Notes

### Document Evolution Chain
v1 → v1 review → v2 → v2 handoff → v3 → v3 handoff → v3 refinement → v3 refinement handoff → draft prompts plan → staff review → final prompts plan → **observability design → staff summary (this session)**

### VPS State (Unchanged)
- Temporal dev server running (7233/8233, namespace `swarm`)
- bubblewrap + temporal-cli in flake.nix
- No `OPENAI_API_KEY` in `.env` yet
- No swarm Go code exists on this branch
- No `harness/agents/` directory exists yet

### Key Sections Added to v3 Plan This Session
| Section | Line Range (approx) | Content |
|---------|---------------------|---------|
| `swarm_spans` table | 678-710 | Replaces flat `swarm_events` with hierarchical spans |
| SSE Events (Span-Based) | 878-925 | 3 span events replace 7 domain events |
| Observability Design | 926-1113 | Rationale, Langfuse comparison, span hierarchy, `runAgent()` instrumentation, workflow spans |
| Dashboard (Langfuse Views) | 1115-1232 | Tree/timeline/log views, SSE handler, templ sketches |
| Resolved Questions 11-13 | 1502-1514 | Dashboard design, Langfuse vs custom, truncation |

### Files to Read for Full Context
1. Start with the staff summary: `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md`
2. Then the updated plan sections listed above (targeted reads, not the full 1514 lines)
3. Langfuse research only if reviewing the "custom vs Langfuse" decision
