---
date: 2026-03-08T22:02:53-07:00
researcher: CoreyCole
git_commit: bdea199cec94a1605d2a0de42309d67a14dafdf2
branch: main
repository: humanlayer
topic: "How HumanLayer interfaces with Linear and strings context together"
tags: [research, codebase, linear, context-threading, workflow-automation, ci-cd]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
---

# Research: How HumanLayer interfaces with Linear and strings context together

**Date**: 2026-03-08T22:02:53-07:00
**Researcher**: CoreyCole
**Git Commit**: bdea199cec94a1605d2a0de42309d67a14dafdf2
**Branch**: main
**Repository**: humanlayer

## Research Question
How does HumanLayer interface with Linear and string context together across its various components?

## Summary

HumanLayer integrates with Linear through two parallel mechanisms: a **standalone TypeScript CLI** (`hack/linear/linear-cli.ts`) for batch/CI operations, and **Linear MCP tools** (`mcp__linear__*`) for interactive Claude Code sessions. Context is threaded across the system at two distinct layers: (1) the **ticket lifecycle pipeline** that carries research/plans/code between Linear, thoughts repo, and GitHub across multi-stage CI workflows, and (2) the **session-level context system** within HumanLayer's daemon (`hld`) that tracks conversation history, approval state, and session parentage through environment variables, SQLite, and event bus propagation.

## Detailed Findings

### 1. Linear Integration: Two Parallel Paths

#### Path A: Linear CLI (`hack/linear/linear-cli.ts`)

A standalone TypeScript CLI using `@linear/sdk`, installed globally as the `linear` command. Used in CI workflows and local scripts.

**Commands:**
- `list-issues` — Query issues by status, assignee, priority; supports `--ids-only --output-format json` for CI piping
- `get-issue <id>` — Dump full issue details (title, description, comments, links, labels) as markdown
- `get-issue-v2 <id> --fields <fields>` — Structured JSON output with specific fields (e.g., `branch` for git branch name)
- `add-comment -i <id> "<body>"` — Add a comment to a ticket
- `add-link <id> <url> --title "<title>"` — Attach a URL to a ticket
- `update-status <id> "<status>"` — Move ticket through workflow states
- `fetch-images <id>` — Download images from ticket markdown to `thoughts/shared/images/<id>/`
- `completion` — Generate shell completions

**Key design**: The CLI is a thin wrapper around the Linear GraphQL API. It outputs markdown by default for human/LLM readability and JSON for machine consumption.

**File**: `hack/linear/linear-cli.ts:1-700+`

#### Path B: Linear MCP Tools (Interactive)

Used during Claude Code sessions via MCP server. Referenced throughout `.claude/commands/` as `mcp__linear__*` tools:
- `mcp__linear__list_issues` — Search/filter issues
- `mcp__linear__get_issue` — Fetch issue details
- `mcp__linear__create_issue` — Create new tickets
- `mcp__linear__update_issue` — Update fields, add links
- `mcp__linear__create_comment` — Add comments
- `mcp__linear__list_teams` / `mcp__linear__list_projects` — Workspace metadata

**File**: `.claude/commands/linear.md:1-389` (comprehensive command definition)

### 2. The Ticket Lifecycle Pipeline (Context Threading Across Stages)

The core context-threading mechanism is a **multi-stage pipeline** that carries accumulated knowledge across separate Claude Code sessions via artifacts persisted in the `thoughts/` repository and Linear ticket comments/links.

