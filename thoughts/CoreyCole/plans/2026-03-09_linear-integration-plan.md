# Swarm Agent Improvements + Linear Integration Plan

## Overview

Two combined efforts:

1. **Agent prompt improvements** (Phase 0) — 5 HumanLayer-inspired changes to agent prompts and Go types that improve research quality, plan actionability, and output searchability. Ready to implement with exact code snippets.

2. **Linear integration** (Phases 1-6) — Add Linear tracking so Temporal workflows push status updates, labels, structured comments, artifact links, and follow-up tickets. All Linear operations use the `linear-cli` Rust binary (already installed) via `exec.CommandContext` — no Go SDK needed.

## Current State

- `linear-cli` is installed at `/home/deploy/.cargo/bin/linear-cli`, workspace `creative-mode` configured with `LINEAR_API_KEY`
- `swarm_tasks` table has `linear_issue_id` column but it's never populated (always NULL)
- Workflow inputs have `LinearIssueID string` field but it's always empty
- No labels exist in Linear. Default workflow states: Backlog, Todo, In Progress, In Review, Done, Canceled, Duplicate
- No Linear API calls anywhere in the codebase
- Agent prompts lack documentarian constraints, verification split, scope exclusions, tags, and a deeper thinking step

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

### Phase 3: Post-processor agent

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

**New types in `types.go`**:

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
    Comment   string             `json:"comment"`
    Followups []FollowupTicket   `json:"followups"`
}

type FollowupTicket struct {
    Title       string `json:"title"`
    Description string `json:"description"`
    Relation    string `json:"relation"` // "blocked-by" or "relates-to"
}
```

**New activity**:

```go
func (a *SwarmActivities) RunLinearContextProcessor(ctx context.Context, input LinearContextInput) (LinearContextOutput, error)
```

Uses the same `runAgentActivity[LinearContextOutput]` pattern as other agents. Agent tier activity options (20min timeout).

**New validation**:

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

### Phase 4: Wire into workflows

**Modified file**: `harness/internal/swarmorch/workflows.go`

#### Accept ticket_id from API

**Modified file**: `harness/internal/server/swarm_api.go`

Update the request struct to accept `ticket_id`:

```go
type swarmTaskRequest struct {
    RequestText   string `json:"request_text"`
    TicketID      string `json:"ticket_id"`       // Linear identifier, e.g. "CRE-15"
}
```

Pass `TicketID` through to `CreateSwarmTaskParams.LinearIssueID` and workflow inputs.

**Modified file**: `harness/internal/server/swarm_dashboard.go`

Add `ticket_id` signal to the dashboard start form so tickets can be linked from the UI.

#### Research workflow Linear integration

Insert Linear activities into `ResearchWorkflow` (standalone mode only — skip when running as child workflow):

```
[existing] Create workflow span, set task to running
[NEW]      FetchLinearTicket → snapshot + inject into agent context
[NEW]      UpdateLinearStatus("In Progress")
[NEW]      UpdateLinearLabels(["type:research", "swarm:research"])
[existing] runResearchSteps() — question generation, parallel research, synthesis
[existing] WriteDocument + PersistArtifact
[NEW]      FetchLinearTicket → re-read for latest comments
[NEW]      RunLinearContextProcessor → structured comment + followups
[NEW]      AddLinearComment(comment)
[NEW]      LinkArtifactToLinear("Research Doc", artifactURL)
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup if not duplicate
[NEW]      UpdateLinearLabels(["type:research"]) — remove swarm:research
[NEW]      UpdateLinearStatus("Done")
[existing] Set task status to completed
```

#### Code change plan workflow Linear integration

Insert Linear activities into `CodeChangePlanWorkflow`:

```
[existing] Create workflow span, set task to running
[NEW]      FetchLinearTicket → snapshot
[NEW]      UpdateLinearStatus("In Progress")
[NEW]      UpdateLinearLabels(["type:code-change", "swarm:research"])
[existing] Execute ResearchWorkflow as child (no Linear ops — child mode)
[NEW]      FetchLinearTicket → re-read for latest comments
[NEW]      RunLinearContextProcessor → comment + followups for research
[NEW]      AddLinearComment(comment)
[NEW]      LinkArtifactToLinear("Research Doc", researchArtifactURL)
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup
[NEW]      UpdateLinearLabels(["type:code-change", "swarm:planning"])
[existing] Planning stages — classify, specialist planners, synthesize
[existing] WriteDocument + PersistArtifact
[NEW]      FetchLinearTicket → re-read again
[NEW]      RunLinearContextProcessor → comment + followups for plan
[NEW]      AddLinearComment(comment)
[NEW]      LinkArtifactToLinear("Implementation Plan", planArtifactURL)
[NEW]      For each followup: SearchLinearIssues, CreateLinearFollowup
[NEW]      UpdateLinearLabels(["type:code-change"]) — remove swarm:planning
[NEW]      UpdateLinearStatus("In Review") — plan ready for human review
[existing] Set task status to completed
```

#### Ticket context injection into agents

The `FetchLinearTicket` activity returns the ticket markdown snapshot. This gets passed alongside `projectContext` to agent inputs. Modify agent input types to include optional `ticketContext`:

```go
// Add to ResearchAgentInput, SpecialistInput, etc.
TicketContext string `json:"ticketContext,omitempty"`
```

Agent system prompts reference it:
```
If ticket context is provided, use it to understand the original problem statement,
any human comments with steering instructions, and linked research/plans.
```

### Phase 5: Artifact URL generation

Artifacts are written to `thoughts/swarm/research/...` and `thoughts/swarm/project-plans/...`. To link them in Linear, we need a URL. Options:

1. **GitHub URL** — if `thoughts/` is committed and pushed, use `https://github.com/org/repo/blob/main/thoughts/swarm/...`
2. **Harness URL** — serve artifact files via a new route `GET /swarm/artifacts/:id/view`
3. **Relative path** — just store the file path as the attachment title (no clickable link)

