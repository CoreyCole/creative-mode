# Swarm Vision Realization — Implementation Plan

## Overview

Bridge from the current Phase 4-complete swarm (code change lifecycle, goroutine+Temporal orchestration, learnings, dashboard) to the full "Chestnut Agent Primitives" vision: task classification, project decomposition with dependency graphs, two-level orchestration (Lead FDE + per-project orchestrators), Linear as source of truth, and Graphite-stacked PRs.

## Current State Analysis

**Built (Phases 1-4):**
- 3 workflow types in state machine: `research`, `code`, `project`
- Code change lifecycle with retry loops (plan revision, verify/implement)
- Temporal integration behind `CM_SWARM_TEMPORAL` feature flag
- Hook-driven completion, learning capture, relevance decay, daily digests
- Discord alerts, dashboard with SSE, per-session JSONL logging
- 8 Claude Code skills (research, code-plan, plan-review, code, code-verify, code-pr, setup, conventions)
- DB schema supports: `previous_workflow_id`, `swarm_project_milestones`, `swarm_tickets.parent_id/project_id`

**Not built:**
- Project skills (`swarm-project-plan`, `swarm-project-review`, `swarm-project-verify`) — referenced in state machine but files don't exist
- Previous attempt reference wiring
- Linear Go API client (skills use `linear-cli` CLI, which is not installed)
- Graphite `gt` CLI (not installed)
- Task classification / routing
- Dependency graph resolution and parallel workflow scheduling
- Two-level orchestration hierarchy
- Verification type expansion (unit/integration/E2E/Playwright)

### Key Discoveries:
- `linear-cli` is not installed on the VPS
- `gt` (Graphite CLI) is not installed on the VPS
- `swarm_project_milestones` table exists with `workflow_id`, `project_id`, `name`, `criteria`, `status` columns (`006_swarm_tables.sql:73-82`)
- `swarm_tickets.parent_id` and `project_id` columns exist but are unused in Go code (`006_swarm_tables.sql:84-98`)
- `SkillForPhase()` maps `project_plan`→`swarm-project-plan`, `project_review`→`swarm-project-review`, `project_verify`→`swarm-project-verify` (`statemachine.go:196-201`)
- `PhaseProjectVerify` retries indefinitely on `logic_failure` — no max check (`statemachine.go:162-169`)
- Phase 4G Temporal changes are uncommitted on `feature/agent-swarm`

## Desired End State

After this plan is complete:

1. **Task classification** routes ideas to the right primitive (question/code change/project) automatically
2. **Project workflows** decompose into research questions + child code-change tickets with a dependency graph
3. **Linear is source of truth** — tickets, comments, status transitions, and dependencies managed programmatically via Go API client
4. **Graphite stacking** orders PRs in dependency order for clean review
5. **Two-level orchestration** — Lead FDE heartbeat oversees all projects; per-project orchestrator heartbeats manage individual workstreams
6. **Previous attempt reference** flows through the full restart path with prior context available
7. **Verification expansion** supports unit, integration, E2E, and manual Playwright checks

**Verification:** `just check` passes, all existing tests pass, new workflow tests pass, project workflow can execute end-to-end with mock ticket data.

## What We're NOT Doing

- **Slack integration** — Discord is the alert channel; Slack deferred
- **AI-based task classification** — Using rule-based classification from ticket YAML footer (simpler, deterministic)
- **Temporal Cloud migration** — Staying with `temporal server start-dev` + SQLite
- **OpenClaw Lead FDE** — The Lead FDE heartbeat will be a Temporal workflow, not an OpenClaw agent (president stays separate)
- **Real-time Linear webhooks** — Polling/CLI-based sync, not webhook receivers
- **Automated PR merge** — Human review remains the final gate
- **Cross-repo orchestration** — Single-repo (creative-mode) only

## Implementation Approach

Nine sub-phases (5A-5I), each independently deployable. Early phases stabilize existing work and fill immediate gaps; later phases build the higher-level orchestration.

