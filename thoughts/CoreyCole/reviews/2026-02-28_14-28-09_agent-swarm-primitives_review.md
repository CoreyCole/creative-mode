---
date: 2026-02-28T14:28:09-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 586d18dd84133dd02672fc99ff516fdf51df1af5
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md
status: complete
last_updated: 2026-02-28T14:45:00-08:00
type: plan_review
---

# Plan Review: Agent Swarm Primitives

### Summary

A well-structured plan with strong composition principles and faithful mapping to the Chestnut flowchart. After discussion with the author and analysis of the OpenClaw codebase (`context/openclaw/`), two of the three original critical issues have been resolved. The remaining critical issue — Project Orchestrator (6️⃣) implementation details — and several concerns warrant attention before implementation.

### Resolved Issues

These were initially flagged as critical but have been addressed through discussion and verification:

1. **~~OpenClaw heartbeat scheduling~~ — RESOLVED**
   - Original concern: Heartbeat mechanism was unproven; HEARTBEAT.md appeared to be just a static markdown file.
   - Resolution: Analysis of `context/openclaw/src/infra/heartbeat-runner.ts` confirms OpenClaw has a fully functional timer-based heartbeat system. The gateway runs a `setTimeout` loop (default 30 minutes, configurable per-agent). On each tick: reads `HEARTBEAT.md` from the workspace (skips if empty/whitespace-only), sends the heartbeat prompt as a user message, gives the agent a full turn with all tools (exec, read, write, process). If the agent replies `HEARTBEAT_OK`, the transcript is pruned to avoid context pollution. Duplicate alerts are suppressed within 24 hours. Active hours are configurable per-agent with timezone support.
   - Key detail for the plan: The Lead FDE agent can use `exec` to run `tmux list-sessions`, `tmux capture-pane -p -t <session>`, etc. during heartbeat turns. No special mechanism needed — the HEARTBEAT.md instructions tell the agent what to check.

2. **~~Human approval flow conflicts with "don't wait" principle~~ — RESOLVED**
   - Original concern: `AskUserQuestion` in spawned Claude Code sessions would hang with no human present.
   - Resolution: The author clarified there are exactly **three human gates**, all at natural boundaries:
     1. **Classification** — When an idea first enters, the system asks clarifying questions to determine project vs code-change. Human is present (they just gave the idea).
     2. **Project kickoff** — Human approves the project plan before execution begins. After approval, the swarm runs autonomously.
     3. **PR merge** — Human reviews code before it ships. PRs go to "In Review" status; no blocking wait.
   - Implication: The plan revision loop inside code-change should be **agent-only** (the 5️⃣.1️⃣ plan-review primitive handles it, not `AskUserQuestion`). Once a project is kicked off, all spawned sessions run autonomously — they update Linear and create PRs without blocking on human input. No need for interactive vs autonomous mode detection.

### Critical Issues (Must Address Before Implementation)

1. **Project Orchestrator (6️⃣) implementation is underspecified**
   - Problem: The Chestnut flowchart shows a two-level orchestration model: Lead FDE (7️⃣) oversees all projects, Project Orchestrators (6️⃣) manage individual workstreams. The handoff states "Project Orchestrators (6️⃣) are ephemeral Claude Code sessions spawned per-project per heartbeat tick." Phase 4 implements the Lead FDE as an OpenClaw agent with a heartbeat, but doesn't specify how it spawns, tracks, or manages per-project orchestrator sessions. The `POST /spawn` endpoint exists but there's no specification of who calls it, when, or how results are collected.
   - Risk: Without per-project orchestrators, the Lead FDE must check every ticket across every project in a single heartbeat turn, which would exceed context limits for non-trivial project counts. The Lead FDE's HEARTBEAT.md needs to include instructions for spawning Claude Code sessions via the harness API (`POST /api/lead-fde/spawn`), and the plan needs to define how those sessions report back (Linear comments are the natural answer, since Linear is the source of truth).
   - Suggestion: Expand Phase 4 to specify: (a) the Lead FDE's HEARTBEAT.md instructions for spawning project orchestrator sessions via `exec` (curl to harness API or direct tmux creation), (b) that orchestrators report status via Linear ticket comments (which the Lead FDE reads on next heartbeat), (c) a max concurrent sessions limit (VPS has 10 GB RAM — each Claude Code session + potential WASM build is significant), (d) session naming convention (e.g., `cm-swarm-{projectID}-orch`).

