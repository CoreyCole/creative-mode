---
ticket: CRE-8-1
workflow: 82e23946
session: b4102b40
version: 1
timestamp: 2026-03-03T02:05:13Z
---

# Plan: Harden secret middlewares — fail-closed + constant-time compare (v1)

## Goal

Fix two security issues in the harness secret middleware: (1) `hookSecretMiddleware` fails open when `CM_HOOK_SECRET` is unset, allowing unauthenticated access to all hook/swarm API routes, and (2) both `hookSecretMiddleware` and `presidentAuthMiddleware` use raw `!=` for secret comparison, which is vulnerable to timing attacks. Replace with `crypto/subtle.ConstantTimeCompare` and add fail-closed behavior.

## Acceptance Criteria

- [ ] `hookSecretMiddleware` returns HTTP 500 when `CM_HOOK_SECRET` is not set (fail-closed)
- [ ] `hookSecretMiddleware` uses `crypto/subtle.ConstantTimeCompare` instead of `!=`
- [ ] `presidentAuthMiddleware` uses `crypto/subtle.ConstantTimeCompare` instead of `!=`
- [ ] `mayorAuthMiddleware` is NOT modified (SQL lookup, not in-memory comparison)
- [ ] `just check` passes

## File Inventory

| # | File | Type | ~Lines | Purpose |
|---|------|------|--------|---------|
| 1 | `harness/internal/server/server.go` | Edit | ~10 | Add `crypto/subtle` import, rewrite `hookSecretMiddleware` to fail-closed + constant-time compare |
| 2 | `harness/internal/server/president_api.go` | Edit | ~5 | Add `crypto/subtle` import, replace `!=` with `subtle.ConstantTimeCompare` in `presidentAuthMiddleware` |

## Implementation Steps

### Step 1: Harden `hookSecretMiddleware` in `server.go`

**File**: `harness/internal/server/server.go`

1. Add `"crypto/subtle"` to the stdlib import block (after `"database/sql"`, alphabetically).

2. Replace the `hookSecretMiddleware` function (lines 712-724) with:

```go
// hookSecretMiddleware validates the X-Hook-Secret header against CM_HOOK_SECRET.
// Fails closed — returns 500 if the secret is not configured.
func hookSecretMiddleware() echo.MiddlewareFunc {
	secret := os.Getenv(swarm.EnvKey("HookSecret"))
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if secret == "" {
				return echo.NewHTTPError(http.StatusInternalServerError, "CM_HOOK_SECRET not configured")
			}
			provided := c.Request().Header.Get("X-Hook-Secret")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
				return echo.NewHTTPError(http.StatusForbidden, "invalid hook secret")
			}
			return next(c)
		}
	}
}
```

Key changes:
- **Fail-closed**: When `secret == ""`, return 500 instead of passing all requests through
- **Constant-time compare**: `subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1` replaces `c.Request().Header.Get("X-Hook-Secret") != secret`
- **Extract header**: `provided` variable for clarity

### Step 2: Harden `presidentAuthMiddleware` in `president_api.go`

**File**: `harness/internal/server/president_api.go`

1. Add `"crypto/subtle"` to the stdlib import block (after `"fmt"`, alphabetically before `"net/http"`).

2. Replace the `!=` comparison on line 30 with `subtle.ConstantTimeCompare`:

```go
if subtle.ConstantTimeCompare([]byte(c.Request().Header.Get("X-President-Secret")), []byte(secret)) != 1 {
```

The fail-closed check (lines 24-28) is already correct and should not be modified.

## Verification Checks

### Compilation
1. `just check` — Full project compilation (Go + Rust + WASM)

### Manual Verification
2. Confirm `crypto/subtle` is imported in both files: `grep -n "crypto/subtle" harness/internal/server/server.go harness/internal/server/president_api.go`
3. Confirm no raw `!=` comparisons remain for secrets: `grep -n 'Header.Get.*!=.*secret' harness/internal/server/server.go harness/internal/server/president_api.go` (should return no results)
4. Confirm `mayor_api.go` was NOT modified: `git diff harness/internal/server/mayor_api.go` (should be empty)

## Risks

- **Local dev without `CM_HOOK_SECRET`**: The fail-closed change means hook/swarm API routes return 500 if the env var isn't set. Mitigation: production always sets it via `.env`, and local dev should too — this is intentional security hardening.
- **Minimal blast radius**: Only two files, two functions. No test files exist for these middlewares. No behavior change for correctly-configured environments.
