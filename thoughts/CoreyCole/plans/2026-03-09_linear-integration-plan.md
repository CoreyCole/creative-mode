# Swarm Agent Improvements + Linear Integration Plan

## Overview

Two combined efforts:

1. **Agent prompt improvements** (Phase 0) — 5 HumanLayer-inspired changes to agent prompts and Go types that improve research quality, plan actionability, and output searchability. Ready to implement with exact code snippets.

2. **Linear integration** (Phases 1-6) — Add Linear tracking so Temporal workflows push status updates, labels, structured comments, artifact links, and follow-up tickets. All Linear operations use the `linear-cli` Rust binary (already installed) via `exec.CommandContext` — no Go SDK needed.

## Current State (updated 2026-03-09)

**Completed** (committed as `d23b4e7` on `feat/agent-primitives`):
- Phase 0: All 5 prompt improvements applied, Go compiles, JS parses
- Phase 1: `harness/internal/linear/` package created, 7 labels exist in Linear
- Phase 2: 7 Linear activities on `SwarmActivities`, all nil-safe
- Phase 4 (partial): Basic Linear activities wired at stage boundaries (status, labels, simple comments, artifact links)
- Phase 5: Artifact URL route + `GetSwarmArtifact` query
- Phase 6: Linear client initialized from env, `HarnessURL` on `SwarmConfig`

**Not yet started**:
- Phase 2.5: Review fixes (artifactURL nil deref bug, tags cleanup, path separator, LookPath)
- Phase 3: Post-processor agent, ticket context injection, full workflow wiring (FetchLinearTicket, follow-ups)

**Known bug**: `artifactURL` panics at runtime — see Phase 2.5a

## Design Decisions

### Labels over custom states

Linear workflow states are sequential — an issue can only be in one state. As the swarm handles more task types (research, code changes, projects, implementation, verification), per-stage states would explode. Instead:

- **States** (minimal, kept as-is): Backlog, Todo, In Progress, In Review, Done, Canceled, Duplicate
- **Labels** (track current activity): `swarm:research`, `swarm:planning`, `swarm:implementing`, `swarm:verifying`, `type:research`, `type:code-change`, `type:project`

A ticket in "In Progress" with label `swarm:research` tells you the swarm is actively researching. When research finishes and planning starts, swap label to `swarm:planning`. States only change at major boundaries (start work, need human review, done).

### Relations over ticket types for research → plan linkage

- **Standalone research**: `type:research` ticket, no parent. Research completes → Done.
- **Code change**: `type:code-change` ticket. Built-in research runs inside the workflow.
- **Code change needing deeper research**: The code change ticket has `blocked-by` relations to `type:research` tickets. Swarm reads linked research docs as additional context before planning.
- **Post-processor spawned follow-ups**: Creates `type:research` tickets with `blocked-by` (prerequisite) or `relates-to` (tangential) relations.

### Comments as knowledge ledger