```
                     thoughts/ repo (persistent context store)
                    ┌──────────────────────────────────────────┐
                    │  tickets/ENG-XXXX.md  (ticket snapshot)  │
                    │  images/ENG-XXXX/     (ticket images)    │
                    │  research/YYYY-MM-DD-ENG-XXXX-*.md       │
                    │  plans/YYYY-MM-DD-ENG-XXXX-*.md          │
                    └──────────────────────────────────────────┘
                          ↑ write          ↓ read
┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│ Research      │───→│ Planning     │───→│ Implementation│───→│ Code Review  │
│ Ticket        │    │ Ticket       │    │ Ticket        │    │ Ticket       │
│               │    │              │    │               │    │              │
│ research      │    │ ready for    │    │ ready for dev │    │ code review  │
│ needed →      │    │ plan →       │    │ → in dev →    │    │              │
│ research in   │    │ plan in      │    │ code review   │    │              │
│ progress →    │    │ progress →   │    │               │    │              │
│ research in   │    │ plan in      │    │               │    │              │
│ review        │    │ review       │    │               │    │              │
└──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘
     Stage 1             Stage 2             Stage 3
  linear-research    linear-create-plan  linear-implement-plan
  -tickets.yml       .yml                .yml
```

#### Stage 1: Research (`linear-research-tickets.yml`)

**Context in**: Linear ticket (title, description, comments, images, links)
**Context out**: Research document in `thoughts/shared/research/`, comment on Linear ticket

Flow:
1. `linear list-issues --status 'research needed'` → fetch ticket IDs (with WIP limit of 5 concurrent)
2. `linear get-issue ENG-XXXX` → dump to `thoughts/shared/tickets/ENG-XXXX.md`
3. `linear fetch-images ENG-XXXX` → download to `thoughts/shared/images/ENG-XXXX/`
4. `linear update-status ENG-XXXX "research in progress"`
5. Claude runs `/research_codebase` with the ticket file as input
6. Claude writes research to `thoughts/shared/research/YYYY-MM-DD-ENG-XXXX-*.md`
7. Git commit+push to thoughts repo
8. `linear add-comment` with Claude's output (includes open questions)
9. `linear update-status ENG-XXXX "research in review"`

**Context threading**: The ticket snapshot + images serve as the "prompt" for Claude. Claude's research output is persisted to `thoughts/` and linked back to Linear. Comments on the ticket allow humans to add instructions that the next stage reads.

**File**: `.github/workflows/linear-research-tickets.yml:1-278`

#### Stage 2: Planning (`linear-create-plan.yml`)

**Context in**: Linear ticket (re-fetched for latest comments) + research doc from Stage 1
**Context out**: Implementation plan in `thoughts/shared/plans/`, comment on Linear ticket

Flow:
1. `linear list-issues --status 'ready for plan'` → fetch ticket IDs
2. Re-fetch ticket with `linear get-issue` (may have new comments since research)
3. Re-fetch images (may have new attachments)
4. `linear update-status "plan in progress"`
5. Claude runs `/create_plan` — reads ticket, locates research in `thoughts/shared/research/`, reads images
6. Claude writes plan to `thoughts/shared/plans/YYYY-MM-DD-ENG-XXXX-*.md`
7. Git push to thoughts repo
8. `linear add-comment` with Claude's plan output
9. `linear update-status "plan in review"`

**Context threading**: The plan stage explicitly reads the research from the previous stage: "locate the research for the ticket in thoughts/shared/research". Human comments on the Linear ticket between stages serve as steering instructions.

**File**: `.github/workflows/linear-create-plan.yml:1-176`

#### Stage 3: Implementation (`linear-implement-plan.yml`)

**Context in**: Linear ticket + research + plan + git branch name
**Context out**: PR on GitHub, link on Linear ticket, code on branch

Flow:
1. `linear list-issues --status 'ready for dev'`
2. `linear get-issue-v2 --fields branch` → extract git branch name from Linear
3. Configure git branch (create or checkout existing)
4. Re-fetch ticket + images
5. `linear update-status "in dev"`
6. Claude runs `/implement_plan` — reads ticket, locates research AND plan in thoughts/
7. Claude runs `/ci_commit` to create atomic commits
8. Pull/merge with Claude resolving conflicts if needed
9. Push branch, create PR via `/ci_describe_pr`
10. `linear add-link` to attach PR URL to ticket
11. `linear add-comment` with PR details
12. `linear update-status "code review"`

