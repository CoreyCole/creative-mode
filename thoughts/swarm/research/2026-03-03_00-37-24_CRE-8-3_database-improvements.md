---
ticket: CRE-8-3
workflow: fbc464c9
session: 278eadb7
timestamp: 2026-03-03T00:37:24Z
---

# Research: Database Improvements — Indexes, Migration Safety, SQLite Tuning, Query Limits

## Questions

From CRE-8 project plan ticket #3:
1. Which columns need indexes? (specifically `worlds.mayor_secret`, `worlds.discord_channel_id`, `swarm_sessions.completed_at`, `swarm_tickets.parent_id`, `swarm_tickets.project_id`)
2. How should migrations be wrapped in transactions for safety?
3. What SQLite PRAGMA tuning is needed? (specifically `synchronous=NORMAL`)
4. Which queries lack LIMIT clauses and need them?
5. How does sqlc regeneration work after query changes?

## Findings

### 1. Missing Indexes — Confirmed Gaps

**Current index count**: ~25 indexes across all tables. Zero indexes on the `worlds` table.

#### High Priority (auth/hot-path)

| Table | Column | Used By | Impact |
|-------|--------|---------|--------|
| `worlds` | `mayor_secret` | `GetWorldByMayorSecret` (`worlds.sql:28-32`) — auth middleware on every mayor API call | Full table scan per auth check |
| `worlds` | `discord_channel_id` | `GetWorldByDiscordChannel` (`worlds.sql:22-26`) — Discord listener on every incoming message | Full table scan per Discord message |

#### Medium Priority (swarm operations)

| Table | Column | Used By | Impact |
|-------|--------|---------|--------|
| `swarm_sessions` | `completed_at` | `CountActiveSwarmSessions` (`swarm.sql:47-49`) — concurrency gate before spawning sessions | Full scan on every session spawn check |
| `swarm_tickets` | `parent_id` | `ListSwarmTicketsByParent` (`swarm_dependencies.sql:17-21`) — project child lookups | Full scan on project operations |
| `swarm_tickets` | `project_id` | `ListSwarmTicketsByProject` + `DeleteSwarmTicketsByProject` (`swarm_dependencies.sql:24-33`) | Full scan; note `swarm_ticket_dependencies.project_id` IS indexed but `swarm_tickets.project_id` is NOT |

#### Bonus (not in original plan but worth adding)

| Table | Column | Used By |
|-------|--------|---------|
| `swarm_project_milestones` | `workflow_id` | `ListSwarmMilestonesByWorkflow` — no index at all on this table |

### 2. Migration Transaction Safety — Current State

**File**: `harness/internal/db/db.go:117-144`

Current migration execution:
```
1. Check if applied (SELECT COUNT)
2. Read embedded SQL content
3. Execute SQL (d.db.ExecContext)
4. Record as applied (INSERT INTO _migrations)
```

