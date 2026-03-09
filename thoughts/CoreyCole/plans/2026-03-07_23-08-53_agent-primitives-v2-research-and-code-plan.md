# Agent Primitives v2 — Research & Code Plan on Temporal + Pi-Mono

## Overview

Design a minimal agent swarm system that uses **Temporal** for workflow orchestration, **pi-mono** (`@mariozechner/pi-agent-core` + `@mariozechner/pi-ai`) for agent execution powered by **Codex 5.3**, and the existing harness infrastructure (SQLite, EventBus, Datastar SSE) for persistence and observability.

This plan covers **Primitive 1 (Research)** and **Primitive 2 (Code Change Plan)** — the two document-generation primitives. No code changes are performed by the swarm in this phase.

### Lessons From Previous Attempt (`feature/agent-swarm`)

The prior branch tried to build all 7 primitives, a full state machine, learning system, handoffs, Claude Code tmux sessions, Linear project decomposition, and a sophisticated dashboard simultaneously. It shipped 350+ files and collapsed under its own scope. Key takeaways:

1. **Too many primitives at once** — we only need research and code-plan now
2. **Claude Code via tmux was heavy** — pi-mono agents are lighter (API-driven, no tmux session management)
3. **The state machine was good** — `DetermineNextPhase()` and typed enums were solid; we'll simplify and reuse the pattern
4. **The dashboard templ was good** — workflow list, detail page, SSE stream patterns all work; we'll adapt for the simpler scope
5. **Prompt templates were good** — `research.md.tmpl` and `code_plan.md.tmpl` contract patterns are sound; we'll port them for pi-mono

## The 7 Primitives (Full Picture)

| # | Primitive | Scope | This Phase |
|---|-----------|-------|------------|
| 1 | **Research** | Answer questions → research doc | **YES** |
| 2 | **Code Change** | Research → plan doc (no code yet) | **YES (plan only)** |
| 2.1 | Verification | Verify code changes | Future |
| 3 | **Project** | Decompose into research + code changes | Future |
| 4 | **Parent Ticket** | Group research into compressed summary | Future |
| 5 | **Project Plan** | Dependency graph + execution plan | Future |
| 6 | **Project Orchestrator** | Heartbeat loop per project | Future |
| 7 | **Lead Orchestrator** | Heartbeat loop across all projects | Future |

## Current State Analysis

### What Exists (On This Branch)

| Component | Status | Location |
|-----------|--------|----------|
| Temporal dev server | **Running** | systemd `temporal-dev.service`, ports 7233/8233, namespace `swarm` |
| EventBus (global + per-world) | **Implemented** | `harness/internal/events/bus.go` |
| Datastar SSE patterns | **Implemented** | `harness/internal/server/events.go`, mayor dashboard |
| Hook auth middleware | **Implemented** | `hookSecretMiddleware()` in `server.go` |
| Output directories | **Created** | `thoughts/swarm/research/`, `thoughts/swarm/plans/` (empty) |
| `.env` swarm vars | **Configured** | `CM_SWARM_TEMPORAL=true`, Linear keys, Discord swarm channel |
| Pi-mono / OpenClaw | **Installed** | `/opt/openclaw/` (pi-mono v0.54.0) |

### What Doesn't Exist Yet

- `go.temporal.io` SDK dependency in `go.mod`
- Any swarm Go code (`internal/swarm/`)
- Swarm API routes
- Swarm DB migrations (only 001-005 exist)
- Swarm dashboard views
- Pi-mono agent wrapper (TypeScript or Go→Node bridge)

### Key Discoveries

- **Pi-mono `@mariozechner/pi-ai`** provides unified LLM API supporting OpenAI Codex via `openai-codex-responses` provider type (`harness/internal/swarm/prompt/` on old branch)
- **Pi-mono `@mariozechner/pi-agent-core`** provides the `Agent` class with tool definitions, streaming, and event hooks — we use this to build research agents with read-only file tools
- **Pi-mono supports Codex 5.3** via `getModel("openai-codex", "codex-5.3")` — lower cost than Claude for research tasks
- **Previous dashboard** (`views/swarm/dashboard.templ` on `feature/agent-swarm`) had workflow list, detail page, SSE stream, gate review panel — good starting point
- **Previous state machine** (`internal/swarm/statemachine.go`) maps phase transitions deterministically — we simplify for 2 phases only
- **Previous prompt templates** (`internal/swarm/prompt/templates/*.md.tmpl`) define research and code_plan contracts — we port these
- **Agent primitives HTML** (`internal/swarm/agent-primatives.html`) visualizes the full 7-primitive flowchart — reference for future phases

## Desired End State

After this plan is implemented:

1. A user opens `/swarm` dashboard in the browser
2. They click "Start Research" and enter a question about the codebase
3. The harness creates a `swarm_task` record, starts a Temporal `ResearchWorkflow`
4. Temporal fans out parallel pi-mono research agents (powered by Codex 5.3) that explore the codebase
5. A synthesis agent compresses findings into a research document at `thoughts/swarm/research/<timestamp>_<task_id>_<slug>.md`
6. The dashboard shows real-time progress via SSE (task status, research questions being investigated, completion)
7. Alternatively, the user clicks "Start Code Plan" — the system runs Research first, then a planning agent produces a plan document at `thoughts/swarm/plans/<timestamp>_<task_id>_<slug>.md`

### Verification