**Context threading**: This stage reads ALL prior artifacts (ticket, research, plan) to execute the implementation. The git branch name comes directly from Linear, ensuring branch-ticket correspondence. The PR is linked back to the ticket for human reviewers.

**File**: `.github/workflows/linear-implement-plan.yml:1-288`

### 3. Interactive Context Threading (Slash Commands)

The `.claude/commands/` directory defines slash commands that thread context in interactive Claude sessions:

#### `/ralph_research` — Interactive research pipeline
Reads the Linear ticket, follows links, conducts codebase research, writes to `thoughts/`, syncs, and updates the Linear ticket with findings.

**File**: `.claude/commands/ralph_research.md:1-82`

#### `/ralph_plan` — Interactive planning pipeline
Reads the Linear ticket + previous research, creates implementation plan, syncs to `thoughts/`, attaches to ticket.

**File**: `.claude/commands/ralph_plan.md:1-60`

#### `/ralph_impl` — Interactive implementation pipeline
Creates a git worktree, launches a separate `humanlayer-nightly` session with the plan, auto-commits and creates PR.

**File**: `.claude/commands/ralph_impl.md:1-34`

#### `/linear` — General ticket management
Full-featured ticket CRUD with conventions for linking `thoughts/` documents to tickets via GitHub URLs.

**File**: `.claude/commands/linear.md:1-389`

#### Local orchestration script (`hack/dex/flow-tmux.sh`)
Orchestrates the full research→plan→Linear update flow locally using tmux windows. Each stage runs in a separate tmux window with the ticket number in the window title for tracking.

**File**: `hack/dex/flow-tmux.sh:1-62`

### 4. Session-Level Context System (HumanLayer Daemon)

Within the HumanLayer daemon (`hld`), context is threaded at the session level through several mechanisms:

#### Environment Variable Injection
When `hld` launches a Claude Code session, it injects:
- `HUMANLAYER_SESSION_ID` — ties MCP approval calls back to the session
- `HUMANLAYER_DAEMON_SOCKET` — socket path for daemon communication
- `HUMANLAYER_RUN_ID` — run identifier for logging

**File**: `hld/session/manager.go:182-544`

#### Conversation Event Storage
Every Claude Code event (messages, tool calls, tool results) is stored in SQLite as `conversation_events` with fields including `session_id`, `tool_id`, `parent_tool_use_id`, `approval_status`, and `approval_id`.

**File**: `hld/store/sqlite.go` — `AddConversationEvent`, `GetSessionConversation` (walks parent chain)

#### Session Fork/Continue
`ContinueSession` inherits ALL parent config (model, system prompt, tools, MCP servers, working dir) and uses `--resume --fork-session` to maintain Claude's conversation context across session boundaries.

**File**: `hld/session/manager.go:1434+`

#### Approval Correlation
Each approval is linked to its originating tool call via `tool_use_id`, connecting the approval UI context back to the specific point in Claude's conversation.

**File**: `hld/approval/manager.go:290+`

#### Real-time Propagation
`EventBus` → SSE → WUI provides live context updates (new approvals, status changes, conversation updates) to the browser UI.

**File**: `hld/bus/events.go`, `hld/api/handlers/sse.go`

### 5. The `thoughts/` Repository as Context Store

The `thoughts/` repo (separate from the main codebase at `humanlayer/thoughts`) serves as the durable context store across the entire pipeline:

| Path | Purpose |
|------|---------|
| `thoughts/shared/tickets/ENG-XXXX.md` | Snapshot of Linear ticket at fetch time |
| `thoughts/shared/images/ENG-XXXX/` | Downloaded images from ticket |
| `thoughts/shared/research/YYYY-MM-DD-ENG-XXXX-*.md` | Research findings |
| `thoughts/shared/plans/YYYY-MM-DD-ENG-XXXX-*.md` | Implementation plans |

Each CI workflow stage commits to this repo, creating a durable audit trail. The `humanlayer thoughts sync` command (from `hlyr`) handles git add/commit/push with retry logic for concurrent access.

