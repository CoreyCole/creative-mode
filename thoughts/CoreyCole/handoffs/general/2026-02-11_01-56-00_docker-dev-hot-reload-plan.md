---
date: 2026-02-11T01:56:00-08:00
researcher: CoreyCole
git_commit: 88442d7fc4961cb44692d53ffc122f7e005d94ea
branch: main
repository: creative-mode
topic: "Native + Docker Dev Environment with Datastar-Native Hot-Reload"
tags: [implementation, strategy, docker, hot-reload, datastar, dev-environment, harness]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Docker Dev Hot-Reload Implementation Plan

## Task(s)

**Status: Plan complete, ready for implementation.**

Created a detailed implementation plan for a hybrid dev environment with Datastar-native hot-reload. The plan went through several iterations based on user feedback, evolving from a simple Docker + templ proxy setup to a sophisticated architecture with:

1. **Host-side file watching** (macOS FSEvents via `fswatch`, no polling)
2. **Docker container** running the Go server on Debian (Ubuntu deploy parity)
3. **Zero-downtime rebuilds** — server builds new binary in background while old server keeps serving
4. **Datastar-native DOM updates** — no `window.location.reload()`, uses `PatchElements` morph via SSE
5. **Three-tier change handling** — `.go`/`.templ` trigger rebuild, `.css` triggers instant cache bust via open SSE (no restart)

The plan document is at Phase 0 (planning complete, no code written yet). Implementation has 3 phases:
- Phase 1: Dev SSE + Rebuild Infrastructure (server code changes)
- Phase 2: Docker Container (Dockerfile, entrypoint, compose)
- Phase 3: Host-Side File Watcher (justfile recipes with fswatch)

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — the complete plan with all code snippets, design decisions, and success criteria
- **Research document**: `thoughts/CoreyCole/research/2026-02-11-local-dev-setup-hot-reload.md` — background research on hot-reload strategies, templ proxy, air, Docker file watching limitations
- **Harness CLAUDE.md**: `harness/CLAUDE.md` — architecture reference for the harness server

## Recent changes

No code changes were made. This session was entirely planning/design work. The only file changes are:
- `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — created and iterated 4 times

## Learnings

1. **macOS Docker inotify is unreliable**: VirtioFS partially propagates CREATE/MODIFY but DELETE is broken (open bug since 2024). Event propagation intermittently stops. `poll = true` is the only safe option IF you must watch inside the container. Our solution: don't watch inside the container at all.

2. **templ proxy does `window.location.reload()`**: The user explicitly rejected this approach. For a Datastar app, hot-reload must work through SSE patching, not page reloads. This led to the dev SSE endpoint design.

3. **Existing graceful shutdown is key**: `harness/main.go:130-138` already handles SIGTERM via `signal.NotifyContext` → `e.Shutdown()` → `main()` returns exit 0. The `/dev/rebuild` endpoint leverages this by sending SIGTERM to self after building the new binary.

4. **CSS changes don't need rebuilds**: Static CSS is served from `harness/static/` (`server.go:100`). The browser just needs to re-fetch the stylesheet. The `devClients` broadcast channel pushes `ExecuteScript` through open SSE connections to bust the CSS cache — no server restart needed.

5. **Layout is the single injection point**: All pages use `layout.Base()` (`views/layout/layout.templ:14-22`). Adding `#page-content` wrapper and conditional `#dev-sse` element there covers every page.

6. **Internal HTTP request for page morph**: The dev SSE handler makes an internal request to `localhost:8080` (forwarding cookies) to re-render the current page, then extracts `#page-content` innerHTML and sends it as a Datastar morph. This reuses all existing handler logic without duplication.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — Complete implementation plan (718 lines) with architecture diagrams, code snippets for all files, design decisions, success criteria, and testing strategy
- `thoughts/CoreyCole/research/2026-02-11-local-dev-setup-hot-reload.md` — Prior research document (pre-existing, not modified this session)

## Action Items & Next Steps

1. **Implement Phase 1**: Dev SSE Infrastructure
   - Create `harness/views/layout/dev.go` (isDevMode helper)
   - Modify `harness/views/layout/layout.templ` (add `#page-content` wrapper + conditional dev SSE)
   - Create `harness/internal/server/dev.go` (handleDevSSE, handleDevRebuild, handleDevReloadStatic, extractPageContent, devClients broadcast)
   - Modify `harness/internal/server/server.go` (register `/dev/sse`, `/dev/rebuild`, `/dev/reload-static` routes gated behind `DEV_MODE`)
   - Verify: `just generate && go build ./... && just lint`

2. **Implement Phase 2**: Docker Container
   - Create `harness/Dockerfile` (golang:1.24-bookworm + gcc)
   - Create `harness/scripts/dev-entrypoint.sh` (build-run loop with SIGTERM trap)
   - Create `harness/docker-compose.yml` (bind mount `..:/app:cached`, named volumes for Go caches)
   - Create `harness/.dockerignore`
   - Verify: `docker build` succeeds, `docker compose up` starts server

3. **Implement Phase 3**: Host-Side File Watcher
   - Modify `harness/justfile` (append `live`, `up`, `down`, `watch`, `shell`, `logs` recipes)
   - Prerequisite: `brew install fswatch` on host
   - Verify: `just live` starts both container + watcher, file changes trigger correct actions

4. **End-to-end testing**: Follow the testing strategy in the plan document (smoke tests, hot-reload tests, zero-downtime tests, edge cases)

## Other Notes

- The plan specifies `fswatch` for host-side file watching. It needs to be installed via `brew install fswatch`. The user also needs `templ` installed on the host for template generation.
- The `devClients` pattern in `dev.go` uses a map of channels with a mutex — similar to the existing `EventBus` pattern in `harness/internal/events/bus.go`. Consider whether to reuse EventBus or keep the lightweight dev-only implementation.
- The `extractPageContent` function uses simple string parsing (not a full HTML parser). It relies on `#page-content` being the last child div before `</body>` in the layout. If the layout structure changes, this parser may need updating.
- The `rebuildMu.TryLock()` pattern prevents concurrent builds but doesn't queue them. If a change arrives during a build, it returns 409. The `fswatch` watcher sends `curl &` (non-blocking), so the next change after the build completes will trigger a new build. This means there's a potential gap where a change during a build is missed. For a dev tool this is acceptable — the developer will just save again.
- Reference Docker implementations exist in `context/datastarui/` (Docker + air) and `context/northstar/` (templ proxy + Datastar).
