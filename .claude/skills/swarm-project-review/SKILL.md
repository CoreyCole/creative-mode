---
name: swarm-project-review
description: Project plan review phase — review a project plan against a checklist, verdict approve or revise. Read-only — does NOT modify the plan.
allowed-tools: Bash, Read, Glob, Grep, Agent
---

# Swarm Project Review Skill

Reviews a project plan and renders a verdict: approve (advance to child ticket creation) or revise (loop back to project_plan).

**This skill is READ-ONLY** — it does NOT modify the plan document.

## Preamble

1. If `$CM_SWARM_TICKET_DESCRIPTION_PATH` is set, read it for the project's instructions and context (the project charter/description from the parent Linear ticket).
2. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document for context from the planning phase.
3. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
4. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `project_review` |
| `CM_SWARM_ATTEMPT` | Current attempt (matches plan version) |
| `CM_SWARM_HANDOFF_PATH` | Handoff from project_plan phase |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_TICKET_DESCRIPTION_PATH` | Project instructions/charter file (if project workflow) |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Find the project plan** — Glob: `thoughts/swarm/project-plans/*_{ticketID}_*_v{attempt}.md`
   - If not found, try without version suffix (v1 is the default)
   - Read the full plan document

2. **Read the ticket** — Understand the project's goals and acceptance criteria.

3. **Verify ticket decomposition against codebase** — For each child ticket in the decomposition:
   - If Type=research: verify the research questions are meaningful and answerable
   - If Type=code: verify the referenced files/modules exist (or the creation makes sense), check that code dependencies align with declared ticket dependencies

4. **Validate dependency graph** — Check that:
   - Dependencies are acyclic (no circular deps)
   - All referenced ticket numbers exist in the decomposition table
   - Parallel waves are truly independent (no hidden code dependencies)
   - Sequential dependencies reflect actual code/data flow

5. **Review checklist** — Evaluate each criterion:

   | # | Criterion | Weight |
   |---|-----------|--------|
   | 1 | **Scope alignment** — Does the decomposition cover the ticket's goals? | Critical |
   | 2 | **Ticket granularity** — Are tickets right-sized? No mega-tickets or trivial tickets? | High |
   | 3 | **Dependency accuracy** — Do declared dependencies match actual code dependencies? | Critical |
   | 4 | **Execution ordering** — Can parallel waves truly run concurrently? | High |
   | 5 | **Milestone coverage** — Do milestones cover all major deliverables? Are criteria measurable? | Medium |
   | 6 | **Risk assessment** — Are integration risks identified with mitigations? | Medium |
   | 7 | **Convention compliance** — Follows swarm naming, paths, footer format? | Medium |

6. **Render verdict**:
   - **Approve** — All Critical criteria pass, no High criteria have blocking issues
   - **Revise** — Any Critical criterion fails, or multiple High criteria have issues

7. **Post Linear comment** — `PROJECT-REVIEW:` prefix:
   ```
   PROJECT-REVIEW: {APPROVE|REVISE} — v{N}

   Checklist:
   - [x] Scope alignment: {brief note}
   - [x] Ticket granularity: {brief note}
   - [x] Dependency accuracy: {brief note}
   - [x] Execution ordering: {brief note}
   - [ ] Milestone coverage: {issue found}
   ...

   {If REVISE: specific feedback for the planner}
   ```

8. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-project-reviews/`:
   - If APPROVE: Next Steps should reference child ticket creation and Wave 1 scheduling
   - If REVISE: What Was NOT Done should list specific issues; Next Steps should list what the planner needs to fix

9. **Write RESULT**:
   - Approve: `RESULT: success` (state machine advances — orchestrator creates child tickets)
   - Revise: `RESULT: logic_failure` (state machine loops back to project_plan)
   ```
   RESULT: {success|logic_failure}
   Phase: project_review
   Handoff: thoughts/swarm/handoffs-project-reviews/{filename}

   Summary: Project plan v{N} {approved|needs revision}: {one-line reason}
   ```

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still perform full review analysis

## Error Handling

- If project plan document not found → `RESULT: logic_failure` (project_plan phase must run first)
- If codebase verification hits ambiguity → note in review, don't block on it
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
