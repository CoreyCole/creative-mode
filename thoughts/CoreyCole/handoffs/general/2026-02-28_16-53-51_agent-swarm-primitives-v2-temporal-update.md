---
date: 2026-02-28T16:53:51-0800
researcher: CoreyCole
git_commit: 094892dcf0bd73f2bd4993d35dd327bbb5b477b6
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v2 — Temporal Orchestration Update & Plan Review"
tags: [implementation, strategy, agent-swarm, temporal, linear-cli, skills, orchestration]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v2 — Temporal Update & Continued Review

## Task(s)

1. **v2 Plan Review & Optimization** — IN PROGRESS. Resumed from previous handoff (`thoughts/CoreyCole/handoffs/general/2026-02-28_15-12-04_agent-swarm-primitives-v2-plan-review.md`). The plan has been significantly updated with two major architectural decisions, but still needs a final review pass before implementation begins.

2. **Orchestration Architecture Evaluation** — COMPLETED. Evaluated three options for Lead FDE orchestration:
   - **OpenClaw heartbeat** (v1 plan) — Rejected. Burns LLM tokens per tick, 30-min resolution, risk of hallucinating CLI flags.
   - **Go `time.Ticker`** (v2 initial) — Evaluated, then superseded. Simple and zero-dependency, but lacks queue management for verification serialization, durable state across restarts, and observability.
   - **Temporal workflows** (v2 final) — CHOSEN. Durable workflows, task queue concurrency control (`swarm-verify` at concurrency 1 prevents OOM), retry policies, web UI observability.

3. **Plan Updates Applied** — COMPLETED. The v2 plan at `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` has been updated with:
   - Phase 4 rewritten: "Go Ticker + Harness API" → "Temporal Workflows + Harness API"
   - Two task queues: `swarm-general` (concurrency 3), `swarm-verify` (concurrency 1)
   - Workflow IDs contain agent index: `swarm-{agentIdx}-{ticketID}` (user-requested)
   - Infrastructure: Temporal server via Nix (`temporal-cli` in `flake.nix`), systemd service with SQLite persistence
   - File inventory updated: 17 new files, 6 modified files
   - All references to Go ticker and OpenClaw heartbeat updated throughout

4. **Final Plan Review** — PLANNED. Next session should do a full review of the updated plan for:
   - Consistency check (all sections reflect Temporal, not ticker)
   - Whether the `swarm-code-change` skill needs `--phase` subcommand support (plan assumes it)
   - Edge cases in the Temporal workflow (what happens when a Claude Code session writes malformed comments)
   - Whether 13 skill files is still the right count after all changes
   - Anything else that should be tightened before implementation

## Critical References

- **v2 Plan (authoritative, updated this session)**: `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md`
- **Previous handoff (for full decision history)**: `thoughts/CoreyCole/handoffs/general/2026-02-28_15-12-04_agent-swarm-primitives-v2-plan-review.md`
- **Nix flake (where Temporal CLI will be added)**: `flake.nix`

## Recent Changes

No code changes — this session was planning/architecture evaluation only. The v2 plan document was updated extensively:
- `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` — Phase 4 fully rewritten, overview updated, file inventory updated, references updated, performance considerations updated

## Learnings

### Architecture Decisions (user-approved)

1. **OpenClaw heartbeat rejected**: Each tick is a full LLM API call (~$0.03-0.10+). At 30-min intervals with all tools available, the agent might hallucinate CLI flags or misparse JSON. No queue management. The orchestration is mechanical — poll Linear, compare timestamps, spawn/reap sessions — so LLM reasoning adds cost without value.

2. **Go ticker superseded by Temporal**: The ticker was simpler (~400 lines, zero deps) but couldn't solve the verification queue problem. With max 4 sessions and `just check` using ~5 GB RAM, two concurrent verifications = OOM. Building a custom semaphore/channel would reinvent Temporal's task queues. The ticker's in-memory state also wouldn't survive harness restarts.

3. **Temporal chosen for**: (a) `swarm-verify` queue with `MaxConcurrentActivityExecutionSize: 1` — only one verification at a time, (b) durable retry loops survive restarts, (c) activity heartbeat recovery — tmux session names stored in heartbeat details so worker restart resumes polling instead of spawning duplicates, (d) web UI for observability.

### Technical Discoveries

