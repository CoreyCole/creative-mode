# Agent Swarm Primitives v2 — Implementation Plan

## Context

Revision of `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` based on staff engineering review (`thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md`). The v1 plan had one critical issue (Project Orchestrator underspecified), six concerns, and four questions. This v2 addresses all of them.

### Key Changes from v1

1. **Flat primitives** — Each primitive is its own skill (`/swarm-research`, `/swarm-code-change`, etc.) instead of one `/swarm` router. Eliminates context window pressure from nested loading.
2. **Agent-only plan review** — No `AskUserQuestion` in autonomous sessions. Three human gates only: classification, project kickoff, PR merge.
3. **Dry-run from Phase 1** — `--dry-run` convention; primitives print actions without executing.
4. **Resume primitive** — Reads Linear comment history to reconstruct state and continue.
5. **Project Orchestrator fully specified** — Session naming, spawning via harness API, Linear comments as reporting, max 4 concurrent sessions, health check endpoint.
6. **Label idempotency** — Check-then-create pattern (list first, create only missing).

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

Each SKILL.md is self-contained. No primitive loads another. SKILL.md documents "Composition hints" — which primitive to run next — but the calling agent or human chains them.

---

## Phase 1: Foundation (Conventions, Setup, Templates)

### Files

**`.claude/skills/swarm-conventions/SKILL.md`** (~150 lines)
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
- **Ticket footer**: Structured YAML block with `swarm_type`, `parent_ticket`, `research_path`, `plan_path`, `pr_url`, `previous_attempt`, `dependencies`
- **Comment prefixes** (parseable by resume): `RESEARCH:`, `PLAN:`, `PLAN-REVIEW:`, `IMPL:`, `VERIFY:`, `PR:`, `REVISION:`, `RESTART:`, `HEARTBEAT:`, `RESUME:`
- **Lifecycle states**: Triage → Backlog → Todo → In Progress → In Review → Done
- **Doc paths**: `thoughts/{git_user}/research/{timestamp}_{slug}.md`, `thoughts/{git_user}/plans/{timestamp}_{slug}.md`
- **Dry-run convention**: All primitives accept `--dry-run`. Print `[DRY-RUN]` prefix per action without executing. linear-cli has native `--dry-run`.
- **Rate limits**: 1500 req/hr. Batch ticket creation sequentially (linear-cli handles 429 retry).
- **Error handling**: exit 3 → stop, run `linear-cli config doctor`. exit 4 → wait 60s, retry once. Mid-execution → comment on ticket, keep In Progress.

**`.claude/skills/swarm-conventions/templates/ticket-description.md`** (~30 lines)
Structured footer template with swarm metadata fields.

**`.claude/skills/swarm-conventions/templates/research-doc.md`** (~25 lines)
YAML frontmatter: `linear_ticket`, `date`, `author`, `topic`. Sections: Summary, Key Findings, Open Questions, Files Referenced, Next Steps.

**`.claude/skills/swarm-conventions/templates/plan-doc.md`** (~30 lines)
YAML frontmatter: `linear_ticket`, `linear_project`, `date`, `author`. Sections: Goal, Success Criteria, Implementation Phases, File Inventory, Dependencies.

**`.claude/skills/swarm-setup/SKILL.md`** (~60 lines)
```yaml
---
name: swarm-setup
description: One-time setup for agent swarm — creates Linear labels, verifies CLI auth. Run once before using other swarm primitives.
allowed-tools: Bash
---
```

Steps:
1. `linear-cli config doctor` — verify auth
2. For each label, check-then-create: `linear-cli l list --output json --compact | jq` to check existence, then `linear-cli l create "name" --color "#hex"` only if missing
3. `linear-cli st list -t CM --output json` — verify workflow states
4. Report summary
5. Supports `--dry-run`

### Success Criteria
- [ ] All files exist with valid YAML frontmatter
- [ ] `/swarm-setup` creates labels idempotently (second run = no-op)
- [ ] `--dry-run` prints actions without executing

---

## Phase 2: Core Primitives (Research, Code-Change, Project)

### `.claude/skills/swarm-research/SKILL.md` (~120 lines)

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

### `.claude/skills/swarm-code-change/SKILL.md` (~150 lines)

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
3. **Research**: Create child research ticket, set parent relation, run research inline (Agent subagent). Comment `RESEARCH: <summary>`.
4. **Plan**: Use `/create_plan` with ticket + research context. Comment `PLAN: Created at <path>`.
5. **Plan review (agent-only)**: Spawn Agent subagent to review plan (file existence, patterns, edge cases). Verdict: APPROVE/REVISE. Comment `PLAN-REVIEW:`. Max 3 revisions.
6. **Implement**: Use `/implement_plan`. Comment `IMPL: Starting`.
7. **Verify**: Run automated checks from plan criteria + `just check`. Comment `VERIFY: PASS/FAIL`. If FAIL, loop to implement (max 3 attempts).
8. **Ship**: Branch, commit, push, PR via `linear-cli g pr $TICKET`. Comment `PR: <url>`. Status → In Review.

