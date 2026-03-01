# Agent Swarm Primitives v2 — Implementation Plan

## Overview

Build a general-purpose agent swarm system through 10 flat, composable Claude Code skills and a Temporal-orchestrated Lead FDE. Revision of the v1 plan based on staff engineering review — addresses the critical Project Orchestrator underspecification, context window pressure from nested loading, and five other concerns. The Lead FDE uses Temporal workflows for durable orchestration with task queue-based concurrency control (verification queue at concurrency 1). LLM reasoning belongs in the worker sessions, not the scheduler — the orchestration logic is mechanical (poll, compare, spawn/reap) but Temporal gives us durable state, retry policies, queue management, and observability via its web UI.

## Current State Analysis

### What Exists
- **38 linear-cli skills** installed (`.agents/skills/linear-*/SKILL.md`) — full Linear CRUD, search, relations, projects, sprints
- **Workflow commands** working: `/create_plan`, `/implement_plan`, `/validate_plan`, `/research_codebase`, `/create_handoff`, `/resume_handoff`, `/describe_pr`, `/create_spec`, `/tkt`
- **OpenClaw president agent** pattern (`harness/internal/president/`) — heartbeat loop, Discord binding, workspace files, skills (reference only — Lead FDE will NOT use OpenClaw)
- **Mayor agent** pattern (`harness/internal/mayor/`) — per-world provisioning, Discord REST API, event-driven
- **Claude Code orchestrator** (`harness/internal/claude/`) — tmux session management, hook scripts, orphaned session reaping
- **Skill directory pattern** proven by `playwright-cli` — SKILL.md + `references/` sub-files loaded on demand
- **Linear team** `CM` configured, **Graphite** for PR stacking

### Key Discoveries
- `harness/internal/president/president.go` — Manager struct with `Provision()`, idempotent (checks SOUL.md), writes 6 workspace files, registers via CLI, binds to Discord
- `harness/internal/server/president_api.go:19-36` — Auth middleware validates `X-President-Secret` header from env var
- `harness/internal/tmux/session.go:18-60` — Session naming `cm-{worldID}-{cpID}`, prompt via `--input-file` (no shell injection)
- `harness/internal/claude/claude.go:290-346` — Orphaned session reaper scans `tmux list-sessions`, kills stale sessions every 5 min
- linear-cli label create is NOT idempotent — errors `"A label with the name already exists"` on duplicate. Need check-then-create pattern.
- linear-cli has native `--dry-run` flag. Exit codes: 0=Success, 1=General, 2=NotFound, 3=Auth, 4=RateLimited.
- Existing Go ticker patterns: orphaned session reaper (5-min, `main.go:223-234`), expired session cleanup (1-hr, `main.go:108-122`).
- No existing queue, retry, or worker pool infrastructure in the harness — all async work is fire-and-forget goroutines. No global build serialization beyond per-user rate limiting.
- VPS Nix flake (`flake.nix`) provides all dev tools via `nix profile install`. Temporal CLI will be added to the flake.

### What's Missing
- No swarm skills or primitives
- No Linear conventions for agent workflow (labels, comment format, ticket structure)
- No orchestration agents beyond president/mayors
- No integration between existing commands and Linear ticket tracking
- No project decomposition or dependency graph tooling

## Desired End State

A fully operational agent swarm where:
1. **Any idea** given to a swarm primitive gets classified, tracked, implemented, and verified — Linear is the source of truth
2. **Projects** decompose into parent → child tickets with dependency graphs and parallelism analysis
3. **Code changes** follow a full lifecycle: ticket → research → plan revision (agent-only) → implement/verify loop → PR → human review
4. **Lead FDE** runs Temporal workflows — polling Linear, spawning Claude Code sessions, detecting stalls, reaping dead sessions — with durable state, queue management (verification at concurrency 1), retry policies, and web UI observability. Zero LLM cost for orchestration.
5. **Human gates** are explicit and minimal: classification, project kickoff, PR merge
6. **Dry-run** works on all primitives for safe iteration

### Verification
1. `/swarm-setup` creates all labels idempotently (second run = no-op)
2. `/swarm-research "how does auth work"` → creates ticket + research doc + Linear comments
3. `/swarm-code-change "add /version endpoint"` → full lifecycle from research to PR
4. `/swarm-project "build notification system"` → project + ticket hierarchy + dependency graph
5. `/swarm-resume CM-XXX` → reads comment history, continues from last phase
6. `GET /api/lead-fde/health` → returns active sessions + capacity
7. `POST /api/lead-fde/spawn` → creates tmux session, enforces max 4

## What We're NOT Doing

- Building a custom workflow engine (Linear IS the workflow engine)
- Replacing existing commands (swarm primitives COMPOSE them)
- LLM-based orchestration (Temporal workflows are deterministic Go code; LLM reasoning is only in the Claude Code worker sessions)
- Auto-merging PRs (human always reviews and merges)
- Slack integration (Discord-only; Slack acknowledged as future extension)
- Custom UI for swarm management (Linear UI + CLI is sufficient)
- Nested primitive loading (flat primitives only, composition hints in docs)