- **Temporal server lightweight mode**: `temporal server start-dev --db-filename /path/temporal.db` runs full server with SQLite. ~200 MB RAM at idle. SQLite not officially supported for production, but viable for <10 workflows on single server.
- **Temporal Go SDK worker is non-blocking**: `worker.Start()` returns immediately, embeddable in existing Go server. `worker.Stop()` for graceful shutdown.
- **Temporal task queue concurrency**: Set on the worker, not the queue: `worker.Options{MaxConcurrentActivityExecutionSize: 1}`.
- **Activity heartbeat pattern for long-running processes**: `activity.RecordHeartbeat(ctx, sessionName)` saves state. On retry, `activity.GetHeartbeatDetails(ctx, &sessionName)` recovers it. Perfect for polling tmux sessions.
- **Temporal schedules replace cron**: `client.ScheduleClient().Create()` with `Intervals: [{Every: 2 * time.Minute}]` replaces Go `time.Ticker`.
- **VPS Nix dependency chain**: `flake.nix` → `vps-bootstrap.sh` runs `nix profile install` → `harness-run.sh` sources `nix-daemon.sh` for PATH. Temporal CLI will follow this same chain.
- **No existing queue/retry infrastructure in harness**: All async work is fire-and-forget goroutines. No worker pools, no global build serialization. The builder has no mutex — concurrent WASM builds from different users could OOM. Temporal fixes this.
- **Workflow IDs should contain agent index** (user-requested): `swarm-{agentIdx}-{ticketID}` for observability in Temporal UI and tmux session naming consistency.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` — the authoritative v2 plan (updated this session with Temporal)
- `thoughts/CoreyCole/handoffs/general/2026-02-28_15-12-04_agent-swarm-primitives-v2-plan-review.md` — previous handoff (contains v1→v2 decision history)
- `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` — v1 plan (superseded)
- `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md` — v1 review
- `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md` — linear-cli research

## Action Items & Next Steps

1. **Final review of updated v2 plan** — Read the full plan and check:
   - All sections consistently reference Temporal (no stale Go ticker or OpenClaw references)
   - The `swarm-code-change` skill's `--phase` subcommand pattern (plan assumes `--phase plan`, `--phase revise`, `--phase implement`, `--phase pr`) — this needs to be designed into the SKILL.md
   - Edge cases: What happens when a Claude Code session crashes without writing a comment? What if comments are malformed? How does the HeartbeatWorkflow handle workflows that are stuck in Temporal (not just stalled in Linear)?
   - Skill count: Should be 9 primitives + 1 conventions = 10 SKILL.md files + 3 templates = 13 total skill files
   - Whether `swarm-status` needs updating to query Temporal visibility instead of / in addition to the health API

2. **Consider phased rollout** — Even with all 5 phases planned, implementing Phases 1-3 first (skills only, no Temporal) validates the primitive design with human-driven usage before investing in Phase 4 automation. The skills work standalone — Temporal just orchestrates them.

3. **After review is complete** — Begin implementation starting with Phase 1 (conventions, setup, templates).

## Other Notes

### Codebase Reference Points
- VPS dependency chain: `flake.nix` → `scripts/vps-bootstrap.sh` (Step 12) → `scripts/harness-run.sh`
- Systemd service (generated in vps-bootstrap.sh Step 9): `ExecStart=scripts/harness-run.sh`, `EnvironmentFile=harness/.env`
- Existing ticker patterns (for reference): `harness/main.go:223-234` (reaper), `harness/main.go:108-122` (session cleanup)
- President agent (reference, NOT used for Lead FDE): `harness/internal/president/`
- Tmux session management: `harness/internal/tmux/session.go`
- Claude orchestrator: `harness/internal/claude/claude.go`
- Builder (no global serialization!): `harness/internal/builder/builder.go`
- Rate limiter (per-user only): `harness/internal/world/rate_limit.go:33-74`

### Decision Evolution Summary
| Version | Orchestration | Why Changed |
|---------|--------------|-------------|
| v1 plan | OpenClaw heartbeat | Original design from Chestnut flowchart |
| v2 initial | Go `time.Ticker` | Mechanical logic doesn't need LLM reasoning |
| v2 final | Temporal workflows | Need verification queue (concurrency 1), durable state, retry loops |

### Temporal Resource Budget (10 GB VPS)
| Component | RAM | Notes |
|-----------|-----|-------|
| Temporal server (SQLite) | ~200 MB | `temporal server start-dev --db-filename` |
| Harness + embedded workers | ~250 MB | Existing + Temporal SDK |
| Up to 3 Claude sessions (general) | ~1.5 GB | On `swarm-general` queue |
| 1 verification session | ~1-5 GB | On `swarm-verify` queue (exclusive) |
| **Total peak** | **~3-7 GB** | **Fits in 10 GB with headroom** |
