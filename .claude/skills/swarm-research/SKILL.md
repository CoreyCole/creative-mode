---
name: swarm-research
description: Research phase — explore codebase and web sources, produce structured research document. Invoked by swarm orchestrator for the research phase of any workflow type.
allowed-tools: Bash, Read, Glob, Grep, Agent, WebSearch, WebFetch
---

# Swarm Research Skill

Explores the codebase and external sources to answer the research questions in a ticket, then writes a structured research document.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document for prior context.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `research` |
| `CM_SWARM_HANDOFF_PATH` | Previous handoff (if any) |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read the ticket** — Understand the research questions or goals from the ticket description. Parse the YAML footer for context (dependencies, parent ticket, etc.).

2. **Explore the codebase** — Use Glob, Grep, Read, and Agent to find relevant code:
   - Identify key files, patterns, and architecture
   - Map dependencies and data flow
   - Note existing conventions and constraints

3. **Search external sources** — If the ticket involves external APIs, libraries, or standards:
   - Use WebSearch and WebFetch to gather documentation
   - Verify version compatibility
   - Note any breaking changes or deprecations

4. **Write research document** — Create `thoughts/swarm/research/{timestamp}_{ticketID}_{slug}.md`:
   ```markdown
   ---
   ticket: {ticketID}
   workflow: {workflowID}
   session: {sessionID}
   timestamp: {ISO 8601}
   ---

   # Research: {ticket title}

   ## Questions
   {List the research questions from the ticket}

   ## Findings

   ### {Finding 1}
   {Detail with file paths, code snippets, links}

   ### {Finding 2}
   ...

   ## Architecture Notes
   {Relevant architecture observations, constraints}

   ## Risks and Considerations
   {Potential issues, edge cases, dependencies}

   ## Recommendations
   {Suggested approach based on findings}
   ```

5. **Post Linear comment** — `RESEARCH:` prefix with summary and doc path:
   ```
   RESEARCH: {one-line summary}
   Doc: thoughts/swarm/research/{filename}
   Key findings:
   - {finding 1}
   - {finding 2}
   ```

6. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-research/`.

7. **Write RESULT** — Post RESULT comment to ticket:
   ```
   RESULT: success
   Phase: research
   Handoff: thoughts/swarm/handoffs-research/{filename}

   Summary: {one-line summary of research findings}
   ```

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files, post comments, or make any mutations
- Still perform all reads and analysis — output what would be written

## Error Handling

- If critical files cannot be found → `RESULT: logic_failure` with details
- If external search fails → continue with codebase-only findings, note gaps
- If interrupted → write partial handoff before exiting

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