Comments are NOT status updates (that's what states/labels are for). Comments are structured context for future agents:

```markdown
## Context
- [Key findings or decisions that matter for future work]

## Learnings
- [Mistakes made and what they taught us]
- [Assumptions that turned out wrong]

## Out of Scope
- [What this ticket explicitly does NOT cover]
- [Items that looked related but belong in separate tickets]
```

### Post-processor agent validates scope

A dedicated agent (`linear-context-processor.js`) runs after major stage completions. It:
1. Reads the artifact (research doc or plan doc)
2. Reads the fresh ticket from Linear (picks up human comments)
3. Validates out-of-scope claims
4. Writes a structured comment
5. Judges which out-of-scope items warrant follow-up research tickets
6. For follow-ups, searches Linear first to avoid duplicates, then creates with appropriate relation

## What We're NOT Doing

- **Linear as trigger** — No webhook/polling to auto-start workflows from Linear status changes. Workflows still start via `POST /api/swarm/tasks/*`. This can be added later.
- **Gates** — No human review gates between stages. Stages chain sequentially. The post-processor comments provide context but don't block.
- **Image fetching** — Deferred. The `linear-cli uploads` command exists but ticket images are rarely relevant to code research/planning. Revisit when implementing UI-focused workflows.
- **Bidirectional sync** — Linear doesn't push back to the swarm. The swarm is the source of truth for execution; Linear is the source of truth for project tracking.

## Implementation

### Phase 0: Agent prompt improvements (HumanLayer-inspired)

5 independent prompt/type changes. No ordering constraints except Go must compile. Exact code provided in `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md`.

#### 0a. Documentarian constraint — Research agents

**`harness/agents/research-agent.js`** — Add CRITICAL block after role description, before `selfReflection()`:

```
CRITICAL — You are a documentarian, not a critic:
- DO NOT suggest improvements or changes to the codebase
- DO NOT critique the implementation or identify problems
- DO NOT propose future enhancements or refactoring
- ONLY describe what exists, how it works, and how components interact
- Your job is to document facts with file:line references, not to evaluate
```

**`harness/agents/research-questions.js`** — Lighter variant after role description:

```
CRITICAL — Decompose into factual questions only:
- Each sub-question must ask WHAT exists or HOW something works
- DO NOT include questions that suggest improvements or evaluate quality
- DO NOT frame questions around "what's wrong" or "what could be better"
- Sub-questions should lead to documentation of current state, not criticism
```

#### 0b. Split verification checks — Go types + JS output

**`harness/internal/swarmorch/types.go:138-145`** — Replace `VerificationChecks []string` with:

```go
AutomatedVerification []string `json:"automatedVerification"`
ManualVerification    []string `json:"manualVerification"`
```

**`harness/internal/swarmorch/artifact.go:288`** — Update validation:

```go
if len(a.AutomatedVerification) == 0 && len(a.ManualVerification) == 0 {
    return errors.New("must include at least one verification check (automated or manual)")
}
```

**`harness/internal/swarmorch/artifact.go:27`** — Update `yamlMultiKeyRe` to replace `verificationChecks` with `automatedVerification|manualVerification`.

**`harness/agents/specialist-planner.js:27-29`** — Update YAML frontmatter output format:

```yaml
automatedVerification:
  - "just check"
  - "go test ./..."
manualVerification:
  - "Verify the UI renders correctly"
  - "Test edge case: empty input"
```

**`harness/agents/plan-synthesizer.js`** — Add guideline: "Preserve the automated vs manual verification distinction from each specialist"

#### 0c. "What We're NOT Doing" scope section

**`harness/agents/specialist-planner.js`** — Add to guidelines: "Flag items that are explicitly out of scope for your domain"

**`harness/agents/plan-synthesizer.js`** — Add to guidelines: "Include a 'What We're NOT Doing' section listing explicit out-of-scope items to prevent scope creep"

#### 0d. Tags in output frontmatter

Add `tags` field to YAML frontmatter in output format for 4 agents:
- `harness/agents/research-agent.js`
- `harness/agents/research-synthesizer.js`
- `harness/agents/specialist-planner.js`
- `harness/agents/plan-synthesizer.js`

With instruction: "Choose 2-5 tags from: database, api, temporal, ui, bevy, wasm, discord, auth, migration, config, build, testing, or other relevant terms."

#### 0e. Deeper thinking prompt

**`harness/agents/lib/prompts.js`** — Add step 4 to `selfReflection()`:

```javascript
export function selfReflection(verb) {
  return `Before starting work:
1. Review the project documentation and skills manifest already provided in your context
2. Use read to load the full content of any skills relevant to your task (paths listed in the manifest)
3. Use search_context if you need to find specific source files beyond what's in the documentation
4. Think deeply about underlying patterns, connections, and architectural implications before proceeding
5. Then proceed with ${verb}`;
}
```

#### Phase 0 files changed

- `harness/agents/research-agent.js` — documentarian CRITICAL block + tags
- `harness/agents/research-questions.js` — factual questions constraint
- `harness/agents/specialist-planner.js` — verification split + tags + scope exclusions
- `harness/agents/plan-synthesizer.js` — preserve verification split + "What We're NOT Doing" + tags
- `harness/agents/research-synthesizer.js` — tags
- `harness/agents/lib/prompts.js` — thinking step in selfReflection()
- `harness/internal/swarmorch/types.go` — PlannerOutput verification split
- `harness/internal/swarmorch/artifact.go` — validation + yamlMultiKeyRe update

### Phase 1: Linear CLI wrapper + Label/Workspace setup

Create a Go package that wraps `linear-cli` commands and set up the Linear workspace with labels.

#### 1a. Linear CLI Go wrapper

**New file**: `harness/internal/linear/cli.go`

A thin wrapper around `exec.CommandContext` calls to `linear-cli`. Each method runs a CLI command with `--output json --compact --quiet` flags and parses the JSON result.

```go
package linear

type Client struct {
    binaryPath string // path to linear-cli binary
    teamKey    string // "CRE"
}

// Core operations:
func (c *Client) GetIssue(ctx context.Context, identifier string) (*Issue, error)
func (c *Client) UpdateStatus(ctx context.Context, identifier, status string) error
func (c *Client) UpdateLabels(ctx context.Context, identifier string, labels []string) error
func (c *Client) AddComment(ctx context.Context, identifier, body string) error
func (c *Client) CreateAttachment(ctx context.Context, identifier, title, url string) error
func (c *Client) CreateIssue(ctx context.Context, title, description string, opts CreateOpts) (string, error)
func (c *Client) SearchIssues(ctx context.Context, query string) ([]Issue, error)
func (c *Client) AddRelation(ctx context.Context, from, relationType, to string) error
func (c *Client) ListRelations(ctx context.Context, identifier string) ([]Relation, error)
```

**New file**: `harness/internal/linear/types.go`

```go
type Issue struct {
    Identifier  string   `json:"identifier"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    State       State    `json:"state"`
    Labels      []Label  `json:"labels"`
    // Comments, relations fetched separately
}

