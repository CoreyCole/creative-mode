---
date: 2026-03-08T01:17:14-08:00
researcher: CoreyCole
git_commit: 2417134078cd956170fdc391a641b2d0b459da69
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives — System Prompts for 6 Agent Scripts + 7 Skill Files"
tags: [implementation, swarm, agent-primitives, pi-mono, system-prompts, skills]
status: draft
last_updated: 2026-03-08
last_updated_by: CoreyCole
---

# Agent Primitives — System Prompts & Skill Files

## Overview

Write the system prompts for 6 pi-mono agent scripts and 7 discoverable skill files. This resolves open questions #1 and #2 from the v3 plan (`thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`).

### Design Principles (from research)

1. **Pi-mono agents are subprocesses** — one task, one artifact, exit. No interactive iteration, no sub-agent spawning.
2. **Pi-mono tool execution is sequential** — tools run one at a time with steering checks between each.
3. **Skill discovery follows pi-mono's pattern** — agents `ls harness/agents/skills/` then `read` relevant files, matching how OpenClaw formats skills as `<available_skills>` XML in system prompts.
4. **Claude Code `.claude/skills/` inspire structure** — role definitions, do/don't rules, output schemas, file:line references. But our prompts are simpler (~1-2K tokens vs 5-10K).
5. **Skill files are pure reference** — domain facts, conventions, file paths, examples. No behavioral instructions. Agent prompts define behavior; skills define domain knowledge.
6. **Compression at every boundary** — agents summarize with file:line references, never dump raw file contents.

## Current State

- v3 plan fully written with architecture, JSONL protocol, Temporal workflows, DB schema, HTTP API, dashboard
- `flake.nix` updated with bubblewrap + temporal-cli
- No implementation code exists — `harness/agents/`, `internal/temporal/`, `internal/swarm/` all absent
- `OPENAI_API_KEY` not yet in `.env`

## What We're NOT Doing

- Writing the agent JS scripts themselves (Phase 2 of v3 plan)
- Writing Go code (Temporal workflows, activities, HTTP handlers)
- Writing the shared JS libraries (`lib/protocol.js`, `lib/orchestrator-tools.js`, `lib/agent-factory.js`)
- Implementing the dashboard
- Any code that runs — this is content authoring only

---

## Phase 1: Skill Files

All at `harness/agents/skills/`. Each ~1-3K tokens of concentrated domain knowledge.

### 1. `project-structure.md`

```markdown
# Project Structure

## Repository Layout

| Directory | Purpose |
|-----------|---------|
| `harness/` | Go server (Echo + SQLite + Datastar + templ) |
| `harness/internal/server/` | HTTP handlers, routes, SSE events |
| `harness/internal/auth/` | Discord OAuth, session middleware |
| `harness/internal/db/` | SQLite wrapper, migrations, sqlc queries |
| `harness/internal/events/` | EventBus: global + per-world pub/sub |
| `harness/internal/world/` | World creation, checkpoints, game servers |
| `harness/internal/claude/` | Claude Code orchestrator (prompt-to-build) |
| `harness/internal/builder/` | Build pipeline: fork → Claude → compile → deploy |
| `harness/internal/tmux/` | Tmux session management |
| `harness/internal/mayor/` | Mayor agent lifecycle, OpenClaw provisioning |
| `harness/internal/president/` | President agent, repo-level operations |
| `harness/internal/discord/` | Discord Gateway listener, message mirroring |
| `harness/views/` | Templ templates (login, lobby, overlay, chat, mayor, create) |
| `harness/static/` | CSS + JS served at /static/ |
| `harness/agents/` | Pi-mono agent scripts + skills |
| `templates/3d/` | 3D Bevy/Lightyear multiplayer template |
| `templates/2d/` | 2D Bevy room-based template |
| `templates/boardgame/` | Board game Bevy/WASM template |
| `site/` | Marketing site + onboarding (Echo + templ) |
| `pkg/` | Shared Go packages: worldchannel, mayorchat, markdown, imagegen |
| `thoughts/` | Plans, reviews, notes, research |
| `scripts/` | Build, format, setup, bootstrap scripts |

## Key Entry Points

- Server main: `harness/main.go`
- Route registration: `harness/internal/server/server.go:107-229`
- DB init + migrations: `harness/internal/db/db.go:25-99`
- EventBus: `harness/internal/events/bus.go` (Subscribe/Publish, 100-event buffer)
- Event types: `harness/internal/events/types.go`

## Build Artifacts

- WASM builds: `data/wasm-builds/{worldID}/{cpID}/`
- DB: `data/creative-mode.db`
- Logs: `data/logs/worlds/{worldID}/{cpID}/`
- Cover images: `data/cover-images/`

## Build Commands

| Command | Where | Purpose |
|---------|-------|---------|
| `just check` | project root | Verify Go + Rust + WASM compile |
| `just fmt` | project root | Format all code |
| `just generate` | harness/ | sqlc + templ + tailwind |
| `just vps-build` | harness/ | Production build |
| `just vps-deploy` | harness/ | Pull + build + restart systemd |
```

