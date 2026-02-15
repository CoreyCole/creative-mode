---
date: 2026-02-15T14:20:24-08:00
researcher: CoreyCole
git_commit: cf8f3953f91d50ff1a64a9131939d713bb02eef1
branch: main
repository: creative-mode
topic: "SQLite Persistence for Sessions & Conversations"
tags: [implementation, sqlite, site, auth, mayor, persistence]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: SQLite Persistence for Site Sessions & Conversations

## Task(s)

**Completed:**
1. **Add SQLite persistence for sessions** — Replaced in-memory `map[string]*Session` in `SessionManager` with `*sql.DB` queries. Sessions now survive `air` hot-reloads and server restarts.
2. **Add SQLite persistence for conversation messages** — `ConversationManager` now stores messages in SQLite. Transient state (rate limits, scripted flag) intentionally stays in-memory (resets on restart are fine/desired).
3. **Create `site/internal/db/` package** — New package with schema, pragmas (WAL mode, busy timeout, foreign keys), and open helper.
4. **Wire up DB in `main.go`** and update Docker/gitignore config.
5. **Minor UI tweak** — Moved "Create World" button to bottom-right corner of chat input area (absolute positioned).

## Critical References
- `site/CLAUDE.md` — Site architecture and mayor onboarding flow
- `CLAUDE.md` — Root project instructions (Docker, build rules, deployment)

## Recent changes

- `site/internal/db/db.go` — **New file**. Opens SQLite via `modernc.org/sqlite` (pure Go, no CGO — compatible with existing `CGO_ENABLED=0` in docker-compose). Creates `sessions` and `conversation_messages` tables. Sets WAL mode + `busy_timeout(5000)` + `foreign_keys(on)` via DSN pragmas. `SetMaxOpenConns(1)` for single-writer safety.
- `site/internal/auth/auth.go` — Removed `sync.RWMutex` and `map[string]*Session`. `SessionManager` now holds `*sql.DB`. `GetSession` does `SELECT ... WHERE expires_at > CURRENT_TIMESTAMP`. New private `createSession()` helper does `INSERT`. `SetInviteVerified`, `SetGuildVerified`, `SetSystemPrompt` do `UPDATE`. `HandleLogout` does `DELETE`. `cleanupLoop` does `DELETE ... WHERE expires_at <= CURRENT_TIMESTAMP`. `NewSessionManager` signature changed: `NewSessionManager(config *Config, db *sql.DB)`.
- `site/internal/mayor/session.go` — Hybrid approach: `ConversationManager` holds `*sql.DB` for messages + in-memory `map[string]*transientState` for rate limits and scripted flag. `AddMessage` inserts into SQLite and updates in-memory `LastMessage`. `GetMessages` queries SQLite ordered by `id ASC`. `NewConversationManager` signature changed: `NewConversationManager(db *sql.DB)`.
- `site/main.go:45-54` — DB init block: reads `SITE_DB_PATH` env (defaults to `data/site.db`), calls `db.New()`, passes `database` to both managers. Also fixed a pre-existing `err` shadow lint issue (renamed to `wcErr` at line 75).
- `site/docker-compose.yml:23` — Added `SITE_DB_PATH=data/site.db` env var.
- `site/.gitignore` — Added `data/` to prevent committing the DB file.
- `site/go.mod` / `site/go.sum` — Added `modernc.org/sqlite` v1.45.0 and transitive deps.
- `site/pages/mayor.templ` — Moved "Create World" button to `absolute right-4 bottom-3` positioning outside the `max-w-2xl` container.

## Learnings

- **`modernc.org/sqlite` driver registration name is `"sqlite"`** (not `"sqlite3"` like mattn's CGO driver). This is what we pass to `sql.Open("sqlite", ...)`.
- **Pragmas via DSN query params**: `?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)` — this is the modernc convention, not `?_journal_mode=WAL`.
- **Time format for SQLite**: Using `"2006-01-02 15:04:05"` Go format string to match SQLite's `CURRENT_TIMESTAMP` format. The `GetSession` query compares `expires_at > CURRENT_TIMESTAMP` for expiry checking.
- **No CGO needed**: `modernc.org/sqlite` is pure Go, so it works with `CGO_ENABLED=0` already set in docker-compose. No build toolchain changes needed.

## Artifacts

- `site/internal/db/db.go` — New SQLite package
- `site/internal/auth/auth.go` — Refactored SessionManager
- `site/internal/mayor/session.go` — Refactored ConversationManager
- `site/main.go` — DB wiring
- `site/docker-compose.yml` — Added env var
- `site/.gitignore` — Added data/
- `site/go.mod` / `site/go.sum` — New dependency
- `site/pages/mayor.templ` — Create World button repositioned

## Action Items & Next Steps

1. **Deploy to EC2 marketing site** — The code is ready to deploy. On EC2:
   - Pull the latest code
   - The `SITE_DB_PATH` env defaults to `data/site.db` (relative to working dir), so either:
     - Set `SITE_DB_PATH` in `~/.config/creative-mode/site.env` to an absolute path like `/var/lib/creative-mode/site.db`, OR
     - Let it default and ensure the working dir in the systemd unit has a writable `data/` subdirectory
   - Run `just install && just build` to pick up the new `modernc.org/sqlite` dependency
   - Copy binary and restart: `cp site-linux /tmp/creative-mode-site && sudo systemctl restart creative-mode-site`
   - The DB file and `data/` directory will be auto-created on first run by `os.MkdirAll` in `db.New()`
2. **Commit the changes** — All changes are unstaged. Run `/commit` when ready.
3. **Test locally** — `just up` in `site/`, dev login, chat with mayor, trigger an air rebuild (touch a `.go` file), verify session and conversation persist across restarts.
4. **Consider backup strategy** — The SQLite DB file should be included in any EC2 backup/snapshot strategy. WAL mode means there may be `-wal` and `-shm` sidecar files alongside `site.db`.

## Other Notes

- **EC2 deployment model**: The marketing site runs as a native Go binary under systemd on EC2 (not Docker). See `site/CLAUDE.md` "EC2 production deployment" section. The systemd unit runs as the `ubuntu` user with `CAP_NET_BIND_SERVICE`. The env file is at `~/.config/creative-mode/site.env`.
- **Docker (local dev)**: No additional Docker changes needed — the bind mount `.:/app:cached` already maps `site/data/` on host to `/app/data/` in the container. The `SITE_DB_PATH=data/site.db` env var is relative to `/app` (the working dir in Docker).
- **`just check` passes** — All Go lint (golangci-lint), formatting, Rust clippy, and templ checks pass.
- **Conversation cleanup**: Messages older than 24 hours are still cleaned up by `cleanupLoop` (hourly `DELETE`). Sessions are cleaned up when `expires_at <= CURRENT_TIMESTAMP` (also hourly).
