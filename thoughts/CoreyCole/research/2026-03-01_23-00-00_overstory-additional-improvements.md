---
date: 2026-03-01T23:00:00-08:00
researcher: CoreyCole
branch: feature/agent-swarm
repository: creative-mode
topic: "Overstory Deep Dive — Additional Swarm Improvements Beyond Existing Plan"
tags: [research, architecture, swarm, overstory, improvements]
status: complete
depends_on: thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md
depends_on_plan: thoughts/CoreyCole/plans/2026-03-01_21-37-45_overstory-swarm-improvements.md
---

# Additional Overstory-Inspired Swarm Improvements

**Previous work**: The [architecture comparison](2026-03-01_21-19-53_overstory-vs-swarm-architecture.md) identified 5 priority improvements. The [implementation plan](../plans/2026-03-01_21-37-45_overstory-swarm-improvements.md) scoped 6 phases. The [plan review](../reviews/2026-03-01_21-49-06_overstory-swarm-improvements_review.md) flagged 3 critical issues and 6 suggestions.

This document identifies **additional improvements** discovered through deeper code analysis of both codebases, beyond what the existing plan covers.

## Category 1: Wire Existing But Disconnected Infrastructure

These are the highest-ROI items because the code already exists — we just need to connect the dots.

### 1.1 Wire Transcript Token Extraction + Cost Tracking

**Current state**: The `internal/swarm/transcript/` package has:
- `ParseFile()` — scans JSONL, extracts per-bucket tokens (input, output, cache_read, cache_creation), detects model
- `DiscoverTranscript()` — finds the most recent `.jsonl` in `~/.claude/projects/` after a given timestamp
- `EstimateCost()` — per-million-token pricing for opus/sonnet/haiku

Migration `007_prompt_versions_and_tokens.sql` added columns to `swarm_sessions`:
- `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens`
- `model_used`, `estimated_cost_usd`, `prompt_version_id`

**The gap**: `handleSessionComplete` in `manager.go` never calls any of these. `CompleteSwarmSession` only writes `total_tokens`. The per-bucket columns and cost are never populated.

**Fix**: In `handleSessionComplete`, after `SendSessionEnd` fires:
1. Call `transcript.DiscoverTranscript(projectRoot, sessionStart)`
2. Call `transcript.ParseFile(transcriptPath)`
3. Call `transcript.EstimateCost(usage)`
4. Write to DB via a new `CompleteSwarmSessionWithTokens` query

**Effort**: Low (~30 lines of new code + 1 SQL query update)
**Impact**: High — gives us per-session cost visibility for free

### 1.2 Wire Prompt Version Tracking

**Current state**: `swarm_prompt_versions` table exists with `(phase, content_hash)` unique constraint. `UpsertSwarmPromptVersion` and `GetSwarmPromptVersion` queries are generated. `prompt.RenderPrompt()` returns `{Content, ContentHash}` with SHA-256. None of this is called from `manager.go`.

**The gap**: When spawning a session, we invoke `/swarm-{phase}` as a skill via `sendSkillPrompt`. We never call `RenderPrompt` or `UpsertSwarmPromptVersion`.

**Fix**: Before session spawn:
1. Call `prompt.RenderPrompt(phase, ctx)` to get the content hash
2. Call `UpsertSwarmPromptVersion(phase, contentHash, content)` to register
3. Pass `prompt_version_id` to the session creation

This enables: "which prompt version was this session using?" and "did the prompt change between attempts?"

**Effort**: Low (~15 lines)
**Impact**: Medium — prompt regression tracking

### 1.3 Populate Learning Tags Field

**Current state**: `swarm_learnings.tags TEXT` column exists. `createLearning()` in `learnings.go` never populates it. The digest's `countTags()` reads tags but they're always empty. The `convention` category has no write path.

**Fix**: Derive tags from context:
- Phase name as a tag (e.g., `research`, `implement`, `verify`)
- Ticket type if available (e.g., `bug`, `feature`)
- Template name if available (e.g., `3d`, `2d`, `boardgame`)

Also wire a `convention` learning category capture path for successful patterns that repeat across 3+ tickets.

**Effort**: Low
**Impact**: Medium — better learning context injection filtering

---

## Category 2: Security Hardening (Beyond Plan Phase 1)

