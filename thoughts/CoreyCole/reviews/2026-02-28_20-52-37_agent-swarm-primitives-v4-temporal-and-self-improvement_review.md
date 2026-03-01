---
date: 2026-02-28T20:52:37-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 35a328e9ffabe7a52efa9eda64dc275702f14dcf
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md
status: complete
type: plan_review
focus: Temporal workflow setup and self-improvement design
---

# Plan Review: Agent Swarm Primitives v4 — Temporal & Self-Improvement Focus

### Summary

The Temporal workflow design is mechanically sound for v1 — short-lived workflows, SQLite state machine, hook-based completion, and fire-and-forget child workflows are all correct patterns. However, the plan has a fundamental gap: **it contains zero mechanisms for the system to learn from its own outputs**. The plan is an excellent orchestrator but has no feedback loops, no cross-workflow knowledge transfer, and no connection to the existing mayor/president learning infrastructure. The swarm can execute workflows but cannot improve at executing them.

### Critical Issues (Must Address Before Implementation)

1. **No self-improvement mechanism exists anywhere in the plan**

   - Problem: The plan describes a deterministic state machine that executes workflows through fixed phases using static skill definitions. When a workflow fails (terminal failure after max retries), it posts a `TERMINAL_FAILURE` comment and adds `needs-human`. When it succeeds, it creates a PR and moves to `done`. In neither case does the system learn anything that would improve future workflows. Specifically:
     - The 14 SKILL.md files are static text. If `swarm-code-plan` consistently produces plans that fail `swarm-plan-review`, the skill instructions never adapt.
     - Each workflow starts fresh with only its own research doc and plan doc. Agent 0's discoveries while implementing Feature A cannot benefit Agent 1 working on Feature B.
     - The heartbeat is purely mechanical — it reads SQLite state and makes deterministic decisions. It never analyzes patterns across workflows.
     - `SwarmConfig` parameters (`MaxPlanRevisions`, `MaxVerifyRetries`, `RetryBackoffSecs`) are static. No tuning based on observed success/failure rates.
     - No post-mortem or retrospective mechanism exists for failed workflows.
   - Risk: The system will make the same mistakes repeatedly. Common failure patterns (e.g., "always forgets to update go.mod when adding dependencies") will recur across every code workflow, consuming retry budgets and human review time. Without learning, the swarm is a fixed-capability system that scales execution but not intelligence.
   - Suggestion: Add a "swarm memory" mechanism. Options (in increasing sophistication):

     **Option A — Shared MEMORY.md (minimal)**:
     Create `harness/internal/swarm/MEMORY.md` (or `.claude/swarm-memory.md`) that every swarm skill reads at session start and optionally appends to at session end. Include sections like "Common Pitfalls", "Patterns That Work", "Codebase Conventions Discovered". This mirrors the existing checkpoint MEMORY.md pattern but for the swarm. The heartbeat could even auto-append terminal failure summaries.

     **Option B — Contribute-learning for swarm (moderate)**:
     Mirror the existing `ContributeLearning()` pattern from `harness/internal/mayor/learning.go:14`. When a swarm session discovers something useful (e.g., a verification failure reveals a new check that should be in the skill), it can POST to a new `/api/swarm/contribute-learning` endpoint that creates a PR modifying the relevant SKILL.md. Human reviews the change. Over time, skills evolve.

     **Option C — Automated failure analysis (ambitious)**:
     Add a `swarm-retrospective` phase that triggers on terminal failure. It reads the full workflow history (research doc, plan, review, implementation, verification failures) and writes a structured post-mortem. The heartbeat aggregates post-mortems and, when patterns emerge (>2 workflows fail for similar reasons), flags them for skill updates or config changes.

     At minimum, Option A should be in Phase 1. Option B should be in Phase 2/3. Option C can be future work.

2. **Swarm is completely disconnected from the existing mayor/president learning system**

   - Problem: The codebase already has three feedback mechanisms: (1) checkpoint MEMORY.md inheritance through fork chains (`harness/internal/claude/memory.go:13`), (2) mayor `ContributeLearning()` → GitHub PR flow (`harness/internal/mayor/learning.go:14`), (3) president heartbeat pattern detection across worlds (`harness/internal/president/heartbeat.go`). The swarm plan references none of these. The swarm is a parallel system with its own state machine, its own sessions, its own dashboard — but no integration with the existing agent hierarchy.
   - Risk: Two problems. First, duplicated infrastructure — the president's heartbeat pattern detection (detect >2 worlds with same failure, diagnose and fix) is exactly what the swarm needs for cross-workflow pattern detection, but it's being rebuilt from scratch in a different way. Second, the swarm operates on the same codebase (templates, harness) as the mayors, but changes made by the swarm (PRs) won't flow back to mayor MEMORY.md, and mayor discoveries won't inform swarm skills.
   - Suggestion: At minimum, document the intentional separation and the future integration path. Better: wire the swarm's EventBus events into the president's observability scope (the president already has a `mayor-status` skill; add a `swarm-status` skill). Even better: have the swarm's `contribute-learning` flow use the same `ContributeLearning()` Go function that mayors use, so template improvements from swarm workflows and mayor workflows go through the same PR pipeline.

