---
date: 2026-03-01T21:49:06-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 951b81767fa422d8253ba7cd17e05901885bab5b
branch: feature/agent-swarm
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-03-01_21-37-45_overstory-swarm-improvements.md
status: complete
type: plan_review
---

# Plan Review: Overstory-Inspired Swarm Improvements

### Summary

Solid plan with well-scoped phases. The 5 improvements (security guards, session checkpoints, structured learnings, progressive health, CLI observability) are the right picks from the Overstory comparison. However, the checkpoint mechanism has a critical design flaw (relies on LLM instruction compliance), the health monitoring references a function that may not exist, and the plan misses several high-value Overstory patterns that the user explicitly asked about — particularly ZFC health reconciliation, incremental SSE, auto-insight extraction, and per-model cost tracking.

### Critical Issues (Must Address Before Implementation)

1. **Checkpoint save mechanism is instruction-only — no enforcement**
   - Problem: Phase 2 adds instructions to `base.md.tmpl` telling the Claude Code session to "periodically write a JSON checkpoint file" via `echo '...' > /tmp/swarm-checkpoint-{sessionID}.json`. This is purely advisory — the LLM may or may not comply. Overstory uses hook-triggered checkpoints: the `PreCompact` hook fires *automatically*, and the system saves whatever state it can extract. Our plan puts the burden on the LLM following instructions reliably.
   - Risk: Sessions that don't write the checkpoint file get no benefit from the entire Phase 2 infrastructure. The PreCompact hook fires and reads... nothing. The hardest sessions to recover from (complex implementations where the agent is deep in multi-file changes) are exactly the ones most likely to forget the checkpoint instruction.
   - Suggestion: Instead of relying on the session to self-report progress, extract checkpoint data from observable state when PreCompact fires:
     - Run `git diff --name-only` to get `files_modified` (guaranteed accurate)
     - Read the last N lines of the JSONL session log for `progress` summary
     - Use `git diff --stat` for a quantitative summary of changes
     - Store the result file's partial content if available
     - Still accept the session's self-reported checkpoint as supplementary data, but don't depend on it exclusively

2. **`detectAndAlertStalls()` may not exist in current codebase**
   - Problem: Phase 4 says "Replace `detectAndAlertStalls()` call in maintenance loop with `monitorWorkflowHealth()`." I could not find a `detectAndAlertStalls()` function in `manager.go`, `health.go`, or any swarmorch file. Stall detection currently happens in `queryActiveWorkflows()` in `health.go:144-191` (inline in the health endpoint query, comparing `updated_at` against `stallCheckMinutes=45`). The maintenance loop (`StartMaintenance`) may call `decayLearningRelevance()` but I could not verify it calls a stall detection function.
   - Risk: If the function doesn't exist, the plan's Phase 4 instructions will confuse the implementer. More importantly, the actual stall detection mechanism (inline in `queryActiveWorkflows`) is different from what the plan assumes — it's a query-time check, not a periodic function.
   - Suggestion: Verify the exact maintenance loop contents. The graduated monitoring should likely be a new periodic function called from `StartMaintenance`, not a replacement for something that may not exist. And `queryActiveWorkflows` should be updated to use the escalation tracker's level for the `Stalled` field.

3. **tmux `send-keys` nudge may corrupt Claude Code session state**
   - Problem: Phase 4's `nudgeSession()` sends text via `tmux send-keys -t {session} "{message}" Enter`. But Claude Code sessions run via `claude --dangerously-skip-permissions --input-file`. They aren't interactive prompts waiting for user input — they're autonomous sessions processing a skill file. Sending text via `send-keys` injects characters into whatever input buffer is active, which could corrupt an ongoing bash command, interfere with Claude Code's internal state, or produce unpredictable behavior.
   - Risk: The nudge could cause the session to fail in ways that are harder to diagnose than the stall it was trying to fix. Overstory's own documentation warns about this and uses file markers instead of direct tmux keys for non-interactive agents.
   - Suggestion: Instead of tmux `send-keys`, use a file-based signal that the session can check. Write a nudge file to `/tmp/swarm-nudge-{sessionID}` and add a `UserPromptSubmit` hook that checks for this file and injects the message into Claude's next turn as system context. Alternatively, use `tmux send-keys` only if the session is at a prompt (check tmux pane content for the `>` indicator first).