- `GET /swarm` renders the dashboard with task list
- `POST /api/swarm/tasks/research` creates a task and returns `task_id`
- `GET /api/swarm/tasks/:id` returns status and artifact paths
- `GET /swarm/events` streams SSE updates for all swarm activity
- Research task produces a well-structured markdown document in `thoughts/swarm/research/`
- Code plan task produces both a research doc and a plan doc
- Temporal UI at `:8233` shows workflow execution history

## What We're NOT Doing

- **Primitive 2.1 (verification)** — no code changes, no test running
- **Primitives 3-7** (project, parent ticket, project plan, orchestrators)
- **Applying code changes** — plans are documents only
- **PR generation** — no git operations
- **OpenClaw/Discord integration** — future; dashboard is the only UI
- **Learning system** — no learnings, digests, or retrospectives
- **Linear ticket creation** — optional linkage only, no automatic ticket creation
- **Claude Code tmux sessions** — we use pi-mono agents instead
- **Gate reviews** — no human-in-the-loop approval gates (future)

## Implementation Approach

### Architecture

**Primitive 1: Research**
```
Browser (Datastar)                    Harness (Go)                         Temporal
      |                                   |                                   |
      |  POST /api/swarm/tasks/research   |                                   |
      |---------------------------------->|  create swarm_task in SQLite       |
      |                                   |  start ResearchWorkflow           |
      |                                   |---------------------------------->|
      |                                   |                                   |
      |  GET /swarm/events (SSE)          |                                   |
      |<==================================|  EventBus publishes task updates  |
      |                                   |                                   |
      |                                   |  Activity: GenerateResearchQs     |
      |                                   |<----------------------------------|
      |                                   |  → research-questions.js (Codex)  |
      |                                   |  → returns sub-questions          |
      |                                   |---------------------------------->|
      |                                   |                                   |
      |                                   |  Activity: RunResearchAgent (×N)  |
      |                                   |<----------------------------------|
      |                                   |  → parallel research-agent.js     |
      |                                   |  → each returns findings          |
      |                                   |---------------------------------->|
      |                                   |                                   |
      |                                   |  Activity: SynthesizeResearchDoc  |
      |                                   |<----------------------------------|
      |                                   |  → research-synthesizer.js        |
      |                                   |  → writes to thoughts/swarm/...   |
      |                                   |---------------------------------->|
      |                                   |                                   |
      |  SSE: task.completed              |                                   |
      |<==================================|                                   |
```

**Primitive 2: Code Change Plan** (extends Research with specialist fan-out)
```
                                      Harness (Go)                         Temporal
                                          |                                   |
                                          |  [ResearchWorkflow completes]     |
                                          |  → researchArtifactPath           |
                                          |                                   |
                                          |  Activity: ClassifyPlanDomains    |
                                          |<----------------------------------|
                                          |  → plan-orchestrator.js           |
                                          |  → returns: [database, api, ui]   |
                                          |---------------------------------->|
                                          |                                   |
                                          |  Activity: RunSpecialist (×N)     |
                                          |<----------------------------------|
                                          |  → parallel:                      |
                                          |    planner-database.js            |
                                          |    planner-api.js                 |
                                          |    planner-ui.js                  |
                                          |  → each returns domain plan       |
                                          |---------------------------------->|
                                          |                                   |
                                          |  Activity: SynthesizePlanDoc      |
                                          |<----------------------------------|
                                          |  → plan-synthesizer.js            |
                                          |  → merges specialist outputs      |
                                          |  → resolves dependency ordering   |
                                          |  → writes to thoughts/swarm/plans |
                                          |---------------------------------->|
```

### Agent Execution: Pi-Mono via Node.js Subprocess

Pi-mono agents run as **Node.js subprocesses** spawned by the Go harness. This avoids embedding a JS runtime in Go while leveraging pi-mono's full agent capabilities.

```
Go Activity                    Node.js Subprocess
     |                              |
     |  exec: node agent.js         |
     |  stdin: { task JSON }        |
     |----------------------------->|
     |                              |  import { Agent } from '@mariozechner/pi-agent-core'
     |                              |  import { getModel } from '@mariozechner/pi-ai'
     |                              |  model = getModel('openai-codex', 'codex-5.3')
     |                              |  agent.prompt(task.question)
     |                              |  → uses read/glob/grep tools on repo
     |                              |
     |  stdout: { result JSON }     |
     |<-----------------------------|
     |                              |
```

**Agent scripts** live at `harness/agents/`:
- `research-questions.js` — decomposes a question into sub-questions
- `research-agent.js` — investigates a single sub-question using file tools
- `research-synthesizer.js` — compresses parallel findings into one document
- `plan-synthesizer.js` — consumes research doc, produces plan doc

Each script:
1. Reads task JSON from stdin
2. Creates a pi-mono `Agent` with Codex 5.3 model and read-only file tools
3. Runs the agent prompt
4. Writes result JSON to stdout

### Why This Approach

1. **Pi-mono handles the hard parts** — model switching, streaming, tool execution, token tracking
2. **Codex 5.3 is cheaper than Claude** for exploratory research — right tool for the job
3. **Node subprocess is simple** — no FFI, no embedded runtime, easy to debug
4. **Temporal handles reliability** — retries, timeouts, workflow history
5. **EventBus handles real-time** — same pattern as mayor dashboard SSE
6. **SQLite handles persistence** — same pattern as existing tables

## Phase 1: Database Schema + Temporal SDK

### Overview
Set up the database tables and Temporal Go SDK dependency.

### Changes Required:

#### 1. Add Temporal Go SDK
**File**: `harness/go.mod`
```
go get go.temporal.io/sdk
```