**Sequencing:** 5A → 5B → 5C → 5D → 5E → 5F → 5G → 5H → 5I

Dependencies: 5D (Linear) must precede 5F (dependency graphs) and 5G (orchestration). 5E (Graphite) must precede 5F. 5B (project skills) must precede 5F and 5G.

---

## Phase 5A: Commit & Stabilize

### Overview
Commit Phase 4G, install Temporal on VPS, verify both execution modes. This is the foundation for everything that follows.

### Changes Required:

#### 1. Commit Phase 4G changes
Commit all uncommitted files on `feature/agent-swarm`:
- `harness/go.mod`, `harness/go.sum` (modified)
- `harness/internal/swarmorch/manager.go` (modified)
- `harness/main.go` (modified)
- `harness/internal/swarmorch/activities.go` (new)
- `harness/internal/swarmorch/temporal.go` (new)
- `harness/internal/swarmorch/workflows.go` (new)
- `harness/internal/swarmorch/workflows_test.go` (new)
- `scripts/setup-temporal.sh` (new)

#### 2. Install Temporal on VPS
Run `scripts/setup-temporal.sh` to install Temporal CLI, create systemd service, and create `swarm` namespace.

#### 3. Verify both modes
- With `CM_SWARM_TEMPORAL=false`: ensure existing goroutine path works
- With `CM_SWARM_TEMPORAL=true`: verify workers connect, heartbeat schedule visible in Temporal UI at `:8233`

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `cd harness && go test ./internal/swarm/... ./internal/swarmorch/...` — all tests pass (51 existing + 6 new)
- [ ] `systemctl status temporal` shows active on VPS
- [ ] `temporal operator namespace describe swarm --address 127.0.0.1:7233` succeeds

#### Manual Verification:
- [ ] Temporal UI accessible at `127.0.0.1:8233` via SSH tunnel
- [ ] Start a dry-run workflow with `CM_SWARM_TEMPORAL=false`, verify goroutine path
- [ ] Start a dry-run workflow with `CM_SWARM_TEMPORAL=true`, verify Temporal path

---

## Phase 5B: Project Skills

### Overview
Write the 3 missing skill files so project workflows can actually execute through the state machine. The DB schema and state machine transitions already exist.

### Changes Required:

#### 1. swarm-project-plan skill
**File**: `.claude/skills/swarm-project-plan/SKILL.md` (NEW)

