# Native + Docker Dev Environment with Datastar-Native Hot-Reload

## Overview

Add a hybrid development environment: **host (macOS) watches files** with native FSEvents and triggers actions, **Docker (Debian) runs the Go server** for Ubuntu deploy parity. Hot-reload is Datastar-native — no `window.location.reload()`. The server rebuilds itself in the background with zero downtime, and the browser DOM is morphed in place via SSE.

## Current State Analysis

### What Exists

- **No hot-reload** — development cycle is manual: edit, `just generate`, `just dev`, refresh browser
- **Server hard-coded to `:8080`** (`main.go:142`), base URL defaults to `http://localhost:8080`
- **Working directory must be `harness/`** — `main.go:25-34` resolves `../data` and `../template` via relative paths
- **CGo required** — `mattn/go-sqlite3` needs gcc (rules out Alpine)
- **No Node.js toolchain** — just hand-written CSS and static JS (no bundler/Tailwind)
- **Most pages use `layout.Base()`** — login, pending, admin, lobby use the shared layout (`views/layout/layout.templ:14-22`). **Exception**: `views/world/world.templ` constructs its own `<html>`/`<body>` and only uses `layout.Head()` for the `<head>` block — it needs a custom body structure for the fullscreen game iframe + overlay
- **SSE connections exist** on lobby (`/events` in `lobby.templ:54`) and world pages (`/world/:id/events`)
- **Datastar morph** is used for all UI updates — `PatchElementTempl`, `PatchElements`, `MarshalAndPatchSignals`
- **Graceful shutdown** already implemented — `main.go:130-138` handles SIGTERM via `signal.NotifyContext`, calls `e.Shutdown()`, `main()` returns with exit code 0

### Key Constraints

| Constraint | Source | Impact |
|-----------|--------|--------|
| CGo required for SQLite | `go.mod:8` — `mattn/go-sqlite3` | Debian-based Docker image |
| Relative path resolution | `main.go:25-34` — `filepath.Join("..", "data")` | WORKDIR must be `/app/harness`, mount at `/app` |
| Port 8080 hard-coded | `main.go:142` | Single port, map via Docker |
| `*_templ.go` gitignored | `.gitignore:8` | Host generates, propagates via bind mount |
| Most pages use `layout.Base()` | `views/layout/layout.templ:14-22` | Single place for most pages; `world.templ` needs refactoring |
| World page bypasses `layout.Base()` | `views/world/world.templ:11-31` | Must refactor to use `layout.Base()` for dev SSE injection |
| macOS Docker lacks inotify | VirtioFS limitation | Host watches files, not container |

## Desired End State

After this plan is implemented:

1. `cd harness && just live` starts both host watcher and Docker container
2. Developer browses `http://localhost:8080`
3. Editing a `.templ` file → host regenerates `_templ.go` → host sends `POST /dev/rebuild` → server builds new binary in background (zero downtime) → graceful restart → SSE reconnects → Datastar morphs page (~2s)
4. Editing a `.go` file → host sends `POST /dev/rebuild` → same zero-downtime flow (~2s)
5. Editing `.css` → host sends `POST /dev/reload-static` → server pushes CSS cache bust through open SSE connection → browser re-fetches stylesheet (~100ms, no restart)
6. **No page reload at any point** — DOM morphed via `PatchElements`, CSS bust via `ExecuteScript`
7. Existing `just dev`, `just generate`, `just build`, `just lint` recipes unchanged

### Architecture

