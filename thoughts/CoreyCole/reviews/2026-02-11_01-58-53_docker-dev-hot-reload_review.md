---
date: 2026-02-11T01:58:53-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 1e6cb945c77d3d54bcce402a8dbc3ebe12584994
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md
status: complete
type: plan_review
---

# Plan Review: Docker Dev Hot-Reload Implementation

### Summary

Well-structured plan with a clever architecture (host-side FSEvents + container-side builds + SSE morph), but contains a **critical factual error** about the layout system that invalidates the morph strategy for the most complex page in the app. The plan also has fragile HTML parsing, a double-rebuild race, and insufficient auth gating on endpoints that can kill the server process.

### Critical Issues (Must Address Before Implementation)

1. **`world.templ` does NOT use `layout.Base()` — plan's core assumption is wrong**
   - Problem: The plan states (Learning #5, Architecture section): "Layout is the single injection point: All pages use `layout.Base()`." This is false. `harness/views/world/world.templ:11-31` constructs its own `<!DOCTYPE html>`, `<html>`, `<body>` structure and only uses `layout.Head()` for the `<head>` block. It does NOT use `layout.Base()`.
   - Risk: The world page — which is the primary interactive page with iframe, overlay, SSE events, and Datastar signals — will have **no `#page-content` wrapper** and **no `#dev-sse` element**. Hot-reload won't work at all on this page. Only login, pending, admin, and lobby pages use `layout.Base()`.
   - Verified: `grep -r "layout.Base" harness/views/**/*.templ` returns only: `login.templ`, `pending.templ`, `admin.templ`, `lobby.templ`. World page is missing.
   - Suggestion: Two options:
     - (a) Refactor `world.templ` to use `layout.Base()` with some mechanism for its unique body children (iframe, overlay, script tag). This is cleaner but requires rethinking the world page structure.
     - (b) Add `#page-content` wrapper and conditional `#dev-sse` element directly to `world.templ` as well. This duplicates the dev injection point but is a smaller change. You'd need to also handle the world page differently in `extractPageContent` since its structure differs (iframe + overlay + script tag).

2. **`extractPageContent` HTML parsing is fragile and incorrect for multiple page structures**
   - Problem: The function finds `id="page-content"`, then looks for the last `</div>` before `</body>`. This assumes `#page-content` is the last `<div>` before `</body>`. But:
     - The lobby page has nested divs inside `#page-content` — the parser would incorrectly grab up to the last nested `</div>`, not the closing `</div>` of `#page-content`.
     - Actually, re-reading: it finds the last `</div>` before `</body>`, which would be the closing tag of `#page-content` if `#dev-sse` is outside it. But if any page adds a `<div>` after `#page-content` (e.g., a modal, toast, or the dev-sse element itself if moved), parsing breaks.
     - The world page (if fixed per issue #1) would have `<script src="/static/game-loader.js"></script>` after the content div, making the last `</div>` NOT the `#page-content` closer.
   - Risk: Silently returns wrong content or empty string, causing morph to fail without obvious error.
   - Suggestion: Use a proper HTML parser (`golang.org/x/net/html`) or at minimum a more robust extraction: find the opening tag, then count nested `<div>`/`</div>` pairs to locate the correct closing tag. Alternatively, use a comment marker like `<!-- /page-content -->` as the end delimiter.

### Concerns (Should Address)

1. **Double rebuild on `.templ` changes**
   - Observation: When a `.templ` file changes, the fswatch script: (1) detects `.templ` change → runs `templ generate` → sends `POST /dev/rebuild`. Then (2) `templ generate` writes `*_templ.go` → fswatch detects that → matches `*_templ.go` case → sends another `POST /dev/rebuild`. If the first build finishes before the second event arrives, this triggers an unnecessary full rebuild. If still building, second gets 409 — but you still get noisy double log output.
   - Suggestion: Add the `_templ.go` exclusion to fswatch itself: `--exclude='_templ\.go$'` before the `--include='\.go$'`. The `.templ` case already handles the generate+rebuild flow, so watching `_templ.go` separately is redundant. Only if someone runs `templ generate` manually (outside the watcher) would you miss it, and that's an acceptable tradeoff.

2. **No authentication on dev endpoints — `DEV_MODE` env var is a weak safeguard**
   - Observation: `/dev/rebuild` can send SIGTERM to the process and execute `go build`. `/dev/reload-static` pushes JavaScript to all connected clients. These are gated only by `DEV_MODE` env var. If `DEV_MODE=true` is accidentally set in a deployment (copy-paste of docker-compose.yml, forgotten env var), these endpoints are wide open with no auth.
   - Suggestion: Add a shared secret check (similar to the existing `hookSecretMiddleware` pattern for `/api/claude-event`). Or bind dev routes to localhost-only. Or add an explicit `if os.Getenv("DEV_MODE") == "true"` check inside each handler as defense-in-depth, not just at registration time.

3. **`datastar.GetSSE` vs `dsutil.GetSSENoCancel` for dev SSE**
   - Observation: The plan uses `datastar.GetSSE("/dev/sse")` which generates `@get('/dev/sse')`. But the existing world page SSE uses `dsutil.GetSSENoCancel()` which adds `{requestCancellation: 'disabled'}`. Without this, Datastar may cancel the dev SSE connection during page interactions (e.g., when a PostSSE fires). This could cause false reconnections and unnecessary morphs.
   - Suggestion: Use `dsutil.GetSSENoCancel("/dev/sse")` for the dev SSE element to match the existing pattern. The dev SSE connection should be persistent and not affected by other SSE actions.

4. **Hardcoded `localhost:8080` in `devMorphPage`**
   - Observation: `devMorphPage` uses `fmt.Sprintf("http://localhost:8080%s", refURL.Path)`. The rest of the codebase uses `HARNESS_URL` env var (defaulting to `http://localhost:8080`). If the port ever changes, this would silently break.
   - Suggestion: Pass the base URL via the `Server` struct or read `HARNESS_URL` env var, consistent with `main.go:109-112`.

5. **Package-level `var devClients` instead of Server field**
   - Observation: `devClients`, `devMu`, and `rebuildMu` are package-level variables in `dev.go`, while the rest of the server state lives on the `Server` struct. This breaks the existing pattern and makes testing harder (if tests are ever added).
   - Suggestion: Move these to fields on the `Server` struct, initialized in `New()` or lazily. This is consistent with how `EventBus` is used.

### Questions (Need Clarification)

1. Should the dev SSE morph work on the world page? If yes, the layout assumption needs fixing. If no (the world page has its own SSE and iframe complications), should this be explicitly documented as a known limitation?

2. The `northstar` reference project's hot-reload (`context/northstar/router/router.go:45-69`) uses `window.location.reload()` — the exact approach this plan explicitly rejects. Has the Datastar-native morph approach been proven to work with the current Datastar version (v1.1.0), or is this novel? The internal-HTTP-request-to-self pattern (fetching from localhost:8080 and extracting innerHTML) is quite unusual.

3. The `#page-content` wrapper is added in prod too (always present, not just in dev). Has the impact on existing CSS been considered? Any styles targeting `body > .lobby` or similar direct-child selectors would break.

4. What happens to existing SSE connections (lobby `/events`, world `/world/:id/events`) during a graceful restart? The plan says "SSE reconnects" but doesn't detail timing. The browser's EventSource has a default 3-second retry. During that window, the user sees no real-time updates. For the world page, this means missed build status updates during the reconnection gap.

### Suggestions (Nice to Have)

1. **Add debouncing to fswatch**: Multiple rapid saves (common with editor auto-save or format-on-save) will trigger multiple curl requests. Add a small debounce (`sleep 0.3 && curl` with dedup) or use `fswatch --latency 0.5` to batch events.

2. **Consider a build notification in the dev SSE**: Before SIGTERM, the server could broadcast a "rebuilding" event through the dev SSE channel so the browser shows a visual indicator (e.g., a small spinner/toast). Currently the user gets no feedback between "save file" and "page morphs".

3. **Log build duration**: In `handleDevRebuild`, measure and log the build time. This helps developers understand the feedback loop latency and identify when the project is getting too large for fast rebuilds.

4. **Consider `--signal SIGTERM` in docker compose**: The default Docker stop signal is already SIGTERM, but being explicit in `docker-compose.yml` with `stop_signal: SIGTERM` and `stop_grace_period: 10s` documents the intent.

### What's Good

- **Zero-downtime rebuild** is genuinely clever — building in the background while the old server keeps serving, then atomic rename + SIGTERM. Build failures are safe.
- **Three-tier change handling** (templ/go = rebuild, css = instant push) is well-thought-out. CSS changes via `ExecuteScript` through open SSE is elegant and truly instant.
- **Host-side watching via FSEvents** is the right call for macOS. Avoids the well-documented Docker inotify/VirtioFS issues.
- **Clean separation of concerns**: host watches → HTTP trigger → container builds. No coupling between the watcher and the build system.
- **Existing infrastructure leveraged well**: graceful shutdown, Datastar SSE APIs, Echo routing patterns.
- **The plan is extremely detailed** — code snippets for every file, design decision rationale, success criteria, testing strategy, performance estimates. This is high-quality planning.

### Recommended Next Steps

1. **Fix the world.templ gap** (Critical #1) — Decide whether to refactor world.templ into layout.Base or add dev SSE injection separately. This is the biggest blocker.
2. **Fix extractPageContent** (Critical #2) — Switch to a proper HTML parser or a more robust delimiter strategy.
3. **Add `_templ.go` exclusion to fswatch** (Concern #1) — Quick fix to prevent double rebuilds.
4. **Use `dsutil.GetSSENoCancel`** (Concern #3) — One-line change to match existing pattern.
5. **Move package-level state to Server struct** (Concern #5) — Aligns with codebase conventions.
6. Then proceed with Phase 1 implementation with these fixes incorporated.
