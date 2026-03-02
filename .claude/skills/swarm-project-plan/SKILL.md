---
name: swarm-project-plan
description: Project planning phase — read research findings and decompose into child tickets with dependency graph, milestones, and Graphite stack plan.
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
---

# Swarm Project Plan Skill

Creates or revises a project plan that decomposes a project ticket into child tickets (research + code changes) with a dependency graph, milestones, and Graphite stack plan.

## Preamble

1. If `$CM_SWARM_TICKET_DESCRIPTION_PATH` is set, read it for the project's instructions and context (the project charter/description from the parent Linear ticket).
2. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document. If this is a revision (attempt > 1), the handoff contains project-review feedback that MUST be addressed.
3. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
4. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `project_plan` |
| `CM_SWARM_ATTEMPT` | Attempt number (1 = first plan, 2+ = revision) |
| `CM_SWARM_HANDOFF_PATH` | Previous handoff (research or project-review) |
| `CM_SWARM_AGGREGATED_RESEARCH_PATH` | Aggregated research from decompose children (if available) |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_TICKET_DESCRIPTION_PATH` | Project instructions/charter file (if project workflow) |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read research** — Find and read research documents for this ticket:
   - If `$CM_SWARM_AGGREGATED_RESEARCH_PATH` is set, read it — this contains aggregated findings from all child research workflows spawned during the decompose phase. This is the primary research input.
   - Also glob: `thoughts/swarm/research/*_{ticketID}_*.md` for the initial research
   - If revision (attempt > 1), also read the previous project plan and the project-review handoff

2. **Read review feedback** (revision only) — The handoff from `project_review` contains:
   - Specific issues to address
   - Suggestions for improvement
   - Items that were approved (keep these)

3. **Analyze scope** — From the ticket and research findings, identify:
   - What research questions remain unanswered (→ research child tickets)
   - What code changes are needed (→ code child tickets)
   - What dependencies exist between changes
   - What can run in parallel vs must be sequential

4. **Create project plan** — Write to `thoughts/swarm/project-plans/{timestamp}_{ticketID}_{slug}_v{N}.md`:

   ```markdown
   ---
   ticket: {ticketID}
   workflow: {workflowID}
   session: {sessionID}
   version: {N}
   timestamp: {ISO 8601}
   previous_version: {path to v{N-1} if revision}
   ---

   # Project Plan: {ticket title} (v{N})

   ## Scope
   {What this project achieves — ties back to ticket goals}

   ## Ticket Decomposition

   | # | Type | Title | Dependencies | Notes |
   |---|------|-------|--------------|-------|
   | 1 | research | {title} | none | Foundation research |
   | 2 | research | {title} | none | Can run parallel with #1 |
   | 3 | code | {title} | 1, 2 | Needs both research results |
   | 4 | code | {title} | 3 | Sequential dependency |
   | 5 | code | {title} | 3 | Can run parallel with #4 |

   ## Dependency Graph

   ```mermaid
   graph TD
       T1[#1 Research: title] --> T3[#3 Code: title]
       T2[#2 Research: title] --> T3
       T3 --> T4[#4 Code: title]
       T3 --> T5[#5 Code: title]
   ```

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

   - [ ] M1: {name} — {measurable criteria}
   - [ ] M2: {name} — {measurable criteria}
   - [ ] M3: {name} — {measurable criteria}
   - [ ] M4: Final verification — `just check` passes with all changes

   ## Graphite Stack Plan

   Branch stack order (bottom to top):
   1. `swarm/{projectID}/research-{slug1}` (ticket #1)
   2. `swarm/{projectID}/research-{slug2}` (ticket #2)
   3. `swarm/{projectID}/{slug3}` (ticket #3)
   4. `swarm/{projectID}/{slug4}` (ticket #4, stacked on #3)
   5. `swarm/{projectID}/{slug5}` (ticket #5, stacked on #3)

   ## Risks
   - {Risk}: {Mitigation}

   ## Revision Notes (v2+ only)
   - Addressed: {review feedback item}
   - Changed: {what was modified from previous version}
   ```

5. **Post Linear comment** — `PROJECT-PLAN:` prefix:
   ```
   PROJECT-PLAN: v{N} — {one-line summary}
   Doc: thoughts/swarm/project-plans/{filename}
   Tickets: {count} ({research count} research, {code count} code)
   Waves: {count} execution waves
   Milestones: {count}
   ```

6. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-project/`.

7. **Write RESULT**:
   ```
   RESULT: success
   Phase: project_plan
   Handoff: thoughts/swarm/handoffs-project/{filename}

   Summary: Created project plan v{N} with {ticket count} child tickets in {wave count} waves
   ```

## Decomposition Guidelines

- **Right-size tickets**: Each code ticket should be completable in a single swarm code workflow (1-3 files, clear scope)
- **Research first**: If a code change depends on understanding something, create a research ticket for it
- **Minimize dependencies**: Prefer parallel waves. Only add dependency edges when there's a real code dependency
- **One concern per ticket**: Don't combine unrelated changes in one ticket
- **Include verification**: The final milestone should always include `just check` and relevant tests

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still perform all reads and analysis

## Error Handling

- If research document not found → `RESULT: logic_failure` (research phase must run first)
- If ticket requirements are too vague to decompose → note in Risks, create a single research ticket as first child
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