```
 Host (macOS)                          Docker Container (Debian)
 ============                          =========================

 fswatch (native FSEvents)             Entrypoint: build-run loop
   │                                     ┌─────────────────────────┐
   ├── *.templ changed                   │ go build -o /tmp/harness│
   │   └── templ generate               │ /tmp/harness            │◄── serves on :8080
   │       └── _templ.go written         │   │                     │
   │                                     │   ├── /dev/sse (SSE)    │
   ├── *.go changed (incl _templ.go)     │   ├── /dev/rebuild      │
   │   └── POST /dev/rebuild ──────────► │   │   └── go build      │ ← builds in background
   │                                     │   │       └── SIGTERM    │ ← graceful restart
   │                                     │   │           └── loop   │ ← entrypoint restarts
   └── *.css changed                     │   │                     │
       └── POST /dev/reload-static ────► │   └── ExecuteScript     │ ← CSS cache bust
                                         │       (via open SSE)     │
                                         └─────────────────────────┘
 Browser ──── localhost:8080 ──────────► Go server
   └── SSE /dev/sse (auto-reconnects)      └── PatchElements morph (no page reload)
```

### Three-Tier Change Handling

| File type | Host action | Server action | Downtime | Latency |
|-----------|------------|---------------|----------|---------|
| `.templ` | `templ generate` → `POST /dev/rebuild` | Build binary in background → graceful restart → SSE morph | ~100ms (restart only) | ~2s total |
| `.go` | `POST /dev/rebuild` | Build binary in background → graceful restart → SSE morph | ~100ms (restart only) | ~2s total |
| `.css` | `POST /dev/reload-static` | Push CSS cache bust through open SSE connection | **Zero** | ~100ms |
| `.js` | (manual reload) | Static JS runs once on load; rarely changes | N/A | N/A |

## What We're NOT Doing

- **Not using templ proxy** — no `:7331`, no `window.location.reload()`
- **Not polling inside the container** — host watches files with native FSEvents
- **Not using air inside the container** — server rebuilds itself via `/dev/rebuild`
- **Not adding Rust/WASM/Trunk** — that's Component 7
- **Not adding Claude Code CLI** — also Component 7

## Implementation Approach

**Host-side**: `fswatch` (or `templ --watch` + justfile) monitors files using macOS-native FSEvents. On change, it runs `templ generate` for `.templ` files, then sends HTTP requests to the server's dev endpoints.

**Container-side**: A build-run entrypoint loop. The server exposes two dev endpoints:
- `POST /dev/rebuild` — builds new binary in background while old server keeps serving, then graceful restart
- `POST /dev/reload-static` — pushes CSS/JS cache bust through open SSE connections (no restart)

**Browser-side**: A persistent SSE connection to `/dev/sse`. On reconnect after server restart, the handler fetches the current page via internal HTTP request and sends it as a Datastar `PatchElements` morph. For CSS changes, the server pushes an `ExecuteScript` through the already-open SSE connection.

---

## Phase 1: Dev SSE + Rebuild Infrastructure

### Overview

Add the server-side hot-reload mechanism: dev SSE endpoint for Datastar morph on reconnect, rebuild endpoint for zero-downtime restarts, and reload-static endpoint for CSS cache busting.

### Changes Required

#### 1. Layout Template Modification
**File**: `harness/views/layout/layout.templ`
**Action**: Modify

Wrap `{ children... }` in `#page-content` div. Add conditional dev SSE connection.

Current:
```go
templ Base(title string) {
	<!DOCTYPE html>
	<html lang="en">
		@Head(title)
		<body>
			{ children... }
		</body>
	</html>
}
```

New:
```go
import "creative-mode/harness/views/dsutil"

templ Base(title string) {
	<!DOCTYPE html>
	<html lang="en">
		@Head(title)
		<body>
			if isDevMode() {
				<div id="dev-sse" data-on-load={ dsutil.GetSSENoCancel("/dev/sse") }></div>
			}
			<div id="page-content">
				{ children... }
			</div>
		</body>
	</html>
}
```

**Notes:**
- `#dev-sse` is deliberately placed **outside** `#page-content` so the morph handler never touches it.
- Uses `dsutil.GetSSENoCancel` (not `datastar.GetSSE`) to prevent Datastar from canceling the dev SSE connection when other SSE actions fire (e.g., PostSSE for form submissions). This matches the pattern used by the world page's SSE connection.

#### 2. World Page Refactor to Use `layout.Base()`
**File**: `harness/views/world/world.templ`
**Action**: Modify

