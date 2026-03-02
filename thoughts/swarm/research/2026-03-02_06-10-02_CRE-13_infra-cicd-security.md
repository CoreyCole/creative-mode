---
ticket: CRE-13
workflow: c61b3839
session: e73675fe
timestamp: 2026-03-02T06:10:02Z
---

# Research: Infrastructure, CI/CD, and Security Hardening

## Questions

1. What database improvements are needed?
2. What CI setup is needed?
3. What Docker hardening is needed?
4. What remaining security hardening items exist?

## Findings

### 1. Database Improvements

#### 1.1 Missing Indexes on Hot Paths

Several columns queried in WHERE clauses have no index:

| Column | Query | Caller | Impact |
|--------|-------|--------|--------|
| `worlds.mayor_secret` | `GetWorldByMayorSecret` | `mayorAuthMiddleware` — every mayor API call | Full table scan per request |
| `worlds.discord_channel_id` | `GetWorldByDiscordChannel` | Discord listener — every incoming message | Full table scan per message |
| `swarm_sessions.completed_at` | `CountActiveSwarmSessions` (`WHERE completed_at IS NULL`) | Swarm health checks | Scans all session rows |
| `swarm_tickets.parent_id` | `ListSwarmTicketsByParent` | Project orchestrator | Scans all ticket rows |
| `swarm_tickets.project_id` | `ListSwarmTicketsByProject` | Project orchestrator | Scans all ticket rows |

At current scale this is invisible, but `mayor_secret` and `discord_channel_id` are hot paths that will degrade as worlds grow.

**Recommendation**: Add a migration with indexes for at minimum `worlds.mayor_secret` and `worlds.discord_channel_id`.

#### 1.2 Migration Safety — No Transaction Wrapping

Migrations at `harness/internal/db/db.go:119-141` execute SQL and record completion as separate statements — no transaction wrapping. A crash between steps can leave a partially-applied migration not tracked in `_migrations`. Migrations 008 and 010 use the destructive `DROP TABLE` + `ALTER TABLE RENAME` pattern for CHECK constraint changes, making this especially risky.

**Recommendation**: Wrap each migration in a transaction (`BEGIN` … `INSERT INTO _migrations` … `COMMIT`).

#### 1.3 N+1 Query in President Status

`handlePresidentMayorStatus` (`internal/server/president_api.go:40-110`) fetches all worlds then executes 3 additional queries per world (checkpoints, builds, activity) = 1 + 3N queries. All serialize through `MaxOpenConns(1)`.

**Recommendation**: Consider a batch query or accept the N+1 given few worlds currently.

#### 1.4 Unbounded Query — GetMayorMessages

`GetMayorMessages` (`internal/db/queries/mayor_messages.sql:6-7`) has no LIMIT clause. Over time a world with heavy Discord activity will load thousands of messages into memory.

**Recommendation**: Add a LIMIT parameter or switch to `GetRecentMayorMessages` (which already exists with LIMIT).

#### 1.5 SQLite Tuning Opportunities

Current pragmas: WAL mode, 5s busy timeout, foreign keys enabled. Missing:
- `PRAGMA synchronous=NORMAL` — safe with WAL, reduces fsync overhead (~2x write performance)
- `PRAGMA cache_size` — default is 2000 pages (8 MB). Could increase for workloads with large reads
- `PRAGMA mmap_size` — enables memory-mapped I/O for faster reads

**Recommendation**: Add `synchronous=NORMAL` (safe with WAL per SQLite docs). Other tuning optional.

#### 1.6 No Session Cache

`SessionMiddleware` executes 2 DB queries per authenticated request (session lookup + user lookup). Both hit indexed PRIMARY KEY columns so they're fast, but there's no in-memory cache.

**Recommendation**: Low priority. Consider an LRU session cache only if request latency becomes an issue.

### 2. CI/CD Setup

#### 2.1 Current State — No Cloud CI

There is **no `.github/` directory** and no GitHub Actions workflows. Quality enforcement relies entirely on:
- `just check` (manual) — format, generate, lint (Go + Rust clippy)
- Claude Code "Stop" hook — runs `check.sh` on every agent session end, exit code 2 blocks the session
- Claude Code deny list — prevents direct `cargo build`/`go build` on macOS

#### 2.2 Test Gap in Check Pipeline

`check.sh` runs formatters + code generators + linters but does **NOT run `go test`**. Tests exist (18 test files in `harness/internal/swarm/` and `swarmorch/`) but are only run manually via `cd harness && just test`.

**Recommendation**: Add `go test ./...` to `check.sh` (after lint, before exit).

#### 2.3 No Tests for Site

