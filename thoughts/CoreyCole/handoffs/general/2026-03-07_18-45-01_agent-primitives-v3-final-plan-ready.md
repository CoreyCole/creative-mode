---
date: 2026-03-08T02:45:01-08:00
researcher: CoreyCole
git_commit: b06cdd7658f62e68307e979258be6a2b5066784c
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives v3 Final — Plan Review Complete, Ready for Implementation"
tags: [implementation, strategy, swarm, agent-primitives, temporal, pi-mono, observability]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives v3 — Final Plan Ready for Implementation

## Task(s)

1. **Staff engineering review of v3 plan** — Status: **complete**. Conducted independent verification of all factual claims in the plan against actual codebase. Found 3 critical issues, 6 concerns, 4 questions. All resolved and incorporated into the final plan.

2. **Write final v3 plan incorporating review fixes** — Status: **complete**. Applied 32 targeted fixes to the v3 plan, producing the authoritative implementation document at `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` (1695 lines).

3. **Phase 1 implementation** — Status: **next step**. The final plan is ready. No code has been written yet. Begin with Phase 1 (Foundation).

## Critical References

1. **Authoritative implementation plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — the ONLY plan to follow. All prior versions (v1-v5, v3 pre-review) are superseded.
2. **Staff review (findings that shaped the final plan)**: `thoughts/CoreyCole/reviews/2026-03-07_18-16-52_agent-primitives-v3-conversational-agents_review.md`
3. **System prompts & skills plan (for Phase 2)**: `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md`

## Recent changes

No code changes. Three document operations:
- Created `thoughts/CoreyCole/reviews/2026-03-07_18-16-52_agent-primitives-v3-conversational-agents_review.md` — staff review
- Created `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — final plan (copy of v3 + 32 review fixes)

## Learnings

### EventBus API (verified against `harness/internal/events/bus.go`)
- `Subscribe(worldID string)` / `Publish(worldID string, event any)` — world-scoped pub/sub
- `SubscribeGlobal()` / `PublishGlobal(event any)` — all-player events
- Channel buffer is 100 events, **drops on slow subscribers** (non-blocking send with `select { default: }` at line 101)
- The plan uses `Subscribe("swarm")` with a synthetic worldID — this works because the key is just a string, but beware of buffer overflow under parallel agent load (200+ events from 5 agents). The fat-morph SSE pattern handles this.

### EventBus Patterns for SSE
- **Mayor dashboard** (`harness/internal/server/mayor_dashboard.go:74-117`): Fat-morph pattern — on ANY event, re-query entire DB and replace full tab contents. Simple, self-healing, proven.
- **World SSE** (`harness/internal/server/events.go:56-114`): Event-specific dispatch with switch/case per event type. More granular but each event type needs handler code.
- **Final plan uses fat-morph** (like mayor dashboard) — on any swarm event, re-query spans from DB and replace entire `#span-tree` container.

### Database Migration Pattern (verified at `harness/internal/db/db.go:93-99`)
- Migrations must be **manually added** to `migrationFiles` slice — NOT auto-discovered from the `migrations/` directory
- Current migrations: 001 through 005. Next is `006_swarm_tables.sql`
- `bootstrapExistingMigrations()` (line 135) handles the case where schema exists but tracking doesn't

### SQLC Configuration (verified at `harness/sqlc.yaml`)
- Engine: SQLite. Queries at `internal/db/queries/`. Schema from `internal/db/migrations/`. Output to `internal/db/sqlc/`
- Has `rename` map for Go field naming and `overrides` for type mapping
- New swarm queries will need rename entries for fields like `task_id`, `parent_span_id`, `span_type`, etc.

### Pi-Mono Agent Core Types (verified at `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/types.d.ts:161-177`)
- `tool_execution_start` event includes `toolCallId: string`, `toolName: string`, `args: any`
- `tool_execution_end` event includes `toolCallId: string`, `toolName: string`, `result: any`, `isError: boolean`
- `toolCallId` is essential for correlating start/end pairs into spans with accurate duration

### Pi-Mono Tools (verified at `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/tools/index.d.ts`)
- `createReadOnlyTools(cwd, options?)` returns `[read, grep, find, ls]` tools — confirmed, no bash/edit/write
- `createCodingTools(cwd, options?)` adds bash/edit/write
- Tools have NO path traversal protection — absolute paths and `../../` work

### Server Struct (`harness/internal/server/server.go:49-66`)
- Implementation will add a `TemporalClient` field to the Server struct
- Route registration is in `RegisterRoutes()` at line 107
- Admin routes are at line 226: `adminGroup := authed.Group("/admin", auth.AdminMiddleware())`

### Go Module (`harness/go.mod`)
- Temporal SDK is NOT in go.mod — needs `go get go.temporal.io/sdk`
- Current Go version: 1.24.3

### cmd.Env Gotcha
- Setting `cmd.Env` in Go **replaces** the inherited environment entirely. Must pass `os.Environ()` or the subprocess loses `OPENAI_API_KEY`, `PATH`, `HOME`, etc.

### SQLite Timestamp Precision
- `datetime('now')` gives second-precision only (e.g., `2026-03-08 02:14:32`). For millisecond `duration_ms`, must set timestamps in Go code via `time.Now().UTC().Format(time.RFC3339Nano)`.

