---
date: 2026-02-28T18:42:59-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 2c68eddfcea832d3ce6c15456fe0ab9cd2f82b04
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-28_17-30-00_agent-swarm-primitives-v3.md
status: complete
type: plan_review
---

# Plan Review: Agent Swarm Primitives v3

### Summary

A mature, well-evolved plan that makes strong architectural choices (SQLite state machine, short-lived Temporal workflows, flat composable skills). The v3 iteration addresses most concerns from the v1 review — state persistence, flat skills, dry-run support, resume capability, and rate limit awareness. However, the plan has a critical mismatch between how it proposes to detect Claude Code session completion (polling `tmux has-session`) and how the existing codebase actually works (hook-driven via POST to `/api/claude-event`). Several other issues around Temporal anti-patterns, missing infrastructure dependencies, and divergence from the Chestnut flowchart warrant attention.

### Critical Issues (Must Address Before Implementation)

1. **Session completion detection: polling vs hooks mismatch**
   - Problem: The `RunClaudeSession` activity (Phase 4, activities.go) polls `tmuxHasSession()` in a 10-second loop to detect when a Claude Code session finishes. But the existing codebase (`harness/internal/claude/claude.go`) uses a completely different model — hook-driven completion where Claude's `.claude/hooks/` scripts POST events to `POST /api/claude-event`, which triggers `BuildCheckpoint` asynchronously. The tmux session is killed *by the hook handler*, not detected as missing by polling.
   - Risk: Polling creates a race condition — the tmux session disappears, but the RESULT comment may not yet be written to Linear. The activity reads the comment immediately after the poll loop exits, potentially before the skill has finished writing it. The existing hook-based pattern avoids this because the hook fires *after* Claude's output is complete.
   - Suggestion: Choose one of: (a) Keep polling but add a delay + retry for reading the RESULT comment (fragile). (b) Adapt the existing hook pattern — have the skill's completion hook POST to a new endpoint (e.g., `POST /api/swarm/session-complete`) which signals a Go channel that the activity is waiting on. This is more robust and consistent with the existing architecture. (c) Have skills write a sentinel file (e.g., `.swarm-result.json`) to a known path, and poll for that file instead of tmux session existence.

2. **`ProcessTicketQueue` spawns workflows from within an activity — Temporal anti-pattern**
   - Problem: The `ProcessTicketQueue` activity calls `a.temporalClient.ExecuteWorkflow()` to spawn new `SessionWorkflow`s. Starting workflows from within activities is a Temporal anti-pattern — if the activity fails *after* spawning the workflow but *before* returning success, the workflow is orphaned and on retry, a duplicate will be spawned.
   - Risk: Duplicate Claude Code sessions consuming VPS resources. The `swarm-verify` queue (concurrency 1) would correctly prevent duplicate verifications, but the `swarm-general` queue (concurrency 3) would allow duplicates.
   - Suggestion: Refactor `HeartbeatWorkflow` to be the orchestrating workflow function (not just an activity runner). Have `ProcessTicketQueue` return a list of `SessionParams` to spawn, and have `HeartbeatWorkflow` call `workflow.ExecuteChildWorkflow()` for each. Child workflows have proper lifecycle management — if the parent is cancelled, children are too.

3. **sqlc won't generate typed enums from CHECK constraints**
   - Problem: The plan states "sqlc generates typed Go constants" from CHECK constraint enums. This is incorrect for SQLite. The sqlc config (`harness/sqlc.yaml`) shows that even `checkpoints.status` (which has CHECK constraints) is typed as plain `string` (line 12: `go_type: string`). sqlc for SQLite does not inspect CHECK constraints to generate Go enum types.
   - Risk: Plan design assumes compile-time safety for phase/status transitions that won't exist. String typos in phase names would be runtime bugs.
   - Suggestion: Define Go `const` blocks manually in `statemachine.go` (the plan already shows this in the code examples). Don't rely on sqlc for this. The CHECK constraints are still valuable as database-level validation.

### Concerns (Should Address)

1. **`temporal-cli` availability in nixpkgs is unverified**
   - Observation: The plan says "Add `temporal-cli` to `flake.nix` packages." The flake.nix currently supports `aarch64-linux` and `x86_64-linux`. Temporal's CLI may not be packaged in nixpkgs (it's a Go binary distributed as GitHub releases). If it's not in nixpkgs, you'd need a custom derivation or `buildGoModule`.
   - Suggestion: Verify with `nix search nixpkgs temporal` on the VPS before committing to this approach. Fallback: install via `go install go.temporal.io/server/cmd/temporal@latest` in the bootstrap script, or download the binary directly.

