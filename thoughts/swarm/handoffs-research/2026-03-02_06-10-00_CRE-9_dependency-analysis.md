---
ticket: CRE-9
phase: research
result: success
session: d27d0b79
workflow: 12ae2656
timestamp: 2026-03-02T06:10:00Z
---

## BLUF
Completed dependency analysis of all 4 largest Go files; identified safe split boundaries for each with no interface extraction needed since all splits stay within their respective packages.

## What Was Done
- Analyzed `swarmorch/manager.go` (1981 lines): identified 10 natural groupings, mapped all 51 functions, documented `m.mu` lock discipline across 3 methods
- Analyzed `server/create.go` (1046 lines): identified 4 natural groupings with clear funnel call chain
- Analyzed `server/server.go` (914 lines): identified 6 groupings in server.go plus 14 handler groups across the package
- Analyzed `swarmorch/project.go` (851 lines): identified 6 natural groupings, mapped all cross-file calls to manager.go
- Documented shared utility functions that need consolidation (`toNullString`, `nowUTC`, `slugify`)
- Wrote comprehensive research document with recommended split targets, line estimates, and risk levels

## What Was NOT Done
- No code changes — research only
- Did not analyze test files for split impact (noted as risk)
- Did not analyze the 5th-10th largest files (game_server.go, world/manager.go, linear/client.go, etc.)

## Key Files
- `thoughts/swarm/research/2026-03-02_06-10-00_CRE-9_go-file-splitting-dependency-analysis.md` — full research document
- `harness/internal/swarmorch/manager.go` — 1981 lines, highest complexity, 10 natural groupings
- `harness/internal/server/create.go` — 1046 lines, cleanest split boundaries
- `harness/internal/server/server.go` — 914 lines, lowest priority split
- `harness/internal/swarmorch/project.go` — 851 lines, moderate coupling with manager.go

## Gotchas
- `advanceWorkflow` (278 lines) in manager.go is the most entangled method — touches 7 of 10 groupings
- `m.mu` mutex is shared across `advanceWorkflow`, `ApproveGate`, `RejectGate` — must document lock discipline
- `toNullString` is defined in learnings.go but used by 5+ methods in manager.go — needs consolidation
- swarmorch package already has 25 files — more splitting needs clear naming conventions
- `RegisterRoutes` in server.go references ~70 handlers across 15 files — this is the package's central wiring

## Next Steps
- Create code tickets for each file split (4 tickets, ordered by risk)
- Start with server/create.go (lowest risk, clearest boundaries)
- Each split should be a pure move — no behavioral changes
