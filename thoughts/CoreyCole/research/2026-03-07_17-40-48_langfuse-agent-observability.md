---
date: 2026-03-07T17:40:48-08:00
researcher: CoreyCole
git_commit: 773d305ee138bceac6e3b412f627dbf37bc5374f
branch: feat/agent-primitives
repository: creative-mode
topic: "Langfuse Agent Observability Features"
tags: [research, langfuse, observability, agent-tracing, tool-calls, evaluation, rag]
status: complete
last_updated: 2026-03-07
last_updated_by: CoreyCole
---

# Research: Langfuse Agent Observability Features

**Date**: 2026-03-07T17:40:48-08:00
**Researcher**: CoreyCole
**Git Commit**: 773d305ee138bceac6e3b412f627dbf37bc5374f
**Branch**: feat/agent-primitives
**Repository**: creative-mode

## Research Question

What features does Langfuse provide for agent observability? Specifically: tool calls, context used, evaluation, multi-step traces, and how the data model works.

## Summary

Langfuse is an open-source (MIT) LLM observability platform with deep agent-specific features. Its data model is hierarchical: **Sessions** group **Traces**, which contain typed **Observations** (10 types including AGENT, TOOL, RETRIEVER, GUARDRAIL). Tool calls are automatically extracted from raw LLM input/output during ingestion — no SDK changes needed. RAG context is tracked via the RETRIEVER observation type. Evaluation supports LLM-as-a-Judge, human annotation queues, and programmatic API scoring. Agent execution graphs are auto-inferred from observation nesting. 30+ framework integrations with no vendor lock-in. A full Langfuse repo is cloned at `context/langfuse/` for reference.

## Detailed Findings

### 1. Core Data Model

**Hierarchy**: Session → Trace → Observation (nested tree)

| Entity | Purpose | Key Fields |
|--------|---------|------------|
| **Session** | Groups traces (e.g., chat thread) | id, projectId, environment, bookmarked |
| **Trace** | Single request/operation | id, name, userId, sessionId, input, output, metadata, tags, release, version, environment |
| **Observation** | Individual step within a trace | id, traceId, parentObservationId, type, name, startTime, endTime, input, output, model, usage, cost, level, metadata |
| **Score** | Quality metric on any entity | name, value, dataType (NUMERIC/CATEGORICAL/BOOLEAN/CORRECTION), source (API/EVAL/ANNOTATION), traceId, observationId, sessionId |

**10 Observation Types** (`context/langfuse/packages/shared/src/domain/observations.ts:5-16`):

| Type | Semantic Meaning |
|------|-----------------|
| SPAN | Generic unit of work / duration |
| EVENT | Discrete occurrence (no duration) |
| GENERATION | LLM model call with prompt/completion |
| **AGENT** | Application flow decisions using tools with LLM guidance |
| **TOOL** | Tool invocation (API call, function execution) |
| **CHAIN** | Link between application steps |
| **RETRIEVER** | Data retrieval step (vector store query, RAG) |
| EMBEDDING | Embedding generation call |
| EVALUATOR | Assessment function (relevance, correctness) |
| GUARDRAIL | Content safety / jailbreak protection |

All types except SPAN and EVENT are "generation-like" — they can carry model, usage, cost, and prompt data.

**Dual Database Architecture**: PostgreSQL (Prisma) for relational data + ClickHouse for high-volume analytics. ClickHouse tables use `ReplicatedReplacingMergeTree` with ZSTD compression. The ClickHouse schema is the source of truth for new features.

### 2. Tool Call Tracking

Tool calls are **automatically extracted** from observation `input`/`output` during worker-side ingestion — no explicit SDK field needed.

**Extraction logic** (`context/langfuse/packages/shared/src/server/ingestion/extractToolsBackend.ts`):

**From input (tool definitions)**:
- OpenAI format: `{tools: [{type: "function", function: {name, description, parameters}}]}`
- Flat format: `{tools: [{name, description, parameters}]}`
- Per-message tools arrays
- LangGraph `role:"tool"` messages

