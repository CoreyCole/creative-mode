---
date: 2026-02-16T08:35:03-08:00
researcher: CoreyCole
git_commit: 2902cfb9c2db2d24b62a251e6d54f014b62587c5
branch: main
repository: creative-mode
topic: "2D Template World Feature Audit: Demo Readiness for All Features"
tags: [research, codebase, 2d-template, demo, assets, rooms, checkpoint, mayor-dashboard, build-pipeline]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: 2D Template World — Complete Feature Audit for Demo

**Date**: 2026-02-16T08:35:03-08:00
**Researcher**: CoreyCole
**Git Commit**: 2902cfb9c2db2d24b62a251e6d54f014b62587c5
**Branch**: main
**Repository**: creative-mode

## Research Question

Audit all features of the 2D template world to ensure they work for the main demo: uploading assets, placing assets, making rooms, forking the world with a prompt, looking at the checkpoint history, and looking at the mayor debug dashboard.

## Summary

The 2D template has a working end-to-end flow for the core demo (generate image -> save -> place in room -> see it render). However, several features have gaps ranging from minor polish issues to non-functional subsystems. The mayor dashboard Memory tab never loads file content, the checkpoint tree "Load" button doesn't actually switch checkpoints, there is no browser UI for direct file upload, and the build pipeline has a known silent failure mode with hook secrets. Below is a feature-by-feature breakdown.

---

## Feature 1: Uploading Assets

### Status: PARTIAL — API works, no browser upload UI, image generation is the primary flow

### How It Works

**Primary flow (Image Generation):**
1. User opens Assets tab in overlay sidebar
2. Types a prompt, clicks "Generate" -> `POST /api/images/generate` calls Gemini API
3. Preview shown via SSE patch into `#image-gen-content`
4. User clicks "Save" -> `POST /api/images/save` writes to `data/shared-assets/generated/{timestamp}-{slug}.png`
5. Asset tree refreshes via SSE

**Secondary flow (API-only upload):**
- `POST /api/assets/upload` at `harness/internal/server/assets.go:23` accepts multipart form data
- Fields: `file` (required), `folder` (optional subdirectory)
- Validates MIME types: png, jpeg, webp, gif
- Writes to `data/shared-assets/{folder}/{filename}`
- **No browser UI exists** — no `<input type="file">`, no upload button anywhere in the views

### Key Files
- `harness/internal/server/imagegen.go:33-176` — Generate + Save handlers
- `harness/internal/server/assets.go:23-100` — Upload handler (API-only)
- `harness/views/imagegen/imagegen.templ:10-47` — Assets tab UI (ImageGenPanel)
- `harness/views/imagegen/asset_tree.templ:6-67` — Asset tree with Place/Copy buttons
- `harness/internal/server/server.go:121` — `GET /assets/*` public serving route

### Issues
1. **No browser upload form** — Upload endpoint exists but only usable via curl. The UI only supports Gemini image generation.
2. **No body size limit** on upload endpoint — No `middleware.BodyLimit("10M")` despite being planned.
3. **Asset tree only shows `generated/` folder** — Files uploaded via API to other folders (e.g., `rooms/`) don't appear in the tree.
4. **No file deletion endpoint** — Assets can only be removed via filesystem access.
5. **MIME validation uses multipart header** — Not content-sniffed, so it can be spoofed.
6. **Duplicate rejection with no overwrite** — Returns 409 Conflict if file exists; no replace mechanism.

---

## Feature 2: Placing Assets in Rooms

### Status: WORKING — Form-based placement flow functional, no visual editor

### How It Works

1. User clicks "Place" on an asset in the tree -> sets `$place_asset_path` signal, fetches room list
2. `GET /api/rooms` at `harness/internal/server/rooms.go:57-70` lists all `.room.json` files
3. User selects a room -> `GET /api/rooms/:roomID/hotspots` shows placement targets
4. Three placement options:
   - **Background**: `POST /api/rooms/:roomID/place/background` sets `background_image`
   - **Existing hotspot**: `POST /api/rooms/:roomID/place/hotspot/:hotspotID` replaces image, auto-resizes to aspect ratio
   - **New hotspot**: `POST /api/rooms/:roomID/place/new` with scale/x/y/label form fields
