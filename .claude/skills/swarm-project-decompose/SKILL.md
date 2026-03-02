---
name: swarm-project-decompose
description: Project decompose phase — analyze research findings and identify independent research topics for deeper investigation before project planning.
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
---

# Swarm Project Decompose Skill

Reads the initial research findings for a project ticket and decomposes them into independent child research topics. Each topic becomes a separate research workflow. The aggregated findings from all child research workflows will feed into the project_plan phase.

## Preamble

1. If `$CM_SWARM_HANDOFF_PATH` is set, read the handoff document from the research phase.
2. If `$CM_SWARM_LEARNING_CONTEXT_PATH` is set, read it for relevant learnings.
3. Load `swarm-conventions` for comment format and document path conventions.

## Environment

| Variable | Purpose |
|----------|---------|
| `CM_SWARM_TICKET_ID` | Ticket identifier |
| `CM_SWARM_WORKFLOW_ID` | Workflow UUID |
| `CM_SWARM_SESSION_ID` | Session UUID |
| `CM_SWARM_PHASE` | Should be `project_decompose` |
| `CM_SWARM_ATTEMPT` | Attempt number |
| `CM_SWARM_HANDOFF_PATH` | Handoff from research phase |
| `CM_SWARM_LEARNING_CONTEXT_PATH` | Learning context file |
| `CM_SWARM_RESULT_PATH` | Path to write RESULT output as final action |
| `CM_SWARM_DRY_RUN` | If `true`, no writes |

## Process

1. **Read research** — Find and read the research document for this ticket:
   - Glob: `thoughts/swarm/research/*_{ticketID}_*.md`
   - Also read the handoff from `$CM_SWARM_HANDOFF_PATH`

2. **Analyze research gaps** — From the research findings, identify:
   - Distinct areas that need deeper, independent investigation
   - Questions that remained unanswered or insufficiently explored
   - Topics that are large enough to warrant their own research workflow
   - Areas where the initial research surface-level findings need depth

3. **Create decompose document** — Write to `thoughts/swarm/decompose/{timestamp}_{ticketID}_{slug}.md`:

   ```markdown
   ---
   ticket: {ticketID}
   workflow: {workflowID}
   session: {sessionID}
   timestamp: {ISO 8601}
   ---

   # Research Decomposition: {ticket title}

   ## Initial Research Summary
   {Brief summary of what the initial research phase found}

   ## Research Topics

   | # | Topic | Description |
   |---|-------|-------------|
   | 1 | Topic title | Specific research question or area to investigate |
   | 2 | Topic title | Specific research question or area to investigate |
   | 3 | Topic title | Specific research question or area to investigate |

   ## Topic Details

   ### 1. {Topic title}
   **Why**: {Why this needs deeper research}
   **Scope**: {What should be investigated}
   **Expected output**: {What the research should produce}

   ### 2. {Topic title}
   ...

   ## Rationale
   {Why these specific topics were chosen and how they relate to the project goals}
   ```

4. **Post Linear comment** — `DECOMPOSE:` prefix:
   ```
   DECOMPOSE: {ticket title}
   Doc: thoughts/swarm/decompose/{filename}
   Research topics: {count}
   Topics: {comma-separated list of topic titles}
   ```

5. **Write handoff** — Write handoff to `thoughts/swarm/handoffs-project/`.

6. **Write RESULT**:
   ```
   RESULT: success
   Phase: project_decompose
   Handoff: thoughts/swarm/handoffs-project/{filename}

   Summary: Decomposed into {count} research topics: {topic titles}
   ```

## Decomposition Guidelines

- **3-7 topics** is ideal — fewer means the initial research was sufficient (consider whether decompose is needed), more means you're slicing too thin
- **Independent topics**: Each topic should be researchable without the results of other topics. If topics depend on each other, combine them
- **Specific questions**: Each topic should have a clear, answerable research question — not a vague area
- **Right-sized**: Each topic should be completable by a single research workflow session
- **Cover the project scope**: Together, the topics should cover all areas needed for the subsequent project plan

## Dry-Run Mode

If `$CM_SWARM_DRY_RUN` is `true`:
- Print `[DRY-RUN]` before each action
- Do NOT write files or post comments
- Still perform all reads and analysis

## Error Handling

- If research document not found: `RESULT: logic_failure` (research phase must run first)
- If the research is already comprehensive enough that no decomposition is needed: still produce a document with 1-2 topics covering the most important areas for depth

## Result File Output

As the very last step, if `$CM_SWARM_RESULT_PATH` is set, write the RESULT block to that file path (in addition to any Linear comment).
