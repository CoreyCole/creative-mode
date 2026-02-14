# Component 1: Go Harness Server Skeleton + DB Layer

## Overview

Set up the foundational Go harness server with Echo routing, SQLite database with migrations, and structured JSONL logging. This is the foundation layer that all other components depend on.

**Dependencies**: None (start immediately)
**Depends on this**: Components 2, 3, 5, 6, 7

## Directory Layout

```
harness/
├── go.mod
├── go.sum
├── main.go
├── justfile
├── sqlc.yaml                           # SQLc configuration
├── internal/
│   ├── server/
│   │   └── server.go                   # Echo router setup, route registration, graceful shutdown
│   ├── db/
│   │   ├── db.go                       # SQLite connection, migrations, WAL mode, embeds sqlc.Queries
│   │   ├── repository.go              # Wrapper methods preserving public API + type aliases
│   │   ├── queries/                    # Annotated SQL files (one per domain entity)
│   │   │   ├── users.sql
│   │   │   ├── sessions.sql
│   │   │   ├── worlds.sql
│   │   │   ├── checkpoints.sql
│   │   │   ├── user_positions.sql
│   │   │   ├── prompt_history.sql
│   │   │   └── messages.sql
│   │   ├── sqlc/                       # Generated code (DO NOT EDIT) — run `sqlc generate`
│   │   │   ├── db.go
│   │   │   ├── models.go
│   │   │   ├── querier.go
│   │   │   ├── users.sql.go
│   │   │   ├── sessions.sql.go
│   │   │   ├── worlds.sql.go
│   │   │   ├── checkpoints.sql.go
│   │   │   ├── user_positions.sql.go
│   │   │   ├── prompt_history.sql.go
│   │   │   └── messages.sql.go
│   │   └── migrations/
│   │       └── 001_initial.sql         # Full schema (runtime migrations)
│   └── logging/
│       └── logger.go                   # slog JSON handler → stderr + file
```

Also create at the repo root:
```
justfile                        # Root justfile (orchestrates harness + template)
.gitignore                      # Ignore data/, *.db, target/, dist/, etc.
scripts/setup.sh                # Placeholder setup script (fleshed out in Component 7)
```

## Database Schema (SQLite)

The full schema in `harness/internal/db/migrations/001_initial.sql`:

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    github_username TEXT NOT NULL,
    avatar_url TEXT,
    role TEXT NOT NULL DEFAULT 'pending',  -- 'admin', 'user', 'pending'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- First user to sign up is auto-promoted to 'admin'.
-- Subsequent users start as 'pending' until an admin approves them.

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,           -- crypto/rand 32-byte hex token
    user_id TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL  -- 7 days from creation
);

CREATE TABLE worlds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    created_by TEXT REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    -- NO active_checkpoint_id here. That's per-user state in user_positions.
);

CREATE TABLE checkpoints (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL,
    parent_checkpoint_id TEXT,  -- NULL for root (initial template)
    name TEXT,
    prompt TEXT,                -- The prompt that created this checkpoint
    status TEXT DEFAULT 'building',  -- building, ready, failed
    build_log TEXT,
    work_summary TEXT,         -- Claude's human-readable summary (from CHANGES.txt)
    files_changed TEXT,        -- JSON array of file paths edited by Claude
    build_duration_ms INTEGER, -- Build time in milliseconds
    dir_path TEXT NOT NULL,    -- Absolute path to project directory
    wasm_path TEXT,            -- Path to built WASM artifacts
    server_port INTEGER,       -- Port for this checkpoint's game server
    created_by TEXT REFERENCES users(id),  -- Who submitted the prompt
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (world_id) REFERENCES worlds(id),
    FOREIGN KEY (parent_checkpoint_id) REFERENCES checkpoints(id)
);

-- Tracks where each user currently is (which world + checkpoint they're viewing)
CREATE TABLE user_positions (
    user_id TEXT NOT NULL REFERENCES users(id),
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    last_accessed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, world_id)
);