The existing plan covers interactive tool blocking and basic path boundaries. These are additional guards from Overstory that we're missing.

### 2.1 Expanded Bash Deny Patterns

Overstory's `guard-rules.ts` blocks 30+ patterns. Our 4 are project-specific (cargo/go/templ/just build). Missing critical patterns:

```
# Destructive git operations
git\s+push                    # agents should never push (PR phase uses specific gt commands)
git\s+reset\s+--hard          # destroys uncommitted work
git\s+clean\s+-[fd]           # removes untracked files

# File destruction
\brm\s+-rf?\s                 # recursive delete
\bsudo\s                      # privilege escalation

# Package managers (could modify lock files, install malicious deps)
\bnpm\s+install\b
\bbun\s+install\b
\bbun\s+add\b

# Runtime eval (bypass shell guards)
\bnode\s+-e\b
\bpython3?\s+-c\b
\bcurl\s+.*\|\s*(sh|bash)\b  # pipe-to-shell attacks

# Secret exfiltration patterns
\bcurl\b.*\$ANTHROPIC_API_KEY
\becho\b.*\$(ANTHROPIC_API_KEY|LINEAR_API_KEY|GRAPHITE_TOKEN|DISCORD_BOT_TOKEN)
```

**Effort**: Low — just add regex patterns to `swarmDenyPatterns`
**Impact**: High — prevents accidental or adversarial damage

### 2.2 Write/Edit Path Boundary Guards

Overstory enforces that Write/Edit tools can only target files within the agent's worktree. Our swarm sessions run in the project root with no path restrictions.

**Approach**: Add `PreToolUse` hooks for `Write` and `Edit` that check `file_path` starts with the project root. The hook handler in `server/swarm_hooks.go` already receives the full tool input JSON — extend it to inspect file paths for these tools.

**Effort**: Medium — need to register additional PreToolUse matchers and add path validation logic
**Impact**: High — prevents writes to `/etc/`, `~/.ssh/`, or other system paths

### 2.3 Secret Redaction in Logs and Events

Overstory's `sanitizer.ts` redacts API keys from logs using regex patterns:
```
sk-ant-[a-zA-Z0-9_-]+        # Anthropic API keys
github_pat_[a-zA-Z0-9_]+     # GitHub PATs
ghp_[a-zA-Z0-9]+             # GitHub tokens
Bearer\s+[a-zA-Z0-9._-]+    # Bearer tokens
ANTHROPIC_API_KEY=[^\s]+      # Env var leaks
```

Our JSONL logs and EventBus events currently pass through raw tool args which could contain secrets if a session runs `env`, `printenv`, or reads `.env` files.

**Effort**: Low — add a `sanitize(string) string` function with regex patterns, apply to log entries
**Impact**: Medium — prevents accidental credential exposure in logs/dashboard

---

## Category 3: Observability Patterns

### 3.1 Tool Arg Filtering for Events

Overstory's `tool-filter.ts` reduces 20KB event payloads to ~200 bytes by keeping only identifying fields per tool type:
- `Bash` → keep `command` (truncated to 80 chars) and `description`
- `Read` → keep `file_path`, `offset`, `limit`
- `Write`/`Edit` → keep `file_path` only (drop content/old_string/new_string)
- `Grep`/`Glob` → keep `pattern`, `path`

Our PostToolUse events publish full tool args to the EventBus (which powers the SSE dashboard). Filtering would:
- Reduce SSE payload sizes (matters for mobile dashboard access)
- Make the events feed more readable
- Prevent accidental secret exposure in the dashboard

**Effort**: Low — port the filter map pattern from overstory
**Impact**: Medium

### 3.2 Incremental SSE with Event Sequence Numbers

**Current gap**: The dashboard SSE handler subscribes to `EventBus.SubscribeGlobal()` and processes events live. On reconnect, all events from the disconnection window are lost.

**Overstory pattern**: `EventBuffer` tracks `lastSeenId` per client. Server sends `id:` field in SSE events. Client sends `Last-Event-ID` header on reconnect. Server replays events since that ID.

**Implementation**:
1. Add auto-increment sequence number to EventBus events
2. Maintain a ring buffer of recent events (last 1000)
3. Read `Last-Event-ID` from SSE request headers
4. On reconnect, replay events from `lastSeenId` through current
5. Include `id:` field in SSE event stream