### 2. `database-conventions.md`

```markdown
# Database Conventions

## Stack

SQLite + WAL mode, single writer connection (MaxOpenConns=1), 5s busy_timeout.
DB wrapper at `harness/internal/db/db.go` embeds `*sqlc.Queries`.

## Migration Pattern

Files at `harness/internal/db/migrations/NNN_name.sql`.

Current: 001_initial, 002_cascades_indexes, 003_template_type,
004_mayor_and_instrumentation, 005_cover_image.

**CRITICAL**: New migrations must be manually added to the `migrationFiles`
slice in `harness/internal/db/db.go:93-99`. They are NOT auto-discovered.

Migrations use `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE ADD COLUMN`.
Bootstrap logic in `bootstrapExistingMigrations()` handles pre-tracking
migrations by checking schema state (pragma_table_info).

## SQLC Workflow

Config: `harness/sqlc.yaml`
Queries: `harness/internal/db/queries/*.sql` (one file per domain)
Generated: `harness/internal/db/sqlc/` (package `sqlc`)

To add a query:
1. Write SQL in `harness/internal/db/queries/<domain>.sql`
2. Run `cd harness && sqlc generate`
3. Use via `s.DB.<MethodName>(ctx, params)`

The sqlc.yaml rename map ensures Go-idiomatic names (discord_id → DiscordID).

## Key Tables

- `worlds` — game worlds (template_type, mayor fields, discord_channel_id)
- `checkpoints` — versioned world snapshots (status: building/ready/failed)
- `users` — Discord OAuth users (role: pending/approved/admin)
- `sessions` — auth sessions (cookie-based)
- `mayor_messages` — Discord-mirrored per-world messages
- `mayor_activity`, `mayor_builds`, `mayor_sessions` — instrumentation

## Transaction Pattern

```go
tx, err := s.DB.BeginTx(ctx)
if err != nil { return err }
defer tx.Rollback()
q := s.DB.WithTx(tx)
// use q.Method(ctx, params)...
return tx.Commit()
```

## Query Files

12 files in `harness/internal/db/queries/`:
checkpoints, worlds, mayor_messages, mayor_activity, mayor_builds,
mayor_sessions, messages, users, sessions, prompt_history,
user_positions, world_invites.
```

### 3. `api-conventions.md`

```markdown
# API Conventions

## Framework

Echo v4 (`github.com/labstack/echo/v4`).
Routes registered in `harness/internal/server/server.go:107-229`.

## Middleware Chain

```
SessionMiddleware(db) → sets c.Get("user") as *sqlc.User
  ApprovedMiddleware() → rejects role="pending"
    AdminMiddleware() → requires role="admin"
```

Extract user: `user, ok := c.Get("user").(*sqlc.User)`

## Auth Patterns

| Header | Middleware | Purpose |
|--------|-----------|---------|
| Cookie "session" | SessionMiddleware | Browser users |
| `X-Hook-Secret` | hookSecretMiddleware | Webhooks (CM_HOOK_SECRET env) |
| `X-Mayor-Secret` | mayorAuthMiddleware | Per-world mayor API |
| `X-President-Secret` | presidentAuthMiddleware | President API |

## Route Groups

| Group | Auth | Prefix |
|-------|------|--------|
| Public | None | /health, /static/*, /assets/* |
| Hook-protected | X-Hook-Secret | /api/claude-event, /api/world-hatched |
| Mayor-protected | X-Mayor-Secret | /api/mayor/* |
| President-protected | X-President-Secret | /api/president/* |
| Approved users | Session cookie | /world/*, /create/*, /swarm/* |
| Admin | Session + admin | /admin/* |

## Request/Response

Bind JSON: `c.Bind(&req)` with `json:"field"` tags.
Bind Datastar signals: `datastar.ReadSignals(c.Request(), &signals)`.
Return JSON: `c.JSON(http.StatusOK, data)`.
Return SSE: `sse := datastar.NewSSE(c.Response().Writer, c.Request())`.
Errors: `echo.NewHTTPError(http.StatusXxx, "message")`.

## SSE Event Flow

1. `EventBus.Subscribe(worldID)` → buffered channel (100 events)
2. Select loop: event channels + heartbeat ticker (30s) + context.Done
3. Send via: `sse.PatchElementTempl()`, `sse.MarshalAndPatchSignals()`, `sse.ExecuteScript()`
4. Non-blocking publish (drops if subscriber slow)

Reference: `harness/internal/server/events.go:56-113`
```

### 4. `ui-conventions.md`

```markdown
# UI Conventions

## Templ Components

Files: `harness/views/**/*.templ`, compile with `templ generate`.
Render: `render(c, views.Component(args))` (helper in server.go).
Composition: `@Layout("title") { <div>content</div> }`.
Children: `{ children... }` in parent template.

Key view files:
- `views/world/world.templ` — game page with iframe + overlay
- `views/world/overlay.templ` — chat, build status, checkpoint nav
- `views/world/mayor_chat.templ` — mayor message panel
- `views/layout/layout.templ` — base HTML layout

## Datastar v1.0.0-RC.6 Attributes

**CRITICAL**: Plugin suffixes use COLON syntax (NOT dashes):
- `data-on:click` (NOT data-on-click)
- `data-bind:field` (NOT data-bind-field)

| Attribute | Purpose | Example |
|-----------|---------|---------|
| `data-signals` | Init reactive state | `data-signals='{"open": false}'` |
| `data-text` | Bind text content | `data-text="$count"` |
| `data-show` | Conditional visibility | `data-show="$expanded"` |
| `data-class` | Conditional CSS (object) | `data-class="{'active': $tab === 'x'}"` |
| `data-bind:field` | Two-way input binding | `data-bind:chat_text` |
| `data-on:click` | Click handler | `data-on:click="@post('/api/chat')"` |
| `data-init` | SSE on element load | `data-init="@get('/events')"` |
| `data-indicator-*` | Loading state | `data-indicator-fetching` |
| `data-attr-*` | Dynamic attribute | `data-attr-disabled="$fetching"` |

## SSE Patch Methods (Go)

```go
sse.PatchElementTempl(component,
    datastar.WithSelectorID("target"),
    datastar.WithModeAppend())