**From output (tool calls/invocations)**:
- Direct: `output.tool_calls[]`
- OpenAI: `output.choices[].message.tool_calls[]`
- Anthropic: `output.content[].type === "tool_use"`
- LangChain: `output.additional_kwargs.tool_calls[]`
- Message arrays with tool_calls

**Storage in ClickHouse** (migration 0033):
- `tool_definitions: Map(String, String)` — key=tool name, value=JSON `{description, parameters}`
- `tool_calls: Array(String)` — JSON strings of `{id, arguments, type, index}`
- `tool_call_names: Array(String)` — parallel array of names for efficient `has()` filtering

**UI Visualization**:
- `ToolCallDefinitionCard` renders each tool as an expandable card with name, description, parameters, and call status ("not called" / "called" / "called Nx")
- `ToolCallInvocationsView` renders individual invocations with wrench icon, tool name, call ID, and arguments
- Tool definitions are prominently displayed at the top of each LLM generation view

### 3. RAG Context Tracking

No dedicated "context" field exists. RAG context is tracked through:

1. **RETRIEVER observation type** — wrap your retrieval step with `as_type="retriever"`. Input = query, output = retrieved documents/chunks, plus timing and metadata.
2. **Arbitrary JSON in input/output** — any observation's input/output can hold structured document data.
3. **Metadata maps** — `Map(LowCardinality(String), String)` for key-value context metadata.

**RAG-specific evaluation** is supported via:
- Faithfulness (answer supported by documents?)
- Groundedness (claims traceable to sources?)
- Relevance (answer addresses the question?)
- RAGAS integration for comprehensive RAG quality assessment
- Component-level evaluation (test chunking, retrieval independently)

### 4. Evaluation / Scoring System

**Score Data Types**: NUMERIC (float with optional min/max), CATEGORICAL (from defined set), BOOLEAN (0/1), CORRECTION (output corrections with `longStringValue`).

**Score Sources**: API (programmatic), EVAL (LLM-as-a-Judge), ANNOTATION (human in UI).

**Four evaluation methods**:

| Method | Description |
|--------|-------------|
| **LLM-as-a-Judge** | Automated evaluation using GPT-4o/Claude/Gemini. Configure evaluator, select judge model, map variables via JSONPath. 80-90% agreement with human evaluators. |
| **Manual UI Scoring** | Quick spot checks via "Annotate" button on trace/observation detail views |
| **Annotation Queues** | Structured human review workflows with Score Configs, user assignment, "Complete + next" workflow. API-manageable. |
| **API/SDK Scoring** | `langfuse.score(traceId, name, value)` — attach scores programmatically |

Scores can target: traces, observations, sessions, or dataset runs.

**Score Analytics dashboard**: Compare score distributions, time series trends, heatmaps, consistency checks between human and automated evaluators.

### 5. Multi-Step Agent Traces

**Parent-child nesting**: Each observation has `parentObservationId` linking to its parent. The `traceId` field links all observations to their containing trace. The UI builds a tree via `nestObservations()` (`context/langfuse/web/src/components/trace2/lib/helpers.ts:12-79`).

**Agent Graphs (GA Nov 2025)**: Visual DAG representations of agent workflows. Auto-inferred from observation timing and nesting — no manual configuration. Two modes:
1. **Observation-based**: Include AGENT/TOOL/CHAIN/RETRIEVER types and the graph generates automatically
2. **LangGraph**: Step/node data drives the graph natively

**Three trace navigation modes**:
1. **Tree view** — hierarchical nesting (default)
2. **Timeline view** — Gantt chart of observation durations
3. **Graph view** — DAG visualization for agent workflows

**Trace Log View**: Concatenated scrollable view of all observations — keyword searchable across entire traces. Valuable for complex agent flows.

### 6. Cost / Latency Tracking

**Token tracking fields per observation**:
- `promptTokens`, `completionTokens`, `totalTokens`
- `usageDetails: Map(String, UInt64)` — flexible keys: `input_cached_tokens`, `output_reasoning_tokens`, `audio_tokens`, etc.
- `costDetails: Map(String, Decimal64)` — per-model/category cost breakdown