Recommend option 2 — add a simple handler that reads the artifact file path from DB and serves it. This keeps artifacts accessible even before they're committed to git.

**New route**: `GET /swarm/artifacts/:id/view` — looks up `swarm_artifacts.file_path`, reads and serves the markdown file.

The URL passed to `LinkArtifactToLinear` would be: `{HARNESS_URL}/swarm/artifacts/{artifactID}/view`

### Phase 6: Manager initialization

**Modified file**: `harness/main.go`

Initialize the Linear client in `initSwarmManager()`:

```go
linearClient := linear.NewClient(
    "/home/deploy/.cargo/bin/linear-cli",  // or discover via which
    os.Getenv("LINEAR_TEAM_KEY"),          // "CRE"
)
```

Pass to `SwarmActivities`. The client is nil-safe — all activities no-op when client is nil or ticket ID is empty.

## Verification

### Automated
- [ ] Go compiles: `cd harness && go build ./...`
- [ ] All 7 agent scripts parse (no JS syntax errors)
- [ ] Linear labels created: `linear-cli labels list --type issue --output json`

### Manual — Phase 0 (prompt improvements)
- [ ] Start a research workflow: output contains `tags:` in frontmatter, no improvement suggestions in findings
- [ ] Start a code_change_plan workflow: specialist plans have `automatedVerification`/`manualVerification`, final plan has "What We're NOT Doing" section

### Manual — Phases 1-6 (Linear integration)
- [ ] Start a research task with `ticket_id`: verify Linear status changes to In Progress then Done, labels swap, comment posted, artifact linked
- [ ] Start a code_change_plan task with `ticket_id`: verify full lifecycle — research comment, plan comment, status ends at "In Review"
- [ ] Start a task WITHOUT `ticket_id`: verify no Linear errors, all activities no-op gracefully
- [ ] Post-processor creates follow-up ticket: verify search dedup works, relation created
- [ ] Artifact URL works: click link in Linear → see rendered markdown

## Files Changed

### New files
- `harness/internal/linear/cli.go` — CLI wrapper
- `harness/internal/linear/types.go` — Linear types
- `harness/agents/linear-context-processor.js` — post-processor agent
- `harness/scripts/setup-linear.sh` — label creation script

### Modified files (Phase 0 — prompt improvements)
- `harness/agents/research-agent.js` — documentarian CRITICAL block + tags
- `harness/agents/research-questions.js` — factual questions constraint
- `harness/agents/specialist-planner.js` — verification split + tags + scope exclusions
- `harness/agents/plan-synthesizer.js` — preserve verification split + "What We're NOT Doing" + tags
- `harness/agents/research-synthesizer.js` — tags
- `harness/agents/lib/prompts.js` — thinking step in selfReflection()
- `harness/internal/swarmorch/types.go` — PlannerOutput verification split
- `harness/internal/swarmorch/artifact.go` — validation + yamlMultiKeyRe update

### Modified files (Phases 1-6 — Linear integration)
- `harness/internal/swarmorch/activities.go` — new Linear activities + LinearClient on struct
- `harness/internal/swarmorch/types.go` — LinearContextInput/Output, FollowupTicket, ticketContext on agent inputs
- `harness/internal/swarmorch/artifact.go` — validation for LinearContextOutput
- `harness/internal/swarmorch/workflows.go` — insert Linear activities at stage boundaries
- `harness/internal/server/swarm_api.go` — accept ticket_id in request, pass to workflow input
- `harness/internal/server/swarm_dashboard.go` — ticket_id signal in start form, artifact view route
- `harness/main.go` — initialize Linear client, pass to SwarmActivities

## Dependencies

- `linear-cli` binary at `/home/deploy/.cargo/bin/linear-cli`
- `LINEAR_API_KEY` in `.env` (already present)
- `LINEAR_TEAM_KEY=CRE` in `.env` (already present)
- Workspace `creative-mode` configured (already done)

## References

- HumanLayer Linear integration research: `thoughts/CoreyCole/research/2026-03-08_22-02-53_humanlayer-linear-context-threading.md`
- Agent primitives flowchart: `thoughts/swarm/agent-primitives-flowchart.html`
- Prior swarm prompt improvements plan: `thoughts/CoreyCole/plans/2026-03-09_04-59-36_swarm-prompt-humanlayer-improvements.md`
- linear-cli skills: `.agents/skills/linear-*/SKILL.md`
