---
name: swarm-code-verify
description: Verification phase — run the plan's verification checks and report structured PASS/FAIL results. Does NOT fix failures.
allowed-tools: Bash, Read, Glob, Grep
---

# Swarm Code Verify Skill

Runs verification checks from the implementation plan and reports structured results. **Does NOT fix failures** — only reports them.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document from the implement phase.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `verify` |
| `CM_SWARM_ATTEMPT` | Verify attempt number |
| `CM_SWARM_HANDOFF_PATH` | Handoff from implement phase |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read the plan** — Find the approved plan:
   - Glob: `thoughts/swarm/plans/*_{ticketID}_*.md`
   - Extract the "Verification Checks" section

2. **Run `just check`** — Full project compilation check:
   ```bash
   just -f /path/to/project/justfile check
   ```
   Record exit code and output.

3. **Run each verification check** — Execute each check from the plan sequentially:
   - Record the command, exit code, and relevant output (truncate to last 50 lines if verbose)
   - Do NOT attempt to fix any failures

4. **Compile results** — For each check:
   ```
   [PASS] Check 1: just check — exit 0
   [FAIL] Check 2: go test ./internal/swarm/... — exit 1
     Error: TestFoo: expected 5, got 3
   ```

5. **Post Linear comment** — `VERIFY:` prefix:
   ```
   VERIFY: {PASS|FAIL} — {passed}/{total} checks passed

   Results:
   - [PASS] just check
   - [FAIL] go test ./internal/swarm/... — TestFoo assertion error
   ```

6. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-code-reviews/`:
   - If FAIL: Gotchas should include the specific errors; Next Steps should describe what the implement phase needs to fix

7. **Write RESULT**:
   - All pass: `RESULT: success` (advance to PR)
   - Any fail: `RESULT: logic_failure` (loop back to implement)
   ```
   RESULT: {success|logic_failure}
   Phase: verify
   Handoff: thoughts/swarm/handoffs-code-reviews/{filename}

   Summary: {passed}/{total} checks passed{; first failure description if any}
   ```

## Important Constraints

- **Do NOT fix failures** — This skill only observes and reports
- **Do NOT modify any files** — Read-only except for handoff and comments
- **Run checks in the plan's order** — Some may depend on previous checks
- **Capture full error output** — The implement phase needs details to fix issues

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT run any commands, write files, or post comments
- List the checks that would be run

## Error Handling

- If plan not found → `RESULT: logic_failure`
- If a check command hangs → kill after 5 minutes, report as FAIL with timeout
- If `just check` itself fails to run (infra) → `RESULT: infra_failure`
- If interrupted → write partial handoff with results gathered so far

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