type State struct {
    Name string `json:"name"`
    Type string `json:"type"` // backlog, unstarted, started, completed, canceled
}

type Label struct {
    Name  string `json:"name"`
    Color string `json:"color"`
}

type Relation struct {
    Type       string `json:"type"` // blocks, blocked-by, related, duplicate
    Identifier string `json:"identifier"`
}

type CreateOpts struct {
    Team        string
    Priority    int
    Labels      []string
    State       string
    Description string // if set, piped via stdin with "-d -"
}
```

#### 1b. Create labels in Linear

**Setup script or one-time commands** (included in plan for reproducibility):

```bash
# Type labels (what kind of ticket)
linear-cli labels create "type:research" --type issue -c "#5B8DB8"
linear-cli labels create "type:code-change" --type issue -c "#8B6CB0"
linear-cli labels create "type:project" --type issue -c "#D4920A"

# Swarm stage labels (what the swarm is currently doing)
linear-cli labels create "swarm:research" --type issue -c "#F2C94C"
linear-cli labels create "swarm:planning" --type issue -c "#F2994A"
linear-cli labels create "swarm:implementing" --type issue -c "#27AE60"
linear-cli labels create "swarm:verifying" --type issue -c "#EB5757"
```

Add these commands to a setup script: `harness/scripts/setup-linear.sh`

### Phase 2: Temporal activities for Linear operations

**Modified file**: `harness/internal/swarmorch/activities.go`

Add `LinearClient *linear.Client` to the `SwarmActivities` struct. All Linear activities no-op when `ticketID` is empty.

New activities:

```go
// FetchLinearTicket fetches the full ticket from Linear and writes a snapshot
// to thoughts/swarm/tickets/{identifier}.md. Returns the issue data for
// injection into agent context.
func (a *SwarmActivities) FetchLinearTicket(ctx context.Context, ticketID string) (*linear.Issue, error)