3. **`ReadTicketQueue` has no transaction isolation — concurrent heartbeats can double-spawn**

   - Problem: `ReadTicketQueue` (plan lines 1396-1449) performs a read-modify-write cycle: `GetRunningWorkflows()` → `GetLatestSession()` → `DetermineNextPhase()` → `UpdateWorkflowPhase()` → append to `spawns`. All are separate queries with no transaction. If a heartbeat takes >2 minutes (slow Linear API in `SyncLinearState`), the next scheduled heartbeat fires, reads the same workflow state, and produces the same spawn decision. Both heartbeats return spawn decisions for the same workflow/phase, and `HeartbeatWorkflow` spawns duplicate children.
   - Risk: Duplicate child workflows. The Temporal workflow ID includes the attempt number (`swarm-{idx}-{ticket}-a{attempt}`), so the second spawn attempt will have the same ID and Temporal will reject it with a `WorkflowExecutionAlreadyStarted` error. This is caught (plan line 1334: `logger.Error`) but the workflow's phase was already updated by the first heartbeat, so the state machine is in an inconsistent state — the workflow is in the next phase but the spawn failed.
   - Suggestion: Wrap the entire `ReadTicketQueue` in a `BEGIN IMMEDIATE` transaction. SQLite's `IMMEDIATE` mode acquires a write lock at `BEGIN`, serializing concurrent writers. This is the pattern recommended by the v4 review (Concern #6) but was not addressed in v4.1. Implementation: the `store` interface should expose a `WithTx(func(tx) error) error` method, and `ReadTicketQueue` should use it.

### Concerns (Should Address)

1. **Temporal `start-dev` server in production**

   - Observation: The plan uses `temporal server start-dev --db-filename /home/deploy/creative-mode/data/temporal.db` as the production deployment. This is explicitly the development server — it's single-node, single-namespace, has no TLS, no auth, and uses a simplified persistence layer. The `--headless` flag only disables the web UI, not the dev-mode behavior.
   - Suggestion: This is probably fine for a single-VPS deployment with <10 concurrent workflows, but should be explicitly documented as a known limitation. The plan should note: (a) no TLS between harness and Temporal — acceptable since both run on the same host, (b) no auth — anyone with network access to port 7233 can interact with workflows, (c) single-node — Temporal server crash means no heartbeats until restart. Add `BindOnIP: localhost` (or `--ip 127.0.0.1`) to the systemd service to prevent external access.

2. **No graceful degradation when Temporal is unavailable**

   - Observation: The plan shows `initSwarm()` in `main.go` calling `client.Dial()` at startup. If Temporal is unreachable, `Dial()` may block or fail. The error handling shows logging + return, which is fine — the harness continues without swarm. But what about Temporal going down *after* startup? Workers will disconnect and attempt reconnection. During this window, scheduled heartbeats don't fire, in-flight `RunClaudeSession` activities lose their heartbeat timeout, and Temporal will eventually time out the activities. The tmux sessions keep running, but no completion signals are processed (the `CompletionRegistry` is in the harness, not Temporal — so the hook still signals, but the activity isn't waiting anymore).
   - Suggestion: Document the recovery path explicitly. When Temporal recovers: (1) workers reconnect, (2) timed-out activities are re-dispatched (this is Temporal's standard behavior, not a "retry" — `MaximumAttempts: 1` doesn't prevent re-dispatch on worker reconnection), (3) the re-dispatched `RunClaudeSession` creates a new completion channel, finds the tmux session is dead (Claude already finished), reads the RESULT comment, and returns. This works but should be documented as a tested recovery path, not assumed.