#### 2. Migration 006: Swarm Tables
**File**: `harness/internal/db/migrations/006_swarm.sql`

```sql
-- Swarm tasks: top-level unit of work (research or code_change_plan)
CREATE TABLE swarm_tasks (
    id TEXT PRIMARY KEY,
    primitive_type TEXT NOT NULL CHECK(primitive_type IN ('research', 'code_change_plan')),
    request_text TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
    workflow_id TEXT,              -- Temporal workflow ID
    linear_issue_id TEXT,          -- optional Linear ticket linkage
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Research questions: fan-out sub-questions for a task
CREATE TABLE swarm_research_questions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    question_text TEXT NOT NULL,
    agent_index INTEGER NOT NULL,   -- 0-based index for ordering
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'running', 'completed', 'failed')),
    result_summary TEXT,            -- compressed findings from this agent
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Artifacts: output documents (research docs, plan docs)
CREATE TABLE swarm_artifacts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL CHECK(artifact_type IN ('research_doc', 'plan_doc')),
    file_path TEXT NOT NULL,        -- relative path in thoughts/swarm/
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Events: SSE-compatible event log
CREATE TABLE swarm_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    detail TEXT,                     -- JSON or plain text
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_swarm_tasks_status ON swarm_tasks(status);
CREATE INDEX idx_swarm_research_questions_task ON swarm_research_questions(task_id);
CREATE INDEX idx_swarm_artifacts_task ON swarm_artifacts(task_id);
CREATE INDEX idx_swarm_events_task ON swarm_events(task_id);
CREATE INDEX idx_swarm_events_created ON swarm_events(created_at);
```

#### 3. Register Migration
**File**: `harness/internal/db/db.go`
Add `006_swarm.sql` to the `migrationFiles` slice.

#### 4. SQLC Queries
**File**: `harness/internal/db/queries/swarm_tasks.sql`
```sql
-- name: CreateSwarmTask :exec
INSERT INTO swarm_tasks (id, primitive_type, request_text, status, workflow_id, linear_issue_id)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSwarmTask :one
SELECT * FROM swarm_tasks WHERE id = ?;

-- name: UpdateSwarmTaskStatus :exec
UPDATE swarm_tasks SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateSwarmTaskWorkflowID :exec
UPDATE swarm_tasks SET workflow_id = ?, updated_at = datetime('now') WHERE id = ?;

-- name: ListSwarmTasks :many
SELECT * FROM swarm_tasks ORDER BY created_at DESC LIMIT ?;

-- name: CreateSwarmResearchQuestion :exec
INSERT INTO swarm_research_questions (id, task_id, question_text, agent_index, status)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateSwarmResearchQuestionStatus :exec
UPDATE swarm_research_questions SET status = ?, result_summary = ?, updated_at = datetime('now') WHERE id = ?;

-- name: GetSwarmResearchQuestions :many
SELECT * FROM swarm_research_questions WHERE task_id = ? ORDER BY agent_index;

-- name: CreateSwarmArtifact :exec
INSERT INTO swarm_artifacts (id, task_id, artifact_type, file_path) VALUES (?, ?, ?, ?);

-- name: GetSwarmArtifacts :many
SELECT * FROM swarm_artifacts WHERE task_id = ? ORDER BY created_at;

-- name: CreateSwarmEvent :exec
INSERT INTO swarm_events (task_id, event_type, detail) VALUES (?, ?, ?);

-- name: GetSwarmEvents :many
SELECT * FROM swarm_events WHERE task_id = ? ORDER BY created_at;

-- name: GetRecentSwarmEvents :many
SELECT * FROM swarm_events ORDER BY created_at DESC LIMIT ?;
```

### Success Criteria:

#### Automated Verification:
- [ ] Migration applies: `sqlite3 data/creative-mode.db < harness/internal/db/migrations/006_swarm.sql`
- [ ] SQLC generates: `cd harness && sqlc generate`
- [ ] Temporal SDK resolves: `cd harness && go mod tidy`
- [ ] Build succeeds: `just check`

---

## Phase 2: Pi-Mono Agent Scripts

### Overview
Create the Node.js agent scripts that pi-mono powers. These are standalone scripts that receive task JSON on stdin and return result JSON on stdout.

### Changes Required:

#### 1. Agent Package Setup
**File**: `harness/agents/package.json`
```json
{
  "name": "swarm-agents",
  "private": true,
  "type": "module",
  "dependencies": {
    "@mariozechner/pi-ai": "^0.57.0",
    "@mariozechner/pi-agent-core": "^0.57.0"
  }
}
```

#### 2. Research Question Generator
**File**: `harness/agents/research-questions.js`

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "request_text": "How does the EventBus work?",
  "repo_root": "/home/deploy/creative-mode",
  "max_questions": 5
}
```

**Behavior**:
- Uses Codex 5.3 to decompose the user's question into 3-5 concrete, answerable sub-questions
- Each sub-question targets a specific area of the codebase
- No file tools needed — this is a pure reasoning step

**Output** (stdout JSON):
```json
{
  "questions": [
    "What is the EventBus struct definition and how are subscribers stored?",
    "Which handlers subscribe to the EventBus and what events do they consume?",
    "How do SSE connections receive events from the EventBus?",
    "What event types exist and where are they published from?"
  ]
}
```

**System prompt**: Focused on decomposing questions into specific, file-level investigation tasks. Instructs the model to generate questions that can be answered by reading specific files.

#### 3. Research Agent
**File**: `harness/agents/research-agent.js`

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "question": "What is the EventBus struct definition and how are subscribers stored?",
  "repo_root": "/home/deploy/creative-mode",
  "agent_index": 0
}
```

