---
name: swarm-plan-review
description: Plan review phase — review an implementation plan against a checklist, verdict approve or revise. Read-only — does NOT modify the plan.
allowed-tools: Bash, Read, Glob, Grep, Agent
---

# Swarm Plan Review Skill

Reviews an implementation plan and renders a verdict: approve (advance to implement) or revise (loop back to code_plan).

**This skill is READ-ONLY** — it does NOT modify the plan document.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document for context from the planning phase.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `plan_review` |
| `CM_SWARM_ATTEMPT` | Current attempt (matches plan version) |
| `CM_SWARM_HANDOFF_PATH` | Handoff from code_plan phase |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Find the plan** — Glob: `thoughts/swarm/plans/*_{ticketID}_*_v{attempt}.md`
   - If not found, try without version suffix (v1 is the default)
   - Read the full plan document

2. **Read the ticket** — Understand acceptance criteria and requirements.

3. **Verify against codebase** — For each file in the File Inventory:
   - If Type=Edit: verify the file exists and the described changes make sense
   - If Type=New: verify the target directory exists and the file doesn't already exist
   - Check that import paths, function signatures, and types referenced in the plan actually exist

4. **Review checklist** — Evaluate each criterion:

   | # | Criterion | Weight |
   |---|-----------|--------|
   | 1 | **Goal alignment** — Does the plan achieve what the ticket asks for? | Critical |
   | 2 | **Acceptance criteria** — Are all ticket criteria covered? | Critical |
   | 3 | **File inventory** — Are all needed files listed? Any missing? | High |
   | 4 | **Implementation steps** — Are steps clear and in correct order? | High |
   | 5 | **Verification checks** — Are checks sufficient to catch regressions? | High |
   | 6 | **Risk assessment** — Are risks identified with mitigations? | Medium |
   | 7 | **Convention compliance** — Does it follow CLAUDE.md and project patterns? | Medium |
   | 8 | **Scope creep** — Does it stay within ticket scope? No unnecessary changes? | Medium |

5. **Render verdict**:
   - **Approve** — All Critical criteria pass, no High criteria have blocking issues
   - **Revise** — Any Critical criterion fails, or multiple High criteria have issues

6. **Post Linear comment** — `PLAN-REVIEW:` prefix:
   ```
   PLAN-REVIEW: {APPROVE|REVISE} — v{N}

   Checklist:
   - [x] Goal alignment: {brief note}
   - [x] Acceptance criteria: {brief note}
   - [x] File inventory: {brief note}
   - [ ] Implementation steps: {issue found}
   ...

   {If REVISE: specific feedback for the planner}
   ```

7. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-plan-reviews/`:
   - If APPROVE: Next Steps should reference the implement phase
   - If REVISE: What Was NOT Done should list specific issues; Next Steps should list what the planner needs to fix

8. **Write RESULT**:
   - Approve: `RESULT: success` (state machine advances to implement)
   - Revise: `RESULT: logic_failure` (state machine loops back to code_plan)
   ```
   RESULT: {success|logic_failure}
   Phase: plan_review
   Handoff: thoughts/swarm/handoffs-plan-reviews/{filename}

   Summary: Plan v{N} {approved|needs revision}: {one-line reason}
   ```

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still perform full review analysis

## Error Handling

- If plan document not found → `RESULT: logic_failure` (code_plan phase must run first)
- If codebase verification hits ambiguity → note in review, don't block on it
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
