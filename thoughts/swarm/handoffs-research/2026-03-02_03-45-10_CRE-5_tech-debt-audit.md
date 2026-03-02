---
ticket: CRE-5
phase: research
result: success
session: 28670d05
workflow: 83f30594
timestamp: 2026-03-02T03:45:10Z
---

## BLUF
Research complete — comprehensive tech debt audit identified 16 duplication categories, 10 convention inconsistencies, 18 observability gaps, and 8 maintainability issues with 18 prioritized recommendations. Prior research (workflow 124c06f6) was validated and is current.

## What Was Done
- Read and validated existing research from workflow 124c06f6 (thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md)
- Confirmed no codebase changes invalidate the findings (only one infra-fix commit since last research)
- Verified all key files referenced in the research still exist

## What Was NOT Done
- No new research needed — prior research is comprehensive and current
- Linear comment not posted (API auth not available in session; prior session already posted RESEARCH comment)

## Key Files
- `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md` — full research document with findings and recommendations
- `harness/internal/swarmorch/manager.go` — 1822-line monolith (top maintainability concern)
- `harness/internal/server/swarm_api.go` — swarm nil guard duplication (18x)
- `templates/2d/` and `templates/boardgame/` — near-identical Rust files (bridge, debug, camera)

## Gotchas
- LINEAR_API_KEY auth requires raw key in Authorization header (not Bearer), but kept failing — may need to verify key is still valid
- Previous workflow attempts all failed due to missing RESULT file writes — this was the critical fix needed

## Next Steps
- This is a `project` type ticket — next phase should decompose into child tickets
- Recommended groupings: quick wins (single PR), observability (single PR), conventions (single PR), template consolidation (separate PRs), structural improvements (multiple PRs), test coverage (separate effort)