## Implementation Approach

**Flat skills**: 10 independent Claude Code skills (`/swarm-research`, `/swarm-code-change`, etc.) instead of one `/swarm` router. Each SKILL.md is self-contained (~100-150 lines). No primitive loads another. Composition hints document recommended sequences.

**Agent-only plan review**: Plan-review primitive runs in a separate Agent subagent for objectivity. No `AskUserQuestion` in autonomous sessions. Three human gates: classification, project kickoff, PR merge.

**Dry-run from Phase 1**: Every primitive respects `--dry-run`. linear-cli has native support. File writes print path + first 10 lines.

**Linear comments as state**: Every phase transition writes a structured comment (`RESEARCH:`, `PLAN:`, `IMPL:`, `VERIFY:`, `PR:`). The resume primitive parses these to reconstruct state. Temporal workflows read them to track progress.

**Temporal-orchestrated Lead FDE**: Temporal workflows replace both the OpenClaw heartbeat and the Go ticker. Two task queues: `swarm-general` (concurrency 3) for research/plan/implement/PR activities, and `swarm-verify` (concurrency 1) for verification — ensuring only one `just check` or build runs at a time to prevent OOM. Durable workflow state survives harness restarts. Temporal web UI provides observability. API endpoints expose health/spawn/kill for manual use and the `/swarm-status` skill.

---

## Skill Directory Structure

```
.claude/skills/
  swarm-conventions/
    SKILL.md                              # Shared reference (~150 lines)
    templates/
      ticket-description.md               # Ticket footer template
      research-doc.md                     # Research doc template
      plan-doc.md                         # Plan doc template
  swarm-setup/SKILL.md                    # One-time label setup (~60 lines)
  swarm-research/SKILL.md                 # Research primitive (~120 lines)
  swarm-code-change/SKILL.md              # Code change lifecycle (~150 lines)
  swarm-project/SKILL.md                  # Project decomposition (~150 lines)
  swarm-verify/SKILL.md                   # Verification (~100 lines)
  swarm-plan-review/SKILL.md              # Plan review, agent context (~120 lines)
  swarm-project-verify/SKILL.md           # Project verification (~100 lines)
  swarm-status/SKILL.md                   # Status dashboard (~80 lines)
  swarm-resume/SKILL.md                   # Resume from ticket history (~120 lines)
```

Note: `swarm-heartbeat` removed — orchestration is now Temporal workflows in `harness/internal/leadfde/`, not a Claude Code skill.

---

## Phase 1: Foundation (Conventions, Setup, Templates)

### Overview
Create the shared conventions reference, one-time setup primitive, and document templates. Establishes the dry-run convention and label taxonomy.

### Changes Required

#### 1. Conventions Reference
**File**: `.claude/skills/swarm-conventions/SKILL.md` (~150 lines)

```yaml
---
name: swarm-conventions
description: Reference for swarm agent conventions — labels, ticket format, comment format, doc templates. Not an action primitive. Load when creating/updating swarm-tracked tickets.
allowed-tools: Bash, Read
---
```

