---
name: database-conventions
description: SQLite WAL mode, SQLC query generation, migration registration pattern, transaction handling
tags: [sqlite, sqlc, migrations, database, schema, queries]
last_verified: 2026-03-08
---

# Database Conventions

## SQLite Configuration

- WAL mode enabled on connection
- Single file at `data/creative-mode.db`
- Wrapper in `internal/db/db.go`

## Migrations

- Location: `harness/internal/db/migrations/NNN_description.sql`
- **Critical**: Migrations must be manually added to `migrationFiles` slice in `db.go` — NOT auto-discovered
- Naming: `001_initial.sql`, `002_description.sql`, etc.
- Applied in order on startup via `RunMigrations()`

## SQLC

- Config: `harness/sqlc.yaml`
- Queries: `harness/internal/db/queries/*.sql`
- Generated code: `harness/internal/db/sqlc/`
- Regenerate: `cd harness && sqlc generate` (or `just generate`)
- Query annotations: `-- name: QueryName :one`, `:many`, `:exec`, `:execresult`

## Query Patterns

```sql
-- name: GetWorld :one
SELECT * FROM worlds WHERE id = ?;

-- name: ListWorlds :many
SELECT * FROM worlds ORDER BY created_at DESC;

-- name: CreateWorld :exec
INSERT INTO worlds (id, name, template) VALUES (?, ?, ?);
```

## Transaction Handling

Use `db.BeginTx()` for multi-statement operations. The DB wrapper exposes both direct query methods and transaction support.