**Behavior**:
- Creates a pi-mono `Agent` with read-only file tools:
  - `read_file` — read file contents
  - `glob_files` — find files by pattern
  - `grep_files` — search file contents
  - `list_directory` — list directory contents
- Agent investigates the sub-question using these tools
- Returns structured findings with file:line references

**Output** (stdout JSON):
```json
{
  "question": "What is the EventBus struct definition...",
  "findings": "The EventBus is defined in harness/internal/events/bus.go:9-13...",
  "files_referenced": ["harness/internal/events/bus.go", "harness/internal/events/types.go"],
  "confidence": "high"
}
```

**System prompt**: Focused on investigating a single question by reading code. Must cite specific file paths and line numbers. Must separate facts from assumptions.

#### 4. Research Synthesizer
**File**: `harness/agents/research-synthesizer.js`

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "request_text": "How does the EventBus work?",
  "findings": [
    {"question": "...", "findings": "...", "files_referenced": [...]},
    {"question": "...", "findings": "...", "files_referenced": [...]}
  ],
  "output_path": "thoughts/swarm/research/2026-03-07_abc123_eventbus.md"
}
```

**Behavior**:
- Compresses all parallel findings into a single, well-structured research document
- Uses the research doc artifact schema (from previous plans)
- Writes the document to the specified output path
- No file tools needed — just text synthesis

**Output** (stdout JSON):
```json
{
  "output_path": "thoughts/swarm/research/2026-03-07_abc123_eventbus.md",
  "summary": "The EventBus provides global and per-world pub/sub..."
}
```

**System prompt**: Focused on compression and synthesis. Must produce the deterministic markdown section headers defined in the artifact contract. Must not add information not present in the findings.

#### 5. Plan Orchestrator + Specialized Planner Agents

The plan synthesizer follows the same fan-out/fan-in pattern as research. A **plan orchestrator** agent classifies the change request, fans out to **specialized planner agents** that each have deep domain context for their area, then a **plan synthesizer** compresses the specialist outputs into one unified plan document.

##### 5a. Plan Orchestrator
**File**: `harness/agents/plan-orchestrator.js`

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "request_text": "Add a new swarm_config table and API endpoint to read it",
  "research_doc_path": "thoughts/swarm/research/2026-03-07_abc123_swarm-config.md",
  "repo_root": "/home/deploy/creative-mode"
}
```

**Behavior**:
- Reads the research document
- Classifies which specialist planners are needed based on the change request
- Returns a list of planner types to fan out to

**Output** (stdout JSON):
```json
{
  "planners": [
    { "type": "database", "focus": "Create swarm_config table with migration and SQLC queries" },
    { "type": "api", "focus": "Add GET /api/swarm/config endpoint" },
    { "type": "general", "focus": "Wire config loading into server startup" }
  ]
}
```

**System prompt**: Classifies change requests into the specialist domains below. Must select at least one planner. Can select multiple for cross-cutting changes. Each planner gets a `focus` string scoping what it should plan for.

##### 5b. Specialized Planner Agents

Each planner agent has **domain-specific system prompt context** baked in — conventions, patterns, file locations, and examples specific to that area of the codebase. All share the same I/O contract but differ in their system prompts.

**File**: `harness/agents/planners/planner-database.js`
**Domain**: SQLite schema changes, migrations, SQLC queries
**Baked-in context**:
- Migration pattern: sequential `NNN_name.sql` files in `harness/internal/db/migrations/`
- Must manually add migration to `migrationFiles` slice in `db.go`
- SQLC query files in `harness/internal/db/queries/*.sql` with `-- name: FuncName :one/:many/:exec` annotations
- Generated code in `harness/internal/db/sqlc/` — never edit directly
- SQLite constraints: `TEXT` for dates (datetime('now')), `CHECK` constraints for enums, `ON DELETE CASCADE` for foreign keys
- Pattern reference: existing migrations 001-005 for naming and style

**File**: `harness/agents/planners/planner-api.js`
**Domain**: Echo HTTP handlers, middleware, route registration
**Baked-in context**:
- Handler pattern: `func (s *Server) handleXxx(c echo.Context) error` in `harness/internal/server/`
- Route registration in `RegisterRoutes()` in `server.go`
- Auth middleware chain: `hookSecretMiddleware()`, `auth.SessionMiddleware`, `auth.ApprovedMiddleware`
- Datastar SSE responses: `datastar.NewSSE(w, r)`, `PatchElementTempl`, `MarshalAndPatchSignals`
- Request parsing: `datastar.ReadSignals(r, &signals)` for Datastar, `c.Bind(&req)` for JSON
- Pattern reference: `mayor_api.go`, `swarm_api.go` for handler structure

**File**: `harness/agents/planners/planner-temporal.js`
**Domain**: Temporal workflow and activity design
**Baked-in context**:
- Go Temporal SDK patterns: workflow functions, activity structs, `workflow.ExecuteActivity`, `workflow.Go` for parallel
- Task queues: `swarm-research`, `swarm-planning`
- Activity options: timeouts, retry policies, heartbeat
- Temporal namespace: `swarm` on `localhost:7233`
- Pattern reference: existing workflow/activity patterns in `harness/internal/temporal/`