**Effort**: Medium
**Impact**: Medium — dashboard reliability during network blips

### 3.3 ZFC Health Reconciliation in Dashboard SSE

**Current gap**: The dashboard trusts DB state (workflow status, session status). If a tmux session dies without hooks firing, the dashboard shows stale "running" state until the 30-second watchSession poll detects it.

**Overstory's ZFC principle**: Observable state (tmux alive?) overrides recorded state (DB says "running"). Signal priority: tmux liveness > PID liveness > recorded state.

**Implementation**: During SSE tick (already fires on a heartbeat timer):
1. For each "running" session, check `tmux has-session -t {name}`
2. If tmux dead but DB says running → immediately update state, emit event
3. This catches stale states within 1-2 seconds instead of 30s

**Effort**: Low
**Impact**: Medium — dashboard accuracy

---

## Category 4: Health Monitoring Enhancements

### 4.1 Per-Phase Stall Thresholds

**Current state**: Single 45-minute threshold for all phases.

**Problem**: Research sessions legitimately run 60+ minutes (web searches, deep codebase exploration). PR creation should complete in <10 minutes. Plan review is a pass/fail check that should complete in <5 minutes.

**Overstory's approach**: Per-capability thresholds (coordinators expected idle, builders expected active). We should do per-phase:

| Phase | Expected Duration | Stall Threshold |
|---|---|---|
| research | 15-60 min | 75 min |
| code_plan | 10-30 min | 45 min |
| plan_review | 3-10 min | 20 min |
| implement | 15-60 min | 75 min |
| verify | 5-15 min | 30 min |
| pr | 3-10 min | 20 min |

**Effort**: Low — add a `phaseStallThresholds map[Phase]time.Duration` and use it in health checks
**Impact**: Medium — reduces false positives (research) and catches real stalls faster (pr)

### 4.2 Activity-Based Staleness Signal

**Current state**: Stall detection uses `updated_at` on `swarm_workflows`, which only advances on phase transitions. A session that's actively calling tools but hasn't transitioned phases won't update this timestamp.

**Overstory pattern**: Factor in recent tool activity from PostToolUse hooks as a staleness signal.

**Implementation**: Track `last_tool_activity` timestamp on `swarm_sessions`. Update it on each PostToolUse hook event. Use `MAX(workflow.updated_at, session.last_tool_activity)` for stall calculations.

**Effort**: Low — add column + update in hook handler
**Impact**: Medium — prevents false stall alerts during long but active phases

### 4.3 Doctor/Health Check System (API-based)

Overstory has 11 categories of health checks with auto-fix: dependencies, config, structure, databases, consistency, agents, merge, logs, version, ecosystem, providers.

Our `/api/swarm/health` only does stall detection. A proper health check endpoint would verify:
- DB connectivity + schema version
- tmux availability (`tmux -V`)
- Linear auth (`LINEAR_API_KEY` set + valid)
- Graphite auth (`GRAPHITE_TOKEN` set)
- Discord connectivity (`DISCORD_SWARM_CHANNEL_ID` set)
- Orphaned tmux sessions (sessions in DB but not in tmux, or vice versa)
- Disk space for logs directory
- Learning decay health (are learnings being generated? decayed?)
- Prompt template integrity (do embedded templates parse?)

**Effort**: Medium
**Impact**: Medium — proactive issue detection, especially useful for VPS ops

---

## Category 5: Learning System Enhancements

### 5.1 Auto-Insight Extraction from Transcripts

Overstory's `analyzer.ts` extracts structured insights from completed sessions:
- **Tool workflow profile**: Read-heavy vs write-heavy vs bash-heavy (from tool stats)
- **Hot files**: Files with 3+ edits indicate complexity (from Edit/Write events)
- **Error patterns**: Which tools failed and how often

Our `transcript/` package already parses JSONL. Extending it to extract tool statistics and file change patterns would feed into the learning system mechanically rather than relying on the session's self-reported result summary.

**Implementation**:
1. Add `ExtractToolStats(entries []TranscriptEntry) []ToolStat` to transcript package
2. Add `ExtractHotFiles(entries []TranscriptEntry) []HotFile`
3. Call after session completion alongside token extraction (Category 1.1)
4. Generate structured learnings: "Session for {ticket} was {workflow-type}, top tools: {list}, hot files: {list}"
5. If error count > 0, capture as `code_bug` learning with tool names

