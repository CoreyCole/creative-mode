---
name: swarm-conventions
description: Reference for swarm agent conventions — labels, ticket format, comment format, doc templates. Not an action primitive. Load when creating/updating swarm-tracked tickets.
allowed-tools: Bash, Read
---

# Swarm Conventions

Reference document for all swarm agent naming, labeling, comment, and document conventions.

## Linear Team

Team: `CM`

## Labels

| Label | Color | Purpose |
|-------|-------|---------|
| `swarm:research` | #3B82F6 | Research workflows |
| `swarm:code` | #10B981 | Code change workflows |
| `swarm:verification` | #EAB308 | Verification phase |
| `swarm:project` | #8B5CF6 | Project workflows |
| `swarm:plan` | #F97316 | Planning phase |
| `swarm:orchestration` | #EF4444 | Orchestration/infra |
| `type:bug` | #DC2626 | Bug fix |
| `type:feature` | #2563EB | New feature |
| `type:refactor` | #7C3AED | Refactoring |
| `type:prototype` | #059669 | Prototype/spike |

## Lifecycle States

Triage → Backlog → Todo → In Progress → In Review → Done

## Ticket Footer

Every swarm-tracked ticket includes a structured YAML footer in the description:

```yaml
---
swarm_type: code|research|project
parent_ticket: CM-XXX (if child of project)
research_path: thoughts/swarm/research/...
plan_path: thoughts/swarm/plans/...
pr_url: https://github.com/...
previous_attempt: CM-XXX (if restart)
dependencies: [CM-XXX, CM-YYY]
```

## Comment Prefixes

All structured comments use a prefix for machine parsing:

| Prefix | Phase | Content |
|--------|-------|---------|
| `RESEARCH:` | research | Research summary + doc path |
| `PLAN:` | code_plan | Plan summary + doc path |
| `PLAN-REVIEW:` | plan_review | Verdict (approve/revise) + feedback |
| `IMPL:` | implement | Implementation summary + files changed |
| `VERIFY:` | verify | Verification result + errors |
| `PR:` | pr | PR URL + summary |
| `REVISION:` | code_plan (retry) | Revision notes referencing review |
| `RESTART:` | any | Full restart context |
| `PROJECT-PLAN:` | project_plan | Project plan summary + doc path |
| `PROJECT-REVIEW:` | project_review | Verdict (approve/revise) + feedback |
| `PROJECT-VERIFY:` | project_verify | Milestone verification results |
| `HEARTBEAT:` | heartbeat | Stall detection / status check |
| `RESUME:` | resume | Resume context after interruption |
| `TERMINAL_FAILURE:` | failed | Post-mortem summary |
| `RESULT:` | any | BLUF outcome for state machine |

## RESULT Comment Format

```
RESULT: success|logic_failure|infra_failure|timeout|context_limit
Phase: {current_phase}
Handoff: thoughts/swarm/handoffs-{type}/{timestamp}_{ticketID}_{detail}.md

Summary: {one-line summary of what happened}
```

## Document Paths

- Research: `thoughts/swarm/research/{timestamp}_{ticketID}_{slug}.md`
- Plans: `thoughts/swarm/plans/{timestamp}_{ticketID}_{slug}_v{N}.md`
- Reviews: `thoughts/swarm/handoffs-plan-reviews/{timestamp}_{ticketID}_{verdict}.md`
- Handoffs: `thoughts/swarm/handoffs-{phase}/{timestamp}_{ticketID}_{detail}.md`
- Project Plans: `thoughts/swarm/project-plans/{timestamp}_{ticketID}_{slug}_v{N}.md`
- Project Reviews: `thoughts/swarm/handoffs-project-reviews/{timestamp}_{ticketID}_{verdict}.md`
- Retrospectives: `thoughts/swarm/retrospectives/{timestamp}_{ticketID}_{summary}.md`
- Digests: `thoughts/swarm/digests/{timestamp}_digest.md`

Timestamp format: `YYYY-MM-DD_HH-MM-SS`

## Plan Versioning

- v1: `{slug}.md`
- v2: `{slug}_v2.md`
- v3: `{slug}_v3.md`
- Review: `{slug}_review.md`, `{slug}_v2_review.md`

