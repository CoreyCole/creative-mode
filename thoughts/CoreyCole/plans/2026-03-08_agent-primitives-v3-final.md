# Agent Primitives v3 (Final) — Conversational Agents on Temporal + Pi-Mono

> **Final version** incorporating staff review findings from `thoughts/CoreyCole/reviews/2026-03-07_18-16-52_agent-primitives-v3-conversational-agents_review.md`. Changes marked with `[REVIEW FIX]` comments.

## Overview

Design a minimal agent swarm system that uses **Temporal** for workflow orchestration, **pi-mono** (`@mariozechner/pi-agent-core` + `@mariozechner/pi-ai`) for agent execution powered by **gpt-5.3-codex**, and the existing harness infrastructure (SQLite, EventBus, Datastar SSE) for persistence and observability.

This plan covers **Primitive 1 (Research)** and **Primitive 2 (Code Change Plan)** — the two document-generation primitives. No code changes are performed by the swarm in this phase.

### What Changed From v2

v2 proposed fire-and-forget agent subprocesses (stdin JSON → stdout JSON). v3 introduces three architectural shifts:

1. **Conversational agents** — agents can ask the Go orchestrator follow-up questions via an `ask_orchestrator` tool. The orchestrator loads context and responds. The workflow step only completes when the agent submits a valid artifact via `submit_artifact`.
2. **Lightweight system prompts + discoverable skills** — instead of baking domain knowledge into each agent's system prompt, agents discover and load skill files from disk as needed using `read_file`.
3. **Production pi-mono tools** — use `createReadOnlyTools(cwd)` from `@mariozechner/pi-coding-agent` instead of custom file tools. These have built-in truncation and are battle-tested.

### Corrections From v2

- **Model provider**: Use `getModel('openai', 'gpt-5.3-codex')` — NOT `getModel('openai-codex', ...)`. The `openai-codex` provider routes through ChatGPT OAuth (`chatgpt.com/backend-api`), which requires interactive browser login. The `openai` provider routes through the standard API (`api.openai.com/v1`) and reads `OPENAI_API_KEY` from env. Same model, different auth path.
- **Available models under `openai` provider**: `gpt-5.3-codex`, `gpt-5.3-codex-spark` (lighter), `gpt-5.2-codex`, `gpt-5.1-codex-max`, etc.
- **`createReadOnlyTools(cwd)`** returns `[readTool, grepTool, findTool, lsTool]` — all resolve paths relative to `cwd`, with built-in truncation (2000 lines / 50KB). No `lib/tools.js` needed. **Note**: does NOT prevent path traversal — absolute paths and `../../` work. See Sandboxing section for mitigation.

### Lessons From Previous Attempt (`feature/agent-swarm`)

The prior branch tried to build all 7 primitives simultaneously. It shipped 350+ files and collapsed under scope. Key takeaways:

1. **Too many primitives at once** — we only need research and code-plan now
2. **Claude Code via tmux was heavy** — pi-mono agents are lighter (API-driven, no tmux session management)
3. **The state machine was good** — `DetermineNextPhase()` and typed enums were solid; we'll simplify and reuse the pattern
4. **The dashboard templ was good** — workflow list, detail page, SSE stream patterns all work; we'll adapt for the simpler scope
5. **Hook-based completion was validated** — `CompletionRegistry` with per-session channels, not polling
6. **State in SQLite, not Temporal** — avoids non-deterministic replay bugs. Temporal is a durable scheduler, not the state machine.
7. **Handoff documents work** — primary context transfer mechanism between sessions
8. **Compression at every boundary** — each stage summarizes, never passes raw file contents forward

### Key Infrastructure Facts

| Component | Status | Location |
|-----------|--------|----------|
| Temporal dev server | **Running** | systemd `temporal-dev.service`, ports 7233/8233, namespace `swarm` |
| Pi-mono / OpenClaw | **Installed** | `/opt/openclaw/` (v0.54.0), packages: `pi-agent-core`, `pi-ai`, `pi-coding-agent`, `pi-tui` |
| EventBus | **Implemented** | `harness/internal/events/bus.go` |
| Datastar SSE patterns | **Implemented** | `harness/internal/server/events.go`, mayor dashboard |
| Hook auth middleware | **Implemented** | `hookSecretMiddleware()` in `server.go` |
| `.env` swarm vars | **Configured** | `CM_SWARM_TEMPORAL=true`, Linear keys, Discord swarm channel |
| Temporal SDK | **NOT in go.mod** | Needs `go get go.temporal.io/sdk` |
| Swarm Go code | **None** | `internal/swarm/` doesn't exist on this branch |

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

## Desired End State

After this plan is implemented:

1. A user opens `/swarm` dashboard in the browser
2. They click "Start Research" and enter a question about the codebase
3. The harness creates a `swarm_task` record, starts a Temporal `ResearchWorkflow`
4. Temporal fans out parallel pi-mono research agents (powered by gpt-5.3-codex) that explore the codebase
5. Agents can ask the Go orchestrator follow-up questions when they need more context
6. A synthesis agent compresses findings into a research document at `thoughts/swarm/research/<timestamp>_<task_id>_<slug>.md`
7. The dashboard shows **real-time tool execution events** via SSE (which tool is running, questions asked, completion)
8. Alternatively, the user clicks "Start Code Plan" — the system runs Research first, then specialist planner agents produce a plan document at `thoughts/swarm/plans/<timestamp>_<task_id>_<slug>.md`

## What We're NOT Doing

- **Primitives 2.1–7** (verification, project, parent ticket, orchestrators)
- **Applying code changes** — plans are documents only
- **PR generation** — no git operations
- **OpenClaw/Discord integration** — dashboard is the only UI
- **Learning system** — no learnings, digests, or retrospectives
- **Linear ticket creation** — optional linkage only
- **Claude Code tmux sessions** — we use pi-mono agents instead
- **Gate reviews** — no human-in-the-loop approval gates
- **Tool call caps** — soft cap at 50 (log warning), hard kill at 100 tool calls per agent run [REVIEW FIX: added cap instead of "monitor only"]

---

## Architecture

### Conversational Agent ↔ Orchestrator Protocol

The core innovation: agents are **conversational partners**, not fire-and-forget subprocesses. Communication uses **bidirectional JSONL over stdin/stdout**.

#### Protocol Messages

**Go → Agent (stdin):**
```jsonl
{"type":"start","task":{...},"systemPrompt":"..."}
{"type":"answer","id":"q1","text":"Here's the context you asked for: ..."}
```

**Agent → Go (stdout):**
```jsonl
{"type":"event","event":"tool_execution_start","tool":"grep","data":{"pattern":"EventBus"},"toolCallId":"tc_1"}
{"type":"event","event":"tool_execution_end","tool":"grep","data":{"matches":3},"toolCallId":"tc_1"}
{"type":"question","id":"q1","text":"How are SSE connections established in this codebase?"}
{"type":"result","data":{"findings":"...","files_referenced":[...],"confidence":"high"}}
```

**Note**: `toolCallId` is required on tool events — Go uses it to correlate start/end pairs into `tool_call` spans with accurate duration tracking.

#### Agent Tool Set

Every agent gets these tools:

| Tool | Source | Purpose |
|------|--------|---------|
| `read_file` | `createReadOnlyTools(cwd)` | Read file contents (with truncation) |
| `grep` | `createReadOnlyTools(cwd)` | Search file contents via ripgrep |
| `find` | `createReadOnlyTools(cwd)` | Find files by glob pattern |
| `ls` | `createReadOnlyTools(cwd)` | List directory contents |
| `ask_orchestrator` | Custom | Ask the Go orchestrator a follow-up question |
| `submit_artifact` | Custom | Submit final output (validated before acceptance) |

#### `ask_orchestrator` Implementation

The tool's `execute` function blocks until Go responds:

```javascript
{
  name: 'ask_orchestrator',
  label: 'Ask Orchestrator',
  description: 'Ask the orchestrator when you need context you cannot find with your file tools',
  parameters: Type.Object({
    question: Type.String({ description: 'What you need to know' })
  }),
  execute: async (id, { question }) => {
    const qid = crypto.randomUUID();
    process.stdout.write(JSON.stringify({ type: 'question', id: qid, text: question }) + '\n');
    const answerLine = await readLineFromStdin();
    const { text } = JSON.parse(answerLine);
    return { content: [{ type: 'text', text }], details: {} };
  }
}
```

#### `submit_artifact` Implementation

Validates the artifact schema before accepting:

```javascript
{
  name: 'submit_artifact',
  label: 'Submit Artifact',
  description: 'Submit your final output when your work is complete',
  parameters: artifactSchema, // varies per agent type
  execute: async (id, artifact) => {
    const errors = validateArtifact(artifact);
    if (errors.length > 0) {
      return {
        content: [{ type: 'text', text: `Validation errors:\n${errors.join('\n')}\nFix and resubmit.` }],
        details: { valid: false }
      };
    }
    process.stdout.write(JSON.stringify({ type: 'result', data: artifact }) + '\n');
    return { content: [{ type: 'text', text: 'Artifact submitted successfully.' }], details: { valid: true } };
  }
}
```

