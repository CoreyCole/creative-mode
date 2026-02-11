---
date: 2026-02-11T02:09:11-08:00
researcher: CoreyCole
git_commit: 1e6cb945c77d3d54bcce402a8dbc3ebe12584994
branch: main
repository: creative-mode
topic: "Docker Dev Hot-Reload Plan — Staff Review + Plan Updates"
tags: [implementation, strategy, docker, hot-reload, datastar, dev-environment, harness, plan-review]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Docker Dev Hot-Reload — Plan Review & Updates

## Task(s)

1. **Resumed from prior handoff** (`thoughts/CoreyCole/handoffs/general/2026-02-11_01-56-00_docker-dev-hot-reload-plan.md`) — plan was complete, ready for implementation.
2. **Staff engineer review of the plan** — completed. Found 2 critical issues, 5 concerns, 4 questions. Review saved as artifact.
3. **Updated the plan to address critical issues** — completed. All critical issues and key concerns have been incorporated into the plan document.
4. **Implementation** — not started. Plan is now ready for Phase 1 implementation.

## Critical References

- **Updated implementation plan**: `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — the authoritative plan with all review fixes incorporated
- **Staff review document**: `thoughts/CoreyCole/reviews/2026-02-11_01-58-53_docker-dev-hot-reload_review.md` — full review with rationale for each issue
- **Harness CLAUDE.md**: `harness/CLAUDE.md` — architecture reference (Datastar SDK patterns, SSE patterns, Echo routing)

## Recent changes

No code changes — only documentation updates:
- `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — updated to address review findings (see details below)
- `thoughts/CoreyCole/reviews/2026-02-11_01-58-53_docker-dev-hot-reload_review.md` — created

## Learnings

1. **`world.templ` bypasses `layout.Base()`**: `harness/views/world/world.templ:11-31` constructs its own `<!DOCTYPE html>`/`<html>`/`<body>` and only uses `layout.Head()`. Only login, pending, admin, and lobby use `layout.Base()`. The plan originally claimed all pages use `layout.Base()` — this was wrong and would have broken hot-reload on the most important page.

2. **`extractPageContent` string parsing is fragile**: The original plan's approach (find last `</div>` before `</body>`) breaks when pages have non-div elements after `#page-content` (e.g., `<script>` tags in world.templ). Replaced with `golang.org/x/net/html` tree walker. Note: `golang.org/x/net` is already an indirect dep at v0.48.0 in `go.mod`.

3. **`datastar.GetSSE` vs `dsutil.GetSSENoCancel`**: The codebase uses `dsutil.GetSSENoCancel()` (`harness/views/dsutil/sse.go:7`) for long-lived SSE connections to prevent Datastar from canceling them during other SSE actions. The dev SSE element must use this pattern too.

4. **No `_test.go` files exist anywhere in the harness** — zero test coverage. The plan doesn't add tests either.

5. **`northstar` reference hot-reload uses `window.location.reload()`** (`context/northstar/router/router.go:45-69`) — the exact approach this plan rejects. The Datastar-native morph approach (internal HTTP request → extract `#page-content` → `PatchElements`) is novel and unproven with this Datastar version.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — updated plan (all review fixes incorporated)
- `thoughts/CoreyCole/reviews/2026-02-11_01-58-53_docker-dev-hot-reload_review.md` — staff engineer review

## Action Items & Next Steps

1. **Implement Phase 1: Dev SSE Infrastructure** (5 changes in the plan):
   - Change 1: Modify `harness/views/layout/layout.templ` — add `#page-content` wrapper + conditional dev SSE with `dsutil.GetSSENoCancel`
   - Change 2: Refactor `harness/views/world/world.templ` — switch from custom `<html>`/`<body>` to `layout.Base()`
   - Change 3: Create `harness/views/layout/dev.go` — `isDevMode()` helper
   - Change 4: Create `harness/internal/server/dev.go` — `devState` struct, `handleDevSSE`, `handleDevRebuild`, `handleDevReloadStatic`, `extractPageContent` (with `golang.org/x/net/html`)
   - Change 5: Modify `harness/internal/server/server.go` — add `dev *devState` field to `Server` struct, register dev routes gated behind `DEV_MODE`
   - Verify: `cd harness && just generate && go build ./... && just lint`

2. **Implement Phase 2: Docker Container** — Dockerfile, docker-compose.yml, .dockerignore, dev-entrypoint.sh

3. **Implement Phase 3: Host-Side File Watcher** — justfile recipes with fswatch (note: `_templ.go` excluded, `--latency=0.3` for debounce)

4. **End-to-end testing** — follow testing strategy in plan document

## Other Notes

- The plan's Phase 1 changes are now numbered 1-5 (was 1-4 before the world.templ refactor was added).
- The `devState` struct pattern (instead of package-level vars) was chosen to match how `EventBus` is used on the `Server` struct. The `dev` field is `nil` when `DEV_MODE` is not set.
- The review raised a question about CSS impact from the `#page-content` wrapper on the world page. The iframe uses `position: fixed` via `.game-iframe` class, which is independent of DOM nesting, so the wrapper should be transparent. Verify visually during Phase 1.
- `golang.org/x/net/html` needs to be imported directly (currently only indirect). Run `go get golang.org/x/net/html` or let `go mod tidy` handle it.
- The review's remaining non-critical concerns (auth on dev endpoints, build notification in dev SSE) were noted but not addressed in the plan — they can be added as follow-up improvements.
