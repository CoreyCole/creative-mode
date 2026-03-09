---
question: What Linear and GitHub integration capabilities already exist in the codebase (client wrappers, auth/config, ticket/linking operations, and any PR-related helpers), and how are these integrations currently invoked from workflows or activities?
confidence: high
filesReferenced:
  - harness/internal/linear/cli.go
  - harness/internal/linear/types.go
  - harness/internal/swarmorch/activities.go
  - harness/internal/swarmorch/workflows.go
  - harness/internal/server/swarm_api.go
  - harness/main.go
  - harness/internal/swarmorch/manager.go
  - harness/internal/swarmorch/types.go
  - harness/internal/db/migrations/006_swarm_tables.sql
  - harness/internal/db/queries/swarm.sql
---

### Linear integration: existing capabilities

- `harness/internal/linear/cli.go:12` defines a dedicated `linear.Client` wrapper around the `linear-cli` binary (exec-based integration, not direct HTTP SDK).
- `harness/internal/linear/cli.go:19` + `harness/main.go:504` show client initialization is config/env-driven:
  - binary from `LINEAR_CLI` or `exec.LookPath("linear-cli")`
  - team key from `LINEAR_TEAM_KEY`
  - integration disabled when either is unavailable (`nil` client path).
- `harness/internal/linear/cli.go:30` + `:47` implement command execution and JSON unmarshalling helpers (`run`, generic `runJSON[T]`) with `--output json --compact --quiet` for structured reads.

Implemented Linear operations in the wrapper:

- Read/fetch:
  - `GetIssue` (`cli.go:63`)
  - `SearchIssues` (`cli.go:154`)
  - `ListRelations` (`cli.go:184`)
- Mutations:
  - `UpdateStatus` (`cli.go:72`)
  - `UpdateLabels` (`cli.go:78`)
  - `AddComment` (`cli.go:95`)
  - `CreateAttachment` (`cli.go:101`)
  - `CreateIssue` (`cli.go:122`) with `CreateOpts` (team/priority/labels/state/description)
  - `AddRelation` (`cli.go:167`) with relation types `blocks|blocked-by|related`

Data shapes used by workflows/activities:

- `harness/internal/linear/types.go:4` `Issue` includes identifier/title/description/url/state/labels/priority.
- `types.go:35` `Relation`, `types.go:41` `CreateOpts`, `types.go:50` `SearchResult`.

### Swarm/Temporal invocation path for Linear

- `harness/main.go:336-354` wires one shared `linearClient` into both:
  - `SwarmManager` (workflow/activity layer)
  - HTTP `Server` (API entrypoints).
- `harness/internal/swarmorch/manager.go:67` injects `linearClient` into `SwarmActivities.LinearClient`.
- `harness/internal/swarmorch/activities.go:436` section provides Temporal activities that bridge workflows → `linear.Client`:
  - `FetchLinearTicket` (`:440`)
  - `UpdateLinearStatus` (`:455`)
  - `UpdateLinearLabels` (`:467`)
  - `AddLinearComment` (`:479`)
  - `LinkArtifactToLinear` (`:491`)
  - `CreateLinearFollowup` (`:504`) (create issue + add relation)
  - `SearchLinearIssues` (`:557`)
- These activities uniformly no-op when `LinearClient == nil` or ticket ID missing where applicable (`activities.go:437`, and per-method guards).

### Workflow behaviors using Linear

Shared helpers in `harness/internal/swarmorch/workflows.go`:

- `runLinearActivity` (`:233`) executes Linear activities as non-fatal fire-and-forget (warn on failure, workflow continues).
- `fetchTicketContext` (`:248`) calls `FetchLinearTicket` and transforms to prompt context.
- `formatTicketContext` (`:274`) injects ticket identifier/status/labels/description into markdown context string for agents.
- `artifactURL` (`:225`) builds `/swarm/artifacts/{id}/view` URLs for Linear attachments.
- `runPostProcessor` (`:294`) runs `RunLinearContextProcessor` agent, posts generated comment, dedup-searches for followups, creates/relates followup tickets.

Research workflow usage:

- At start (standalone mode), sets ticket state/labels:
  - `UpdateLinearStatus("In Progress")` + `UpdateLinearLabels(["type:research","swarm:research"])` (`workflows.go:628-633`).
- Pulls ticket context into agent inputs when `LinearIssueID` present (`:623-624`).
- At completion:
  - post-processor comment/followups (`:686-694`)
  - artifact link attachment via `LinkArtifactToLinear` (`:697-698`)
  - labels reduced to `type:research` (`:700-704`)
  - status to `Done` (`:706`).

Code-change-plan workflow usage:

- Start:
  - fetch ticket context (`:776-777`)
  - set status `In Progress` + labels `type:code-change, swarm:research` (`:780-785`).
- After research child workflow:
  - post-processor run for research artifact and transition labels to `swarm:planning` (`:811-824`).
- Final completion:
  - post-processor run for plan doc (`:961-969`)
  - attach implementation plan artifact URL (`:972-973`)
  - labels reduced to `type:code-change` (`:975-979`)
  - status set `In Review` (`:981`).

### API-layer Linear invocation (task creation)

- `harness/internal/server/swarm_api.go:43` auto-creates a Linear ticket when request has no `ticketID` and `Server.LinearClient` exists.
- Auto-created ticket uses:
  - title = `requestText`
  - `CreateOpts{Labels: labelsForPrimitive(...), State: "In Progress"}` (`swarm_api.go:46-53`).
- Primitive label mapping lives in `labelsForPrimitive` (`swarm_api.go:318`):
  - research → `type:research`, `swarm:research`
  - code plan → `type:code-change`, `swarm:planning`.

### Persistence/linking of ticket IDs in swarm records

- Schema includes `linear_issue_id` on `swarm_tasks` (`harness/internal/db/migrations/006_swarm_tables.sql:9`).
- SQL query layer reads/writes this column (`harness/internal/db/queries/swarm.sql:4,14,18,22`).
- API task creation stores provided/auto-created ticket into this field (`swarm_api.go:74`).
- Swarm manager start methods propagate `ticketID` into workflow inputs and DB task creation params (`harness/internal/swarmorch/manager.go:113,148`).

### Linear context processor integration

- Swarm includes a dedicated post-processing artifact type for ticket updates:
  - input/output structs in `harness/internal/swarmorch/types.go:110-120`
  - activity wrapper `RunLinearContextProcessor` in `activities.go:533`
  - workflow hook `runPostProcessor` in `workflows.go:294`.
- This path is used after research and plan stages to generate structured comments and follow-up ticket suggestions, then invoke Linear comment/search/create/relation operations.

### GitHub integration status in current codebase

- Across investigated source paths, there is no GitHub client wrapper analogous to `internal/linear`, no GitHub auth/config env wiring in `main.go`, and no workflow/activity calls targeting GitHub APIs.
- No PR-specific helper functions were found in the inspected integration/workflow entrypoints (no activity methods or server start handlers that create/link PRs).
- Current swarm external ticketing/linking operations are Linear-focused (status, labels, comments, attachments, follow-up issue relations).
