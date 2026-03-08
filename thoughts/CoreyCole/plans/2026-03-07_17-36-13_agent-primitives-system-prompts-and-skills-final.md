# Agent Primitives — System Prompts & Skill Files (Final)

## Overview

Write the 7 discoverable skill files and 6 system prompts (as Go constants) for the swarm agent system. This resolves open questions #1 and #2 from the v3 plan. No running code — content authoring only.

This is a revision of the draft plan (`thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md`) incorporating all critical issues and concerns from the staff review (`thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md`).

### Changes From Draft

| Review Issue | Resolution |
|---|---|
| Critical #1: `temporal-conventions.md` describes non-existent code | Added `STATUS: TARGET PATTERNS` header; deferred line references until code exists |
| Critical #2: Plan orchestrator tool selection uses fragile array index | Changed to `tools.find(t => t.name === 'read')` |
| Critical #3: Artifact schemas don't match Go activity unmarshaling | Verified all 6 schemas; Go activities unmarshal `msg.Data` into typed structs wrapping the fields |
| Concern #1: Skill files go stale after implementation | Use function/method names instead of line numbers; added `last_verified` frontmatter |
| Concern #2: 2000-char limit too tight for research findings | Changed to advisory "aim for 2000 characters" with 3000 hard cap |
| Concern #3: Skill files duplicate CLAUDE.md | Added maintenance comment headers |
| Concern #4: Synthesizers can't verify input | Added "flag discrepancies" instruction to synthesizer prompts |
| Concern #5: `ask_orchestrator` available but not documented on some agents | Added "available but not needed" note to affected prompts |

## Current State

- v3 plan fully written with architecture, JSONL protocol, Temporal workflows, DB schema, HTTP API, dashboard
- `flake.nix` updated with bubblewrap + temporal-cli
- No implementation code exists — `harness/agents/`, `internal/temporal/`, `internal/swarm/` all absent
- `OPENAI_API_KEY` not yet in `.env`
- All file references below verified against codebase at commit `773d305`

## What We're NOT Doing

- Writing the agent JS scripts themselves (Phase 2 of v3 plan)
- Writing Go code (Temporal workflows, activities, HTTP handlers)
- Writing the shared JS libraries (`lib/protocol.js`, `lib/orchestrator-tools.js`, `lib/agent-factory.js`)
- Implementing the dashboard
- Any code that runs — this is content authoring only

## Desired End State

After this plan is implemented:

1. `ls harness/agents/skills/` lists 7 `.md` files, each valid markdown with function/method name references (not line numbers)
2. `harness/internal/swarmorch/prompts.go` contains 6 Go string constants, each under 2K tokens (~8K chars)
3. Each skill file has `last_verified` frontmatter and a maintenance comment header
4. Each system prompt's artifact schema matches the corresponding Go activity's expected struct (documented in this plan)
5. The `temporal-conventions.md` skill is clearly marked as target patterns (not existing code)

### Verification

**Automated:**
- `ls harness/agents/skills/*.md | wc -l` → 7
- `grep -c 'last_verified:' harness/agents/skills/*.md` → each file has 1
- `go build ./internal/swarmorch/` compiles (verifies prompts.go syntax)
- `grep -l 'func\|method\|struct' harness/agents/skills/*.md | wc -l` → 7 (all reference real identifiers)

**Manual:**
- Each skill file's function/method references match actual codebase (spot check 3-4 per file)
- System prompts are unambiguous — an LLM reading one knows exactly what to do
- Artifact schemas in prompts match the Go struct definitions listed in this plan

---

## Phase 1: Skill Files

All at `harness/agents/skills/`. Each ~1-3K tokens of concentrated domain knowledge.

**Design principles (from review):**
- Use **function/method names** instead of line numbers (more stable across edits)
- Add `last_verified` frontmatter for staleness tracking
- Add maintenance comment header linking to CLAUDE.md source
- Skills are **pure reference** — domain facts only, no behavioral instructions

### 1. `project-structure.md`

