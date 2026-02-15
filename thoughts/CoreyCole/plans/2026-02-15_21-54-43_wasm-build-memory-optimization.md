# WASM Build Memory Optimization — Implementation Plan

## Overview

Redesign how template worlds serve WASM and how builds are serialized to prevent OOM on the VPS. Template worlds should default to pre-built static WASM (no trunk serve running), with on-demand live reload limited to 1 trunk serve at a time. All `trunk build` invocations should be serialized via a global semaphore. The lobby should show a green "live" indicator on whichever template world currently has trunk serve active.

## Current State Analysis

- **VPS**: 31 GB RAM (recently upgraded from 10 GB), 8 cores
- **Template worlds** auto-start `trunk serve` on harness boot via `EnsureTemplateWorlds` → `startTemplateDevServers` (`manager.go:511`)
- **Each `wasm-bindgen` invocation**: ~5 GB RAM (3D) / ~4 GB (2D)
- **2 trunk serve instances** run on boot (one per template type), each watching for file changes
- **No global build concurrency control** — `Builder.Build()` is called via fire-and-forget goroutines
- **Pre-built `dist/` directories exist** in both templates: `templates/2d/dist/` (122 MB WASM) and `templates/3d/client/dist/` (185 MB WASM)
- **Template world checkpoints point to the template dir** — `DirPath = templateDir`, no file copy

### Key Discoveries:
- `trunk serve` is only for template worlds (`startTemplateDevServers` at `manager.go:511`); user world forks already use `trunk build` via `Builder.Build()` (`builder.go:50`)
- Template worlds currently have `WasmPath.Valid = false` — they rely entirely on trunk serve for WASM
- The iframe decision tree in `world.templ:10-46` already handles both trunk serve URLs and static `/wasm/` paths
- `handleWASMArtifacts` (`server.go:543`) serves from `data/wasm-builds/{worldID}/{cpID}/`
- Pre-built dist/ files exist but aren't used by the harness — they'd need to be copied or symlinked to `wasm-builds/`
- The lobby (`lobby.templ:43-54`) renders all worlds identically — no template vs user distinction

## Desired End State

1. **Template worlds accessible via static builds by default** — no trunk serve on boot
2. **On-demand trunk serve** — admin/president can enable live reload for exactly 1 template world at a time
3. **Lobby shows "live" indicator** — green dot + "Live" text on the template world card that has active trunk serve
4. **Build serialization** — global semaphore limits concurrent `wasm-bindgen` invocations (both `trunk build` and `trunk serve` rebuilds)

### Verification:
- Harness boots with 0 trunk serve sessions running
- Template worlds load WASM from static `dist/` files
- Toggling "live" on a template world starts trunk serve, shows green indicator in lobby
- Starting live on world B while world A is live stops world A's trunk serve first
- Multiple simultaneous user builds queue instead of running in parallel

## What We're NOT Doing

- Reducing wasm-bindgen memory usage (no known flags/options)
- Shared trunk serve across template types (each type has different source)
- Pre-building at deploy time via CI (overkill — dist/ files are committed)
- Build priority system (mayor vs user builds)
- Offloading builds to a separate machine

## Implementation Approach

Use the existing `dist/` directories as the static WASM source for template worlds. Add an API endpoint and UI toggle for enabling/disabling trunk serve per template world (max 1 at a time). Add a counting semaphore on `Builder` to serialize checkpoint builds.

---

## Phase 1: Static WASM for Template Worlds

### Overview
Make template worlds serve pre-built WASM from their `dist/` directory by default, removing the auto-start of trunk serve on boot.

### Changes Required:

#### 1. Stop auto-starting trunk serve on boot
**File**: `harness/internal/world/manager.go`
**Changes**: Remove the `StartTrunkServe` call from `startTemplateDevServers`. Template worlds should only start game servers (3D), not trunk serve.

```go
// startTemplateDevServers starts dev servers for a template world.
// 3D: cargo watch (game server) only. Trunk serve is on-demand.
// 2D: no servers needed (client-only).
func (m *Manager) startTemplateDevServers(
	ctx context.Context, worldID, cpID, templateType, templateDir string,
) error {
	// 3D worlds need a game server (cargo watch).
	if templateType == "3d" {
		srv, err := m.GameServers.ConnectDev(worldID, cpID, templateDir)
		if err != nil {
			return fmt.Errorf("starting dev game server: %w", err)
		}

		// Sync server port to DB.
		_, _ = m.db.UpdateCheckpointServerPort(ctx, sqlc.UpdateCheckpointServerPortParams{
			ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
			ID:         cpID,
		})
	}

	// No trunk serve on boot — template worlds use static dist/ builds.
	// Trunk serve is started on-demand via StartLiveReload.
	return nil
}
```