// UpdateLinearStatus changes the workflow state on the Linear ticket.
func (a *SwarmActivities) UpdateLinearStatus(ctx context.Context, ticketID, status string) error

// UpdateLinearLabels sets labels on the Linear ticket, replacing swarm:* labels.
func (a *SwarmActivities) UpdateLinearLabels(ctx context.Context, ticketID string, labels []string) error

// AddLinearComment posts a structured comment to the Linear ticket.
func (a *SwarmActivities) AddLinearComment(ctx context.Context, ticketID, body string) error

// LinkArtifactToLinear attaches an artifact URL to the Linear ticket.
func (a *SwarmActivities) LinkArtifactToLinear(ctx context.Context, ticketID, title, url string) error

// CreateLinearFollowup creates a new research ticket and links it to the parent.
func (a *SwarmActivities) CreateLinearFollowup(ctx context.Context, parentID, title, description, relationType string) (string, error)

// SearchLinearIssues checks if a similar ticket already exists.
func (a *SwarmActivities) SearchLinearIssues(ctx context.Context, query string) ([]linear.Issue, error)
```

Activity options: infrastructure tier (30s timeout, 3 retries) — these are fast CLI calls.

### Phase 2.5: Review fixes (bugs and hardening from staff eng review)

Addresses critical bug and concerns from `thoughts/CoreyCole/reviews/2026-03-09_00-22-17_linear-integration-plan_review.md`.

#### 2.5a. Fix `artifactURL` nil pointer dereference — CRITICAL

**Bug**: `artifactURL(a, researchArtifactID)` at `workflows.go:525` and `workflows.go:784` accesses `a.config.HarnessURL` where `a` is the nil `*SwarmActivities` pointer used for Temporal activity references. This panics at runtime whenever an artifact ID is non-empty.

**Fix**: Add `HarnessURL` field to both workflow input structs and thread it from `SwarmManager`:

```go
// workflows.go — add to both input structs
type ResearchWorkflowInput struct {
    TaskID        string `json:"taskID"`
    RequestText   string `json:"requestText"`
    RepoRoot      string `json:"repoRoot"`
    ParentSpanID  string `json:"parentSpanID,omitempty"`
    LinearIssueID string `json:"linearIssueID,omitempty"`
    HarnessURL    string `json:"harnessURL,omitempty"`
}