The world page currently bypasses `layout.Base()` and constructs its own `<html>`/`<body>`. It must be refactored to use `layout.Base()` so the `#page-content` wrapper and dev SSE element are injected consistently across all pages.

Current:
```go
templ Page(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User, signals OverlaySignals, serverPort int, checkpoints []sqlc.Checkpoint) {
	<!DOCTYPE html>
	<html lang="en">
		@layout.Head(w.Name)
		<body>
			if serverPort > 0 {
				<iframe id="game-frame"
					src={ fmt.Sprintf("/wasm/%s/%s/index.html?server_port=%d", w.ID, cp.ID, serverPort) }
					class="game-iframe">
				</iframe>
			} else {
				<iframe id="game-frame" class="game-iframe"></iframe>
			}
			<div id="harness-overlay"
				data-signals={ templ.JSONString(signals) }
				data-on-load={ dsutil.GetSSENoCancel(fmt.Sprintf("/world/%s/events", w.ID)) }>
				@Overlay(w, cp, checkpoints)
			</div>
			<script src="/static/game-loader.js"></script>
		</body>
	</html>
}
```

New:
```go
templ Page(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User, signals OverlaySignals, serverPort int, checkpoints []sqlc.Checkpoint) {
	@layout.Base(w.Name) {
		if serverPort > 0 {
			<iframe id="game-frame"
				src={ fmt.Sprintf("/wasm/%s/%s/index.html?server_port=%d", w.ID, cp.ID, serverPort) }
				class="game-iframe">
			</iframe>
		} else {
			<iframe id="game-frame" class="game-iframe"></iframe>
		}
		<div id="harness-overlay"
			data-signals={ templ.JSONString(signals) }
			data-on-load={ dsutil.GetSSENoCancel(fmt.Sprintf("/world/%s/events", w.ID)) }>
			@Overlay(w, cp, checkpoints)
		</div>
		<script src="/static/game-loader.js"></script>
	}
}
```

**Notes:**
- The iframe, overlay, and script tag become children of `layout.Base()`, which wraps them in `#page-content`.
- The `#page-content` wrapper is a plain div — it won't affect the iframe's fullscreen `position: fixed` CSS since that's independent of DOM nesting.
- The `<script src="/static/game-loader.js">` tag inside `#page-content` means it will re-execute on morph. For a dev tool this is fine — the game loader just sets up iframe communication and re-running it is harmless.
- On dev morph after rebuild, the iframe src will be re-set, causing the WASM game to reload. This is desirable — the developer likely changed server-side code that affects WASM serving.

#### 3. Dev Mode Helper
**File**: `harness/views/layout/dev.go`
**Action**: Create

```go
package layout

import "os"

// isDevMode checks if the DEV_MODE environment variable is set.
func isDevMode() bool {
	return os.Getenv("DEV_MODE") == "true"
}
```

#### 4. Dev Server Endpoints
**File**: `harness/internal/server/dev.go`
**Action**: Create