Content:
- **Linear team**: `CM`
- **Labels** with colors: `swarm:research` (#3B82F6), `swarm:code-change` (#10B981), `swarm:verification` (#EAB308), `swarm:project` (#8B5CF6), `swarm:plan` (#F97316), `swarm:orchestration` (#EF4444), plus `type:bug`, `type:feature`, `type:refactor`, `type:prototype`
- **Ticket footer**: Structured YAML block — `swarm_type`, `parent_ticket`, `research_path`, `plan_path`, `pr_url`, `previous_attempt`, `dependencies`
- **Comment prefixes** (parseable by resume): `RESEARCH:`, `PLAN:`, `PLAN-REVIEW:`, `IMPL:`, `VERIFY:`, `PR:`, `REVISION:`, `RESTART:`, `HEARTBEAT:`, `RESUME:`
- **Lifecycle states**: Triage → Backlog → Todo → In Progress → In Review → Done
- **Doc paths**: `thoughts/{git_user}/research/{timestamp}_{slug}.md`, `thoughts/{git_user}/plans/{timestamp}_{slug}.md`
- **Dry-run convention**: All primitives accept `--dry-run`. Print `[DRY-RUN]` prefix per action. linear-cli native `--dry-run`.
- **Rate limits**: 1500 req/hr. Batch sequentially (linear-cli handles 429 retry).
- **Error handling**: exit 3 → stop, `linear-cli config doctor`. exit 4 → wait 60s, retry once. Mid-execution → comment on ticket, keep In Progress.

#### 2. Templates
**File**: `.claude/skills/swarm-conventions/templates/ticket-description.md` (~30 lines)
Structured footer template with swarm metadata fields.

**File**: `.claude/skills/swarm-conventions/templates/research-doc.md` (~25 lines)
YAML frontmatter: `linear_ticket`, `date`, `author`, `topic`. Sections: Summary, Key Findings, Open Questions, Files Referenced, Next Steps.

**File**: `.claude/skills/swarm-conventions/templates/plan-doc.md` (~30 lines)
YAML frontmatter: `linear_ticket`, `linear_project`, `date`, `author`. Sections: Goal, Success Criteria, Phases, File Inventory, Dependencies.

#### 3. Setup Primitive
**File**: `.claude/skills/swarm-setup/SKILL.md` (~60 lines)

```yaml
---
name: swarm-setup
description: One-time setup for agent swarm — creates Linear labels, verifies CLI auth. Run once before using other swarm primitives.
allowed-tools: Bash
---
```

Steps:
1. `linear-cli config doctor` — verify auth
2. Check-then-create for each label: `linear-cli l list --output json --compact` → check existence → `linear-cli l create "name" --color "#hex"` only if missing
3. `linear-cli st list -t CM --output json` — verify workflow states
4. Report summary; supports `--dry-run`

### Success Criteria

#### Automated Verification:
- [ ] `ls .claude/skills/swarm-conventions/SKILL.md` exists
- [ ] `ls .claude/skills/swarm-conventions/templates/` shows 3 template files
- [ ] `ls .claude/skills/swarm-setup/SKILL.md` exists
- [ ] YAML frontmatter parses correctly (grep for `---` delimiters)

#### Manual Verification:
- [ ] `/swarm-setup` creates all labels in Linear (verify in Linear UI)
- [ ] `/swarm-setup` run twice = no errors (idempotent)
- [ ] `/swarm-setup --dry-run` prints labels without creating them

---

## Phase 2: Core Primitives (Research, Code-Change, Project)

### Overview
Create the three core primitives. Each is flat, self-contained, and documents composition hints.

### Changes Required

#### 1. Research Primitive
**File**: `.claude/skills/swarm-research/SKILL.md` (~120 lines)

```yaml
---
name: swarm-research
description: Deep research on a topic or ticket with Linear tracking. Creates research doc in thoughts/ and updates ticket.
allowed-tools: Bash, Read, Glob, Grep, Agent, WebSearch, WebFetch
---
```

Process:
1. Parse input: topic string or ticket ID. Optional `--dry-run`.
2. Get or create ticket: `linear-cli i create "[RESEARCH] <topic>" -t CM -l swarm:research --id-only`
3. Start work: `linear-cli i update $TICKET -s "In Progress"`, comment `RESEARCH: Starting`
4. Research: Agent subagents (codebase-analyzer, web-search-researcher) in parallel
5. Write doc: `thoughts/{user}/research/{timestamp}_{slug}.md` with `linear_ticket` frontmatter
6. Update Linear: comment summary + key findings + open questions. Done if no open questions.
7. Output: doc path + ticket ID

**Composition hint**: "After research, run `/swarm-code-change` with this ticket as context."

#### 2. Code Change Lifecycle
**File**: `.claude/skills/swarm-code-change/SKILL.md` (~150 lines)

```yaml
---
name: swarm-code-change
description: Full code change lifecycle — research, plan, implement, verify, PR. Tracks everything in Linear.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent
---
```

Process:
1. Parse input: description, ticket ID, or `previous:CM-XXX` for restart. Optional `--dry-run`.
2. **Initiate**: Get or create ticket with `swarm:code-change`. If restart, `linear-cli rel add $NEW -r related $OLD`, comment `RESTART:`. Start work.
3. **Research**: Create child research ticket, set parent relation (`linear-cli rel parent $CHILD $TICKET`), run research inline (Agent subagent). Comment `RESEARCH: <summary>`.
4. **Plan**: Use `/create_plan` with ticket + research context. Comment `PLAN: Created at <path>`.
5. **Plan review (agent-only)**: Spawn Agent subagent to review plan (file existence, patterns, edge cases). Verdict: APPROVE/REVISE. Comment `PLAN-REVIEW:`. Max 3 revisions.
6. **Implement**: Use `/implement_plan`. Comment `IMPL: Starting`.
7. **Verify**: Run automated checks from plan criteria + `just check`. Comment `VERIFY: PASS/FAIL`. If FAIL, loop to implement (max 3 attempts).
8. **Ship**: Branch, commit, push, PR via `linear-cli g pr $TICKET`. Comment `PR: <url>`. Status → In Review.

**Human gates**: Classification (step 1) and PR merge (step 8). No `AskUserQuestion` between.

#### 3. Project Decomposition
**File**: `.claude/skills/swarm-project/SKILL.md` (~150 lines)

```yaml
---
name: swarm-project
description: Decompose a high-level goal into tracked workstreams with dependency analysis. Creates Linear project + ticket hierarchy.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent, WebSearch
---
```

Process:
1. Parse input: goal or project ID. Optional `--dry-run`.
2. Create project: `linear-cli p create "<name>" -t CM --status planned --id-only`
3. Research: 2-5 research questions → child tickets → Agent subagents (parallel)
4. Decompose: 2-7 workstreams with scope, dependencies, complexity
5. Create tickets: parent per workstream, children per task. Set blocking relations (`linear-cli rel add $A -r blocks $B`).
6. Write plan: `thoughts/{user}/plans/{timestamp}_{slug}.md` with dependency graph (ASCII), parallelism analysis
7. Plan review: Agent subagent reviews. Comment `PLAN-REVIEW:`.
8. **Human gate (project kickoff)**: Present plan summary, ask approval
9. Update: `linear-cli p update $PROJECT --status started`
10. Document which workstreams can start (no blockers) vs blocked

### Success Criteria

#### Automated Verification:
- [ ] `ls .claude/skills/swarm-research/SKILL.md` exists
- [ ] `ls .claude/skills/swarm-code-change/SKILL.md` exists
- [ ] `ls .claude/skills/swarm-project/SKILL.md` exists
- [ ] Each file has valid YAML frontmatter
- [ ] No primitive file references another primitive's SKILL.md path

#### Manual Verification:
- [ ] `/swarm-research "how does session auth work"` → ticket + doc + comments in Linear
- [ ] `/swarm-code-change "add /health-detailed endpoint"` → full lifecycle to PR
- [ ] `/swarm-project "improve error handling"` → project + ticket hierarchy
- [ ] `--dry-run` prints actions without executing on all three

---

## Phase 3: Support Primitives (Verify, Plan-Review, Status, Resume)

### Overview
Create verification, review, and state recovery primitives. Note: orchestration heartbeat moved to Phase 4 as Temporal workflows (not a Claude Code skill).

### Changes Required

#### 1. Verification
**File**: `.claude/skills/swarm-verify/SKILL.md` (~100 lines)
Read plan → extract criteria → run each automated check → record results → comment structured summary → return PASS/FAIL.

#### 2. Plan Review
**File**: `.claude/skills/swarm-plan-review/SKILL.md` (~120 lines)
Read plan → evaluate (completeness, feasibility, file refs exist, edge cases, gaps) → verdict APPROVE/REVISE → comment. "Run in Agent subagent for isolation."

#### 3. Project Verification
**File**: `.claude/skills/swarm-project-verify/SKILL.md` (~100 lines)
List project issues → categorize by workstream/status → identify blockers (>24h stale) → comment summary → recommend continue/reprioritize/escalate.

#### 4. Status Dashboard
**File**: `.claude/skills/swarm-status/SKILL.md` (~80 lines)
Dashboard: active projects table, awaiting review, in-progress with last update, recently completed. Also queries Lead FDE health API for active session info. Read-only, no side effects.

#### 5. Resume from Ticket History
**File**: `.claude/skills/swarm-resume/SKILL.md` (~120 lines)

```yaml
---
name: swarm-resume
description: Resume work on a ticket by reading its Linear comment history to reconstruct state. Picks up where the last session left off.
allowed-tools: Bash, Read, Glob, Grep, Write, Edit, Agent
---
```

Process:
1. `linear-cli i get $TICKET --output json` — ticket details
2. `linear-cli cm list $TICKET --output json` — full comment history
3. Parse comment prefixes to determine last completed phase
4. Read referenced docs (research, plan) from paths in comments
5. Comment `RESUME: Resuming from <phase>. Context: <summary>`
6. Continue from appropriate phase based on label type
7. Example: `swarm:code-change` + last `VERIFY: FAIL` → resume at implementation

### Success Criteria

#### Automated Verification:
- [ ] All 9 primitive SKILL.md files exist under `.claude/skills/swarm-*/`
- [ ] Each has valid YAML frontmatter with `name`, `description`, `allowed-tools`

#### Manual Verification:
- [ ] `/swarm-verify CM-XXX` runs checks and reports PASS/FAIL
- [ ] `/swarm-plan-review` produces structured review in Agent subagent
- [ ] `/swarm-status` displays formatted dashboard (including Lead FDE session info)
- [ ] `/swarm-resume CM-XXX` correctly identifies last phase and continues

---

## Phase 4: Lead FDE (Temporal Workflows + Harness API)

### Overview
Create the Lead FDE as Temporal workflows with session tracking and a harness API. Temporal provides durable workflow execution, task queue-based concurrency control, retry policies, and web UI observability. The orchestration logic is deterministic Go code — no LLM cost. Intelligence lives in the Claude Code worker sessions.

### Design Decision: Why Temporal

**Evaluated and rejected**:
- **OpenClaw heartbeat**: Burns ~$0.03-0.10+ per tick for LLM to interpret HEARTBEAT.md and run `curl`/`linear-cli`. Risk of hallucinating flags. No queue management. No retry durability.
- **Go `time.Ticker`**: Simple, zero dependencies. But: in-memory state lost on restart, no queue management for verification, no built-in retry policies, no observability UI. Would need to build a custom semaphore for verification serialization.

**Why Temporal wins**:
1. **Verification queue**: `swarm-verify` task queue with `MaxConcurrentActivityExecutionSize: 1`. Only one `just check` or build runs at a time — prevents OOM on the 10 GB VPS. No custom semaphore needed.
2. **Durable retry loops**: The implement/verify loop (max 3 attempts) and plan-review loop (max 3 revisions) survive harness restarts. With a Go ticker, a crash mid-loop loses state.
3. **Session recovery**: Activities record tmux session names in heartbeat details. On worker restart, the activity resumes polling the same tmux session instead of spawning a duplicate.
4. **Observability**: Temporal web UI shows running workflows, current activity, retry history, queue depth. No need to build a custom dashboard.
5. **Resource footprint**: ~200 MB RAM for Temporal server with SQLite backend. Fits easily on the 10 GB VPS.

### Infrastructure

#### Temporal Server
**Install via Nix**: Add `temporal-cli` to `flake.nix` packages. The Temporal CLI includes `temporal server start-dev` which runs a full server with SQLite persistence.

**File**: `flake.nix` — add `temporal-cli` to the `paths` list in `packages.default` and `devShells.default`.

**Systemd service**: `scripts/vps-bootstrap.sh` — add a new step to create `/etc/systemd/system/temporal.service`:
```ini
[Unit]
Description=Temporal Server (SQLite)
After=network.target
Before=creative-mode.service

[Service]
Type=simple
User=deploy
ExecStart=/home/deploy/.nix-profile/bin/temporal server start-dev \
    --db-filename /home/deploy/creative-mode/data/temporal.db \
    --port 7233 --ui-port 8233 --headless \
    --namespace swarm \
    --log-format json --log-level warn
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Note: `--headless` disables the web UI by default on the VPS (accessible via Tailscale if needed by removing this flag). SQLite persistence means workflow state survives restarts.

#### Go SDK Dependency
**File**: `harness/go.mod` — add `go.temporal.io/sdk`.

### Changes Required

#### 1. Workflows
**File**: `harness/internal/leadfde/workflows.go` (~250 lines)

**Workflow IDs contain the agent index** for observability and deduplication: `swarm-{agentIdx}-{ticketID}` (e.g., `swarm-0-CM-123`, `swarm-1-CM-456`). The agent index is assigned at spawn time from the pool of available slots (0 through maxSessions-1).

```go
// CodeChangeWorkflow — full lifecycle with durable retry loops
func CodeChangeWorkflow(ctx workflow.Context, params CodeChangeParams) error {
    // Workflow ID: swarm-{agentIdx}-{ticketID}
    generalCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        TaskQueue:           "swarm-general",
        StartToCloseTimeout: 2 * time.Hour,
        HeartbeatTimeout:    2 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
    })
    verifyCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        TaskQueue:           "swarm-verify",   // concurrency 1
        StartToCloseTimeout: 30 * time.Minute,
        HeartbeatTimeout:    2 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
    })

    // 1. Research (general queue)
    err := workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
        params.TicketID, "/swarm-research").Get(ctx, nil)
    if err != nil { return err }

    // 2. Plan (general queue)
    err = workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
        params.TicketID, "/swarm-code-change --phase plan").Get(ctx, nil)
    if err != nil { return err }

    // 3. Plan review loop (max 3 revisions — durable across restarts)
    for i := 0; i < 3; i++ {
        var verdict string
        err = workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
            params.TicketID, "/swarm-plan-review").Get(ctx, &verdict)
        if err != nil { return err }
        if verdict == "APPROVE" { break }
        err = workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
            params.TicketID, "/swarm-code-change --phase revise").Get(ctx, nil)
        if err != nil { return err }
    }

    // 4. Implement + verify loop (verify on dedicated queue — concurrency 1)
    for i := 0; i < 3; i++ {
        err = workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
            params.TicketID, "/swarm-code-change --phase implement").Get(ctx, nil)
        if err != nil { return err }
        var pass bool
        err = workflow.ExecuteActivity(verifyCtx, RunVerification,
            params.TicketID).Get(ctx, &pass)
        if err != nil { return err }
        if pass { break }
    }

    // 5. PR (general queue)
    return workflow.ExecuteActivity(generalCtx, SpawnClaudeSession,
        params.TicketID, "/swarm-code-change --phase pr").Get(ctx, nil)
}

