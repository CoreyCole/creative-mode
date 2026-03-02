---
date: 2026-03-01T19:45:56-08:00
researcher: CoreyCole
git_commit: 68aa6bae42c6e529ef3e8d48f0e5d40bc485c812
branch: feature/agent-swarm
repository: creative-mode
topic: "Overstory vs Creative Mode Swarm: Architecture Comparison and Optimization Insights"
tags: [research, codebase, swarm, overstory, orchestration, agent-system, linear, sqlite]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
---

# Research: Overstory vs Creative Mode Swarm — Architecture Comparison and Optimization Insights

**Date**: 2026-03-01T19:45:56-08:00
**Researcher**: CoreyCole
**Git Commit**: 68aa6bae42c6e529ef3e8d48f0e5d40bc485c812
**Branch**: feature/agent-swarm
**Repository**: creative-mode

## Research Question

Compare the overstory multi-agent orchestration system with our swarm implementation in the harness server. Identify patterns, abstractions, and insights from overstory that could optimize our swarm — particularly around prompt management, agent coordination, observability, and state management.

## Summary

Overstory and our swarm share the same core architecture: spawn Claude Code sessions in tmux, coordinate via SQLite, poll for completion. But overstory is **significantly more mature** in three areas that matter for our system: (1) prompt composition via layered templates + Canopy, (2) inter-agent messaging via a SQLite mail system, and (3) observability depth via event/timeline/cost tracking. Our swarm has advantages in Linear integration and web-based dashboarding (templ+datastar vs TUI). Below are 10 actionable insights ranked by impact.

## Detailed Findings

### Architecture Comparison Matrix

| Dimension | Our Swarm | Overstory |
|-----------|-----------|-----------|
| **Runtime** | Go (harness server) | TypeScript/Bun CLI |
| **State storage** | SQLite (8 tables, sqlc) | SQLite (4 databases: sessions, events, metrics, mail) |
| **Task tracking** | Linear (external, via CLI skills) | Beads/Seeds (pluggable CLI backends) |
| **Agent spawning** | tmux + `claude --dangerously-skip-permissions --input-file` | tmux + runtime adapter (Claude/Pi/Copilot/Codex) |
| **Agent types** | 7 skills (research, plan, review, implement, verify, PR, conventions) | 8 roles (coordinator, lead, scout, builder, reviewer, merger, monitor, supervisor) |
| **Workflow model** | Linear phase pipeline with retry loops | Free-form delegation with hierarchy enforcement |
| **Agent communication** | Filesystem handoffs only (no inter-agent messaging) | SQLite mail system + tmux nudge + filesystem handoffs |
| **Prompt management** | Raw SKILL.md files per skill | Canopy (versioned, section-based, inherited) + overlay templates |
| **Observability** | templ+Datastar SSE dashboard (web) | TUI dashboard + trace + feed + replay + inspect + costs (CLI) |
| **Merge strategy** | N/A (single-agent-at-a-time per ticket) | 4-tier conflict resolution (clean, auto, AI, reimagine) |
| **Health monitoring** | tmux poll every 15s | 3-tier watchdog (mechanical, AI triage, monitor agent) |
| **Crash recovery** | `RecoverWorkflows()` on startup | Checkpoint/handoff system with session bridging |
| **Cost tracking** | `total_tokens` per session (from result file) | Per-model pricing, live burn rate, transcript parsing |
| **Learning system** | `swarm_learnings` table with relevance decay | Mulch (external structured expertise tool) |

### Insight 1: Prompt Composition — The Biggest Gap (HIGH IMPACT)

**Overstory's approach**: Three-tier prompt pipeline:
1. **Canopy** (`cn` CLI): Section-based prompt authoring with inheritance chains, schema validation, versioning. Prompts stored as JSONL records with `sections: [{name, body}]`. Inheritance via `extends` field (e.g., `builder extends leaf-worker extends base-agent`).
2. **Template variables**: Emitted `.md` files contain `{{PLACEHOLDER}}` tokens resolved at spawn time.
3. **Overlay generation**: `generateOverlay()` reads `templates/overlay.md.tmpl`, injects the full base definition via `{{BASE_DEFINITION}}`, resolves all variables in a single pass.

**Our approach**: Raw `SKILL.md` files per skill with a conventions skill loaded manually. No inheritance, no templating, no schema validation. Each skill duplicates the 3-step preamble (read handoff, read learnings, load conventions).