### Concerns (Should Address)

1. **Context window pressure from nested primitive loading**
   - Observation: A `/swarm project` invocation loads SKILL.md + project.md + conventions.md, then calls research (research.md), which calls code-change (code-change.md), which calls plan-review (plan-review.md) and verify (verify.md). That's 6+ markdown files plus CLAUDE.md plus the existing conversation. The playwright-cli skill works because it's a flat reference for a single tool — the swarm skill is a deeply nested orchestration system.
   - Suggestion: Keep primitive files concise (~100-150 lines max). Consider whether Claude actually needs to load sub-primitives or can just invoke them as separate `/swarm research` commands. Flatter is better for context management.

2. **No workflow state persistence between steps**
   - Observation: The code-change lifecycle has ~8 distinct phases. If a Claude Code session crashes, times out, or hits context limits mid-workflow, there's no way to resume. Linear ticket status captures the high-level state, but not which sub-step within a phase was reached. The plan says "Never leave ticket in inconsistent state" (Phase 5) but doesn't say how.
   - Suggestion: Each phase transition should update the Linear ticket with a structured comment (the emoji-prefixed comments in conventions.md partially do this). Add explicit guidance: "Before starting each phase, read the ticket's comment history to determine where a previous attempt left off." The heartbeat system naturally handles this — the Lead FDE checks stalled tickets and re-prompts or spawns fresh sessions.

3. **Flowchart shows Slack; plan uses Discord**
   - Observation: The Chestnut flowchart explicitly shows "Slack ⚠️ ONLY if critical" for the Project Orchestrator, and "Slack only for critical human input needs" in Design Principles. The plan uses Discord exclusively for the Lead FDE binding. If this system is meant to be general-purpose (portable to any codebase), Slack is far more common than Discord.
   - Suggestion: Either (a) acknowledge this is a Discord-only implementation with Slack as a future extension, or (b) abstract the notification channel so Slack support can be added later. For Phases 1-3 (Claude Code skills), this doesn't matter — but Phase 4 (OpenClaw + Discord binding) bakes in Discord.

4. **Label creation idempotency is assumed but unverified**
   - Observation: The setup primitive says "idempotent — skips if exists" for label creation, but doesn't specify how. `linear-cli` may not have a "create if not exists" semantic for labels — it likely returns an error if a label with the same name already exists.
   - Suggestion: Verify `linear-cli label create` behavior when a label exists. If it errors, the setup primitive needs to `linear-cli label list --output json` first, diff against desired labels, and only create missing ones.

5. **`PresidentManager` pattern has known gaps**
   - Observation: The plan says to follow the president pattern for Lead FDE provisioning. Analysis shows `PresidentManager` is a **provisioning-only object** — after `Provision()`, it's never used again. `s.PresidentManager` is never dereferenced by any handler. The API handlers are stateless, shelling out to tmux. However, the underlying OpenClaw heartbeat system IS functional (see Resolved Issue #1), so the gap is in the Go-side harness code, not in OpenClaw itself.
   - Suggestion: The Lead FDE Manager should go beyond the president pattern: add a health check endpoint that queries OpenClaw agent status and a way to query active orchestrator tmux sessions.

6. **No dry-run capability for testing**
   - Observation: The testing strategy (Phase 5) is entirely manual. There's no way to test a primitive without creating real Linear tickets. The plan mentions `--dry-run` in passing but doesn't specify how primitives would support it.
   - Suggestion: Add a `--dry-run` convention to `conventions.md`: when present, primitives print what they would do (ticket creation, comment posting, status updates) without executing. Essential for iterating on the primitives without polluting Linear.

### Questions (Need Clarification)

1. When the Lead FDE spawns a Claude Code session (`claude --command "/swarm code-change CM-123"`), does that session have access to `.claude/skills/swarm/`? It should if the working directory is the repo root, but this needs to be verified since the plan doesn't address it.