3. **`EventBus.Subscribe("swarm")` uses a synthetic key**

   - Observation: The EventBus's `Subscribe(worldID string)` method is semantically for world IDs. The swarm dashboard uses `Subscribe("swarm")` — a synthetic key that isn't a world ID. This works mechanically (it's just a map key) but conflates the abstraction. If someone adds world ID validation, or if a world is ever created with ID "swarm", this breaks.
   - Suggestion: Either add a `SubscribeGlobal`-style method for non-world topics (e.g., `SubscribeTopic(topic string)`) or document that `Subscribe` accepts arbitrary string keys, not just world IDs. The existing `SubscribeGlobal()` doesn't work here because the swarm dashboard shouldn't receive all global events (chat, player join/leave).

4. **linear-cli as a runtime dependency with no health check**

   - Observation: The entire Linear sync layer (`SyncLinearState` activity) shells out to `linear-cli`. If `linear-cli` is not installed, not authenticated, or its auth token expires, every heartbeat tick will fail the sync activity. The plan mentions `linear-cli config doctor` in `swarm-setup`, but this is a one-time skill invocation — there's no runtime health check.
   - Suggestion: Add a `linear-cli config doctor` check to `SyncLinearState` (or at least on first invocation / periodic re-check). If it fails, skip sync gracefully and log a warning rather than erroring every 2 minutes.

5. **No mechanism for the swarm to observe its own success rate**

   - Observation: The `swarm_events` table captures every state transition, and the dashboard shows live activity. But there's no aggregation — no success rate per workflow type, no average time per phase, no failure rate by skill. The `swarm-status` skill (Phase 3) queries `/api/swarm/health` for capacity and active workflows but shows no historical metrics.
   - Suggestion: Add a `swarm_metrics` view or periodic aggregation that computes: (a) workflow completion rate by type (research/code/project), (b) average phase duration, (c) top failure reasons, (d) retry rate per phase. This is the minimum observability needed to know if the swarm is actually effective. Without it, the operator has no way to know if the swarm is helping or just churning through retries.

6. **Harness restart during `ContributeLearning` git operations**

   - Observation: The existing `ContributeLearning()` at `harness/internal/mayor/learning.go:14-69` executes a sequence of `git checkout -b` → `git add` → `git commit` → `git push` → `gh pr create` → `git checkout main`. If the harness crashes mid-sequence, the repo is left on a stale branch. The cleanup (line 58) only attempts `git checkout main` if a command within the sequence fails, but a harness process kill (SIGKILL) would skip the cleanup entirely. The swarm's `swarm-code-pr` skill uses Graphite `gt create` instead, but if any swarm mechanism adopts the same `ContributeLearning()` pattern, it inherits this fragility.
   - Suggestion: Either (a) run git operations in a worktree so the main repo is never dirtied, or (b) add a startup check that verifies the repo is on `main` branch and has a clean working tree. Option (b) is simpler and already a good practice.

7. **`createSwarmTmuxSession` writes hooks to the session's working directory**

   - Observation: The plan says (line 644) the swarm on-stop hook is "written to a workspace-local `.claude/hooks/` directory." Swarm sessions run from the repo root. This means writing to `.claude/hooks/on-stop.sh` at the repo root — which is the project-level hooks directory. If the repo root already has `.claude/hooks/` (e.g., from a `.claude/settings.json` that references hooks), the swarm hook would overwrite or conflict with existing hooks.
   - Suggestion: Check if `.claude/hooks/` already exists at the repo root. If so, either (a) chain the existing hook with the swarm hook (run both), or (b) use a different directory for swarm hooks and configure Claude Code's hook path via an env var or config option. The safest approach is to write the hook to a temp directory and pass the hook path to Claude Code via `--hooks-dir` or equivalent CLI flag (verify this flag exists in Claude Code's CLI).

### Questions (Need Clarification)

1. **What is the intended self-improvement story?** The plan title includes "primitives" — is the idea that self-improvement is a future layer on top of these primitives? If so, should Phase 1 at least establish the data model for it (e.g., a `swarm_learnings` table)?

2. **How does the swarm interact with the president?** The president is "currently disabled in production" but the plan doesn't mention it at all. If the president is re-enabled, should it oversee the swarm? Should the president's heartbeat detect swarm failures and intervene? The agent hierarchy in CLAUDE.md shows President → Mayors → Claude Code, but the swarm sits outside this hierarchy.

3. **Should swarm sessions have MEMORY.md inheritance?** The existing build pipeline at `harness/internal/claude/memory.go:13` appends each prompt to MEMORY.md, and forks inherit the full chain. Swarm sessions run from the repo root with no checkpoint fork. Should there be an equivalent mechanism where each swarm session reads/appends to a persistent swarm memory file?

4. **What happens to the RESULT comment timing?** The v4 review asked this (Question 1) and the v4.1 handoff lists it as "Open Question #1." The on-stop hook fires when Claude Code exits. If the skill uses `linear-cli cm create` to write the RESULT comment as its last action, there's a race: does the RESULT comment exist in Linear when the hook fires? `linear-cli` is synchronous, so if the RESULT comment is the last thing before Claude exits, it should be written before `on-stop` fires. But if Claude Code's own cleanup runs between the skill's last command and the hook, the ordering could be different. This needs a definitive answer.

5. **How does `findAvailableSlot` work exactly?** The plan mentions it at line 1434 but doesn't define it. Does it query `swarm_sessions` for active sessions? Count by agent index? If two heartbeats overlap (before the transaction isolation fix), both could assign slot 0 to different workflows.