## Artifacts

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — **THE implementation plan** (1695 lines, 32 review fixes applied)
- `thoughts/CoreyCole/reviews/2026-03-07_18-16-52_agent-primitives-v3-conversational-agents_review.md` — staff review findings
- `thoughts/CoreyCole/reviews/2026-03-08_agent-primitives-v3-staff-summary.md` — earlier staff summary (from prior session)
- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — pre-review v3 plan (superseded by final)
- `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md` — Langfuse research
- `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md` — system prompts & skills plan

## Action Items & Next Steps

### Phase 1: Foundation (start here)
**Plan reference**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md:1557-1572`

1. Create `harness/internal/db/migrations/006_swarm_tables.sql` — schema at plan lines 718-784. Note: `started_at` has NO SQLite default (set in Go code for ms precision)
2. Add `"migrations/006_swarm_tables.sql"` to `migrationFiles` slice in `harness/internal/db/db.go:93-99`
3. Add bootstrap check to `bootstrapExistingMigrations()` in `db.go:135` (check for `swarm_tasks` table existence)
4. Create `harness/internal/db/queries/swarm.sql` — SQLC queries for span CRUD: `CreateSwarmSpan`, `CompleteSwarmSpan`, `FailSwarmSpan`, `GetSwarmSpansByTask`, `GetSwarmSpanTree` (recursive CTE), `ListSwarmTasks`, `CleanupOrphanedSpans`
5. Update `harness/sqlc.yaml` with rename entries for new fields
6. Run `cd harness && sqlc generate`
7. `cd harness && go get go.temporal.io/sdk && go mod tidy`
8. Add span event constants to `harness/internal/events/types.go` (plan line 991-996)
9. Create `harness/agents/package.json` (plan lines 815-826) + `ln -s /opt/openclaw/node_modules harness/agents/node_modules`
10. Create shared agent libraries: `harness/agents/lib/protocol.js` (plan lines 833-857), `lib/orchestrator-tools.js` (plan lines 860-903), `lib/agent-factory.js` (plan lines 906-951)
11. Create skill files at `harness/agents/skills/*.md` (plan lines 316-329)
12. Add startup orphan span cleanup + JSONL log cleanup goroutine in `main.go`

### Phase 2: Agent Scripts
**Plan reference**: lines 1573-1580

Write 6 JS agent scripts, test each standalone with manual JSON input via stdin.

### Phase 3: Temporal Workflows + Activities (with Span Instrumentation)
**Plan reference**: lines 1581-1594

Create `harness/internal/temporal/{client,activities,workflows,worker}.go`. Key code: `runAgent()` at plan lines 1054-1152, span helpers at lines 1154-1207, workflow spans at lines 1209-1251.

### Phase 4: HTTP API + SSE
**Plan reference**: lines 1595-1601

Swarm API routes (plan lines 960-974), SSE handler using fat-morph pattern (plan lines 1278-1320).

### Phase 5: Dashboard (Langfuse-Inspired Views)
**Plan reference**: lines 1602-1613

Templ files at `harness/views/swarm/` (plan lines 1384-1392). Three views: tree (default), timeline, event log.

## Other Notes

### VPS State (Unchanged)
- Temporal dev server running: `temporal-dev.service` (systemd), ports 7233 (gRPC) / 8233 (UI), namespace `swarm`
- bubblewrap + temporal-cli already in `flake.nix`
- No `OPENAI_API_KEY` in `.env` yet — must add before Phase 2 testing
- No swarm Go code exists on this branch — `internal/swarm/` and `internal/temporal/` don't exist yet
- No `harness/agents/` directory exists yet

### Key Review Fixes in Final Plan (search for `[REVIEW FIX]`)
| Fix | Location in Final Plan | What Changed |
|-----|----------------------|--------------|
| Tool call caps (50 warn, 100 kill) | lines 87, 373, runAgent() | Added hard limit on runaway agents |
| `answerQuestion` concrete algorithm | lines 210-305 | Replaced pseudocode with regex+grep impl |
| Fat-morph SSE pattern | lines 1278-1320 | Replaced granular per-span patching |
| Orphaned child span cleanup | runAgent() error path, resolved Q18 | `failOrphanedChildSpans()` + startup query |
| `started_at` ms precision | schema line 772, span helpers | Go timestamps, not SQLite default |
| `computeDuration` via spanEntry | runAgent() lines 1071-1072 | Stores startedAt with spanID |
| `cmd.Env` inheritance | DirectRunner, lines 1498-1505 | Pass `os.Environ()`, not custom env |
| `readLine` stdin closure | protocol.js lines 841-845 | Reject promise on stdin close |
| Dashboard admin-only | routes line 970-973, resolved Q14 | `adminGroup` not `approved` |
| Node_modules symlink | Phase 1, resolved Q15 | Symlink from OpenClaw, not npm install |
| Child workflow span type | resolved Q16 | Use `stage` type, not `workflow` |
| JSONL log cleanup | Phase 1, resolved Q21 | 7-day retention goroutine |

### Document Evolution Chain
v1 → v1 review → v2 → v2 handoff → v3 → v3 handoff → v3 refinement → v3 refinement handoff → draft prompts plan → staff review → final prompts plan → observability design → staff summary → **staff review → final plan (this session)**

### Directory Casing Note
Documents exist under both `thoughts/CoreyCole/` (reviews, handoffs, research) and `thoughts/coreycole/` (plans). These are separate directories on the filesystem. The final plan is at `thoughts/coreycole/plans/` (lowercase).