**What we should steal**:
- **Section-based composition**: Extract common sections (preamble, communication protocol, result format, failure modes) into a base template. Each skill only defines its unique workflow section.
- **Template variable resolution**: Use Go `text/template` to inject `{{.TicketID}}`, `{{.HandoffPath}}`, `{{.LearningContextPath}}`, `{{.BranchName}}` at spawn time instead of relying on env vars that each skill must read manually.
- **Schema validation**: Define required sections per skill type (e.g., every code skill must have a "verification" section, every review skill must declare it's read-only).

**Concrete implementation**: A `PromptBuilder` in `harness/internal/swarm/` that:
```go
type PromptBuilder struct {
    BaseTemplate string           // shared preamble + protocol
    SkillSections map[string]string // per-skill workflow sections
    Variables    map[string]string // resolved at spawn time
}

func (pb *PromptBuilder) Render(skill string) (string, error) {
    // 1. Load base template
    // 2. Merge skill-specific sections
    // 3. Resolve {{variables}} from workflow context
    // 4. Return complete prompt written to temp file
}
```

This eliminates the duplicated preamble across all 7 skills and makes adding new skills trivial.

### Insight 2: Inter-Agent Messaging — Missing Entirely (HIGH IMPACT)

**Overstory's approach**: Purpose-built SQLite mail system (`mail.db`) with:
- Typed protocol messages (`worker_done`, `merge_ready`, `dispatch`, `escalation`, etc.)
- Thread-based replies
- Group broadcast (`@all`, `@builders`)
- Pull delivery via `UserPromptSubmit` hook injection
- Push delivery via tmux nudge for urgent messages
- Auto-nudge logic that avoids I/O corruption (writes file markers instead of direct tmux keys)

**Our approach**: No inter-agent communication. Each Claude Code session is fully independent. The orchestrator mediates everything through filesystem handoffs and env vars.

**Why this matters**: Our code workflow has 6 phases that run sequentially on the same ticket. If the plan reviewer wants to ask the planner a clarifying question, it can't — it can only reject with `logic_failure` and the planner starts from scratch. Overstory agents can negotiate.

**What we should consider**:
- For our current sequential pipeline, messaging has limited value since phases don't overlap.
- **If we add parallel agents** (e.g., multiple builders working different files on the same ticket), messaging becomes essential for coordination.
- The simpler version: add a `swarm_messages` table and have the orchestrator inject relevant messages into the next session's context (similar to how we already inject learning context).

### Insight 3: Event/Timeline Granularity (MEDIUM IMPACT)

**Overstory's approach**: `events.db` stores per-tool-call events with:
- `tool_name`, `tool_args`, `tool_duration_ms`
- Correlated start/end events
- Indexes on agent+time, run+time, type+time
- Event types: `tool_start`, `tool_end`, `session_start`, `session_end`, `mail_sent`, `spawn`, `error`, `custom`

This enables: `ov trace` (per-agent timeline), `ov replay` (cross-agent interleaved timeline), `ov errors` (aggregated error view), `ov feed` (real-time stream).

**Our approach**: `swarm_events` table has 17 event types but only records workflow-level transitions (`workflow_started`, `phase_started`, `session_completed`, etc.). No tool-level granularity.

**What we should steal**:
- **Tool-level events via Claude Code hooks**: Claude Code's `PreToolUse`/`PostToolUse` hooks can POST events to `/api/swarm/event`. This gives us tool-call timelines without changing the agent skills.
- **Incremental event polling via auto-increment ID**: Our SSE dashboard currently re-queries all workflows and events on each swarm event. Overstory's `EventBuffer` pattern (track `lastSeenId`, query `WHERE id > $lastSeenId`) is much more efficient for SSE.

### Insight 4: Inline Health Reconciliation (MEDIUM IMPACT)

**Overstory's approach**: Both the dashboard and status commands call `evaluateHealth()` during data loading, reconciling observable state (tmux alive? pid alive?) against recorded state on every render tick. The "ZFC Principle" (Zero Failure Crash) treats observable state as ground truth.

**Our approach**: `watchSession()` polls `tmux has-session` every 15s in a goroutine. If the session disappears, it triggers completion. But the dashboard doesn't independently verify tmux liveness — it trusts the DB state.

**What we should steal**:
- On each SSE tick, verify tmux sessions are alive for all running workflows before sending the dashboard fragment. If a tmux session died but the watcher goroutine hasn't fired yet, the dashboard should show it immediately rather than waiting up to 15 seconds.

### Insight 5: Cost Tracking Depth (MEDIUM IMPACT)

**Overstory's approach**: Three modes:
1. **Historical**: Per-session token counts (input, output, cache_read, cache_creation) with per-model pricing
2. **Live**: `token_snapshots` table with real-time burn rate ($/min, tokens/min)
3. **Self**: Parses the orchestrator's own Claude Code transcript JSONL

**Our approach**: `total_tokens` field on `swarm_sessions`, populated from the RESULT file. No per-model pricing, no burn rate, no orchestrator self-cost.

**What we should steal**:
- Parse Claude Code's transcript JSONL (`~/.claude/projects/*/` directory) after session completion for accurate token breakdowns. Our RESULT file only has a single `total_tokens` number.
- Add pricing calculations — even rough ones — to show $ cost per workflow in the dashboard.

### Insight 6: Crash Recovery — Checkpoint System (MEDIUM IMPACT)

**Overstory's approach**: `checkpoint.json` captures progress, files modified, pending work, branch state. On crash, the next session loads the checkpoint and continues. `handoffs.json` is an append-only log of session transitions.

**Our approach**: `RecoverWorkflows()` on harness restart checks tmux liveness and re-attaches watchers. But if a Claude Code session died mid-work, the next session in the same phase starts from scratch.

**What we should steal**:
- Our handoff documents already serve this purpose partially. But they're written at session END, not during execution.
- Add a `PreCompact` hook (Claude Code fires this before context compression) that writes a checkpoint handoff. This way, if a session hits context limits, the next session has partial progress.

### Insight 7: Capability-Based Guards (LOW IMPACT for current design)

**Overstory's approach**: `settings.local.json` deployed to each worktree with PreToolUse hooks that block Write/Edit for read-only agents, restrict file paths to the worktree for builders, and block git push/force operations for all agents.

**Our approach**: We use `--dangerously-skip-permissions` so all tools are allowed. Guard behavior is encoded in the SKILL.md instructions only (soft enforcement).

**What we should consider**:
- Our sequential pipeline means only one agent runs at a time per ticket, so tool conflicts are less dangerous.
- But adding PreToolUse guards for review/verify skills (preventing accidental writes) would be a cheap safety improvement.

### Insight 8: Progressive Escalation for Stalled Agents (LOW IMPACT)

**Overstory's approach**: 4-level escalation: warn → nudge → AI triage → terminate, with time-based progression and `stalledSince` tracking.

**Our approach**: Binary — either the tmux session exists or it doesn't. No stall detection during execution.

**What we should consider**:
- Add a `stall_timeout` to our config. If `watchSession()` has been polling for > N minutes without the tmux session ending, escalate (log warning, eventually kill).
- The AI triage tier (Tier 1) is clever but complex. A simpler version: if stalled for 45 minutes, kill and mark as `timeout`.

### Insight 9: Agent Identity Persistence (LOW IMPACT)

**Overstory's approach**: YAML-based agent identity files (CVs) tracking sessions completed, expertise domains, recent tasks. Identity accumulates across assignments.

**Our approach**: No persistent agent identity. Each session is stateless beyond the learning system.

**Not recommended to adopt**: Our agents are ephemeral by design — each phase is a fresh Claude Code session. Persistent identity makes more sense for overstory's model where the same "builder-1" agent may be reused across multiple tasks.

### Insight 10: Merge Queue (N/A for current design)

**Overstory's approach**: FIFO merge queue with 4-tier conflict resolution because multiple agents work in parallel worktrees.

**Our approach**: Sequential pipeline — one agent at a time per ticket, all working on the same branch.

**Not applicable now**: If we add parallel agents working different files on the same ticket, we'd need merge coordination. But for sequential execution, this is unnecessary complexity.

## Architecture Insights

### Key Pattern: HOW/WHAT Separation

Overstory's most powerful pattern is separating agent instructions into:
- **HOW layer** (base definition): Workflow, constraints, failure modes, communication protocol — shared across all instances of a capability.
- **WHAT layer** (overlay): Task ID, file scope, spec path, branch name — unique per spawn.

Our swarm partially does this (SKILL.md = HOW, env vars = WHAT) but the boundary is blurry. Skills manually read env vars to get their WHAT context. A template system would make this explicit and DRY.

### Key Pattern: Defense-in-Depth

Overstory enforces agent behavior at three levels:
1. **Instruction text** (base definition tells agent what it may do)
2. **Overlay constraints** (per-spawn worktree/file restrictions)
3. **Runtime guards** (PreToolUse hooks that mechanically block tools)

We only have level 1 (SKILL.md text). Adding level 3 (PreToolUse hooks for read-only phases) would be a cheap win.

### Key Pattern: Fire-and-Forget Side Effects

Both systems use this pattern for non-critical operations: learning capture, event recording, expertise fetching. If it fails, log and continue. This is correct for an orchestrator — the primary workflow must not be blocked by observability.

### Key Anti-Pattern from Overstory: Over-Engineering

Overstory's STEELMAN.md is refreshingly honest about the risks of agent swarms. Key quote: "20-agent swarm: $60 vs single agent: $9 for a 2hr speedup." Their system has 32 CLI commands, 4 SQLite databases, 11 doctor check categories, and 3 watchdog tiers. This complexity has real maintenance cost.

Our swarm is deliberately simpler: 7 skills, 1 database, 1 dashboard. We should adopt specific patterns (prompt composition, event granularity, cost tracking) without importing the full complexity.

## Code References

### Our Swarm
- `harness/internal/swarm/statemachine.go:43` — `DetermineNextPhase()` pure state machine
- `harness/internal/swarmorch/manager.go:210` — `spawnSession()` creates tmux + sends skill prompt
- `harness/internal/swarmorch/manager.go:291` — `handleSessionComplete()` reads RESULT, advances workflow
- `harness/internal/swarm/learnings.go:144` — `GetLearningContext()` assembles learning markdown
- `harness/internal/swarm/handoffs.go:22` — `ResolveHandoffPath()` finds most recent handoff
- `harness/internal/db/migrations/006_swarm_tables.sql` — full schema (8 tables)
- `harness/views/swarm/dashboard.templ` — Datastar SSE dashboard
- `harness/internal/server/swarm_dashboard.go:63` — SSE event handler

### Overstory
- `context/overstory/src/agents/overlay.ts:274` — `generateOverlay()` template rendering
- `context/overstory/src/mail/store.ts:46-59` — mail schema
- `context/overstory/src/mail/client.ts:93-116` — injection formatting
- `context/overstory/src/events/store.ts:107` — event storage
- `context/overstory/src/watchdog/health.ts:92` — `evaluateHealth()` ZFC principle
- `context/overstory/src/commands/dashboard.ts:249` — `EventBuffer` incremental polling
- `context/overstory/src/metrics/pricing.ts:30` — per-model cost rates
- `context/overstory/src/merge/resolver.ts:563` — 4-tier merge resolver
- `context/overstory/.canopy/prompts.jsonl` — Canopy section-based prompts
- `context/overstory/agents/builder.md` — Canopy-emitted base definition with template vars

## Prioritized Recommendations

| Priority | Insight | Effort | Impact |
|----------|---------|--------|--------|
| 1 | Prompt composition via templates (Insight 1) | Medium | High — eliminates duplication, makes new skills trivial |
| 2 | Event granularity via Claude Code hooks (Insight 3) | Low | Medium — enables timeline/replay views in dashboard |
| 3 | Inline health check on SSE tick (Insight 4) | Low | Medium — faster stale state detection |
| 4 | Cost tracking with pricing (Insight 5) | Low | Medium — visible $ cost per workflow |
| 5 | Incremental SSE via lastSeenId (Insight 3) | Low | Medium — more efficient dashboard updates |
| 6 | PreToolUse guards for read-only phases (Insight 7) | Low | Low — cheap safety improvement |
| 7 | Stall timeout detection (Insight 8) | Low | Low — prevents zombie sessions |
| 8 | Checkpoint on PreCompact (Insight 6) | Medium | Medium — survives context limit hits |
| 9 | Inter-agent messaging (Insight 2) | High | Future — only needed for parallel agents |
| 10 | Merge queue (Insight 10) | High | Future — only needed for parallel agents |

## Open Questions

1. **Should we build our own Canopy equivalent or use Go `text/template`?** Canopy adds versioning, inheritance, and schema validation. Go templates give us variable resolution but not the rest. A middle ground: Go templates for variable resolution + a YAML config file defining skill sections and inheritance.

2. **When do we add parallel agents?** The current sequential pipeline is simple and correct. Parallel agents (e.g., multiple builders on different files) would unlock overstory-like speedups but require messaging + merge coordination. This is a significant architectural decision.

3. **Should we parse Claude Code transcripts for token data?** The transcript JSONL files contain detailed token breakdowns. Parsing them would give us much better cost tracking than the single `total_tokens` from RESULT files. But it requires knowing the transcript file location, which varies by platform.

4. **How do we handle Canopy-style prompt versioning?** Overstory uses JSONL records with version counters. We could version our skill templates in Git (they're already tracked) and use the commit hash as the version. Simpler than a custom versioning system.