```go
package server

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"golang.org/x/net/html"
)

// devState holds dev hot-reload state. Initialized lazily when dev
// routes are registered. Lives on the Server struct (not package-level)
// to match the existing pattern for EventBus, etc.
type devState struct {
	mu       sync.Mutex
	clients  map[chan string]struct{}
	buildMu  sync.Mutex
}

func newDevState() *devState {
	return &devState{
		clients: make(map[chan string]struct{}),
	}
}

func (d *devState) addClient(ch chan string) {
	d.mu.Lock()
	d.clients[ch] = struct{}{}
	d.mu.Unlock()
}

func (d *devState) removeClient(ch chan string) {
	d.mu.Lock()
	delete(d.clients, ch)
	d.mu.Unlock()
}

func (d *devState) broadcast(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.clients {
		select {
		case ch <- msg:
		default: // skip slow clients
		}
	}
}

// handleDevSSE serves the development hot-reload SSE endpoint.
// On (re)connection after a server restart, it fetches the current page
// and sends it as a Datastar PatchElements morph.
// It also listens for push events (e.g., CSS reload) on its channel.
func (s *Server) handleDevSSE(c echo.Context) error {
	w := c.Response().Writer
	r := c.Request()
	sse := datastar.NewSSE(w, r)

	// Register this client for push events (CSS reload, etc.)
	eventCh := make(chan string, 8)
	s.dev.addClient(eventCh)
	defer s.dev.removeClient(eventCh)

	// On (re)connect: morph the page content
	s.devMorphPage(sse, r)

	// Listen for push events until disconnect
	for {
		select {
		case msg := <-eventCh:
			if msg == "reload-static" {
				_ = sse.ExecuteScript(
					`document.querySelectorAll('link[rel="stylesheet"]').forEach(` +
						`l=>{const u=new URL(l.href);` +
						`u.searchParams.set("v",Date.now());l.href=u.toString()})`,
				)
			}
		case <-r.Context().Done():
			return nil
		}
	}
}

// devMorphPage fetches the current page via internal HTTP request
// and sends the #page-content innerHTML as a Datastar morph.
func (s *Server) devMorphPage(
	sse *datastar.ServerSentEventGenerator,
	r *http.Request,
) {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return
	}

	refURL, err := url.Parse(referer)
	if err != nil {
		return
	}

	// Use HARNESS_URL env var (same source as main.go) for the internal
	// request, falling back to localhost:8080.
	baseURL := os.Getenv("HARNESS_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	internalURL := baseURL + refURL.Path
	if refURL.RawQuery != "" {
		internalURL += "?" + refURL.RawQuery
	}

	pageReq, err := http.NewRequestWithContext(
		r.Context(), http.MethodGet, internalURL, nil,
	)
	if err != nil {
		return
	}
	pageReq.Header.Set("Cookie", r.Header.Get("Cookie"))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(pageReq)
	if err != nil {
		s.Logger.Warn("dev: failed to fetch page",
			"url", internalURL, "error", err)
		return
	}
	defer resp.Body.Close()

	content, err := extractPageContent(resp.Body)
	if err != nil {
		s.Logger.Warn("dev: failed to extract page content", "error", err)
		return
	}
	if content != "" {
		_ = sse.PatchElements(
			content,
			datastar.WithSelectorID("page-content"),
			datastar.WithModeInner(),
		)
	}
}

// handleDevRebuild builds a new binary in the background while the
// old server keeps serving. Once the build succeeds, it triggers a
// graceful restart via SIGTERM. Build failures leave the old server
// running.
func (s *Server) handleDevRebuild(c echo.Context) error {
	if !s.dev.buildMu.TryLock() {
		return c.JSON(http.StatusConflict,
			map[string]string{"status": "already building"})
	}

	go func() {
		defer s.dev.buildMu.Unlock()

		start := time.Now()
		s.Logger.Info("dev: building new binary...")

		cmd := exec.Command(
			"go", "build", "-o", "/tmp/harness-next", ".",
		)
		cmd.Dir = "/app/harness"
		cmd.Env = os.Environ()

		if output, err := cmd.CombinedOutput(); err != nil {
			s.Logger.Error("dev: build failed",
				"error", err, "output", string(output),
				"duration", time.Since(start))
			return // old server keeps running
		}

		if err := os.Rename(
			"/tmp/harness-next", "/tmp/harness",
		); err != nil {
			s.Logger.Error("dev: rename failed", "error", err)
			return
		}

		s.Logger.Info("dev: build succeeded, restarting",
			"duration", time.Since(start))

		// Trigger existing graceful shutdown in main.go
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)
	}()

	return c.JSON(http.StatusAccepted,
		map[string]string{"status": "building"})
}

// handleDevReloadStatic pushes a CSS/JS cache bust to all connected
// dev SSE clients. No server restart needed.
func (s *Server) handleDevReloadStatic(c echo.Context) error {
	s.dev.broadcast("reload-static")
	return c.JSON(http.StatusOK,
		map[string]string{"status": "reloading"})
}

// extractPageContent uses golang.org/x/net/html to extract the
// innerHTML of the #page-content div. This is robust against varying
// page structures (nested divs, script tags, etc.) unlike string-based
// parsing.
func extractPageContent(body io.Reader) (string, error) {
	doc, err := html.Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	node := findNodeByID(doc, "page-content")
	if node == nil {
		return "", nil
	}

	// Render all children (innerHTML) into a string.
	var sb strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&sb, child); err != nil {
			return "", fmt.Errorf("render child: %w", err)
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

// findNodeByID walks the HTML tree to find the element with the given id.
func findNodeByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findNodeByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
```