**File**: `harness/agents/planners/planner-ui.js`
**Domain**: templ templates, Datastar attributes, SSE dashboard views
**Baked-in context**:
- templ component pattern: `.templ` files compiled to Go with `templ generate`
- Datastar attributes: `data-signals`, `data-init`, `data-on:click`, `data-show`, `data-class`, `data-bind`
- SSE connection: `data-init={ datastar.GetSSE("/path") }`
- Signal naming: snake_case, `$signal_name` in expressions
- Patch pattern: server renders templ fragment → `sse.PatchElementTempl(component, opts...)`
- Pattern reference: `views/swarm/dashboard.templ`, `views/mayor/dashboard.templ`

**File**: `harness/agents/planners/planner-general.js`
**Domain**: General Go code changes, wiring, configuration, anything that doesn't fit a specialist
**Baked-in context**:
- Project structure: `harness/internal/` packages, `main.go` initialization
- Existing patterns: dependency injection via struct fields, `slog` structured logging
- Error handling: `echo.NewHTTPError` for HTTP, `fmt.Errorf("doing x: %w", err)` for wrapping

**Shared planner I/O contract:**

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "focus": "Create swarm_config table with migration and SQLC queries",
  "request_text": "Add a new swarm_config table and API endpoint",
  "research_doc": "<full research document content>",
  "repo_root": "/home/deploy/creative-mode"
}
```

**Output** (stdout JSON):
```json
{
  "domain": "database",
  "plan_section": "### Database Changes\n\n#### Migration 007...\n\n#### SQLC Queries...",
  "files_affected": ["harness/internal/db/migrations/007_swarm_config.sql", "harness/internal/db/queries/swarm_config.sql"],
  "verification_checks": ["sqlite3 data/creative-mode.db < migration", "cd harness && sqlc generate"],
  "risks": ["Migration must be added to migrationFiles slice manually"],
  "dependencies": ["Must run before API endpoint implementation"]
}
```

Each planner agent has read-only file tools to verify its claims against the actual codebase.

##### 5c. Plan Synthesizer
**File**: `harness/agents/plan-synthesizer.js`

**Input** (stdin JSON):
```json
{
  "task_id": "abc123",
  "request_text": "Add a new swarm_config table and API endpoint",
  "research_doc_path": "thoughts/swarm/research/2026-03-07_abc123_swarm-config.md",
  "planner_outputs": [
    { "domain": "database", "plan_section": "...", "files_affected": [...], "verification_checks": [...], "risks": [...], "dependencies": [...] },
    { "domain": "api", "plan_section": "...", "files_affected": [...], "verification_checks": [...], "risks": [...], "dependencies": [...] }
  ],
  "output_path": "thoughts/swarm/plans/2026-03-07_abc123_swarm-config.md"
}
```

**Behavior**:
- Receives all specialist planner outputs
- Resolves cross-domain dependencies (e.g., database migration must precede API handler)
- Orders implementation phases based on dependency graph
- Merges verification checks, risks, and file inventories
- Produces the unified plan document following the artifact schema
- Writes the document to the specified output path

**Output** (stdout JSON):
```json
{
  "output_path": "thoughts/swarm/plans/2026-03-07_abc123_swarm-config.md",
  "summary": "Plan with 3 phases, 5 files, 4 verification checks",
  "phase_order": ["database", "api", "general"]
}
```

**System prompt**: Focused on merging specialist plans into a coherent whole. Must resolve ordering from dependency hints. Must produce deterministic section headers. Must not claim code was changed.

##### Adding New Specialists

New planner agents are added by:
1. Creating `harness/agents/planners/planner-<domain>.js` with a domain-specific system prompt
2. Adding the domain to the plan orchestrator's classification logic
3. No workflow changes needed — the fan-out is dynamic based on orchestrator output

#### 6. Shared Agent Utilities
**File**: `harness/agents/lib/tools.js`

Defines the read-only file tools for pi-mono agents:
- `read_file(path)` — `fs.readFileSync()`
- `glob_files(pattern, cwd)` — `glob.sync()` or `fast-glob`
- `grep_files(pattern, path)` — `child_process.execSync('rg ...')`
- `list_directory(path)` — `fs.readdirSync()`

All tools enforce the `repo_root` boundary — no reading outside the repo.

**File**: `harness/agents/lib/model.js`

Creates the Codex 5.3 model instance:
```javascript
import { getModel } from '@mariozechner/pi-ai';
export const codexModel = getModel('openai-codex', 'codex-5.3');
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness/agents && npm install` succeeds
- [ ] `echo '{"request_text":"test","max_questions":3}' | node research-questions.js` returns valid JSON
- [ ] `echo '{"question":"test","repo_root":"/home/deploy/creative-mode"}' | node research-agent.js` returns findings JSON

---

## Phase 3: Temporal Workflows + Activities

### Overview
Implement the Go Temporal workflows and activities that orchestrate the pi-mono agents.

### Changes Required:

#### 1. Temporal Client Setup
**File**: `harness/internal/temporal/client.go`

```go
package temporal

import (
    "go.temporal.io/sdk/client"
)

func NewClient() (client.Client, error) {
    return client.Dial(client.Options{
        HostPort:  "localhost:7233",
        Namespace: "swarm",
    })
}
```

#### 2. Workflow Definitions
**File**: `harness/internal/temporal/workflows.go`

Two workflows:

**ResearchWorkflow**:
```
Input:  { TaskID, RequestText, RepoRoot, MaxQuestions }
Steps:
  1. UpdateTaskStatus("running")
  2. EmitEvent("research.started")
  3. GenerateResearchQuestions → sub-questions[]
  4. EmitEvent("research.questions_generated", questions)
  5. For each question in parallel:
     a. RunResearchAgent(question) → findings
     b. EmitEvent("research.agent_completed", agentIndex)
  6. SynthesizeResearchDoc(allFindings) → artifactPath
  7. PersistArtifact(artifactPath, "research_doc")
  8. UpdateTaskStatus("completed")
  9. EmitEvent("task.completed", artifactPath)