sse.MarshalAndPatchSignals(map[string]any{"field": ""})

sse.ExecuteScript("window.location.href='/path'")
```

## Best Practices

- Signals for: user input binding, simple UI toggles ONLY
- Server state → PatchElementTempl (not signals)
- `@post`/`@get` auto-include signals in request
- Use `data-init` for SSE connections (NOT data-on-load)
```

### 5. `temporal-conventions.md`

```markdown
# Temporal Conventions

## Infrastructure

Dev server: systemd `temporal-dev.service`, SQLite-backed.
Ports: 7233 (gRPC), 8233 (UI dashboard at http://localhost:8233).
Namespace: `swarm`.
Binary: `/home/deploy/.nix-profile/bin/temporal` (v1.5.1, Nix).
Env: `CM_SWARM_TEMPORAL=true` in `.env`.

## CLI

```bash
temporal workflow list --namespace swarm
temporal schedule list --namespace swarm
temporal workflow describe --namespace swarm --workflow-id <id>
```

## Workflow Rules

Workflow functions are **deterministic** — no I/O, no random, no time.Now().
Side effects go in activities. Temporal replays workflows on recovery.

```go
func MyWorkflow(ctx workflow.Context, input MyInput) (MyOutput, error) {
    actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Minute,
        HeartbeatTimeout:    2 * time.Minute,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval: 5 * time.Second,
            MaximumAttempts: 3,
        },
    })
    var result MyResult
    err := workflow.ExecuteActivity(actCtx, activities.DoWork, input).Get(actCtx, &result)
    return MyOutput{Result: result}, err
}
```

## Parallel Fan-Out

```go
var futures []workflow.Future
for _, item := range items {
    f := workflow.ExecuteActivity(actCtx, activities.Process, item)
    futures = append(futures, f)
}
for _, f := range futures {
    var r Result
    if err := f.Get(actCtx, &r); err != nil { ... }
}
```

## Activity Rules

- Must be **idempotent** (Temporal retries on failure)
- Use `activity.RecordHeartbeat(ctx, details)` for long-running activities
- Context cancellation = Temporal wants to stop you
- State in SQLite, not Temporal — avoid non-deterministic replay bugs

## Task Queues

| Queue | Concurrency | Purpose |
|-------|-------------|---------|
| `swarm-agents` | 4 | All agent subprocess activities |

## Worker

```go
w := worker.New(client, "swarm-agents", worker.Options{
    MaxConcurrentActivityExecutionSize: 4,
})
w.RegisterWorkflow(ResearchWorkflow)
w.RegisterActivity(&activities)
w.Start()
```
```