This skill creates a project plan that:
- Reads research findings (from the research phase)
- Decomposes the project into child tickets (research questions + code changes)
- Builds a dependency graph (what's parallel, sequential, independent)
- Maps to a Graphite stack plan (PR ordering)
- Creates a milestone checklist

The plan document goes to `thoughts/swarm/project-plans/` (directory already scaffolded).

Output includes:
- **Ticket Decomposition Table**: ticket ID placeholder, type (research/code), title, dependencies, parent
- **Dependency Graph**: Mermaid diagram showing parallel/sequential relationships
- **Milestone Checklist**: Named milestones with pass criteria
- **Graphite Stack Plan**: Branch ordering for PRs

```markdown
# Project Plan: {title}

## Scope
{What this project achieves}

## Ticket Decomposition

| # | Type | Title | Dependencies | Notes |
|---|------|-------|--------------|-------|
| 1 | research | {title} | none | Foundation research |
| 2 | research | {title} | none | Can run parallel with #1 |
| 3 | code | {title} | 1, 2 | Needs both research results |
| 4 | code | {title} | 3 | Sequential dependency |
| 5 | code | {title} | 3 | Can run parallel with #4 |

## Dependency Graph

{Mermaid diagram}

## Execution Order

### Wave 1 (parallel)
- Ticket #1: {title}
- Ticket #2: {title}

### Wave 2 (after Wave 1)
- Ticket #3: {title}

### Wave 3 (parallel, after Wave 2)
- Ticket #4: {title}
- Ticket #5: {title}

## Milestones

- [ ] M1: Research complete — all research tickets done
- [ ] M2: Core implementation — tickets #3 complete
- [ ] M3: Full implementation — all code tickets done
- [ ] M4: Verification — `just check` passes with all changes

## Graphite Stack Plan

Branch stack order (bottom to top):
1. `swarm/{projectID}/research-{slug1}` (ticket #1)
2. `swarm/{projectID}/research-{slug2}` (ticket #2)
3. `swarm/{projectID}/{slug3}` (ticket #3)
4. `swarm/{projectID}/{slug4}` (ticket #4, stacked on #3)
5. `swarm/{projectID}/{slug5}` (ticket #5, stacked on #3)

## Risks
- {Risk}: {Mitigation}
```

#### 2. swarm-project-review skill
**File**: `.claude/skills/swarm-project-review/SKILL.md` (NEW)

Reviews the project plan against a checklist:
- Scope alignment with ticket
- Ticket decomposition completeness (are all pieces covered?)
- Dependency accuracy (are the edges correct?)
- Execution ordering (can waves actually run in parallel?)
- Milestone criteria specificity (are they measurable?)
- Risk assessment

Verdict: approve → `PhaseDone` (orchestrator creates child tickets), revise → loop back to `PhaseProjectPlan`.

Review checklist:
1. **Scope alignment** — Does the decomposition cover the ticket's goals? (Critical)
2. **Ticket granularity** — Are tickets right-sized? No mega-tickets? (High)
3. **Dependency accuracy** — Do declared dependencies match actual code dependencies? (Critical)
4. **Execution ordering** — Can parallel waves truly run concurrently? (High)
5. **Milestone coverage** — Do milestones cover all major deliverables? (Medium)
6. **Risk assessment** — Are integration risks identified? (Medium)
7. **Convention compliance** — Follows swarm naming, paths, footer format? (Medium)

#### 3. swarm-project-verify skill
**File**: `.claude/skills/swarm-project-verify/SKILL.md` (NEW)

Verifies project milestones after all child tickets complete:
- Reads the project plan's milestone checklist
- Runs each milestone's verification criteria
- Reports PASS/FAIL per milestone
- On all pass → project done
- On any fail → retry (state machine has no max retries for project_verify)

This is analogous to `swarm-code-verify` but at the project level — checking that the aggregate of child changes meets the project's acceptance criteria.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] All 3 skill files exist and follow the skill template pattern
- [ ] `SkillForPhase("project_plan")` returns `"swarm-project-plan"` (already wired in state machine)
- [ ] State machine transitions: research(success) → project_plan → project_review → done

#### Manual Verification:
- [ ] Dry-run a project workflow: `POST /api/swarm/start {"ticket_id": "CM-TEST", "workflow_type": "project"}`
- [ ] Verify each phase invokes the correct skill

---

## Phase 5C: Previous Attempt Reference & Full Restart

### Overview
Wire `previous_workflow_id` into the StartWorkflow API so the "Full Restart" path from the flowchart works — a new attempt references the prior ticket, branch, and code.

### Changes Required:

#### 1. Extend StartWorkflow API
**File**: `harness/internal/server/swarm_api.go` (MODIFY)

Add `previous_workflow_id` to the start request:
```go
type startSwarmRequest struct {
    TicketID           string `json:"ticket_id"`
    WorkflowType       string `json:"workflow_type"`
    TicketURL          string `json:"ticket_url"`
    PreviousWorkflowID string `json:"previous_workflow_id"` // NEW
}
```

#### 2. Pass previous context to sessions
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

In `buildEnv()`, when `previous_workflow_id` is set:
- Set `CM_SWARM_PREVIOUS_WORKFLOW_ID` env var
- Resolve the previous workflow's branch name and set `CM_SWARM_PREVIOUS_BRANCH`
- Resolve the previous workflow's latest handoff and set `CM_SWARM_PREVIOUS_HANDOFF_PATH`
- Resolve the previous workflow's research doc and set `CM_SWARM_PREVIOUS_RESEARCH_PATH`

#### 3. Update skills to read previous context
**File**: `.claude/skills/swarm-research/SKILL.md` (MODIFY)
**File**: `.claude/skills/swarm-conventions/SKILL.md` (MODIFY)

Add to preamble: "If `$CM_SWARM_PREVIOUS_HANDOFF_PATH` is set, read the previous attempt's handoff for context. Note what worked and what failed."

Add new env vars to the conventions reference table:
```
CM_SWARM_PREVIOUS_WORKFLOW_ID    — Previous workflow for restart context
CM_SWARM_PREVIOUS_BRANCH         — Previous workflow's git branch
CM_SWARM_PREVIOUS_HANDOFF_PATH   — Previous workflow's last handoff
CM_SWARM_PREVIOUS_RESEARCH_PATH  — Previous workflow's research doc
```

#### 4. Update StartWorkflow to store previous_workflow_id
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

In `StartWorkflow()`, pass `previousWorkflowID` to `CreateSwarmWorkflow()` — the column already exists in the schema.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `go test ./internal/swarmorch/...` — test that previous_workflow_id is stored and env vars are set
- [ ] API accepts `previous_workflow_id` in start request

#### Manual Verification:
- [ ] Create workflow A, let it complete/fail
- [ ] Create workflow B with `previous_workflow_id = A`
- [ ] Verify B's research phase sees `CM_SWARM_PREVIOUS_*` env vars

---

## Phase 5D: Linear Integration

### Overview
Install `linear-cli` and create a Go-level Linear API wrapper so the harness can programmatically create tickets, update statuses, post comments, and manage dependencies — making Linear the source of truth.

### Changes Required:

#### 1. Install linear-cli
**File**: `scripts/setup-linear.sh` (NEW)

Install `linear-cli` via npm/npx. Verify auth with `linear-cli config doctor`. Add to PATH in `scripts/harness-run.sh`.

#### 2. Linear Go wrapper
**File**: `harness/internal/linear/client.go` (NEW)

Thin wrapper around `linear-cli` via `exec.CommandContext`:
```go
type Client struct {
    binPath string
    teamKey string // "CM"
    logger  *slog.Logger
}

// Ticket operations
func (c *Client) CreateTicket(ctx, title, description, labels []string, parentID string) (ticketID string, err error)
func (c *Client) UpdateStatus(ctx, ticketID, status string) error
func (c *Client) PostComment(ctx, ticketID, body string) error
func (c *Client) AddLabel(ctx, ticketID, label string) error
func (c *Client) SetParent(ctx, ticketID, parentID string) error

// Query operations
func (c *Client) GetTicket(ctx, ticketID string) (*Ticket, error)
func (c *Client) ListChildren(ctx, parentID string) ([]Ticket, error)

// Dependency operations (Linear relations)
func (c *Client) AddDependency(ctx, ticketID, dependsOnID string) error
func (c *Client) ListDependencies(ctx, ticketID string) ([]string, error)
```

Each method shells out to `linear-cli` with the appropriate subcommand, parses JSON output. All calls are serialized per-client to respect rate limits (1500 req/hr).

#### 3. Wire into swarm manager
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

Add `linearClient *linear.Client` field. New `SetLinearClient()` method. When set:
- `StartWorkflow()` creates the Linear ticket (if not exists) and syncs data to `swarm_tickets`
- `advanceWorkflow()` posts phase transition comments to Linear
- `markFailed()` posts terminal failure comment
- `markCompleted()` posts completion comment and moves to "In Review" or "Done"

#### 4. Wire into main.go
**File**: `harness/main.go` (MODIFY)

Conditionally create `linear.Client` when `LINEAR_CLI_PATH` (or auto-detect) is available. Call `swarmManager.SetLinearClient()`.

#### 5. Sync ticket data on workflow events
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

After Linear comment/status updates, upsert `swarm_tickets` with current data so the dashboard stays in sync.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `linear-cli config doctor` passes on VPS
- [ ] `go test ./internal/linear/...` — mock-based tests for client methods
- [ ] Manager creates tickets when Linear client is set

#### Manual Verification:
- [ ] Start a workflow → Linear ticket created with correct labels
- [ ] Phase transitions → comments posted to Linear
- [ ] Workflow completion → ticket moved to appropriate status

---

## Phase 5E: Graphite Integration

### Overview
Install Graphite `gt` CLI and integrate branch stacking into the PR skill so dependent PRs are properly ordered for review.

### Changes Required:

#### 1. Install Graphite CLI
**File**: `scripts/setup-graphite.sh` (NEW)

Install `gt` CLI. Auth with repo. Verify `gt --version`.

#### 2. Graphite wrapper
**File**: `harness/internal/graphite/client.go` (NEW)

```go
type Client struct {
    binPath string
    repoDir string
    logger  *slog.Logger
}

func (c *Client) CreateBranch(ctx, branchName string) error
func (c *Client) StackOn(ctx, branchName, parentBranch string) error
func (c *Client) Submit(ctx context.Context) error  // gt submit (creates/updates PRs for whole stack)
func (c *Client) ListStack(ctx context.Context) ([]StackEntry, error)
```

#### 3. Update swarm-code-pr skill
**File**: `.claude/skills/swarm-code-pr/SKILL.md` (MODIFY)

When `$CM_SWARM_STACK_PARENT` is set (from project orchestrator):
- Use `gt create {branch}` instead of `git checkout -b`
- Stack on the parent branch
- Use `gt submit` instead of `gh pr create` for proper stack ordering

Add env vars:
```
CM_SWARM_STACK_PARENT  — Parent branch for Graphite stacking (set by project orchestrator)
CM_SWARM_STACK_ORDER   — Position in stack (for PR description)
```

#### 4. Wire into manager
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

Add `graphiteClient *graphite.Client` field. When set and workflow has a `project_id`, `buildEnv()` sets `CM_SWARM_STACK_PARENT` based on the project plan's dependency graph.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `gt --version` succeeds on VPS
- [ ] `go test ./internal/graphite/...` — mock-based tests

#### Manual Verification:
- [ ] Create a branch with `gt create`, verify it appears in stack
- [ ] `gt submit` creates PRs with correct stacking order

---

## Phase 5F: Dependency Graph & Parallel Scheduling

### Overview
Project orchestrators can create child tickets with dependency edges, schedule parallel execution waves, and track milestone completion. This is the heart of primitive #5 from the flowchart.

### Changes Required:

#### 1. Dependency graph data model
**File**: `harness/internal/swarm/dependencies.go` (NEW)

```go
type DependencyGraph struct {
    Tickets []TicketNode
    Edges   []DependencyEdge
}

type TicketNode struct {
    TicketID     string
    WorkflowType WorkflowType
    Title        string
    Status       string
}

type DependencyEdge struct {
    From string // ticket that must complete first
    To   string // ticket that depends on From
}

// ComputeWaves returns tickets grouped into parallel execution waves.
// Wave N can only start when all Wave N-1 tickets are complete.
func (g *DependencyGraph) ComputeWaves() [][]TicketNode

// ReadyTickets returns tickets with all dependencies satisfied.
func (g *DependencyGraph) ReadyTickets(completed map[string]bool) []TicketNode
```

#### 2. DB tables for project dependencies
**File**: `harness/internal/db/migrations/007_swarm_dependencies.sql` (NEW)

```sql
CREATE TABLE IF NOT EXISTS swarm_ticket_dependencies (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL,
    depends_on_ticket_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(ticket_id, depends_on_ticket_id)
);
CREATE INDEX idx_swarm_deps_ticket ON swarm_ticket_dependencies(ticket_id);
CREATE INDEX idx_swarm_deps_project ON swarm_ticket_dependencies(project_id);
```

#### 3. sqlc queries for dependencies
**File**: `harness/internal/db/queries/swarm_dependencies.sql` (NEW)

Queries for creating dependencies, listing deps for a ticket, listing all deps for a project, checking if all deps are satisfied.

#### 4. Project spawner in manager
**File**: `harness/internal/swarmorch/project.go` (NEW)

When a project workflow completes the `project_review` phase with success:
1. Parse the approved project plan document
2. Create child tickets in Linear via `linearClient`
3. Store dependency edges in `swarm_ticket_dependencies`
4. Start child workflows for Wave 1 (no dependencies) via `StartWorkflow()`
5. Create `swarm_project_milestones` records

#### 5. Heartbeat integration
**File**: `harness/internal/swarmorch/activities.go` (MODIFY)

Add `CheckProjectProgress` activity to `ReadTicketQueue`:
- For each project with status `running`:
  - Check which child workflows are complete
  - Compute next ready wave via `ReadyTickets()`
  - Start workflows for newly-unblocked tickets
  - Update milestone statuses

#### 6. Update HeartbeatWorkflow
**File**: `harness/internal/swarmorch/workflows.go` (MODIFY)

Add `CheckProjectProgress` to the heartbeat activity sequence.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] Migration applies cleanly
- [ ] `go test ./internal/swarm/...` — dependency graph tests (topological sort, wave computation, cycle detection)
- [ ] `go test ./internal/swarmorch/...` — project spawner tests

