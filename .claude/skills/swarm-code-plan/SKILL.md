---
name: swarm-code-plan
description: Code planning phase — read research findings and create a versioned implementation plan with file inventory and verification checks.
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
---

# Swarm Code Plan Skill

Creates or revises a versioned implementation plan based on research findings.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document. If this is a revision (attempt > 1), the handoff contains plan-review feedback that MUST be addressed.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `code_plan` |
| `CM_SWARM_ATTEMPT` | Attempt number (1 = first plan, 2+ = revision) |
| `CM_SWARM_HANDOFF_PATH` | Previous handoff (research or plan-review) |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read research** — Find and read the research document for this ticket:
   - Glob: `thoughts/swarm/research/*_{ticketID}_*.md`
   - If revision (attempt > 1), also read the previous plan and the plan-review handoff

2. **Read review feedback** (revision only) — The handoff from `plan_review` contains:
   - Specific issues to address
   - Suggestions for improvement
   - Items that were approved (keep these)

3. **Create implementation plan** — Write to `thoughts/swarm/plans/{timestamp}_{ticketID}_{slug}_v{N}.md`:
   ```markdown
   ---
   ticket: {ticketID}
   workflow: {workflowID}
   session: {sessionID}
   version: {N}
   timestamp: {ISO 8601}
   previous_version: {path to v{N-1} if revision}
   ---

   # Plan: {ticket title} (v{N})

   ## Goal
   {What this plan achieves — ties back to ticket}

   ## Acceptance Criteria
   - [ ] {Criterion 1}
   - [ ] {Criterion 2}

   ## File Inventory

   | # | File | Type | ~Lines | Purpose |
   |---|------|------|--------|---------|
   | 1 | `path/to/file` | New/Edit | 50 | {what and why} |

   ## Implementation Steps

   ### Step 1: {title}
   {Detailed description of what to do, including code patterns to follow}

   ### Step 2: {title}
   ...

   ## Verification Checks

   Each check must be independently runnable:

   1. `just check` — Full project compilation
   2. `cd harness && go test ./path/to/...` — Unit tests pass
   3. {Additional check} — {What it verifies}

   ## Risks
   - {Risk 1}: {Mitigation}

   ## Revision Notes (v2+ only)
   - Addressed: {review feedback item}
   - Changed: {what was modified from previous version}
   ```

4. **Post Linear comment** — `PLAN:` prefix:
   ```
   PLAN: v{N} — {one-line summary}
   Doc: thoughts/swarm/plans/{filename}
   Files: {count} ({new} new, {edit} edit)
   Checks: {count} verification checks
   ```

5. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-code/`.

6. **Write RESULT**:
   ```
   RESULT: success
   Phase: code_plan
   Handoff: thoughts/swarm/handoffs-code/{filename}

   Summary: Created plan v{N} with {file count} files and {check count} verification checks
   ```

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still perform all reads and analysis

## Error Handling

- If research document not found → `RESULT: logic_failure` (research phase must run first)
- If ticket requirements are ambiguous → note ambiguities in Risks section, proceed with best interpretation
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