### Concerns (Should Address)

1. **CLI tool DB access won't work with Docker dev setup**
   - Observation: Phase 5's `swarmctl` opens the SQLite DB directly for read-only queries. But on macOS, the harness runs inside Docker (per CLAUDE.md: "On macOS, always run the harness in Docker"). The DB file is inside the container. The CLI on the host can access it via bind mount, but the DB path varies by environment — it's `data/harness.db` inside the container but maps to a host path via volume.
   - Suggestion: Either (a) have `swarmctl` accept a `--db` flag with the DB path, (b) default to the Docker volume mount path, or (c) query the harness HTTP API instead of the DB directly. Option (c) is cleanest — the health, metrics, and events endpoints already exist. The CLI could just format API responses.

2. **RecordLearningOutcome SQL has parameter binding issue**
   - Observation: The SQL uses `?` four times for the outcome type in CASE expressions, plus once for the ID. sqlc would need the caller to pass the outcome string 4 times and the ID once: `RecordLearningOutcome(ctx, outcome, outcome, outcome, outcome, id)`. This is error-prone.
   - Suggestion: Use a single `@outcome` named parameter or restructure the query to reference the parameter once (e.g., use a CTE or bind once to a local variable with `WITH params AS (SELECT ? AS outcome)`).

3. **Migration file ambiguity between Phase 2 and Phase 3**
   - Observation: Phase 2 proposes `009_session_checkpoints.sql`. Phase 3 says "same migration as Phase 2 — OR a separate `010_structured_learnings.sql` if phases are done separately." This ambiguity means two implementers working on different phases could collide on migration numbering.
   - Suggestion: Decide now. If phases will be implemented sequentially by one person, use one migration. If they might be parallel or out of order, assign numbers now: `009_session_checkpoints.sql`, `010_structured_learnings.sql`.

4. **Phase ordering and dependencies are implicit**
   - Observation: The 5 phases are presented sequentially but their dependencies aren't stated. Phase 1 (guards) and Phase 5 (CLI) are fully independent. Phase 2 (checkpoints) and Phase 3 (learnings) share a potential migration. Phase 4 (health) is independent. Can they be built in parallel? Which must be sequential?
   - Suggestion: Add an explicit dependency graph. This helps if multiple sessions/developers work on different phases.

5. **`tags TEXT` column discovery note is misleading**
   - Observation: The plan's "Key Discoveries" section says the `tags` column on `swarm_learnings` is "unused — can repurpose for domain." But Phase 3 adds a NEW `domain TEXT` column via `ALTER TABLE` rather than repurposing `tags`. The discovery is irrelevant to the implementation.
   - Suggestion: Minor, but either repurpose `tags` as `domain` (avoids adding a column) or remove the misleading discovery note.

6. **Escalation tracker is in-memory only — lost on harness restart**
   - Observation: Phase 4's `EscalationTracker` uses an in-memory map (`map[string]EscalationLevel`). If the harness restarts, all escalation state is lost. A workflow that was at `EscalationWarn` restarts at `EscalationNone` and must re-escalate through all tiers.
   - Suggestion: This is probably acceptable given the harness restarts infrequently and the escalation re-builds within 30-90 minutes. But document this as a known limitation. If it becomes a problem, persist escalation level in the `swarm_workflows` table.

### Questions (Need Clarification)

1. Should the checkpoint mechanism also capture `git stash` state? If the session has uncommitted changes that don't survive a restart, those changes are lost even with a checkpoint document describing them.

2. For Phase 4's kill escalation at 90 minutes — what happens to the workflow? The plan says "watchSession will detect death and handle completion" — but with what result? `infra_failure`? `timeout`? The state machine treats these differently (infra_failure retries, timeout is terminal).

3. The plan says "No dashboard UI changes" — but the escalation level (Phase 4) and checkpoint status (Phase 2) are useful dashboard information. Should the dashboard at least display these fields when they exist?

4. For Phase 5's `swarmctl` — should it be buildable on macOS (where `go build` is denied by settings.json)? The justfile recipe uses `go run` which is also blocked. How do developers actually run this locally?

### Suggestions (Nice to Have)

