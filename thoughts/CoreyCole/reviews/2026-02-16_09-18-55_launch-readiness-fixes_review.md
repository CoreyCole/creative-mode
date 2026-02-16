---
date: 2026-02-16T09:18:55+0000
reviewer: Claude (Staff Eng Review)
git_commit: 4e18c93f584e238f771be0484df6eca934e65687
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-16_09-12-34_launch-readiness-fixes.md
status: complete
type: plan_review
---

# Plan Review: Launch Readiness Fixes

### Summary

Well-structured plan with accurate code references and good phasing. However, the proposed CSP will **break all Datastar interactivity** because it's missing `'unsafe-eval'`, which Datastar requires for its `Function()` constructor-based expression engine. Two other implementation details — UTF-8-unsafe message truncation and an unbounded HTTP client in `downloadOnboardingJSON` — need fixing before launch.

### Critical Issues (Must Address Before Implementation)

These issues could cause significant problems if not resolved:

1. **CSP missing `'unsafe-eval'` will break Datastar completely**
   - Problem: The proposed CSP sets `script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net` but omits `'unsafe-eval'`. Datastar evaluates **all** expressions (`data-on:click`, `data-show`, `data-class`, `data-signals`) using the `Function()` constructor internally ([confirmed by official docs](https://data-star.dev/reference/security)). Without `'unsafe-eval'`, the browser will refuse to execute every expression, breaking theme toggling, chat submission, cover art buttons, form interactions — effectively the entire site.
   - Risk: Deploying this CSP makes the site non-functional. There are 40+ occurrences of Datastar expression attributes across 7 template files in `site/`.
   - Suggestion: Add `'unsafe-eval'` to `script-src`:
     ```
     script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net
     ```
     This is the [official Datastar recommendation](https://data-star.dev/reference/security). Document the trade-off (weakens CSP XSS protection) but it's unavoidable with Datastar's architecture.

2. **Message truncation produces invalid UTF-8**
   - Problem: Phase 3.2 uses `content[:maxMessageLen]` which slices by byte offset, not character boundary. Multi-byte UTF-8 characters (emoji like the egg marker, CJK characters, accented text) will be split mid-codepoint.
   - Risk: Invalid UTF-8 can crash `json.Marshal`, corrupt SQLite storage, or produce garbled display. Users who paste emoji-heavy text or non-ASCII content will trigger this.
   - Suggestion: Use `[]rune` conversion:
     ```go
     const maxMessageLen = 2000
     runes := []rune(content)
     if len(runes) > maxMessageLen {
         content = string(runes[:maxMessageLen])
     }
     ```
     Note: This also aligns with Discord's own 2000-character limit, which counts characters, not bytes.

3. **`downloadOnboardingJSON` has no timeout or size limit**
   - Problem: Phase 4.3 introduces `http.Get(url)` to download JSON from Discord CDN attachments. Go's default `http.Client` has **no timeout**, and `json.NewDecoder(resp.Body).Decode(...)` reads the entire body into memory with no size limit.
   - Risk: (a) A slow or hung Discord CDN response blocks the goroutine indefinitely. (b) A maliciously large attachment (Discord allows up to 25MB for bots) could cause the server to allocate excessive memory. (c) Multiple concurrent reads of large attachments could OOM the server.
   - Suggestion: Use a client with timeout and limit the response body:
     ```go
     func (c *Client) downloadOnboardingJSON(url string) (*OnboardingData, error) {
         client := &http.Client{Timeout: 15 * time.Second}
         resp, err := client.Get(url)
         if err != nil {
             return nil, fmt.Errorf("downloading onboarding attachment: %w", err)
         }
         defer resp.Body.Close()

         // Limit to 1MB — onboarding JSON should be small.
         limited := io.LimitReader(resp.Body, 1<<20)
         var data OnboardingData
         if err := json.NewDecoder(limited).Decode(&data); err != nil {
             return nil, fmt.Errorf("decoding onboarding attachment: %w", err)
         }
         return &data, nil
     }
     ```
     Requires `"io"` and `"time"` imports.

### Concerns (Should Address)

These warrant attention but aren't blockers:

1. **Invite rate limiter leaks memory**
   - Observation: The `inviteAttempts` map (`map[string]time.Time`) grows indefinitely — every session that attempts an invite code adds an entry that's never removed. Sessions expire after 7 days, but the map entries persist forever.
   - Suggestion: Either (a) add a periodic cleanup goroutine that removes entries older than 1 minute, or (b) use a simpler approach like `sync.Map` with a goroutine that Range/Deletes stale entries every 5 minutes. For launch scale this is unlikely to matter, but it's a latent leak.

2. **Static asset cache may serve stale assets after deployment**
   - Observation: CSS already uses content-hashed filenames (`out.*.css` via `filepath.Glob` in `root.templ:14`), so 24h cache is fine there. But `favicon.png`, `banner.jpeg`, and other static assets have fixed filenames — they'll serve stale for up to 24 hours after deployment.
   - Suggestion: Either (a) accept this trade-off for launch (24h is reasonable for images), or (b) add a cache-busting query param like `?v={commit}` to asset URLs in templates. Option (a) is fine for launch.

3. **`sendAndPin` and `formatOnboardingMessage` removal needs care**
   - Observation: The plan says to remove `sendAndPin` and `formatOnboardingMessage` after rewriting `PinOnboardingData`. The plan correctly notes that `splitConversation` and `extractJSON` must be kept for the legacy reader. Just flagging to verify at implementation time that no other callers exist.
   - Suggestion: `grep -r sendAndPin` and `grep -r formatOnboardingMessage` before removing to confirm they're only called from `PinOnboardingData`.

4. **WriteTimeout of 120s may be tight for edge cases**
   - Observation: The SSE response for `/mayor/chat` involves Claude streaming (10-30s typical) plus optional cover art generation (3-10s Gemini call). In the worst case, a slow Claude response followed by cover art generation could approach 120s.
   - Suggestion: Consider 180s for `WriteTimeout` to provide more margin for slow API responses. Alternatively, keep 120s and accept that very slow responses will be cut off (acceptable for launch).

### Questions (Need Clarification)

These need answers before proceeding:

1. Has `@v1.0.0-RC.7` been verified as compatible with the Go SDK `v1.1.0` currently used server-side? Datastar CDN bundles and Go SDK versions don't always align — a mismatch could cause signal protocol incompatibilities.
2. Does `middleware.BodyLimit("1M")` affect SSE **response** streaming, or only request bodies? Echo's documentation suggests request-only, but worth a quick test since SSE streams could be >1MB for long conversations.
3. Discord CDN attachment URLs have expiration tokens — will the `downloadOnboardingJSON` helper work if the pinned message is hours or days old? The URL might need to be re-fetched from the message's `Attachments` field each time.

### Suggestions (Nice to Have)

Optional improvements:

1. **Add `Referrer-Policy: strict-origin-when-cross-origin`** to the security headers — Echo's `middleware.Secure` supports it via `ReferrerPolicy` config field.
2. **Consider `Permissions-Policy`** to disable unused browser APIs (camera, microphone, geolocation) — low effort, good defense-in-depth.
3. **Pin Datastar with an integrity hash** (SRI): `<script src="..." integrity="sha384-...">` — prevents CDN compromise. This requires computing the hash from the specific version's bundle.
4. **Log rate-limited invite attempts** — useful for detecting brute-force attempts post-launch. A simple `c.Logger().Warnf("Rate-limited invite attempt from session %s", session.ID)` would suffice.

### What's Good

Positive observations worth noting:

- **Accurate code references**: Every file path and line number in the plan was verified correct against the actual codebase. The planner clearly read the code before writing.
- **Good phasing**: Each phase is independently deployable with clear success criteria. Phase 1 (security headers) can ship immediately without waiting for later phases.
- **Backwards compatibility for Discord reader**: The dual-format approach in Phase 4 (file attachment for new channels, code-block fallback for existing) is well-designed and prevents breakage of existing worlds.
- **Explicit "What We're NOT Doing" section**: Clear scope boundaries prevent creep. The P2 items are all reasonable deferrals.
- **Re-hatch protection already verified**: The plan correctly identified that `SetHatched` is already in place (`session.go:185-200`, called at `handler.go:302` and `handler.go:545`).
- **devMode awareness**: The plan correctly identified that `devMode` needs to move earlier in `main.go` for CORS config, and that localhost must be allowed in dev mode.

### Recommended Next Steps

1. **Fix the CSP** (Critical #1) — add `'unsafe-eval'` to `script-src` before implementing Phase 1
2. **Fix message truncation** (Critical #2) — use `[]rune` slicing in Phase 3.2
3. **Add timeout + size limit to `downloadOnboardingJSON`** (Critical #3) — before implementing Phase 4.3
4. **Verify Datastar version compatibility** (Question #1) — test `@v1.0.0-RC.7` locally before deploying
5. **Implement Phase 1 first** and verify with `curl -I` + browser smoke test before proceeding to later phases
6. **Test the full onboarding flow** end-to-end after all phases — the success criteria in the plan are good, follow them
