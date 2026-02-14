# Development Guide: Frequent Intentional Compaction

This project uses **frequent intentional compaction** — a workflow that structures the entire development process around context management, keeping context window utilization in the 40-60% range and building high-leverage human review into the pipeline.

Based on Dex Horthy's [Advanced Context Engineering for Coding Agents](https://github.com/humanlayer/advanced-context-engineering-for-coding-agents/blob/main/ace-fca.md).

## Core Principle

LLMs are stateless functions. The context window is the **only lever** you have to affect output quality. Optimize for:

1. **Correctness** — no incorrect information in context
2. **Completeness** — no missing information
3. **Size** — minimize noise
4. **Trajectory** — keep the agent on track

## The Workflow: Research → Plan → Implement

Split every non-trivial task into three phases, each in a **fresh context window**. The output of each phase is a structured markdown artifact that gets human review before feeding into the next phase.

### 1. Research

Understand the codebase, relevant files, and information flow. Produces a `research.md` artifact.

- Uses subagents to search/read without polluting the main context
- Human reviews the research for correctness before proceeding
- Bad research = thousands of bad lines of code downstream

### 2. Plan

Outline exact implementation steps, files to edit, and testing/verification for each phase. Produces a `plan.md` artifact.

- Reads the research artifact as input
- Human reviews the plan for correctness and approach
- Bad plan = hundreds of bad lines of code downstream

### 3. Implement

Step through the plan phase by phase. For complex work, compact status back into the plan file after each phase is verified.

- Reads the plan artifact as input
- This is the only step that needs a git worktree (research/planning can happen on main)
- Human reviews the final code/PR

## Why This Works

- **Context stays clean** — each phase starts fresh, only loading the compact artifact from the previous phase
- **Human leverage is maximized** — reviewing 200 lines of a plan catches errors that would produce 2000 lines of bad code
- **Mental alignment** — specs and plans keep the team on the same page about what's changing and why
- **Works in large codebases** — the research phase handles the complexity of understanding 300k+ LOC projects

## Reference Prompts

The latest workflow prompts are maintained in the HumanLayer repo:

https://github.com/humanlayer/humanlayer/tree/main/.claude

Key prompts used in this workflow:

| Prompt | Purpose |
|--------|---------|
| `research_codebase.md` | Kick off a codebase research phase with subagents |
| `create_plan.md` | Create an implementation plan from research output |
| `create_handoff.md` | Compact current session state into a handoff artifact |
| `resume_handoff.md` | Resume work from a previous handoff artifact |