## Dry-Run Convention

All primitives accept `--dry-run`. When active:
- Print `[DRY-RUN]` prefix per action
- No Linear API mutations
- No file writes
- No git operations

## Rate Limits

Linear API: 1500 req/hr. Batch sequentially. Go client serializes mutations via mutex and retries on 429.

## Error Handling

- Exit 3 → stop immediately
- Exit 4 → wait 60s, retry once
- Mid-execution failure → write RESULT comment on ticket, keep In Progress status

## Handoff and Learning Context Preamble

Every skill session MUST begin with this 3-step preamble before doing any phase-specific work:

1. **Read handoff** — If `$CM_SWARM_HANDOFF_PATH` is set and the file exists, read it. This is the previous session's handoff document containing context, decisions, and gotchas.
2. **Read learning context** — If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set and the file exists, read it. This contains relevant learnings from past workflows (phase-specific, critical, and ticket-specific).
3. **Load conventions** — Reference this document (`swarm-conventions`) for comment formats, document paths, and naming conventions.

If neither env var is set, proceed normally — this is the first session for this ticket.

## Handoff Writing

As the **last act** before writing the RESULT comment, every skill MUST write a handoff document:

**Directory**: `thoughts/swarm/{HandoffDir(phase)}/` — see `HandoffDir` mapping:
- `research` → `handoffs-research`
- `code_plan`, `implement`, `pr` → `handoffs-code`
- `plan_review` → `handoffs-plan-reviews`
- `verify` → `handoffs-code-reviews`
- `project_plan`, `project_verify` → `handoffs-project`
- `project_review` → `handoffs-project-reviews`

**Filename**: `{YYYY-MM-DD_HH-MM-SS}_{ticketID}_{sanitized-detail}.md`

**Template**:
```markdown
---
ticket: {ticketID}
phase: {phase}
result: {success|logic_failure|...}
session: {sessionID}
workflow: {workflowID}
timestamp: {ISO 8601}
---

## BLUF
{One sentence: what happened and what the next session needs to know.}

## What Was Done
- {Bullet list of completed actions}

## What Was NOT Done
- {Bullet list of remaining work or deferred items}

## Key Files
- `path/to/file` — {why it matters}

## Gotchas
- {Non-obvious issues encountered, workarounds applied}

## Next Steps
- {Concrete next actions for the following phase/session}
```

## Swarm Environment Variables

| Variable | Set By | Purpose |
|----------|--------|---------|
| `CM_SWARM_TICKET_ID` | Orchestrator | Linear ticket identifier (e.g., `CM-123`) |
| `CM_SWARM_WORKFLOW_ID` | Orchestrator | Current workflow UUID |
| `CM_SWARM_SESSION_ID` | Orchestrator | Current session UUID |
| `CM_SWARM_PHASE` | Orchestrator | Current phase (e.g., `research`, `code_plan`) |
| `CM_SWARM_ATTEMPT` | Orchestrator | Current attempt number (1-based) |
| `CM_SWARM_HANDOFF_PATH` | Orchestrator | Path to previous session's handoff document |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Orchestrator | Path to file containing relevant learnings |
| `CM_SWARM_DRY_RUN` | User/Orchestrator | If `true`, no mutations (Linear, git, files) |
| `CM_SWARM_TICKET_URL` | Orchestrator | Linear ticket URL for comment posting |
| `CM_SWARM_BRANCH` | Orchestrator | Git branch name for this workflow |
| `CM_SWARM_PREVIOUS_WORKFLOW_ID` | Orchestrator | Previous workflow for restart context |
| `CM_SWARM_PREVIOUS_BRANCH` | Orchestrator | Previous workflow's git branch |
| `CM_SWARM_PREVIOUS_HANDOFF_PATH` | Orchestrator | Previous workflow's last handoff |
| `CM_SWARM_PREVIOUS_RESEARCH_PATH` | Orchestrator | Previous workflow's research doc |
| `CM_SWARM_STACK_PARENT` | Project Orchestrator | Parent branch for Graphite stacking |
| `CM_SWARM_STACK_ORDER` | Project Orchestrator | Position in stack for PR description |