type CodeChangePlanWorkflowInput struct {
    TaskID        string `json:"taskID"`
    RequestText   string `json:"requestText"`
    RepoRoot      string `json:"repoRoot"`
    LinearIssueID string `json:"linearIssueID,omitempty"`
    HarnessURL    string `json:"harnessURL,omitempty"`
}
```

Change `artifactURL` to a pure function:

```go
func artifactURL(harnessURL, artifactID string) string {
    if harnessURL == "" {
        return "/swarm/artifacts/" + artifactID + "/view"
    }
    return strings.TrimRight(harnessURL, "/") + "/swarm/artifacts/" + artifactID + "/view"
}
```

Update callers:
- `workflows.go:525`: `artifactURL(input.HarnessURL, researchArtifactID)`
- `workflows.go:784`: `artifactURL(input.HarnessURL, planArtifactID)`

Thread `HarnessURL` from `SwarmConfig` through `SwarmManager.StartResearch` and `StartCodePlan`:

```go
// manager.go — pass config.HarnessURL into workflow inputs
_, err := m.client.ExecuteWorkflow(ctx, opts, ResearchWorkflow,
    ResearchWorkflowInput{
        TaskID:        taskID,
        RequestText:   requestText,
        RepoRoot:      m.repoRoot,
        LinearIssueID: ticketID,
        HarnessURL:    m.config.HarnessURL,
    },
)
```

Also pass `HarnessURL` when `CodeChangePlanWorkflow` spawns the child `ResearchWorkflow`:

```go
// workflows.go — child workflow call
childInput := ResearchWorkflowInput{
    TaskID:        input.TaskID,
    RequestText:   input.RequestText,
    RepoRoot:      input.RepoRoot,
    ParentSpanID:  spanID,
    LinearIssueID: input.LinearIssueID,
    HarnessURL:    input.HarnessURL,
}
```

**Files**: `workflows.go`, `manager.go`

#### 2.5b. Remove orphaned `tags` from agent prompts

The `tags` field is instructed in all agent output formats but no Go struct has a `Tags` field — data is silently dropped. Also missing from `yamlMultiKeyRe`. Rather than adding dead weight to every artifact, remove `tags` from agent prompts until we have a consumer (e.g., search/filter on dashboard).

**Remove** the `tags:` block and tag guidance line from:
- `harness/agents/research-agent.js`
- `harness/agents/research-synthesizer.js`
- `harness/agents/specialist-planner.js`
- `harness/agents/plan-synthesizer.js`

**Files**: 4 agent JS files

#### 2.5c. Fix path traversal separator in artifact handler

**`harness/internal/server/swarm_dashboard.go`** — change:

```go
// Before
if !strings.HasPrefix(absPath, absRoot) {

// After
if !strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) {
```

Matches the pattern used by `handleWASMArtifacts` in the same file.

**Files**: `swarm_dashboard.go`

#### 2.5d. Use `exec.LookPath` for `linear-cli` binary

**`harness/main.go`** — replace hardcoded path with env var + PATH lookup:

```go
linearBin := os.Getenv("LINEAR_CLI")
if linearBin == "" {
    var err error
    linearBin, err = exec.LookPath("linear-cli")
    if err != nil {
        logger.Warn("linear-cli not found in PATH, Linear integration disabled")
    }
}
```

**Files**: `main.go`

#### Phase 2.5 files changed

- `harness/internal/swarmorch/workflows.go` — fix `artifactURL`, add `HarnessURL` to input structs
- `harness/internal/swarmorch/manager.go` — thread `HarnessURL` into workflow inputs
- `harness/agents/research-agent.js` — remove tags
- `harness/agents/research-synthesizer.js` — remove tags
- `harness/agents/specialist-planner.js` — remove tags
- `harness/agents/plan-synthesizer.js` — remove tags
- `harness/internal/server/swarm_dashboard.go` — path separator fix
- `harness/main.go` — `exec.LookPath` for linear-cli

### Phase 3: Post-processor agent + ticket context injection + workflow wiring

Phase 3 now includes the post-processor agent, ticket context injection (originally Phase 4), and wiring the currently-uncalled activities (`FetchLinearTicket`, `CreateLinearFollowup`, `SearchLinearIssues`) into workflows.

#### 3a. Post-processor agent

**New file**: `harness/agents/linear-context-processor.js`

This agent receives the artifact content, fresh ticket data, and produces:
1. A structured comment (Context / Learnings / Out of Scope sections)
2. A list of follow-up recommendations with relation types

```javascript
import { runAgent } from './lib/agent-factory.js';

await runAgent({
  withFileTools: true,
  withSearchContext: true,
  systemPrompt: `You are a Linear context processor. Your job is to analyze completed work artifacts and produce structured context for the Linear ticket.

CRITICAL — You are a validator, not a performer:
- DO NOT suggest additional work to do on the current ticket
- DO NOT critique the quality of the research or plan
- ONLY extract key context, validate scope claims, and identify genuine follow-ups

Guidelines:
- Read the artifact carefully and extract the most important findings/decisions
- Compare the artifact's scope against the original ticket description
- Validate "out of scope" claims: is the item truly out of scope for this ticket?
- For each validated out-of-scope item, judge if it warrants a follow-up research ticket
- Search existing Linear tickets before recommending a new one
- Use "blocked-by" relation if the follow-up is a prerequisite for the current ticket's next stage
- Use "relates-to" relation if the follow-up is tangential/independent

## Output Format

Write your output to the path specified in the task's outputPath field:

\`\`\`
---
comment: |
  ## Context
  - [Key finding 1]
  - [Key finding 2]

  ## Learnings
  - [Mistake or surprising discovery, if any]

  ## Out of Scope
  - [Validated out-of-scope item 1]
  - [Validated out-of-scope item 2]
followups:
  - title: "Research: [topic]"
    description: "During work on {ticketID}, [item] was identified as out of scope. Research whether this warrants its own ticket."
    relation: "relates-to"
  - title: "Research: [blocker topic]"
    description: "Prerequisite for {ticketID}: [why this blocks progress]"
    relation: "blocked-by"
---
\`\`\`

Only include sections with actual content. Omit empty sections.
Follow-ups list can be empty if nothing warrants a new ticket.`,
  prompt: (task) => `Process the completed artifact and produce Linear context.

Ticket: ${task.ticketID}
Ticket data:
${task.ticketData}

Artifact type: ${task.artifactType}
Artifact content:
${task.artifactContent}

Write your output to: ${task.outputPath}`,
});
```

#### 3b. New Go types

**`harness/internal/swarmorch/types.go`** — add:

```go
type LinearContextInput struct {
    TaskID          string `json:"taskID"`
    TicketID        string `json:"ticketID"`
    TicketData      string `json:"ticketData"`      // JSON from linear-cli i get
    ArtifactType    string `json:"artifactType"`     // "research_doc" or "plan_doc"
    ArtifactContent string `json:"artifactContent"`  // the full artifact text
    OutputPath      string `json:"outputPath"`
    RepoRoot        string `json:"repoRoot"`
}

type LinearContextOutput struct {
    Comment   string           `json:"comment"`
    Followups []FollowupTicket `json:"followups"`
}

type FollowupTicket struct {
    Title       string `json:"title"`
    Description string `json:"description"`
    Relation    string `json:"relation"` // "blocked-by" or "relates-to"
}
```

#### 3c. New activity + validation

**`harness/internal/swarmorch/activities.go`** — add:

```go
func (a *SwarmActivities) RunLinearContextProcessor(ctx context.Context, input LinearContextInput) (LinearContextOutput, error)
```

Uses the same `runAgentActivity[LinearContextOutput]` pattern as other agents. Agent tier activity options (20min timeout).

**`harness/internal/swarmorch/artifact.go`** — add validation:

```go
func validateLinearContextOutput(a LinearContextOutput) error {
    if a.Comment == "" {
        return errors.New("must produce a comment")
    }
    validRelations := map[string]bool{"blocked-by": true, "relates-to": true}
    for i, f := range a.Followups {
        if f.Title == "" {
            return fmt.Errorf("followup[%d] missing title", i)
        }
        if !validRelations[f.Relation] {
            return fmt.Errorf("followup[%d] invalid relation %q", i, f.Relation)
        }
    }
    return nil
}
```

#### 3d. Ticket context injection into agents

The `FetchLinearTicket` activity returns the ticket data. Format it as markdown context and inject alongside `projectContext` into agent inputs.

**`harness/internal/swarmorch/types.go`** — add optional field to agent inputs that receive external context:

```go
// Add to GenerateQuestionsInput, ResearchAgentInput, ClassifyInput, SpecialistInput
TicketContext string `json:"ticketContext,omitempty"`
```

Agent system prompts reference it (append to system prompt when non-empty):
```
If ticket context is provided, use it to understand the original problem statement,
any human comments with steering instructions, and linked research/plans.
```

#### 3e. Wire into workflows — complete Linear lifecycle

Replace the simplified Phase 4 wiring with the full lifecycle including `FetchLinearTicket`, post-processor, and follow-up creation.

**Research workflow** (standalone mode only — skip when `isChild`):

```
[existing] Create workflow span, set task to running
[NEW]      FetchLinearTicket → ticket data for context injection
[existing] UpdateLinearStatus("In Progress")     — already wired
[existing] UpdateLinearLabels(["type:research", "swarm:research"]) — already wired
[existing] runResearchSteps() — question generation, parallel research, synthesis
           (inject ticketContext into GenerateQuestionsInput + ResearchAgentInput)
[existing] WriteDocument + PersistArtifact
[NEW]      FetchLinearTicket → re-read for latest comments
[NEW]      RunLinearContextProcessor → structured comment + followups
[UPDATE]   AddLinearComment — use post-processor comment instead of hardcoded template
[existing] LinkArtifactToLinear("Research Doc", artifactURL) — already wired
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup if not duplicate
[existing] UpdateLinearLabels(["type:research"]) — already wired
[existing] UpdateLinearStatus("Done") — already wired
[existing] Set task status to completed
```

**Code change plan workflow**:

```
[existing] Create workflow span, set task to running
[NEW]      FetchLinearTicket → ticket data for context injection
[existing] UpdateLinearStatus("In Progress") — already wired
[existing] UpdateLinearLabels(["type:code-change", "swarm:research"]) — already wired
[existing] Execute ResearchWorkflow as child (no Linear ops — child mode)
[NEW]      FetchLinearTicket → re-read for latest comments
[NEW]      RunLinearContextProcessor → comment + followups for research
[UPDATE]   AddLinearComment — use post-processor comment
[existing] LinkArtifactToLinear("Research Doc", researchArtifactURL) — already wired (fix from 2.5a)
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup
[existing] UpdateLinearLabels(["type:code-change", "swarm:planning"]) — already wired
[existing] Planning stages — classify, specialist planners, synthesize
           (inject ticketContext into ClassifyInput + SpecialistInput)
[existing] WriteDocument + PersistArtifact
[NEW]      FetchLinearTicket → re-read again
[NEW]      RunLinearContextProcessor → comment + followups for plan
[UPDATE]   AddLinearComment — use post-processor comment
[existing] LinkArtifactToLinear("Implementation Plan", planArtifactURL) — already wired (fix from 2.5a)
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup
[existing] UpdateLinearLabels(["type:code-change"]) — already wired
[existing] UpdateLinearStatus("In Review") — already wired
[existing] Set task status to completed
```

#### Phase 3 files changed

- `harness/agents/linear-context-processor.js` — NEW: post-processor agent
- `harness/internal/swarmorch/types.go` — LinearContextInput/Output, FollowupTicket, TicketContext on agent inputs
- `harness/internal/swarmorch/activities.go` — RunLinearContextProcessor activity
- `harness/internal/swarmorch/artifact.go` — validateLinearContextOutput
- `harness/internal/swarmorch/workflows.go` — FetchLinearTicket calls, post-processor calls, follow-up creation loops, ticket context injection

## Verification

### Automated
- [ ] Go compiles: `just vps-build` from `harness/`
- [ ] All agent scripts parse (no JS syntax errors)
- [ ] Linear labels created: `linear-cli labels list --type issue --output json`

### Manual — Phase 0 (prompt improvements)
- [ ] Start a research workflow: output contains no improvement suggestions in findings
- [ ] Start a code_change_plan workflow: specialist plans have `automatedVerification`/`manualVerification`, final plan has "What We're NOT Doing" section

### Manual — Phase 2.5 (review fixes)
- [ ] `artifactURL` no longer panics: start a workflow with `ticketID` and verify artifact URL is constructed correctly
- [ ] `linear-cli` found via PATH: restart harness with `LINEAR_CLI` unset, verify it still finds the binary
- [ ] Tags removed: start a workflow and verify agent output no longer includes `tags:` in frontmatter

### Manual — Phase 3 (post-processor + ticket context + full wiring)
- [ ] Start a research task with `ticket_id`: verify Linear status changes to In Progress then Done, labels swap, post-processor comment posted, artifact linked
- [ ] Start a code_change_plan task with `ticket_id`: verify full lifecycle — post-processor research comment, post-processor plan comment, status ends at "In Review"
- [ ] Start a task WITHOUT `ticket_id`: verify no Linear errors, all activities no-op gracefully
- [ ] Post-processor creates follow-up ticket: verify search dedup works, relation created
- [ ] Artifact URL works: click link in Linear → see rendered markdown
- [ ] Ticket context injection: verify agents receive ticket description/comments when `ticketID` is provided

## Files Changed

### New files
- `harness/internal/linear/cli.go` — CLI wrapper
- `harness/internal/linear/types.go` — Linear types
- `harness/agents/linear-context-processor.js` — post-processor agent
- `harness/scripts/setup-linear.sh` — label creation script

### Modified files (Phase 0 — prompt improvements)
- `harness/agents/research-agent.js` — documentarian CRITICAL block
- `harness/agents/research-questions.js` — factual questions constraint
- `harness/agents/specialist-planner.js` — verification split + scope exclusions
- `harness/agents/plan-synthesizer.js` — preserve verification split + "What We're NOT Doing"
- `harness/agents/research-synthesizer.js`
- `harness/agents/lib/prompts.js` — thinking step in selfReflection()
- `harness/internal/swarmorch/types.go` — PlannerOutput verification split
- `harness/internal/swarmorch/artifact.go` — validation + yamlMultiKeyRe update

### Modified files (Phase 2.5 — review fixes)
- `harness/internal/swarmorch/workflows.go` — fix `artifactURL` nil deref, add `HarnessURL` to input structs
- `harness/internal/swarmorch/manager.go` — thread `HarnessURL` into workflow inputs
- `harness/agents/research-agent.js` — remove tags
- `harness/agents/research-synthesizer.js` — remove tags
- `harness/agents/specialist-planner.js` — remove tags
- `harness/agents/plan-synthesizer.js` — remove tags
- `harness/internal/server/swarm_dashboard.go` — path separator fix
- `harness/main.go` — `exec.LookPath` for linear-cli

### Modified files (Phase 3 — post-processor + wiring)
- `harness/internal/swarmorch/types.go` — LinearContextInput/Output, FollowupTicket, TicketContext on agent inputs
- `harness/internal/swarmorch/activities.go` — RunLinearContextProcessor activity
- `harness/internal/swarmorch/artifact.go` — validateLinearContextOutput
- `harness/internal/swarmorch/workflows.go` — FetchLinearTicket, post-processor, follow-ups, ticket context injection

### Modified files (Phases 1-6 — Linear integration, already committed)
- `harness/internal/swarmorch/activities.go` — Linear activities + LinearClient on struct
- `harness/internal/swarmorch/workflows.go` — Linear activities at stage boundaries
- `harness/internal/server/swarm_api.go` — accept ticket_id in request
- `harness/internal/server/swarm_dashboard.go` — ticket_id signal, artifact view route
- `harness/main.go` — initialize Linear client

## Dependencies

- `linear-cli` binary (found via `exec.LookPath` or `LINEAR_CLI` env var)
- `LINEAR_API_KEY` in `.env` (already present)
- `LINEAR_TEAM_KEY=CRE` in `.env` (already present)
- `HARNESS_URL` in `.env` (for absolute artifact URLs in Linear)
- Workspace `creative-mode` configured (already done)

## References

- Staff eng review: `thoughts/CoreyCole/reviews/2026-03-09_00-22-17_linear-integration-plan_review.md`
- HumanLayer Linear integration research: `thoughts/CoreyCole/research/2026-03-08_22-02-53_humanlayer-linear-context-threading.md`
- Agent primitives flowchart: `thoughts/swarm/agent-primitives-flowchart.html`
- Prior swarm prompt improvements plan: `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md`
- linear-cli skills: `.agents/skills/linear-*/SKILL.md`