**Design decisions:**
- **`devState` struct on Server**: Client tracking and build mutex live on the `Server` struct (via a `dev *devState` field) instead of package-level vars. This matches the existing pattern for `EventBus`, `WorldManager`, etc. and is testable.
- **`golang.org/x/net/html` parser**: Replaces the fragile string-based `extractPageContent`. The old approach found the "last `</div>` before `</body>`" which broke on pages with non-div elements after `#page-content` (e.g., `<script>` tags in the world page). The HTML parser correctly walks the DOM tree and extracts children regardless of nesting structure.
- **`HARNESS_URL` env var**: `devMorphPage` reads `HARNESS_URL` (same source as `main.go:109-112`) instead of hardcoding `localhost:8080`. Keeps the internal request URL consistent with the server's actual address.
- **Build duration logging**: `handleDevRebuild` logs build duration to help developers understand the feedback loop latency.
- **Zero-downtime rebuild**: `handleDevRebuild` builds `/tmp/harness-next` in the background while the old server keeps serving. Only after a successful build does it `Rename` + `SIGTERM`. Build failures are safe — old server keeps running.
- **`buildMu.TryLock()`**: Prevents concurrent builds. Returns 409 Conflict if already building.
- **`devMorphPage` on SSE connect**: On each (re)connection (including after graceful restart), fetches the current page via internal HTTP request (forwarding cookies for auth), extracts `#page-content`, sends as Datastar morph.
- **SIGTERM triggers existing graceful shutdown**: `main.go:130-138` already handles SIGTERM — drains connections, shuts down cleanly, `main()` returns with exit 0. The entrypoint loop then starts the new binary.

#### 5. Server Struct + Route Registration
**File**: `harness/internal/server/server.go`
**Action**: Modify

Add `dev *devState` field to the `Server` struct and register dev routes in `RegisterRoutes`, gated behind `DEV_MODE`:

```go
// Add field to Server struct:
type Server struct {
    // ... existing fields ...
    dev *devState // nil when DEV_MODE is not set
}

// In RegisterRoutes, after static file serving and before health check:
if os.Getenv("DEV_MODE") == "true" {
    s.dev = newDevState()
    e.GET("/dev/sse", s.handleDevSSE)
    e.POST("/dev/rebuild", s.handleDevRebuild)
    e.POST("/dev/reload-static", s.handleDevReloadStatic)
}
```

The `dev` field is `nil` when `DEV_MODE` is not set, so no dev state is allocated in production. The dev handlers are only registered conditionally, so the nil field is never accessed.

### Success Criteria

#### Automated Verification:
- [ ] `cd harness && just generate && go build ./...` succeeds
- [ ] `cd harness && just lint` passes

#### Manual Verification:
- [ ] `DEV_MODE=true go run .` — lobby page source contains `id="dev-sse"` and `id="page-content"`
- [ ] `DEV_MODE=true go run .` — world page source also contains `id="dev-sse"` and `id="page-content"` (verifies world.templ refactor)
- [ ] Without `DEV_MODE` — page source has `id="page-content"` but NOT `id="dev-sse"`
- [ ] World page iframe and overlay still render correctly after refactor (CSS unaffected)
- [ ] `POST /dev/rebuild` returns 202, server rebuilds and restarts
- [ ] `POST /dev/reload-static` returns 200, browser CSS refreshes
- [ ] Concurrent rebuild requests return 409

