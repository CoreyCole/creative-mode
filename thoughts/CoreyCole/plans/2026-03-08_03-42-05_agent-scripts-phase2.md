# Agent Scripts Phase 2: JS Execution Layer with Self-Discovering Context

## Overview

Phase 1 (Go infrastructure) is complete on `feat/agent-primitives`: migration 006, SQLC queries, event constants, cleanup routines. This phase creates the JS agent execution layer — 6 agent scripts, shared libraries, skill files, and Go types — that pi-mono agents use to explore the codebase and produce research/plan artifacts.

Key design: **agents self-discover context** via `search_context` tool (deterministic keyword grep, no LLM call) rather than having domain knowledge baked into system prompts. Skills are markdown files with YAML frontmatter for search indexing.

**Authoritative plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`
**Pi-mono reference**: `context/pi-mono/` (v0.57.1)

## Current State Analysis

| Component | Status |
|-----------|--------|
| Migration 006 (`swarm_tasks`, `swarm_spans`, etc.) | Created, untracked |
| SQLC queries + generated code | Created, untracked |
| Event constants (`EventSpanStarted`, etc.) | Added to `types.go` |
| `harness/internal/swarmorch/` | Does not exist |
| `harness/agents/` | Does not exist |
| Pi-mono packages (`@mariozechner/*` on npm) | Available: `pi-agent-core`, `pi-ai`, `pi-coding-agent` |

### Key Discoveries:

- `Agent` class: `@mariozechner/pi-agent-core` exports `Agent` with `setModel()`, `setSystemPrompt()`, `setTools()`, `subscribe()`, `prompt(string)` — `context/pi-mono/packages/agent/src/agent.ts:102`
- `getModel`: `@mariozechner/pi-ai` exports `getModel(provider, modelId)` — `context/pi-mono/packages/ai/src/models.ts:20`
- `createReadOnlyTools(cwd)`: `@mariozechner/pi-coding-agent` exports this, returns `[read, grep, find, ls]` — `context/pi-mono/packages/coding-agent/src/core/tools/index.ts:122`
- `Type`: Re-exported from `@mariozechner/pi-ai` (wraps `@sinclair/typebox`)
- `AgentTool` interface requires: `name`, `label`, `description`, `parameters` (TypeBox schema), `execute(toolCallId, params, signal?, onUpdate?)` → `Promise<{content, details}>`
- Agent events: `tool_execution_start { toolCallId, toolName, args }`, `tool_execution_end { toolCallId, toolName, result, isError }`

## Desired End State

After this phase:

1. `harness/agents/` directory exists with `package.json` and installed `node_modules`
2. `harness/agents/lib/` contains protocol, tools, search-context, and agent-factory modules
3. `harness/agents/skills/` contains 7 skill files with YAML frontmatter
4. 6 agent scripts exist and pass `node --check` syntax validation
5. `harness/internal/swarmorch/types.go` defines Go structs for all artifact schemas and JSONL protocol messages
6. Module resolution works (`cd harness/agents && node -e "import('./lib/agent-factory.js')"`)
7. `cd harness && go build ./internal/swarmorch/` compiles

### Verification:

```bash
# Syntax check all JS
for f in harness/agents/*.js harness/agents/lib/*.js; do node --check "$f"; done

# Module resolution
cd harness/agents && node -e "import('./lib/agent-factory.js')"

# Go types compile
cd harness && go build ./internal/swarmorch/

# Full lint pass
just check
```

## What We're NOT Doing

- Temporal workflows/activities (Phase 3)
- Go `runAgent()` function (Phase 3)
- Dashboard UI (Phase 4)
- HTTP API routes (Phase 4)
- SSE event handlers (Phase 4)
- Code execution agents (future primitive)
- OpenClaw/Discord integration

## Implementation Approach

Bottom-up: scaffolding → shared libs → skill files → agent scripts → Go types. Each phase is independently testable.

---

## Phase 1: Scaffolding

### Overview
Create `harness/agents/` directory, `package.json`, run `npm install` to pull pi-mono packages from npm (`@mariozechner/*` scope from `github.com/badlogic/pi-mono`).

### Changes Required:

#### 1. `harness/agents/package.json`
**File**: `harness/agents/package.json` (new)

```json
{
  "name": "swarm-agents",
  "private": true,
  "type": "module",
  "dependencies": {
    "@mariozechner/pi-ai": "^0.54.0",
    "@mariozechner/pi-agent-core": "^0.54.0",
    "@mariozechner/pi-coding-agent": "^0.54.0"
  }
}
```

#### 2. Install dependencies

```bash
cd harness/agents && npm install
```

`node_modules/` is already covered by the root `.gitignore` glob.

### Success Criteria:

#### Automated Verification:
- [ ] `ls harness/agents/node_modules/@mariozechner/` lists `pi-ai`, `pi-agent-core`, `pi-coding-agent`
- [ ] `cd harness/agents && node -e "import('@mariozechner/pi-agent-core')"` succeeds

---

## Phase 2: Shared Library — Protocol

### Overview
JSONL stdin/stdout protocol for Go ↔ Agent communication.

### Changes Required:

#### 1. `harness/agents/lib/protocol.js` (new)

```javascript
import { createInterface } from 'readline';

let rl;
let stdinClosed = false;

export function initProtocol() {
  rl = createInterface({ input: process.stdin, terminal: false });
  rl.on('close', () => { stdinClosed = true; });
}

export function readLine() {
  return new Promise((resolve, reject) => {
    if (stdinClosed) {
      reject(new Error('stdin closed — orchestrator exited'));
      return;
    }
    const onLine = (line) => {
      rl.removeListener('close', onClose);
      resolve(JSON.parse(line));
    };
    const onClose = () => {
      rl.removeListener('line', onLine);
      reject(new Error('stdin closed — orchestrator exited'));
    };
    rl.once('line', onLine);
    rl.once('close', onClose);
  });
}

export function sendEvent(event, tool, data, toolCallId) {
  process.stdout.write(JSON.stringify({ type: 'event', event, tool, data, toolCallId }) + '\n');
}

export function sendQuestion(id, text) {
  process.stdout.write(JSON.stringify({ type: 'question', id, text }) + '\n');
}

export function sendResult(data) {
  process.stdout.write(JSON.stringify({ type: 'result', data }) + '\n');
}
```

**Critical**: `readLine()` returns parsed JSON objects (not raw strings). Must reject pending promises on stdin `close` event and clean up listeners to prevent memory leaks.

### Success Criteria:

#### Automated Verification:
- [ ] `node --check harness/agents/lib/protocol.js` passes

---

## Phase 3: Shared Library — Orchestrator Tools

### Overview
Three custom tools compatible with pi-mono's `AgentTool` interface.

### Changes Required:

#### 1. `harness/agents/lib/orchestrator-tools.js` (new)

```javascript
import { Type } from '@mariozechner/pi-ai';
import { randomUUID } from 'crypto';
import { readLine, sendQuestion, sendResult } from './protocol.js';

export function createAskOrchestratorTool() {
  return {
    name: 'ask_orchestrator',
    label: 'Ask Orchestrator',
    description: 'Ask the orchestrator when you need context you cannot find with your file tools. Use for architectural questions, cross-cutting concerns, or when stuck.',
    parameters: Type.Object({
      question: Type.String({ description: 'What you need to know' })
    }),
    execute: async (_id, { question }) => {
      const qid = randomUUID();
      sendQuestion(qid, question);
      const answer = await readLine();
      return { content: [{ type: 'text', text: answer.text }], details: {} };
    }
  };
}

export function createSubmitArtifactTool(schema, validate) {
  return {
    name: 'submit_artifact',
    label: 'Submit Artifact',
    description: 'Submit your final output when your work is complete. The artifact will be validated before acceptance.',
    parameters: schema,
    execute: async (_id, artifact) => {
      const errors = validate ? validate(artifact) : [];
      if (errors.length > 0) {
        return {
          content: [{ type: 'text', text: `Validation errors:\n${errors.join('\n')}\nFix these issues and call submit_artifact again.` }],
          details: { valid: false }
        };
      }
      sendResult(artifact);
      return {
        content: [{ type: 'text', text: 'Artifact submitted successfully.' }],
        details: { valid: true }
      };
    }
  };
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `node --check harness/agents/lib/orchestrator-tools.js` passes

---

## Phase 4: Shared Library — Search Context Tool

### Overview
The self-discovery tool. Deterministic grep pipeline — no LLM call. Reads YAML frontmatter from skill files, matches against query keywords, optionally greps source files.

### Changes Required:

#### 1. `harness/agents/lib/search-context.js` (new)

```javascript
import { Type } from '@mariozechner/pi-ai';
import { readFileSync, readdirSync } from 'fs';
import { join, relative } from 'path';
import { execSync } from 'child_process';

// Stopwords to filter from queries
const STOPWORDS = new Set([
  'the', 'a', 'an', 'is', 'are', 'was', 'were', 'be', 'been', 'being',
  'have', 'has', 'had', 'do', 'does', 'did', 'will', 'would', 'could',
  'should', 'may', 'might', 'shall', 'can', 'need', 'dare', 'ought',
  'used', 'to', 'of', 'in', 'for', 'on', 'with', 'at', 'by', 'from',
  'as', 'into', 'through', 'during', 'before', 'after', 'above', 'below',
  'between', 'out', 'off', 'over', 'under', 'again', 'further', 'then',
  'once', 'here', 'there', 'when', 'where', 'why', 'how', 'all', 'each',
  'every', 'both', 'few', 'more', 'most', 'other', 'some', 'such', 'no',
  'nor', 'not', 'only', 'own', 'same', 'so', 'than', 'too', 'very',
  'just', 'because', 'but', 'and', 'or', 'if', 'while', 'about', 'what',
  'which', 'who', 'whom', 'this', 'that', 'these', 'those', 'i', 'me',
  'my', 'we', 'our', 'you', 'your', 'he', 'him', 'his', 'she', 'her',
  'it', 'its', 'they', 'them', 'their'
]);

export function extractKeywords(query) {
  return query
    .toLowerCase()
    .split(/[\s,;:.()\[\]{}'"]+/)
    .filter(w => w.length > 2 && !STOPWORDS.has(w));
}

// Skill index cache (loaded once per process)
let skillIndexCache = null;

function parseFrontmatter(content) {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  const fm = {};
  for (const line of match[1].split('\n')) {
    const colonIdx = line.indexOf(':');
    if (colonIdx === -1) continue;
    const key = line.slice(0, colonIdx).trim();
    let val = line.slice(colonIdx + 1).trim();
    // Handle YAML arrays like [foo, bar, baz]
    if (val.startsWith('[') && val.endsWith(']')) {
      val = val.slice(1, -1).split(',').map(s => s.trim());
    }
    fm[key] = val;
  }
  return fm;
}

export function loadSkillIndex(skillsDir) {
  if (skillIndexCache) return skillIndexCache;
  const index = [];
  try {
    const files = readdirSync(skillsDir).filter(f => f.endsWith('.md'));
    for (const file of files) {
      const fullPath = join(skillsDir, file);
      const content = readFileSync(fullPath, 'utf-8');
      const fm = parseFrontmatter(content);
      index.push({
        path: fullPath,
        file,
        name: fm.name || file.replace('.md', ''),
        description: fm.description || '',
        tags: Array.isArray(fm.tags) ? fm.tags : [],
      });
    }
  } catch (e) {
    // Skills dir may not exist yet
  }
  skillIndexCache = index;
  return index;
}

export function rankByRelevance(skillIndex, keywords) {
  return skillIndex
    .map(skill => {
      const searchText = `${skill.description} ${skill.tags.join(' ')}`.toLowerCase();
      const score = keywords.reduce((acc, kw) => acc + (searchText.includes(kw) ? 1 : 0), 0);
      return { ...skill, score };
    })
    .filter(s => s.score > 0)
    .sort((a, b) => b.score - a.score);
}

function grepFiles(searchDirs, keywords, opts = {}) {
  const { glob = '*.{go,templ,sql,js,md}', maxResults = 10, contextLines = 2 } = opts;
  const results = [];

  for (const dir of searchDirs) {
    for (const kw of keywords.slice(0, 3)) { // limit to 3 keywords
      try {
        const out = execSync(
          `grep -rl --include="${glob}" "${kw}" "${dir}" 2>/dev/null | head -${maxResults}`,
          { encoding: 'utf-8', timeout: 5000 }
        ).trim();
        if (!out) continue;
        for (const filePath of out.split('\n')) {
          if (!filePath || results.length >= maxResults) break;
          const relPath = relative(dir, filePath);
          if (!results.some(r => r.path === relPath)) {
            results.push({ path: relPath, keyword: kw });
          }
        }
      } catch (e) {
        // grep returns non-zero when no matches
      }
      if (results.length >= maxResults) break;
    }
    if (results.length >= maxResults) break;
  }
  return results;
}

function formatResults(matchedSkills, codeMatches) {
  const parts = [];

  if (matchedSkills.length > 0) {
    parts.push('## Matching Skills\n');
    for (const skill of matchedSkills) {
      parts.push(`- **${skill.name}** (${skill.file}): ${skill.description} [score: ${skill.score}]`);
    }
  }

  if (codeMatches.length > 0) {
    parts.push('\n## Matching Source Files\n');
    for (const match of codeMatches) {
      parts.push(`- \`${match.path}\` (matched: "${match.keyword}")`);
    }
  }

  if (parts.length === 0) {
    return 'No matching skills or source files found for your query. Try different keywords or use file tools (grep, find) to search directly.';
  }

  parts.push('\nUse the `read` tool to load the full content of any relevant matches.');
  return parts.join('\n');
}

export function createSearchContextTool(skillsDir, searchDirs) {
  return {
    name: 'search_context',
    label: 'Search Context',
    description: 'Search for relevant skills, conventions, and code patterns in the codebase. Use this before starting work to discover what you need to know. Returns file paths and descriptions — use read to load full content.',
    parameters: Type.Object({
      query: Type.String({ description: 'Natural language description of what you need to know (e.g., "database migration patterns, SQLC conventions")' }),
      focus: Type.Optional(Type.Union([
        Type.Literal('all'),
        Type.Literal('skills-only'),
        Type.Literal('code-only')
      ], { description: 'Search focus: all (default), skills-only, or code-only', default: 'all' }))
    }),
    execute: async (_toolCallId, { query, focus = 'all' }) => {
      const keywords = extractKeywords(query);

      let matchedSkills = [];
      if (focus !== 'code-only') {
        const skillIndex = loadSkillIndex(skillsDir);
        matchedSkills = rankByRelevance(skillIndex, keywords);
      }

      let codeMatches = [];
      if (focus !== 'skills-only' && searchDirs.length > 0) {
        codeMatches = grepFiles(searchDirs, keywords);
      }

      const text = formatResults(matchedSkills, codeMatches);
      return {
        content: [{ type: 'text', text }],
        details: { skillCount: matchedSkills.length, codeCount: codeMatches.length }
      };
    }
  };
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `node --check harness/agents/lib/search-context.js` passes

---

## Phase 5: Shared Library — Agent Factory

### Overview
`runAgent()` — the main entry point used by all 6 agent scripts. Bootstraps protocol, reads start message, creates Agent with tools, subscribes to events, runs prompt.

### Changes Required:

#### 1. `harness/agents/lib/agent-factory.js` (new)

```javascript
import { Agent } from '@mariozechner/pi-agent-core';
import { getModel } from '@mariozechner/pi-ai';
import { createReadOnlyTools } from '@mariozechner/pi-coding-agent';
import { createAskOrchestratorTool, createSubmitArtifactTool } from './orchestrator-tools.js';
import { createSearchContextTool } from './search-context.js';
import { initProtocol, readLine, sendEvent } from './protocol.js';
import { join } from 'path';

export async function runAgent({
  artifactSchema,
  validate,
  systemPrompt,
  prompt,
  repoRoot,
  skillsDir,
  withFileTools = true,
  withSearchContext = true,
}) {
  initProtocol();

  // Read start message from Go orchestrator
  const startMsg = await readLine();
  const task = startMsg.task;
  const finalSystemPrompt = startMsg.systemPrompt || systemPrompt;
  const cwd = repoRoot || task.repo_root;
  const finalSkillsDir = skillsDir || join(cwd, 'harness', 'agents', 'skills');

  const model = getModel('openai', 'gpt-5.3-codex');

  const tools = [];
  if (withFileTools) {
    tools.push(...createReadOnlyTools(cwd));
  }
  if (withSearchContext) {
    tools.push(createSearchContextTool(finalSkillsDir, withFileTools ? [cwd] : []));
  }
  tools.push(createAskOrchestratorTool());
  tools.push(createSubmitArtifactTool(artifactSchema, validate));

  const agent = new Agent();
  agent.setModel(model);
  agent.setSystemPrompt(finalSystemPrompt);
  agent.setTools(tools);

  // Stream tool events to Go for span creation + SSE dashboard
  agent.subscribe(event => {
    if (event.type === 'tool_execution_start') {
      sendEvent('tool_execution_start', event.toolName, event.args, event.toolCallId);
    } else if (event.type === 'tool_execution_end') {
      sendEvent('tool_execution_end', event.toolName, event.result, event.toolCallId);
    }
  });

  // Build user prompt from task data
  const userPrompt = typeof prompt === 'function' ? prompt(task) : prompt;
  await agent.prompt(userPrompt);
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `node --check harness/agents/lib/agent-factory.js` passes

---

## Phase 6: Skill Files

### Overview
7 concise markdown reference docs (~1-2K tokens each) with YAML frontmatter for `search_context` indexing. Content derived from existing CLAUDE.md files.

### Changes Required:

All files go in `harness/agents/skills/`.

#### 1. `project-structure.md`
```yaml
---
name: project-structure
description: Directory layout, key packages, data flow, entry points, Go server structure
tags: [project, structure, packages, directories, architecture]
last_verified: 2026-03-08
---
```
Content: directory table from root CLAUDE.md, data flow diagram, key packages summary from harness/CLAUDE.md.

#### 2. `database-conventions.md`
```yaml
---
name: database-conventions
description: SQLite WAL mode, SQLC query generation, migration registration pattern, transaction handling
tags: [sqlite, sqlc, migrations, database, schema, queries]
last_verified: 2026-03-08
---
```
Content: Migration naming and registration in `migrationFiles` slice (from MEMORY.md), SQLC workflow (`sqlc.yaml` location, `just generate`), query patterns from existing `.sql` files, WAL mode config.

#### 3. `api-conventions.md`
```yaml
---
name: api-conventions
description: Echo routes, auth middleware chain, SSE patterns, request handling, Datastar ReadSignals
tags: [echo, routes, auth, middleware, sse, api, datastar]
last_verified: 2026-03-08
---
```
Content: Auth middleware chain, route patterns, SSE with EventBus, ReadSignals, hook secret middleware from harness/CLAUDE.md.

#### 4. `ui-conventions.md`
```yaml
---
name: ui-conventions
description: templ components, Datastar colon syntax, fat-morph SSE, signals, PatchElementTempl
tags: [templ, datastar, sse, ui, signals, html, components]
last_verified: 2026-03-08
---
```
Content: Datastar attribute reference (colon syntax!), signal best practices, SSE pattern, templ rendering from harness/CLAUDE.md.

#### 5. `temporal-conventions.md`
```yaml
---
name: temporal-conventions
description: Temporal namespace swarm, workflow activity rules, worker setup, task queues
tags: [temporal, workflow, activity, worker, task-queue]
last_verified: 2026-03-08
---
```
Content: Dev server location, namespace `swarm`, ports 7233/8233, binary path, env var `CM_SWARM_TEMPORAL`, systemd service `temporal-dev.service`.

#### 6. `build-system.md`
```yaml
---
name: build-system
description: Nix deps, just commands, WASM 5GB constraint, deployment topology, VPS vs macOS
tags: [nix, just, build, wasm, deploy, systemd, air]
last_verified: 2026-03-08
---
```
Content: VPS vs macOS constraints, `just check/fmt/vps-build`, WASM RAM limit, deployment topology from root CLAUDE.md.

#### 7. `agent-hierarchy.md`
```yaml
---
name: agent-hierarchy
description: President mayors Claude Code hierarchy, OpenClaw workspace, Discord integration, build pipeline
tags: [agents, mayor, president, openclaw, discord, claude-code]
last_verified: 2026-03-08
---
```
Content: Agent hierarchy diagram, OpenClaw workspace structure, Discord listener, build notification flow from root CLAUDE.md.

### Success Criteria:

#### Automated Verification:
- [ ] All 7 `.md` files exist in `harness/agents/skills/`
- [ ] Each has YAML frontmatter with `name`, `description`, `tags`
- [ ] Each is < 3KB (concise enough for token budget)

---

## Phase 7: Agent Scripts

### Overview
6 agent scripts, each importing `runAgent()` from the shared factory. System prompts include a self-reflection preamble instructing agents to use `search_context` before starting work.

### Self-Reflection Preamble (shared across agents with tools):

```
Before starting work:
1. Reflect on what aspects of this codebase you need to understand for this task
2. Call search_context with your requirements to discover relevant skills and patterns
3. Use read to load the full content of any matched skills
4. Then proceed with your investigation/planning
```

### Changes Required:

#### 1. `harness/agents/research-questions.js`

Decomposes a research question into 2-5 focused sub-questions. `withFileTools: true`, `withSearchContext: true`.

Artifact schema: `{ questions: [{ question: string, rationale: string, suggested_files: string[] }] }`

System prompt: Role is to decompose a codebase question into concrete sub-questions. Each should target specific files or patterns. Use search_context to understand project structure before decomposing.

User prompt: Built from `task.request_text` and `task.max_questions`.

#### 2. `harness/agents/research-agent.js`

Investigates one sub-question using file tools. Primary codebase explorer. `withFileTools: true`, `withSearchContext: true`.

Artifact schema: `{ question: string, findings: string, files_referenced: string[], confidence: "high" | "medium" | "low" }`

System prompt: Investigate a single codebase question using file tools. Load relevant skills first. Produce compressed findings with file:line references. Do NOT include raw file contents — summarize. Use ask_orchestrator for cross-cutting context.

User prompt: Built from `task.question`.

#### 3. `harness/agents/research-synthesizer.js`

Combines multiple research findings into a research document. `withFileTools: false`, `withSearchContext: false` — works entirely from provided findings.

Artifact schema: `{ document: string, summary: string, output_path: string }`

System prompt: Compress parallel research findings into a single research document. Organize by theme, not by sub-question. Do not add information not present in the findings.

User prompt: Built from `task.request_text`, `task.findings`, `task.output_path`.

#### 4. `harness/agents/plan-orchestrator.js`

Classifies which specialist domains need plans. `withFileTools: true`, `withSearchContext: true`.

Artifact schema: `{ planners: [{ type: string, focus: string }] }`

System prompt: Classify a change request into specialist planner domains. Available domains: database, api, temporal, ui, general. Select 1-4 based on what the change touches.

User prompt: Built from `task.request_text` and `task.research_doc_path`.

#### 5. `harness/agents/specialist-planner.js`

Creates a detailed plan section for one domain. `withFileTools: true`, `withSearchContext: true`.

Artifact schema: `{ domain: string, plan_section: string, files_affected: string[], verification_checks: string[], risks: string[], dependencies: string[] }`

System prompt: Produce a plan section for your domain. Load the relevant skill for your domain. Read actual code to verify claims. Include verification checks and dependency information.

User prompt: Built from `task.domain`, `task.focus`, `task.research_doc`.

#### 6. `harness/agents/plan-synthesizer.js`

Merges specialist plans into unified implementation plan. `withFileTools: false`, `withSearchContext: false`.

Artifact schema: `{ document: string, summary: string, phase_order: string[], output_path: string }`

System prompt: Merge specialist plans into a unified implementation plan. Resolve cross-domain dependencies. Order phases based on dependency graph.

User prompt: Built from `task.request_text`, `task.research_doc_summary`, `task.planner_outputs`, `task.output_path`.

### Success Criteria:

#### Automated Verification:
- [ ] `node --check` passes for all 6 agent scripts
- [ ] Each script imports `runAgent` and calls it with appropriate config

---

## Phase 8: Go Types

### Overview
Go struct definitions matching all 6 artifact schemas for JSON unmarshaling, plus JSONL protocol message types.

### Changes Required:

#### 1. `harness/internal/swarmorch/types.go` (new)

```go
package swarmorch

import "encoding/json"

// --- JSONL Protocol Messages ---

// StartMessage is sent from Go to agent on stdin
type StartMessage struct {
    Type         string          `json:"type"`          // always "start"
    Task         json.RawMessage `json:"task"`
    SystemPrompt string          `json:"systemPrompt,omitempty"`
}

// AnswerMessage is sent from Go to agent in response to a question
type AnswerMessage struct {
    Type string `json:"type"` // always "answer"
    ID   string `json:"id"`
    Text string `json:"text"`
}

// AgentMessage is a generic message received from agent on stdout
type AgentMessage struct {
    Type       string          `json:"type"`                 // "event", "question", "result"
    Event      string          `json:"event,omitempty"`      // for type=event
    Tool       string          `json:"tool,omitempty"`       // for type=event
    Data       json.RawMessage `json:"data,omitempty"`       // for type=event or type=result
    ToolCallID string          `json:"toolCallId,omitempty"` // for type=event
    ID         string          `json:"id,omitempty"`         // for type=question
    Text       string          `json:"text,omitempty"`       // for type=question
}

// --- Agent Input Types ---

type GenerateQuestionsInput struct {
    TaskID       string `json:"task_id"`
    RequestText  string `json:"request_text"`
    RepoRoot     string `json:"repo_root"`
    MaxQuestions  int    `json:"max_questions"`
}

type ResearchAgentInput struct {
    TaskID     string `json:"task_id"`
    Question   string `json:"question"`
    RepoRoot   string `json:"repo_root"`
    AgentIndex int    `json:"agent_index"`
}

type SynthesizeInput struct {
    TaskID      string            `json:"task_id"`
    RequestText string            `json:"request_text"`
    Findings    []ResearchFinding `json:"findings"`
    OutputPath  string            `json:"output_path"`
}

type ClassifyInput struct {
    TaskID          string `json:"task_id"`
    RequestText     string `json:"request_text"`
    ResearchDocPath string `json:"research_doc_path"`
    RepoRoot        string `json:"repo_root"`
}

type SpecialistInput struct {
    TaskID      string `json:"task_id"`
    Domain      string `json:"domain"`
    Focus       string `json:"focus"`
    RequestText string `json:"request_text"`
    ResearchDoc string `json:"research_doc"`
    RepoRoot    string `json:"repo_root"`
}

type PlanSynthesizeInput struct {
    TaskID             string           `json:"task_id"`
    RequestText        string           `json:"request_text"`
    ResearchDocSummary string           `json:"research_doc_summary"`
    PlannerOutputs     []PlannerOutput  `json:"planner_outputs"`
    OutputPath         string           `json:"output_path"`
}

// --- Artifact Output Types ---

type SubQuestion struct {
    Question       string   `json:"question"`
    Rationale      string   `json:"rationale"`
    SuggestedFiles []string `json:"suggested_files"`
}

type QuestionArtifact struct {
    Questions []SubQuestion `json:"questions"`
}

type ResearchFinding struct {
    Question        string   `json:"question"`
    Findings        string   `json:"findings"`
    FilesReferenced []string `json:"files_referenced"`
    Confidence      string   `json:"confidence"` // "high", "medium", "low"
}

type SynthesizeResult struct {
    Document   string `json:"document"`
    Summary    string `json:"summary"`
    OutputPath string `json:"output_path"`
}

type PlannerSpec struct {
    Type  string `json:"type"`
    Focus string `json:"focus"`
}

type ClassifyResult struct {
    Planners []PlannerSpec `json:"planners"`
}

type PlannerOutput struct {
    Domain             string   `json:"domain"`
    PlanSection        string   `json:"plan_section"`
    FilesAffected      []string `json:"files_affected"`
    VerificationChecks []string `json:"verification_checks"`
    Risks              []string `json:"risks"`
    Dependencies       []string `json:"dependencies"`
}

type PlanSynthesizeResult struct {
    Document   string   `json:"document"`
    Summary    string   `json:"summary"`
    PhaseOrder []string `json:"phase_order"`
    OutputPath string   `json:"output_path"`
}

// --- Span Helper Types ---

type SpanParams struct {
    ID           string          `json:"id"`
    TaskID       string          `json:"task_id"`
    ParentSpanID string          `json:"parent_span_id,omitempty"`
    SpanType     string          `json:"span_type"`
    Name         string          `json:"name"`
    InputJSON    json.RawMessage `json:"input_json,omitempty"`
    StartedAt    string          `json:"started_at"`
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./internal/swarmorch/` compiles
- [ ] `cd harness && go vet ./internal/swarmorch/` passes

---

## Phase 9: Pre-Step — Commit Phase 1

### Overview
Commit all existing Phase 1 changes (migration, SQLC, events, main.go) before adding new files.

### Changes Required:
Stage and commit all currently modified/untracked Phase 1 files:
- `harness/internal/db/migrations/006_swarm_tables.sql`
- `harness/internal/db/queries/swarm.sql`
- `harness/internal/db/sqlc/swarm.sql.go`
- `harness/internal/db/sqlc/models.go`
- `harness/internal/db/sqlc/querier.go`
- `harness/internal/db/db.go`
- `harness/internal/events/types.go`
- `harness/main.go`
- `harness/sqlc.yaml`
- `flake.nix`

**Note**: This should be done FIRST, before any Phase 2 work, to keep commits clean. Moved to Phase 9 in the plan numbering but executed first chronologically.

### Success Criteria:
- [ ] All Phase 1 files committed
- [ ] `git status` shows clean for Phase 1 files

---

## Testing Strategy

### Unit Tests:
- `search_context` tool: create a test script that calls `createSearchContextTool` with sample skill files and verifies keyword matching works
- Protocol: verify `readLine()` rejects on stdin close

### Integration Tests:
- `echo '{"type":"start","task":{"question":"test"}}' | node harness/agents/research-questions.js` — should bootstrap protocol and fail at LLM call (no OPENAI_API_KEY), confirming protocol + agent factory work

### Automated Verification (full):
```bash
# 1. Syntax check all JS
for f in harness/agents/*.js harness/agents/lib/*.js; do node --check "$f"; done

# 2. Module resolution
cd harness/agents && node -e "import('./lib/agent-factory.js')"

# 3. Go types compile
cd harness && go build ./internal/swarmorch/

# 4. Full lint
just check

# 5. Search context tool test
node harness/agents/test-search-context.js
```

## Performance Considerations

- `search_context` keyword extraction + grep: < 10ms for skill files, < 100ms for source grep (limited to 3 keywords × 10 results)
- Skill index cached after first call per process — no re-reading files
- Each agent subprocess is short-lived (minutes), so cache lifetime is appropriate

## References

- Authoritative plan: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`
- Pi-mono Agent class: `context/pi-mono/packages/agent/src/agent.ts`
- Pi-mono tools: `context/pi-mono/packages/coding-agent/src/core/tools/index.ts`
- Pi-mono skill discovery: `context/pi-mono/packages/coding-agent/src/core/skills.ts`
- Pi-mono compaction: `context/pi-mono/packages/coding-agent/src/core/compaction/compaction.ts`