```markdown
---
last_verified: 2026-03-08
---
<!-- Derived from root CLAUDE.md "Project Structure" section. If project structure changes, update both. -->

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
| `harness/internal/swarmorch/` | Swarm orchestration: Temporal activities, agent runner, prompts |
| `harness/agents/` | Pi-mono agent scripts + skill files |
| `harness/views/` | Templ templates (login, lobby, overlay, chat, mayor, create, swarm) |
| `harness/static/` | CSS + JS served at /static/ |
| `templates/3d/` | 3D Bevy/Lightyear multiplayer template |
| `templates/2d/` | 2D Bevy room-based template |
| `templates/boardgame/` | Board game Bevy/WASM template |
| `site/` | Marketing site + onboarding (Echo + templ) |
| `pkg/` | Shared Go packages: worldchannel, mayorchat, markdown, imagegen |
| `thoughts/` | Plans, reviews, notes, research |
| `scripts/` | Build, format, setup, bootstrap scripts |

## Key Entry Points

- Server main: `harness/main.go`
- Route registration: `RegisterRoutes()` in `harness/internal/server/server.go`
- DB init + migrations: `New()` in `harness/internal/db/db.go`
- EventBus: `harness/internal/events/bus.go` — `Subscribe()`, `Publish()`, 100-event buffer
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
---
last_verified: 2026-03-08
---
<!-- Derived from harness/CLAUDE.md and codebase patterns. Update when migrations or query files change. -->

# Database Conventions

## Stack

SQLite + WAL mode, single writer connection (MaxOpenConns=1), 5s busy_timeout.
DB wrapper at `harness/internal/db/db.go` — `DB` struct embeds `*sqlc.Queries`.

## Migration Pattern

Files at `harness/internal/db/migrations/NNN_name.sql`.

**CRITICAL**: New migrations must be manually added to the `migrationFiles`
slice in `runMigrations()` in `harness/internal/db/db.go`. They are NOT auto-discovered.

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
---
last_verified: 2026-03-08
---
<!-- Derived from harness/CLAUDE.md and server.go patterns. Update when auth or route patterns change. -->

# API Conventions

## Framework

Echo v4 (`github.com/labstack/echo/v4`).
Routes registered in `RegisterRoutes()` in `harness/internal/server/server.go`.

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

Reference: `handleWorldSSE()` in `harness/internal/server/events.go`
```

### 4. `ui-conventions.md`

```markdown
---
last_verified: 2026-03-08
---
<!-- Derived from harness/CLAUDE.md Datastar section and templ patterns. Update when Datastar version changes. -->

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
---
last_verified: 2026-03-08
status: target_patterns
---
<!-- STATUS: TARGET PATTERNS — This documents conventions for the swarm system being built.
     The Temporal infrastructure (dev server, namespace) exists, but no Go workflow/activity code
     exists yet. Agents should treat this as a specification, not discoverable code. -->

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
---
last_verified: 2026-03-08
---
<!-- Derived from root CLAUDE.md "Build & Check" and "Running the Server" sections. Update when deps change. -->

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
Build pipeline (`Builder` struct in `harness/internal/builder/builder.go`) serializes per-world.
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
---
last_verified: 2026-03-08
---
<!-- Derived from root CLAUDE.md "Agent System" section. Update when agent architecture changes. -->

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
3. `ProvisionFromWebhook()` in `harness/internal/mayor/mayor.go`:
   - Reads onboarding data from Discord pinned messages
   - Creates world in DB (default template: 2d)
   - Generates workspace files (`writeWorkspaceFiles()` in `workspace.go`)
   - Registers OpenClaw agent (`provisionAgent()` in `openclaw.go`)
4. Mayor responds in Discord via OpenClaw
5. Triggers builds via `POST /api/mayor/build`

## Key Files

- `harness/internal/mayor/mayor.go` — Manager, provision, Discord posting
- `harness/internal/mayor/workspace.go` — Workspace file generation
- `harness/internal/mayor/openclaw.go` — CLI integration (provisionAgent, BindAgentToDiscord, DeleteAgent)
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

System prompts are Go string constants in `harness/internal/swarmorch/prompts.go`, sent to agents via the JSONL start message's `systemPrompt` field.

### Design Notes

- **Prompts are Go constants, not .md files** — short (~1-2K tokens), tightly coupled to artifact schemas, rarely change. Skills are the extensible knowledge layer.
- **Plan orchestrator uses named tool lookup** — `tools.find(t => t.name === 'read')`, not array index.
- **Domain-to-skill mapping in prompt text** — specialist planner prompt says "database → database-conventions.md".
- **No tool call caps** — soft advisory limits. Monitor via SSE dashboard.
- **`ask_orchestrator` is always available** — `agent-factory.js` adds it to every agent. Prompts that don't need it say so explicitly.
- **Artifact schemas match Go structs** — each prompt section below documents both the JS schema and the Go struct it deserializes into.

### Go Struct Definitions (for Activity Unmarshaling)

These are the Go types that `json.Unmarshal(msg.Data, &result)` deserializes into. Each agent's `submit_artifact` schema must produce JSON matching these structs.

```go
// harness/internal/swarmorch/types.go

