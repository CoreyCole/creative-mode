# Agent Swarm Primitives v2 — Implementation Plan

## Overview

Build a general-purpose agent swarm system through 11 flat, composable Claude Code skills and a Lead FDE orchestration agent. Revision of the v1 plan based on staff engineering review — addresses the critical Project Orchestrator underspecification, context window pressure from nested loading, and five other concerns.

## Current State Analysis

### What Exists
- **38 linear-cli skills** installed (`.agents/skills/linear-*/SKILL.md`) — full Linear CRUD, search, relations, projects, sprints
- **Workflow commands** working: `/create_plan`, `/implement_plan`, `/validate_plan`, `/research_codebase`, `/create_handoff`, `/resume_handoff`, `/describe_pr`, `/create_spec`, `/tkt`
- **OpenClaw president agent** pattern (`harness/internal/president/`) — heartbeat loop, Discord binding, workspace files, skills
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
- OpenClaw heartbeat (`context/openclaw/src/infra/heartbeat-runner.ts`) — timer-based, reads HEARTBEAT.md, gives agent full turn with exec/read/write tools, prunes on HEARTBEAT_OK

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
4. **Lead FDE** runs heartbeat loops, spawning orchestrator sessions via harness API, detecting stalls, escalating blockers
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
- Persistent agent processes (OpenClaw is per-turn, Claude Code sessions are ephemeral)
- Auto-merging PRs (human always reviews and merges)
- Slack integration (Discord-only; Slack acknowledged as future extension)
- Custom UI for swarm management (Linear UI + CLI is sufficient)
- Nested primitive loading (flat primitives only, composition hints in docs)

## Implementation Approach

**Flat skills**: 11 independent Claude Code skills (`/swarm-research`, `/swarm-code-change`, etc.) instead of one `/swarm` router. Each SKILL.md is self-contained (~100-150 lines). No primitive loads another. Composition hints document recommended sequences.

**Agent-only plan review**: Plan-review primitive runs in a separate Agent subagent for objectivity. No `AskUserQuestion` in autonomous sessions. Three human gates: classification, project kickoff, PR merge.

**Dry-run from Phase 1**: Every primitive respects `--dry-run`. linear-cli has native support. File writes print path + first 10 lines.

**Linear comments as state**: Every phase transition writes a structured comment (`RESEARCH:`, `PLAN:`, `IMPL:`, `VERIFY:`, `PR:`). The resume primitive parses these to reconstruct state. The Lead FDE reads them to track progress.

**Lead FDE spawns via harness API**: `POST /api/lead-fde/spawn` creates tmux sessions `cm-swarm-{ticketID}`. Max 4 concurrent. Health check endpoint. Orchestrators report via Linear comments.

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
  swarm-heartbeat/SKILL.md                # Orchestration heartbeat (~120 lines)
  swarm-status/SKILL.md                   # Status dashboard (~80 lines)
  swarm-resume/SKILL.md                   # Resume from ticket history (~120 lines)
```

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

## Phase 3: Support Primitives (Verify, Plan-Review, Heartbeat, Status, Resume)

### Overview
Create verification, review, orchestration, and state recovery primitives.

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

#### 4. Orchestration Heartbeat
**File**: `.claude/skills/swarm-heartbeat/SKILL.md` (~120 lines)
Query in-progress issues + active projects → identify stalls (>4h) → comment `HEARTBEAT:` → check dependency deadlocks → report. When run by Lead FDE, also checks tmux sessions via health API.

#### 5. Status Dashboard
**File**: `.claude/skills/swarm-status/SKILL.md` (~80 lines)
Dashboard: active projects table, awaiting review, in-progress with last update, recently completed. Read-only, no side effects.

#### 6. Resume from Ticket History
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
- [ ] All 10 primitive SKILL.md files exist under `.claude/skills/swarm-*/`
- [ ] Each has valid YAML frontmatter with `name`, `description`, `allowed-tools`

#### Manual Verification:
- [ ] `/swarm-verify CM-XXX` runs checks and reports PASS/FAIL
- [ ] `/swarm-plan-review` produces structured review in Agent subagent
- [ ] `/swarm-heartbeat` queries Linear and identifies stalled tickets
- [ ] `/swarm-status` displays formatted dashboard
- [ ] `/swarm-resume CM-XXX` correctly identifies last phase and continues

---

## Phase 4: Lead FDE (OpenClaw Agent + Harness API)

### Overview
Create the Lead FDE as an OpenClaw agent with session tracking and a harness API. This is the most detailed phase, addressing the v1 review's critical issue.

### Changes Required

#### 1. Lead FDE Manager
**File**: `harness/internal/leadfde/leadfde.go` (~250 lines)

Follows president pattern (`harness/internal/president/president.go`) but adds session tracking:

```go
type Manager struct {
    openclawHome, openclawBin, harnessURL, secret, channelID string
    db     *db.DB
    logger *slog.Logger
    mu             sync.Mutex
    activeSessions map[string]*SwarmSession
    maxSessions    int // 4
}