The `site/` directory has zero test files. The site's `.golangci.yml` enables only 5 linters vs the harness's 40+.

**Recommendation**: Low priority — site is simple. Could add basic handler tests.

#### 2.4 GitHub Actions Opportunity

A basic CI workflow would provide:
- Quality gate on PRs (especially for swarm-generated PRs)
- Protection for `main` branch
- Automated test runs

Suggested pipeline:
```yaml
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - Format check (golangci-lint fmt, cargo fmt --check)
      - Generate (sqlc, templ, tailwind)
      - Lint (golangci-lint, cargo clippy)
      - Test (go test ./...)
```

**Constraint**: Rust/WASM clippy needs `wasm32-unknown-unknown` target and ~5 GB RAM. The CI runner must handle this.

#### 2.5 Deployment Automation

- **Site** (EC2): Already auto-deploys via GitHub webhook (`internal/webhook/handler.go`) with HMAC-SHA256 verification. No changes needed.
- **Harness** (VPS): Manual `just vps-deploy` (git pull → build → systemctl restart). Could add a webhook or CD trigger, but the VPS is only accessible via Tailscale, limiting options.

### 3. Docker Hardening

#### 3.1 Context — Dev-Only Docker

Both Docker setups are **development-only**. Production runs on bare metal:
- Harness: `air` under systemd on VPS
- Site: native Go binary under systemd on EC2

Docker is used only for macOS local development.

#### 3.2 Current Issues

| Issue | Harness | Site |
|-------|---------|------|
| Runs as root | Yes | Yes |
| No capabilities dropped | Yes | Yes |
| No resource limits | Yes | Yes |
| No health checks | Yes | Yes |
| No `--init` (tini) | Yes | Yes |
| Base image not pinned (digest) | `golang:1.24-bookworm` | `golang:1.25-alpine` |
| Base image not minimal | Bookworm (large) | Alpine (minimal) |
| No `no-new-privileges` | Yes | Yes |
| No read-only rootfs | Yes | Yes |
| Curl-pipe-bash installs | Claude CLI, rustup | — |
| Cargo tools not version-pinned | `trunk`, `cargo-watch` | — |
| Ports bound to 0.0.0.0 | 201 ports | 1 port |
| `.dockerignore` | Present | Missing |

#### 3.3 Recommendations (Prioritized)

**High value / low effort**:
1. Add `init: true` to docker-compose (enables tini for zombie reaping)
2. Add `mem_limit` to prevent WASM builds from OOMing the host
3. Bind ports to `127.0.0.1` instead of `0.0.0.0`
4. Add `.dockerignore` to site

**Medium value / medium effort**:
5. Add non-root USER to Dockerfiles
6. Pin base images by digest
7. Pin all cargo tool versions
8. Add health checks

**Lower priority (dev-only context)**:
9. Drop capabilities
10. Set `read_only: true` with explicit tmpfs mounts
11. Multi-stage builds
12. Replace curl-pipe-bash with checksummed downloads

### 4. Security Hardening

#### 4.1 HIGH — hookSecretMiddleware Fails Open

`hookSecretMiddleware` at `server.go:712-722` passes ALL requests when `CM_HOOK_SECRET` is unset:
```go
if secret != "" && c.Request().Header.Get("X-Hook-Secret") != secret {
```

This leaves the swarm API, Claude event endpoint, and world-hatched webhook completely unprotected if the env var is missing. The president middleware correctly fails closed (returns 503).

**Recommendation**: Fail closed — return 500 "hook secret not configured" when `CM_HOOK_SECRET` is empty.

#### 4.2 MEDIUM — No CSRF Tokens

Session-authenticated POST endpoints have no CSRF protection:
- `POST /api/chat`, `POST /world/:worldID/prompt`, `POST /world/create`
- `POST /swarm/:id/approve|reject|cancel`
- `POST /admin/users/:userID/approve|reject`

`SameSite=Lax` cookies provide partial mitigation (blocks cross-origin POST from forms). Datastar's fetch-based actions are same-origin only. Residual risk: cross-site form POST.

**Recommendation**: Add Echo CSRF middleware for session-authenticated routes, or accept the risk given `SameSite=Lax` and the internal-only access model.

#### 4.3 MEDIUM — No Request Body Size Limits

No `BodyLimit` middleware anywhere. An attacker (or buggy client) could send arbitrarily large request bodies, causing OOM.

**Recommendation**: Add `e.Use(middleware.BodyLimit("10M"))` or similar.

#### 4.4 MEDIUM — Timing-Unsafe Secret Comparison