1. **Adopt ZFC principle for dashboard health (from Overstory)**
   - The earlier research (Insight 4) identified that our dashboard trusts DB state without independently verifying tmux liveness. Overstory's "Zero Failure Crash" principle says observable state (tmux alive?) should override recorded state (DB says "running"). Adding a tmux liveness check during SSE ticks would catch stale states within 1-2 seconds instead of waiting up to 30s for `watchSession` to detect it.
   - This is low-effort, medium-impact, and could be Phase 6 or folded into Phase 4.

2. **Incremental SSE via `lastSeenId` (from Overstory)**
   - The earlier research (Insight 3) identified that the dashboard re-queries all events on each SSE tick. Overstory's `EventBuffer` pattern tracks `lastSeenId` and queries `WHERE id > $lastSeenId`. This is a significant efficiency improvement for long-running dashboards.
   - Low effort, independent of the 5 phases.

3. **Auto-insight extraction from session transcripts**
   - Overstory auto-analyzes completed session transcripts to extract tool profiles (top tools, error rates), file profiles (hot files), and inferred domain expertise. Our `internal/swarm/transcript/` package already parses transcripts for token counting — extending it to extract tool statistics and file change patterns would feed directly into Phase 3's structured learnings.
   - Could replace the instruction-based learning capture with mechanical extraction.

4. **Per-model cost pricing for Phase 5's `costs` command**
   - The plan's `costs` subcommand shows token counts but the pricing calculation needs per-model rates. Overstory maintains a `MODEL_PRICING` map with input/output/cache rates per model. Without this, the "Est. Cost" column in the example output is hand-waved.
   - The transcript parser (`internal/swarm/transcript/pricing.go`) may already have this — worth checking before implementing.

5. **Dispatch overrides for workflow customization**
   - Overstory's coordinator can inject per-task overrides (`SKIP_REVIEW`, `MAX_AGENTS`) that modify agent behavior without new agent types. For our swarm, this could mean: simple tickets skip `plan_review`, well-understood tickets skip `research`, bug fixes go directly to `implement`. Currently this requires changing `SwarmConfig` globally.
   - Could be implemented as per-workflow config overrides in the start API.

6. **Activity-based staleness thresholds**
   - Overstory uses different staleness thresholds for different agent types (coordinators expected to be idle waiting for mail, builders expected to be actively working). Phase 4 uses fixed thresholds for all workflows. But `research` sessions are expected to be slower (web searches, reading) while `implement` sessions should show steady tool activity.
   - Consider per-phase thresholds or factor in recent tool activity (from PostToolUse hooks) as a staleness signal.

### What's Good

- **Correct prioritization**: Security guards (Phase 1) first is the right call — highest impact, lowest effort. The plan correctly identifies that only 4 bash deny patterns and no tool blocking is a real security gap.
- **Builds on existing infrastructure**: Each phase extends existing code (hooks, health, learnings) rather than building new subsystems. The PreCompact hook already fires, the learning table already has `tags`, the `watchSession` goroutine already polls tmux.
- **Clear "What We're NOT Doing" section**: Explicitly declining agent hierarchy, SQLite mail, 4-tier merge, runtime adapters, and AI triage implementation. This prevents scope creep and shows the planner understood the Overstory patterns deeply enough to reject the ones that don't fit.
- **Specific code references with line numbers**: The plan references exact file locations and line numbers, making implementation straightforward. The code examples are concrete enough to implement directly.
- **Test-first success criteria**: Each phase has both automated and manual verification criteria. The test schema update in `manager_test.go` is correctly identified as a dependency.
- **Performance considerations section**: Noting that checkpoint saves happen on an already-fired hook, escalation tracking is in-memory, CLI opens DB read-only — shows awareness of operational impact.

### Recommended Next Steps

1. Resolve the 3 critical issues before starting implementation:
   - Redesign checkpoint save to use observable state (git diff) rather than depending on LLM compliance
   - Verify the maintenance loop's actual stall detection mechanism and adjust Phase 4 accordingly
   - Replace tmux `send-keys` nudge with a file-based or hook-injected mechanism
2. Fix migration numbering: assign `009` and `010` explicitly
3. Decide whether `swarmctl` queries the DB directly or calls the HTTP API
4. Consider adding 1-2 of the "missed Overstory patterns" (ZFC health reconciliation, incremental SSE) as Phase 6 — they're low-effort and address real gaps identified in the earlier research
5. Verify the transcript pricing module (`internal/swarm/transcript/pricing.go`) — it may already support per-model cost calculations needed by Phase 5
