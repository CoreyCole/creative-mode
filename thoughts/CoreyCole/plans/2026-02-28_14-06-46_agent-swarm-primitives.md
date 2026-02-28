# Agent Swarm Primitives — Implementation Plan

## Overview

Build a general-purpose agent swarm system ("Chestnut Agent Primitives") that turns high-level ideas into deployed software through seven composable primitives and a two-level orchestration model. The system uses a single **Claude Code skill** (`.claude/skills/swarm/`) with primitives as sub-files, **linear-cli** for state tracking (Linear is the source of truth), and **OpenClaw** for orchestration heartbeats. Designed to be portable to any codebase with a CLAUDE.md.

## Current State Analysis

### What Exists:
- **38 linear-cli skills** installed (`.agents/skills/linear-*/SKILL.md`) — full Linear CRUD, search, relations, projects, sprints, milestones
- **Workflow commands** already working: `/create_plan`, `/implement_plan`, `/validate_plan`, `/research_codebase`, `/create_handoff`, `/resume_handoff`, `/describe_pr`, `/create_spec`, `/tkt`
- **OpenClaw president agent** pattern (`harness/internal/president/`) — heartbeat loop, Discord binding, workspace files, skills — serves as reference implementation for orchestration agents
- **Graphite** for PR stacking
- **Linear team** `CM` configured
- **Skill subdirectory pattern** proven by `playwright-cli` — `SKILL.md` + `references/*.md` sub-files loaded on demand

### Key Discoveries:
- linear-cli supports `--id-only` for chaining, `--output json` for machine parsing, `--quiet` for agent use, stdin piping for descriptions
- OpenClaw agents are per-turn (not persistent processes) — heartbeat is triggered by HEARTBEAT.md, not a cron job
- Skills use YAML frontmatter + markdown links to sub-files; Claude loads sub-files on demand when referenced
- linear-cli `rel parent CHILD PARENT` sets parent/child relationships; `rel add A -r blocks B` sets blocking
- Single skill entry point routes via arguments: `/swarm research`, `/swarm code-change`, `/swarm project`

### Missing:
- No swarm skill or primitives
- No Linear conventions for agent swarm workflow
- No orchestration agents beyond the existing president
- No integration between existing commands and Linear ticket tracking
- No project decomposition or dependency graph tooling

## Desired End State

A fully operational agent swarm where:

1. **Any idea** can be given to `/swarm` and it gets classified, decomposed, tracked, implemented, and verified — with Linear as the source of truth throughout
2. **Projects** decompose into parent tickets -> child tickets with dependency graphs, parallel/sequential analysis, and Graphite stack planning
3. **Code changes** follow a full lifecycle: ticket -> research -> plan revision loop -> implement/verify loop -> PR -> human review -> merge/revision/restart
4. **Orchestration agents** run heartbeat loops to keep work moving — reprompting stalled agents, creating new tickets, escalating blockers
5. **Human touchpoints** are explicit and minimal — agents answer their own questions first, humans approve plans and review PRs
6. **The system is general-purpose** — works on any codebase with a CLAUDE.md, not hardcoded to creative-mode

### Verification:
1. `/swarm "add dark mode support"` -> classifies as project -> creates Linear project -> decomposes into research + code change tickets -> generates dependency graph
2. `/swarm research CM-123` -> produces research doc in `thoughts/`, comments summary on ticket, marks done
3. `/swarm code-change CM-456` -> research -> plan -> review -> implement -> verify -> PR, all tracked in Linear
4. `/swarm heartbeat` -> checks stalled projects -> reprompts orchestrators -> escalates critical blockers
5. Full restart capability: human rejects PR -> new ticket created referencing previous attempt -> fresh cycle with prior context

## What We're NOT Doing

- Building a custom workflow engine (Linear IS the workflow engine)
- Replacing existing commands (swarm primitives COMPOSE them)
- Persistent agent processes (OpenClaw is per-turn, Claude Code sessions are ephemeral)
- Auto-merging PRs (human always reviews and merges)
- Slack/Discord integration for non-critical communication (Linear comments are the default)
- Custom UI for swarm management (Linear UI + CLI is sufficient)

## Skill Directory Structure