CREATE TABLE prompt_history (
    id TEXT PRIMARY KEY,
    checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    prompt_text TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Global chat + system notification log (shared across all players)
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,              -- 'chat', 'build.started', 'build.completed', 'build.failed',
                                    -- 'player.joined', 'player.left'
    user_id TEXT REFERENCES users(id),  -- NULL for system messages
    world_id TEXT REFERENCES worlds(id),  -- context world (NULL for global messages)
    checkpoint_id TEXT REFERENCES checkpoints(id),  -- for build-related messages
    content TEXT NOT NULL,           -- chat text or system message description
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_messages_created_at ON messages(created_at);
```

## Implementation Details

### Database Layer (`harness/internal/db/db.go`)

The DB struct embeds a SQLc `Queries` instance for type-safe database access:

```go
package db

import (
    "database/sql"
    "embed"
    "fmt"
    "creative-mode/harness/internal/db/sqlc"
    _ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
    db      *sql.DB
    queries *sqlc.Queries
}

func New(dbPath string) (*DB, error) {
    sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
    if err != nil {
        return nil, err
    }
    sqlDB.Exec("PRAGMA journal_mode=WAL")
    sqlDB.Exec("PRAGMA busy_timeout=5000")

    d := &DB{
        db:      sqlDB,
        queries: sqlc.New(sqlDB),
    }
    if err := d.runMigrations(); err != nil {
        return nil, fmt.Errorf("running migrations: %w", err)
    }
    return d, nil
}
```

### SQLc Query Files (`harness/internal/db/queries/*.sql`)

SQL queries are defined in annotated `.sql` files, one per domain entity. SQLc generates type-safe Go code from these. Each query has a name and return annotation:

- `:exec` — INSERT/UPDATE/DELETE, returns only `error`
- `:execresult` — returns `sql.Result` (for `RowsAffected()` checks)
- `:one` — returns single row
- `:many` — returns `[]T`

Example (`users.sql`):
```sql
-- name: GetUserByID :one
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE id = ?;

-- name: ListUsers :many
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users ORDER BY created_at ASC;

-- name: UpdateUserRole :execresult
UPDATE users SET role = ? WHERE id = ?;
```

### Repository Layer (`harness/internal/db/repository.go`)

Thin wrapper methods that preserve the existing public API while delegating to SQLc:

```go
// Type aliases expose SQLc-generated models under the db package.
type User = sqlc.User
type Session = sqlc.Session
type World = sqlc.World
type Checkpoint = sqlc.Checkpoint
type Message = sqlc.Message
```

Wrapper methods handle:
1. Mapping individual parameters to SQLc's generated param structs
2. Converting `:one` query `sql.ErrNoRows` to the `nil, nil` pattern
3. Checking `RowsAffected() == 0` for `:execresult` queries
4. Type conversions (e.g., `int` ↔ `int64` for counts/limits)
5. Custom business logic (`GetCheckpointAncestry` does in-memory tree walking)

### Model Types

Model types are generated by SQLc in `internal/db/sqlc/models.go` and re-exported
via type aliases in `repository.go`. Nullable columns use `sql.NullString` / `sql.NullInt64`.
TIMESTAMP columns are overridden to `time.Time` via `sqlc.yaml` config.

### Logging (`harness/internal/logging/logger.go`)

```go
package logging

import (
    "io"
    "log/slog"
    "os"
    "path/filepath"
)

func NewLogger(logDir string) *slog.Logger {
    os.MkdirAll(logDir, 0755)
    logFile, _ := os.OpenFile(
        filepath.Join(logDir, "harness.jsonl"),
        os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
    )
    multiWriter := io.MultiWriter(os.Stderr, logFile)
    return slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))
}
```

### Server Skeleton (`harness/internal/server/server.go`)

Set up the Echo router with route groups. Register placeholder handlers that return 501 Not Implemented for routes that other components will fill in. The key routes to register:

```go
package server

import (
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
)

type Server struct {
    db     *db.DB
    logger *slog.Logger
}

func New(database *db.DB, logger *slog.Logger) *Server {
    return &Server{db: database, logger: logger}
}

func (s *Server) RegisterRoutes(e *echo.Echo) {
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    // Static files (public, no auth)
    e.Static("/assets", "../data/shared-assets")
    e.Static("/static", "static")

    // Auth routes will be registered by Component 2
    // World routes will be registered by Component 3
    // Claude event endpoint will be registered by Component 5
    // UI views will be registered by Component 6

    // For now, register a health check
    e.GET("/health", s.handleHealth)
}

func (s *Server) handleHealth(c echo.Context) error {
    return c.JSON(200, map[string]string{"status": "ok"})
}
```

### Main Entry Point (`harness/main.go`)

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"

    "github.com/labstack/echo/v4"
    // internal imports
)

func main() {
    dataDir := filepath.Join("..", "data")
    os.MkdirAll(dataDir, 0755)

    logger := logging.NewLogger(filepath.Join(dataDir, "logs"))

    database, err := db.New(filepath.Join(dataDir, "creative-mode.db"))
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close()

    e := echo.New()
    srv := server.New(database, logger)
    srv.RegisterRoutes(e)

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    go func() {
        <-ctx.Done()
        logger.Info("Shutting down...")
        e.Shutdown(context.Background())
    }()

    logger.Info("Harness server starting on :8080")
    e.Logger.Fatal(e.Start(":8080"))
}
```

### Root Justfile

```just
default:
    @just --list

harness:
    cd harness && just dev

setup:
    ./scripts/setup.sh
```

### Harness Justfile (`harness/justfile`)

```just
default:
    @just --list

dev:
    go run .

build:
    go build -o harness .

test:
    go test ./...

sqlc:
    sqlc generate

generate:
    sqlc generate
    templ generate
```

### .gitignore

```
data/
*.db
target/
dist/
node_modules/
.env
template-target/
*_templ.go
```

### Go Dependencies

```
github.com/labstack/echo/v4
github.com/mattn/go-sqlite3
github.com/google/uuid
github.com/a-h/templ
github.com/starfederation/datastar-go/datastar
github.com/coreycole/datastarui
golang.org/x/oauth2
```

### Tool Dependencies

```
sqlc                           # SQL code generator (brew install sqlc)
```

## Interface Contract

This component provides to other components:

1. **DB type with query methods** — all components use `*db.DB` for data access
2. **Model types** (User, Session, World, Checkpoint, Message) — SQLc-generated in `internal/db/sqlc/models.go`, re-exported via type aliases in `repository.go` (e.g., `db.User` is `sqlc.User`). Nullable columns use `sql.NullString` / `sql.NullInt64`.
3. **Logger** — `*slog.Logger` passed to all components
4. **Echo instance** — other components register their routes on the same Echo router
5. **Server struct** — other components extend it with additional handlers or compose with it

Other components should be able to:
- Import `internal/db` and use query methods
- Import `internal/logging` for the logger
- Register routes on the Echo instance created in `main.go`

## Success Criteria

### Automated Verification
- [x] `cd harness && sqlc generate` succeeds with no errors
- [x] `cd harness && go build ./...` compiles successfully
- [x] `cd harness && go test ./...` passes
- [ ] Echo server starts and responds on `:8080`
- [ ] `GET /health` returns `{"status": "ok"}`
- [ ] SQLite database creates all tables on first run (users, sessions, worlds, checkpoints, user_positions, prompt_history, messages)
- [ ] WAL mode and busy_timeout are set

### Manual Verification
- [ ] `data/creative-mode.db` created with correct schema
- [ ] `data/logs/harness.jsonl` receives log entries
- [ ] Server gracefully shuts down on SIGINT (Ctrl+C)

## Environment Variables

Required (used by other components but must not crash if missing at this stage):
```
GITHUB_CLIENT_ID          # Used by Component 2 (auth)
GITHUB_CLIENT_SECRET      # Used by Component 2 (auth)
ANTHROPIC_API_KEY          # Used by Component 5 (claude)
HARNESS_URL=http://localhost:8080
```