2. What's the max number of concurrent Claude Code sessions the VPS can handle? The VPS has 10 GB RAM. Each WASM build uses ~5 GB. Claude Code sessions also consume resources. If the Lead FDE spawns 3 project orchestrators each spawning 2 code-change sessions, that's 6+ concurrent sessions.

3. Should primitives be invocable by other primitives (nested), or should they always return to SKILL.md which re-routes? The plan implies nesting (project calls research, code-change calls verify) but this creates deep call stacks within a single Claude Code session.

4. The plan says "Linear team: Default `CM` (read from CLAUDE.md or pass as argument)." How does a primitive read the team from CLAUDE.md? Claude Code loads CLAUDE.md into context, but there's no structured field for team key. Is it just parsed from the text?

### Suggestions (Nice to Have)

1. **Add a `/swarm resume <ticket>` primitive** — Given that sessions can crash or time out, a resume primitive that reads a ticket's comment history and picks up where the last session left off would be invaluable. This aligns with the existing `/resume_handoff` pattern and the heartbeat system's natural recovery model.

2. **Rate limit awareness** — The Linear API allows 1500 requests/hour. A project decomposition that creates 20 tickets, sets 15 relations, and posts 20 comments uses ~55 requests. Add a request counter to conventions so primitives can batch and pace themselves.

3. **Consider shipping Phases 1-3 first** — These are immediately useful as human-driven skills and validate the primitive design before investing in Phase 4 automation. The heartbeat system is proven, but the Lead FDE's specific HEARTBEAT.md instructions and project orchestrator spawning pattern need design iteration that benefits from real-world usage of the primitives.

4. **Template for plan docs** — The plan has a template for research docs and ticket descriptions, but not for plan docs created by `/create_plan`. Since the plan-review primitive evaluates plans, having a standard structure would improve review quality.

### What's Good

- **Strong composition principle**: Reusing `/create_plan`, `/implement_plan`, `/validate_plan`, etc. instead of reimplementing is excellent. The primitives stay thin and benefit from improvements to the underlying commands.
- **Linear as source of truth**: Clean separation — agents don't store state internally, everything is in Linear. Observable and debuggable via the Linear UI.
- **Faithful flowchart mapping**: Every numbered primitive from the Chestnut flowchart has a corresponding markdown file. The three nested loops in the code-change lifecycle (plan revision, implement/verify, full restart) are all captured.
- **Phased approach with clear boundaries**: Each phase is independently testable. Phase 1 establishes conventions, Phase 2 delivers value, Phase 3 adds polish, Phase 4 adds automation.
- **Convention-driven design**: Emoji-prefixed comments, structured ticket footers, and label taxonomy create a consistent audit trail that makes debugging straightforward.
- **Well-defined scope**: The "What We're NOT Doing" section is clear and prevents scope creep.
- **Proven heartbeat foundation**: The OpenClaw heartbeat system (`context/openclaw/src/infra/heartbeat-runner.ts`) is mature — timer scheduling, HEARTBEAT_OK pruning, duplicate suppression, active hours, coalesced wakes. Phase 4 builds on solid infrastructure.
- **Clear human touchpoints**: Three gates (classification, project kickoff, PR merge) are at natural boundaries. The swarm is autonomous between gates, which aligns with the "don't wait" design principle.

### Recommended Next Steps

1. **Specify the Project Orchestrator spawning pattern** — Expand Phase 4 to detail how the Lead FDE's HEARTBEAT.md instructs it to spawn per-project Claude Code sessions, how those sessions report back via Linear comments, and resource limits.

2. **Update code-change.md to remove human plan approval** — The plan revision loop should be agent-only (5️⃣.1️⃣ plan-review). Remove `AskUserQuestion` from the plan revision loop. Human approval happens only at classification and project kickoff.

3. **Implement Phases 1-3 first** — Validate the primitives with human-driven usage before building Phase 4 automation. This lets you iterate on the HEARTBEAT.md instructions based on real experience with the primitives.

4. **Add dry-run support from the start** — Essential for development iteration without polluting Linear.

5. **Verify linear-cli label and relation commands** — Run `linear-cli label create --help` and `linear-cli rel add --help` to confirm exact syntax before writing primitives that depend on them.

6. **Keep primitives concise** — Target ~100-150 lines per primitive file. If a primitive needs more, split the logic or rely more on the composed commands.