```
.claude/skills/swarm/
├── SKILL.md                          # Main entry point — routing + overview (~250 lines)
├── primitives/
│   ├── conventions.md                # Labels, ticket format, comment conventions
│   ├── setup.md                      # One-time Linear label setup
│   ├── research.md                   # 1️⃣ Research primitive
│   ├── code-change.md                # 2️⃣ Code change lifecycle
│   ├── verify.md                     # 2️⃣.1️⃣ Verification
│   ├── project.md                    # 3️⃣ 4️⃣ 5️⃣ Project decomposition
│   ├── plan-review.md                # 5️⃣.1️⃣ Plan review
│   ├── project-verify.md             # 3️⃣.1️⃣ Project verification
│   ├── heartbeat.md                  # 6️⃣ 7️⃣ Orchestration heartbeat
│   └── status.md                     # Status dashboard
└── templates/
    ├── ticket-description.md         # Ticket description template with swarm metadata
    └── research-doc.md               # Research doc frontmatter + structure template
```

## Implementation Approach

**Single skill, multiple primitives**: One `/swarm` command routes via first argument to the appropriate primitive sub-file. Claude loads primitives on demand via markdown links.

**Convention over configuration**: Linear conventions (labels, ticket structure, description format) are defined in `primitives/conventions.md`.

**Composition over reimplementation**: Each primitive composes existing commands — `/research_codebase` for research, `/create_plan` for planning, `/implement_plan` for implementation, `/validate_plan` for verification, `/describe_pr` for PRs.

**Linear as state machine**: Ticket status drives the workflow. Agents read status to determine what to do next. Comments capture context for handoffs between agents.

---

## Phase 1: Skill Entry Point & Conventions

### Overview
Create the skill directory, main SKILL.md entry point, conventions reference, setup primitive, and templates.

### Changes Required:

#### 1. Main entry point
**File**: `.claude/skills/swarm/SKILL.md` (new)

```yaml
---
name: swarm
description: Agent swarm orchestration system. Turns high-level ideas into Linear-tracked, implemented software through composable primitives. Use when creating projects, research tasks, code changes, or checking swarm status.
allowed-tools: Bash, Read, Write, Edit, Grep, Glob, Agent, WebSearch, WebFetch, AskUserQuestion
---
```

