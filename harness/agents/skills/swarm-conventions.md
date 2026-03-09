---
name: swarm-conventions
description: Swarm orchestration system - JSONL protocol, Temporal workflows, agent scripts, output format
tags: [swarm, agent, protocol, workflow, orchestration]
last_verified: 2026-03-09
---

# Swarm Conventions

## Overview

The swarm system orchestrates AI agent tasks (research, planning) using Temporal workflows that spawn JS agent subprocesses. Agents communicate with the Go harness via a bidirectional JSONL protocol over stdin/stdout.

## JSONL Protocol

### Go → Agent (stdin)

**Start message** (sent once at launch):
```json
{"type":"start","task":{...},"systemPrompt":"...","projectContext":"...","config":{"model":"provider:model"}}
```

**Answer message** (response to agent question):
```json
{"type":"answer","id":"<questionID>","text":"<context from grep>"}
```

### Agent → Go (stdout)

**Event messages** (tool/inference lifecycle):
```json
{"type":"event","event":"tool_execution_start","tool":"read_file","data":{...},"toolCallID":"tc_1"}
{"type":"event","event":"tool_execution_end","tool":"read_file","data":{...},"toolCallID":"tc_1"}
{"type":"event","event":"inference_start","data":{"model":"gpt-5.3-codex"}}
{"type":"event","event":"inference_end","data":{"model":"...","usage":{...}}}
```

**Question messages** (agent asks orchestrator for context):
```json
{"type":"question","id":"q_1","text":"How does the build pipeline work?"}
```

**Heartbeat messages** (liveness):
```json
{"type":"heartbeat"}
```

## Agent Scripts

Located in `harness/agents/`:

| Script | Purpose | Workflow |
|--------|---------|----------|
| `research-questions.js` | Decompose request into sub-questions | Research |
| `research-agent.js` | Investigate a single question | Research |
| `research-synthesizer.js` | Combine findings into a document | Research |
| `plan-orchestrator.js` | Classify domains for planning | CodeChangePlan |
| `specialist-planner.js` | Create plan for a specific domain | CodeChangePlan |
| `plan-synthesizer.js` | Combine plans into final document | CodeChangePlan |

All agents use the shared factory in `harness/agents/lib/agent-factory.js`.

## Output Format

Agent output files use YAML frontmatter + markdown body:

```yaml
---
question: "How does X work?"
findings: "Detailed findings here..."
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/db/queries/worlds.sql
confidence: high
---

# Research: How does X work?

Detailed markdown content...
```

## Output Paths

All output goes under `thoughts/swarm/`:

| Category | Path Pattern | Content |
|----------|-------------|---------|
| Research questions | `research-questions/<timestamp>_<slug>.yaml` | Decomposed sub-questions |
| Research findings | `research-findings/<timestamp>_<slug>.md` | Per-question findings |
| Research synthesis | `research/<timestamp>_<slug>.md` | Combined research doc |
| Plan classifications | `plan-classifications/<timestamp>_<slug>.yaml` | Domain classification |
| Specialist plans | `specialist-plans/<timestamp>_<domain>.md` | Per-domain plan |
| Project plans | `project-plans/<timestamp>_<slug>.md` | Final synthesized plan |

## Key Source Files

| File | Purpose |
|------|---------|
| `harness/internal/swarmorch/workflows.go` | Temporal workflow definitions |
| `harness/internal/swarmorch/activities.go` | Activity implementations |
| `harness/internal/swarmorch/agent.go` | Agent subprocess management, JSONL loop |
| `harness/internal/swarmorch/types.go` | Protocol message types, input/output structs |
| `harness/internal/swarmorch/manager.go` | SwarmManager: Temporal client, worker lifecycle |
| `harness/internal/swarmorch/context.go` | Project context loading for agent injection |
| `harness/internal/swarmorch/artifact.go` | Output file parsing (YAML frontmatter + markdown) |

## Temporal Integration

- Task queue: `swarm-agents`
- Workflows: `ResearchWorkflow`, `CodeChangePlanWorkflow`
- Activity struct: `SwarmActivities` (holds DB, EventBus, runner, config)
- Activity timeouts: 20min start-to-close, 60s heartbeat
- Agents heartbeat on every stdout line read

## Spans (Tracing)

Hierarchical spans stored in `swarm_spans` table:
- `workflow` → `stage` → `agent` → `tool_call` / `llm_call` / `question`
- Created/completed via helper functions in `agent.go`
- Aggregate metadata (token counts, cost) rolled up to agent spans