#### Manual Verification:
- [ ] Create a project workflow → research → plan → review → child tickets created in Linear
- [ ] Child tickets have correct dependency edges
- [ ] Wave 1 tickets start immediately, Wave 2 waits for Wave 1

---

## Phase 5G: Two-Level Orchestration

### Overview
Replace the flat HeartbeatWorkflow with a two-level hierarchy: Lead FDE heartbeat (global) oversees all active projects; per-project orchestrator heartbeats manage individual workstreams. Maps to flowchart primitives #6 and #7.

### Changes Required:

#### 1. ProjectOrchestratorWorkflow
**File**: `harness/internal/swarmorch/workflows.go` (MODIFY)

```go
// ProjectOrchestratorWorkflow manages a single project's lifecycle.
// Runs as a long-lived workflow, waking every 2 minutes to check progress.
func ProjectOrchestratorWorkflow(ctx workflow.Context, projectID string) error {
    // Loop (ContinueAsNew every 100 iterations to avoid history buildup):
    // 1. Check child ticket statuses
    // 2. Advance ready tickets (start next wave)
    // 3. Detect stalls in child workflows
    // 4. Post Linear comments on parent ticket
    // 5. If all milestones pass → complete project
    // 6. If critical blocker → alert
}
```

#### 2. LeadFDEWorkflow (replaces HeartbeatWorkflow for project oversight)
**File**: `harness/internal/swarmorch/workflows.go` (MODIFY)

