---
date: 2026-02-16T09:39:51-08:00
researcher: CoreyCole
git_commit: 2902cfb9c2db2d24b62a251e6d54f014b62587c5
branch: main
repository: creative-mode
topic: "2D Demo Feature Fixes Implementation Plan"
tags: [implementation, plan, 2d-template, mayor-dashboard, checkpoint, upload]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
type: implementation_plan
---

# 2D Demo Feature Fixes — Implementation Plan

## Overview

Fix the remaining demo-blocking bugs identified in the [2D Demo Feature Audit](../research/2026-02-16_08-35-03_2d-demo-feature-audit.md). After cross-referencing the audit against the current codebase, **3 of the original 9 items are already fixed** (hook secret auth, dialog backdrop, label z-ordering) and **1 is a known limitation** (iframe reload per placement — the WASM binary is browser-cached so only Bevy init is repeated, and there's no lighter mechanism to force room JSON re-reads). This plan covers the **5 actionable remaining items**.

## Current State Analysis

### Already Fixed (removed from scope)
| # | Issue | Evidence |
|---|-------|----------|
| P0-1 | Hook scripts missing X-Hook-Secret | All 3 hook scripts use `${CM_HOOK_SECRET:+-H "X-Hook-Secret: $CM_HOOK_SECRET"}`. `session.go:48-51` propagates the env var. |
| P1-5 | Dialog text no backdrop | `interaction.rs:112-134` already spawns dark backdrop sprite at z=9.5 + text at z=10.0 |
| P1-6 | Labels overlay image hotspots | `room.rs:312` has `if hotspot.image.is_none()` guard — labels only render on non-image hotspots |

### Remaining Issues (this plan)
| # | Priority | Issue | Location |
|---|----------|-------|----------|
| 1 | P0 | loadCheckpoint() ignores cpID | `harness/static/game-loader.js:39-41` |
| 2 | P0 | Memory tab never loads | `harness/views/mayor/dashboard.templ:248-269` |
| 3 | P0 | SSE patch target IDs missing | `harness/internal/server/mayor_dashboard.go:78,84` |
| 4 | P1 | Messages tab not live-updated | `harness/internal/server/mayor_dashboard.go:67` |
| 5 | P1 | No browser upload UI | No upload form exists in any templ file |
| 6 | P1 | Full iframe reload per placement | `rooms.go:141-143,218-219,362-363` — by design, WASM caches binary (known limitation, not fixing) |

### Key Discoveries
- `handleCheckpointView` at `server.go:392-413` already exists and does exactly what we need — calls `SetUserPosition` to update the user's checkpoint, then returns JSON. Nothing calls it from the frontend.
- `handleMayorFileRead` at `mayor_dashboard.go:96-127` works and returns `{name, content}` JSON. The Memory tab just never fetches from it.
- SSE handler targets `#mayor-builds-tab` and `#mayor-activity-tab` but those IDs don't exist anywhere in the DOM — the templ components `BuildsTab` and `ActivityTab` render their content inside anonymous divs.
- The world overlay SSE at `events.go:327-341` shows the correct pattern for handling `EventMayorMessage` — the mayor dashboard SSE handler just needs to follow the same pattern.
- `POST /api/assets/upload` at `server.go:203` exists and handles multipart upload, but there's no HTML form anywhere.

## Desired End State

After this plan is complete:
1. Clicking "Load" on a checkpoint in the tree navigates the user to that specific checkpoint (not just the world's default)
2. The mayor dashboard Memory tab loads and displays SOUL.md and MEMORY.md content
3. The mayor dashboard Builds and Activity tabs live-update via SSE
4. The mayor dashboard Messages tab live-updates when new Discord messages arrive
5. An "Upload" button in the world overlay opens a file upload form that POSTs to `/api/assets/upload`
6. After uploading an asset, the game iframe reloads to pick up the new file without a full WASM re-download

### Verification
- Navigate to a world, open checkpoint tree, click "Load" on a non-current checkpoint → URL changes and iframe shows the correct checkpoint's WASM build
- Navigate to `/mayor/:worldID`, click Memory tab → see SOUL.md and MEMORY.md content (or "File not found" messages)
- On the mayor dashboard, trigger a build → Builds and Activity tabs update in real-time without page refresh
- Send a message in the world's Discord channel → Messages tab shows it in real-time
- In the world overlay, click Upload → select a file → file appears in shared assets → click Reload → game picks up new asset

## What We're NOT Doing

- **P2 items** from the audit (empty-room styling, generic error handling, asset picker UI, checkpoint diff view, build log viewer)
- **Iframe hot-reload optimization** — the placement system (`rooms.go`) triggers `f.src=f.src` after each asset placement, which reloads the WASM iframe. A lighter approach (postMessage → Bevy system to re-fetch room JSON) would require WASM changes and is out of scope. The browser caches the WASM binary, so the actual overhead is Bevy init (~2-3s), not the ~5MB download.
- **Asset management UI** (browsing, deleting, organizing assets) — just the upload form
- **Mayor dashboard Memory tab editing** — the save endpoint exists (`PUT /mayor/:worldID/file`) but wiring up an edit UI is out of scope

## Implementation Approach

Five targeted fixes across 5 files. No new Go packages, no schema changes, no WASM rebuilds. All changes are in the harness (Go server + templ templates + static JS).

---

## Phase 1: Fix Checkpoint Loading

### Overview
Make `loadCheckpoint()` actually navigate to the specified checkpoint by calling the existing `handleCheckpointView` endpoint first.

### Changes Required

#### 1. `harness/static/game-loader.js`
**File**: `harness/static/game-loader.js:38-41`
**Changes**: Update `loadCheckpoint` to call `GET /world/:worldID/checkpoint/:cpID` (which updates `user_positions` via `SetUserPosition`), then navigate to the world page.

```javascript
// loadCheckpoint navigates to a checkpoint's world page.
// First updates the user's position via the checkpoint API, then navigates.
window.loadCheckpoint = function(worldID, checkpointID) {
    fetch('/world/' + worldID + '/checkpoint/' + checkpointID)
        .then(function() {
            window.location.href = '/world/' + worldID;
        })
        .catch(function() {
            // Fallback: navigate anyway (server will use whatever position is stored)
            window.location.href = '/world/' + worldID;
        });
};
```

### Success Criteria

#### Automated Verification:
- [ ] Build succeeds: `cd harness && go build -o /dev/null .`
- [ ] Static file has no syntax errors: open in browser console

#### Manual Verification:
- [ ] Open a world with multiple ready checkpoints
- [ ] Open checkpoint tree, click "Load" on a non-current checkpoint
- [ ] Page reloads showing the correct checkpoint's content
- [ ] URL is `/world/{worldID}` (same as before, but user_positions updated)

---

## Phase 2: Fix Mayor Dashboard SSE Targets + Memory Tab

### Overview
Fix the three mayor dashboard issues: SSE targeting non-existent DOM IDs, Memory tab never loading, and Messages tab not live-updating.

### Changes Required

#### 1. Add IDs to BuildsTab and ActivityTab containers
**File**: `harness/views/mayor/dashboard.templ:72-76`
**Changes**: Add `id="mayor-builds-tab"` and `id="mayor-activity-tab"` to the wrapper divs so SSE patches have targets.

Current:
```go
<div data-show="$active_tab === 'builds'" style="display: none;">
    @BuildsTab(data.Builds)
</div>
<div data-show="$active_tab === 'activity'" style="display: none;">
    @ActivityTab(data.Activity)
</div>
```

New:
```go
<div id="mayor-builds-tab" data-show="$active_tab === 'builds'" style="display: none;">
    @BuildsTab(data.Builds)
</div>
<div id="mayor-activity-tab" data-show="$active_tab === 'activity'" style="display: none;">
    @ActivityTab(data.Activity)
</div>
```

#### 2. Add ID to Messages tab container + live-update via SSE
**File**: `harness/views/mayor/dashboard.templ:78-80`
**Changes**: Add `id="mayor-messages-tab"` to the Messages wrapper div.

Current:
```go
<div data-show="$active_tab === 'messages'" style="display: none;">
    @MessagesTab(data.Messages)
</div>
```

New:
```go
<div id="mayor-messages-tab" data-show="$active_tab === 'messages'" style="display: none;">
    @MessagesTab(data.Messages)
</div>
```

#### 3. Update SSE handler to also refresh Messages tab
**File**: `harness/internal/server/mayor_dashboard.go:56-92`
**Changes**: In the SSE handler, also fetch and patch messages. Handle `EventMayorMessage` events specifically to refresh the messages tab.

```go
func (s *Server) handleMayorDashboardSSE(c echo.Context) error {
	worldID := c.Param("worldID")
	r := c.Request()
	sse := datastar.NewSSE(c.Response().Writer, r)

	worldCh := s.EventBus.Subscribe(worldID)
	defer s.EventBus.Unsubscribe(worldID, worldCh)

	for {
		select {
		case <-worldCh:
			ctx := r.Context()
			builds, _ := s.DB.GetMayorBuilds(ctx, sqlc.GetMayorBuildsParams{
				WorldID: worldID, Limit: defaultQueryLimit,
			})
			activity, _ := s.DB.GetMayorActivity(ctx, sqlc.GetMayorActivityParams{
				WorldID: worldID, Limit: defaultQueryLimit,
			})
			messages, _ := s.DB.GetMayorMessages(ctx, worldID)
			if err := sse.PatchElementTempl(
				mayorview.BuildsTab(builds),
				datastar.WithSelectorID("mayor-builds-tab"),
			); err != nil {
				return nil
			}
			if err := sse.PatchElementTempl(
				mayorview.ActivityTab(activity),
				datastar.WithSelectorID("mayor-activity-tab"),
			); err != nil {
				return nil
			}
			if err := sse.PatchElementTempl(
				mayorview.MessagesTab(messages),
				datastar.WithSelectorID("mayor-messages-tab"),
			); err != nil {
				return nil
			}
		case <-r.Context().Done():
			return nil
		}
	}
}
```

#### 4. Fix Memory tab to fetch workspace files on load
**File**: `harness/views/mayor/dashboard.templ:248-269`
**Changes**: Replace the static "Loading..." divs with a `data-init` that fetches the files via JavaScript, using the existing `GET /mayor/:worldID/file?name=SOUL.md` endpoint.

```go
templ MemoryTab(w sqlc.World) {
	<div class="space-y-4">
		<div>
			<h3 class="text-sm font-semibold mb-2">SOUL.md</h3>
			<div
				id="mayor-soul-content"
				class="rounded border border-border bg-card p-4 text-sm whitespace-pre-wrap font-mono max-h-96 overflow-y-auto"
			>
				Loading...
			</div>
		</div>
		<div>
			<h3 class="text-sm font-semibold mb-2">MEMORY.md</h3>
			<div
				id="mayor-memory-content"
				class="rounded border border-border bg-card p-4 text-sm whitespace-pre-wrap font-mono max-h-96 overflow-y-auto"
			>
				Loading...
			</div>
		</div>
	</div>
	<script>
		(function() {
			var worldID = document.querySelector('[data-signals]')?.dataset?.signals;
			// Extract worldID from the current URL: /mayor/{worldID}
			var parts = window.location.pathname.split('/');
			var wid = parts[2]; // /mayor/{worldID}
			if (!wid) return;

			function loadFile(name, targetID) {
				fetch('/mayor/' + wid + '/file?name=' + name)
					.then(function(r) { return r.json(); })
					.then(function(data) {
						var el = document.getElementById(targetID);
						if (el) el.textContent = data.content || '(empty)';
					})
					.catch(function() {
						var el = document.getElementById(targetID);
						if (el) el.textContent = '(file not found)';
					});
			}
			loadFile('SOUL.md', 'mayor-soul-content');
			loadFile('MEMORY.md', 'mayor-memory-content');
		})();
	</script>
}
```

### Success Criteria

#### Automated Verification:
- [ ] templ generates cleanly: `cd harness && templ generate`
- [ ] Build succeeds: `cd harness && go build -o /dev/null .`

#### Manual Verification:
- [ ] Navigate to `/mayor/{worldID}` — Overview tab renders
- [ ] Click "Builds" tab — shows builds list
- [ ] Click "Activity" tab — shows activity list
- [ ] Click "Messages" tab — shows messages
- [ ] Click "Memory" tab — SOUL.md and MEMORY.md content loads (or shows "file not found" for worlds without mayor workspaces)
- [ ] Trigger a build → Builds and Activity tabs update without refresh
- [ ] Send a Discord message in the world's channel → Messages tab updates without refresh

---

## Phase 3: Add Upload UI to World Overlay

### Overview
Add an "Upload" button to the world overlay top bar that opens a file upload form. Files are uploaded to `POST /api/assets/upload` which stores them in `data/shared-assets/`.

### Changes Required

#### 1. Add Upload button and form to overlay top bar
**File**: `harness/views/world/overlay.templ:42-59`
**Changes**: Add an "Upload" button next to the existing Reload button, and a hidden upload form that appears when clicked.

```go
templ OverlayTopBar(w sqlc.World) {
	<div class="bg-[rgba(17,17,17,0.92)] backdrop-blur-lg px-2 md:px-4 py-2 flex flex-col gap-1 border-b border-border">
		<div class="flex items-center justify-between">
			<a href="/" class="font-bold text-sm hover:text-primary transition-colors">Creative Mode</a>
			<div class="flex items-center gap-1.5">
				@button.Button(button.ButtonArgs{Size: "sm", Variant: "outline", Attributes: templ.Attributes{"data-on:click": OE.ToggleTree()}}) {
					Tree
				}
				@button.Button(button.ButtonArgs{Size: "sm", Variant: "outline", Attributes: templ.Attributes{"data-on:click": "$show_upload = !$show_upload"}}) {
					Upload
				}
				@button.Button(button.ButtonArgs{Size: "sm", Variant: "outline", Attributes: templ.Attributes{"data-on:click": "var f=document.getElementById('game-frame');if(f){f.src=f.src;}"}}) {
					Reload
				}
				@button.Button(button.ButtonArgs{Size: "sm", Variant: "ghost", Attributes: templ.Attributes{"data-on:click": OE.Minimize()}}) {
					—
				}
			</div>
		</div>
		<div class="text-muted-foreground text-[13px]">{ w.Name }</div>
		<div data-show="$show_upload" style="display: none;" class="flex items-center gap-2 py-1">
			<form
				id="upload-form"
				enctype="multipart/form-data"
				class="flex items-center gap-2 flex-1"
			>
				<input type="file" name="file" required class="text-sm text-muted-foreground file:mr-2 file:py-1 file:px-2 file:rounded file:border-0 file:text-sm file:bg-muted file:text-foreground flex-1"/>
				<input type="hidden" name="folder" value="rooms"/>
				@button.Button(button.ButtonArgs{Size: "sm", Variant: "default", Attributes: templ.Attributes{"type": "button", "data-on:click": "window.__uploadAsset()"}}) {
					Send
				}
			</form>
			<span id="upload-status" class="text-xs text-muted-foreground"></span>
		</div>
	</div>
}
```

#### 2. Add upload JS function and `show_upload` signal
**File**: `harness/static/game-loader.js` (append)
**Changes**: Add a `__uploadAsset` function that reads the form and POSTs via fetch.

```javascript
// Upload asset from overlay form.
window.__uploadAsset = function() {
    var form = document.getElementById('upload-form');
    if (!form) return;
    var status = document.getElementById('upload-status');
    var data = new FormData(form);

    if (status) status.textContent = 'Uploading...';

    fetch('/api/assets/upload', { method: 'POST', body: data })
        .then(function(r) {
            if (!r.ok) throw new Error('Upload failed: ' + r.status);
            return r.json();
        })
        .then(function(result) {
            if (status) status.textContent = 'Uploaded: ' + (result.path || 'ok');
            form.reset();
        })
        .catch(function(err) {
            if (status) status.textContent = err.message;
        });
};
```

#### 3. Add `show_upload` signal to overlay signals
**File**: `harness/views/world/signals.go:13-33`
**Changes**: Add `ShowUpload bool` field to `OverlaySignals` struct. Default is `false` (zero value), so no change needed in `DefaultOverlaySignals()`.

```go
ShowUpload          bool    `json:"show_upload"`           //nolint:tagliatelle // Datastar signal names use snake_case
```

### Success Criteria

#### Automated Verification:
- [ ] templ generates cleanly: `cd harness && templ generate`
- [ ] Build succeeds: `cd harness && go build -o /dev/null .`

#### Manual Verification:
- [ ] Open a world, expand overlay
- [ ] Click "Upload" — file input form appears
- [ ] Select a file, click "Send" — status shows "Uploaded: rooms/filename.png"
- [ ] Click "Reload" — game iframe reloads and can reference the new asset
- [ ] Upload without selecting a file — form validation prevents submission

---

## Testing Strategy

### Manual Testing Steps
1. **Checkpoint loading**: Create a world with 2+ checkpoints. Click "Load" on a non-current checkpoint. Verify the iframe shows the correct checkpoint's WASM build.
2. **Mayor dashboard Memory tab**: Navigate to `/mayor/{worldID}` for a world that has a mayor provisioned. Click Memory tab. Verify SOUL.md and MEMORY.md content displays.
3. **Mayor dashboard SSE**: Open the mayor dashboard. Trigger a build (via Discord or API). Verify Builds and Activity tabs update in real-time.
4. **Mayor messages SSE**: Open the mayor dashboard Messages tab. Send a message in the world's Discord channel. Verify the message appears without refresh.
5. **Upload**: Open a world overlay. Upload a PNG file. Verify the status shows success. Click Reload. Verify the asset is accessible in the game.

## Performance Considerations

- The mayor dashboard SSE handler now fetches messages on every world event (not just message events). This is fine for demo-level traffic but could be optimized later to only refetch messages on `EventMayorMessage` events.
- The Memory tab uses a one-shot JS fetch on render, not SSE. This means workspace file changes won't live-update. This is acceptable — the files change infrequently and the user can refresh the page.

## References

- Feature audit: `thoughts/CoreyCole/research/2026-02-16_08-35-03_2d-demo-feature-audit.md`
- Launch readiness plan (non-overlapping): `thoughts/CoreyCole/plans/2026-02-16_09-12-34_launch-readiness-fixes.md`
- Checkpoint view endpoint: `harness/internal/server/server.go:392-413`
- Mayor file read endpoint: `harness/internal/server/mayor_dashboard.go:96-127`
- World overlay SSE mayor message pattern: `harness/internal/server/events.go:327-341`
