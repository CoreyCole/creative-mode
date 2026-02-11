---
date: 2026-02-11T00:53:26-08:00
researcher: CoreyCole
git_commit: 88442d7fc4961cb44692d53ffc122f7e005d94ea
branch: main
repository: creative-mode
topic: "Local Development Setup: Docker, Hot-Reload, and Fast UI Iteration Strategy"
tags: [research, codebase, docker, hot-reload, templ, air, development-workflow, harness]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
---

# Research: Local Development Setup — Docker, Hot-Reload, and Fast UI Iteration

**Date**: 2026-02-11T00:53:26-08:00
**Researcher**: CoreyCole
**Git Commit**: 88442d7fc4961cb44692d53ffc122f7e005d94ea
**Branch**: main
**Repository**: creative-mode

## Research Question

What is our current local development setup? Check Docker, docker-compose, and ensure we have a strategy for fast-reloading the harness UI when making changes, so we can quickly iterate, see changes in the browser, and debug.

## Summary

**Current state**: The harness has **no Docker configuration and no hot-reload**. Development is entirely manual: edit files, run `just generate`, run `just dev`, manually refresh browser. This creates a slow feedback loop that needs fixing.

**Docker plan**: A full Docker environment (Ubuntu 24.04 + Rust + Go + Claude Code CLI) is planned in Component 7 (`thoughts/CoreyCole/plans/component-7-integration-docker.md`) but is for end-to-end integration, not UI iteration.

**Recommended strategy**: Use templ's built-in proxy + air for a multi-process hot-reload setup that gives sub-second feedback for template-only changes and 1-3 second feedback for Go code changes, with automatic browser reload.

---

## Current State Analysis

### What Exists

| Component | Status | Location |
|-----------|--------|----------|
| Harness Dockerfile | **Does not exist** | — |
| Harness docker-compose | **Does not exist** | — |
| Hot-reload / file watcher | **Does not exist** | — |
| Air config (.air.toml) | **Does not exist** in harness | Only in `context/datastarui/` |
| templ proxy | **Not configured** | — |
| Development command | `just dev` → `go run .` | `harness/justfile:4-5` |
| Code generation | `just generate` → `sqlc generate` + `templ generate` | `harness/justfile:16-18` |

### Current Development Cycle (Manual)

1. Edit `.templ` files, SQL queries, or Go source
2. Run `cd harness && just generate` (regenerate Go from templ + sqlc)
3. Stop the running server (Ctrl+C)
4. Run `just dev` (starts `go run .` on `:8080`)
5. Manually refresh the browser
6. Run `just lint` for linting

**Pain points**: Every change requires 3-4 manual steps. Template-only text changes still require a full server restart.

### Reference Implementation (context/datastarui)

The datastarui reference project has a working Docker + air setup:
- `context/datastarui/docker-compose.yml` — Docker Compose with bind mount + air
- `context/datastarui/.air.toml` — Air watches `.go`, `.templ`, `.html`, `.css`, `.js` files
- `context/datastarui/justfile:31-32` — `watch` recipe runs `air`
- Uses polling (required for Docker bind mounts on macOS)
- Single-process: air calls `just build` which runs `templ generate → go build → tailwind build`

---

## Recommended Hot-Reload Strategy

### Multi-Process Approach with templ Proxy + Air

This is the recommended approach from the templ documentation and community. Three parallel processes, each handling one concern:

```
Process 1: templ generate --watch --proxy   → watches .templ files, runs proxy with auto-reload JS
Process 2: air                               → watches .go files, rebuilds & restarts server
Process 3: air (asset watcher)               → watches CSS/JS, triggers browser reload
```

### How It Works

1. **templ proxy** runs at `localhost:7331`, reverse-proxying to the Go server at `localhost:8080`
2. The proxy injects a small JS snippet before `</body>` that opens an SSE connection back to the proxy
3. When templ regenerates `_templ.go` files, air detects the change, rebuilds, and restarts the server
4. The proxy detects the server is back and sends a reload event via SSE
5. The injected JS triggers `window.location.reload()`

**Key optimization**: With `TEMPL_DEV_MODE=true`, templ also writes `_templ.txt` files. The generated Go code reads these at runtime, so **text-only template changes skip the Go rebuild entirely** — the server serves updated content without restarting.

### Implementation: Harness Justfile Additions

```just
# Hot-reload: templ watcher + proxy (Process 1)
live-templ:
    TEMPL_DEV_MODE=true templ generate --watch --proxy="http://localhost:8080" --open-browser=false -v

# Hot-reload: Go server with air (Process 2)
live-server:
    air

# Hot-reload: static asset watcher (Process 3)
live-assets:
    go run github.com/air-verse/air@v1.64.5 \
      --build.cmd "templ generate --notify-proxy" \
      --build.bin "true" \
      --build.delay "100" \
      --build.include_dir "static" \
      --build.include_ext "js,css"

# Run all three in parallel (development mode)
live:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    just -f {{justfile()}} live-templ &
    just -f {{justfile()}} live-server &
    just -f {{justfile()}} live-assets &
    wait
```

### Implementation: Air Config (.air.toml)

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o tmp/harness ."
  bin = "tmp/harness"
  delay = 200
  exclude_dir = ["tmp", "static", "thoughts"]
  exclude_regex = [".*_templ\\.go", "_test\\.go"]
  include_ext = ["go"]
  include_file = []
  stop_on_error = false
  send_interrupt = true
  kill_delay = 500