Output: { ArtifactPath, Summary }
```

**CodeChangePlanWorkflow**:
```
Input:  { TaskID, RequestText, RepoRoot, MaxQuestions }
Steps:
  1. UpdateTaskStatus("running")
  2. EmitEvent("code_plan.started")
  3. Run child ResearchWorkflow
     → researchArtifactPath
  4. EmitEvent("code_plan.classifying")
  5. ClassifyPlanDomains(requestText, researchDoc) → plannerSpecs[]
     (plan-orchestrator.js determines which specialists to fan out to)
  6. EmitEvent("code_plan.planning_started", plannerTypes)
  7. For each plannerSpec in parallel:
     a. RunSpecialistPlanner(plannerSpec) → plannerOutput
        (planner-database.js, planner-api.js, etc.)
     b. EmitEvent("code_plan.specialist_completed", domain)
  8. SynthesizePlanDoc(allPlannerOutputs) → planArtifactPath
     (plan-synthesizer.js merges specialist outputs + resolves ordering)
  9. PersistArtifact(planArtifactPath, "plan_doc")
  10. UpdateTaskStatus("completed")
  11. EmitEvent("task.completed", planArtifactPath)
Output: { ResearchArtifactPath, PlanArtifactPath, Summary }
```

#### 3. Activity Definitions
**File**: `harness/internal/temporal/activities.go`

Activities wrap the pi-mono agent subprocess calls:

```go
type SwarmActivities struct {
    db        *db.DB
    eventBus  *events.EventBus
    repoRoot  string
    agentsDir string  // path to harness/agents/
}

// GenerateResearchQuestions calls research-questions.js
func (a *SwarmActivities) GenerateResearchQuestions(ctx context.Context, input GenerateQuestionsInput) ([]string, error)

// RunResearchAgent calls research-agent.js for a single sub-question
func (a *SwarmActivities) RunResearchAgent(ctx context.Context, input ResearchAgentInput) (ResearchAgentOutput, error)

// SynthesizeResearchDoc calls research-synthesizer.js
func (a *SwarmActivities) SynthesizeResearchDoc(ctx context.Context, input SynthesizeInput) (string, error)

// ClassifyPlanDomains calls plan-orchestrator.js to determine which specialist planners to use
func (a *SwarmActivities) ClassifyPlanDomains(ctx context.Context, input ClassifyInput) ([]PlannerSpec, error)

// RunSpecialistPlanner calls planners/planner-<domain>.js for a specific domain
func (a *SwarmActivities) RunSpecialistPlanner(ctx context.Context, input SpecialistPlannerInput) (PlannerOutput, error)

// SynthesizePlanDoc calls plan-synthesizer.js to merge specialist outputs into unified plan
func (a *SwarmActivities) SynthesizePlanDoc(ctx context.Context, input PlanSynthesizeInput) (string, error)

// UpdateTaskStatus updates SQLite and publishes to EventBus
func (a *SwarmActivities) UpdateTaskStatus(ctx context.Context, taskID string, status string) error

// PersistArtifact records the artifact in SQLite
func (a *SwarmActivities) PersistArtifact(ctx context.Context, taskID, artifactType, filePath string) error

// EmitEvent records event in SQLite and publishes to EventBus
func (a *SwarmActivities) EmitEvent(ctx context.Context, taskID, eventType, detail string) error
```

Each agent-calling activity:
1. Marshals input to JSON
2. Spawns `node <script>.js` subprocess with JSON on stdin
3. Reads JSON result from stdout
4. Returns structured result
5. On failure: returns error for Temporal retry

#### 4. Temporal Worker
**File**: `harness/internal/temporal/worker.go`

Registers workflows and activities, starts the worker with the Temporal client. Two task queues:
- `swarm-research` — for research activities (higher concurrency)
- `swarm-planning` — for synthesis/planning activities (lower concurrency)

#### 5. Agent Subprocess Runner
**File**: `harness/internal/temporal/agent_runner.go`

Shared utility for spawning pi-mono agent scripts:
```go
func RunAgentScript(ctx context.Context, agentsDir, script string, input any) (json.RawMessage, error)
```
- Marshals input to JSON
- Runs `node <agentsDir>/<script>` with stdin pipe
- Reads stdout, captures stderr for error context
- Respects context cancellation (kills subprocess)
- Timeout: 5 minutes per agent (configurable via Temporal activity options)

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes with Temporal SDK imported
- [ ] Temporal worker starts and registers with namespace `swarm`
- [ ] Workflows appear in Temporal UI at `:8233`

---

## Phase 4: HTTP API + EventBus Integration

### Overview
Add the harness HTTP endpoints that start tasks and stream SSE events.

### Changes Required:

#### 1. Swarm API Handler
**File**: `harness/internal/server/swarm_api.go`

```go
// POST /api/swarm/tasks/research
// Body: { "request_text": "...", "linear_issue_id": "..." }
// Returns: { "task_id": "...", "status": "pending" }
func (s *Server) handleSwarmStartResearch(c echo.Context) error

// POST /api/swarm/tasks/code-change-plan
// Body: { "request_text": "...", "linear_issue_id": "..." }
// Returns: { "task_id": "...", "status": "pending" }
func (s *Server) handleSwarmStartCodePlan(c echo.Context) error