```go
// LeadFDEWorkflow is the global overseer.
// Runs on the ops queue, fires every 2 minutes via schedule.
func LeadFDEWorkflow(ctx workflow.Context) error {
    // 1. Run maintenance (DetectStalls, ReapSessions, DecayLearnings, GenerateDigest)
    // 2. Check each active project's orchestrator health
    // 3. Cross-project dependency check (shared resources like verify queue)
    // 4. If any project orchestrator is stalled, reprompt it
    // 5. Spawn sessions for non-project workflows (research, standalone code)
}
```

This replaces `HeartbeatWorkflow` as the top-level scheduled workflow.

#### 3. New activities
**File**: `harness/internal/swarmorch/activities.go` (MODIFY)

- `CheckProjectHealth` — queries all active project orchestrator workflows, flags any that haven't progressed
- `RepromptStalledOrchestrator` — signals a stalled project orchestrator workflow to re-check
- `PostProjectUpdate` — posts a status comment on the project's parent Linear ticket

#### 4. Project orchestrator spawning
**File**: `harness/internal/swarmorch/project.go` (MODIFY)

When a project plan is approved and child tickets are created (Phase 5F):
- Start a `ProjectOrchestratorWorkflow` as a long-lived Temporal workflow
- The orchestrator manages the project's child ticket lifecycle
- Store the orchestrator workflow ID in `swarm_workflows` for the project

