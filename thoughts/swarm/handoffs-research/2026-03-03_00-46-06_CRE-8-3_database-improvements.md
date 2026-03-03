---
ticket: CRE-8-3
phase: research
result: success
session: 4b6e6a5c
workflow: 070b2a4a
timestamp: 2026-03-03T00:46:06Z
---

## BLUF
Research for CRE-8-3 (database improvements) is complete. All findings from prior research verified against current codebase — 6 missing indexes, migration transaction safety gap, missing PRAGMA synchronous=NORMAL, and unbounded GetMayorMessages query all confirmed.

## What Was Done
- Read and verified existing research doc from prior workflow (fbc464c9)
- Confirmed no indexes exist on `worlds` table (mayor_secret, discord_channel_id)
- Confirmed no index on `swarm_sessions.completed_at`, `swarm_tickets.parent_id`, `swarm_tickets.project_id`, `swarm_project_milestones.workflow_id`
- Verified migration runner in db.go:130-143 lacks transaction wrapping
- Confirmed PRAGMA synchronous=NORMAL is missing (only WAL + busy_timeout set)
- Confirmed GetMayorMessages has no LIMIT — called from mayor_dashboard.go:39 and :98
- Verified sqlc.yaml config and migration file numbering (next is 012)

## What Was NOT Done
- No new research needed — prior findings are comprehensive and accurate

## Key Files
- `thoughts/swarm/research/2026-03-03_00-37-24_CRE-8-3_database-improvements.md` — full research document
- `harness/internal/db/db.go` — migration runner + PRAGMA setup (lines 25-147)
- `harness/internal/db/queries/mayor_messages.sql` — unbounded GetMayorMessages query
- `harness/internal/db/migrations/` — 12 migration files (011 is highest, with collision)
- `harness/sqlc.yaml` — sqlc config for code generation

## Gotchas
- Two migration files share the `011_` prefix — both are registered in migrationFiles slice, so next should be `012_`
- `swarm_ticket_dependencies.project_id` IS indexed but `swarm_tickets.project_id` is NOT — easy to confuse
- PRAGMA statements cannot run inside transactions — don't wrap synchronous=NORMAL in the migration tx
- Changing GetMayorMessages signature breaks the Querier interface — all callers + any mocks must update

## Next Steps
- Proceed to code_plan phase using the research document
- Implementation order: migration file → register in db.go → PRAGMA → tx wrapping → query LIMIT → sqlc regen → verify