type GenerateQuestionsResult struct {
    Questions []string `json:"questions"`
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
    Type  string `json:"type"`  // "database", "api", "temporal", "ui", "general"
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
```

### 1. Question Generator (`research-questions.js`)

**Tools**: `submit_artifact` only (pure reasoning — no file tools)
**`ask_orchestrator`**: Available but not needed for this task.

**System prompt:**
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

Call submit_artifact with your questions when ready.
```

**User prompt template:**
```
Decompose this question into sub-questions for parallel investigation:

"{request_text}"

Repository root: {repo_root}
Maximum questions: {max_questions}
```

**Artifact schema (JS):** `Type.Object({ questions: Type.Array(Type.String()) })`
**Go struct:** `GenerateQuestionsResult`

---

### 2. Research Agent (`research-agent.js`)

**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`

**System prompt:**
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
- Aim for under 2000 characters, hard cap at 3000 — compress aggressively
- Prefer finding answers in code over using ask_orchestrator
- Stay focused on your assigned question — do not investigate tangents
```

**User prompt template:**
```
Investigate this question about the codebase:

"{question}"

Repository root: {repo_root}
```

**Artifact schema (JS):**
```
Type.Object({
  question: Type.String(),
  findings: Type.String(),
  files_referenced: Type.Array(Type.String()),
  confidence: Type.Union([Type.Literal("high"), Type.Literal("medium"), Type.Literal("low")])
})
```
**Go struct:** `ResearchFinding`

---

### 3. Research Synthesizer (`research-synthesizer.js`)

**Tools**: `submit_artifact` only (no file tools — pure synthesis)
**`ask_orchestrator`**: Available but not needed — work only with the provided findings.

**System prompt:**
```
You synthesize parallel research findings into a single research document.

Rules:
- Organize by theme or component — NOT by which agent found what
- Preserve ALL file:line references from the findings
- Do NOT add information not present in the findings
- If findings seem contradictory or reference files that conflict with each other,
  note the discrepancy explicitly rather than silently resolving it
- Include a 2-3 sentence summary at the top
- Use markdown headers (##, ###) to organize sections
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

**User prompt template:**
```
Synthesize these research findings into a single document.

Original question: "{request_text}"

Findings from {N} research agents:
{findings_json}

Output path: {output_path}
```

**Artifact schema (JS):**
```
Type.Object({
  document: Type.String(),
  summary: Type.String(),
  output_path: Type.String()
})
```
**Go struct:** `SynthesizeResult`

---

### 4. Plan Orchestrator / Domain Classifier (`plan-orchestrator.js`)

**Tools**: `read` (single tool, selected by name) + `submit_artifact`
**`ask_orchestrator`**: Available if classification is ambiguous, but prefer reading the research doc.

**Tool selection (review fix):**
```javascript
// Use named lookup, NOT array index
const readTool = createReadOnlyTools(repoRoot).find(t => t.name === 'read');
const tools = [readTool, createSubmitArtifactTool(schema, validate)];
```

**System prompt:**
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
- If you need clarification on the request scope, use ask_orchestrator

Call submit_artifact with your domain assignments.
```

**User prompt template:**
```
Classify this code change request into specialist planner domains.

Request: "{request_text}"
Research document: {research_doc_path}

Read the research document, then submit your domain assignments.
```

**Artifact schema (JS):**
```
Type.Object({
  planners: Type.Array(Type.Object({
    type: Type.String(),
    focus: Type.String()
  }))
})
```
**Go struct:** `ClassifyResult`

---

### 5. Specialist Planner (`specialist-planner.js`)

**Tools**: `createReadOnlyTools(cwd)` + `ask_orchestrator` + `submit_artifact`

**System prompt:**
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
- Cite file paths and function/method names for every existing pattern you reference
- Include verification commands (e.g. "cd harness && sqlc generate")
- List all files to be created or modified
- Note dependencies on other domains ("requires migration before API")
- Note risks ("migration must be added to migrationFiles slice manually")
- Keep plan_section under 4000 characters
- Use ask_orchestrator only for cross-domain questions
```

**User prompt template:**
```
Produce an implementation plan for the "{domain}" domain.

Focus: {focus}
Original request: "{request_text}"

Research context:
{research_doc_content}
```

**Artifact schema (JS):**
```
Type.Object({
  domain: Type.String(),
  plan_section: Type.String(),
  files_affected: Type.Array(Type.String()),
  verification_checks: Type.Array(Type.String()),
  risks: Type.Array(Type.String()),
  dependencies: Type.Array(Type.String())
})
```
**Go struct:** `PlannerOutput`

---

### 6. Plan Synthesizer (`plan-synthesizer.js`)

**Tools**: `submit_artifact` only (no file tools)
**`ask_orchestrator`**: Available but not needed — work only with the provided specialist outputs.

**System prompt:**
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
- Preserve all file path and function/method references from specialist outputs
- If specialist outputs seem contradictory, note the conflict explicitly
- Do NOT claim code was changed — this is a plan document
- Combine all verification steps into a final ordered checklist
- Target under 10K characters total

Call submit_artifact when your document is ready.
```

**User prompt template:**
```
Merge these specialist plan sections into a unified implementation plan.

Original request: "{request_text}"
Research summary: {research_doc_summary}

Specialist outputs:
{planner_outputs_json}

Output path: {output_path}
```

**Artifact schema (JS):**
```
Type.Object({
  document: Type.String(),
  summary: Type.String(),
  phase_order: Type.Array(Type.String()),
  output_path: Type.String()
})
```
**Go struct:** `PlanSynthesizeResult`

---

## Implementation Approach

### File Creation Order

1. Create `harness/agents/skills/` directory
2. Write all 7 skill files (Phase 1)
3. Create `harness/internal/swarmorch/` directory
4. Write `harness/internal/swarmorch/types.go` with Go struct definitions
5. Write `harness/internal/swarmorch/prompts.go` with 6 Go string constants (Phase 2)

### System Prompt Delivery

System prompts are Go string constants. The Go activity sends them in the JSONL start message:

```json
{"type":"start","task":{...},"systemPrompt":"<prompt text>"}
```

The agent script reads `startMsg.systemPrompt` and passes to `agent.setSystemPrompt()`.

**Rationale**: Prompts are short (~1-2K tokens), tightly coupled to JS artifact schemas, and rarely change. Skill files are the extensible knowledge layer.

### Skill File Maintenance

Skill files use function/method names (not line numbers) for stability. Each file has:
- `last_verified` frontmatter for staleness tracking
- HTML comment header linking to the source-of-truth (CLAUDE.md section or codebase pattern)

After each implementation phase, do a maintenance pass:
1. Check `last_verified` dates
2. Verify function/method names still exist
3. Update any new tables, routes, or query files
4. Bump `last_verified`

---

## Success Criteria

### Phase 1 (Skill Files) — Automated
- `ls harness/agents/skills/*.md | wc -l` → 7
- Each file has `last_verified:` in frontmatter
- Each file has `<!-- Derived from` or `<!-- STATUS:` comment header
- No line number references (use `grep -n ':[0-9]\+-[0-9]\+' harness/agents/skills/*.md` — should be zero matches outside of code blocks)

### Phase 1 (Skill Files) — Manual
- Spot-check 3-4 function/method references per file against codebase
- Verify `temporal-conventions.md` has the target patterns warning header

### Phase 2 (Prompts) — Automated
- `go build ./internal/swarmorch/` compiles
- Each prompt constant is under 8000 characters (~2K tokens)
- Each artifact schema in prompt text matches the corresponding Go struct in `types.go`

### Phase 2 (Prompts) — Manual
- Read each prompt — an LLM should know exactly what to do
- Verify domain-to-skill mapping in specialist planner prompt is complete
- Verify all 6 artifact schemas match the Go struct definitions

---

## References

- v3 plan (authoritative architecture): `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
- Draft plan: `thoughts/CoreyCole/plans/2026-03-07_17-17-14_agent-primitives-system-prompts-and-skills.md`
- Review: `thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md`
- Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-03-07_17-26-54_agent-primitives-system-prompts-plan.md`
- Pi-mono source: `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/`
- Pi-mono coding tools: `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/tools/index.js`