// GET /api/swarm/tasks/:taskID
// Returns: task status + artifact paths
func (s *Server) handleSwarmGetTask(c echo.Context) error

// POST /api/swarm/tasks/:taskID/cancel
// Cancels the Temporal workflow
func (s *Server) handleSwarmCancelTask(c echo.Context) error
```

Each "start" handler:
1. Creates a `swarm_task` record in SQLite with status `pending`
2. Starts the corresponding Temporal workflow with the task ID
3. Updates the task with the Temporal workflow ID
4. Returns the task ID immediately (async execution)

#### 2. Swarm SSE Handler
**File**: `harness/internal/server/swarm_sse.go`

```go
// GET /swarm/events — SSE stream for swarm dashboard
func (s *Server) handleSwarmSSE(c echo.Context) error
```

Pattern: subscribe to a new `swarm` topic on EventBus (or use global with event type filtering). On each event, render appropriate templ fragment and `PatchElementTempl`.

#### 3. EventBus Extension
**File**: `harness/internal/events/types.go`

Add swarm event types:
```go
const (
    EventSwarmTaskStarted     = "swarm.task.started"
    EventSwarmTaskCompleted   = "swarm.task.completed"
    EventSwarmTaskFailed      = "swarm.task.failed"
    EventSwarmResearchStarted = "swarm.research.started"
    EventSwarmAgentCompleted  = "swarm.agent.completed"
    EventSwarmPlanStarted     = "swarm.plan.started"
)
```

#### 4. Route Registration
**File**: `harness/internal/server/server.go`

Add swarm routes in `RegisterRoutes()`:
```go
// Swarm API (hook secret auth)
swarmAPI := e.Group("/api/swarm", hookSecretMiddleware())
swarmAPI.POST("/tasks/research", s.handleSwarmStartResearch)
swarmAPI.POST("/tasks/code-change-plan", s.handleSwarmStartCodePlan)
swarmAPI.GET("/tasks/:taskID", s.handleSwarmGetTask)
swarmAPI.POST("/tasks/:taskID/cancel", s.handleSwarmCancelTask)

