---
date: 2026-02-11T12:37:55-08:00
researcher: CoreyCole
git_commit: f8fc608931d3d753f0882f5fe601c3ce14ec1fe8
branch: main
repository: creative-mode
topic: "Dev Auth for Multi-User E2E Testing"
tags: [implementation, auth, dev-mode, e2e-testing, playwright]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Dev Auth for Multi-User E2E Testing

## Task(s)

| Task | Status |
|------|--------|
| Implement dev-only email/username login gated behind `DEV_MODE=true` | **Completed** |
| Enable auth handler creation without GitHub credentials in dev mode | **Completed** |
| Build verification (generate + build + lint) | **Completed** |
| E2E testing with playwright-cli multi-session | **Planned — for next agent** |

The implementation follows the plan at the transcript: `/Users/coreycole/.claude/projects/-Users-coreycole-cdev-creative-mode/4c59f4a1-0033-485b-8f23-594bf26f8b11.jsonl`

## Critical References

- `harness/CLAUDE.md` — Auth middleware chain docs, Datastar patterns, templ patterns
- `CLAUDE.md` — playwright-cli usage, E2E testing tips (especially Datastar form interop)

## Recent Changes

- `harness/internal/auth/auth.go:28-37` — Added exported role constants (`RoleAdmin`, `RoleUser`, `RolePending`) and `hash/fnv` import
- `harness/internal/auth/auth.go:418-493` — Added `HandleDevLogin` POST handler and `devGitHubID` helper
- `harness/internal/auth/middleware.go:66,83` — Updated to use role constants
- `harness/views/login/login.templ` — `Page()` now accepts `devMode bool`; added `DevLoginForm()` component
- `harness/internal/server/server.go:55,62,67` — Pass `s.dev != nil` to `login.Page()`
- `harness/internal/server/server.go:108-110` — Register `POST /dev/auth/login` route in DEV_MODE block
- `harness/internal/server/server.go:70` — Use `auth.RolePending` constant
- `harness/main.go:83-106` — Refactored to switch statement; added dev-mode auth handler case; consolidated `baseURL`/`harnessURL` into single variable
- `harness/static/styles.css:55-62` — Added `.dev-login`, `.dev-divider`, `.dev-login-form`, `.dev-input`, `.dev-select` styles

## Learnings

- **Dev GitHub IDs use negative numbers**: `devGitHubID()` uses FNV-32a hash negated, so dev users never collide with real GitHub user IDs (which are always positive).
- **Auth handler is required for all auth routes**: The server's `RegisterRoutes` returns early if `AuthHandler == nil` (line 121-123 in server.go), so creating the handler in dev mode is essential — otherwise even the logout route won't be registered.
- **Lint strictness**: The project uses `goconst` (3+ occurrences triggers), `gocritic` (if-else chains → switch), and `golines` (line length). All role string literals across `auth.go`, `middleware.go`, and `server.go` needed to use the new constants to satisfy `goconst`.
- **`baseURL` consolidation**: `main.go` had duplicate `HARNESS_URL` reads (`baseURL` for auth, `harnessURL` for orchestrator). Consolidated to single `baseURL` to fix `goconst` and simplify.

## Artifacts

- `harness/internal/auth/auth.go` — HandleDevLogin + devGitHubID + role constants
- `harness/views/login/login.templ` — DevLoginForm component
- `harness/internal/server/server.go` — Route registration + devMode template param
- `harness/main.go` — Dev mode auth handler creation
- `harness/static/styles.css` — Dev login form styles
- `harness/internal/auth/middleware.go` — Role constant usage (lint fix)

## Action Items & Next Steps

1. **Start the harness with `DEV_MODE=true`** (no GitHub creds needed):
   ```bash
   cd /Users/coreycole/cdev/creative-mode/harness && DEV_MODE=true just dev
   ```

2. **Test multi-user sessions with playwright-cli**:
   ```bash
   playwright-cli -s=admin open http://localhost:8080 --headed --persistent
   playwright-cli -s=user2 open http://localhost:8080 --headed --persistent
   ```

3. **Verify the following scenarios**:
   - Admin session: fill username "admin", role "admin", submit → should land on lobby
   - User session: fill username "testuser", role "user", submit → should land on lobby
   - Both sessions are independent (different usernames in header)
   - Pending role: log in as "pending-user" with role "pending" → should see pending page
   - Admin page: admin session can access `/admin/users`, user2 session gets 403
   - Re-login with same username retains the same user (deterministic GitHub ID)
   - Role change: log in as existing user with different role → role updates

4. **Enable the 3 skipped E2E tests** that require multi-user roles (if they exist as test files)

## Other Notes

- The dev login form has an orange dashed border to visually distinguish it from the real GitHub OAuth login.
- The form uses a standard HTML form POST (not Datastar signals), so playwright `fill` + `click` should work normally — no need for the `page.evaluate(fetch(...))` workaround documented in CLAUDE.md for Datastar forms.
- Named playwright sessions (`-s=name`) have fully isolated cookie jars, so each session is a completely independent user.
- Changes are uncommitted — commit when testing is verified.