// ResearchWorkflow — research only
func ResearchWorkflow(ctx workflow.Context, params ResearchParams) error { ... }

// ProjectWorkflow — decomposition, spawns child CodeChangeWorkflows
func ProjectWorkflow(ctx workflow.Context, params ProjectParams) error { ... }

// HeartbeatWorkflow — scheduled every 2 min, replaces Go ticker
func HeartbeatWorkflow(ctx workflow.Context) error {
    ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        TaskQueue:           "swarm-general",
        StartToCloseTimeout: 2 * time.Minute,
    })
    // Reap dead sessions
    _ = workflow.ExecuteActivity(ctx, ReapDeadSessions).Get(ctx, nil)
    // Kill stuck sessions (>2h)
    _ = workflow.ExecuteActivity(ctx, KillStuckSessions, 2*time.Hour).Get(ctx, nil)
    // Comment on stalled tickets (>4h since last comment)
    _ = workflow.ExecuteActivity(ctx, CommentOnStalledTickets).Get(ctx, nil)
    // Spawn ready tickets (Todo with swarm labels → CodeChangeWorkflow)
    return workflow.ExecuteActivity(ctx, SpawnReadyTickets).Get(ctx, nil)
}
```

**Workflow ID convention**: `swarm-{agentIdx}-{ticketID}` ensures:
- Each ticket has at most one active workflow (Temporal deduplication by workflow ID)
- The agent index (0-3) is visible in the Temporal UI for capacity tracking
- Tmux sessions use the same naming: `cm-swarm-{agentIdx}-{ticketID}`

#### 2. Activities
**File**: `harness/internal/leadfde/activities.go` (~200 lines)

```go
type Activities struct {
    repoRoot string
    logger   *slog.Logger
}

