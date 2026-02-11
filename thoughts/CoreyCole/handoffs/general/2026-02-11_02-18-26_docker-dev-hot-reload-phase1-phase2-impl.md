---
date: 2026-02-11T02:18:26-08:00
researcher: CoreyCole
git_commit: 1c7974d1a74212a448a0fbd05280028ec8889577
branch: main
repository: creative-mode
topic: "Docker Dev Hot-Reload — Phase 1 & Phase 2 Implementation"
tags: [implementation, docker, hot-reload, datastar, dev-environment, harness]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Docker Dev Hot-Reload — Phase 1 & Phase 2 Implementation

## Task(s)

1. **Phase 1: Dev SSE Infrastructure** — **completed**. All 5 changes implemented, builds and lints clean.
2. **Phase 2: Docker Container** — **completed**. Dockerfile, entrypoint, docker-compose.yml, .dockerignore all created. `docker compose config` validates. Docker image build not tested (OrbStack/Docker daemon was not running).
3. **Phase 3: Host-Side File Watcher** — **not started**. This is the next phase.

Working from the implementation plan: `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md`

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — the authoritative plan with all review fixes incorporated. Phase 3 details are in the "Phase 3: Host-Side File Watcher" section.
- **Staff review**: `thoughts/CoreyCole/reviews/2026-02-11_01-58-53_docker-dev-hot-reload_review.md` — full review context
- **Harness CLAUDE.md**: `harness/CLAUDE.md` — architecture reference (Datastar SDK patterns, SSE patterns, Echo routing)

## Recent changes

### Phase 1 (Dev SSE Infrastructure)

- `harness/views/layout/layout.templ:1-24` — added `import "creative-mode/harness/views/dsutil"`, wrapped `{ children... }` in `<div id="page-content">`, added conditional `<div id="dev-sse">` with `dsutil.GetSSENoCancel("/dev/sse")` when `isDevMode()` is true. The `#dev-sse` element is placed **outside** `#page-content` so morph never touches it.
- `harness/views/world/world.templ:10-21` — refactored from custom `<!DOCTYPE html>`/`<html>`/`<body>` to `@layout.Base(w.Name) { ... }`. The iframe, overlay, and script tag are now children of `layout.Base()`.
- `harness/views/layout/dev.go` — **new file**. `isDevMode()` helper checking `os.Getenv("DEV_MODE") == "true"`.
- `harness/internal/server/dev.go` — **new file**. Contains `devState` struct (client tracking + build mutex), `handleDevSSE` (SSE morph on reconnect + CSS reload listener), `handleDevRebuild` (zero-downtime background build + SIGTERM), `handleDevReloadStatic` (CSS cache bust broadcast), `extractPageContent` + `findNodeByID` (robust HTML parser via `golang.org/x/net/html`).
- `harness/internal/server/server.go:34-43` — added `dev *devState` field to `Server` struct.
- `harness/internal/server/server.go:104-110` — registered `/dev/sse`, `/dev/rebuild`, `/dev/reload-static` routes gated behind `DEV_MODE=true`.
- `harness/go.mod` / `harness/go.sum` — upgraded `golang.org/x/net` from indirect v0.48.0 to direct v0.50.0. Also upgraded `golang.org/x/crypto` v0.48.0, `golang.org/x/sys` v0.41.0, `golang.org/x/text` v0.34.0.

### Phase 2 (Docker Container)

- `harness/Dockerfile` — **new file**. `golang:1.24-bookworm`, gcc + libc for CGo, copies entrypoint, `WORKDIR /app/harness`, exposes 8080.
- `harness/scripts/dev-entrypoint.sh` — **new file**. Build-run loop with SIGTERM/SIGINT trap, initial `go build -o /tmp/harness .`, restart loop.
- `harness/docker-compose.yml` — **new file**. Harness service with port 8080, `..:/app:cached` bind mount, Go mod/build cache volumes, `CGO_ENABLED=1` + `DEV_MODE=true`.
- `harness/.dockerignore` — **new file**. Excludes `.git`, `data/`, `*.db`, `*_templ.go`, `tmp/`.

