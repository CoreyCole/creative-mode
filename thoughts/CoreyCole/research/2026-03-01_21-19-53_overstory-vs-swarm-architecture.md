---
date: 2026-03-01T21:19:53-08:00
researcher: CoreyCole
git_commit: 951b81767fa422d8253ba7cd17e05901885bab5b
branch: feature/agent-swarm
repository: creative-mode
topic: "Overstory vs Creative Mode Swarm — Architecture Comparison"
tags: [research, architecture, swarm, overstory, multi-agent, orchestration]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
---

# Research: Overstory vs Creative Mode Swarm — Architecture Comparison

**Date**: 2026-03-01T21:19:53-08:00
**Researcher**: CoreyCole
**Git Commit**: 951b81767fa422d8253ba7cd17e05901885bab5b
**Branch**: feature/agent-swarm
**Repository**: creative-mode

## Research Question

How does the Overstory multi-agent orchestration framework compare architecturally to our swarm system, and what patterns from Overstory could improve our implementation?

## Summary

Overstory and our swarm solve the same core problem — orchestrating Claude Code sessions for autonomous feature work — but with fundamentally different architectures. **Overstory is peer-to-peer** (no server, the user's Claude Code session IS the orchestrator, agents communicate via SQLite mail). **Our swarm is centralized** (Go server orchestrates everything, agents communicate via HTTP hooks back to the harness). This difference ripples through every design decision.

The systems share: tmux session management, Claude Code skill invocation, JSONL transcript parsing, human review gates, and learning capture. They diverge on: hierarchy (Overstory has coordinator→lead→specialist; we're flat), agent communication (SQLite mail vs HTTP hooks), security (PreToolUse shell guards vs HTTP-based bash deny list), session continuity (checkpoints/handoffs vs none), merge strategy (4-tier conflict resolution vs Graphite stacking), and observability (rich TUI CLI vs web dashboard).

## Detailed Findings

### 1. Orchestration Model

| Dimension | Overstory | Creative Mode Swarm |
|-----------|-----------|---------------------|
| **Architecture** | Peer-to-peer (no server) | Centralized server (Go harness) |
| **Orchestrator** | User's Claude Code session + `ov` CLI + CLAUDE.md | `Manager` struct in `internal/swarmorch/` |
| **Agent runtime** | TypeScript (Bun), zero build step | Go server with HTTP API |
| **Persistence** | 5 SQLite DBs in `.overstory/` | Single SQLite via sqlc in harness |
| **Workflow trigger** | Human instruction or tracker issue | Linear ticket via API or dashboard |
| **Concurrency model** | tmux sessions with stagger delay (2s default) | Goroutines + `sync.Mutex` (or Temporal) |

**Key insight**: Overstory's no-server design eliminates a deployment target but limits real-time observability to CLI polling. Our server model enables live SSE dashboards and HTTP hook integration at the cost of infrastructure dependency.

### 2. Agent Hierarchy

#### Overstory: 3-Tier Code-Enforced Hierarchy

```
Coordinator (depth 0, opus, persistent, can spawn leads only)
  └── Lead (depth 1, opus, owns work stream, can spawn specialists)
        ├── Scout (depth 2, haiku, read-only exploration)
        ├── Builder (depth 2, sonnet, implementation)
        ├── Reviewer (depth 2, sonnet, read-only validation)
        └── Merger (depth 2, sonnet, branch integration)
```

Hierarchy enforced by `validateHierarchy()` in `sling.ts:336-353` — throws `HierarchyError` if coordinator tries to spawn anything other than a lead. Depth limit enforced at `sling.ts:497-502`. Model costs graduated by decision complexity (haiku for scouts, sonnet for builders, opus for leads/coordinator).

Additional limits: `maxConcurrent=25`, `maxSessionsPerRun`, `maxAgentsPerLead=5`, task-level locking, stagger delay.

#### Our Swarm: Flat, Phase-Sequential

```
Manager (Go struct, always running)
  └── Workflow (one per Linear ticket)
        └── Session (one active at a time, sequential phases)
```

One Claude Code session per workflow, advancing through phases sequentially. No parallel agent decomposition within a single ticket. Project workflows decompose into child code workflows, but each child runs independently.

`maxSessions=4` limits total concurrent workflows (not agents within a workflow).

**Gap**: Our swarm can't parallelize within a single ticket. A complex feature gets one builder at a time. Overstory would spawn a lead that fans out to 3-5 builders working different file scopes simultaneously, then merges.

**Counter-argument (from Overstory's own STEELMAN.md)**: Multi-agent parallelism has compounding error rates and merge conflict overhead. Sequential phases with human gates may produce higher quality output for most tickets.

### 3. Agent Communication

#### Overstory: SQLite Mail

- `mail.db` with WAL mode, 12 typed message types, thread support, broadcast groups
- Hook-injected: `UserPromptSubmit` hook runs `ov mail check --inject` before every agent action
- Auto-dispatch mail sent BEFORE tmux session creation (eliminates race condition)
- ~1-5ms per query, concurrent-safe across 25+ agent processes

Protocol types enable structured coordination: `worker_done` (task complete), `merge_ready` (branch verified), `escalation` (severity-graded alerts), `dispatch` (work assignment).

#### Our Swarm: HTTP Hooks + EventBus

- Claude Code sessions POST hook events to harness endpoints (`/api/swarm/hook/*`)
- `CompletionRegistry` + `StartRegistry` use buffered channels for exactly-once signaling
- EventBus publishes `swarm.*` events for dashboard SSE
- No inter-session communication — all coordination goes through the Manager

**Gap**: Our agents can't talk to each other. If we adopt hierarchy (leads spawning builders), we'd need either a mail system or message-passing through the harness API.

**Advantage**: Our centralized model means the Manager has complete state visibility without polling. Overstory's coordinator must poll mail and tracker to understand fleet state.

### 4. Security / Guardrails

#### Overstory: Shell-Based PreToolUse Guards

Three guard layers deployed as `settings.local.json` hooks in each worktree:

1. **Path boundary**: Write/Edit/NotebookEdit restricted to agent's worktree. Bash file-modifying commands (`sed -i`, `echo >`, etc.) path-checked for builders.
2. **Bash danger**: Blocks `git push`, `git reset --hard`, wrong branch naming.
3. **Capability**: Read-only roles (scout, reviewer) have Write/Edit completely blocked. File-modifying Bash commands use whitelist-first approach.

Also blocks Claude Code's native team/task tools (`Task`, `TeamCreate`, `SendMessage`, etc.) — forces agents to use `ov sling` for delegation.

Guards are POSIX shell scripts (no runtime deps). `ENV_GUARD` pattern ensures hooks are no-ops for the user's own session.

#### Our Swarm: HTTP-Based Bash Deny List

- `PreToolUse` hook for `Bash` tool sends command to `/api/swarm/hook/pre-tool-use`
- Server checks against 4 regex patterns: `cargo build/clippy/check`, `go build`, `templ generate`, `just generate`
- Returns `{"permissionDecision": "deny"}` on match

**Gap**: We only deny specific build commands. No path boundary enforcement, no Write/Edit restrictions, no capability-based guards. A swarm session could `rm -rf /` or `git push --force` without intervention. We don't block interactive tools (`AskUserQuestion`, `EnterPlanMode`) which hang headless sessions.

**Priority improvement**: Add path boundary guards and block interactive tools for swarm sessions.

### 5. Session Continuity

#### Overstory: 3-Layer Persistence

1. **Identity** (permanent): `.overstory/agents/{name}/identity.yaml` — CV with sessions completed, expertise domains, recent tasks. Persists across assignments.
2. **Sandbox** (durable): Git worktree + branch. Survives session restarts.
3. **Session** (ephemeral): tmux + pid + checkpoint.

**Checkpoints**: Saved before context compaction (`PreCompact` hook). Contains `progressSummary`, `filesModified`, `pendingWork`.

**Handoffs**: When session dies, `initiateHandoff()` saves checkpoint with `toSessionId: null`. New session calls `resumeFromHandoff()` to load checkpoint and pick up where predecessor left off.

#### Our Swarm: No Session Continuity

- `context_limit` result resumes same phase without attempt increment
- No checkpoint data preserved — new session starts from scratch with only the handoff document from the previous *phase*
- No agent identity across sessions

**Gap**: When a swarm session hits context limits or crashes, we lose all in-progress work context. The next session gets the phase handoff but no information about what the crashed session had already accomplished.

**Improvement opportunity**: Save a checkpoint (files modified, progress summary, pending work) on `PreCompact` hook. Load it as additional context when resuming `context_limit` sessions.

### 6. Merge / Branch Strategy

#### Overstory: Git Worktrees + 4-Tier Merge Queue

Every agent gets its own worktree + branch. Coordinator merges via FIFO queue with escalating resolution:

1. **Tier 1 — Clean merge**: `git merge --no-edit`
2. **Tier 2 — Auto-resolve**: Parse conflict markers, keep incoming (agent) changes
3. **Tier 3 — AI-resolve**: Spawn Claude `--print` with conflicted file, validate output isn't prose
4. **Tier 4 — Reimagine**: Abort merge, send both versions to Claude for reimplementation

Historical patterns from Mulch skip tiers that have historically failed for similar files.

#### Our Swarm: Graphite Stacking

- Single branch per workflow (no worktree isolation)
- PR creation via Graphite CLI (`gt branch create`, `gt stack submit`)
- Project child workflows use `CM_SWARM_STACK_PARENT` and `CM_SWARM_STACK_ORDER` for stacking
- No automated conflict resolution

**Different tradeoffs**: Overstory optimizes for parallel agents that inevitably create merge conflicts. We optimize for sequential phases that never conflict within a workflow. Graphite stacking handles cross-workflow ordering for project decompositions.

### 7. Prompt Composition

#### Overstory: 2-Layer Template + Mulch Expertise

- **Layer 1 (Base)**: Reusable role `.md` files in `agents/` (HOW to be a builder, scout, etc.)
- **Layer 2 (Overlay)**: Generated per-task `CLAUDE.md` in worktree (WHAT to work on). Template at `templates/overlay.md.tmpl` with 22+ `{{VARIABLE}}` substitutions.
- **Mulch priming**: Pre-fetched domain expertise embedded directly in the overlay at spawn time.
- **Beacon**: Structured startup message via tmux `send-keys` with agent name, task ID, and boot instructions.

#### Our Swarm: Go text/template + Embedded Templates

- **Base template**: `base.md.tmpl` with header, handoff context, learning context, conventions, and `"process"` block
- **Phase templates**: Each phase defines `{{ define "process" }}` with phase-specific instructions
- **`RenderPrompt()`**: Parses base + phase template via `template.ParseFS()`, executes with `PromptContext`
- **Skill invocation**: Template rendered to temp file, sent as `/{skill}` via tmux `send-keys`

**Similar patterns, different packaging**: Both use templates with variable substitution. Overstory embeds everything in a single CLAUDE.md file (worktree-scoped). We render prompts on-the-fly and invoke them as skills.

**Gap**: We don't embed historical learnings into prompts as richly as Overstory's Mulch priming. Our `LearningContent` field in `PromptContext` includes recent learnings, but it's not structured by domain or classified by reliability.

### 8. Health Monitoring

#### Overstory: 4-Tier Progressive Watchdog

- **Tier 0**: Mechanical daemon (30s poll). Checks tmux liveness, PID, recorded state. Progressive nudging: warn → nudge → AI triage → kill (at 1-minute intervals).
- **Tier 1**: AI triage (opt-in). Reads last 50 lines of session log, sends to Claude `--print` for classification (`retry`/`terminate`/`extend`).
- **Tier 2**: Monitor agent (persistent Claude Code session). Patrol loop every 2-5 min with anomaly detection.
- **Tier 3**: Human operator.

ZFC principle: observable state (tmux alive?) overrides recorded state (DB says "working"). A live tmux session with recorded "zombie" state → investigate (don't auto-kill).

#### Our Swarm: Basic Stall Detection

- `watchSession()` goroutine polls every 30s for tmux liveness
- `StallMinutes=45` config triggers stall alert
- Crash recovery: if tmux dies without hooks firing, treats as `infra_failure`
- No progressive nudging, no AI triage, no agent-level monitoring

**Gap**: Our stall detection is binary (running or dead). No graduated response, no AI-assisted diagnosis, no anomaly detection across agents.

### 9. Learnings / Expertise

#### Overstory: Mulch (External Structured Expertise)

- JSONL file-based storage per domain (e.g., `architecture.jsonl`, `agents.jsonl`)
- 6 record types: `convention`, `pattern`, `failure`, `decision`, `reference`, `guide`
- 3 classifications: `foundational` (permanent), `tactical` (14-day shelf life), `observational` (30-day)
- Outcome tracking: each record accumulates `success`/`failure`/`partial` outcomes
- Auto-recording at session end via PostToolUse hooks (`ml diff HEAD~1`)
- Priming at session start: all domain expertise injected into overlay
- Merge conflict patterns stored and queried to skip historically-failing resolution tiers

#### Our Swarm: DB-Backed Learnings

- `swarm_learnings` table with category, severity, content, relevance_score
- 5 categories: `plan_issue`, `code_bug`, `pattern`, `post_mortem`, `convention`
- Captured by `captureLearnings()` based on session result + phase
- `getLearningContext()` builds markdown from phase-relevant + critical + ticket-specific learnings
- `decayLearningRelevance()` applies 0.95 multiplier over time
- `GenerateDigest()` produces periodic summary with pattern detection

**Gap**: Our learnings are less structured. No domain separation, no classification tiers, no outcome tracking on individual learnings. The relevance decay is a blunt instrument compared to Mulch's shelf-life-based expiry per classification tier.

**Shared strength**: Both systems capture learnings and feed them back into future sessions. Both have periodic digest/summary generation.

### 10. Observability

#### Overstory: Rich CLI Suite

| Command | Purpose |
|---------|---------|
| `ov dashboard` | Live TUI (2s poll) with agents, feed, tasks, mail, merge queue, metrics |
| `ov trace` | Per-agent chronological event timeline |
| `ov replay` | Multi-agent interleaved event reconstruction |
| `ov costs` | Token/cost analysis by agent, run, capability (supports `--live` and `--self`) |
| `ov feed` | Real-time event stream with `--follow` |
| `ov inspect` | Deep agent view: session state, tmux capture, tool stats, token usage |
| `ov errors` | Aggregated error events with agent grouping |
| `ov doctor` | 11-category health check system with auto-fix |

Backed by `events.db` with 5 indexes including partial index on errors.

#### Our Swarm: Web Dashboard + API

| Endpoint | Purpose |
|----------|---------|
| `/swarm` | Dashboard: workflows table, metrics/health, events, learnings |
| `/swarm/:id` | Workflow detail: phase timeline, sessions, gate review panel |
| `/swarm/events` | SSE: live updates for all `swarm.*` events |
| `/api/swarm/metrics` | Cached metrics (60s) |
| `/api/swarm/health` | Health + stall detection |
| `/api/swarm/learnings` | Learning CRUD |
| Per-session JSONL logs | Tool-level event recording |

**Gap**: No CLI observability tools, no multi-agent replay, no per-agent cost analysis, no `doctor` health check. Our dashboard is web-first which is good for remote access but lacks the deep inspection capabilities of `ov inspect` (tmux pane capture, live tool stats).

**Advantage**: Our SSE-powered dashboard provides real-time updates without polling, and the web UI is more accessible than a TUI for non-developer stakeholders reviewing gates.

### 11. Runtime Adapters

#### Overstory: Pluggable `AgentRuntime` Interface

4 adapters: Claude, Codex, Pi, Copilot. Each implements `buildSpawnCommand`, `deployConfig`, `detectReady`, `parseTranscript`, `buildEnv`. Registry pattern for resolution.

Different security models per runtime: Claude uses PreToolUse hooks, Codex uses OS sandbox, Pi uses TypeScript guard extensions, Copilot has no guard mechanism.

#### Our Swarm: Claude-Only

Hardcoded to Claude Code. `tmux send-keys` with `claude --dangerously-skip-permissions --input-file {path} ; exit`.

**Not a priority gap**: We only need Claude Code. Runtime abstraction adds complexity without clear benefit given our use case.

## Architecture Insights

### Fundamental Design Philosophy Difference

**Overstory**: "Your Claude Code session IS the orchestrator." The human operates the coordinator directly, with the `ov` CLI providing tools. This is a developer-centric, terminal-native design.

**Creative Mode Swarm**: "The harness server IS the orchestrator." The human triggers workflows via dashboard or API. The server manages everything autonomously. This is a service-centric, web-native design.

Neither is strictly better — they serve different use cases. Overstory excels at interactive development sessions where the developer wants fine-grained control. Our swarm excels at autonomous background work triggered by Linear tickets.

### Patterns to Adopt (Priority Order)

1. **Session Checkpoints** (high impact, medium effort)
   - Save checkpoint on `PreCompact` hook: files modified, progress summary, pending work
   - Load checkpoint when resuming `context_limit` sessions
   - Enables graceful recovery from context exhaustion

2. **Security Guards** (high impact, low effort)
   - Block interactive tools (`AskUserQuestion`, `EnterPlanMode`, `EnterWorktree`) in swarm sessions
   - Add path boundary enforcement for Write/Edit in hooks
   - Block `git push --force`, `git reset --hard`, `rm -rf`

3. **Progressive Health Monitoring** (medium impact, medium effort)
   - Replace binary stall detection with graduated levels: warn → nudge → escalate → kill
   - Tmux text nudge at level 1 (send message to stuck session)
   - Optional AI triage at level 2 (read session log, classify failure)
   - Clean escalation tracking per-workflow

4. **Structured Learnings** (medium impact, medium effort)
   - Add domain field to learnings (architecture, testing, deployment, etc.)
   - Add classification tier (foundational/tactical/observational)
   - Expire based on classification rather than flat decay
   - Track success/failure outcomes per learning

5. **CLI Observability** (low-medium impact, medium effort)
   - `just swarm trace <workflow-id>` — event timeline
   - `just swarm costs` — token/cost breakdown by workflow
   - `just swarm doctor` — health check (DB connectivity, tmux availability, Linear auth, etc.)
   - These complement the web dashboard for developer debugging

### Patterns to NOT Adopt

1. **No-server architecture**: Our server enables SSE dashboards, HTTP hooks, Linear/Discord integration, and centralized state. The server is an asset, not a liability.

2. **Full agent hierarchy** (coordinator→lead→specialist): Our sequential phase model is simpler and sufficient for ticket-scoped work. The STEELMAN.md argument against multi-agent parallelism (compounding errors, merge conflicts) applies to most of our tickets.

3. **SQLite mail system**: Our HTTP hooks + EventBus provide the same coordination without another database. Only needed if we adopt agent hierarchy.

4. **Custom YAML parser**: We use standard Go libraries. No need for hand-rolled parsers.

5. **4-tier merge resolution**: Graphite stacking handles our branch strategy. We don't have parallel agents creating merge conflicts.

6. **Runtime adapter abstraction**: We only use Claude Code. YAGNI.

## Code References

### Overstory
- `context/overstory/src/commands/sling.ts` — 14-step spawn pipeline, hierarchy validation
- `context/overstory/src/agents/overlay.ts` — Two-layer instruction generation
- `context/overstory/src/agents/hooks-deployer.ts` — PreToolUse shell guard generation
- `context/overstory/src/agents/identity.ts` + `checkpoint.ts` + `lifecycle.ts` — 3-layer persistence
- `context/overstory/src/mail/store.ts` — SQLite mail schema and operations
- `context/overstory/src/merge/resolver.ts` — 4-tier conflict resolution
- `context/overstory/src/watchdog/daemon.ts` + `health.ts` — Progressive watchdog
- `context/overstory/src/mulch/client.ts` — Structured expertise system
- `context/overstory/src/metrics/transcript.ts` — Token cost tracking
- `context/overstory/src/config.ts` — 3-layer config merge with custom YAML parser

### Creative Mode Swarm
- `harness/internal/swarm/statemachine.go` — Phase state machine
- `harness/internal/swarm/enums.go` — Domain enums
- `harness/internal/swarm/env.go` — Session environment config
- `harness/internal/swarmorch/manager.go` — Orchestrator (workflow lifecycle, gates, sessions)
- `harness/internal/swarmorch/hooks.go` — Claude Code hook config + bash deny list
- `harness/internal/swarmorch/learnings.go` — Learning capture and context retrieval
- `harness/internal/swarmorch/alerts.go` — Discord alert manager with dedup
- `harness/internal/swarmorch/project.go` — Project workflow child decomposition
- `harness/internal/swarm/prompt/` — Go text/template prompt composition
- `harness/internal/swarm/transcript/` — JSONL parsing + token cost tracking
- `harness/internal/server/swarm_hooks.go` — Server-side hook handlers
- `harness/internal/server/swarm_dashboard.go` — Dashboard SSE + gate actions

## Open Questions

1. **How well does Overstory's hierarchy actually perform?** The STEELMAN.md documents concerns about compounding errors and cost amplification. Real-world performance data would help decide if hierarchy is worth the complexity.

2. **What's the right checkpoint granularity?** Overstory saves on PreCompact. Should we also checkpoint on phase completion (redundant with handoffs) or on specific tool milestones?

3. **Should learnings be ticket-scoped or global?** Overstory's Mulch is global across all agents. Our learnings are ticket-scoped but feed into global context. Which produces better future sessions?

4. **Is AI triage worth the token cost?** Overstory defaults to `tier1Enabled: false`. The cost of spawning Claude to diagnose a stuck session may not be justified if most stalls resolve with a simple tmux nudge.