**Two-tier cost**: User-provided costs are prioritized; otherwise Langfuse infers using built-in tokenizers (OpenAI cl100k/o200k, Anthropic native, Gemini).

**`completionStartTime`**: Time-to-first-token marker for latency analysis.

**Aggregation**: Costs and usage roll up from observations → trace → session with per-model breakdowns.

### 7. Prompt Management

- Version control with integer versions
- Deployment labels (production, staging, etc.)
- Client-side caching (SDK-level, fast retrieval)
- Prompt-to-trace linking (analyze performance by prompt version)
- **Prompt composition**: Recursive `@@@langfusePrompt:name=X|version=Y@@@` replacement with max depth 5 and circular dependency detection
- Side-by-side comparison and diff dialog
- MCP server for AI agents to fetch/update prompts directly
- Config support (temperature, max_tokens alongside prompt text)

### 8. SDK & API Integration

**Ingestion endpoints**:
- `POST /api/public/ingestion` — legacy batch endpoint (HTTP 207 multi-status)
- `POST /api/public/otel/v1/traces` — OpenTelemetry endpoint (protobuf + JSON, preferred)

**Processing pipeline**: API validation → S3 upload → BullMQ queue → Worker merges events → ClickHouse write. Async, non-blocking.

**OTel Type Mapping** (`context/langfuse/packages/shared/src/server/otel/ObservationTypeMapper.ts`): Priority-based chain maps OTel spans to Langfuse types:
- `langfuse.observation.type` attribute (direct)
- OpenInference `span.kind` (AGENT→AGENT, TOOL→TOOL, LLM→GENERATION)
- GenAI `operation.name` (invoke_agent→AGENT, execute_tool→TOOL)
- Vercel AI SDK, LiveKit, model-based fallbacks

**30+ Framework Integrations**: LangChain, LangGraph, OpenAI Agents SDK, Claude Agent SDK, CrewAI, Google ADK, Pydantic AI, LlamaIndex, Vercel AI SDK, Haystack, DSPy, AutoGen, SmolAgents, Spring AI, Semantic Kernel, and more.

**No Go SDK** — use OpenTelemetry or REST API directly for Go applications.

### 9. Dashboard / UI Features

**Trace detail**: Name, timestamp, latency, session link, user ID, environment, release, cost badge, usage badge, tags, I/O preview (formatted ChatML + JSON toggle), scores tab, log view tab.

**Observation detail**: All trace-level info plus model, model parameters, time-to-first-token, level (DEBUG/WARNING/ERROR), status message, linked prompt, "Jump to Playground" button.

**Filtering/search**: Environment, trace name, user ID, session ID, metadata key-value, version/release, bookmark, tags, level, latency range, token ranges, cost ranges, score ranges. Full-text search by ID or metadata. Within-trace observation search.

**Custom dashboards**: Build analytics views with configurable widgets.

### 10. Comparison with Alternatives

| Dimension | Langfuse | LangSmith | Arize Phoenix | Braintrust |
|-----------|----------|-----------|---------------|------------|
| Open Source | Yes (MIT) | No | Yes | No |
| Self-hosting | Docker/K8s | No | Yes | No |
| Agent focus | 10 obs types, agent graphs | Deep LangChain integration | OTel-based | Eval-focused |
| Framework lock-in | None (OTel) | LangChain affinity | None (OTel) | None |
| Free tier | 1M spans/mo cloud; unlimited self-host | Per-trace pricing | Free self-hosted | Managed pricing |

**Langfuse strengths**: Broadest integrations, open source, purpose-built agent types, free self-hosted.
**Weaknesses**: Self-hosting requires PostgreSQL + ClickHouse + Redis + S3 (~$3-4k/mo infra). No Go SDK. CI/CD eval blocking less mature than Braintrust.

## Code References