#### 2. Serve template world WASM from dist/ directory
**File**: `harness/internal/world/manager.go`
**Changes**: In `createTemplateWorldDev`, set `WasmPath` to the template's `dist/` directory so the iframe loads from `/wasm/{worldID}/{cpID}/`. Also in `ensureTemplateDevReady`, ensure WasmPath is set.

The `dist/` directory already exists at `templates/2d/dist/` and `templates/3d/client/dist/`. We need to either:
- **Option A**: Symlink `data/wasm-builds/{worldID}/{cpID}/` → `templates/{type}/[client/]dist/`
- **Option B**: Set `WasmPath` to the dist directory and modify `handleWASMArtifacts` to serve from it directly

Option A is cleaner — `handleWASMArtifacts` doesn't change, and symlinks are resolved transparently.

```go
// In createTemplateWorldDev, after DB commit:

// Symlink dist/ into wasm-builds so handleWASMArtifacts can serve it.
distDir := filepath.Join(templateDir, "dist")
if templateType == "3d" {
	distDir = filepath.Join(templateDir, "client", "dist")
}
if _, err := os.Stat(distDir); err == nil {
	wasmDir := filepath.Join(m.dataDir, "wasm-builds", worldID, cpID)
	_ = os.MkdirAll(filepath.Dir(wasmDir), 0o750)
	_ = os.Symlink(distDir, wasmDir)

	// Update checkpoint WasmPath so the iframe loads static WASM.
	_ = m.db.UpdateCheckpointWasmPath(ctx, sqlc.UpdateCheckpointWasmPathParams{
		WasmPath: sql.NullString{String: wasmDir, Valid: true},
		ID:       cpID,
	})
}
```

Same logic in `ensureTemplateDevReady` for existing template worlds that don't have WasmPath set yet.

#### 3. Fix 2D iframe reload after build
**File**: `harness/internal/server/events.go`
**Changes**: The `EventBuildCompleted` handler (line 285-295) only reloads the iframe for 3D worlds (`serverPort > 0`). Add a reload path for 2D worlds too.

```go
case events.EventBuildCompleted:
	// ... existing signal patch ...

	serverPort, _ := e["serverPort"].(int)
	worldID, _ := e["worldID"].(string)
	cpID, _ := e["cpID"].(string)

	if serverPort > 0 {
		// 3D: reload with server_port
		script := fmt.Sprintf(
			"var f=document.getElementById('game-frame');"+
				"if(f){f.src='/wasm/%s/%s/index.html?server_port=%d';}",
			worldID, cpID, serverPort,
		)
		if err := sse.ExecuteScript(script); err != nil {
			return err
		}
	} else {
		// 2D: reload without server_port
		script := fmt.Sprintf(
			"var f=document.getElementById('game-frame');"+
				"if(f){f.src='/wasm/%s/%s/index.html';}",
			worldID, cpID,
		)
		if err := sse.ExecuteScript(script); err != nil {
			return err
		}
	}
```

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build -o /dev/null .`
- [ ] `tmux list-sessions` after harness boot shows 0 `cm-trunk-*` sessions
- [ ] Template world pages load WASM from `/wasm/{worldID}/{cpID}/index.html`
- [ ] Lint passes: `cd harness && golangci-lint run ./...`

#### Manual Verification:
- [ ] Navigate to 2D Template World in lobby — game loads from static WASM
- [ ] Navigate to 3D Template World in lobby — game loads from static WASM with game server
- [ ] Memory usage on boot is lower (no trunk serve processes)

---

## Phase 2: On-Demand Live Reload (1 at a Time)

### Overview
Add an API endpoint and UI toggle to enable/disable trunk serve for a specific template world. Only one template world can have live reload active at a time — enabling it on world B stops it on world A.

### Changes Required:

#### 1. Add live reload management to WorldManager
**File**: `harness/internal/world/manager.go`
**Changes**: Add `StartLiveReload` and `StopLiveReload` methods. Track which world has active live reload.

```go
// StartLiveReload enables trunk serve for a template world.
// Only one world can be live at a time — stops any existing live reload first.
func (m *Manager) StartLiveReload(ctx context.Context, worldID string) error {
	// Stop any existing live reload.
	m.StopLiveReload(ctx)

	// Look up the world's checkpoint and template dir.
	w, err := m.db.GetWorld(ctx, worldID)
	if err != nil {
		return fmt.Errorf("getting world: %w", err)
	}

	checkpoints, err := m.db.GetCheckpointTree(ctx, worldID)
	if err != nil || len(checkpoints) == 0 {
		return fmt.Errorf("no checkpoints for world %s", worldID)
	}
	cp := checkpoints[0]

	templateDir, ok := m.templateDirs[w.TemplateType]
	if !ok {
		return fmt.Errorf("not a template world")
	}

	// Only allow live reload on template worlds (DirPath == templateDir).
	if cp.DirPath != templateDir {
		return fmt.Errorf("live reload only supported for template worlds")
	}

	// Start trunk serve.
	_, err = m.GameServers.StartTrunkServe(worldID, cp.ID, templateDir)
	if err != nil {
		return fmt.Errorf("starting trunk serve: %w", err)
	}

	return nil
}