### 6. Human-in-the-Loop Context Steering

A key design pattern is that humans can inject context between stages via Linear comments:

1. Claude researches a ticket and moves it to "research in review"
2. Human reviews research, adds a comment: "Focus on the SQLite migration approach, not the in-memory option"
3. Claude enters planning stage, reads the ticket with all comments (including the human's steering comment)
4. The instruction "Pay extra attention to the comments — if you (LinearLayer/claude) are mentioned in a comment you should treat that comment as an instruction" appears in every workflow

This creates a feedback loop where humans guide the AI through asynchronous comments on Linear tickets, without needing synchronous interaction.

## Code References

- `hack/linear/linear-cli.ts:1-700+` — Linear CLI implementation
- `.claude/commands/linear.md:1-389` — Interactive Linear command definition
- `.github/workflows/linear-research-tickets.yml:1-278` — Research CI workflow
- `.github/workflows/linear-create-plan.yml:1-176` — Planning CI workflow
- `.github/workflows/linear-implement-plan.yml:1-288` — Implementation CI workflow
- `.claude/commands/ralph_research.md:1-82` — Interactive research command
- `.claude/commands/ralph_plan.md:1-60` — Interactive planning command
- `.claude/commands/ralph_impl.md:1-34` — Interactive implementation command
- `hack/dex/flow-tmux.sh:1-62` — Local orchestration script
- `hld/session/manager.go:182-544` — Session launch with env var injection
- `hld/store/sqlite.go` — Conversation event persistence
- `hld/approval/manager.go:290+` — Approval-to-conversation correlation
- `hlyr/src/mcp.ts:50-181` — MCP request_permission tool
- `hlyr/src/daemonClient.ts:352-364` — Daemon RPC client

## Architecture Insights

### Dual Integration Pattern
Linear is accessed via two complementary paths: CLI for batch/CI (where `@linear/sdk` queries are needed) and MCP tools for interactive sessions (where Claude can call tools directly). This avoids coupling the daemon to Linear — the daemon knows nothing about Linear; all Linear logic lives in the CLI tool and slash command definitions.

### Context Accumulation Model
Each pipeline stage adds to a growing context corpus without modifying previous artifacts. Research adds to plans, plans add to implementation — but each stage re-reads the original ticket for the latest state. This "append-only with re-read" pattern ensures no context is lost while allowing human corrections.

### Separation of Durable vs. Ephemeral Context
- **Durable**: `thoughts/` repo (research, plans, ticket snapshots), Linear ticket state, git branches
- **Ephemeral**: Claude Code session conversation, daemon SQLite events, EventBus notifications

### Linear as State Machine
The Linear workflow states form a deterministic state machine with fallback transitions on failure:
```
research needed → research in progress → research in review → ready for plan
                  (failure → research needed)
ready for plan → plan in progress → plan in review → ready for dev
                 (failure → ready for plan)
ready for dev → in dev → code review → done
                (failure → ready for dev)
```

### WIP Limits
The research workflow enforces a configurable WIP limit (default 5) on "research in review" tickets to prevent Claude from overwhelming human reviewers with research to review.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md` — Prior research on the Linear CLI architecture
- `thoughts/CoreyCole/research/2026-03-08_21-51-08_humanlayer-commands-agents-skills.md` — Research on how commands/agents/skills connect across hlyr, hld, and WUI
- `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md` — Plan for context passing between agent sessions
- `thoughts/CoreyCole/handoffs/general/2026-03-09_swarm-review-bugs-context.md` — Handoff about "Deterministic Project Context Injection"

## Open Questions

1. How does the system handle conflicts when multiple CI workflows try to commit to the `thoughts/` repo simultaneously? (The research workflow has retry logic, but the plan workflow uses a simpler single-push)
2. Is there a mechanism to detect when research/plans become stale relative to codebase changes?
3. Could the Linear CLI and MCP tools be unified into a single integration layer?
4. How are images handled when Linear ticket images change between stages?