2. **Graphite (`gt create`) is not installed in the codebase**
   - Observation: `swarm-code-pr` references `gt create --title "..." --body "..."` for PR creation via Graphite. But `gt` (Graphite CLI) is not in `flake.nix`, not in any `go.mod`, and not referenced anywhere in the codebase outside of plan documents. It's an unverified dependency.
   - Suggestion: Either (a) add Graphite installation to the bootstrap/Nix setup, or (b) use `gh pr create` instead (gh CLI is a more standard choice and may already be available), or (c) verify `gt` is already installed on the VPS.

3. **Migration file must be added to hardcoded list in `db.go`**
   - Observation: The plan says to create `harness/internal/db/migrations/XXX_swarm_tables.sql` but doesn't mention that migration files must be manually added to the `migrationFiles` slice at `harness/internal/db/db.go:93-99`. Migrations are NOT auto-discovered — they must be explicitly listed.
   - Suggestion: Add a step to Phase 1: "Add `migrations/006_swarm_tables.sql` to the `migrationFiles` slice in `harness/internal/db/db.go`."

4. **HeartbeatWorkflow swallows activity errors**
   - Observation: The `HeartbeatWorkflow` code runs all four activities sequentially and ignores errors (`_ = workflow.ExecuteActivity(...).Get(ctx, nil)`). If `SyncLinearState` fails, `ProcessTicketQueue` runs with stale data. If `ProcessTicketQueue` panics, `ReapSessions` never runs.
   - Suggestion: At minimum, log errors. Consider making `SyncLinearState` a prerequisite for `ProcessTicketQueue` (sequential with error propagation). `ReapSessions` and `DetectStalls` are independent and can swallow errors more safely.

5. **Significant divergence from Chestnut flowchart architecture**
   - Observation: The Chestnut flowchart (the HTML file you shared) shows a **two-level orchestration model**: OpenClaw as human interface → Lead FDE heartbeat (7️⃣) checking on Project Orchestrators (6️⃣), with each level having distinct responsibilities. The v3 plan replaces this entirely with a single `HeartbeatWorkflow` + SQLite state machine. The flowchart's "OpenClaw" box, "Lead FDE" role, "Project Orchestrator" role, and "Reprompt stalled leads" capability are all absent.
   - Risk: The deterministic Go state machine is simpler and cheaper (zero LLM cost), which is a strong advantage. But it loses the flowchart's ability to handle ambiguous situations (e.g., "this ticket is stuck because the API spec changed — should we re-research?"). The state machine can only do `if phase == X && result == Y → phase Z`.
   - Suggestion: Acknowledge this intentional simplification in the plan's Decision History. The current architecture covers 90% of cases with the state machine; add a note about future evolution toward an LLM orchestrator layer for ambiguous situations (e.g., a "smart escalation" skill that gets invoked when the state machine detects a pattern it can't handle).

6. **No "Full Restart" path in state machine**
   - Observation: The Chestnut flowchart shows three outcomes after human PR review: Merge, Revision, or Full Restart (new ticket referencing old). The v3 state machine has `done` and `failed` as terminal states, plus `plan_review → code_plan` for revision. But there's no mechanism for a "Full Restart" — creating a new workflow that references a prior failed/rejected attempt as context.
   - Suggestion: Add a `previous_attempt` field to `swarm_workflows` (nullable, references another workflow ID). The `/swarm-resume` or `/swarm-code` skills could read the previous attempt's artifacts for context. This enables the flowchart's restart path.

7. **No unit tests for the state machine**
   - Observation: The entire harness has zero `*_test.go` files. The state machine (`DetermineNextPhase`) is the most critical piece of logic — it has 15+ branches with attempt counters, config limits, and failure modes. All testing is manual.
   - Suggestion: At minimum, add `statemachine_test.go` with table-driven tests covering every transition in the state machine table. This is ~100 lines of test code that prevents regressions in the most important logic. Consider also testing `SkillForPhase` mapping.

8. **Max 4 sessions vs 3+1 queue split may be confusing**
   - Observation: `MaxSessions` defaults to 4. `swarm-general` has concurrency 3, `swarm-verify` has concurrency 1. But these are activity-level limits, not global limits. The `ProcessTicketQueue` activity checks `CountActiveSessions` against `MaxSessions` as a global cap. However, the Temporal workers would independently accept work up to their concurrency limits regardless of what's in SQLite.
   - Suggestion: Clarify that `MaxSessions` is the *total* global cap enforced in `ProcessTicketQueue`, while the worker concurrency limits are a safety net. If `MaxSessions=4`, the state machine should never spawn more than 4, and the 3+1 split ensures at most 3 general + 1 verify.