#### Go Activity Side

```go
func (a *SwarmActivities) runAgent(ctx context.Context, script string, input any) (json.RawMessage, error) {
    cmd := exec.CommandContext(ctx, "node", filepath.Join(a.agentsDir, script))
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()

    // Send initial task
    enc := json.NewEncoder(stdin)
    enc.Encode(StartMessage{Type: "start", Task: input, SystemPrompt: a.buildSystemPrompt(script)})

    // Message loop: read until result
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        var msg AgentMessage
        json.Unmarshal(scanner.Bytes(), &msg)

        switch msg.Type {
        case "event":
            a.eventBus.Publish("swarm", msg) // → SSE dashboard

        case "question":
            answer := a.answerQuestion(ctx, msg.Text, input)
            enc.Encode(AnswerMessage{Type: "answer", ID: msg.ID, Text: answer})

        case "result":
            cmd.Wait()
            return msg.Data, nil
        }
    }
    return nil, fmt.Errorf("agent exited without submitting artifact")
}
```

#### How Go Answers Questions

[REVIEW FIX: replaced pseudocode with concrete v1 algorithm]

When the orchestrator receives a question from an agent, it uses a three-step keyword extraction + grep pipeline:

1. **Extract keywords** — regex-based extraction of file paths (`foo/bar.go`), capitalized identifiers (`EventBus`, `SwarmTask`), and package names from the question text
2. **Grep skills** — run `grep -ril` with each keyword against `harness/agents/skills/` to find matching skill files
3. **Grep source** — run `grep -rl` with each keyword against `harness/` (excluding `node_modules/`, `static/`, `agents/skills/`) to find matching source files
4. **Read and concatenate** — read matching skill files (full) and source files (truncated to 4KB each), return concatenated with source labels