## Learnings

1. **Lint fixes needed for dev.go**: The initial implementation hit 5 lint issues — `errcheck` on `resp.Body.Close()` (fix: wrap in `func() { _ = resp.Body.Close() }()`), `gocritic` httpNoBody (fix: use `http.NoBody` instead of `nil`), `mnd` magic numbers (fix: extract to named constants), `noctx` (fix: use `exec.CommandContext(context.Background(), ...)` instead of `exec.Command(...)`).

2. **`WithModeInner()` confirmed in datastar-go v1.1.0**: The SDK at `/Users/coreycole/go/pkg/mod/github.com/starfederation/datastar-go@v1.1.0/datastar/elements-sugar.go:80` has `WithModeInner()`, `WithSelectorID()`, `PatchElements()`, and `ExecuteScript()` — all APIs the plan depends on.

3. **`golang.org/x/net` version bump**: `go get golang.org/x/net/html` upgraded from v0.48.0 to v0.50.0, and pulled along crypto/sys/text upgrades. This is fine — they're all standard library extensions.

4. **OrbStack as Docker runtime**: The machine uses OrbStack (not Docker Desktop). Docker socket is at `unix:///Users/coreycole/.orbstack/run/docker.sock`. OrbStack was not running during this session, so `docker build` was not tested.

## Artifacts

- `harness/views/layout/layout.templ` — modified
- `harness/views/world/world.templ` — modified
- `harness/views/layout/dev.go` — new
- `harness/internal/server/dev.go` — new
- `harness/internal/server/server.go` — modified (lines 34-43, 104-110)
- `harness/Dockerfile` — new
- `harness/scripts/dev-entrypoint.sh` — new
- `harness/docker-compose.yml` — new
- `harness/.dockerignore` — new
- `harness/go.mod` / `harness/go.sum` — modified (dependency upgrades)

## Action Items & Next Steps

1. **Commit Phase 1 + Phase 2 changes** — all files listed in Artifacts above are uncommitted.

2. **Start OrbStack and test Docker build** — `docker build -t harness-dev -f harness/Dockerfile harness/` should succeed. Then `cd harness && docker compose up` to verify the container starts and serves on `:8080`.

3. **Implement Phase 3: Host-Side File Watcher** — add justfile recipes per the plan:
   - `live` — starts both Docker container + host file watcher
   - `up` / `down` — Docker compose up/down
   - `watch` — fswatch with `--latency=0.3`, `--exclude='_templ\.go$'`, routes `.templ` → generate + rebuild, `.go` → rebuild, `.css` → reload-static
   - `shell` / `logs` — convenience recipes
   - Prerequisites: `brew install fswatch`, `templ` CLI already installed

4. **End-to-end testing** — follow the testing strategy in the plan document (smoke tests, hot-reload tests, zero-downtime tests, edge cases).

5. **Visual verification of world page** — confirm the `#page-content` wrapper doesn't affect the fullscreen iframe CSS (`.game-iframe` uses `position: fixed`, should be independent of DOM nesting, but verify visually).

## Other Notes

- All Phase 1 changes pass `just generate && go build ./... && just lint` with 0 issues.
- The `#page-content` wrapper is always present (dev and prod). Only the `#dev-sse` element is conditional on `DEV_MODE=true`.
- The `dev` field on `Server` is `nil` when `DEV_MODE` is not set — no dev state allocated in production.
- The plan's Phase 3 fswatch config excludes `_templ.go` files to prevent double rebuilds (`.templ` handler already runs `templ generate` before triggering rebuild).
- The review's non-critical concerns (auth on dev endpoints, build notification in dev SSE) were deferred — can be added as follow-up improvements.