// SpawnClaudeSession — spawns tmux session, polls for completion with heartbeat
func (a *Activities) SpawnClaudeSession(ctx context.Context, ticketID, prompt string) (string, error) {
    // Resume from previous attempt if worker restarted
    var sessionName string
    if activity.HasHeartbeatDetails(ctx) {
        activity.GetHeartbeatDetails(ctx, &sessionName)
    }
    if sessionName == "" {
        // Assign agent index from workflow ID: "swarm-{idx}-{ticket}"
        sessionName = fmt.Sprintf("cm-%s", workflow.GetInfo(ctx).WorkflowExecution.ID)
        // ... create tmux session, write prompt to file, send-keys
    }
    // Poll loop
    for {
        if !tmuxHasSession(sessionName) {
            return readLastComment(ticketID), nil // session finished
        }
        activity.RecordHeartbeat(ctx, sessionName)
        select {
        case <-ctx.Done(): return "", ctx.Err()
        case <-time.After(10 * time.Second):
        }
    }
}

// RunVerification — runs on swarm-verify queue (concurrency 1)
func (a *Activities) RunVerification(ctx context.Context, ticketID string) (bool, error) {
    // Run `just check` in a tmux session, poll for completion
    // Parse exit code to determine PASS/FAIL
    // Post VERIFY: comment on ticket
    ...
}