### 6. `build-system.md`

```markdown
# Build System

## Dependencies

Nix flake (`flake.nix`): go_1_24, golangci-lint, gcc, tmux, sqlc,
nodejs_22, python3, temporal-cli, sqlite, just, jq, git, curl, bubblewrap.
Rust: system-wide via rustup (not Nix).
Node.js: `/home/deploy/.nix-profile/bin/node` v22.22.0.

## Just Commands

### Project Root

| Command | Purpose |
|---------|---------|
| `just check` | Verify Go + Rust + WASM compile (scripts/check.sh) |
| `just fmt` | Format all code (scripts/fmt.sh) |

### harness/

| Command | Purpose |
|---------|---------|
| `just generate` | sqlc generate + templ generate + tailwind |
| `just build` | go build -o harness . |
| `just vps-build` | templ + tailwind + CGO_ENABLED=1 go build |
| `just vps-deploy` | git pull + vps-build + systemctl restart |
| `just sqlc` | sqlc generate only |

## WASM Build Constraints

Each wasm-bindgen uses ~5 GB RAM. VPS has 10 GB total.
Only ONE template build at a time — two simultaneous builds OOM.
Build pipeline (`harness/internal/builder/`) serializes per-world.
Timeouts: 5min incremental, 15min initial.

## VPS Runtime

Harness runs via `air` under systemd (`creative-mode.service`).
Air watches files, rebuilds to `/tmp/harness`.
`scripts/harness-run.sh` sets PATH for Nix, Rust, Go tools.

## Deployment Topology

| Server | Runs | Access |
|--------|------|--------|
| EC2 (Ubuntu) | site (`site/`) | Public: creative-mode.ai → Caddy → :3000 |
| VPS (Nix) | harness (`harness/`) | Internal: Tailscale 100.x.x.x:8080 |

Connected via Tailscale. Site → harness webhook: `POST /api/world-hatched`.
```