---

## Phase 2: Docker Container

### Overview

Create the Docker infrastructure for running the server on Debian (deploy parity). No file watching inside the container — the host triggers rebuilds.

### Changes Required

#### 1. Dockerfile
**File**: `harness/Dockerfile`
**Action**: Create

```dockerfile
FROM golang:1.24-bookworm

# gcc + libc for CGo (mattn/go-sqlite3)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy entrypoint
COPY scripts/dev-entrypoint.sh /usr/local/bin/dev-entrypoint.sh
RUN chmod +x /usr/local/bin/dev-entrypoint.sh

WORKDIR /app/harness

EXPOSE 8080

ENTRYPOINT ["dev-entrypoint.sh"]
```

**Design decisions:**
- `golang:1.24-bookworm` — Debian-based for CGo, matches `go.mod`
- No templ/air/sqlc in image — host handles generation, container only needs Go + gcc for building
- Minimal image — just Go toolchain and CGo dependencies

#### 2. Entrypoint Script
**File**: `harness/scripts/dev-entrypoint.sh`
**Action**: Create

```bash
#!/bin/bash

# Clean shutdown: forward SIGTERM to server, then exit
trap 'kill $PID 2>/dev/null; wait $PID; exit 0' SIGTERM SIGINT

echo "=== Creative Mode Harness — Dev Container ==="
echo ""
echo "  Browse:  http://localhost:8080"
echo "  Rebuild: POST /dev/rebuild"
echo "  CSS:     POST /dev/reload-static"
echo ""

# Initial build
echo "Building..."
go build -o /tmp/harness . || exit 1

# Run in a restart loop.
# The /dev/rebuild endpoint builds a new binary, then sends SIGTERM.
# Graceful shutdown exits 0, and this loop restarts with the new binary.
while true; do
    /tmp/harness &
    PID=$!
    wait $PID
    EXIT_CODE=$?
    # If binary was removed or non-zero exit (not graceful), stop
    [ ! -f /tmp/harness ] && exit 1
    echo "Restarting... (exit code: $EXIT_CODE)"
done
```

**Design decisions:**
- **Build-run loop**: The entrypoint builds the binary once, then runs it in a loop. When `/dev/rebuild` succeeds and sends SIGTERM, the server exits cleanly (exit 0 from graceful shutdown), and the loop starts the new binary.
- **Trap SIGTERM/SIGINT**: When `docker compose down` sends SIGTERM to PID 1 (the entrypoint), the trap forwards it to the server process and exits cleanly.
- **No file watching**: The container never watches files. It only rebuilds when told to via `/dev/rebuild`.

#### 3. Docker Compose
**File**: `harness/docker-compose.yml`
**Action**: Create

```yaml
services:
  harness:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    volumes:
      - ..:/app:cached
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/root/.cache/go-build
    environment:
      - CGO_ENABLED=1
      - DEV_MODE=true

volumes:
  go-mod-cache:
  go-build-cache:
```

#### 4. Docker Ignore
**File**: `harness/.dockerignore`
**Action**: Create

```
.git
data/
*.db
*_templ.go
tmp/
```

### Success Criteria

#### Automated Verification:
- [ ] `docker build -t harness-dev -f harness/Dockerfile harness/` succeeds
- [ ] `cd harness && docker compose config` validates

#### Manual Verification:
- [ ] `docker compose up` starts container, server accessible at `:8080`
- [ ] `curl -X POST http://localhost:8080/dev/rebuild` triggers rebuild in container logs
- [ ] `docker compose down` stops cleanly

---

## Phase 3: Host-Side File Watcher

### Overview

Add host-side file watching using native macOS FSEvents. The watcher runs `templ generate` for template changes and sends HTTP requests to the container's dev endpoints.

### Changes Required

#### 1. Justfile Updates
**File**: `harness/justfile`
**Action**: Modify — append dev recipes