**Effort**: Medium
**Impact**: Medium — mechanical insight extraction is more reliable than LLM self-reporting

### 5.2 Domain-Based Learning Classification

Overstory's Mulch uses 6 record types and 3 classification tiers:
- Types: `convention`, `pattern`, `failure`, `decision`, `reference`, `guide`
- Tiers: `foundational` (permanent), `tactical` (14-day shelf life), `observational` (30-day)
- Outcome tracking: each record accumulates `success`/`failure`/`partial`

Our current decay is a flat `*= 0.95` per hour for all learnings. Foundational learnings (e.g., "always use just check, never direct go build") shouldn't decay. Tactical learnings (e.g., "the 2D template currently has a bug in room transitions") should decay faster.

**Implementation**: Add `classification TEXT CHECK(classification IN ('foundational', 'tactical', 'observational'))` to `swarm_learnings`. Use different decay rates:
- `foundational`: no decay
- `tactical`: `*= 0.95` per hour (current rate)
- `observational`: `*= 0.90` per hour (faster decay)

Auto-classify based on category: `convention` → foundational, `pattern` → tactical, `code_bug` → observational, `post_mortem` → tactical, `plan_issue` → observational.

**Effort**: Low
**Impact**: Medium — foundational conventions stop disappearing

---

## Category 6: Workflow Configuration

### 6.1 Per-Workflow Override System

Overstory's coordinator can inject per-task overrides that modify agent behavior without new agent types. For our swarm, this means:

- Simple bug fixes skip `research` phase (go directly to `code_plan`)
- Well-understood tickets skip `plan_review` gate
- Trivial changes skip `verify` phase
- Emergency fixes skip all gates

**Implementation**: Add optional `overrides` JSON field to workflow start API:
```json
{
  "ticket_id": "CM-123",
  "overrides": {
    "skip_phases": ["research"],
    "skip_gates": ["plan_review"],
    "stall_threshold_minutes": 30
  }
}
```

The state machine's `NextPhase()` would check `skip_phases` before transitioning.

**Effort**: Medium
**Impact**: Medium — enables workflow-type-specific optimizations

---

## Priority Ranking

| # | Improvement | Effort | Impact | Dependencies |
|---|---|---|---|---|
| 1.1 | Wire transcript token extraction + cost | Low | High | None |
| 2.1 | Expanded bash deny patterns | Low | High | None |
| 4.1 | Per-phase stall thresholds | Low | Medium | None |
| 4.2 | Activity-based staleness signal | Low | Medium | None |
| 2.3 | Secret redaction in logs | Low | Medium | None |
| 1.3 | Populate learning tags | Low | Medium | None |
| 3.3 | ZFC health reconciliation in SSE | Low | Medium | None |
| 2.2 | Write/Edit path boundary guards | Medium | High | Plan Phase 1 |
| 3.1 | Tool arg filtering for events | Low | Medium | None |
| 5.2 | Domain-based learning classification | Low | Medium | Plan Phase 3 |
| 1.2 | Wire prompt version tracking | Low | Medium | None |
| 3.2 | Incremental SSE | Medium | Medium | None |
| 5.1 | Auto-insight extraction | Medium | Medium | 1.1 |
| 4.3 | Doctor health check system | Medium | Medium | None |
| 6.1 | Per-workflow overrides | Medium | Medium | None |

The top 7 items are all **low effort** and can be done independently of the existing plan phases. Items 1.1 and 2.1 are the highest-ROI: they wire up code that already exists or add obvious safety patterns.

## Relationship to Existing Plan

These improvements are **additive** to the 6-phase plan. They don't conflict with any planned work. Several naturally fit as extensions:

- Items 2.1, 2.2, 2.3 extend **Phase 1 (Security Guards)**
- Items 1.1, 1.2, 3.1 could become a new **Phase 7 (Wiring)** — connecting existing disconnected code
- Items 4.1, 4.2, 3.3 extend **Phase 4 (Progressive Health)**
- Items 5.1, 5.2, 1.3 extend **Phase 3 (Structured Learnings)**
- Items 3.2, 4.3 extend **Phase 5 (CLI Observability)** (or rather, general observability)
- Item 6.1 is independent and could be a standalone phase