// ReapDeadSessions, KillStuckSessions, CommentOnStalledTickets, SpawnReadyTickets
// — same logic as the Go ticker, but now Temporal activities with structured error handling
```

#### 3. Worker Setup
**File**: `harness/internal/leadfde/worker.go` (~100 lines)

```go
func SetupWorkers(temporalClient client.Client, activities *Activities) (worker.Worker, worker.Worker) {
    // General queue: research, plan, implement, PR — up to 3 concurrent
    generalWorker := worker.New(temporalClient, "swarm-general", worker.Options{
        MaxConcurrentActivityExecutionSize: 3,
    })
    generalWorker.RegisterWorkflow(CodeChangeWorkflow)
    generalWorker.RegisterWorkflow(ResearchWorkflow)
    generalWorker.RegisterWorkflow(ProjectWorkflow)
    generalWorker.RegisterWorkflow(HeartbeatWorkflow)
    generalWorker.RegisterActivity(activities)

    // Verify queue: only 1 verification at a time (OOM prevention)
    verifyWorker := worker.New(temporalClient, "swarm-verify", worker.Options{
        MaxConcurrentActivityExecutionSize: 1,
    })
    verifyWorker.RegisterActivity(activities)

    return generalWorker, verifyWorker
}
```

#### 4. API Endpoints
**File**: `harness/internal/server/leadfde_api.go` (~150 lines)

Auth: `X-LeadFDE-Secret` header (same pattern as `presidentAuthMiddleware` at `president_api.go:19-36`).

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/api/lead-fde/health` | GET | Running workflows + capacity | `{status, workflows: [{id, ticket_id, state, started_at}], capacity: {used, max}}` |
| `/api/lead-fde/spawn` | POST | Start a CodeChangeWorkflow | 202 `{status: "spawned", workflow_id}` or 429 `{status: "at_capacity"}` |
| `/api/lead-fde/workflow/:id` | GET | Workflow detail | `{id, ticket_id, state, current_activity, history}` |
| `/api/lead-fde/cancel/:id` | POST | Cancel workflow | `{status: "cancelled"}` |

Health endpoint queries Temporal visibility API (`client.ListWorkflow`) for running `swarm-*` workflows. No in-memory state needed — Temporal is the source of truth.

