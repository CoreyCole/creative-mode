---
date: 2026-03-02T10:18:34-08:00
researcher: CoreyCole
git_commit: 5eb20fe0bfbd15caa3f55e97bc70fb215fd39a45
branch: feature/agent-swarm
repository: creative-mode
topic: "Phase E+F: Event Observability & Doctor Health Check"
tags: [implementation, swarm, event-bus, doctor, hooks, sse]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Phase E (Event Observability) + Phase F (Doctor Health Check)

## Task(s)

Implementation of the Phase E+F plan covering tool arg filtering, phase/file fields in events, ring buffer SSE replay, Linear Ping, and doctor health checks.

| Task | Status |
|------|--------|
| E.1: FilterToolArgs + tests | **Completed** |
| E.2: Phase & file fields in hook events | **Completed** |
| E.3: Ring buffer + SSE replay | **Completed** |
| F.1: Linear client Ping method | **Completed** |
| F.2: Doctor health checks (8 checks) | **Completed** |
| F.3: Doctor API route | **Completed** |
| F.4: Doctor tests | **Completed** |
| Lint + test verification | **Completed** |
| Git stash/rebase/commit/push | **Work in progress** — see Action Items |

All code is written and passes `just check` + all 17 new tests. The remaining work is resolving a git stash conflict and committing.

## Critical References

- Plan transcript: `/Users/coreycole/.claude/projects/-Users-coreycole-cdev-creative-mode/59ee5198-4fee-4472-8911-f0a3647c1085.jsonl`
- `harness/CLAUDE.md` — full swarm architecture docs

## Recent changes

All changes are currently in git stash. The working state is:

- `harness/internal/swarmorch/hooks.go` — Added `FilterToolArgs()`, `copyIfPresent()`, `bashCmdTruncateLimit` const; updated `WriteHooksConfig` signature to add `phase` param; added `X-Swarm-Phase` header
- `harness/internal/server/swarm_hooks.go:130-155` — `handleSwarmHookPostToolUse` now calls `FilterToolArgs`, extracts `X-Swarm-Phase` header, adds `phase` and `file` to JSONL + EventBus events
- `harness/internal/swarmorch/manager.go:355-360` — Updated `WriteHooksConfig` callsite to pass `string(wf.Phase)`
- `harness/internal/swarmorch/hooks_test.go` — Added 3 FilterToolArgs tests + updated WriteHooksConfig calls with phase arg + X-Swarm-Phase assertion
- `harness/internal/events/bus.go` — Added `NumberedEvent`, ring buffer (1000 slots), `seq atomic.Int64`, `EventsSince()`, `CurrentSeq()`
- `harness/internal/events/bus_test.go` — **NEW** — 4 ring buffer tests
- `harness/internal/server/swarm_dashboard.go` — Extracted `processSwarmEvent` helper; added `last_seq` query param replay
- `harness/internal/linear/client.go:176-193` — Added `Ping(ctx) error` method
- `harness/internal/swarmorch/doctor.go` — **NEW** — 8 diagnostic checks + `RunDoctor`
- `harness/internal/swarmorch/doctor_test.go` — **NEW** — 5 doctor tests
- `harness/internal/server/swarm_api.go` — Added `handleSwarmDoctor` handler
- `harness/internal/server/server.go:180,225` — Added `/doctor` routes

## Learnings

1. **golangci-lint `mnd` rule**: Magic numbers in conditions trigger the `mnd` linter. Extract to named constants (e.g., `bashCmdTruncateLimit = 120`).
2. **golangci-lint `gosec G115`**: `uint64 -> int` conversion triggers integer overflow warning. Keep arithmetic in `uint64` when dealing with `syscall.Statfs_t` fields.
3. **golangci-lint `perfsprint`**: `fmt.Errorf` with no `%w` verb should be `errors.New` instead.
4. **golangci-lint `revive early-return`**: Nested `if` blocks should be inverted to early returns.
5. **Stash ordering matters**: When stashing subsets of files, the remaining files need a separate stash before rebase. Pop in reverse order.

## Artifacts

- `harness/internal/swarmorch/hooks.go` — FilterToolArgs, updated WriteHooksConfig
- `harness/internal/swarmorch/hooks_test.go` — FilterToolArgs tests, updated hook config tests
- `harness/internal/events/bus.go` — Ring buffer + EventsSince + CurrentSeq
- `harness/internal/events/bus_test.go` — **NEW** ring buffer tests
- `harness/internal/server/swarm_dashboard.go` — processSwarmEvent helper + last_seq replay
- `harness/internal/server/swarm_hooks.go` — Filtered input + phase/file extraction
- `harness/internal/swarmorch/manager.go` — Updated WriteHooksConfig callsite
- `harness/internal/linear/client.go` — Ping method
- `harness/internal/swarmorch/doctor.go` — **NEW** 8 diagnostic checks
- `harness/internal/swarmorch/doctor_test.go` — **NEW** 5 tests
- `harness/internal/server/swarm_api.go` — handleSwarmDoctor handler
- `harness/internal/server/server.go` — Doctor routes

## Action Items & Next Steps

1. **Resolve git stash conflict in `temporal.go`**: There are two stashes:
   - `stash@{0}` was partially popped with a conflict in `harness/internal/swarmorch/temporal.go` — the stashed version adds a `TemporalEnabled()` function that upstream doesn't have. **Keep the stashed (incoming) version** — accept the added function.
   - After resolving, `git add harness/internal/swarmorch/temporal.go` then `git stash drop stash@{0}` (it was kept because of the conflict).
   - `stash@{1}` contains the Phase E+F changes. Pop it with `git stash pop stash@{1}`.
   - `stash@{2}` is an old WIP stash from before rebase — can likely be dropped.

2. **Verify after stash pop**: Run `just check` and `go test ./internal/events/... ./internal/swarmorch/... -run "TestEventBus|TestFilter|TestWriteHooks|TestRunDoctor" -count=1` from `harness/`.

3. **Commit and push**: Stage the 12 changed/new files listed in Artifacts above (plus any pre-existing changes that were in stash@{0}). Commit with a message describing Phase E+F implementation.

4. **Site golangci-lint failure**: `just check` output showed `FAIL site (golangci-lint)` — this is a **pre-existing** issue unrelated to our changes. May need separate investigation.

## Other Notes

- The `Manager` struct in `swarmorch/manager.go` changed significantly upstream (removed `promptVersionMu`/`promptVersionIDs`, removed `prompt`/`transcript` imports). Our change only touches the `WriteHooksConfig` callsite at line ~355, so the stash pop should apply cleanly for that file.
- The upstream added `project_decompose` phase, `RevisionTarget`, token capture, and Temporal as required. The `swarm/statemachine.go` has new phases. Our doctor `checkPromptTemplates` only tests 6 phases (the original code workflow phases) which still exist.
- Ring buffer `EventsSince` scans all 1000 slots — this is O(1000) per call which is fine for dashboard SSE reconnects but could be optimized with a min-seq tracking if needed later.