5. Room JSON written to disk -> SSE executes `f.src=f.src` to reload iframe

### Key Files
- `harness/internal/server/rooms.go:27-436` — All room/placement handlers and helpers
- `harness/views/imagegen/placement.templ:11-159` — Placement UI (room list, targets, new hotspot form)
- `harness/views/world/signals.go:28-32` — Placement signals (place_scale, place_x, place_y, place_label)

### Issues
1. **No visual editor** — Position X/Y entered numerically (top-left coords in 1280x720 space). No click-to-place on canvas.
2. **Full iframe reload per placement** — Every placement reloads the entire ~5MB WASM app (2-3 second delay). The `reload-room` postMessage path exists in Bevy but doesn't re-fetch assets due to caching.
3. **Dialog text has no backdrop** — White text on image backgrounds is unreadable (`interaction.rs:112-124`).
4. **Labels render on top of image hotspots** — Text labels at z=2.0 overlay placed sprites (`room.rs:313-323`).
5. **No room creation UI** — Can't create a new room from the browser; must edit JSON by hand or via Claude Code builds.

---

## Feature 3: Making Rooms

### Status: PARTIAL — Rooms are data-driven JSON, created by template seeding or Claude Code, no browser room creation

### How It Works

- Rooms are JSON files in `data/shared-assets/rooms/` following schema at `rooms.go:27-45`
- On startup, `main.go:61-83` seeds from `templates/2d/rooms/` (won't overwrite existing)
- Two default rooms: `lobby.room.json` and `garden.room.json`
- New rooms created by: (a) manually adding JSON files, (b) Claude Code during a build prompt, or (c) API
- Room JSON schema: `id`, `name`, `background_color`, `background_image`, `hotspots[]`
- Hotspot actions: `dialog`, `navigate_room`, `navigate_world`, `navigate_checkpoint`, `open_embed`

### Key Files
- `templates/2d/rooms/lobby.room.json` — Default lobby room
- `templates/2d/rooms/garden.room.json` — Default garden room
- `templates/2d/src/room.rs:31-40` — Rust side room JSON schema
- `templates/2d/src/room.rs:157-326` — Room loading + entity spawning

### Issues
1. **No room creation UI** — No "New Room" button or form. Rooms can only be added via filesystem, Claude Code builds, or by directly POSTing room JSON.
2. **All worlds share the same `data/shared-assets/`** — No per-world or per-checkpoint asset isolation. Changes to a room affect all users.

---

## Feature 4: Forking the World with a Prompt

### Status: WORKING with KNOWN FAILURE MODES — Core pipeline functional, hook secret issue causes silent failures

### How It Works

1. User types prompt in bottom bar, clicks "Build" -> `POST /world/:worldID/prompt`
2. `handlePrompt` at `server.go:418-502` reads `prompt_text` + `current_checkpoint_id` signals
3. `Orchestrator.HandlePrompt` at `claude.go:73-135`:
   - Forks checkpoint: copies dir, hardlinks build cache, DB insert with status "building"
   - Updates MEMORY.md with the prompt
   - Creates tmux session `cm-{worldID}-{cpID}`
   - Sends prompt to Claude Code via `claude --dangerously-skip-permissions --input-file`
4. Immediate SSE response: `build_status="editing"`, `prompt_text=""`
5. Claude Code edits files; hook scripts POST events to `/api/claude-event`
6. On `claude.session_stopped`, `BuildCheckpoint` runs:
   - For 2D: skips server binary build, runs `trunk build --release` only
   - Updates DB status to "ready"
   - Publishes `EventBuildCompleted`
7. SSE handler reloads iframe to `/wasm/{worldID}/{cpID}/index.html`

### Key Files
- `harness/views/world/overlay.templ:62-87` — Prompt input + Build button
- `harness/internal/server/server.go:418-502` — handlePrompt
- `harness/internal/claude/claude.go:73-253` — HandlePrompt + BuildCheckpoint
- `harness/internal/builder/builder.go:50-158` — Build method (trunk build)
- `harness/internal/world/manager.go:224-308` — ForkCheckpoint
- `harness/internal/server/events.go:249-308` — SSE event handlers for build status

### 2D-Specific Differences from 3D
- No dev game server during Claude editing (skipped at `claude.go:95`)
- No server binary build (skipped at `builder.go:87`)
- No prod game server after build (skipped at `claude.go:214`)
- Trunk runs from project root, not `client/` subdir (`builder.go:117-119`)
- Iframe reload omits `?server_port` parameter (`events.go:297-305`)

### Issues
1. **CRITICAL: Hook scripts don't include `X-Hook-Secret` header** — If `CM_HOOK_SECRET` is set, all hook callbacks are rejected with 403. `on-stop.sh` never reaches the harness, `BuildCheckpoint` never fires, checkpoint stays stuck "building" forever. (Per research doc `2026-02-16_08-43-21`)
2. **Rate limiter**: 30-second cooldown + no concurrent builds per user (`rate_limit.go:12,57-70`).
3. **Build timeout**: 15 min initial, 5 min incremental (`builder.go:21-23`).
4. **WASM build constraint**: Each `wasm-bindgen` uses ~5GB RAM. VPS has 10GB, so only 1 build at a time.

---

## Feature 5: Checkpoint History

### Status: PARTIALLY BROKEN — Tree displays but "Load" button doesn't switch checkpoints

### How It Works

1. User clicks "Tree" button in overlay top bar -> toggles `$show_checkpoint_tree` signal
2. `CheckpointTree` at `views/world/checkpoint_tree.templ:8-31` renders flat chronological list
3. Each checkpoint shows: status dot (green/blue/red), name or truncated prompt, "Load" button
4. "Load" button calls `loadCheckpoint(worldID, cpID)` at `game-loader.js:39-41`
5. `loadCheckpoint` does `window.location.href = '/world/' + worldID` — **discards cpID**
6. On page load, `handleWorldView` reads `user_positions` table to determine checkpoint
7. `user_positions` is updated only during `ForkCheckpoint` (automatic) or `handleCheckpointView` POST

### Key Files
- `harness/views/world/checkpoint_tree.templ:8-31` — Tree panel UI
- `harness/static/game-loader.js:39-41` — loadCheckpoint function
- `harness/internal/server/server.go:316-390` — handleWorldView (reads user position)
- `harness/internal/server/server.go:393-413` — handleCheckpointView (updates user position)
- `harness/internal/db/migrations/001_initial.sql:28-54` — Checkpoint + user_positions schema
- `harness/internal/db/db.go:191-239` — GetCheckpointAncestry

### Lineage View
- "Lineage" tab in chat panel fetches `/world/:worldID/lineage/:cpID`
- `handleLineage` at `server.go:726-737` calls `GetCheckpointAncestry`
- Shows root-to-current chain with prompts, summaries, timestamps

### Issues
1. **CRITICAL: "Load" button doesn't switch checkpoints** — `loadCheckpoint()` ignores the cpID parameter and just navigates to `/world/{worldID}`. The user lands on whatever checkpoint `user_positions` already points to. It needs to call `POST /world/:worldID/checkpoint/:cpID` first to update position before navigating.
2. **Flat tree rendering** — No visual tree/branching structure. Just a chronological list even though the data model supports branching via `parent_checkpoint_id`.
3. **No checkpoint deletion** — Can't remove failed or unwanted checkpoints.

---

## Feature 6: Mayor Debug Dashboard

### Status: PARTIALLY WORKING — Overview/Builds/Activity tabs work, Messages tab static, Memory tab broken

### How It Works

1. Navigate to `/mayor/:worldID` (requires approved user)
2. `handleMayorDashboard` at `mayor_dashboard.go:19-53` fetches: world, checkpoints, builds (50), activity (50), messages (all), sessions (50)
3. Page rendered with 5 tabs: Overview, Builds, Activity, Messages, Memory
4. SSE stream at `/mayor/:worldID/events` subscribes to world EventBus channel

### Key Files
- `harness/internal/server/mayor_dashboard.go:19-162` — All dashboard handlers
- `harness/views/mayor/dashboard.templ:36-269` — Dashboard templates
- `harness/internal/server/server.go:181-184` — Route registration

### Tab Details

**Overview** (WORKING): 3 stat cards (checkpoints, builds, messages) + recent activity + recent builds

**Builds** (WORKING with SSE): Build list with status badges (building=yellow, ready=green, failed=red), timestamps, prompts. Live-updated via SSE `PatchElementTempl` targeting `#mayor-builds-tab`.

**Activity** (WORKING with SSE): Activity list with type badges, details, timestamps. Live-updated via SSE targeting `#mayor-activity-tab`.

**Messages** (STATIC): Discord messages with author name (color-coded: mayor=primary, system=yellow, user=default), timestamps, content. **NOT live-updated** — SSE handler only patches builds and activity, not messages. New messages after page load require a full refresh.

**Memory** (BROKEN): Shows "Loading..." placeholder divs for SOUL.md and MEMORY.md. **No mechanism loads the content** — no `data-init`, no JS, no SSE push triggers the `/mayor/:worldID/file` GET endpoint. The file read/write API exists and works, but nothing in the UI calls it.

### Issues
1. **Memory tab never loads** — `MemoryTab` at `dashboard.templ:248-269` renders `<div id="mayor-soul-content">Loading...</div>` but nothing fetches the file content.
2. **SSE patch targets may be missing** — SSE patches target `#mayor-builds-tab` and `#mayor-activity-tab`, but the templ components don't render root elements with those IDs. Patches may be silently dropped.
3. **Messages tab not live-updated** — New Discord messages after page load don't appear until refresh.
4. **DB query errors silently discarded** — Queries 2-6 in `handleMayorDashboard` assign errors to `_` (lines 31-41).
5. **Messages query has no limit** — `GetMayorMessages` returns ALL messages for a world with no pagination.

---

## Architecture Insights

### Data-Driven Room System
Rooms are JSON files on disk, not database records. The WASM client fetches them via HTTP with `no-cache` headers. This means changes are instant (just edit JSON and reload) but there's no versioning, no per-checkpoint isolation, and no undo.

### Shared Asset Directory
All 2D worlds share `data/shared-assets/`. This is simple but means asset changes affect all users of all worlds. Per-world or per-checkpoint asset directories would need the build pipeline to copy assets into each checkpoint's dir.

### SSE-Driven UI
All real-time updates flow through Datastar SSE: build status, chat messages, asset tree refresh, placement results, and iframe reloads. The pattern is consistent: server renders templ fragments, patches DOM via `PatchElementTempl` or updates signals via `MarshalAndPatchSignals`.

### Template Type Branching
2D vs 3D is a string check (`templateType == "3d"`) scattered through the codebase, not a strategy pattern. Key branch points: `claude.go:95`, `builder.go:87`, `manager.go:188`, `events.go:285`, `world.templ:12`.

---

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/research/2026-02-16_08-43-21_2d-world-review-and-build-pipeline.md` — Identified the hook secret silent failure, dialog backdrop issue, and label overlay issue
- `thoughts/CoreyCole/plans/2026-02-16_09-12-34_launch-readiness-fixes.md` — Launch hardening plan covering CORS, body limits, security headers, SQLite tuning
- `thoughts/CoreyCole/handoffs/general/2026-02-13_11-42-28_2d-asset-image-support.md` — Original implementation of image generation + placement flow
- `thoughts/CoreyCole/handoffs/general/2026-02-13_15-25-28_mayor-dashboard-design.md` — Mayor dashboard design specs
- `thoughts/CoreyCole/handoffs/general/2026-02-13_13-27-43_gemini-image-gen-datastar-bug.md` — Known Datastar binding bug with image generation

---

## Prioritized Fix List for Demo

### P0 — Must Fix for Demo

| # | Issue | Feature | Impact |
|---|-------|---------|--------|
| 1 | Hook scripts missing `X-Hook-Secret` header | Build Pipeline | Builds silently fail, checkpoint stuck "building" forever |
| 2 | `loadCheckpoint()` ignores cpID | Checkpoint History | "Load" button doesn't switch checkpoints |
| 3 | Memory tab never loads file content | Mayor Dashboard | Tab shows "Loading..." permanently |
| 4 | SSE patch target IDs missing from templ | Mayor Dashboard | Build/Activity live updates may be silently dropped |

### P1 — Should Fix for Demo Polish

| # | Issue | Feature | Impact |
|---|-------|---------|--------|
| 5 | Dialog text has no backdrop | Room Rendering | Text unreadable on images |
| 6 | Labels overlay image hotspots | Room Rendering | Placed sprites have text on top |
| 7 | Messages tab not live-updated | Mayor Dashboard | New Discord messages don't appear |
| 8 | Full iframe reload per placement | Asset Placement | 2-3 second delay per placement |
| 9 | No browser upload UI | Asset Upload | Can't upload custom images from browser |

### P2 — Nice to Have

| # | Issue | Feature | Impact |
|---|-------|---------|--------|
| 10 | Flat checkpoint tree (no branching) | Checkpoint History | Doesn't show parent-child relationships |
| 11 | No room creation UI | Rooms | Must create rooms via Claude Code builds |
| 12 | No body limit on upload | Asset Upload | Accepts arbitrarily large files |
| 13 | Asset tree only shows generated/ | Asset Upload | Manually uploaded files hidden |
| 14 | Messages query has no limit | Mayor Dashboard | Could be slow with many messages |

---

## Code References

- `harness/internal/server/assets.go:23-100` — Asset upload handler
- `harness/internal/server/imagegen.go:33-176` — Image generation + save
- `harness/internal/server/rooms.go:27-436` — Room listing, placement handlers
- `harness/internal/server/server.go:316-502` — World view + prompt handlers
- `harness/internal/server/events.go:56-321` — SSE event handlers
- `harness/internal/server/mayor_dashboard.go:19-162` — Mayor dashboard handlers
- `harness/internal/claude/claude.go:73-253` — HandlePrompt + BuildCheckpoint
- `harness/internal/builder/builder.go:50-158` — Build method
- `harness/internal/world/manager.go:224-308` — ForkCheckpoint
- `harness/views/world/world.templ:10-64` — World page template
- `harness/views/world/overlay.templ:12-87` — Overlay UI
- `harness/views/world/checkpoint_tree.templ:8-31` — Checkpoint tree panel
- `harness/views/mayor/dashboard.templ:36-269` — Mayor dashboard templates
- `harness/views/imagegen/placement.templ:11-159` — Placement UI
- `harness/static/game-loader.js:1-55` — postMessage bridge
- `templates/2d/src/room.rs:31-326` — Room schema + rendering
- `templates/2d/src/interaction.rs:112-124` — Dialog spawning

## Open Questions

1. Should we add per-checkpoint asset directories instead of the shared `data/shared-assets/`?
2. Should the room system support a visual drag-and-drop editor, or is the numeric placement form sufficient for the demo?
3. Should we gate the mayor dashboard behind admin-only access or keep it available to all approved users?
4. Is the 2D template intended to be the only demo template, or will 3D and boardgame also be shown?