Content includes:
- Quick reference table mapping operations to primitives
- Operation routing logic: parse first argument, load appropriate primitive
- Links to all primitives and templates
- Design principles (don't wait for human input, agents answer own questions, Linear is source of truth)

Operations:
| Invocation | Routes To | Purpose |
|---|---|---|
| `/swarm <idea>` | Classify + route | Auto-classify and route to right primitive |
| `/swarm research <topic or ticket>` | `primitives/research.md` | Research primitive (1️⃣) |
| `/swarm code-change <desc or ticket>` | `primitives/code-change.md` | Code change lifecycle (2️⃣) |
| `/swarm verify <ticket>` | `primitives/verify.md` | Verification (2️⃣.1️⃣) |
| `/swarm project <goal or project>` | `primitives/project.md` | Project decomposition (3️⃣ 4️⃣ 5️⃣) |
| `/swarm plan-review <plan path>` | `primitives/plan-review.md` | Plan review (5️⃣.1️⃣) |
| `/swarm project-verify <project>` | `primitives/project-verify.md` | Project verification (3️⃣.1️⃣) |
| `/swarm heartbeat` | `primitives/heartbeat.md` | Orchestration heartbeat (6️⃣ 7️⃣) |
| `/swarm status` | `primitives/status.md` | Status dashboard |
| `/swarm setup` | `primitives/setup.md` | One-time label setup |

When invoked without a recognized operation, classify the input:
- Need to understand something -> route to `research`
- Specific scoped change -> route to `code-change`
- Large multi-part goal -> route to `project`

#### 2. Conventions reference
**File**: `.claude/skills/swarm/primitives/conventions.md` (new)

Defines:
- **Linear team**: Default `CM` (read from CLAUDE.md or pass as argument)
- **Labels**: `swarm:research`, `swarm:code-change`, `swarm:verification`, `swarm:project`, `swarm:plan`, `swarm:orchestration`, plus `type:bug`, `type:feature`, `type:refactor`, `type:prototype`
- **Ticket description format**: Structured footer with swarm metadata (type, parent, research path, plan path, PR, previous attempt, dependencies)
- **Ticket lifecycle states**: Triage -> Backlog -> Todo -> In Progress -> In Review -> Done
- **Comment conventions**: Emoji-prefixed audit trail (`🔬 Research complete`, `📝 Plan created`, `🔨 Implementation started`, `🧪 Verification`, `📦 PR created`, `🔄 Revision requested`, `↻ Full restart`, `🤖 Heartbeat check`)
- **Parent/child structure**: Projects -> parent tickets (workstreams) -> child tickets (research, code-change, verification)
- **Dependency conventions**: `blocks`/`blocked-by` for sequential, `parent/child` for grouping
- **Research doc convention**: `thoughts/{git_user}/research/{timestamp}_{topic}.md` with `linear_ticket` in frontmatter
- **Plan doc convention**: `thoughts/{git_user}/plans/{timestamp}_{topic}.md` with `linear_ticket` and `linear_project` in frontmatter

#### 3. Setup primitive
**File**: `.claude/skills/swarm/primitives/setup.md` (new)

One-time setup:
1. Verify `linear-cli config doctor` passes
2. Create swarm labels (idempotent — skips if exists) with color coding
3. Verify workflow states exist for the team
4. Report setup status

#### 4. Templates
**File**: `.claude/skills/swarm/templates/ticket-description.md` (new)

Template for the structured swarm metadata footer appended to all swarm-created tickets.

**File**: `.claude/skills/swarm/templates/research-doc.md` (new)

Template for research document YAML frontmatter and section structure, including `linear_ticket` field.

### Success Criteria:

#### Automated Verification:
- [ ] `.claude/skills/swarm/SKILL.md` exists with valid YAML frontmatter
- [ ] `ls .claude/skills/swarm/primitives/` shows conventions.md, setup.md
- [ ] `ls .claude/skills/swarm/templates/` shows ticket-description.md, research-doc.md
- [ ] `/swarm setup` runs and creates labels in Linear

#### Manual Verification:
- [ ] `/swarm` is discoverable as a skill in Claude Code
- [ ] Labels visible in Linear UI with correct colors

---

## Phase 2: Core Primitive Files (1️⃣ 2️⃣ 3️⃣)

### Overview
Create the three core primitive files that handle research, code changes, and project decomposition.

### Changes Required:

#### 1. Research primitive
**File**: `.claude/skills/swarm/primitives/research.md` (new)

Invoked via `/swarm research <topic or ticket ID>`.

Process:
1. **Get or create ticket**: If given a ticket ID, fetch it. If given a topic, create a ticket with `swarm:research` label using `linear-cli i create "[RESEARCH] <topic>" -t CM -l swarm:research --id-only`
2. **Start work**: `linear-cli i update $TICKET -s "In Progress"`, comment `🔬 Starting research`
3. **Research**: Use Agent tool with appropriate subagent types (codebase-analyzer, web-search-researcher, thoughts-locator). Run agents in parallel where possible. Read all relevant files identified.
4. **Write research doc**: Generate metadata (git user, timestamp, branch, commit). Write to `thoughts/{git_user}/research/{timestamp}_{topic-slug}.md` using the research-doc template. Include `linear_ticket` in frontmatter.
5. **Update Linear**: Comment with summary + key findings + open questions. If no open questions -> Done. If open questions needing human input -> In Review.
6. **Return**: Output research doc path and ticket ID for calling primitives to use.

#### 2. Code change lifecycle
**File**: `.claude/skills/swarm/primitives/code-change.md` (new)

Invoked via `/swarm code-change <description or ticket ID>`. Optional: `previous:<ticket>` for full restart.

Process phases:

**Initiate**: Get or create ticket with `swarm:code-change` label. If restart, reference previous attempt via `linear-cli rel add $NEW -r related $OLD`. Start work with `linear-cli i start $TICKET --checkout`.

**Research**: Create child research ticket, execute research primitive, read resulting doc to inform the plan.

**Plan Revision Loop** (iterates v1 -> v2 -> v3... until approved):
- Create plan using `/create_plan` with ticket description + research doc as context
- Create plan review child ticket, execute plan-review primitive
- Set ticket to In Review, present plan to user via AskUserQuestion
- If approved -> continue. If revision needed -> comment, revise plan, loop back.

**Implement & Verify Loop** (Max's Bolt):
- Set to In Progress, execute `/implement_plan` with approved plan
- Execute verify primitive with plan's success criteria
- If fail -> comment with failure details, loop back to implement with context
- If pass -> comment success, continue

**Ship**: Execute `/describe_pr`, link PR via `linear-cli g pr $TICKET`, set to In Review.

**Human Review Outcomes** (presented via AskUserQuestion):
- Merge -> `linear-cli i update $TICKET -s Done`
- Revision needed -> back to plan revision loop with feedback
- Full restart -> create new ticket referencing this one, cancel old, restart flow

#### 3. Project decomposition
**File**: `.claude/skills/swarm/primitives/project.md` (new)

Invoked via `/swarm project <goal or project ID>`.

Process:
1. **Create or get project**: `linear-cli p create "<name>" -t CM --icon "🚀" --status planned --id-only`
2. **Initial research**: Create research parent ticket, break goal into 2-5 research questions, create child research tickets, execute research for each (parallel where possible)
3. **Synthesize research**: Read all research docs, create compressed summary comment on research parent ticket
4. **Create project plan & dependency graph** (5️⃣): Decompose into workstreams (parent tickets). For each, determine research, code changes, verification, dependencies, parallelism. Write project plan doc to `thoughts/` with dependency graph, parallelism analysis, and Graphite stack plan.
5. **Create tickets**: For each workstream create parent ticket. For each task create child ticket. Set blocking relations for sequential dependencies.
6. **Plan verification** (5️⃣.1️⃣): Execute plan-review primitive on the project plan. Present to human for approval.
7. **Update project**: `linear-cli p update $PROJECT --status started`
8. **Begin execution**: For each independent workstream (no blocking dependencies), execute code-change primitive.

### Success Criteria:

#### Automated Verification:
- [ ] `ls .claude/skills/swarm/primitives/` shows research.md, code-change.md, project.md
- [ ] Each file has clear process steps with linear-cli commands
- [ ] No hardcoded file paths — uses `git config user.name` for paths

#### Manual Verification:
- [ ] `/swarm research "how does the auth system work"` produces a research doc and updates Linear
- [ ] `/swarm code-change "add a /version endpoint"` walks through the full lifecycle
- [ ] `/swarm project "build a notification system"` creates project, decomposes, creates dependency graph

---

## Phase 3: Verification & Orchestration Primitives (2️⃣.1️⃣ 3️⃣.1️⃣ 5️⃣.1️⃣ 6️⃣ 7️⃣)

### Overview
Create the verification, review, and orchestration primitives.

### Changes Required:

#### 1. Verification primitive
**File**: `.claude/skills/swarm/primitives/verify.md` (new)

Invoked via `/swarm verify <ticket ID>`.

Process:
1. Read the plan document linked in the ticket
2. Extract automated and manual success criteria
3. Run each automated check, record command + exit code + output
4. Run agent-verifiable checks (file existence, endpoint responses)
5. List manual-only checks as pending human verification
6. Comment results on ticket with structured summary
7. Return PASS (all automated + agent checks pass) or FAIL (with failure details)

#### 2. Plan review primitive
**File**: `.claude/skills/swarm/primitives/plan-review.md` (new)

Invoked via `/swarm plan-review <plan path> <ticket ID>`.

Should be run by a DIFFERENT agent context (use Agent tool with isolation).

Process:
1. Read plan document completely
2. Evaluate against checklist: completeness (success criteria, file paths, code samples), feasibility (files exist, compatible with patterns, correct dependency order, no circular deps), gaps (edge cases, error handling, performance, security), research gaps (missing research, unverified assumptions, open questions)
3. Verify all file references actually exist in the codebase
4. Produce review: verdict (APPROVE/REVISE), strengths, gaps, new research questions, suggested changes
5. Comment review on ticket. If REVISE, create research tickets for any new questions.

#### 3. Project verification primitive
**File**: `.claude/skills/swarm/primitives/project-verify.md` (new)

Invoked via `/swarm project-verify <project ID or name>`.

Process:
1. List all issues in project via `linear-cli i list -t CM --output json`
2. Categorize: total, completed, in progress, blocked, not started — by workstream and type
3. Check milestones via `linear-cli ms list`
4. Identify blockers: tickets in progress >24h without updates, blocked by stuck tickets, missing dependencies
5. Report summary comment with progress, workstream status, blockers, recommendation (continue/reprioritize/escalate)

#### 4. Heartbeat primitive
**File**: `.claude/skills/swarm/primitives/heartbeat.md` (new)

Invoked via `/swarm heartbeat`.

Process:
1. **Discover**: Query Linear for all in-progress issues and active projects
2. **Check each project** (Lead FDE level 7️⃣): Run project-verify, identify stalled workstreams, check for dependency deadlocks, look for cross-project conflicts
3. **Check each in-progress ticket** (Project Orchestrator level 6️⃣): Read recent comments, check if dependencies resolved, comment with status check if stalled, identify what's blocking and take action
4. **Actions available**: Comment on tickets, create new research tickets, update priorities, create new child tickets for missed work
5. **Report summary**: Per-project status with actions taken
6. **Escalation** (critical only): All workstreams blocked, ticket in progress >48h with no commits, dependency deadlock, milestone date passed with <50% completion

#### 5. Status primitive
**File**: `.claude/skills/swarm/primitives/status.md` (new)

Invoked via `/swarm status`.

Process:
1. Query active projects, in-progress issues, recently completed, issues in review, sprint status
2. Format dashboard: projects table (name, progress, active tickets, blocked), awaiting human review list, in-progress list with last update, recently completed, metrics (velocity, cycle time)

### Success Criteria:

#### Automated Verification:
- [ ] `ls .claude/skills/swarm/primitives/` shows all 10 primitive files
- [ ] Each file references correct linear-cli commands
- [ ] No hardcoded ticket IDs or project IDs

#### Manual Verification:
- [ ] `/swarm verify CM-XXX` runs checks and reports results
- [ ] `/swarm plan-review thoughts/path/plan.md CM-XXX` produces structured review
- [ ] `/swarm heartbeat` queries Linear and reports status
- [ ] `/swarm status` shows dashboard of current activity

---

## Phase 4: Lead FDE Orchestration Agent (OpenClaw)

### Overview
Create the Lead FDE as an OpenClaw agent that runs on a heartbeat loop, monitoring all projects and driving the swarm forward. Extends the existing president agent pattern.

### Changes Required:

#### 1. Lead FDE workspace files
**Directory**: `$OPENCLAW_HOME/workspaces/lead-fde/` (created at runtime by provisioning code)

**SOUL.md**: Defines the Lead FDE identity — orchestration brain of the agent swarm. Operating principles: never wait for human input unnecessarily, answer own questions first, Linear is source of truth, comments are communication channel, be proactive not reactive. Safety rules: autonomous for ticket comments/research tickets/status updates; requires approval for new projects/cancelling tickets; forbidden from merging PRs/deleting projects.

**AGENTS.md**: Two modes — heartbeat (query Linear, assess projects, act on stalls, report, escalate critical) and reactive (respond to Discord messages with status/queries/reprioritization). Includes decision tree for handling stalled, blocked, and off-track projects.

**HEARTBEAT.md**: Every 30 minutes — query all CM in-progress tickets, check update times, identify stalled tickets (>24h), check dependency deadlocks, verify milestone progress, cross-reference with git activity. Actions: comment on stalled tickets, create follow-up research tickets, update MEMORY.md, post summary to Discord if significant changes.

**Skills**: `swarm-heartbeat/SKILL.md` and `swarm-status/SKILL.md` — OpenClaw skill frontmatter pointing to linear-cli usage patterns.

#### 2. Lead FDE provisioning
**File**: `harness/internal/leadfde/leadfde.go` (new)

Follows `harness/internal/president/president.go` pattern:
- `Manager` struct with `openclawHome`, `openclawBin`, `db`, `logger`
- `Provision()` — idempotent (check SOUL.md exists), writes workspace files, creates agent via CLI, binds to Discord channel
- Called from `harness/main.go` on startup if `LEAD_FDE_DISCORD_CHANNEL_ID` env var is set

#### 3. Lead FDE API endpoints
**File**: `harness/internal/server/leadfde_api.go` (new)

Routes under `/api/lead-fde` with auth middleware (validates `X-LeadFDE-Secret`):
- `GET /status` — returns swarm overview from Linear
- `POST /heartbeat` — trigger heartbeat manually
- `POST /spawn` — spawn Claude Code session for a task (creates tmux session `cm-swarm-{ticketID}`, runs `claude --command "/swarm code-change CM-123"`)

#### 4. Environment variables
Add to `harness/.env`:
- `LEAD_FDE_DISCORD_CHANNEL_ID` — can reuse `#creative-mode-dev` or create new channel
- `LEAD_FDE_SECRET` — auth for API endpoints

#### 5. Wire into harness startup
**File**: `harness/main.go` — add Lead FDE provisioning alongside president provisioning, guarded by env var check.

### Success Criteria:

#### Automated Verification:
- [ ] `ls $OPENCLAW_HOME/workspaces/lead-fde/` shows SOUL.md, AGENTS.md, HEARTBEAT.md, MEMORY.md, skills/
- [ ] `openclaw agents list` shows `lead-fde` agent
- [ ] `just check` passes (Go compilation)
- [ ] Lead FDE API endpoints respond

#### Manual Verification:
- [ ] Lead FDE responds in Discord when mentioned
- [ ] Heartbeat runs and reports on Linear status
- [ ] Lead FDE can spawn Claude Code sessions
- [ ] Stalled tickets get comments from the Lead FDE

---

## Phase 5: Integration Testing & Documentation

### Overview
End-to-end testing and CLAUDE.md documentation.

### Changes Required:

#### 1. Update CLAUDE.md
**File**: `CLAUDE.md` — add Agent Swarm System section:

```markdown
## Agent Swarm System

Composable agent swarm for turning high-level ideas into Linear-tracked, implemented software.

### Quick Start
| Command | Purpose |
|---------|---------|
| `/swarm setup` | One-time: create Linear labels |
| `/swarm "your idea"` | Classify and route to the right primitive |
| `/swarm research <topic>` | Deep research with Linear tracking |
| `/swarm code-change <desc>` | Full code change lifecycle |
| `/swarm project <goal>` | Decompose into tracked workstreams |
| `/swarm status` | Current swarm activity dashboard |
| `/swarm heartbeat` | Manual orchestration heartbeat |
```

#### 2. Error handling conventions
Add to each primitive:
- linear-cli exit code 3 (auth error): stop, tell user to run `linear-cli config doctor`
- linear-cli exit code 4 (rate limited): wait 60s, retry once
- Mid-execution failure: comment on ticket with error, keep status In Progress, report to user
- Never leave ticket in inconsistent state

### Success Criteria:

#### Automated Verification:
- [ ] All 10 primitive files + SKILL.md + 2 templates exist
- [ ] CLAUDE.md updated with swarm section

#### Manual Verification:
- [ ] `/swarm "research how auth works"` -> classifies -> creates ticket -> produces research doc -> updates Linear
- [ ] `/swarm code-change "add /version endpoint"` -> full lifecycle works
- [ ] `/swarm project "build notification system"` -> decomposition works
- [ ] `/swarm heartbeat` -> queries and reports
- [ ] Full restart: reject PR -> new ticket references old -> fresh cycle

---

## File Inventory

### New Files (15)

| File | Phase | Purpose |
|------|-------|---------|
| `.claude/skills/swarm/SKILL.md` | 1 | Main entry point — routing + overview |
| `.claude/skills/swarm/primitives/conventions.md` | 1 | Labels, ticket format, comment conventions |
| `.claude/skills/swarm/primitives/setup.md` | 1 | One-time Linear label setup |
| `.claude/skills/swarm/templates/ticket-description.md` | 1 | Ticket description template |
| `.claude/skills/swarm/templates/research-doc.md` | 1 | Research doc template |
| `.claude/skills/swarm/primitives/research.md` | 2 | Research primitive (1️⃣) |
| `.claude/skills/swarm/primitives/code-change.md` | 2 | Code change lifecycle (2️⃣) |
| `.claude/skills/swarm/primitives/project.md` | 2 | Project decomposition (3️⃣ 4️⃣ 5️⃣) |
| `.claude/skills/swarm/primitives/verify.md` | 3 | Verification primitive (2️⃣.1️⃣) |
| `.claude/skills/swarm/primitives/plan-review.md` | 3 | Plan review (5️⃣.1️⃣) |
| `.claude/skills/swarm/primitives/project-verify.md` | 3 | Project verification (3️⃣.1️⃣) |
| `.claude/skills/swarm/primitives/heartbeat.md` | 3 | Orchestration heartbeat (6️⃣ 7️⃣) |
| `.claude/skills/swarm/primitives/status.md` | 3 | Status dashboard |
| `harness/internal/leadfde/leadfde.go` | 4 | Lead FDE OpenClaw provisioning |
| `harness/internal/server/leadfde_api.go` | 4 | Lead FDE API endpoints |

### Modified Files (2)

| File | Phase | Purpose |
|------|-------|---------|
| `CLAUDE.md` | 5 | Add swarm system documentation |
| `harness/main.go` | 4 | Wire Lead FDE provisioning on startup |

---

## Testing Strategy

### Unit Tests:
- Ticket description format generation
- Dependency graph analysis (detect cycles, compute parallelism)
- Heartbeat stall detection logic

### Integration Tests:
- `/swarm research` creates ticket + doc + comments
- `/swarm code-change` full lifecycle (may use `--dry-run` for non-destructive testing)
- Dependency relations set correctly (`linear-cli rel list`)

### Manual Testing Steps:
1. Run `/swarm setup` to create labels
2. Run `/swarm "research how the auth system works"` — verify research flow
3. Run `/swarm "add a /version endpoint"` — verify code change flow
4. Run `/swarm "build a notification system"` — verify project decomposition
5. Run `/swarm heartbeat` — verify orchestration
6. Check Linear UI: verify all tickets, projects, relations, comments
7. Trigger a full restart: reject a PR, verify new ticket references old one

## Performance Considerations

- linear-cli calls are fast (single HTTP request each) but rate-limited (Linear API: 1500 req/hr)
- Heartbeat should batch Linear queries (list all issues once, not per-ticket)
- Claude Code sessions are expensive — spawn only when needed
- Research agents should run in parallel where possible (Agent tool supports this)
- Project decomposition should create all tickets in a batch, then set all relations

## Architecture Diagram

```
                    ┌──────────────────────────────────┐
                    │         Human Interface           │
                    │  /swarm "high-level idea"         │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │    SKILL.md (Classifier/Router)   │
                    │  Analyze → Classify → Route       │
                    └──┬──────────┬──────────┬─────────┘
                       │          │          │
              ┌────────▼──┐ ┌────▼─────┐ ┌──▼──────────┐
              │ primitives│ │primitives│ │ primitives/  │
              │ /research │ │/code-    │ │ project.md   │
              │ .md (1️⃣)  │ │change.md │ │ (3️⃣ 4️⃣ 5️⃣)   │
              │           │ │ (2️⃣)     │ │              │
              └───────────┘ └────┬─────┘ └──────┬──────┘
                                  │              │
                    ┌─────────────▼──────────────▼──────┐
                    │         Composition Layer          │
                    │  /research_codebase                │
                    │  /create_plan + plan-review.md     │
                    │  /implement_plan + verify.md       │
                    │  /describe_pr                      │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │       Linear (Source of Truth)     │
                    │  Projects → Parent Tickets         │
                    │  → Child Tickets → Relations       │
                    │  → Comments (audit trail)          │
                    │  → Status (workflow state)         │
                    └──────────────┬───────────────────┘
                                   │
              ┌────────────────────▼───────────────────┐
              │     Orchestration (OpenClaw Heartbeat)   │
              │                                         │
              │  Lead FDE (7️⃣)                           │
              │  ├── Every 30 min: check all projects   │
              │  ├── Reprompt stalled agents             │
              │  ├── Create new tickets for gaps         │
              │  └── Escalate critical blockers          │
              │                                         │
              │  Project Orchestrators (6️⃣)              │
              │  ├── Claude Code sessions per project   │
              │  ├── Check ticket progress               │
              │  └── Keep workstreams moving              │
              └─────────────────────────────────────────┘
```

## Dependencies

| Dependency | Status |
|------------|--------|
| linear-cli | Installed (38 skills) |
| Linear API key | Configured |
| Linear team CM | Created |
| OpenClaw | Installed on VPS |
| Claude Code commands | Working (`/create_plan`, etc.) |
| Graphite | Available for PR stacking |
| Discord bot | Configured (for Lead FDE Discord binding) |

## References

- Chestnut Agent Primitives Flowchart: `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html`
- Linear CLI Research: `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md`
- Existing Agent Hierarchy: `CLAUDE.md` (Agent System section)
- President Agent (reference impl): `harness/internal/president/`
- OpenClaw Orchestration Research: `thoughts/CoreyCole/research/2026-02-27_18-52-19_master-plan-orchestration-openclaw-agents.md`
- World Agents Master Plan: `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md`
- Linear CLI Skills: `.agents/skills/linear-*/SKILL.md` (38 skills)