// StopLiveReload stops trunk serve for whichever template world is currently live.
func (m *Manager) StopLiveReload(ctx context.Context) {
	// Scan all servers for one with an active trunk session.
	for _, srv := range m.GameServers.RecoveredServers() {
		if srv.TrunkSessionName != "" {
			m.GameServers.StopTrunkServe(srv.WorldID, srv.CPID)
		}
	}
}

// GetLiveReloadWorldID returns the world ID that currently has trunk serve active, or "".
func (m *Manager) GetLiveReloadWorldID() string {
	for _, srv := range m.GameServers.RecoveredServers() {
		if srv.TrunkSessionName != "" && srv.TrunkPort > 0 {
			return srv.WorldID
		}
	}
	return ""
}
```

#### 2. Add API endpoint for toggling live reload
**File**: `harness/internal/server/server.go`
**Changes**: Add admin-only endpoint `POST /api/live-reload/:worldID` to toggle live reload on/off.

```go
// In registerRoutes, admin group:
adminGroup.POST("/api/live-reload/:worldID", s.handleToggleLiveReload)
adminGroup.DELETE("/api/live-reload", s.handleStopLiveReload)
```

**File**: `harness/internal/server/live_reload.go` (new file)
```go
package server

func (s *Server) handleToggleLiveReload(c echo.Context) error {
	worldID := c.Param("worldID")
	ctx := c.Request().Context()

	// Check if this world is already live.
	currentLive := s.WorldManager.GetLiveReloadWorldID()
	if currentLive == worldID {
		// Toggle off.
		s.WorldManager.StopLiveReload(ctx)
		s.EventBus.PublishGlobal(map[string]any{
			"event":   events.EventLiveReloadChanged,
			"worldID": "",
		})
		return c.JSON(http.StatusOK, map[string]string{"status": "stopped"})
	}

	// Start live reload (stops any other world's trunk serve first).
	if err := s.WorldManager.StartLiveReload(ctx, worldID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	s.EventBus.PublishGlobal(map[string]any{
		"event":   events.EventLiveReloadChanged,
		"worldID": worldID,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "live", "worldID": worldID})
}

func (s *Server) handleStopLiveReload(c echo.Context) error {
	s.WorldManager.StopLiveReload(c.Request().Context())
	s.EventBus.PublishGlobal(map[string]any{
		"event":   events.EventLiveReloadChanged,
		"worldID": "",
	})
	return c.JSON(http.StatusOK, map[string]string{"status": "stopped"})
}
```

#### 3. Add EventLiveReloadChanged event type
**File**: `harness/internal/events/types.go`
**Changes**: Add new event type.

```go
const EventLiveReloadChanged = "live_reload.changed"
```

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build -o /dev/null .`
- [ ] `POST /api/live-reload/{worldID}` starts trunk serve for that world
- [ ] Second `POST /api/live-reload/{otherWorldID}` stops the first and starts the second
- [ ] `DELETE /api/live-reload` stops all trunk serve
- [ ] Only admin users can access the endpoints

#### Manual Verification:
- [ ] Toggling live reload on a template world starts trunk serve (check `tmux list-sessions`)
- [ ] Only 1 trunk serve runs at a time
- [ ] The live world page shows trunk-served WASM (hot reload works)

---

## Phase 3: Lobby "Live" Indicator

### Overview
Show a green dot with "Live" text on the lobby world card that currently has active trunk serve. Update in real-time via SSE when live reload is toggled.

### Changes Required:

#### 1. Pass live world ID to lobby template
**File**: `harness/internal/server/server.go`
**Changes**: In `handleRoot`, check which world has active trunk serve and pass it to the template.

```go
// In handleRoot, after fetching worlds:
liveWorldID := s.WorldManager.GetLiveReloadWorldID()
return render(c, lobby.Page(&user, worlds, liveWorldID))
```

#### 2. Update lobby template with live indicator
**File**: `harness/views/lobby/lobby.templ`
**Changes**: Add `liveWorldID` parameter, show green dot on matching world card.

```go
templ Page(user *sqlc.User, worlds []sqlc.World, liveWorldID string) {
	// ... existing header ...
	for _, w := range worlds {
		<a
			href={ templ.SafeURL("/world/" + w.ID) }
			class="block p-4 border border-border rounded-lg bg-card hover:border-muted-foreground/40 transition-colors no-underline text-inherit"
		>
			<div class="flex items-center gap-2 mb-1">
				<h3 class="text-base">{ w.Name }</h3>
				if w.ID == liveWorldID {
					<span class="inline-flex items-center gap-1 text-xs text-emerald-400 font-medium">
						<span class="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
						Live
					</span>
				}
			</div>
			if w.Description.Valid {
				<p class="text-muted-foreground text-[13px]">{ w.Description.String }</p>
			}
		</a>
	}
```

#### 3. Live-update the indicator via SSE
**File**: `harness/internal/server/events.go`
**Changes**: Handle `EventLiveReloadChanged` in `handleGlobalEvent` to re-render the world cards grid via SSE.

```go
case events.EventLiveReloadChanged:
	liveWorldID, _ := e["worldID"].(string)
	// Re-render the world cards via PatchElementTempl.
	worlds, _ := s.DB.ListWorlds(context.Background())
	return sse.PatchElementTempl(
		lobby.WorldCards(worlds, liveWorldID),
		datastar.WithSelectorID("world-cards"),
	)
```

This requires extracting the world cards grid into a separate templ component with a wrapper `div id="world-cards"`:

**File**: `harness/views/lobby/lobby.templ`
```go
templ WorldCards(worlds []sqlc.World, liveWorldID string) {
	for _, w := range worlds {
		// ... card with live indicator (same as above) ...
	}
}
```

And wrapping the grid div with the ID:
```go
<div id="world-cards" class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3 mb-6">
	@WorldCards(worlds, liveWorldID)
</div>
```

#### 4. Add admin toggle button on lobby (optional)
**File**: `harness/views/lobby/lobby.templ`
**Changes**: For admin users, show a small "Go Live" / "Stop" button on template world cards.

```go
if user.Role == "admin" && isTemplateWorld(w) {
	if w.ID == liveWorldID {
		<button
			class="text-xs text-red-400 hover:text-red-300"
			data-on:click__prevent={ datastar.PostSSE(fmt.Sprintf("/api/live-reload/%s", w.ID)) }
		>
			Stop Live
		</button>
	} else {
		<button
			class="text-xs text-emerald-400 hover:text-emerald-300"
			data-on:click__prevent={ datastar.PostSSE(fmt.Sprintf("/api/live-reload/%s", w.ID)) }
		>
			Go Live
		</button>
	}
}
```

To determine if a world is a template world, check if the name matches `templateWorldName()` convention or add a `is_template` column to the worlds table.

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build -o /dev/null .`
- [ ] `templ generate` runs cleanly
- [ ] Lint passes: `cd harness && golangci-lint run ./...`

#### Manual Verification:
- [ ] Lobby shows green "Live" dot + text on the active live-reload world
- [ ] Toggling live reload updates all connected lobby clients in real-time via SSE
- [ ] Admin sees "Go Live" / "Stop Live" buttons on template world cards
- [ ] Non-admin users see the live indicator but not the toggle buttons

---

## Phase 4: Build Semaphore

### Overview
Add a global counting semaphore to `Builder` that limits concurrent `Build()` calls to 1 (configurable). Builds that can't acquire the semaphore wait up to their timeout.

### Changes Required:

#### 1. Add semaphore to Builder
**File**: `harness/internal/build/builder.go`
**Changes**: Add `golang.org/x/sync/semaphore` to serialize builds.

```go
import "golang.org/x/sync/semaphore"

type Builder struct {
	db            *db.DB
	logger        *slog.Logger
	wasmBuildsDir string
	logsDir       string
	buildSem      *semaphore.Weighted // limits concurrent builds
}

func NewBuilder(
	database *db.DB,
	logger *slog.Logger,
	wasmBuildsDir, logsDir string,
) *Builder {
	return &Builder{
		db:            database,
		logger:        logger,
		wasmBuildsDir: wasmBuildsDir,
		logsDir:       logsDir,
		buildSem:      semaphore.NewWeighted(1), // max 1 concurrent build
	}
}
```

#### 2. Acquire semaphore in Build()
**File**: `harness/internal/build/builder.go`
**Changes**: Wrap the build execution with semaphore acquire/release.

```go
func (b *Builder) Build(cp *sqlc.Checkpoint, isInitial bool, templateType string) error {
	timeout := BuildTimeoutIncremental
	if isInitial {
		timeout = BuildTimeoutInitial
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Wait for build slot — serializes all builds system-wide.
	b.logger.Info("waiting for build slot",
		"worldID", cp.WorldID, "cpID", cp.ID)
	waitStart := time.Now()

	if err := b.buildSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("waiting for build slot: %w", err)
	}
	defer b.buildSem.Release(1)

	waitDuration := time.Since(waitStart)
	if waitDuration > time.Second {
		b.logger.Info("build slot acquired after wait",
			"worldID", cp.WorldID, "cpID", cp.ID,
			"waitMs", waitDuration.Milliseconds())
	}

	// ... rest of Build() unchanged ...
```

#### 3. Add dependency
**File**: `harness/go.mod`
**Changes**: Add `golang.org/x/sync` dependency.

```bash
cd harness && go get golang.org/x/sync
```

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build -o /dev/null .`
- [ ] `go mod tidy` succeeds
- [ ] Lint passes: `cd harness && golangci-lint run ./...`

#### Manual Verification:
- [ ] Submit 2 prompts simultaneously (different users) — second build waits for first to finish
- [ ] Build logs show "waiting for build slot" / "build slot acquired after wait" messages
- [ ] Builds still complete successfully (no deadlock)
- [ ] Build timeout still works (semaphore acquire respects the context deadline)

---

## Testing Strategy

### Unit Tests:
- `GetLiveReloadWorldID` returns correct world when trunk is running
- `StartLiveReload` stops existing live reload before starting new one
- Build semaphore blocks second build until first completes

### Integration Tests:
- Boot harness → verify 0 trunk sessions
- Toggle live reload → verify 1 trunk session
- Toggle live on another world → verify first stopped, second started
- Submit build while another is running → verify queuing behavior

### Manual Testing Steps:
1. Deploy to VPS, verify clean boot (no trunk serve)
2. Navigate to template worlds in lobby — verify WASM loads from static files
3. As admin, click "Go Live" on 2D Template World — verify green indicator appears
4. Navigate to 2D Template World — verify trunk-served WASM with hot reload
5. Edit a template file — verify live reload works
6. Click "Go Live" on 3D Template World — verify 2D stops, 3D starts
7. Submit a user prompt while a build is running — verify it queues
8. Monitor `htop` during concurrent activity — verify no OOM

## Performance Considerations

- Build semaphore with max=1 means builds queue. With N users, the Nth user waits up to N×5min (incremental timeout). This is acceptable because builds typically take 1-3 minutes.
- Template worlds loading from `dist/` symlinks avoids file copies and doubles as cache.
- The 3D game server (`cargo watch`) still runs continuously for the 3D template world — this is separate from trunk serve and uses minimal memory until a rebuild is triggered.

## Migration Notes

- Existing template worlds in SQLite have `wasm_path = NULL`. The `ensureTemplateDevReady` path needs to set up the symlink and update WasmPath on first boot after this change.
- No DB schema changes required — all state is in-memory (GameServerManager) or uses existing columns (WasmPath).
- The `dist/` directories must be kept up to date. The president should run `trunk build --release` and commit the output whenever template code changes significantly.

## References

- Handoff: `thoughts/CoreyCole/handoffs/general/2026-02-15_20-35-20_wasm-build-memory-optimization.md`
- Builder: `harness/internal/build/builder.go:50`
- GameServerManager: `harness/internal/world/game_server.go:67`
- StartTrunkServe: `harness/internal/world/game_server.go:611`
- EnsureTemplateWorlds: `harness/internal/world/manager.go:359`
- Lobby template: `harness/views/lobby/lobby.templ:12`
- World page iframe: `harness/views/world/world.templ:10-46`
- WASM artifact handler: `harness/internal/server/server.go:543`
- Build event handler: `harness/internal/server/events.go:259`
- Rate limiter: `harness/internal/world/rate_limit.go`
- Dev build mutex pattern: `harness/internal/server/dev.go:152`