**Human gates**: Classification (step 1) and PR merge (step 8). No `AskUserQuestion` between.

### `.claude/skills/swarm-project/SKILL.md` (~150 lines)

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
5. Create tickets: parent per workstream, children per task, blocking relations
6. Write plan: `thoughts/{user}/plans/{timestamp}_{slug}.md` with dependency graph
7. Plan review: Agent subagent reviews. Comment `PLAN-REVIEW:`.
8. **Human gate (project kickoff)**: Present plan summary, ask approval
9. Update: `linear-cli p update $PROJECT --status started`
10. Document which workstreams can start (no blockers) vs blocked

### Success Criteria
- [ ] `/swarm-research "how does auth work"` → ticket + doc + comments
- [ ] `/swarm-code-change "add /version endpoint"` → full lifecycle
- [ ] `/swarm-project "build notification system"` → project + ticket hierarchy
- [ ] `--dry-run` works on all three
- [ ] No primitive loads another primitive's SKILL.md

---

## Phase 3: Support Primitives

### `.claude/skills/swarm-verify/SKILL.md` (~100 lines)
Read plan → extract criteria → run checks → comment results → return PASS/FAIL.

### `.claude/skills/swarm-plan-review/SKILL.md` (~120 lines)
Read plan → evaluate (completeness, feasibility, file refs exist, gaps) → verdict APPROVE/REVISE → comment. "Run in Agent subagent for isolation."

### `.claude/skills/swarm-project-verify/SKILL.md` (~100 lines)
List project issues → categorize by workstream/status → identify blockers (>24h stale) → comment summary → recommend continue/reprioritize/escalate.

### `.claude/skills/swarm-heartbeat/SKILL.md` (~120 lines)
Query in-progress issues + active projects → identify stalls (>4h) → comment `HEARTBEAT:` → check dependency deadlocks → report. When run by Lead FDE, also checks tmux sessions via health API.

### `.claude/skills/swarm-status/SKILL.md` (~80 lines)
Dashboard: active projects table, awaiting review, in-progress with last update, recently completed. Read-only.

### `.claude/skills/swarm-resume/SKILL.md` (~120 lines)
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
4. Read referenced docs (research, plan) from comment paths
5. Comment `RESUME: Resuming from <phase>. Context: <summary>`
6. Continue from appropriate phase based on label type
7. Example: `swarm:code-change` + last `VERIFY: FAIL` → resume at implementation

### Success Criteria
- [ ] All 10 primitive SKILL.md files exist
- [ ] `/swarm-resume CM-XXX` correctly identifies last phase and continues
- [ ] `/swarm-heartbeat` detects stalled tickets

---

## Phase 4: Lead FDE (OpenClaw Agent + Harness API)

### 4.1 Lead FDE Manager

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
- `Provision()` — idempotent, writes workspace files, registers via CLI, binds Discord
- `SpawnSession(ticketID, prompt) (string, error)` — creates tmux `cm-swarm-{ticketID}`, enforces max 4
- `GetActiveSessions() []SwarmSession`
- `ReapSessions()` — removes entries for dead tmux sessions (5min ticker)
- `KillSession(name) error`

**Session naming**: `cm-swarm-{ticketID}` (e.g., `cm-swarm-CM-123`). Distinct from `cm-{worldID}-{cpID}` world builds.

**Spawn**: writes prompt to temp file, creates tmux session at repo root, sends `claude --dangerously-skip-permissions --input-file <path>`. Returns 202 or error if at capacity.

### 4.2 Workspace Files

**File**: `harness/internal/leadfde/workspace.go` (~200 lines)

Created at `{OPENCLAW_HOME}/workspaces/lead-fde/`:
- **SOUL.md** — Identity, operating principles, safety rules (autonomous: comments/research/status/spawn; approval: new projects; forbidden: merge PRs/delete projects)
- **AGENTS.md** — Heartbeat mode + reactive mode
- **HEARTBEAT.md** — The critical spec:
  1. Check active sessions via `curl GET /api/lead-fde/health`
  2. Kill stuck sessions (>2h)
  3. Query Linear for in-progress + todo tickets
  4. Comment `HEARTBEAT:` on stalled tickets (>4h)
  5. Spawn orchestrators for ready todo tickets via `curl POST /api/lead-fde/spawn`
  6. If at capacity (429), note queued tickets in MEMORY.md
  7. Check project progress, escalate if all workstreams blocked
  8. Update MEMORY.md
- **IDENTITY.md** — Name, role, communication style
- **USER.md** — Who interacts with the Lead FDE
- **MEMORY.md** — Starts empty, updated each heartbeat

**How orchestrators report back**: Spawned sessions run `/swarm-resume CM-XXX`. They write structured comments (`RESEARCH:`, `PLAN:`, `IMPL:`, `VERIFY:`, `PR:`) on the Linear ticket. Lead FDE reads these via `linear-cli cm list CM-XXX --output json` on next heartbeat. Linear comments ARE the reporting mechanism.