### 7. `agent-hierarchy.md`

```markdown
# Agent Hierarchy

## Architecture

```
President (global, optional)
├── Skills: mayor-status, repo-build, template-update, deploy
├── Channel: DISCORD_PRESIDENT_CHANNEL_ID
└── Auto-provisions on startup if env vars set

Mayors (per-world)
├── OpenClaw agent with personality from onboarding
├── Workspace: {OPENCLAW_HOME}/workspaces/world-{worldID}/
│   ├── SOUL.md, AGENTS.md, IDENTITY.md, USER.md, MEMORY.md
│   └── skills/ (world-build, world-status, contribute-learning)
├── Discord channel: one per world (private)
└── Triggers builds via POST /api/mayor/build

Claude Code (per-build session)
├── Runs in tmux: cm-{worldID}-{cpID}
├── Pipeline: ForkCheckpoint → edit → BuildCheckpoint → deploy
└── Hook scripts POST events to /api/claude-event
```

## Mayor Lifecycle

1. Site onboarding → Discord channel created (`pkg/worldchannel`)
2. `POST /api/world-hatched` webhook → harness
3. `harness/internal/mayor/mayor.go` `ProvisionFromWebhook()`:
   - Reads onboarding data from Discord pinned messages
   - Creates world in DB (default template: 2d)
   - Generates workspace files (`workspace.go`)
   - Registers OpenClaw agent
4. Mayor responds in Discord via OpenClaw
5. Triggers builds via `POST /api/mayor/build`

## Key Files

- `harness/internal/mayor/mayor.go` — Manager, provision, Discord posting
- `harness/internal/mayor/workspace.go` — Workspace file generation
- `harness/internal/mayor/openclaw.go` — CLI integration
- `harness/internal/president/president.go` — President lifecycle
- `harness/internal/discord/listener.go` — Gateway message mirroring

## Discord Integration

Single `DISCORD_BOT_TOKEN`, three discordgo session types:
- **REST** (`pkg/worldchannel.Client`): channel creation, messages, pinning
- **Gateway** (`internal/discord.Listener`): real-time message mirroring
  - Classifies: non-bot → "user", bot+[BUILD prefix → "system", other bot → "mayor"
  - Flow: Discord → MessageCreate → DB insert → EventBus publish → SSE
- **Mayor init**: channel registration for listener

## OpenClaw

Installed: `/opt/openclaw/` (v0.54.0).
CLI: `/opt/openclaw/node_modules/.bin/openclaw`.
Gateway health: `localhost:18789`.
```

---

## Phase 2: System Prompts

System prompts will be Go string constants in `harness/internal/swarmorch/prompts.go`, sent to agents via the JSONL start message's `systemPrompt` field.

### Design Notes

- **Plan orchestrator gets only the `read` tool** — not full `createReadOnlyTools`. The agent script will manually push only `createReadOnlyTools(cwd)[0]`.
- **Domain-to-skill mapping is in the specialist planner prompt** — agent discovers and loads via file tools.
- **No tool call caps** — soft limits in prompts ("keep under N chars"). Monitor via SSE dashboard.

### 1. Question Generator (`research-questions.js`)

**Tools**: None (pure reasoning)

```
You decompose codebase questions into investigation-ready sub-questions.

Given a question about a codebase, produce 3-5 concrete sub-questions that
can each be investigated independently by a research agent with file-reading
tools (read, grep, find, ls).

Rules:
- Each sub-question should target specific files, patterns, or components
- Questions must be answerable by reading code — not by asking humans
- Avoid overlap — each sub-question covers distinct territory
- Frame as "How does X work?" or "Where is X defined?" — not "Does X exist?"
- Include likely file paths or package names when you can infer them
- Do NOT exceed the max_questions limit

Call submit_artifact with your questions array when ready.
```

**User prompt**:
```
Decompose this question into sub-questions for parallel investigation:

"{request_text}"

Repository root: {repo_root}
Maximum questions: {max_questions}
```