### Suggestions (Nice to Have)

1. **Add a `swarm-retrospective` skill** — When a workflow reaches terminal failure, automatically run a lightweight skill that reads the full comment history and writes a structured post-mortem to `thoughts/shared/retrospectives/`. Over time, these accumulate into a searchable knowledge base of "what went wrong and why."

2. **Add workflow-level MEMORY.md** — Each workflow could have a `thoughts/shared/swarm/{ticketID}/MEMORY.md` that persists across phases within the same workflow. The research skill writes findings, the plan skill reads them, the implementation skill reads them. Currently, inter-phase communication is only through Linear comments, which are linear/chronological and hard to structure.

3. **Add `swarm_learnings` table to Phase 1 schema** — Even if learning logic comes later, establishing the data model now (columns: `id`, `source_workflow_id`, `category` (pattern/pitfall/convention), `content`, `created_at`) means the schema migration doesn't need to change when self-improvement is added.

4. **Connect `swarm_events` to the existing EventBus event type system** — The plan defines 17 swarm `EventType` constants but the existing `events/types.go` only has 11 untyped string constants. Consider unifying these — either add the swarm events to `types.go`, or have the swarm use its own typed event system. Currently the plan publishes `map[string]any` to the EventBus (line 707-714) with an `"event"` key, which is ad-hoc.

5. **Temporal Workflow ID as semantic version** — Instead of `swarm-{idx}-{ticket}-a{attempt}`, consider `swarm-{ticket}-{phase}-{attempt}` (e.g., `swarm-CM-123-research-a1`). This makes Temporal UI browsing much more informative — you can see at a glance which phase each workflow represents. The agent index is less important in the ID (it's stored in SQLite anyway).

### What's Good

- **Hook-based completion model correctly mirrors the existing pattern**: The plan identifies that `templates/*/.claude/hooks/on-stop.sh` → `POST /api/claude-event` → `BuildCheckpoint` is the proven completion pattern and adapts it faithfully. The dual-path design (hook signal + 30s tmux health check fallback) is robust.

- **Short-lived workflows + SQLite state machine is the right architecture**: Avoiding long-running Temporal workflows eliminates the biggest class of Temporal bugs (non-deterministic replay, signal handling races, workflow versioning). The SQLite state machine is simple, queryable, and debuggable.

- **Zero LLM cost for orchestration**: All scheduling is deterministic Go code. This is a significant insight — most agent orchestration systems burn LLM tokens on routing/scheduling decisions. Here, intelligence lives only in Claude Code sessions where it provides value.

- **The phase transition table is explicit and testable**: Every state transition is enumerated. Table-driven tests in Phase 1 will catch regressions. This is the single highest-value test investment.

- **Fire-and-forget child workflows with `PARENT_CLOSE_POLICY_ABANDON`**: Correctly handles the case where the heartbeat (parent) completes before child sessions finish. The `GetChildWorkflowExecution().Get()` call ensures the child is started before the parent exits.

- **Phased delivery with standalone skills**: Phases 1-3 deliver value without Temporal. Skills work via direct CLI invocation. The Temporal layer (Phase 4) orchestrates them without changing their behavior.

- **The v4 evolution from v1-v3 shows genuine architectural learning**: Each iteration fixed real problems found in review. The decision history is well-documented.

### Recommended Next Steps

1. **Define the self-improvement story** — Even if implementation is deferred, the plan should articulate how the swarm will learn from its outputs. At minimum, add a shared `swarm-memory.md` read at session start (Phase 1). Add `contribute-learning` as a swarm capability (Phase 2). Add failure retrospectives (Phase 3 or future).

2. **Address transaction isolation in `ReadTicketQueue`** — Wrap in `BEGIN IMMEDIATE`. This is a low-effort, high-impact fix that prevents the most likely race condition.

3. **Add `swarm_learnings` to the Phase 1 schema** — Even if unused initially, it establishes the data model for future self-improvement without requiring a new migration.

4. **Document the Temporal recovery path** — Write explicit scenarios: (a) harness restart mid-session, (b) Temporal restart mid-workflow, (c) both restart simultaneously. For each, trace the exact sequence of events and verify the system converges to a correct state.

5. **Verify hook directory handling** — Confirm that writing `.claude/hooks/on-stop.sh` at the repo root doesn't conflict with any existing hooks. If it does, determine the Claude Code CLI flag for specifying a hooks directory.

6. **Add observability metrics** — At minimum, add a `GET /api/swarm/metrics` endpoint that returns workflow completion rate, average phase duration, and failure rate. This is necessary for operators to evaluate whether the swarm is effective.

7. **Connect swarm to the president** — Add a `swarm-status` skill to the president's skill set so the president can observe swarm health alongside mayor health. This doesn't require the president to be enabled — it's just wiring for when it is.