[log]
  time = false
  main_only = false

[color]
  main = "magenta"
  watcher = "cyan"
  build = "yellow"
  runner = "green"

[misc]
  clean_on_exit = true

[screen]
  clear_on_rebuild = false
  keep_scroll = true
```

**Critical**: `exclude_regex = [".*_templ\\.go"]` prevents air from detecting templ-generated files and entering an infinite rebuild loop. templ's `--watch` handles `.templ` → `_templ.go` regeneration; air only watches hand-written `.go` files.

### Feedback Speed by Change Type

| Change Type | What Happens | Time to Browser Update |
|-------------|-------------|----------------------|
| Text-only in `.templ` (HTML/CSS classes) | templ writes `_templ.txt`, server reads at next request, proxy reloads browser | ~200ms (no rebuild) |
| Go logic in `.templ` (conditionals, loops) | templ regenerates `_templ.go` → air rebuilds → proxy reloads | 1-3 seconds |
| Go source (handlers, middleware) | air detects change → rebuilds → proxy reloads | 1-3 seconds |
| Static CSS/JS | Asset watcher detects → `--notify-proxy` triggers reload | ~200ms |
| SQL queries | Must manually run `just sqlc` first, then air picks up generated `.go` | Manual + 1-3 seconds |

### Browser Access

- **Development URL**: `http://localhost:7331` (templ proxy, with auto-reload)
- **Direct server URL**: `http://localhost:8080` (no auto-reload, useful for SSE debugging)

### Compatibility with Datastar SSE

The templ proxy injects its reload JS via a script tag before `</body>`. Datastar's SSE connections use separate endpoints (`/events`, `/world/:id/events`) and will not conflict. The Northstar reference project (`context/northstar/`) uses templ's hot-reload proxy successfully with Datastar SSE.

---

## Docker Strategy

### For UI Iteration (Now): No Docker Needed

The harness UI iteration workflow should run **natively on macOS** without Docker. The harness only needs Go + templ + sqlc + air — all lightweight native tools. Docker adds latency (bind mount file events, container overhead) that slows the feedback loop.

### For End-to-End Integration (Component 7): Docker

The full Docker environment from `component-7-integration-docker.md` is needed for:
- Rust/WASM compilation (Bevy game template)
- Claude Code CLI inside tmux sessions
- Game server process management
- Production-like environment validation

This is a separate concern from UI iteration. The two workflows coexist:
- `just live` — native macOS, fast UI iteration (templ proxy + air)
- `docker compose up` — full integration environment (when Component 7 is implemented)

### Docker Compose (Planned, from Component 7)

```yaml
services:
  creative-mode:
    build: .
    ports:
      - "8080:8080"
      - "9001-9020:9001-9020"
    volumes:
      - .:/app
      - cargo-registry:/root/.cargo/registry
      - cargo-git:/root/.cargo/git
      - template-target:/app/template/target
    environment:
      - GITHUB_CLIENT_ID
      - GITHUB_CLIENT_SECRET
      - ANTHROPIC_API_KEY
      - HARNESS_URL=http://localhost:8080
    stdin_open: true
    tty: true
```

This does NOT need to exist yet for harness UI development.

---

## Prerequisites for the Hot-Reload Setup

### Tools to Install

| Tool | Install Command | Purpose |
|------|----------------|---------|
| Go 1.24+ | Already installed | Server runtime |
| templ | `go install github.com/a-h/templ/cmd/templ@latest` | Template compiler + proxy |
| air | `go install github.com/air-verse/air@latest` | Go file watcher + rebuilder |
| sqlc | Already installed | SQL code generator (manual trigger) |

### Environment Variables

```bash
export TEMPL_DEV_MODE=true  # Enable text-only fast path (skip rebuild for HTML changes)
```

---

## Code References

- `harness/justfile` — Current build recipes (no hot-reload)
- `harness/main.go:140-142` — Server starts on `:8080`
- `context/datastarui/.air.toml` — Reference air config (single-process approach)
- `context/datastarui/docker-compose.yml` — Reference Docker + air setup
- `context/datastarui/justfile:31-32` — Reference `watch` recipe
- `thoughts/CoreyCole/plans/component-7-integration-docker.md` — Full Docker integration plan

## Architecture Insights

- The harness has **no Node.js toolchain** — no Tailwind, no PostCSS, no bundler. Just hand-written CSS at `static/styles.css` (394 lines) and two static JS files (Datastar + game-loader.js). This dramatically simplifies the hot-reload story.
- templ's `--proxy` with `TEMPL_DEV_MODE=true` is specifically designed for this stack (Go + templ) and provides the fastest possible feedback loop.
- The multi-process approach (templ watch + air + asset watcher) is strictly better than the single-process approach used in datastarui, because it separates concerns and enables the text-only fast path.

## Open Questions

1. Should we add the `.air.toml` and `just live` recipes now, or wait until more UI work is planned?
2. Should `TEMPL_DEV_MODE=true` be set in a `.env` file or just documented as a dev requirement?
3. For sqlc changes, should we add a file watcher for `.sql` files that auto-runs `sqlc generate`, or keep it manual?