**Artifact schema**: `{ questions: string[] }`

### 2. Research Agent (`research-agent.js`)

**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`

```
You investigate a single question about a codebase using file tools.

Workflow:
1. Run `ls harness/agents/skills/` to see available domain knowledge
2. Read any skills relevant to your question
3. Use grep and find to locate relevant source files
4. Read key files to understand the implementation
5. If you need architectural context you cannot find in files, use ask_orchestrator
6. Call submit_artifact with your compressed findings

Rules:
- ALWAYS cite file paths with line numbers: `path/to/file.go:9-13`
- Summarize what you find — NEVER include raw file contents in output
- Report confidence: "high" (read the code), "medium" (inferring), "low" (guessing)
- Keep findings under 2000 characters — compress aggressively
- Prefer finding answers in code over using ask_orchestrator
- Stay focused on your assigned question — do not investigate tangents
```

**User prompt**:
```
Investigate this question about the codebase:

"{question}"

Repository root: {repo_root}
```

**Artifact schema**: `{ question: string, findings: string, files_referenced: string[], confidence: "high"|"medium"|"low" }`

### 3. Research Synthesizer (`research-synthesizer.js`)

**Tools**: `submit_artifact` only (no file tools)

```
You synthesize parallel research findings into a single research document.

Rules:
- Organize by theme or component — NOT by which agent found what
- Preserve ALL file:line references from the findings
- Do NOT add information not present in the findings
- Include a 2-3 sentence summary
- Use markdown headers (##, ###) to organize sections
- If findings conflict, note the discrepancy and cite both sources
- Target 3-5K characters for the document

Follow this document structure:
---
task_id: {task_id}
primitive: research
---
# Research: {question}
## Summary
## Findings
### {Theme 1}
### {Theme 2}
## Key Files
| File | Lines | Purpose |

Call submit_artifact when your document is ready.
```

**User prompt**:
```
Synthesize these research findings into a single document.

Original question: "{request_text}"

Findings from {N} research agents:
{findings_json}

Output path: {output_path}
```

**Artifact schema**: `{ document: string, summary: string, output_path: string }`

### 4. Plan Orchestrator / Domain Classifier (`plan-orchestrator.js`)

**Tools**: `read` (single tool) + `submit_artifact`

```
You classify code change requests into specialist planner domains.

Read the research document, then select 1-4 domains that need plans.
Each domain gets a focused scope string — this is the specialist's
entire brief, so make it specific and actionable.

Available domains:
| Domain | Use when |
|--------|----------|
| database | Schema changes, migrations, SQLC queries |
| api | HTTP endpoints, middleware, auth, SSE |
| temporal | Workflows, activities, task queues, schedules |
| ui | Templ components, Datastar attributes, SSE patches |
| general | Wiring, config, startup, cross-cutting concerns |

Rules:
- Read the research document before classifying
- Most changes need 2-3 domains, rarely all 5
- Focus strings must be specific: "Add migration for swarm_tasks table
  with SQLC queries for CRUD" — not "database stuff"
- Single-domain changes are fine — use just 1 if appropriate

Call submit_artifact with your domain assignments.
```

**User prompt**:
```
Classify this code change request into specialist planner domains.

Request: "{request_text}"
Research document: {research_doc_path}

Read the research document, then submit your domain assignments.
```

**Artifact schema**: `{ planners: [{ type: string, focus: string }] }`

### 5. Specialist Planner (`specialist-planner.js`)

**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`

```
You produce an implementation plan section for a specific domain.

Workflow:
1. Load your domain's skill file from harness/agents/skills/:
   - database → database-conventions.md
   - api → api-conventions.md
   - temporal → temporal-conventions.md
   - ui → ui-conventions.md
   - general → project-structure.md
2. Read the actual source files relevant to your focus area
3. Verify existing patterns before proposing new code
4. Call submit_artifact with your plan section

Rules:
- NEVER guess at patterns — read the code and verify
- Cite file:line for every existing pattern you reference
- Include verification commands (e.g. "cd harness && sqlc generate")
- List all files to be created or modified
- Note dependencies on other domains ("requires migration before API")
- Note risks ("migration must be added to migrationFiles slice manually")
- Keep plan_section under 4000 characters
- Use ask_orchestrator only for cross-domain questions
```

**User prompt**:
```
Produce an implementation plan for the "{domain}" domain.

Focus: {focus}
Original request: "{request_text}"

Research context:
{research_doc_content}
```

**Artifact schema**: `{ domain: string, plan_section: string, files_affected: string[], verification_checks: string[], risks: string[], dependencies: string[] }`

### 6. Plan Synthesizer (`plan-synthesizer.js`)

**Tools**: `submit_artifact` only (no file tools)

```
You merge specialist plan sections into a unified implementation plan.

Document structure:
# Plan: {title}
## Summary
{2-3 sentence overview}
## Phase 1: {domain}
{specialist plan content}
### Files Affected
### Verification
## Phase 2: {domain}
...
## Cross-Cutting Risks
## Verification Checklist
- [ ] ordered verification steps

Rules:
- Order phases by dependency: database → api → temporal → ui → general
- Use dependency hints from specialist outputs to determine order
- Resolve conflicts between specialists modifying the same file
- Preserve all file:line references from specialist outputs
- Do NOT claim code was changed — this is a plan document
- Combine all verification steps into a final ordered checklist
- Target under 10K characters total

Call submit_artifact when your document is ready.
```

**User prompt**:
```
Merge these specialist plan sections into a unified implementation plan.

Original request: "{request_text}"
Research summary: {research_doc_summary}

Specialist outputs:
{planner_outputs_json}

Output path: {output_path}
```

**Artifact schema**: `{ document: string, summary: string, phase_order: string[], output_path: string }`

---

## Implementation Approach

### File Creation

1. Create `harness/agents/skills/` directory
2. Write all 7 skill files
3. Create `harness/agents/lib/` directory (already specified in v3 plan)
4. Write 6 agent scripts with embedded system prompts (or Go constants in `prompts.go`)

### System Prompt Delivery

System prompts are Go string constants in `harness/internal/swarmorch/prompts.go`. The Go activity sends them in the JSONL start message:

```json
{"type":"start","task":{...},"systemPrompt":"<prompt text>"}
```

The agent script reads `startMsg.systemPrompt` and passes to `agent.setSystemPrompt()`.

**Rationale**: Prompts are short (~1-2K tokens), tightly coupled to artifact schemas in JS scripts, and rarely change. Skill files are the extensible knowledge layer — not prompts.

### Plan Orchestrator Tool Selection

The plan orchestrator needs only the `read` tool (to read the research doc), not full `createReadOnlyTools`. The script will manually extract just the read tool:

```javascript
const tools = [createReadOnlyTools(repoRoot)[0]]; // read tool only
tools.push(createSubmitArtifactTool(schema, validate));
```

This keeps it lightweight and prevents unnecessary file exploration.

---

## Success Criteria

### Automated
- `ls harness/agents/skills/` lists 7 .md files
- Each skill file is valid markdown with no broken references
- `grep -c "file:line" harness/agents/skills/*.md` shows references in every file

### Manual
- Each skill file's file:line references match actual codebase locations
- System prompts are under 2K tokens each (estimate: chars / 4)
- Prompt instructions are unambiguous — an LLM reading them would know exactly what to do
- Artifact schemas match the types specified in the v3 plan

---

## References

- v3 plan: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
- Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-03-07_16-58-03_agent-primitives-v3-refinement-sandboxing.md`
- Pi-mono source: `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/`
- Claude Code skills: `.claude/skills/` (create_plan.md, research_codebase.md, review_plan.md)
- Claude Code agents: `.claude/agents/` (codebase-analyzer.md, codebase-locator.md)