// Swarm dashboard (approved users)
approved.GET("/swarm", s.handleSwarmDashboard)
approved.GET("/swarm/events", s.handleSwarmSSE)
approved.GET("/swarm/:taskID", s.handleSwarmTaskDetail)
```

#### 5. Server Struct Extension
**File**: `harness/internal/server/server.go`

Add Temporal client to Server struct:
```go
type Server struct {
    // ... existing fields
    TemporalClient client.Client  // nil if Temporal not configured
}
```

#### 6. Main Initialization
**File**: `harness/main.go`

Initialize Temporal client, worker, and activities on startup (only if `CM_SWARM_TEMPORAL=true`):
```go
if os.Getenv("CM_SWARM_TEMPORAL") == "true" {
    temporalClient, err := temporal.NewClient()
    // create SwarmActivities with db, eventBus, repoRoot
    // start worker in background goroutine
    server.TemporalClient = temporalClient
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `curl -X POST localhost:8080/api/swarm/tasks/research -H 'X-Hook-Secret: ...' -d '{"request_text":"test"}'` returns 200 with task_id
- [ ] `curl localhost:8080/api/swarm/tasks/<id>` returns task status

---

## Phase 5: Dashboard (Datastar + templ)

### Overview
Create the `/swarm` dashboard with task list, start buttons, and SSE live updates.

### Changes Required:

#### 1. Dashboard Page
**File**: `harness/views/swarm/dashboard.templ`

Adapted from `feature/agent-swarm` branch dashboard but simplified:
- **Header**: "Swarm Dashboard" with breadcrumb to "/"
- **Tabs**: "Tasks" (default), "Events"
- **Task list**: table of swarm_tasks with status badges, artifact links
- **Start buttons**: "Start Research" and "Start Code Plan" — each opens a form with textarea for request_text
- **SSE connection**: `data-init={ datastar.GetSSE("/swarm/events") }`

#### 2. Task Detail Page
**File**: `harness/views/swarm/task_detail.templ`

- Task metadata (type, status, created_at, workflow_id)
- Research questions list with per-question status
- Artifact links (clickable to view the generated markdown)
- Event timeline
- Cancel button (if running)

#### 3. Dashboard Signals
```go
type SwarmDashboardSignals struct {
    ActiveTab   string `json:"active_tab"`
    RequestText string `json:"request_text"`
}
```

#### 4. SSE Fragments
Templ components for patching into the dashboard via SSE:
- `TaskRow(task)` — single row in the task list
- `ResearchQuestionRow(question)` — status update for a research sub-question
- `ArtifactLink(artifact)` — link to generated document

#### 5. Dashboard Handler
**File**: `harness/internal/server/swarm_dashboard.go`

```go
func (s *Server) handleSwarmDashboard(c echo.Context) error {
    tasks, _ := s.DB.ListSwarmTasks(c.Request().Context(), 50)
    return render(c, swarm.Page(swarm.DashboardData{Tasks: tasks}))
}

func (s *Server) handleSwarmTaskDetail(c echo.Context) error {
    taskID := c.Param("taskID")
    task, _ := s.DB.GetSwarmTask(c.Request().Context(), taskID)
    questions, _ := s.DB.GetSwarmResearchQuestions(c.Request().Context(), taskID)
    artifacts, _ := s.DB.GetSwarmArtifacts(c.Request().Context(), taskID)
    events, _ := s.DB.GetSwarmEvents(c.Request().Context(), taskID)
    return render(c, swarm.TaskDetailPage(...))
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `templ generate` succeeds
- [ ] `just check` passes

#### Manual Verification:
- [ ] `/swarm` renders with task list and start buttons
- [ ] Clicking "Start Research" shows a form, submitting creates a task
- [ ] Task status updates in real-time via SSE
- [ ] Task detail page shows research questions and artifacts
- [ ] Artifact links open the generated markdown files

---

## Artifact Contracts

### Research Document Schema
**Path**: `thoughts/swarm/research/YYYY-MM-DD_<task_id>_<slug>.md`

```markdown
---
task_id: <task_id>
primitive: research
timestamp: <ISO 8601>
model: codex-5.3
---

# Research: <original question>

## Questions Investigated
1. <sub-question 1>
2. <sub-question 2>
...

## Findings

### <Area 1>
- Finding with reference (`file.go:line`)
- Connection to other components

### <Area 2>
...

## Architecture Notes
<Relevant patterns, constraints, conventions>

## Risks and Unknowns
<What couldn't be determined, low-confidence areas>

## Recommendations
<Suggested approach or answer based on findings>

## References
- `path/to/file.go:123` — Description
- `path/to/other.go:45-67` — Description
```

### Code Plan Document Schema
**Path**: `thoughts/swarm/plans/YYYY-MM-DD_<task_id>_<slug>.md`

```markdown
---
task_id: <task_id>
primitive: code_change_plan
timestamp: <ISO 8601>
model: codex-5.3
research_doc: thoughts/swarm/research/<corresponding research doc>
---

# Code Change Plan: <title>

## Request Summary
<What was asked for>

## Research Inputs
<Reference to research doc, key findings that inform this plan>

## Current State
<What exists now, discovered via research>

## Desired End State
<What should exist after implementation>

## Out of Scope
<What this plan explicitly does NOT do>

## Implementation Phases

### Phase 1: <name>
#### Changes:
| File | Type | Purpose |
|------|------|---------|
| `path/file` | New/Edit | What and why |

#### Steps:
1. <specific step>
2. <specific step>

### Phase 2: <name>
...

## Verification Strategy
<Placeholder for Primitive 2.1 — what checks would validate this plan>

## Risks and Mitigations
- <risk>: <mitigation>

## Open Questions
<Items requiring human decision>

## Confidence
<high/medium/low with reasoning>
```

## Prompt Safety Rules

All agent prompts must enforce:
1. **Never claim code was changed** — these agents produce documents, not edits
2. **Distinguish facts vs assumptions** — cite file:line for facts, mark assumptions
3. **If evidence is missing, say so** — don't invent details to fill gaps
4. **Keep section headers exact** — enables downstream parsing
5. **Stay within repo boundary** — file tools must not read outside repo root

## Testing Strategy

### Unit Tests:
- State machine transitions (simplified from old branch's `statemachine_test.go`)
- Agent subprocess runner (mock subprocess, validate JSON piping)
- SQLC query tests (create task, update status, persist artifact)

### Integration Tests:
- Full ResearchWorkflow with real Temporal (test namespace)
- Agent script invocation with real pi-mono + Codex (rate-limited)
- SSE event delivery from activity to browser

### Manual Testing Steps:
1. Open `/swarm`, click "Start Research", enter "How does the EventBus work?"
2. Watch SSE stream show task created → research started → agents completing → synthesis → done
3. Click artifact link, verify research doc is well-structured
4. Open Temporal UI at `:8233`, verify workflow history
5. Repeat with "Start Code Plan" — verify both research doc and plan doc are produced

## Performance Considerations

- **Codex 5.3 costs** — each research agent call costs tokens; limit `max_questions` to 5 by default
- **Parallel agent limit** — fan-out max 5 agents to avoid API rate limits
- **Node subprocess overhead** — ~200ms startup per agent script; acceptable for research tasks
- **Single task at a time** — no concurrency limit enforced in Phase 1, but Temporal worker concurrency caps it naturally

## Migration Path (This Plan → Future Phases)

1. **Primitive 2.1 (Verification)**: Add `PhaseVerify` to state machine, new `verify-agent.js` that runs tests
2. **Primitive 3 (Project)**: Add `CodeChangePlanWorkflow` as child of `ProjectWorkflow`, decompose into multiple code changes
3. **Primitives 4-5 (Parent Ticket, Project Plan)**: Linear integration for ticket creation, dependency graph generation
4. **Primitives 6-7 (Orchestrators)**: Heartbeat schedules in Temporal, cross-project coordination
5. **OpenClaw/Discord front-end**: Replace dashboard buttons with Discord interface via mayor system

## References

- Previous plans: `thoughts/coreycole/plans/2026-03-07_agent-swarm-primitives-phase1-research-and-code-plan.md`
- Previous plans: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- Previous implementation: `git show origin/feature/agent-swarm -- harness/internal/swarm/`
- Agent primitives flowchart: `git show origin/feature/agent-swarm:harness/internal/swarm/agent-primatives.html`
- Previous dashboard: `git show origin/feature/agent-swarm:harness/views/swarm/dashboard.templ`
- Pi-mono repo: `github.com/badlogic/pi-mono`
- Pi-mono on this server: `/opt/openclaw/node_modules/@mariozechner/`
- Temporal dev server: `systemctl status temporal-dev`
- Research skill contract: `.claude/skills/research_codebase.md`
- Plan skill contract: `.claude/skills/create_plan.md`