### Questions (Need Clarification)

1. How does the `SessionWorkflow` know when a skill session has finished if the Claude Code process exits but the hook hasn't fired yet? The plan's polling loop detects tmux death, but the RESULT comment may not be written yet. What's the contract?

2. The plan says "Heartbeat scheduled every 2 min" via `client.ScheduleClient().Create()`. What happens if the heartbeat schedule already exists on harness restart? Does `Create` return an error? Should it use `CreateOrUpdate` or check-then-create?

3. What happens when a Claude Code session crashes without writing a RESULT comment? The `parseResultComment` in `RunClaudeSession` would fail. Is this an `infra_failure` that triggers retry?

4. The plan says Linear sync uses `linear-cli i list -t CM -l "swarm:*"`. Does `linear-cli` support wildcard label matching? If not, you'd need to list with each specific swarm label.

### Suggestions (Nice to Have)

1. **Add a `swarm_workflows.previous_workflow_id` column** — Enables the "Full Restart" path from the Chestnut flowchart, where a new attempt references a previous one for context.

2. **Consider hook-based completion instead of polling** — Create a `POST /api/swarm/session-complete` endpoint that skills call via their hooks. This is more consistent with the existing codebase pattern and avoids the polling/race condition issue.

3. **Add `statemachine_test.go` in Phase 1** — Table-driven tests for every transition. This is the single highest-value test you can write since the state machine drives all orchestration.

4. **Add a `swarm_workflows.branch_name` column** — Track the git branch associated with each workflow. Useful for cleanup, PR linking, and the dashboard.

### What's Good

- **Decisive architectural evolution**: v1 (OpenClaw heartbeat) → v2 (Temporal long-running) → v3 (short-lived + SQLite state machine) shows clear learning. The final architecture is simpler, cheaper, and more debuggable than either predecessor.
- **Zero LLM orchestration cost**: All scheduling/routing is deterministic Go code. LLM spending is concentrated in the Claude Code worker sessions where it provides maximum value.
- **SQLite as queryable state**: The dashboard, API, and state machine all read from the same source of truth. No need to reconstruct state from Temporal history.
- **Flat, composable skills**: Each skill does one thing, writes a structured RESULT comment, and exits. No nested loading, no deep call stacks. Context pressure is minimized.
- **Comprehensive state machine table**: The phase transition table is explicit, covering happy path, revisions, retries, and terminal failures. The `infra_failure` vs `logic_failure` distinction is thoughtful.
- **Well-defined scope boundaries**: "What We're NOT Doing" section prevents scope creep. No auto-merge, no Slack, no webhook sync v1.
- **Phased delivery**: Each phase is independently testable. Phases 1-3 (skills only) deliver immediate value without Temporal.
- **Retry backoff**: The `retry_backoff_secs` configuration prevents thrashing on repeated failures.
- **Addresses all v1 review concerns**: State persistence, resume capability, dry-run support, rate limits, flat skills, plan doc template — all present.
- **Strong integration with existing patterns**: EventBus subscription, secret-based API auth, templ-based dashboard, sqlc query generation — all follow established harness patterns.

### Recommended Next Steps

1. **Resolve the session completion detection model** — Decide between polling and hooks before implementation. This affects the core `RunClaudeSession` activity and the contract between skills and the orchestrator. Hook-based is recommended for consistency with the existing codebase.

2. **Refactor `ProcessTicketQueue` to return spawn decisions, not execute them** — Move workflow spawning into `HeartbeatWorkflow` using child workflows. This is a small change that prevents duplicate workflow issues.

3. **Verify infrastructure dependencies** — Check `temporal-cli` in nixpkgs, Graphite CLI availability, and `linear-cli` wildcard label matching before committing to Phase 4 implementation.

4. **Add migration registration step** — Explicitly document adding the new migration file to the hardcoded list in `db.go:93-99`.

5. **Write `statemachine_test.go` as part of Phase 1** — Test every row in the state machine transition table. This is ~100 lines of high-value test code.

6. **Implement Phases 1-3 first** — Validate the skill design with human-driven usage before building Temporal automation. The skills work standalone; the state machine and Temporal just orchestrate them. This was recommended in the v1 review and remains good advice.

7. **Document the Chestnut flowchart divergence** — The plan intentionally simplifies the two-level orchestration (Lead FDE + Project Orchestators) into a single heartbeat + state machine. This is a reasonable choice but should be explicitly documented as a decision, not an oversight.