type SwarmSession struct {
    Name, TicketID, Prompt string
    StartedAt              time.Time
}
```

Key methods:
- `Provision()` — idempotent (checks SOUL.md exists), writes workspace files, registers via CLI, binds Discord
- `SpawnSession(ticketID, prompt) (string, error)` — creates tmux `cm-swarm-{ticketID}`, enforces max 4 concurrent
- `GetActiveSessions() []SwarmSession` — snapshot of active sessions
- `ReapSessions()` — scans `tmux list-sessions` for `cm-swarm-*`, removes dead entries (5min ticker)
- `KillSession(name) error` — kills tmux session and removes from tracking

**Session naming**: `cm-swarm-{ticketID}` (e.g., `cm-swarm-CM-123`). Distinct from `cm-{worldID}-{cpID}` world builds.

**Spawn flow**: Lock mutex → reap dead sessions → check count < maxSessions → write prompt to temp file → `tmux new-session -d -s cm-swarm-{ticketID} -c {repoRoot}` → `tmux send-keys "claude --dangerously-skip-permissions --input-file <path>" Enter` → track in activeSessions → return 202.

#### 2. Workspace Files
**File**: `harness/internal/leadfde/workspace.go` (~200 lines)

Created at `{OPENCLAW_HOME}/workspaces/lead-fde/`:

**SOUL.md** — Identity: Lead FDE, orchestration brain. Safety: autonomous for comments/research/status/spawn; approval for new projects; forbidden: merge PRs/delete projects.

**AGENTS.md** — Heartbeat mode (periodic) + reactive mode (Discord messages).

**HEARTBEAT.md** — The critical specification:
1. Check active sessions: `curl GET /api/lead-fde/health -H "X-LeadFDE-Secret: {SECRET}"`
2. Kill stuck sessions (>2h): `curl POST /api/lead-fde/kill/cm-swarm-{TICKET}`
3. Query Linear: `linear-cli i list -t CM -s "In Progress" --output json --compact`
4. Comment `HEARTBEAT:` on stalled tickets (>4h since last comment)
5. Spawn for ready todo tickets: `curl POST /api/lead-fde/spawn -d '{"ticket_id":"CM-XXX","prompt":"/swarm-resume CM-XXX"}'`
6. If at capacity (429), note queued tickets in MEMORY.md
7. Check project progress, escalate if all workstreams blocked
8. Update MEMORY.md with observations

**How orchestrators report back**: Spawned sessions write structured comments (`RESEARCH:`, `PLAN:`, `IMPL:`, `VERIFY:`, `PR:`) on the Linear ticket. Lead FDE reads these via `linear-cli cm list CM-XXX --output json` on next heartbeat. Linear comments ARE the reporting mechanism.

**IDENTITY.md**, **USER.md**, **MEMORY.md** — Standard OpenClaw workspace files.

#### 3. Lead FDE Skills
**File**: `harness/internal/leadfde/skills.go` (~80 lines)

OpenClaw skills pointing to harness API (same pattern as `harness/internal/president/skills.go`):
- `swarm-health/SKILL.md` — `curl GET /api/lead-fde/health`
- `swarm-spawn/SKILL.md` — `curl POST /api/lead-fde/spawn`

#### 4. API Endpoints
**File**: `harness/internal/server/leadfde_api.go` (~200 lines)

Auth: `X-LeadFDE-Secret` header (same pattern as `presidentAuthMiddleware` at `president_api.go:19-36`).

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/api/lead-fde/health` | GET | Sessions + capacity | `{status, active_sessions: [{name, ticket_id, started_at, alive}], capacity: {used, max}}` |
| `/api/lead-fde/spawn` | POST | Spawn session | 202 `{status: "spawned", session, ticket_id}` or 429 `{status: "at_capacity"}` |
| `/api/lead-fde/session/:name` | GET | Session detail | `{name, ticket_id, started_at, alive}` |
| `/api/lead-fde/kill/:name` | POST | Kill session | `{status: "killed"}` |

#### 5. Wiring
**File**: `harness/main.go` — Add `initLeadFDEManager()` (same pattern as `initPresidentManager` at lines 391-422). Env var guard: `LEAD_FDE_DISCORD_CHANNEL_ID` + `LEAD_FDE_SECRET`. Add 5-min reaper ticker goroutine.

**File**: `harness/internal/server/server.go` — Add `LeadFDEManager *leadfde.Manager` field to Server struct. Register `/api/lead-fde` route group with auth middleware.

#### 6. Environment Variables

| Variable | Purpose |
|----------|---------|
| `LEAD_FDE_DISCORD_CHANNEL_ID` | Discord channel for reports |
| `LEAD_FDE_SECRET` | Auth for `/api/lead-fde/*` endpoints |

### Success Criteria

#### Automated Verification:
- [ ] `just check` passes (Go compilation)
- [ ] `ls harness/internal/leadfde/` shows leadfde.go, workspace.go, skills.go
- [ ] `ls harness/internal/server/leadfde_api.go` exists