#### 5. Update Temporal runtime
**File**: `harness/internal/swarmorch/temporal.go` (MODIFY)

Register `ProjectOrchestratorWorkflow` and `LeadFDEWorkflow` on appropriate workers. Update heartbeat schedule to use `LeadFDEWorkflow` instead of `HeartbeatWorkflow`.

#### 6. Dashboard updates
**File**: `harness/views/swarm/dashboard.templ` (MODIFY)

Add project view showing:
- Project status with milestone progress bar
- Child ticket statuses with dependency visualization
- Active orchestrator heartbeat timestamp

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `go test ./internal/swarmorch/...` — workflow tests for ProjectOrchestratorWorkflow, LeadFDEWorkflow
- [ ] Temporal test suite: orchestrator checks child statuses, advances waves, detects stalls

#### Manual Verification:
- [ ] Start a project → orchestrator workflow appears in Temporal UI
- [ ] Child tickets progress through waves
- [ ] Stalled child triggers alert from orchestrator
- [ ] LeadFDE detects stalled orchestrator

---

## Phase 5H: Task Classification

### Overview
Implement rule-based routing from the ticket YAML footer's `swarm_type` field to the correct workflow type, plus an AI fallback for tickets without explicit classification.

### Changes Required:

#### 1. Classification logic
**File**: `harness/internal/swarm/classify.go` (NEW)

