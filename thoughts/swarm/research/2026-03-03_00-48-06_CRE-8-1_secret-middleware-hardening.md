---
ticket: CRE-8-1
workflow: 82e23946
session: 28588bcb
timestamp: 2026-03-03T00:48:06Z
---

# Research: Harden secret middlewares — fail-closed + constant-time compare

## Questions

1. Which middleware functions use raw `!=` for secret comparison instead of `crypto/subtle.ConstantTimeCompare`?
2. Which middleware fails open when the secret env var is unset?
3. What is the correct fix for each middleware?

## Findings

### Finding 1: `hookSecretMiddleware` fails open AND uses raw `!=`

**File**: `harness/internal/server/server.go:714-724`

```go
func hookSecretMiddleware() echo.MiddlewareFunc {
    secret := os.Getenv(swarm.EnvKey("HookSecret"))
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if secret != "" && c.Request().Header.Get("X-Hook-Secret") != secret {
                return echo.NewHTTPError(http.StatusForbidden, "invalid hook secret")
            }
            return next(c)
        }
    }
}
```

**Two issues**:
1. **Fails open**: When `CM_HOOK_SECRET` is unset (empty string), the entire condition short-circuits and ALL requests pass through. This means `/api/claude-event`, `/api/world-hatched`, and the entire `/api/swarm/*` group are unprotected.
2. **Timing attack**: Uses `!=` string comparison, which leaks secret length and prefix bytes via response timing.

**Routes affected** (from `RegisterRoutes`):
- `POST /api/claude-event` (line 145)
- `POST /api/world-hatched` (line 148)
- `Group /api/swarm` — all swarm routes (line 166): start, cancel, status, hooks, gates, learnings, health, metrics, doctor

**Fix**:
- Return `http.StatusInternalServerError` (500) when secret is empty — fail closed
- Replace `!=` with `crypto/subtle.ConstantTimeCompare`
- The secret is read once at middleware creation time (closure), so `os.Getenv` only runs once — this is fine

### Finding 2: `presidentAuthMiddleware` already fails closed, but uses raw `!=`

**File**: `harness/internal/server/president_api.go:20-36`

```go
func presidentAuthMiddleware() echo.MiddlewareFunc {
    secret := os.Getenv("PRESIDENT_SECRET")
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if secret == "" {
                return echo.NewHTTPError(
                    http.StatusServiceUnavailable,
                    "president not configured",
                )
            }
            if c.Request().Header.Get("X-President-Secret") != secret {
                return echo.NewHTTPError(http.StatusForbidden, "invalid president secret")
            }
            return next(c)
        }
    }
}
```

**One issue**: Uses raw `!=` at line 30 — timing attack vector.

**Already correct**: Returns 503 when secret is unset — properly fails closed.

**Routes affected**:
- `Group /api/president` (line 158): mayor-status, repo-build, template-update, deploy

**Fix**: Replace `!=` with `crypto/subtle.ConstantTimeCompare` only.

### Finding 3: `mayorAuthMiddleware` uses SQL lookup — no in-memory comparison

**File**: `harness/internal/server/mayor_api.go:83-101`

```go
func (s *Server) mayorAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        secret := c.Request().Header.Get("X-Mayor-Secret")
        if secret == "" {
            return echo.NewHTTPError(http.StatusUnauthorized, "missing mayor secret")
        }
        w, err := s.DB.GetWorldByMayorSecret(c.Request().Context(), sql.NullString{
            String: secret, Valid: true,
        })
        if err != nil {
            return echo.NewHTTPError(http.StatusForbidden, "invalid mayor secret")
        }
        c.Set("mayor_world", &w)
        return next(c)
    }
}
```

**No timing attack risk**: The secret comparison happens inside SQLite via `WHERE mayor_secret = ?`. SQL comparisons don't leak timing information in a meaningful way because query execution time is dominated by I/O, indexing, and parsing overhead rather than byte-by-byte comparison.

**Already fails closed**: Returns 401 when header is missing, 403 when no matching world found.

**No changes needed** for this middleware. The project plan mentions it, but `crypto/subtle.ConstantTimeCompare` is not applicable to SQL lookups — the comparison happens in the database engine, not in Go code.

**Note**: The `worlds.mayor_secret` column currently lacks an index (identified in CRE-8-3), which means a full table scan happens on every mayor API request. Adding the index is a separate concern (CRE-8-3).

### Finding 4: No `crypto/subtle` usage anywhere in the codebase

A global search confirms `crypto/subtle` is not imported in any Go file in the harness or site. The only references are in the project plan documents and research docs.

### Finding 5: `hookSecretMiddleware` is also used by the site (via HTTP header)

The site at `site/internal/mayor/handler.go:638-661` reads `CM_HOOK_SECRET` and sets `X-Hook-Secret` header when calling the harness. The site is a **client** of the hook secret, not a validator — so the site code doesn't need changes. The fix is purely server-side in the harness.

## Architecture Notes

- The secret middlewares are **package-level functions** (not methods on `*Server`), except `mayorAuthMiddleware` which is a method on `*Server` because it needs `s.DB`.
- `hookSecretMiddleware()` reads the env var via `swarm.EnvKey("HookSecret")` which resolves to `CM_HOOK_SECRET`. `presidentAuthMiddleware()` reads `PRESIDENT_SECRET` directly.
- Both `hookSecretMiddleware` and `presidentAuthMiddleware` capture the secret at creation time (in the outer function) and close over it — the env var is read once, not on every request. This is correct and doesn't need to change.
- `crypto/subtle.ConstantTimeCompare` requires `[]byte` args and returns `int` (1 for equal, 0 for not equal). Both input lengths should match for constant-time behavior — use `subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1` as the comparison.

## Risks and Considerations

1. **Breaking change when `CM_HOOK_SECRET` is unset**: Changing `hookSecretMiddleware` to fail-closed (500) means the harness will reject all hook/swarm API calls if the env var isn't set. This is the correct behavior for production security, but could break local dev setups that don't set the var. The existing systemd service and `.env` file always set it, so this should be fine in practice.

2. **Mayor middleware doesn't need `subtle`**: The project plan says to add `subtle.ConstantTimeCompare` to all three middlewares, but `mayorAuthMiddleware` does a SQL lookup — not an in-memory comparison. Applying `subtle` there isn't possible without refactoring the middleware (e.g., fetching the world by other means and then comparing secrets in Go), which would be over-engineering.

3. **Minimal blast radius**: Only two files need changes (`server.go`, `president_api.go`). No test files exist for these middlewares, so no tests need updating.

## Recommendations

### Changes to make:
1. **`server.go:hookSecretMiddleware`**: Add fail-closed check (return 500 if secret is empty), replace `!=` with `subtle.ConstantTimeCompare`
2. **`president_api.go:presidentAuthMiddleware`**: Replace `!=` with `subtle.ConstantTimeCompare` (already fails closed)
3. **Add `"crypto/subtle"` import** to both files

### Changes NOT to make (deviation from project plan):
- **`mayor_api.go`**: Do NOT add `subtle.ConstantTimeCompare` — it uses SQL lookup, not in-memory comparison. No change needed.

### Implementation pattern:
```go
// hookSecretMiddleware — fail-closed + constant-time compare
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

### Verification:
- `just check` must pass
- Manual: unset `CM_HOOK_SECRET` → hook endpoints return 500
- Manual: set wrong secret → hook endpoints return 403