All three secret middlewares use `!=` for string comparison:
- `hookSecretMiddleware` (`server.go:716`)
- `presidentAuthMiddleware` (`president_api.go:30`)
- `mayorAuthMiddleware` (`mayor_api.go:90`)

**Recommendation**: Use `crypto/subtle.ConstantTimeCompare`.

#### 4.5 MEDIUM — Swarm Dashboard Gate Actions Not Admin-Only

`/swarm/:id/approve`, `/swarm/:id/reject`, `/swarm/:id/cancel` are accessible to any approved user via the dashboard. The API equivalents use hook secret auth.

**Recommendation**: Gate dashboard swarm actions behind `AdminMiddleware`.

#### 4.6 LOW — No Security Response Headers

Missing headers: `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Strict-Transport-Security`.

**Recommendation**: Add Echo `middleware.Secure()` or a custom middleware with these headers.

#### 4.7 LOW — Failed Auth Not Logged

Mayor and hook secret middleware return 403 without logging the failed attempt. This hinders security monitoring.

**Recommendation**: Log failed auth attempts with source IP.

#### 4.8 LOW — First-User-Is-Admin

`auth.go:219` grants the first registered user admin role. On a fresh deployment, whoever OAuth-logins first becomes admin.

**Recommendation**: Acceptable given controlled deployment. Could add an `ADMIN_DISCORD_ID` env var for explicit admin designation.

#### 4.9 INFORMATIONAL — Secrets in Skill Files on Disk

President, hook, and mayor secrets are embedded in plaintext agent skill files written to `data/openclaw/` at runtime. The directory is gitignored and only accessible to the system user.

**Recommendation**: Acceptable given the deployment model. No action needed unless the threat model changes.

#### 4.10 INFORMATIONAL — DEV_MODE Auth Bypass

`DEV_MODE=true` exposes `/dev/auth/login` allowing arbitrary role selection. Production must never set this.

**Recommendation**: Verify `DEV_MODE` is not set in production `.env`. Consider logging a startup warning if `DEV_MODE=true` and `HARNESS_URL` is not localhost.

## Architecture Notes

- **SQLite single-writer model**: `MaxOpenConns(1)` is correct for SQLite. All DB operations serialize through one connection. WAL mode allows concurrent reads from other processes (e.g., SQLite CLI for debugging).
- **Tailscale network isolation**: The harness VPS blocks all public traffic via UFW. Only Tailscale peers can reach port 8080. This significantly reduces the attack surface — most security findings assume an attacker on the Tailscale network or a compromised authenticated session.
- **Claude-as-CI**: The Stop hook in `.claude/settings.json` acts as a CI gate for AI agent sessions. This is creative but doesn't protect human pushes or non-Claude workflows.
- **Self-deploying site**: The marketing site's GitHub webhook CD pipeline is well-implemented with HMAC-SHA256 signature verification, mutex-guarded rebuilds, and atomic binary replacement.

## Risks and Considerations

1. **Migration crash safety**: The biggest technical debt — a crash during migrations 008 or 010 (which use DROP TABLE + RENAME) could lose a table entirely since there's no transaction wrapping.
2. **No CI for PRs**: Swarm-generated PRs currently have no automated quality gate beyond the Stop hook that ran during the session. A reviewer must trust that the agent's check passed.
3. **Open-by-default hook secret**: If `CM_HOOK_SECRET` is accidentally unset, the swarm API is wide open. This is the highest-severity security finding.
4. **Docker is dev-only**: Most Docker hardening items are lower priority since production doesn't use Docker. Focus effort on production security (items 4.1–4.5).

## Recommendations

### Priority 1 — Quick Wins (Low effort, high impact)
1. Fix `hookSecretMiddleware` to fail closed when secret is unset
2. Add `crypto/subtle.ConstantTimeCompare` for all secret comparisons
3. Add `middleware.BodyLimit("10M")` to Echo
4. Add `go test ./...` to `check.sh`
5. Add missing indexes for `worlds.mayor_secret` and `worlds.discord_channel_id`
6. Add LIMIT to `GetMayorMessages` query

### Priority 2 — Medium Effort
7. Wrap migrations in transactions
8. Add security response headers via Echo middleware
9. Gate swarm dashboard actions behind AdminMiddleware
10. Add `PRAGMA synchronous=NORMAL` for SQLite performance
11. Log failed auth attempts

### Priority 3 — Larger Efforts
12. Set up GitHub Actions CI (format + lint + test)
13. Docker hardening (non-root user, resource limits, port binding, init)
14. Add CSRF middleware for session-authenticated routes

### Priority 4 — Nice to Have
15. In-memory session cache
16. Batch query for president mayor status
17. Site test coverage
18. Pin Docker base images by digest