```go
// ClassifyTicket determines workflow type from ticket metadata.
func ClassifyTicket(title, description string) WorkflowType {
    // 1. Check YAML footer for explicit swarm_type
    // 2. If not present, use keyword rules:
    //    - Contains "research", "investigate", "explore" → research
    //    - Contains "project:", multi-ticket references → project
    //    - Default → code
}
```

#### 2. Auto-classify in StartWorkflow
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

When `workflow_type` is empty in the API request, call `ClassifyTicket()` to infer it. Log the classification decision.

#### 3. Classification from Linear ticket
**File**: `harness/internal/swarmorch/manager.go` (MODIFY)

When Linear client is available and `workflow_type` is not provided:
- Fetch ticket details via `linearClient.GetTicket()`
- Parse YAML footer from description
- Use explicit `swarm_type` if present
- Fall back to keyword classification

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `go test ./internal/swarm/...` — classification tests (explicit footer, keyword rules, defaults)
- [ ] API auto-classifies when `workflow_type` is omitted

#### Manual Verification:
- [ ] Create ticket with `swarm_type: research` footer → starts research workflow
- [ ] Create ticket with no footer, title "Implement dark mode" → starts code workflow
- [ ] Create ticket with `swarm_type: project` footer → starts project workflow

---

## Phase 5I: Verification Expansion

### Overview
Expand `swarm-code-verify` to support four verification types from the flowchart: unit tests, integration tests, E2E Playwright, and manual Playwright CLI checks.

### Changes Required:

#### 1. Update plan template for verification types
**File**: `.claude/skills/swarm-code-plan/SKILL.md` (MODIFY)

Update the Verification Checks section template:
```markdown
## Verification Checks

### Unit Tests
1. `cd harness && go test ./internal/swarm/...` — State machine tests

### Integration Tests
2. `cd harness && go test -tags=integration ./internal/swarmorch/...` — Integration tests

### E2E Tests
3. `playwright-cli open http://localhost:8080/swarm --headed` → snapshot → verify element exists