```go
func (a *SwarmActivities) answerQuestion(ctx context.Context, question string, taskContext any) string {
    var parts []string

    // Step 1: Extract keywords via regex
    // Match file paths (foo/bar.go), CamelCase identifiers (EventBus), and quoted terms
    keywords := extractKeywords(question)
    if len(keywords) == 0 {
        return "I don't have specific context for that question. Use your file tools to investigate directly."
    }

    // Step 2: Grep skills directory for matching skill files
    skillsDir := filepath.Join(a.agentsDir, "skills")
    for _, kw := range keywords {
        // grep -ril <keyword> harness/agents/skills/
        out, err := exec.CommandContext(ctx, "grep", "-ril", kw, skillsDir).Output()
        if err != nil {
            continue
        }
        for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
            if path == "" {
                continue
            }
            content, _ := os.ReadFile(path)
            relPath, _ := filepath.Rel(a.agentsDir, path)
            parts = append(parts, fmt.Sprintf("## Skill: %s\n%s", relPath, string(content)))
        }
    }

    // Step 3: Grep source files (limited to top 5 matches)
    for _, kw := range keywords[:min(len(keywords), 3)] { // limit keywords to avoid huge grep runs
        out, err := exec.CommandContext(ctx, "grep", "-rl",
            "--include=*.go", "--include=*.templ", "--include=*.sql",
            "--exclude-dir=node_modules", "--exclude-dir=static",
            kw, filepath.Join(a.repoRoot, "harness"),
        ).Output()
        if err != nil {
            continue
        }
        matches := strings.Split(strings.TrimSpace(string(out)), "\n")
        for i, path := range matches {
            if path == "" || i >= 5 { // cap at 5 files per keyword
                break
            }
            content, _ := os.ReadFile(path)
            relPath, _ := filepath.Rel(a.repoRoot, path)
            parts = append(parts, fmt.Sprintf("## File: %s\n%s", relPath, truncate(string(content), 4000)))
        }
    }

    // Deduplicate parts by path
    seen := map[string]bool{}
    var unique []string
    for _, p := range parts {
        header := strings.SplitN(p, "\n", 2)[0]
        if !seen[header] {
            seen[header] = true
            unique = append(unique, p)
        }
    }

    if len(unique) == 0 {
        return "I don't have specific context for that question. Use your file tools to investigate directly."
    }
    return strings.Join(unique, "\n\n---\n\n")
}

// extractKeywords pulls file paths, CamelCase identifiers, and quoted terms from a question.
var (
    filePathRe   = regexp.MustCompile(`[\w\-./]+\.\w+`)         // e.g., harness/internal/events/bus.go
    identifierRe = regexp.MustCompile(`[A-Z][a-z]+[A-Z]\w+`)    // e.g., EventBus, SwarmTask
    quotedRe     = regexp.MustCompile("`([^`]+)`")               // e.g., `swarm_spans`
)

func extractKeywords(question string) []string {
    seen := map[string]bool{}
    var keywords []string
    for _, re := range []*regexp.Regexp{filePathRe, identifierRe, quotedRe} {
        for _, m := range re.FindAllString(question, -1) {
            m = strings.Trim(m, "`")
            if !seen[m] && len(m) > 2 {
                seen[m] = true
                keywords = append(keywords, m)
            }
        }
    }
    return keywords
}
```

**v1 limitation**: Keyword extraction is syntactic, not semantic. If an agent asks "how do real-time updates work?" it won't match "EventBus" or "SSE" unless those terms appear in the question. This is acceptable because agents already have file tools — `ask_orchestrator` is a fallback for cross-cutting context, not the primary mechanism. Future: spawn another agent to answer (resolved question #9).

### Discoverable Skills Architecture

Instead of baking domain knowledge into system prompts, skills live on disk:

```
harness/agents/skills/
├── project-structure.md      — directory layout, key files, packages
├── database-conventions.md   — migrations, SQLC, sqlc.yaml overrides, patterns
├── api-conventions.md        — Echo handlers, middleware, Datastar SSE, auth
├── ui-conventions.md         — templ components, Datastar attributes, SSE
├── temporal-conventions.md   — workflow/activity patterns, task queues
├── build-system.md           — Nix, just commands, WASM constraints
└── agent-hierarchy.md        — mayors, president, OpenClaw architecture
```

Each skill file is ~1-3K tokens of concentrated domain knowledge with conventions, patterns, file paths, and examples.

**Agents discover skills via their system prompt:**
```
Skills are available at harness/agents/skills/. Use `ls` to see available
skills, then `read_file` to load any that are relevant to your task.
Load skills BEFORE starting your investigation — they contain conventions
and patterns that will help you produce accurate output.
```

**Adding new domain knowledge = dropping a file.** No agent code changes needed.

### Context Management: Compression at Every Boundary

Each pipeline stage compresses the previous stage's output:

```
User Question (~100 tokens)
    ↓
Question Generator → 3-5 sub-questions (~500 tokens)
    ↓
Research Agents (parallel) → compressed findings per agent (~1-2K each)
    ↓
Research Synthesizer → research document (~3-5K tokens)
    ↓
Plan Orchestrator → specialist assignments (~500 tokens)
    ↓
Specialist Planners (parallel) → plan sections per domain (~2-4K each)
    ↓
Plan Synthesizer → unified plan document (~5-10K tokens)
```

Context never accumulates unboundedly. Each agent produces **summaries with file:line references**, not raw file contents.

### Context Budget Per Agent

| Agent | System Prompt | Input | Tool Growth | Output | Risk |
|-------|--------------|-------|-------------|--------|------|
| Question Generator | ~2K | ~1K | 0 (no tools) | ~500 | None |
| Research Agent (×N) | ~2K | ~1K | ~10-25K | ~1-2K | Medium — file reads |
| Research Synthesizer | ~1K | ~5-10K | 0 (no tools) | ~3-5K | None |
| Plan Orchestrator | ~1K | ~4-6K | 0 (no tools) | ~500 | None |
| Specialist Planner (×N) | ~2K | ~4-6K | ~5-15K | ~2-4K | Low |
| Plan Synthesizer | ~1K | ~8-17K | 0 (no tools) | ~5-10K | None |

[REVIEW FIX] Soft cap at 50 tool calls (log warning), hard kill at 100 tool calls per agent run. Prevents runaway token spend from pathological grep/read loops. See `runAgent()` implementation.

---

## Research Primitive: Detailed Flow

### Stage 1: Question Generator

**Script**: `harness/agents/research-questions.js`
**Tools**: None (pure reasoning)
**Model**: `gpt-5.3-codex`

**System prompt** (~2K tokens):
- Role: decompose a codebase question into 3-5 concrete sub-questions
- Each sub-question should target specific files or patterns
- Skills list available at `harness/agents/skills/` (but this agent doesn't need to load them)
- Output format: JSON array of question strings

**Input**:
```json
{
  "task_id": "abc123",
  "request_text": "How does the EventBus work?",
  "repo_root": "/home/deploy/creative-mode",
  "max_questions": 5
}
```

**Output** (via `submit_artifact`):
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

### Stage 2: Research Agents (parallel)

**Script**: `harness/agents/research-agent.js`
**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`
**Model**: `gpt-5.3-codex`

**System prompt** (~2K tokens):
- Role: investigate a single codebase question using file tools
- Load relevant skills from `harness/agents/skills/` before starting
- Produce compressed findings with file:line references
- Do NOT include raw file contents in output — summarize
- Cite specific paths and line numbers for every claim
- Use `ask_orchestrator` if you need context you can't find with file tools
- Submit findings via `submit_artifact` when done

**Input**:
```json
{
  "task_id": "abc123",
  "question": "What is the EventBus struct definition and how are subscribers stored?",
  "repo_root": "/home/deploy/creative-mode",
  "agent_index": 0
}
```

**Typical agent conversation**:
```
Agent: Let me load relevant skills first.
[calls ls("harness/agents/skills/")]
[calls read_file("harness/agents/skills/project-structure.md")]
Agent: Now let me investigate.
[calls grep({pattern: "EventBus", path: "harness/"})]
[calls read_file("harness/internal/events/bus.go")]
[calls read_file("harness/internal/events/types.go")]
Agent: I need more context about how SSE connections consume these events.
[calls ask_orchestrator({question: "How are SSE connections set up in the harness?"})]
[receives answer with relevant file excerpts]
[calls read_file("harness/internal/server/events.go")]
Agent: I have enough information now.
[calls submit_artifact({findings: "...", files_referenced: [...], confidence: "high"})]
```

**Output** (via `submit_artifact`):
```json
{
  "question": "What is the EventBus struct definition...",
  "findings": "The EventBus is defined at harness/internal/events/bus.go:9-13 as...",
  "files_referenced": ["harness/internal/events/bus.go:9-13", "harness/internal/events/types.go:1-25"],
  "confidence": "high"
}
```

### Stage 3: Research Synthesizer

**Script**: `harness/agents/research-synthesizer.js`
**Tools**: `submit_artifact` only (pure synthesis, no file tools needed)
**Model**: `gpt-5.3-codex`

**System prompt** (~1K tokens):
- Role: compress parallel research findings into a single research document
- Follow the research document artifact schema (below)
- Do not add information not present in the findings
- Organize by theme, not by sub-question

**Input**:
```json
{
  "task_id": "abc123",
  "request_text": "How does the EventBus work?",
  "findings": [
    {"question": "...", "findings": "...", "files_referenced": [...]},
    {"question": "...", "findings": "...", "files_referenced": [...]}
  ],
  "output_path": "thoughts/swarm/research/2026-03-08_abc123_eventbus.md"
}
```

**Output** (via `submit_artifact`):
```json
{
  "document": "<full markdown content>",
  "summary": "The EventBus provides global and per-world pub/sub...",
  "output_path": "thoughts/swarm/research/2026-03-08_abc123_eventbus.md"
}
```

Go writes the document to disk after receiving the result.

---

## Code Change Plan Primitive: Detailed Flow

Extends Research with specialist fan-out.

### Stage 4: Plan Orchestrator (classification)

**Script**: `harness/agents/plan-orchestrator.js`
**Tools**: `read_file` (to read the research doc) + `submit_artifact`
**Model**: `gpt-5.3-codex`

**System prompt** (~1K tokens):
- Role: classify a change request into specialist planner domains
- Available domains: `database`, `api`, `temporal`, `ui`, `general`
- Select 1-4 domains based on what the change touches
- Each gets a `focus` string scoping what it should plan for

**Input**:
```json
{
  "task_id": "abc123",
  "request_text": "Add a swarm_config table and API endpoint to read it",
  "research_doc_path": "thoughts/swarm/research/2026-03-08_abc123_swarm-config.md",
  "repo_root": "/home/deploy/creative-mode"
}
```

**Output** (via `submit_artifact`):
```json
{
  "planners": [
    {"type": "database", "focus": "Create swarm_config table with migration and SQLC queries"},
    {"type": "api", "focus": "Add GET /api/swarm/config endpoint"},
    {"type": "general", "focus": "Wire config loading into server startup"}
  ]
}
```

### Stage 5: Specialist Planners (parallel)

**Script**: `harness/agents/specialist-planner.js` (single script, domain passed as input)
**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`
**Model**: `gpt-5.3-codex`

**System prompt** (~2K tokens):
- Role: produce a plan section for your domain
- Load the relevant skill from `harness/agents/skills/` for your domain
- Read actual code to verify your claims — don't guess at patterns
- Produce: plan section, files affected, verification checks, risks, dependencies
- Use `ask_orchestrator` for cross-domain questions

Each specialist loads its own domain skill:
- `database` domain → loads `skills/database-conventions.md`
- `api` domain → loads `skills/api-conventions.md`
- `temporal` domain → loads `skills/temporal-conventions.md`
- `ui` domain → loads `skills/ui-conventions.md`
- `general` domain → loads `skills/project-structure.md`

**Input**:
```json
{
  "task_id": "abc123",
  "domain": "database",
  "focus": "Create swarm_config table with migration and SQLC queries",
  "request_text": "Add a swarm_config table and API endpoint",
  "research_doc": "<full research document content>",
  "repo_root": "/home/deploy/creative-mode"
}
```

**Output** (via `submit_artifact`):
```json
{
  "domain": "database",
  "plan_section": "### Database Changes\n\n#### Migration 006...\n\n#### SQLC Queries...",
  "files_affected": ["harness/internal/db/migrations/006_swarm.sql"],
  "verification_checks": ["sqlite3 data/creative-mode.db < migration", "cd harness && sqlc generate"],
  "risks": ["Migration must be added to migrationFiles slice manually"],
  "dependencies": ["Must complete before API endpoint implementation"]
}
```

### Stage 6: Plan Synthesizer

**Script**: `harness/agents/plan-synthesizer.js`
**Tools**: `submit_artifact` only
**Model**: `gpt-5.3-codex`

**System prompt** (~1K tokens):
- Role: merge specialist plans into a unified implementation plan
- Resolve cross-domain dependencies using dependency hints from specialist outputs
- Order phases based on dependency graph
- Produce deterministic section headers following the plan artifact schema
- Do not claim code was changed — this is a plan document

**Input**:
```json
{
  "task_id": "abc123",
  "request_text": "Add a swarm_config table and API endpoint",
  "research_doc_summary": "<~500 token summary>",
  "planner_outputs": [
    {"domain": "database", "plan_section": "...", "files_affected": [...], ...},
    {"domain": "api", "plan_section": "...", "files_affected": [...], ...}
  ],
  "output_path": "thoughts/swarm/plans/2026-03-08_abc123_swarm-config.md"
}
```

**Output** (via `submit_artifact`):
```json
{
  "document": "<full plan markdown>",
  "summary": "Plan with 3 phases, 5 files, 4 verification checks",
  "phase_order": ["database", "api", "general"],
  "output_path": "thoughts/swarm/plans/2026-03-08_abc123_swarm-config.md"
}
```

---

## Temporal Architecture

### Two Workflow Types

```
ResearchWorkflow (minutes)
├── UpdateTaskStatus("running")
├── EmitEvent("research.started")
├── GenerateResearchQuestions → sub-questions[]
├── EmitEvent("research.questions_generated")
├── For each question in parallel (workflow.Go):
│   ├── RunResearchAgent(question) → findings  (conversational)
│   └── EmitEvent("research.agent_completed")
├── SynthesizeResearchDoc(allFindings) → artifactPath
├── PersistArtifact(artifactPath)
├── UpdateTaskStatus("completed")
└── EmitEvent("task.completed")

CodeChangePlanWorkflow (minutes)
├── UpdateTaskStatus("running")
├── EmitEvent("code_plan.started")
├── Run child ResearchWorkflow → researchArtifactPath
├── ClassifyPlanDomains → plannerSpecs[]
├── EmitEvent("code_plan.planning_started")
├── For each plannerSpec in parallel (workflow.Go):
│   ├── RunSpecialistPlanner(spec) → plannerOutput  (conversational)
│   └── EmitEvent("code_plan.specialist_completed")
├── SynthesizePlanDoc(allOutputs) → planArtifactPath
├── PersistArtifact(planArtifactPath)
├── UpdateTaskStatus("completed")
└── EmitEvent("task.completed")
```

### Task Queues

| Queue | Concurrency | Purpose |
|-------|-------------|---------|
| `swarm-agents` | 4 | All agent activities (research, planning, synthesis) |

Single queue for now. Split into `swarm-research` / `swarm-planning` later if needed.

### Workflow IDs

```
Temporal workflow ID: swarm-research-{taskID}
                      swarm-codeplan-{taskID}
Tmux: N/A (no tmux — agents are subprocesses)
```

### Activity Definitions

```go
type SwarmActivities struct {
    db        *db.DB
    eventBus  *events.EventBus
    repoRoot  string
    agentsDir string // path to harness/agents/
}

// Agent-running activities (conversational — use bidirectional JSONL)
func (a *SwarmActivities) GenerateResearchQuestions(ctx context.Context, input GenerateQuestionsInput) ([]string, error)
func (a *SwarmActivities) RunResearchAgent(ctx context.Context, input ResearchAgentInput) (ResearchFinding, error)
func (a *SwarmActivities) SynthesizeResearchDoc(ctx context.Context, input SynthesizeInput) (SynthesizeResult, error)
func (a *SwarmActivities) ClassifyPlanDomains(ctx context.Context, input ClassifyInput) ([]PlannerSpec, error)
func (a *SwarmActivities) RunSpecialistPlanner(ctx context.Context, input SpecialistInput) (PlannerOutput, error)
func (a *SwarmActivities) SynthesizePlanDoc(ctx context.Context, input PlanSynthesizeInput) (PlanSynthesizeResult, error)

// Infrastructure activities (simple DB/EventBus operations)
func (a *SwarmActivities) UpdateTaskStatus(ctx context.Context, taskID, status string) error
func (a *SwarmActivities) PersistArtifact(ctx context.Context, taskID, artifactType, filePath string) error
func (a *SwarmActivities) EmitEvent(ctx context.Context, taskID, eventType, detail string) error
```

All agent-running activities use the shared `runAgent()` method (bidirectional JSONL protocol).

### Activity Timeouts & Retries

```go
researchAgentOpts := workflow.ActivityOptions{
    StartToCloseTimeout: 10 * time.Minute,  // generous — agent may ask questions
    HeartbeatTimeout:    2 * time.Minute,    // Go heartbeats while waiting for agent
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    5 * time.Second,
        MaximumAttempts:    3,
        NonRetryableErrorTypes: []string{"ArtifactValidationError"},
    },
}
```

---

## Database Schema

### Migration 006: Swarm Tables

```sql
CREATE TABLE swarm_tasks (
    id TEXT PRIMARY KEY,
    primitive_type TEXT NOT NULL CHECK(primitive_type IN ('research', 'code_change_plan')),
    request_text TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
    workflow_id TEXT,
    linear_issue_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE swarm_research_questions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    question_text TEXT NOT NULL,
    agent_index INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'running', 'completed', 'failed')),
    result_summary TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE swarm_artifacts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL CHECK(artifact_type IN ('research_doc', 'plan_doc')),
    file_path TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Hierarchical span table (Langfuse-inspired trace/span model)
