---
name: swarm-project-verify
description: Project verification phase — verify project milestones after all child tickets complete. Reports structured PASS/FAIL per milestone.
allowed-tools: Bash, Read, Glob, Grep, Agent
---

# Swarm Project Verify Skill

Verifies that a project's milestones are met after all child tickets have completed. Reports PASS/FAIL per milestone.

**This skill is READ-ONLY for project artifacts** — it reads plans, child handoffs, and code, but does not modify them. It may run verification commands (tests, `just check`).

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document for context.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier (the project parent ticket) |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `project_verify` |
| `CM_SWARM_ATTEMPT` | Attempt number (1 = first verification, 2+ = retry) |
| `CM_SWARM_HANDOFF_PATH` | Previous handoff |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Find the project plan** — Glob: `thoughts/swarm/project-plans/*_{ticketID}_*.md`
   - Read the approved project plan (latest version)
   - Extract the milestones checklist

2. **Gather child ticket results** — For each child ticket in the decomposition:
   - Glob: `thoughts/swarm/handoffs-code/*_{childTicketID}_*.md` and `thoughts/swarm/handoffs-research/*_{childTicketID}_*.md`
   - Read the final handoff for each child
   - Verify each child's RESULT was `success`
   - Note any children that failed or are incomplete

3. **Verify each milestone** — For each milestone in the plan:

   **a. Check completion criteria:**
   - If the milestone references specific tests: run them
   - If the milestone references `just check`: run it
   - If the milestone references file existence: verify files exist
   - If the milestone references child ticket completion: verify all referenced children succeeded

   **b. Record result:**
   ```
   [PASS] M1: {name} — {what was verified}
   [FAIL] M2: {name} — {what failed and why}
   [SKIP] M3: {name} — {why it was skipped}
   ```

4. **Run integration verification** — Beyond individual milestones:
   - `just check` — full project compilation
   - Run any cross-cutting tests that span multiple child tickets
   - Verify no regressions from the combined changes

5. **Compile report**:
   ```
   ## Project Verification Report

   Project: {ticketID} — {title}
   Plan: {plan path}
   Child Tickets: {total} ({passed} passed, {failed} failed, {incomplete} incomplete)

   ### Milestone Results
   [PASS] M1: {name} — {detail}
   [FAIL] M2: {name} — {detail}
   [PASS] M3: {name} — {detail}
   [PASS] M4: Final verification — just check passes

   ### Child Ticket Status
   - {childID}: {type} — {status} — {one-line summary}
   - {childID}: {type} — {status} — {one-line summary}

   ### Overall: {PASS|FAIL}
   {Summary of what passed and what needs attention}
   ```

6. **Post Linear comment** — `PROJECT-VERIFY:` prefix:
   ```
   PROJECT-VERIFY: {PASS|FAIL}
   Milestones: {passed}/{total} passed
   Children: {completed}/{total} completed
   {If FAIL: specific failures listed}
   ```

7. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-project/`:
   - If PASS: What Was Done lists all verified milestones
   - If FAIL: What Was NOT Done lists failed milestones with details; Next Steps lists what needs fixing

8. **Write RESULT**:
   - All milestones pass: `RESULT: success` (project workflow completes)
   - Any milestone fails: `RESULT: logic_failure` (state machine retries project_verify)
   ```
   RESULT: {success|logic_failure}
   Phase: project_verify
   Handoff: thoughts/swarm/handoffs-project/{filename}

   Summary: Project verification {passed|failed}: {passed}/{total} milestones, {detail}
   ```

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still run verification commands and report results

## Error Handling

- If project plan not found → `RESULT: logic_failure` (cannot verify without plan)
- If child handoffs not found → note as incomplete children, continue with available data
- If `just check` fails → milestone FAIL, include error output in report
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