**Problem**: Steps 3 and 4 are NOT wrapped in a transaction. If the server crashes between executing the migration SQL and recording it in `_migrations`, the migration is partially applied but untracked. On next startup:
- Idempotent DDL (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`) → safe, re-runs fine
- Non-idempotent DDL (`ALTER TABLE ADD COLUMN`) → **fails** with "duplicate column name" (migrations 003, 005, 009, 011)
- Table recreation pattern (008, 010) → dangerous, could lose data if only partially applied

**Fix**: Wrap execution + recording in a single transaction:
```go
tx, err := d.db.BeginTx(ctx, nil)
// Execute migration SQL within tx
// Record in _migrations within tx
tx.Commit()
```

**Caveat**: SQLite's `CREATE TABLE`/`DROP TABLE`/`ALTER TABLE` are DDL and work inside transactions. However, `PRAGMA` statements cannot be executed inside transactions. Migrations 001 (initial schema) and some others are pure DDL, so transaction wrapping is safe.

### 3. SQLite PRAGMA Tuning

**File**: `harness/internal/db/db.go:25-48`

Currently set:
- `journal_mode=WAL` (line 37) — correct
- `busy_timeout=5000` (line 44) — correct
- `_foreign_keys=on` (connection string, line 26) — correct

**Missing**: `PRAGMA synchronous=NORMAL`

With WAL mode, SQLite defaults to `synchronous=FULL`. The WAL journal provides crash safety even at `NORMAL` sync level. Switching to `NORMAL` reduces fsync calls (one per checkpoint instead of one per commit), improving write throughput. The SQLite documentation explicitly recommends this for WAL mode.

**Implementation**: Add after the `busy_timeout` PRAGMA:
```go
if _, err := sqlDB.ExecContext(ctx, "PRAGMA synchronous=NORMAL"); err != nil {
    _ = sqlDB.Close()
    return nil, fmt.Errorf("setting synchronous mode: %w", err)
}
```

### 4. Unbounded Queries

**Most critical unbounded query**: `GetMayorMessages` (`mayor_messages.sql:5-7`)
```sql
SELECT ... FROM mayor_messages WHERE world_id = ? ORDER BY created_at ASC;
```
No LIMIT — returns every message ever sent in a world's Discord channel. Called from `mayor_dashboard.go:39` and rendered directly into the dashboard without pagination. Will grow without bound.

**Fix approach**: Add a LIMIT parameter:
```sql
-- name: GetMayorMessages :many
SELECT ... FROM mayor_messages WHERE world_id = ? ORDER BY created_at ASC LIMIT ?;
```

All callers must be updated to pass a limit value. The dashboard currently fetches all messages; a reasonable default is 200-500.

**Other unbounded queries (lower priority)**: Most swarm queries (`ListSwarmEventsByWorkflow`, `ListSwarmSessionsByWorkflow`, `ListSwarmTickets`, etc.) are unbounded but operate on naturally small datasets in the current system. These are noted but not in scope for this ticket per the plan.

### 5. sqlc Regeneration

**Config**: `harness/sqlc.yaml`
- Engine: SQLite
- Schema source: `internal/db/migrations/` (reads all `.sql` files)
- Query source: `internal/db/queries/`
- Output: `internal/db/sqlc/`
- `emit_interface: true` → generates `Querier` interface
- `emit_empty_slices: true` → `:many` returns `[]T{}` not `nil`

**Regeneration command**: `just generate` (from `harness/`) runs `sqlc generate` + `templ generate` + tailwind build. On VPS, run directly. On macOS, must use Docker.

**Important**: After modifying query files, run `just generate` to regenerate `sqlc/` Go files, then verify with `just check`.

### 6. Migration File Naming

The new migration should be numbered `012`. Current highest registered migration: `011_prompt_versions_and_tokens.sql`.

Note: There are two `011_` prefixed files (collision), but both are registered in the `migrationFiles` slice. The new migration should be `012_database_improvements.sql`.

## Architecture Notes

- **Single-writer model**: `SetMaxOpenConns(1)` means all writes are serialized. Index additions only affect read performance (lookups), not write contention.
- **WAL mode**: Read operations don't block writes and vice versa. Adding indexes to `worlds` table is safe even under load.
- **Embedded migrations**: All migrations are compiled into the binary via `//go:embed`. The new migration file must be added to both the `migrations/` directory AND the `migrationFiles` slice in `db.go`.
- **sqlc schema awareness**: sqlc reads all migration files in `internal/db/migrations/` to understand the schema. The new migration's `CREATE INDEX` statements will be visible to sqlc but won't affect generated code (indexes don't change query signatures).

## Risks and Considerations

1. **Migration on running production DB**: The new migration adds indexes to existing tables. `CREATE INDEX IF NOT EXISTS` is idempotent and non-blocking for reads (SQLite takes a brief write lock during index creation). Safe to apply on a running server.

2. **Transaction wrapping for existing migrations**: The fix changes the migration runner for ALL future migrations. It does NOT retroactively re-run existing migrations. This is forward-looking safety.

3. **GetMayorMessages callers**: Changing the query signature from 1 parameter (world_id) to 2 parameters (world_id, limit) will break all callers until they're updated. Must update `mayor_dashboard.go` and any other callers in the same commit.

4. **sqlc regeneration**: After changing `mayor_messages.sql`, the generated Go code in `sqlc/` will change. The `Querier` interface will also change. Any mocks or test doubles implementing `Querier` must be updated.

## Recommendations

### Implementation Order

1. **Create migration file** `012_database_improvements.sql`:
   - `CREATE INDEX IF NOT EXISTS idx_worlds_mayor_secret ON worlds(mayor_secret);`
   - `CREATE INDEX IF NOT EXISTS idx_worlds_discord_channel_id ON worlds(discord_channel_id);`
   - `CREATE INDEX IF NOT EXISTS idx_swarm_sessions_completed_at ON swarm_sessions(completed_at);`
   - `CREATE INDEX IF NOT EXISTS idx_swarm_tickets_parent_id ON swarm_tickets(parent_id);`
   - `CREATE INDEX IF NOT EXISTS idx_swarm_tickets_project_id ON swarm_tickets(project_id);`
   - `CREATE INDEX IF NOT EXISTS idx_swarm_milestones_workflow ON swarm_project_milestones(workflow_id);`

2. **Register migration** in `db.go` `migrationFiles` slice.

3. **Add `PRAGMA synchronous=NORMAL`** in `db.go` after `busy_timeout`.

4. **Wrap migration execution in transactions** in `db.go:117-144`.

5. **Add LIMIT to `GetMayorMessages`** in `mayor_messages.sql`, update callers.

6. **Regenerate sqlc**: `just generate` from `harness/`.

7. **Verify**: `just check` passes.
