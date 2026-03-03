---
ticket: CRE-8-1
phase: research
result: success
session: 28588bcb
workflow: 82e23946
timestamp: 2026-03-03T00:48:06Z
---

## BLUF
Research complete — identified two middlewares needing `crypto/subtle.ConstantTimeCompare` and one (`hookSecretMiddleware`) that also needs a fail-closed fix. The third middleware (`mayorAuthMiddleware`) uses SQL lookup and needs no changes.

## What Was Done
- Identified all three secret middleware functions and their security posture
- Analyzed `hookSecretMiddleware` fail-open vulnerability (passes all requests when `CM_HOOK_SECRET` unset)
- Confirmed `presidentAuthMiddleware` already fails closed but uses raw `!=`
- Confirmed `mayorAuthMiddleware` uses SQL lookup (not vulnerable to timing attack)
- Verified `crypto/subtle` is not used anywhere in the codebase yet
- Checked site-side hook secret usage (client only, no changes needed)
- Wrote research document with implementation pattern

## What Was NOT Done
- No code changes (research phase only)
- No Linear comment (API key expired)

## Key Files
- `harness/internal/server/server.go:714-724` — `hookSecretMiddleware` (fail-open + raw `!=`)
- `harness/internal/server/president_api.go:20-36` — `presidentAuthMiddleware` (raw `!=` only)
- `harness/internal/server/mayor_api.go:83-101` — `mayorAuthMiddleware` (SQL lookup, no changes needed)
- `thoughts/swarm/research/2026-03-03_00-48-06_CRE-8-1_secret-middleware-hardening.md` — full research

## Gotchas
- **Mayor middleware deviation from plan**: Project plan says to add `subtle` to all three middlewares, but `mayorAuthMiddleware` does a SQL lookup — `subtle.ConstantTimeCompare` is not applicable. Skip it.
- **Linear API key expired**: Could not post comment to Linear. The result file is the primary output.
- **Dev mode impact**: Fail-closed on `hookSecretMiddleware` means local dev without `CM_HOOK_SECRET` set will get 500s on hook/swarm routes. This is acceptable — production always sets it.

## Next Steps
- Code plan phase: implement the two-file change (server.go + president_api.go)
- Add `"crypto/subtle"` import to both files
- Verify with `just check`
