---
name: swarm-code
description: Implementation phase — read approved plan and implement changes step by step. Respects CLAUDE.md build constraints.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent
---

# Swarm Code Skill

Implements the changes described in an approved plan, step by step.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document from plan_review (approval context).
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `implement` |
| `CM_SWARM_HANDOFF_PATH` | Handoff from plan_review phase |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read the approved plan** — Find the latest approved plan:
   - Glob: `thoughts/swarm/plans/*_{ticketID}_*.md`
   - Use the highest version number

2. **Read CLAUDE.md** — Review project-level and subdirectory CLAUDE.md files for:
   - Build constraints (NEVER run `cargo build`/`go build` directly on macOS)
   - Coding conventions and patterns
   - File organization rules

3. **Implement step by step** — Follow the plan's Implementation Steps in order:
   - For each file in the File Inventory:
     - **New files**: Use Write tool
     - **Existing files**: Read first, then use Edit tool
   - Follow existing code patterns (imports, naming, error handling)
   - Match the surrounding code style exactly
   - **After each plan step**: Check for context pressure:
     ```bash
     test -f "/tmp/swarm-context-pressure-$CM_SWARM_SESSION_ID" && echo "CONTEXT_PRESSURE"
     ```
     If the sentinel exists, stop implementing. Write a handoff listing completed
     and remaining steps, then write `RESULT: context_limit`. See `swarm-conventions`
     for the full protocol.

4. **Post Linear comment** — `IMPL:` prefix:
   ```
   IMPL: {one-line summary}
   Files changed:
   - `path/to/file1` — {what changed}
   - `path/to/file2` — {what changed}
   ```

5. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-code/`.

6. **Write RESULT**:
   ```
   RESULT: success
   Phase: implement
   Handoff: thoughts/swarm/handoffs-code/{filename}

   Summary: Implemented {N} files per plan v{V}
   ```

## Build Constraints

**CRITICAL** — On macOS, NEVER run these commands directly:
- `cargo build`, `cargo clippy`, `cargo check`
- `go build`, `templ generate`
- `just generate`

These corrupt Docker's trunk builds via the shared bind-mount `target/` dir. Use `just check` from the project root only (it uses isolated `CARGO_TARGET_DIR`).

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write or edit files, post comments, or make any mutations
- Still read all files and describe what would be changed

## Error Handling

- If plan not found → `RESULT: logic_failure`
- If a file referenced in plan doesn't exist (for Edit) → note in handoff, skip that file
- If implementation diverges from plan → document the divergence in handoff
- If interrupted → write partial handoff listing what was and wasn't completed
- If context pressure detected → stop, write partial handoff with completed/remaining steps, `RESULT: context_limit`

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