-- Replaces flat swarm_events. Each span has an optional parent, enabling
-- tree views (nested agent → tool calls) and timeline views (Gantt charts).
CREATE TABLE swarm_spans (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    parent_span_id TEXT REFERENCES swarm_spans(id),
    span_type TEXT NOT NULL CHECK(span_type IN (
        'workflow',      -- top-level: ResearchWorkflow or CodeChangePlanWorkflow
        'stage',         -- pipeline stage: question_generation, parallel_research, synthesis
        'agent',         -- single agent subprocess run
        'tool_call',     -- agent tool invocation (grep, read_file, ls, find)
        'llm_call',      -- LLM generation (if pi-mono exposes it)
        'question'       -- ask_orchestrator round-trip
    )),
    name TEXT NOT NULL,            -- e.g. "research-agent-0", "grep", "ask_orchestrator"
    status TEXT NOT NULL DEFAULT 'running'
        CHECK(status IN ('running', 'completed', 'failed')),
    input_json TEXT,               -- task input / tool args (truncated for large payloads)
    output_json TEXT,              -- artifact / tool result (truncated)
    error_message TEXT,            -- set on failure
    started_at TEXT NOT NULL,      -- [REVIEW FIX] set in Go code via time.Now().UTC().Format(time.RFC3339Nano) for ms precision
    ended_at TEXT,
    duration_ms INTEGER,           -- computed in Go: endedAt.Sub(startedAt).Milliseconds()
    metadata_json TEXT             -- extensible: model name, token counts, cost, agent_index, etc.
);

CREATE INDEX idx_swarm_tasks_status ON swarm_tasks(status);
CREATE INDEX idx_swarm_research_questions_task ON swarm_research_questions(task_id);
CREATE INDEX idx_swarm_artifacts_task ON swarm_artifacts(task_id);
CREATE INDEX idx_swarm_spans_task ON swarm_spans(task_id);
CREATE INDEX idx_swarm_spans_parent ON swarm_spans(parent_span_id);
CREATE INDEX idx_swarm_spans_started ON swarm_spans(started_at);
```

---

## Agent Scripts Structure

```
harness/agents/
├── package.json                    — dependencies: pi-agent-core, pi-ai, pi-coding-agent
├── lib/
│   ├── protocol.js                 — bidirectional JSONL stdin/stdout helpers
│   ├── orchestrator-tools.js       — ask_orchestrator + submit_artifact tool factories
│   └── agent-factory.js            — createAgent(model, tools, systemPrompt) helper
├── skills/
│   ├── project-structure.md
│   ├── database-conventions.md
│   ├── api-conventions.md
│   ├── ui-conventions.md
│   ├── temporal-conventions.md
│   ├── build-system.md
│   └── agent-hierarchy.md
├── research-questions.js           — stage 1: decompose question
├── research-agent.js               — stage 2: investigate sub-question
├── research-synthesizer.js         — stage 3: compress findings → doc
├── plan-orchestrator.js            — stage 4: classify domains
├── specialist-planner.js           — stage 5: plan for one domain
└── plan-synthesizer.js             — stage 6: merge plans → doc
```

### `package.json`

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

Use the versions installed at `/opt/openclaw/` — can symlink or copy `node_modules`.

### Shared Library: `lib/protocol.js`

```javascript
import { createInterface } from 'readline';

let rl;

export function initProtocol() {
  rl = createInterface({ input: process.stdin, terminal: false });
}