```just
# --- Development (Docker + Host Watcher) ---

# Start everything: Docker container + host file watcher
live:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    just -f {{justfile()}} up &
    sleep 5  # wait for container to be ready
    just -f {{justfile()}} watch &
    wait

# Start Docker container
up:
    docker compose up --build

# Stop Docker container
down:
    docker compose down

# Host-side file watcher (native FSEvents, no polling)
watch:
    #!/usr/bin/env bash
    echo "Watching for file changes..."
    fswatch -0 -r --latency=0.3 \
        --include='\.templ$' \
        --include='\.go$' \
        --include='\.css$' \
        --exclude='_templ\.go$' \
        --exclude='.*' \
        . | while IFS= read -r -d '' file; do
        case "$file" in
            *.templ)
                # Template source — regenerate, then rebuild
                echo "  .templ changed: $file → generate + rebuild"
                templ generate 2>&1 | tail -1
                curl -s -X POST http://localhost:8080/dev/rebuild > /dev/null &
                ;;
            *.go)
                echo "  .go changed: $file → rebuild"
                curl -s -X POST http://localhost:8080/dev/rebuild > /dev/null &
                ;;
            *.css)
                echo "  .css changed: $file → reload static"
                curl -s -X POST http://localhost:8080/dev/reload-static > /dev/null &
                ;;
        esac
    done

# Open a shell in the running container
shell:
    docker compose exec harness bash

# Show container logs
logs:
    docker compose logs -f harness
```

**Design decisions:**
- **`fswatch`**: macOS-native file watcher using FSEvents. Instant detection, no polling. Install via `brew install fswatch`.
- **`--latency=0.3`**: Batches rapid events (common with editor auto-save or format-on-save) into 300ms windows, reducing redundant rebuilds.
- **`--exclude='_templ\.go$'`**: Prevents double rebuilds. Without this, editing a `.templ` file would trigger: (1) `.templ` case → `templ generate` + rebuild, then (2) the generated `_templ.go` change → another rebuild. The `--exclude` ensures only the `.templ` case fires. The `.templ` case already runs `templ generate` before triggering the rebuild, so the generated `_templ.go` is included in that build. If someone runs `templ generate` manually outside the watcher, the next hand-written `.go` edit will pick up the changes.
- **Two-tier routing** (was three): `.templ` → generate + rebuild, `.go` → rebuild only, `.css` → reload static (no restart). The `_templ.go` tier was removed because it caused double rebuilds and is redundant with the `.templ` handler.
- **`curl &`**: Non-blocking — watcher doesn't wait for rebuild to complete before processing the next change.
- **`just live`**: Single command starts both Docker container and host watcher. `trap 'kill 0' EXIT` ensures both processes are killed on Ctrl+C.

#### 2. Prerequisites
The host needs:
- `fswatch` — `brew install fswatch`
- `templ` — `go install github.com/a-h/templ/cmd/templ@v0.3.977`
- `curl` — already on macOS

### Success Criteria

#### Automated Verification:
- [ ] `which fswatch` succeeds (installed)
- [ ] `which templ` succeeds (installed)
- [ ] `cd harness && just live` starts both container and watcher

#### Manual Verification:
- [ ] Edit `views/login/login.templ` — watcher logs "generate + rebuild", browser DOM updates in ~2s
- [ ] Edit `internal/server/server.go` — watcher logs "rebuild", browser DOM updates in ~2s
- [ ] Edit `static/styles.css` — watcher logs "reload static", CSS refreshes in ~100ms (no restart in container logs)
- [ ] Introduce Go syntax error — container logs show build failure, old server keeps running
- [ ] Fix error — next change triggers successful rebuild
- [ ] `Ctrl+C` on `just live` — both container and watcher stop cleanly

---

## Testing Strategy

### Smoke Tests:
1. `just live` starts container + watcher without errors
2. `curl -s http://localhost:8080` returns HTML with `id="page-content"`
3. Browser Network tab shows SSE connection to `/dev/sse`