#### Manual Verification:
- [ ] Lead FDE provisions on harness startup (idempotent)
- [ ] `curl -H "X-LeadFDE-Secret: $SECRET" localhost:8080/api/lead-fde/health` returns session list + capacity
- [ ] `curl -X POST -H "X-LeadFDE-Secret: $SECRET" -d '{"ticket_id":"CM-1","prompt":"test"}' localhost:8080/api/lead-fde/spawn` creates tmux session
- [ ] 5th spawn returns 429
- [ ] Reaper cleans dead sessions after 5 minutes
- [ ] Lead FDE heartbeat queries Linear, spawns sessions, reports via Discord

---

## Phase 5: Integration Testing & CLAUDE.md

### Overview
End-to-end testing and documentation.

### Changes Required

#### 1. CLAUDE.md Update
**File**: `CLAUDE.md`

Add "## Agent Swarm System" section:
- Primitives table (all 11 skills)
- Recommended sequences (quick research, scoped change, large project)
- Human gates (classification, project kickoff, PR merge)
- Dry-run documentation
- Lead FDE API reference

#### 2. Error Handling Conventions
Each primitive includes standardized error handling:
- linear-cli exit code 3 → "Auth error. Run `linear-cli config doctor`"
- linear-cli exit code 4 → wait 60s, retry once
- Mid-execution failure → comment on ticket, keep In Progress
- Crash recovery → `/swarm-resume` reads comment history

### Success Criteria

#### Automated Verification:
- [ ] All 10 primitive SKILL.md + 1 conventions SKILL.md + 3 templates = 14 skill files exist
- [ ] CLAUDE.md contains "Agent Swarm System" section
- [ ] `just check` passes

#### Manual Verification:
- [ ] `/swarm-setup --dry-run` prints labels without creating
- [ ] `/swarm-research "session auth"` → ticket + doc + comments
- [ ] `/swarm-code-change "add /health-detailed endpoint"` → full lifecycle to PR
- [ ] `/swarm-resume CM-XXX` on completed ticket → reconstructs state
- [ ] `/swarm-project "improve error handling"` → project + hierarchy
- [ ] `/swarm-heartbeat` → queries Linear, identifies stalls
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
5. Run `/swarm-heartbeat` → verify stall detection

### Lead FDE API Testing:
1. `curl GET /api/lead-fde/health` → verify response format
2. `curl POST /api/lead-fde/spawn` → verify session creation
3. Spawn 4 sessions, attempt 5th → verify 429
4. Kill a session → verify cleanup
5. Wait for reaper → verify dead sessions removed

### End-to-End:
1. `/swarm-project "add rate limiting"` → creates project
2. Verify ticket hierarchy in Linear
3. `POST /api/lead-fde/spawn` for one workstream
4. Watch Linear for comment updates
5. Verify PR created
6. `/swarm-heartbeat` detects in-progress session

## Performance Considerations

- linear-cli calls: ~1500 req/hr budget. Project decomposition (20 tickets + 15 relations + 20 comments = ~55 requests) is well within limits.
- WASM builds: ~5 GB RAM each. Lead FDE max 4 sessions accounts for this — but note swarm sessions don't trigger WASM builds (they're code-change sessions, not world builds).
- Claude Code sessions: Each session uses RAM for the Claude process. 4 concurrent is conservative for 10 GB VPS.
- Heartbeat: Batch Linear queries (list all issues once, not per-ticket). Single `linear-cli i list -t CM -s "In Progress" --output json` call.

---

## File Inventory

### New Files (18)

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
| `.claude/skills/swarm-heartbeat/SKILL.md` | 3 | 120 |
| `.claude/skills/swarm-status/SKILL.md` | 3 | 80 |
| `.claude/skills/swarm-resume/SKILL.md` | 3 | 120 |
| `harness/internal/leadfde/leadfde.go` | 4 | 250 |
| `harness/internal/leadfde/workspace.go` | 4 | 200 |
| `harness/internal/leadfde/skills.go` | 4 | 80 |
| `harness/internal/server/leadfde_api.go` | 4 | 200 |

### Modified Files (3)

| File | Phase | Change |
|------|-------|--------|
| `CLAUDE.md` | 5 | Add Agent Swarm System section |
| `harness/main.go` | 4 | Wire Lead FDE init + reaper |
| `harness/internal/server/server.go` | 4 | Add LeadFDEManager field + routes |

## References

- v1 plan: `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md`
- v1 review: `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md`
- Chestnut flowchart: `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html`
- Linear CLI research: `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md`
- President agent (reference impl): `harness/internal/president/`
- Mayor agent (reference impl): `harness/internal/mayor/`
- Session management: `harness/internal/tmux/session.go`
- Claude orchestrator: `harness/internal/claude/claude.go`
- OpenClaw heartbeat: `context/openclaw/src/infra/heartbeat-runner.ts`