- `context/langfuse/packages/shared/src/domain/observations.ts:5-16` — ObservationType enum (10 types)
- `context/langfuse/packages/shared/src/server/ingestion/extractToolsBackend.ts` — Tool call extraction from raw I/O
- `context/langfuse/packages/shared/clickhouse/migrations/clustered/0033_add_tool_call_columns.up.sql` — Tool call ClickHouse columns
- `context/langfuse/packages/shared/prisma/schema.prisma:332-413` — Trace and Observation Prisma models
- `context/langfuse/packages/shared/src/server/ingestion/types.ts:250-270` — Event type constants (all 18 event types)
- `context/langfuse/packages/shared/src/server/otel/ObservationTypeMapper.ts:165-468` — OTel span type mapping chain
- `context/langfuse/worker/src/services/IngestionService/index.ts:904-919` — Worker-side tool extraction wiring
- `context/langfuse/web/src/components/trace2/lib/helpers.ts:12-79` — Tree nesting algorithm
- `context/langfuse/web/src/components/trace2/components/IOPreview/components/ToolCallDefinitionCard.tsx` — Tool definition UI cards
- `context/langfuse/web/src/components/trace2/components/ToolCallInvocationsView.tsx` — Tool call invocation UI
- `context/langfuse/web/src/features/trace-graph-view/components/TraceGraphView.tsx` — Agent graph visualization
- `context/langfuse/packages/shared/src/domain/scores.ts:4-27` — Score types and sources
- `context/langfuse/packages/shared/src/server/services/PromptService/index.ts` — Prompt resolution with composition
- `context/langfuse/web/src/components/trace2/components/SpanContent.tsx` — Tree node content with cost/usage heat maps

## Architecture Insights

1. **Single polymorphic observations table**: All 10 observation types share one ClickHouse table, distinguished by `type` column. Generation-specific fields (model, usage, cost) are nullable on non-generation types. This simplifies querying but means the schema is wide.

2. **Tool extraction as post-processing**: Tool definitions and calls are NOT part of the ingestion schema. They are extracted from raw `input`/`output` JSON by pattern-matching against known LLM API formats (OpenAI, Anthropic, LangChain, LangGraph, Vercel). This is powerful — existing instrumentation automatically gets tool visibility without SDK changes.

3. **Event sourcing with S3 durability**: All ingestion uses append-only events uploaded to S3 before queueing. Worker merges events using last-writer-wins semantics. This provides durability, replay capability, and allows async processing.

4. **Dual write paths converge**: The legacy REST endpoint and the OTel endpoint both write to the same ClickHouse tables through different processing pipelines, but with identical final schemas.

5. **Flexible cost/usage maps**: ClickHouse uses `Map(String, number)` instead of fixed columns for usage/cost, allowing arbitrary breakdown keys (cached tokens, reasoning tokens, audio tokens, etc.) without schema migrations.

## Relevance to Creative Mode

For observing our OpenClaw mayor agents and Claude Code build sessions:
- **No Go SDK** means we'd use the REST API or OTel endpoint directly from the harness
- The **AGENT/TOOL observation types** map well to mayor skill invocations and build actions
- **Session grouping** could track multi-turn mayor conversations
- **Cost tracking** would help monitor per-world Claude API spend
- **Tool call extraction** would automatically capture mayor tool usage from raw LLM I/O
- **Evaluation** could score mayor responses and build quality
- Self-hosting requires significant infra (PostgreSQL, ClickHouse, Redis, S3) — cloud tier at $249/mo or 1M free spans may be more practical

## Open Questions

1. How well does the OTel endpoint work for Go applications without a native SDK? What's the ergonomics overhead?
2. Can Langfuse handle the volume of traces from concurrent Claude Code build sessions without significant latency?
3. Is the self-hosted infrastructure overhead justified vs. the cloud free tier (1M spans/mo)?
4. How would we integrate with OpenClaw's existing logging — would we instrument at the harness level or the OpenClaw agent level?
5. Could Langfuse's prompt management replace our current SOUL.md/MEMORY.md workspace files for mayor prompts?