### Manual Playwright Checks
4. Navigate to `/swarm` → verify dashboard loads → screenshot
```

#### 2. Update verify skill
**File**: `.claude/skills/swarm-code-verify/SKILL.md` (MODIFY)

Add structured verification phases:
1. **Compilation**: `just check`
2. **Unit Tests**: Run plan's unit test commands
3. **Integration Tests**: Run plan's integration test commands (if any)
4. **E2E Tests**: Run plan's Playwright commands (if any) using `playwright-cli`
5. **Manual Checks**: Run plan's manual Playwright sequences (snapshot, interact, screenshot)

Each category reports independently:
```
[PASS] Unit Tests: 3/3 passed
[FAIL] Integration Tests: 1/2 passed — TestSwarmWorkflowE2E failed
[SKIP] E2E Tests: no E2E checks in plan
[PASS] Manual Checks: dashboard loads, screenshot taken
```

#### 3. Playwright integration in verify
**File**: `.claude/skills/swarm-code-verify/SKILL.md` (MODIFY)

Add `playwright-cli` to allowed tools. For E2E/manual checks:
- Use `playwright-cli open` with `--headed --persistent` for stateful sessions
- Use `playwright-cli snapshot` to verify elements
- Use `playwright-cli screenshot` and read the PNG for visual verification
- Use `playwright-cli console error` to catch JS errors

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] Plan template includes all 4 verification categories
- [ ] Verify skill handles each category independently

#### Manual Verification:
- [ ] Dry-run verify with all 4 categories → structured report
- [ ] E2E check uses playwright-cli correctly
- [ ] Manual check takes screenshot and reports

---

## Testing Strategy

### Unit Tests:
- `dependencies_test.go` — Graph construction, topological sort, wave computation, cycle detection
- `classify_test.go` — YAML footer parsing, keyword rules, edge cases
- `linear/client_test.go` — Mock exec tests for each CLI command
- `graphite/client_test.go` — Mock exec tests
- `project_test.go` — Project plan parsing, child ticket creation logic

### Integration Tests:
- Project workflow E2E: start → research → plan → review → child tickets created → wave scheduling
- Two-level orchestration: LeadFDE detects stalled orchestrator, reprompts
- Dependency graph: Wave N waits for Wave N-1, ready tickets computed correctly

### Temporal Workflow Tests:
- `ProjectOrchestratorWorkflow` — checks children, advances waves, completes on all milestones
- `LeadFDEWorkflow` — runs maintenance + project oversight, handles stalled orchestrators

### Manual Testing Steps:
1. Start a code workflow end-to-end, verify all phases
2. Start a project workflow, verify decomposition into child tickets
3. Verify parallel waves execute concurrently
4. Kill a child workflow mid-phase, verify orchestrator detects and handles
5. Verify Linear comments posted at each transition
6. Verify Graphite stacking in PR creation

## Performance Considerations

- Linear API rate limit: 1500 req/hr — serialize calls, batch where possible
- `swarm-verify` queue at concurrency 1 prevents OOM from parallel `just check` (each WASM build uses ~5GB)
- Project orchestrator polling every 2 minutes is sufficient — child workflows signal via Temporal
- Dependency graph computation is O(V+E), negligible for expected project sizes (<50 tickets)

## Migration Notes

- New migration `007_swarm_dependencies.sql` adds the `swarm_ticket_dependencies` table
- No existing data needs migration — new tables only
- Feature flags: `CM_SWARM_TEMPORAL=true` remains required for orchestration features
- Linear/Graphite are optional — system degrades gracefully without them (skills fall back to manual CLI)

## References

- Flowchart: HTML document provided in conversation
- v5 master plan: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- Phase 4 completion plan: `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md`
- Phase 4G handoff: `thoughts/CoreyCole/handoffs/general/2026-03-01_12-34-32_swarm-phase4g-temporal-integration.md`
- Existing state machine: `harness/internal/swarm/statemachine.go`
- Existing orchestrator: `harness/internal/swarmorch/manager.go`
- Skill files: `.claude/skills/swarm-*/SKILL.md`
- Conventions: `.claude/skills/swarm-conventions/SKILL.md`