// [REVIEW FIX] Handle stdin closure (Go crash while agent waits for answer)
export function readLine() {
  return new Promise((resolve, reject) => {
    rl.once('line', resolve);
    rl.once('close', () => reject(new Error('stdin closed — orchestrator exited')));
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

### Shared Library: `lib/orchestrator-tools.js`

```javascript
import { Type } from '@sinclair/typebox';
import { randomUUID } from 'crypto';
import { readLine, sendQuestion, sendResult } from './protocol.js';

export function createAskOrchestratorTool() {
  return {
    name: 'ask_orchestrator',
    label: 'Ask Orchestrator',
    description: 'Ask the orchestrator when you need context you cannot find with your file tools. Use this for architectural questions, cross-cutting concerns, or when you are stuck.',
    parameters: Type.Object({
      question: Type.String({ description: 'What you need to know' })
    }),
    execute: async (_id, { question }) => {
      const qid = randomUUID();
      sendQuestion(qid, question);
      const line = await readLine();
      const { text } = JSON.parse(line);
      return { content: [{ type: 'text', text }], details: {} };
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
      const errors = validate(artifact);
      if (errors.length > 0) {
        return {
          content: [{ type: 'text', text: `Validation errors:\n${errors.join('\n')}\nFix these issues and call submit_artifact again.` }],
          details: { valid: false }
        };
      }
      sendResult(artifact);
      return { content: [{ type: 'text', text: 'Artifact submitted successfully.' }], details: { valid: true } };
    }
  };
}
```

### Shared Library: `lib/agent-factory.js`

```javascript
import { Agent } from '@mariozechner/pi-agent-core';
import { getModel } from '@mariozechner/pi-ai';
import { createReadOnlyTools } from '@mariozechner/pi-coding-agent';
import { createAskOrchestratorTool, createSubmitArtifactTool } from './orchestrator-tools.js';
import { initProtocol, readLine, sendEvent } from './protocol.js';

export async function runAgent({ artifactSchema, validate, systemPrompt, prompt, repoRoot, withFileTools = true }) {
  initProtocol();

  // Read start message
  const startLine = await readLine();
  const startMsg = JSON.parse(startLine);
  const task = startMsg.task;
  const finalSystemPrompt = startMsg.systemPrompt || systemPrompt;

  const model = getModel('openai', 'gpt-5.3-codex');

  const tools = [];
  if (withFileTools) {
    tools.push(...createReadOnlyTools(repoRoot || task.repo_root));
  }
  tools.push(createAskOrchestratorTool());
  tools.push(createSubmitArtifactTool(artifactSchema, validate));

  const agent = new Agent();
  agent.setModel(model);
  agent.setSystemPrompt(finalSystemPrompt);
  agent.setTools(tools);

  // Stream tool events to Go for span creation + SSE dashboard
  // toolCallId is critical: Go uses it to correlate start/end into span lifecycle
  agent.subscribe(event => {
    if (event.type === 'tool_execution_start') {
      sendEvent('tool_execution_start', event.toolName, event.args, event.toolCallId);
    } else if (event.type === 'tool_execution_end') {
      sendEvent('tool_execution_end', event.toolName, event.result, event.toolCallId);
    }
  });

  // Run the agent
  const userPrompt = typeof prompt === 'function' ? prompt(task) : prompt;
  await agent.prompt(userPrompt);
}
```

---

## HTTP API

### Routes

```go
// Swarm API (hook secret auth)
swarmAPI := e.Group("/api/swarm", hookSecretMiddleware())
swarmAPI.POST("/tasks/research", s.handleSwarmStartResearch)
swarmAPI.POST("/tasks/code-change-plan", s.handleSwarmStartCodePlan)
swarmAPI.GET("/tasks/:taskID", s.handleSwarmGetTask)
swarmAPI.POST("/tasks/:taskID/cancel", s.handleSwarmCancelTask)

// [REVIEW FIX] Swarm dashboard (admin only — shows internal traces, consumes API credits)
adminGroup.GET("/swarm", s.handleSwarmDashboard)
adminGroup.GET("/swarm/events", s.handleSwarmSSE)
adminGroup.GET("/swarm/:taskID", s.handleSwarmTaskDetail)
```

### SSE Events (Span-Based)

The dashboard subscribes to `EventBus("swarm")` and receives span lifecycle events. All events carry a full or partial `swarm_spans` row so the dashboard can build the trace tree incrementally.

| Event | Data | Dashboard Update |
|-------|------|-----------------|
| `span.started` | Full span row (id, task_id, parent_span_id, span_type, name, input_json, started_at) | Append span node to tree view. If `span_type=workflow`, add row to task list. |
| `span.completed` | `{span_id, output_json, duration_ms, ended_at}` | Update span node: show duration badge, output preview, mark green |
| `span.failed` | `{span_id, error_message, duration_ms, ended_at}` | Update span node: show error, mark red |
| `task.completed` | `{task_id, artifact_path}` | Show artifact link on task row, update status badge |
| `task.failed` | `{task_id, error}` | Show error on task row |

**Why span-based instead of domain-specific events**: The old design had separate events for `agent.tool_call`, `agent.question_asked`, `research.questions_generated`, etc. — each requiring custom SSE handler code and templ components. Span-based events use a single `SpanRow` component that renders differently based on `span_type`. Adding new span types (e.g., `llm_call` when pi-mono exposes generation events) requires zero SSE handler changes.

**Event type constants** (added to `harness/internal/events/types.go`):
```go
EventSpanStarted   = "span.started"
EventSpanCompleted = "span.completed"
EventSpanFailed    = "span.failed"
EventSwarmTaskCompleted = "swarm.task.completed"
EventSwarmTaskFailed    = "swarm.task.failed"
```

---

## Observability Design (Langfuse-Inspired)

### Design Rationale

Langfuse provides hierarchical trace/span observability for LLM agents: Sessions → Traces → Observations (with 10 types: AGENT, TOOL, GENERATION, etc.). Its key insight is that **one polymorphic span table with parent-child nesting** can represent any agent execution topology — from flat tool calls to deeply nested multi-agent workflows.

We adopt this model but with two advantages over Langfuse:
1. **Real-time SSE streaming** — Langfuse ingests events asynchronously and shows traces after-the-fact. Our dashboard shows agents working *live* (tool calls appearing as they happen, parallel agents progressing simultaneously). This is a debugger, not a log viewer.
2. **No external infrastructure** — Langfuse requires PostgreSQL + ClickHouse + Redis + S3 (~$3-4k/mo self-hosted). We use SQLite + in-memory EventBus, which is appropriate for our scale (<1000 traces/day, single server).

### What We Skip From Langfuse (Not Worth Building Now)

| Langfuse Feature | Why Skip |
|---|---|
| ClickHouse analytics engine | SQLite handles our volume; add OLAP later if needed |
| Cost/token tracking per span | Pi-mono agents don't expose per-call token usage from subprocesses; revisit when we instrument the model provider |
| LLM-as-a-Judge evaluation / scoring | Premature — get spans working first, add scoring later |
| Prompt management / versioning | We have SOUL.md + skill files — better fit for workspace-based agents |
| Multi-tenant / environments | Single deployment, single user |
| Agent graph DAG visualization | Tree + timeline views are sufficient for v1; DAG is a stretch goal |

### Span Hierarchy Example

A research workflow produces this span tree:

```
workflow: ResearchWorkflow (task_id=abc123)              ← 45s total
├── stage: question_generation                           ← 8s
│   └── agent: research-questions                        ← 8s
│       └── tool_call: submit_artifact                   ← <1s
├── stage: parallel_research                             ← 30s
│   ├── agent: research-agent-0                          ← 25s
│   │   ├── tool_call: ls (harness/agents/skills/)       ← <1s
│   │   ├── tool_call: read_file (skills/project-structure.md) ← <1s
│   │   ├── tool_call: grep (EventBus, harness/)         ← 1s
│   │   ├── tool_call: read_file (events/bus.go)         ← <1s
│   │   ├── question: ask_orchestrator                   ← 3s
│   │   │   └── "How are SSE connections established?"
│   │   ├── tool_call: read_file (server/events.go)      ← <1s
│   │   └── tool_call: submit_artifact                   ← <1s
│   ├── agent: research-agent-1                          ← 20s
│   │   └── ... (similar pattern)
│   └── agent: research-agent-2                          ← 28s
│       └── ... (similar pattern)
└── stage: synthesis                                     ← 7s
    └── agent: research-synthesizer                      ← 7s
        └── tool_call: submit_artifact                   ← <1s
```

### Span Creation in `runAgent()`

The Go activity creates and completes spans as it processes JSONL messages from the agent subprocess. Each span is persisted to `swarm_spans` AND published to EventBus for live SSE streaming.

```go
// [REVIEW FIX] spanEntry tracks both ID and start time for duration computation (Concern #1)
type spanEntry struct {
    spanID    string
    startedAt time.Time
}

func (a *SwarmActivities) runAgent(ctx context.Context, taskID, parentSpanID, script string, input any) (json.RawMessage, error) {
    // Create agent span
    agentSpanID := uuid.NewString()
    agentStartedAt := time.Now().UTC()
    a.createSpan(ctx, SpanParams{
        ID: agentSpanID, TaskID: taskID, ParentSpanID: parentSpanID,
        SpanType: "agent", Name: script, InputJSON: marshal(input),
        StartedAt: agentStartedAt,
    })

    // [REVIEW FIX] Inherit parent env so OPENAI_API_KEY, PATH, HOME propagate (Concern #2)
    cmd := a.runner.BuildCommand(ctx, script, os.Environ())
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()

    // Send start message
    json.NewEncoder(stdin).Encode(startMsg)

    // [REVIEW FIX] Track active tool spans with start time for duration (Concern #1)
    activeToolSpans := map[string]spanEntry{} // toolCallId → spanEntry
    // [REVIEW FIX] Track tool call count for soft/hard caps
    toolCallCount := 0

    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        activity.RecordHeartbeat(ctx, "processing")
        msg := parseJSONL(scanner.Text())

        switch msg.Type {
        case "event":
            if msg.Event == "tool_execution_start" {
                toolCallCount++
                if toolCallCount == 50 {
                    slog.Warn("agent approaching tool call limit", "script", script, "taskID", taskID, "count", toolCallCount)
                }
                if toolCallCount >= 100 {
                    // [REVIEW FIX] Hard cap at 100 tool calls — kill runaway agents
                    a.failSpan(ctx, agentSpanID, fmt.Sprintf("killed: exceeded 100 tool calls"))
                    a.failOrphanedChildSpans(ctx, activeToolSpans) // clean up children
                    cmd.Process.Kill()
                    return nil, fmt.Errorf("agent %s exceeded tool call limit (100)", script)
                }
                spanID := uuid.NewString()
                now := time.Now().UTC()
                activeToolSpans[msg.ToolCallID] = spanEntry{spanID: spanID, startedAt: now}
                a.createSpan(ctx, SpanParams{
                    ID: spanID, TaskID: taskID, ParentSpanID: agentSpanID,
                    SpanType: "tool_call", Name: msg.Tool,
                    InputJSON: truncateJSON(msg.Args, 4000),
                    StartedAt: now,
                })
            } else if msg.Event == "tool_execution_end" {
                if entry, ok := activeToolSpans[msg.ToolCallID]; ok {
                    a.completeSpan(ctx, entry.spanID, entry.startedAt, truncateJSON(msg.Result, 4000))
                    delete(activeToolSpans, msg.ToolCallID)
                }
            }

        case "question":
            qSpanID := uuid.NewString()
            qStartedAt := time.Now().UTC()
            a.createSpan(ctx, SpanParams{
                ID: qSpanID, TaskID: taskID, ParentSpanID: agentSpanID,
                SpanType: "question", Name: "ask_orchestrator",
                InputJSON: marshal(map[string]string{"question": msg.Text}),
                StartedAt: qStartedAt,
            })
            answer := a.answerQuestion(ctx, msg.Text, taskContext)
            json.NewEncoder(stdin).Encode(map[string]any{"type": "answer", "id": msg.ID, "text": answer})
            a.completeSpan(ctx, qSpanID, qStartedAt, marshal(map[string]string{"answer": truncate(answer, 2000)}))

        case "result":
            a.completeSpan(ctx, agentSpanID, agentStartedAt, msg.Data)
            return msg.Data, nil
        }
    }

    // [REVIEW FIX] Clean up orphaned child spans on agent crash (Critical #3)
    a.failSpan(ctx, agentSpanID, "agent exited without submitting artifact")
    a.failOrphanedChildSpans(ctx, activeToolSpans)
    return nil, fmt.Errorf("agent exited without submitting artifact")
}

// [REVIEW FIX] Clean up child spans when parent agent crashes
func (a *SwarmActivities) failOrphanedChildSpans(ctx context.Context, activeSpans map[string]spanEntry) {
    for toolCallID, entry := range activeSpans {
        a.failSpan(ctx, entry.spanID, "parent agent crashed")
        delete(activeSpans, toolCallID)
    }
}
```

**Span helper methods** (on `SwarmActivities`):

[REVIEW FIX: `completeSpan` now takes `startedAt` to compute duration in Go, not from DB. `createSpan` uses Go-set timestamps for millisecond precision.]

```go
// SpanParams includes StartedAt set by the caller (time.Now().UTC())
type SpanParams struct {
    ID, TaskID, ParentSpanID, SpanType, Name string
    InputJSON                                json.RawMessage
    StartedAt                                time.Time
}

func (a *SwarmActivities) createSpan(ctx context.Context, p SpanParams) {
    // 1. Insert into swarm_spans table with Go-computed timestamp
    a.db.CreateSwarmSpan(ctx, sqlc.CreateSwarmSpanParams{
        ID: p.ID, TaskID: p.TaskID, ParentSpanID: toNullString(p.ParentSpanID),
        SpanType: p.SpanType, Name: p.Name,
        InputJSON: toNullString(string(p.InputJSON)),
        StartedAt: p.StartedAt.Format(time.RFC3339Nano), // ms precision
    })
    // 2. Publish to EventBus for live SSE
    a.eventBus.Publish("swarm", map[string]any{
        "event": events.EventSpanStarted,
        "span":  p,
    })
}

// [REVIEW FIX] completeSpan accepts startedAt to compute duration_ms in Go (Concern #1)
func (a *SwarmActivities) completeSpan(ctx context.Context, spanID string, startedAt time.Time, outputJSON json.RawMessage) {
    endedAt := time.Now().UTC()
    durationMs := endedAt.Sub(startedAt).Milliseconds()
    // 1. Update swarm_spans row
    a.db.CompleteSwarmSpan(ctx, sqlc.CompleteSwarmSpanParams{
        ID: spanID, Status: "completed", OutputJSON: toNullString(string(outputJSON)),
        EndedAt: toNullString(endedAt.Format(time.RFC3339Nano)),
        DurationMs: sql.NullInt64{Int64: durationMs, Valid: true},
    })
    // 2. Publish completion
    a.eventBus.Publish("swarm", map[string]any{
        "event":       events.EventSpanCompleted,
        "span_id":     spanID,
        "output_json": outputJSON,
        "duration_ms": durationMs,
        "ended_at":    endedAt,
    })
}

func (a *SwarmActivities) failSpan(ctx context.Context, spanID, errMsg string) {
    endedAt := time.Now().UTC()
    a.db.FailSwarmSpan(ctx, sqlc.FailSwarmSpanParams{
        ID: spanID, Status: "failed", ErrorMessage: toNullString(errMsg),
        EndedAt: toNullString(endedAt.Format(time.RFC3339Nano)),
    })
    a.eventBus.Publish("swarm", map[string]any{
        "event":         events.EventSpanFailed,
        "span_id":       spanID,
        "error_message": errMsg,
        "ended_at":      endedAt,
    })
}
```

### Workflow-Level Spans

Temporal workflows create the top-level `workflow` and `stage` spans. Agent spans are created by activities.

```go
func ResearchWorkflow(ctx workflow.Context, task SwarmTask) error {
    // Create workflow span
    workflowSpanID := uuid.NewString()
    workflow.ExecuteActivity(ctx, a.CreateSpan, SpanParams{
        ID: workflowSpanID, TaskID: task.ID,
        SpanType: "workflow", Name: "ResearchWorkflow",
        InputJSON: marshal(task),
    })

    // Stage 1: Question generation
    stage1SpanID := uuid.NewString()
    workflow.ExecuteActivity(ctx, a.CreateSpan, SpanParams{
        ID: stage1SpanID, TaskID: task.ID, ParentSpanID: workflowSpanID,
        SpanType: "stage", Name: "question_generation",
    })
    var questions QuestionArtifact
    workflow.ExecuteActivity(ctx, a.RunAgent, taskID, stage1SpanID, "research-questions.js", task).Get(ctx, &questions)
    workflow.ExecuteActivity(ctx, a.CompleteSpan, stage1SpanID, marshal(questions))

    // Stage 2: Parallel research (fan-out)
    stage2SpanID := uuid.NewString()
    // ... create stage span, fan out agents with parentSpanID=stage2SpanID ...

    // Stage 3: Synthesis
    // ... same pattern ...

    workflow.ExecuteActivity(ctx, a.CompleteSpan, workflowSpanID, marshal(artifact))
    return nil
}
```

---

## Dashboard (Langfuse-Inspired Views)

### Task List Page (`/swarm`)

- Table: task ID, type (research/code_plan), status badge, created_at, duration, artifact links
- "Start Research" button → form with textarea
- "Start Code Plan" button → form with textarea
- SSE connection: `data-init={ datastar.GetSSE("/swarm/events") }`
- Live status: running tasks show a spinner + current stage name

### Task Detail Page (`/swarm/:taskID`)

Three view tabs (inspired by Langfuse's trace detail):

#### Tree View (Default)

Hierarchical nested view of all spans. Each span renders as a row with:
- **Indent** based on nesting depth (parent_span_id chain)
- **Icon** by span_type: workflow=🔄, stage=📦, agent=🤖, tool_call=🔧, question=❓
- **Name** + status badge (running=blue spinner, completed=green ✓, failed=red ✗)
- **Duration** badge (right-aligned, e.g. "25s", "< 1s")
- **Expandable** `<details>` for input/output JSON preview (truncated to 500 chars)
- **Live updates via fat-morph**: [REVIEW FIX] On ANY swarm event, re-query all spans from DB and replace the entire span tree container. This follows the proven mayor dashboard pattern (`mayor_dashboard.go:74-117`) and is self-healing — if an SSE event is dropped due to EventBus buffer overflow (100-event buffer), the next event triggers a full re-render that corrects the stale state. Granular per-span patching can be added as a performance optimization later if the tree re-render is too slow (unlikely for <1000 spans per task).

```go
// [REVIEW FIX] SSE handler uses fat-morph pattern (Critical #2)
// Re-query DB on any event, replace entire tree — proven pattern from mayor dashboard
func (s *Server) handleSwarmSSE(c echo.Context) error {
    sse := datastar.NewSSE(c.Response().Writer, c.Request())
    r := c.Request()
    taskID := c.Param("taskID") // empty on /swarm (task list), set on /swarm/:taskID (detail)

    ch := s.EventBus.Subscribe("swarm")
    defer s.EventBus.Unsubscribe("swarm", ch)

    heartbeat := time.NewTicker(30 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
        case <-ch:
            ctx := r.Context()
            if taskID != "" {
                // Task detail page: re-render full span tree
                spans, _ := s.DB.GetSwarmSpanTree(ctx, taskID)
                task, _ := s.DB.GetSwarmTask(ctx, taskID)
                if err := sse.PatchElementTempl(
                    swarmview.SpanTree(task, spans),
                    datastar.WithSelectorID("span-tree"),
                ); err != nil {
                    return nil
                }
            } else {
                // Task list page: re-render task table
                tasks, _ := s.DB.ListSwarmTasks(ctx)
                if err := sse.PatchElementTempl(
                    swarmview.TaskList(tasks),
                    datastar.WithSelectorID("swarm-task-list"),
                ); err != nil {
                    return nil
                }
            }
        case <-heartbeat.C:
            if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
                return nil
            }
        case <-r.Context().Done():
            return nil
        }
    }
}
```

templ component sketch:
```go
templ SpanRow(span SpanData, depth int) {
    <div id={ "span-" + span.ID }
         class="span-row"
         style={ fmt.Sprintf("padding-left: %dpx", depth * 24) }>
        <span class="span-icon">{ spanTypeIcon(span.SpanType) }</span>
        <span class="span-name">{ span.Name }</span>
        <span class={ "badge", statusClass(span.Status) }>{ span.Status }</span>
        if span.DurationMs > 0 {
            <span class="span-duration">{ formatDuration(span.DurationMs) }</span>
        }
        if span.InputJSON != "" || span.OutputJSON != "" {
            <details class="span-details">
                <summary>I/O</summary>
                if span.InputJSON != "" {
                    <pre class="span-input">{ truncate(span.InputJSON, 500) }</pre>
                }
                if span.OutputJSON != "" {
                    <pre class="span-output">{ truncate(span.OutputJSON, 500) }</pre>
                }
            </details>
        }
    </div>
}
```

#### Timeline View

CSS-based Gantt chart showing span durations as horizontal bars. Reveals parallelism (e.g., 3 research agents running simultaneously).

- Each span = a horizontal bar, width = `duration_ms / workflow_total_ms * 100%`
- X-axis = time elapsed since workflow start
- Y-axis = spans stacked vertically, grouped by parent
- Color-coded by span_type
- Hover shows span name + duration
- Only renders on completed/failed tasks (needs final durations)

Implementation: pure CSS grid + templ. No JavaScript charting library needed. The server computes bar positions and widths, renders as `style="left: X%; width: Y%"` on `<div>` elements.

#### Event Log View

Flat chronological list of all spans (Langfuse's "Log View"). Keyword-searchable via a `data-bind:search_filter` input that uses `data-show="$search_filter === '' || name.includes($search_filter)"` on each row. Useful for finding specific tool calls across a complex trace.

### Templ Files

```
harness/views/swarm/
├── dashboard.templ          — task list page with start forms
├── task_detail.templ        — detail page with tab switching (tree/timeline/log)
├── span_row.templ           — single span row component (used by tree + log views)
├── timeline.templ           — Gantt chart component
└── components.templ         — shared badges, icons, formatters
```

---

## Artifact Contracts

### Research Document

**Path**: `thoughts/swarm/research/YYYY-MM-DD_<task_id>_<slug>.md`

```markdown
---
task_id: <task_id>
primitive: research
timestamp: <ISO 8601>
model: gpt-5.3-codex
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

## Key Files
| File | Lines | Purpose |
|------|-------|---------|
| `path/to/file.go` | 9-13 | Description |

## Summary
<2-3 sentence summary>
```

### Plan Document

**Path**: `thoughts/swarm/plans/YYYY-MM-DD_<task_id>_<slug>.md`

```markdown
---
task_id: <task_id>
primitive: code_change_plan
timestamp: <ISO 8601>
model: gpt-5.3-codex
research_doc: <path to research doc>
---

# Plan: <request description>

## Overview
<1-2 paragraph summary of what this plan achieves>

## Research Reference
<link to research doc>

## Implementation Phases

### Phase 1: <domain>
<plan section from specialist>

### Phase 2: <domain>
...

## Files Affected
| File | Action | Description |
|------|--------|-------------|
| `path/to/file` | Create/Modify | What changes |

## Verification Checks
- [ ] Check 1
- [ ] Check 2

## Risks & Considerations
- Risk 1
- Risk 2

## Dependencies
- Phase 1 must complete before Phase 2
```

---

## Sandboxing Strategy

### Threat Model

Agent subprocesses run pi-mono tools (`createReadOnlyTools(cwd)`) that have **no path traversal protection** — absolute paths and `../../` work. Verified against pi-mono v0.54.0 source. The agents are our code (not user-submitted), but a prompt injection via tool output could cause an agent to read sensitive files (`.env`, SSH keys, database).

### Approach: Defense in Depth, Not Coupled to Core

Sandboxing is an **optional layer** around `exec.CommandContext`, not baked into the JSONL protocol or Temporal activity logic.

```go
// The activity calls runAgent() which uses exec.CommandContext.
// Sandboxing wraps the command construction — everything else is identical.
type AgentRunner interface {
    BuildCommand(ctx context.Context, script string, env []string) *exec.Cmd
}

// v1 (dev): direct exec — trusted agents on our server
// [REVIEW FIX] env param is the FULL parent environment (os.Environ()), not a subset.
// Caller passes os.Environ() so OPENAI_API_KEY, PATH, HOME all propagate.
// To add extra vars: append(os.Environ(), "EXTRA=value")
type DirectRunner struct{ NodePath, AgentsDir string }
func (r *DirectRunner) BuildCommand(ctx context.Context, script string, env []string) *exec.Cmd {
    cmd := exec.CommandContext(ctx, r.NodePath, filepath.Join(r.AgentsDir, script))
    cmd.Env = env // must be os.Environ() or superset — setting cmd.Env replaces, doesn't merge
    return cmd
}

// v2 (VPS hardening): bubblewrap — read-only repo, hidden .env/data
type BwrapRunner struct{ NodePath, AgentsDir, RepoRoot string }
func (r *BwrapRunner) BuildCommand(ctx context.Context, script string, env []string) *exec.Cmd {
    cmd := exec.CommandContext(ctx, "sudo", "bwrap",
        "--ro-bind", "/usr", "/usr",
        "--ro-bind", "/lib", "/lib",
        "--ro-bind", filepath.Dir(r.NodePath), "/nix-profile",
        "--ro-bind", "/nix", "/nix",
        "--ro-bind", r.RepoRoot, "/repo",
        "--ro-bind", "/dev/null", "/repo/harness/.env",
        "--tmpfs", "/repo/data",
        "--ro-bind", r.AgentsDir, "/agents",
        "--proc", "/proc", "--dev", "/dev",
        "--unshare-pid", "--die-with-parent",
        "--", "/nix-profile/node", filepath.Join("/agents", script),
    )
    cmd.Env = env
    return cmd
}
```

### Bubblewrap (bwrap) — Verified Working

Tested on VPS (Ubuntu 24.04, kernel 6.8.0, aarch64):
- **Bidirectional JSONL stdin/stdout**: works identically to unsandboxed
- **Read-only codebase**: `--ro-bind /repo` → writes get `EROFS`
- **`.env` hidden**: `--ro-bind /dev/null /repo/harness/.env` → reads get `EACCES`
- **Database hidden**: `--tmpfs /repo/data` → files get `ENOENT`
- **No home/SSH access**: only explicitly bound paths exist in sandbox
- **`--die-with-parent`**: child dies when Go activity is cancelled
- **Startup overhead**: ~5-10ms (negligible vs minutes-long agent runs)
- **Memory overhead**: zero beyond the child process

**Caveat**: Requires `sudo` or AppArmor profile due to `apparmor_restrict_unprivileged_userns=1` on Ubuntu 24.04. Options: sudoers rule for bwrap, AppArmor profile, or disable the sysctl.

### Production Path: Container Isolation

When moving to Temporal Cloud, the **Temporal server** is managed but **workers still run on our infrastructure**. If workers run in containers (ECS, K8s):
- The container IS the sandbox — mount repo read-only, exclude `.env`
- No bwrap needed (and bwrap inside Docker requires `--privileged`)
- Use `DirectRunner` — container boundaries provide equivalent isolation
- Network policies at container/pod level replace `--unshare-net`

**Temporal Cloud migration only changes connection config** (mTLS certs, namespace endpoint). Activities, workflows, agent scripts, and the JSONL protocol are all identical.

### v1 Decision: Start with DirectRunner

For initial implementation, use `DirectRunner` (plain `exec.CommandContext("node", ...)`). Reasons:
1. Agents are our code, not user-submitted
2. Agents already run with `OPENAI_API_KEY` in env — they have API access by design
3. The codebase is the thing agents are supposed to read
4. bwrap adds operational complexity (sudo/AppArmor) for marginal dev-phase benefit
5. Add `BwrapRunner` later as hardening when agents are stable

---

## Implementation Phases

### Phase 1: Foundation
- Migration 006 + register in `db.go` (includes `swarm_spans` table with parent-child nesting, `started_at` set by Go code not SQLite default)
- SQLC queries for swarm tables including span CRUD: `CreateSwarmSpan`, `CompleteSwarmSpan`, `FailSwarmSpan`, `GetSwarmSpansByTask`, `GetSwarmSpanTree` (recursive CTE for nesting), `CleanupOrphanedSpans`
- `go get go.temporal.io/sdk` + go mod tidy
- Add `temporal-cli` to `flake.nix` (already present on old `feature/agent-swarm` branch)
- Add `OPENAI_API_KEY` to `harness/.env`
- `harness/agents/package.json` + symlink `node_modules` from `/opt/openclaw/` [REVIEW FIX: symlink, not npm install]
- Shared agent libraries (`lib/protocol.js`, `lib/orchestrator-tools.js`, `lib/agent-factory.js`)
- Skill files (`harness/agents/skills/*.md`)
- Add span event types to `harness/internal/events/types.go`: `EventSpanStarted`, `EventSpanCompleted`, `EventSpanFailed`, `EventSwarmTaskCompleted`, `EventSwarmTaskFailed`
- [REVIEW FIX] Startup orphan span cleanup query in `main.go` or Temporal worker init
- [REVIEW FIX] JSONL log cleanup goroutine (delete files older than 7 days from `{dataDir}/logs/swarm/`)

### Phase 2: Agent Scripts
- `research-questions.js`
- `research-agent.js`
- `research-synthesizer.js`
- `plan-orchestrator.js`
- `specialist-planner.js`
- `plan-synthesizer.js`
- Test each script standalone with manual JSON input

### Phase 3: Temporal Workflows + Activities (with Span Instrumentation)
- `harness/internal/temporal/client.go`
- `harness/internal/temporal/activities.go`:
  - `runAgent()` with JSONL protocol + `DirectRunner`
  - `createSpan()`, `completeSpan()`, `failSpan()` helpers — dual-write to SQLite + EventBus
  - Span creation on every JSONL message type: `tool_execution_start/end` → tool_call spans, `question` → question spans
  - `truncateJSON()` utility — cap input/output at 4KB to avoid bloating SQLite
- `harness/internal/temporal/workflows.go`:
  - `ResearchWorkflow` creates `workflow` + `stage` spans, passes `parentSpanID` to activities
  - `CodeChangePlanWorkflow` same pattern
- `harness/internal/temporal/worker.go`

### Phase 4: HTTP API + SSE
- `harness/internal/server/swarm_api.go` (start, get, cancel + span query endpoints)
- `harness/internal/server/swarm_sse.go` — subscribes to `EventBus("swarm")`, dispatches `span.started/completed/failed` events to Datastar SSE. Follows mayor dashboard pattern: `PatchElementTempl` for span rows, `WithModeAppend` for new spans, ID-targeted replace for completions.
- Route registration in `server.go`
- Temporal client in Server struct + main.go initialization

### Phase 5: Dashboard (Langfuse-Inspired Views)
- `harness/views/swarm/dashboard.templ` — task list with live status, start forms
- `harness/views/swarm/task_detail.templ` — three-tab detail page:
  - **Tree view** (default): nested span rows with expand/collapse, live SSE updates
  - **Timeline view**: CSS Gantt chart showing parallelism and duration
  - **Event log**: flat chronological list with keyword search
- `harness/views/swarm/span_row.templ` — polymorphic span component (icon + name + status + duration + expandable I/O)
- `harness/views/swarm/timeline.templ` — Gantt chart component (server-computed bar positions)
- `harness/views/swarm/components.templ` — badges, icons, duration formatters
- `harness/internal/server/swarm_dashboard.go` — page handlers + span tree query
- CSS for span tree indentation, timeline bars, status colors

### Phase 6 (Future): Hardening + Observability Extensions
- Add `BwrapRunner` with sudoers/AppArmor config
- Config flag to select runner (`CM_AGENT_SANDBOX=bwrap|direct`)
- Container-based deployment for Temporal Cloud workers
- Cost/token tracking: instrument pi-mono model provider to emit `llm_call` spans with usage data
- Scoring/evaluation: add `swarm_scores` table (numeric/categorical) attachable to any span, with manual annotation UI on span detail
- Agent graph visualization: DAG view inferred from span parent-child + timing relationships

---

## Resolved Questions

### 1. Model provider
**Use `getModel('openai', 'gpt-5.3-codex')`** — routes through standard OpenAI API, reads `OPENAI_API_KEY` from env. The `openai-codex` provider requires ChatGPT OAuth (interactive browser login) and is not suitable for subprocess agents.

### 2. OPENAI_API_KEY sourcing
Add `OPENAI_API_KEY` to `harness/.env`. Go passes it to agent subprocesses via `cmd.Env`. The `openai` provider in pi-ai reads it from `process.env.OPENAI_API_KEY`.

### 3. Node.js path
`/home/deploy/.nix-profile/bin/node` (v22.22.0, Nix-managed). In PATH, so `"node"` works in `exec.CommandContext`.

### 4. Parallel agent limits
VPS has **31GB total, 29GB available**. 5 Node.js subprocesses (~100-200MB each) use ~1GB. No concern.

### 5. Agent subprocess timeout
`exec.CommandContext` with context timeout. If `submit_artifact` never gets called, context cancels after 10 minutes (matching Temporal `StartToCloseTimeout`), subprocess gets killed, Go returns error, Temporal retries up to `MaximumAttempts: 3`.

### 6. createReadOnlyTools path traversal
**NOT prevented** — absolute paths and `../../` work. Verified against pi-mono v0.54.0 source at `/opt/openclaw/`. Mitigated by: agents are our code (not user-submitted), bwrap available as hardening layer, container isolation in production.

### 7. Error propagation
Subprocess crash → `cmd.Wait()` returns non-zero → scanner loop exits (stdout closes) → Go returns `"agent exited without submitting artifact"` error → Temporal retries.

### 8. Streaming events
**Stream individually as spans.** Each `tool_execution_start` creates a `tool_call` span (persisted to `swarm_spans` + published to EventBus). Each `tool_execution_end` completes the span (updates row + publishes completion). `toolCallId` from the JSONL protocol correlates start/end pairs. Dashboard receives span lifecycle events and updates the tree view in real time.

### 9. answerQuestion sophistication
**v1: regex keyword extraction + grep-based skill/file matching.** [REVIEW FIX] Concrete algorithm now specified — see "How Go Answers Questions" section. Extracts file paths, CamelCase identifiers, and quoted terms via regex, then greps skills/ and source files. Limitation: syntactic, not semantic — "how do real-time updates work?" won't match "EventBus" unless the term appears in the question. Acceptable because agents have file tools as primary mechanism. Future: spawn another agent to answer.

### 10. Agent script hot-reload
**Yes.** Each activity spawns a new subprocess, so edits to agent scripts take effect on next invocation without harness restart.

## Remaining Open Questions

1. **Exact system prompt text** — currently described conceptually, not written. Need concrete prompts for each of the 6 agent scripts.
2. **Skill file content** — what goes in `database-conventions.md`, `api-conventions.md`, etc. Need to write these.
3. **Temporal activity heartbeat pattern** — during long `ask_orchestrator` waits, Go must heartbeat Temporal. Should heartbeat on every JSONL message received from the agent. [Note: heartbeat is already shown in `runAgent()` code — `activity.RecordHeartbeat(ctx, "processing")` on every scanner.Scan()]

## Resolved Questions (Observability)

### 11. Dashboard component design
**Resolved.** [REVIEW FIX: switched from granular per-span patching to fat-morph pattern] Langfuse-inspired three-view design:
- **Tree view** (default): nested `SpanRow` components. SSE handler uses **fat-morph pattern** (same as mayor dashboard) — on any swarm event, re-query `GetSwarmSpanTree` from DB and replace the entire `#span-tree` container. Self-healing: if an SSE event is dropped due to EventBus buffer overflow, the next event corrects the stale state.
- **Timeline view**: CSS Gantt chart for completed tasks. Server computes bar positions as percentages. Shows parallelism (e.g., 3 research agents running simultaneously).
- **Event log view**: flat chronological list with `data-bind:search_filter` for keyword search across span names.

### 12. Langfuse vs. custom observability
**Custom.** Langfuse requires PostgreSQL + ClickHouse + Redis + S3 (~$3-4k/mo self-hosted) and has no Go SDK. Our SSE dashboard has a unique advantage: real-time streaming (Langfuse shows traces after-the-fact). We adopt Langfuse's data model (hierarchical spans with parent-child nesting) but render it live via Datastar SSE. The `swarm_spans` table replaces the flat `swarm_events` table from the original design.

### 13. Span data truncation
**Truncate input/output JSON at 4KB.** Tool args and results can be large (file contents from `read_file`). `truncateJSON()` caps stored/streamed data to keep SQLite lean and SSE responsive. Full data remains in agent subprocess stdout logs (JSONL files at `{dataDir}/logs/`) for debugging.

## Resolved Questions (Staff Review)

### 14. Dashboard access control
[REVIEW FIX] **Admin-only for v1.** The swarm dashboard shows internal agent execution traces and consumes API credits. Route under `adminGroup` (requires `role="admin"`), not `approved`. Can relax to approved users later if needed.

### 15. Agent scripts npm install coordination
[REVIEW FIX] **Symlink `node_modules` from OpenClaw.** `harness/agents/package.json` lists dependencies, but `npm install` is NOT run during build. Instead, the setup script creates: `ln -s /opt/openclaw/node_modules harness/agents/node_modules`. This avoids duplicate installs and version drift. Add a check to `harness/agents/lib/agent-factory.js` that verifies the symlink exists and logs a clear error if not.

### 16. Child workflow span hierarchy
[REVIEW FIX] **Child workflows get `stage` type, not `workflow`.** When `CodeChangePlanWorkflow` runs `ResearchWorkflow` as a child, the child's top-level span is `span_type='stage'` with `name='research'`, NOT `span_type='workflow'`. Only the outermost workflow gets `span_type='workflow'`. This avoids confusing `workflow → stage → workflow` nesting.

### 17. Tool call caps
[REVIEW FIX] **Soft cap at 50, hard kill at 100.** `runAgent()` counts tool calls. At 50: `slog.Warn(...)`. At 100: `failSpan()` on agent + all child spans, `cmd.Process.Kill()`, return error. Prevents runaway API spend from pathological grep/read loops.

### 18. Orphaned span cleanup
[REVIEW FIX] **Two mechanisms.** (1) In `runAgent()` error path: iterate `activeToolSpans` and `failSpan()` each child. (2) On startup: `UPDATE swarm_spans SET status='failed', error_message='orphaned' WHERE status='running' AND ended_at IS NULL AND started_at < datetime('now', '-15 minutes')`. Prevents ghost "running" spans from accumulating after crashes.

### 19. SSE event drop resilience
[REVIEW FIX] **Fat-morph pattern.** SSE handler re-queries DB on any event and replaces entire container (span tree or task list). Follows proven mayor dashboard pattern. EventBus drops on slow subscribers (100-event buffer, non-blocking send) — fat-morph is self-healing because the next event corrects any stale UI state.

### 20. Timestamp precision
[REVIEW FIX] **Millisecond precision via Go timestamps.** `started_at` and `ended_at` are set in Go code using `time.Now().UTC().Format(time.RFC3339Nano)`, not SQLite's `datetime('now')` (which is second-precision only). `duration_ms` is computed in Go as `endedAt.Sub(startedAt).Milliseconds()`.

### 21. JSONL log cleanup
[REVIEW FIX] **Startup cleanup goroutine.** Same pattern as session cleanup in `main.go:108-122` — `time.NewTicker(24 * time.Hour)`, delete JSONL log files older than 7 days from `{dataDir}/logs/swarm/`.
