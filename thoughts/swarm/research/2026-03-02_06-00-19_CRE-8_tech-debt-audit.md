---
ticket: CRE-8
workflow: e8c72519
session: e4752ae1
timestamp: 2026-03-02T06:00:19Z
---

# Research: Tech Debt Audit

## Questions
1. What areas of the codebase have accumulated tech debt?
2. What cleanup work is needed?
3. What should be prioritized?

## Findings

### 1. Large Go Files Need Splitting

Several Go files exceed 500 lines and mix multiple concerns:

| File | Lines | Concerns Mixed |
|------|-------|----------------|
| `harness/internal/swarmorch/manager.go` | 1981 | Workflow lifecycle, learning capture, metrics, sessions, gates, events |
| `harness/internal/server/create.go` | 1046 | Form handler, world creation, building, auth checks |
| `harness/internal/server/server.go` | 914 | Route registration, auth handlers, SSE setup |
| `harness/internal/swarmorch/project.go` | 851 | Project decompose/plan/verify/spawn |
| `harness/internal/world/game_server.go` | 771 | Game server lifecycle (start, recover, stop) |
| `harness/internal/world/manager.go` | 767 | World creation, checkpoint forking, naming |
| `harness/internal/linear/client.go` | 755 | GraphQL wrapper, mutations, state resolution |
| `site/internal/mayor/handler.go` | 717 | Chat streaming, image processing, world hatching |
| `site/internal/monitor/handler.go` | 716 | Multiple endpoints in one handler |
| `harness/internal/server/swarm_api.go` | 513 | All swarm API routes |

**Recommendation**: Split `manager.go` into `orchestrator.go`, `sessions.go`, `learnings.go`, `gates.go`. Split `create.go` into `world_create.go`, `form_parsing.go`, `chat_handler.go`.

### 2. Missing Test Coverage

Critical business logic packages have zero test files:

| Package | Files | Impact |
|---------|-------|--------|
| `pkg/mayorchat` | 9 .go files, 0 tests | Conversation manager, message persistence, rate limiting untested |
| `pkg/worldchannel` | 7 .go files, 0 tests | Discord channel creation, message classification untested |
| `pkg/markdown` | 1 file, 0 tests | Goldmark rendering untested |
| `pkg/imagegen` | 1 file, 0 tests | Gemini API integration untested |
| `site/internal/*` | 15+ files, 0 tests | Auth, mayor handler, monitor routes untested |

Partially tested: `harness/internal/swarm/` (state machine tested, env config not), `harness/internal/linear/` (basic queries tested, mutations partial).

### 3. Duplicated Discord Auth Logic

Both `harness/internal/auth/auth.go` and `site/internal/auth/auth.go` implement ~150 lines of overlapping logic:
- Discord OAuth callback processing
- Session creation
- User role resolution
- Token exchange

**Recommendation**: Extract to `pkg/discordauth/` shared package.

### 4. Duplicated SSE Endpoint Setup

SSE subscription boilerplate is repeated across:
- `harness/internal/server/events.go`
- `harness/internal/server/mayor_dashboard.go`
- `harness/internal/server/swarm_dashboard.go`

Each manually creates EventBus subscriptions and defers unsubscribe. Could extract a helper.

### 5. Undocumented Error Suppression

15+ instances of `_ = expr` across the codebase silently discard errors without comments explaining why. Key locations:
- `harness/internal/swarmorch/manager.go` lines 143, 196, 269, 397, 629, 748
- `pkg/mayorchat/conversation.go` lines 77, 268, 271, 277, 293, 330, 339

All are non-critical (cleanup, optional telemetry), but lack justification comments.

### 6. Rust/Bevy Magic Numbers

Hardcoded values scattered across templates without named constants:

**2D template `src/camera.rs`**: zoom thresholds (2.0), min zoom (0.5), drag threshold (10.0), tap duration (0.3), scroll factors (0.9/1.1) — all magic numbers.

**2D template `src/interaction.rs`**: color values (`Color::srgba(1.0, 1.0, 1.0, 0.1)` etc.) used inline instead of constants.

**3D template `client/src/main.rs`**: camera sensitivity (0.003), pitch limits (1.55, 1.0), follow speed (15.0), eye height (1.5) — all hardcoded.

**Boardgame `src/board.rs`**: board offset (`+ 4.0`) in coordinate conversion without explanation.

### 7. 3D Client Main.rs Too Large (622 lines)

`templates/3d/client/src/main.rs` mixes camera system, input system, mesh sync, and scene setup. Should split into `camera_system.rs`, `input_system.rs`, `rendering.rs`.