#### 5. Wiring
**File**: `harness/main.go` — Add `initTemporalClient()` and `initLeadFDEWorkers()`. Env var guard: `LEAD_FDE_SECRET` + `TEMPORAL_ADDRESS` (default `localhost:7233`). Create Temporal schedule for HeartbeatWorkflow (every 2 min). Workers start non-blocking via `worker.Start()`, stop on shutdown.

**File**: `harness/internal/server/server.go` — Add `TemporalClient client.Client` field to Server struct. Register `/api/lead-fde` route group with auth middleware.

#### 6. Infrastructure Files
**File**: `flake.nix` — Add `temporal-cli` to packages.
**File**: `scripts/vps-bootstrap.sh` — Add step for Temporal systemd service + SQLite DB path.
**File**: `scripts/harness-run.sh` — Add `TEMPORAL_ADDRESS` default export.

#### 7. Environment Variables

| Variable | Purpose |
|----------|---------|
| `LEAD_FDE_SECRET` | Auth for `/api/lead-fde/*` endpoints |
| `TEMPORAL_ADDRESS` | Temporal server address (default `localhost:7233`) |

### Success Criteria

#### Automated Verification:
- [ ] `just check` passes (Go compilation with `go.temporal.io/sdk`)
- [ ] `ls harness/internal/leadfde/` shows workflows.go, activities.go, worker.go
- [ ] `ls harness/internal/server/leadfde_api.go` exists
- [ ] `temporal-cli` available in Nix profile

#### Manual Verification:
- [ ] Temporal server starts via systemd, web UI accessible on :8233
- [ ] Workers connect on harness boot (log message)
- [ ] HeartbeatWorkflow runs every 2 min (visible in Temporal UI)
- [ ] `curl POST /api/lead-fde/spawn` starts a CodeChangeWorkflow (visible in Temporal UI)
- [ ] Workflow ID format: `swarm-{agentIdx}-{ticketID}`
- [ ] 4th spawn returns 429 (all agent slots full)
- [ ] Verification activities queue behind each other (only 1 runs at a time)
- [ ] Kill harness mid-workflow → restart → workflow resumes at correct activity
- [ ] Stalled tickets get HEARTBEAT comments within 2 ticks

---

## Phase 5: Integration Testing & CLAUDE.md

### Overview
End-to-end testing and documentation.

### Changes Required

#### 1. CLAUDE.md Update
**File**: `CLAUDE.md`

Add "## Agent Swarm System" section:
- Primitives table (all 10 skills)
- Recommended sequences (quick research, scoped change, large project)
- Human gates (classification, project kickoff, PR merge)
- Dry-run documentation
- Lead FDE API reference
- Temporal architecture (server, workers, task queues, workflow IDs)

#### 2. Error Handling Conventions
Each primitive includes standardized error handling:
- linear-cli exit code 3 → "Auth error. Run `linear-cli config doctor`"
- linear-cli exit code 4 → wait 60s, retry once
- Mid-execution failure → comment on ticket, keep In Progress
- Crash recovery → `/swarm-resume` reads comment history

### Success Criteria

#### Automated Verification:
- [ ] All 9 primitive SKILL.md + 1 conventions SKILL.md + 3 templates = 13 skill files exist
- [ ] CLAUDE.md contains "Agent Swarm System" section
- [ ] `just check` passes

#### Manual Verification:
- [ ] `/swarm-setup --dry-run` prints labels without creating
- [ ] `/swarm-research "session auth"` → ticket + doc + comments
- [ ] `/swarm-code-change "add /health-detailed endpoint"` → full lifecycle to PR
- [ ] `/swarm-resume CM-XXX` on completed ticket → reconstructs state
- [ ] `/swarm-project "improve error handling"` → project + hierarchy
- [ ] HeartbeatWorkflow auto-detects stalls and posts HEARTBEAT comments (verify in Temporal UI + Linear)
- [ ] Full restart: reject PR → new ticket references old → fresh cycle

---

## Testing Strategy

### Dry-Run Testing (no Linear side effects):
1. `/swarm-setup --dry-run` → prints labels it would create
2. `/swarm-research "auth system" --dry-run` → prints ticket creation, comment, doc path
3. `/swarm-code-change "add /version endpoint" --dry-run` → prints full lifecycle actions

### Live Integration Testing:
1. Run `/swarm-setup` → verify labels in Linear UI
2. Run `/swarm-research "how does session auth work"` → verify ticket + doc + comments
3. Run `/swarm-code-change "add a /health-detailed endpoint"` → verify full lifecycle
4. Run `/swarm-resume CM-XXX` → verify state reconstruction from comments
5. Verify HeartbeatWorkflow detects stalled tickets (Temporal UI + Linear comments)