### 4.3 Lead FDE Skills

**File**: `harness/internal/leadfde/skills.go` (~80 lines)

OpenClaw skills pointing to harness API:
- `swarm-health/SKILL.md` — `curl GET /api/lead-fde/health`
- `swarm-spawn/SKILL.md` — `curl POST /api/lead-fde/spawn`

### 4.4 API Endpoints

**File**: `harness/internal/server/leadfde_api.go` (~200 lines)

Auth: `X-LeadFDE-Secret` header (same pattern as `presidentAuthMiddleware`).

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/api/lead-fde/health` | GET | Sessions + capacity | `{status, active_sessions, capacity}` |
| `/api/lead-fde/spawn` | POST | Spawn session | 202 `{session}` or 429 `{at_capacity}` |
| `/api/lead-fde/session/:name` | GET | Session detail | `{name, ticket, alive}` |
| `/api/lead-fde/kill/:name` | POST | Kill session | `{status: "killed"}` |

### 4.5 Wiring

**File**: `harness/main.go` — Add `initLeadFDEManager()` (same pattern as `initPresidentManager` at lines 391-422). Env var guard: `LEAD_FDE_DISCORD_CHANNEL_ID` + `LEAD_FDE_SECRET`.

**File**: `harness/internal/server/server.go` — Add `LeadFDEManager` field to Server struct, register `/api/lead-fde` route group.

### 4.6 Environment Variables

| Variable | Purpose |
|----------|---------|
| `LEAD_FDE_DISCORD_CHANNEL_ID` | Discord channel for reports |
| `LEAD_FDE_SECRET` | Auth for API endpoints |

### Success Criteria
- [ ] `just check` passes
- [ ] Lead FDE provisions on startup (idempotent)
- [ ] `GET /api/lead-fde/health` returns session list
- [ ] `POST /api/lead-fde/spawn` creates tmux session
- [ ] 5th spawn returns 429
- [ ] Reaper cleans dead sessions

---

## Phase 5: Integration Testing & CLAUDE.md

### CLAUDE.md Update
Add "## Agent Swarm System" section with primitives table, recommended sequences, human gates, dry-run docs, Lead FDE API reference.

### Testing

| Test | How |
|------|-----|
| Dry-run all primitives | `--dry-run` flag, verify no Linear side effects |
| Label idempotency | Run setup twice |
| Research flow | `/swarm-research` end-to-end |
| Code-change flow | `/swarm-code-change` end-to-end |
| Resume flow | `/swarm-resume` on completed ticket |
| API endpoints | `curl` to all Lead FDE routes |
| Session limits | Spawn 5, verify 429 on 5th |
| Go compilation | `just check` |
| End-to-end | `/swarm-project` → spawned sessions → PRs |

---

## File Inventory

### New Files (18)

| File | Phase | Lines |
|------|-------|-------|
| `.claude/skills/swarm-conventions/SKILL.md` | 1 | ~150 |
| `.claude/skills/swarm-conventions/templates/ticket-description.md` | 1 | ~30 |
| `.claude/skills/swarm-conventions/templates/research-doc.md` | 1 | ~25 |
| `.claude/skills/swarm-conventions/templates/plan-doc.md` | 1 | ~30 |
| `.claude/skills/swarm-setup/SKILL.md` | 1 | ~60 |
| `.claude/skills/swarm-research/SKILL.md` | 2 | ~120 |
| `.claude/skills/swarm-code-change/SKILL.md` | 2 | ~150 |
| `.claude/skills/swarm-project/SKILL.md` | 2 | ~150 |
| `.claude/skills/swarm-verify/SKILL.md` | 3 | ~100 |
| `.claude/skills/swarm-plan-review/SKILL.md` | 3 | ~120 |
| `.claude/skills/swarm-project-verify/SKILL.md` | 3 | ~100 |
| `.claude/skills/swarm-heartbeat/SKILL.md` | 3 | ~120 |
| `.claude/skills/swarm-status/SKILL.md` | 3 | ~80 |
| `.claude/skills/swarm-resume/SKILL.md` | 3 | ~120 |
| `harness/internal/leadfde/leadfde.go` | 4 | ~250 |
| `harness/internal/leadfde/workspace.go` | 4 | ~200 |
| `harness/internal/leadfde/skills.go` | 4 | ~80 |
| `harness/internal/server/leadfde_api.go` | 4 | ~200 |

### Modified Files (3)

| File | Phase | Change |
|------|-------|--------|
| `CLAUDE.md` | 5 | Add Agent Swarm System section |
| `harness/main.go` | 4 | Wire Lead FDE init + reaper |
| `harness/internal/server/server.go` | 4 | Add LeadFDEManager + routes |

---

## Critical Reference Files

- `harness/internal/president/president.go` — Manager pattern
- `harness/internal/server/president_api.go` — API endpoint pattern
- `harness/internal/tmux/session.go` — Session creation pattern
- `.claude/skills/playwright-cli/SKILL.md` — Skill frontmatter format
- `harness/main.go` — Initialization wiring pattern