### 8. Duplicate Bridge Plugin

Both `templates/2d/` and `templates/boardgame/` implement near-identical `BridgePlugin` for JS interop. Could be extracted to a shared crate.

### 9. Infrastructure Concerns

**Database migrations**: Manually maintained list in `db.go` — not auto-discovered. Migrations 008 and 010 recreate tables (SQLite limitation), risking data loss on failure. No timestamp-based naming.

**Missing indexes**: `swarm_workflows.workflow_type` (used in filtering), `swarm_events.created_at` (timeline queries).

**No CI/CD pipeline**: No `.github/workflows/` directory. Deployments are manual (`just vps-deploy`). `scripts/check.sh` exists but isn't enforced pre-deploy.

**Docker**: Harness container runs as root. No health checks or resource limits in docker-compose. Claude Code CLI installed via unverified curl pipe.

**Dependency snapshot**: `datastarui` pinned to dev snapshot (`v0.0.0-20260131...`), not a release. No automated dependency update strategy (renovate/dependabot).

**Build race condition**: `scripts/check.sh` uses `/tmp/cm-check-target/` without locking — concurrent `just check` runs could conflict.

### 10. Security Hardening Backlog

A prior research document (`thoughts/CoreyCole/research/2026-02-13_...vps-deployment-security-hardening.md`) identified 11 items still unimplemented:
- Security headers middleware (HSTS, X-Content-Type-Options, X-Frame-Options)
- HTTP rate limiting
- Request body size limits
- Server timeouts (Read, Write, Idle)
- Logout cookie flag mismatch
- Chat/prompt length limits
- `postMessage` origin validation

### 11. Clean Areas (No Issues)

- **Zero TODO/FIXME/HACK comments** in source code — good hygiene
- **No commented-out code blocks** found
- **No bare panics** except 2 config-time validations in `swarm/env.go` (legitimate)
- **Consistent error wrapping** with `fmt.Errorf(...%w...)` throughout Go code
- **Proper concurrency safety** with `sync.Mutex` usage
- **Context propagation** — all long-lived operations accept `context.Context`
- **Boardgame `rules.rs`** — excellent test coverage and documentation

## Architecture Notes

- The codebase follows a monorepo structure with clear separation between harness (Go server), templates (Rust/Bevy clients), site (marketing), and shared packages
- The swarm orchestrator (`internal/swarmorch/`) is the largest and most complex subsystem, accounting for ~3000 lines across `manager.go` and `project.go`
- Database is SQLite with WAL mode — appropriate for single-server deployment but migrations need care
- Deploy topology (EC2 site + VPS harness connected via Tailscale) adds operational complexity

## Risks and Considerations

1. **Splitting manager.go is high-risk**: It's the core orchestrator with tight coupling between methods. Needs careful interface extraction.
2. **Adding tests to pkg/ requires mocking**: `mayorchat` and `worldchannel` depend on Discord API and SQLite — need interface abstractions first.
3. **Migration table recreation**: Current approach works but is fragile. Any future schema changes to `swarm_workflows` or `swarm_sessions` should use ALTER TABLE where possible.
4. **No CI means any refactoring is risky**: Consider adding at least `just check` as a pre-deploy gate.

## Recommendations

### Priority 1 — High Impact, Low Risk
1. **Add missing database indexes** (swarm_workflows.workflow_type, swarm_events.created_at)
2. **Extract magic numbers to named constants** in Rust templates
3. **Add justification comments** to all `_ = ` error suppressions
4. **Add `just check` as pre-deploy gate** in VPS deploy script

### Priority 2 — High Impact, Medium Risk
5. **Add tests to `pkg/mayorchat`** — most critical untested package (onboarding flow)
6. **Split `swarmorch/manager.go`** — largest file, hardest to maintain
7. **Extract shared Discord auth** to `pkg/discordauth/`
8. **Add security headers middleware** (from existing hardening backlog)

### Priority 3 — Medium Impact
9. **Split `server/create.go`** and **`server/server.go`**
10. **Extract 3D client modules** from `main.rs`
11. **Add health checks and resource limits** to docker-compose
12. **Extract shared BridgePlugin** for Bevy templates
13. **Add basic CI** (GitHub Actions running `just check`)

### Priority 4 — Low Impact / Polish
14. **Standardize SSE subscription helper**
15. **Review `#[allow(dead_code)]` annotations** in boardgame template
16. **Pin `datastarui` to release version** when available
17. **Add timestamp-based migration naming**
