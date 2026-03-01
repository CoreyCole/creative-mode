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

Linear API: 1500 req/hr. Batch sequentially. `linear-cli` handles 429 retry.

## Error Handling

- Exit 3 → stop immediately
- Exit 4 → wait 60s, retry once
- Mid-execution failure → write RESULT comment on ticket, keep In Progress status