### Hot-Reload Tests:
4. **templ**: Edit text in `views/login/login.templ`. Watcher runs `templ generate` + `POST /dev/rebuild`. Container builds new binary (old server keeps serving). Graceful restart. Browser DOM morphs. ~2s.
5. **Go**: Edit `internal/server/server.go`. `POST /dev/rebuild`. Same flow. ~2s.
6. **CSS**: Edit `static/styles.css`. `POST /dev/reload-static`. No rebuild, no restart. CSS cache busted via open SSE. ~100ms.

### Zero-Downtime Tests:
7. Open browser during rebuild — server responds normally (old binary still serving)
8. Send multiple rapid `POST /dev/rebuild` — first returns 202, subsequent return 409 (already building)
9. Build failure — server logs error, old server keeps running, browser unaffected

### Edge Cases:
10. Syntax error in `.templ` → templ logs error, no `_templ.go` generated, no rebuild triggered
11. Syntax error in `.go` → container logs build failure, old server keeps running
12. `docker compose down` while build in progress → entrypoint trap handles SIGTERM cleanly
13. No auth (OAuth env vars unset) → login page renders, dev SSE still works

## Performance

| Scenario | Timeline |
|----------|---------|
| `.templ` change | 0ms: FSEvents detects → 50ms: templ generates → 100ms: POST /dev/rebuild → 100-1500ms: go build (background, server still serving) → 1500ms: graceful restart (~100ms) → 1600ms: SSE reconnects + morph |
| `.go` change | 0ms: FSEvents detects → 50ms: POST /dev/rebuild → 50-1500ms: go build → 1500ms: restart → 1600ms: morph |
| `.css` change | 0ms: FSEvents detects → 50ms: POST /dev/reload-static → 50ms: ExecuteScript via open SSE → 100ms: browser re-fetches CSS |
| Subsequent builds | Go build cache makes incremental builds fast (~500ms for small changes) |

## Migration Notes

No migration needed. All changes are additive:

**New files:**
- `harness/Dockerfile`
- `harness/docker-compose.yml`
- `harness/.dockerignore`
- `harness/scripts/dev-entrypoint.sh`
- `harness/internal/server/dev.go`
- `harness/views/layout/dev.go`

**Modified files:**
- `harness/views/layout/layout.templ` — add `#page-content` wrapper + conditional dev SSE
- `harness/views/world/world.templ` — refactor to use `layout.Base()` instead of custom `<html>`/`<body>`
- `harness/internal/server/server.go` — add `dev *devState` field, register `/dev/sse`, `/dev/rebuild`, `/dev/reload-static`
- `harness/justfile` — append `live`, `up`, `down`, `watch`, `shell`, `logs` recipes

**New dependency:**
- `golang.org/x/net/html` — already an indirect dependency (`golang.org/x/net v0.48.0` in `go.mod`), just needs a direct import for the HTML parser

Existing workflows (`just dev`, `just generate`, `just build`, `just lint`) unchanged. The `#page-content` wrapper is always present (dev and prod). Dev SSE element only appears when `DEV_MODE=true`.

## Prerequisites

| Tool | Install | Purpose |
|------|---------|---------|
| Docker Desktop | Already installed | Run server on Debian |
| `fswatch` | `brew install fswatch` | Host-side file watcher (native FSEvents) |
| `templ` | `go install github.com/a-h/templ/cmd/templ@v0.3.977` | Host-side template generation |
| `curl` | Already on macOS | Trigger rebuild/reload endpoints |

## References

- Research: `thoughts/CoreyCole/research/2026-02-11-local-dev-setup-hot-reload.md`
- Component 7: `thoughts/CoreyCole/plans/component-7-integration-docker.md`
- Existing graceful shutdown: `harness/main.go:130-138`
- Datastar SDK: `PatchElements`, `ExecuteScript`, `WithSelectorID`, `WithModeInner`
- EventSource auto-reconnect: Browser built-in