### Lead FDE + Temporal Testing:
1. `curl GET /api/lead-fde/health` → verify response (queries Temporal visibility)
2. `curl POST /api/lead-fde/spawn` → verify CodeChangeWorkflow starts (visible in Temporal UI)
3. Spawn 4 workflows, attempt 5th → verify 429 (all agent slots full)
4. Cancel a workflow → verify tmux session cleaned up
5. Kill harness mid-workflow → restart → verify workflow resumes at correct activity
6. Run 2 workflows that both reach verification → verify only 1 runs `just check` at a time (swarm-verify queue)

### End-to-End:
1. `/swarm-project "add rate limiting"` → creates project
2. Verify ticket hierarchy in Linear
3. Move a workstream ticket to Todo → HeartbeatWorkflow auto-starts CodeChangeWorkflow
4. Watch Temporal UI for workflow progression + Linear for structured comments
5. Verify PR created
6. `/swarm-status` shows active workflows + ticket progress

## Performance Considerations

- **linear-cli calls**: ~1500 req/hr budget. Project decomposition (20 tickets + 15 relations + 20 comments = ~55 requests) well within limits. HeartbeatWorkflow: ~3-5 calls per tick at 2-min interval = ~90-150 req/hr.
- **WASM builds**: ~5 GB RAM each. The `swarm-verify` queue (concurrency 1) ensures only one verification/build runs at a time — prevents OOM.
- **Claude Code sessions**: Each uses meaningful RAM. `swarm-general` queue (concurrency 3) + `swarm-verify` queue (concurrency 1) = max 4 concurrent, conservative for 10 GB VPS.
- **Temporal server**: ~200 MB RAM with SQLite backend at idle. Negligible CPU for <10 concurrent workflows.
- **Resource budget**: Temporal (~200 MB) + harness + workers (~250 MB) + up to 3 Claude sessions (~1.5 GB) + 1 verification (~1-5 GB) = ~3-7 GB peak. Fits in 10 GB with headroom.
- **Zero LLM API cost** for orchestration. All Anthropic API spend goes to the Claude Code worker sessions.

---

## File Inventory

### New Files (17)

| File | Phase | ~Lines |
|------|-------|--------|
| `.claude/skills/swarm-conventions/SKILL.md` | 1 | 150 |
| `.claude/skills/swarm-conventions/templates/ticket-description.md` | 1 | 30 |
| `.claude/skills/swarm-conventions/templates/research-doc.md` | 1 | 25 |
| `.claude/skills/swarm-conventions/templates/plan-doc.md` | 1 | 30 |
| `.claude/skills/swarm-setup/SKILL.md` | 1 | 60 |
| `.claude/skills/swarm-research/SKILL.md` | 2 | 120 |
| `.claude/skills/swarm-code-change/SKILL.md` | 2 | 150 |
| `.claude/skills/swarm-project/SKILL.md` | 2 | 150 |
| `.claude/skills/swarm-verify/SKILL.md` | 3 | 100 |
| `.claude/skills/swarm-plan-review/SKILL.md` | 3 | 120 |
| `.claude/skills/swarm-project-verify/SKILL.md` | 3 | 100 |
| `.claude/skills/swarm-status/SKILL.md` | 3 | 80 |
| `.claude/skills/swarm-resume/SKILL.md` | 3 | 120 |
| `harness/internal/leadfde/workflows.go` | 4 | 250 |
| `harness/internal/leadfde/activities.go` | 4 | 200 |
| `harness/internal/leadfde/worker.go` | 4 | 100 |
| `harness/internal/server/leadfde_api.go` | 4 | 150 |

### Modified Files (6)

| File | Phase | Change |
|------|-------|--------|
| `CLAUDE.md` | 5 | Add Agent Swarm System section |
| `harness/main.go` | 4 | Wire Temporal client + workers + heartbeat schedule |
| `harness/internal/server/server.go` | 4 | Add TemporalClient field + routes |
| `harness/go.mod` | 4 | Add `go.temporal.io/sdk` |
| `flake.nix` | 4 | Add `temporal-cli` to packages |
| `scripts/vps-bootstrap.sh` | 4 | Add Temporal systemd service step |

## References

- v1 plan: `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md`
- v1 review: `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md`
- Chestnut flowchart: `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html`
- Linear CLI research: `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md`
- President agent (reference, NOT used for Lead FDE): `harness/internal/president/`
- Mayor agent (reference): `harness/internal/mayor/`
- Session management: `harness/internal/tmux/session.go`
- Claude orchestrator: `harness/internal/claude/claude.go`
- Nix flake (VPS dependencies): `flake.nix`
- VPS bootstrap (systemd, Nix install): `scripts/vps-bootstrap.sh`
- Harness run wrapper (PATH setup): `scripts/harness-run.sh`
- Temporal Go SDK: `go.temporal.io/sdk` v1.40+
- Temporal server (dev mode with SQLite): `temporal server start-dev --db-filename`
- OpenClaw heartbeat (evaluated, rejected for Lead FDE): `context/openclaw/src/infra/heartbeat-runner.ts`
- Go ticker (evaluated, superseded by Temporal): simpler but lacks queue management, durable state, observability
