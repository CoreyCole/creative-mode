---
date: 2026-02-13T17:55:08-08:00
researcher: CoreyCole
git_commit: 14760be7ee6bcfcd1f035efdcaeac538d586b5d1
branch: main
repository: creative-mode
topic: "Responsive Layout — Mobile + Desktop Support for Harness UI and Bevy Game"
tags: [investigation, responsive, layout, mobile, bevy, harness, css]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Responsive Layout for Mobile + Desktop

## Task(s)

### Completed: Assets Tab UI Fixes + Room Image Placement
All committed in `14760be`. The Assets tab now has inline input, collapsible file tree with Place buttons, and a full placement workflow (rooms → hotspots → place as background/existing hotspot/new hotspot with aspect-ratio-preserving sizing).

### Planned: Responsive Layout Investigation
The user wants to support both mobile and desktop screen sizes for:
1. **The harness UI** (overlay panel with chat/assets tabs, game controls)
2. **The Bevy game canvas** (both 2D and 3D templates)

**Known issue**: The 2D game has its height restricted/not filling the viewport, while the 3D template properly takes up the entire view width/height.

## Critical References
- `harness/CLAUDE.md` — Full server architecture, Datastar patterns, overlay structure
- `templates/2d/CLAUDE.md` — 2D Bevy room-based template (room coordinate system is 1280x720)
- `templates/3d/CLAUDE.md` — 3D Bevy/Lightyear template

## Recent changes
- `harness/internal/server/rooms.go` — New file: room listing, hotspot listing, and 3 placement endpoints (background, existing hotspot, new hotspot)
- `harness/views/imagegen/placement.templ` — New file: placement wizard UI (room list → target list → new hotspot form → success)
- `harness/views/imagegen/asset_tree.templ` — Added "Place" button on each asset in the file tree
- `harness/views/imagegen/types.go` — Added `RoomInfo`, `HotspotInfo`, `ImageDimensions` types
- `harness/views/world/signals.go` — Added placement signals (`place_asset_path`, `place_scale`, `place_x`, `place_y`, `place_label`)
- `harness/internal/server/server.go:170-174` — Registered 5 new room placement routes
- `harness/views/chat/chat.templ` — Simplified layout (removed separate assets bottom bar)
- `harness/views/imagegen/imagegen.templ` — Restructured panel with inline input bar and empty `#image-gen-content` div
- `harness/views/chat/expressions.go:36-41` — `SelectAssetsTab()` triggers `@get('/api/assets/tree')`

## Learnings
- **Room coordinate system is fixed at 1280x720**: The 2D template uses a fixed logical resolution. Bevy's `Window` is configured with `resolution: WindowResolution::new(1280.0, 720.0)` and `fit_canvas_to_parent: true`. Room hotspot positions are defined in this coordinate space. This will be important for responsive — the game canvas can scale but coordinates remain in 1280x720 space.
- **3D template uses `fit_canvas_to_parent: true`** as well, and does not hardcode a resolution — it fills its container. The difference likely lies in the iframe/container CSS, not Bevy config.
- **Overlay is a fixed-width 320px panel**: `harness/views/chat/chat.templ:10` uses `w-80` (320px). This won't work on mobile — it's wider than many phone screens or takes too much space.
- **Game iframe is loaded inside a flex container**: `harness/views/world/overlay.templ` renders the game iframe and overlay side by side. The iframe has `flex-1` to fill remaining space after the 320px panel.
- **Trunk's `index.html` sets canvas sizing**: Each template's `index.html` has CSS for `#bevy-canvas` — this is where the 2D height restriction likely originates.
- **Chromakey removal + image placement both work correctly** — verified by testing the full generate → save → place → reload flow.

## Artifacts
- `harness/internal/server/rooms.go` — Room API handlers (list, hotspots, place background/hotspot/new)
- `harness/views/imagegen/placement.templ` — Placement wizard UI components
- `harness/views/imagegen/asset_tree.templ` — Asset tree with Place buttons
- `harness/views/imagegen/types.go` — Room/hotspot/image dimension types
- `harness/views/world/signals.go` — Overlay signals including placement signals

## Action Items & Next Steps

1. **Investigate the 2D height restriction**: Compare the 2D and 3D template's `index.html` and CSS for `#bevy-canvas`. The 3D template fills the viewport correctly; the 2D does not. Check:
   - `templates/2d/index.html` — canvas CSS rules
   - `templates/3d/index.html` — canvas CSS rules (working reference)
   - Bevy `WindowPlugin` config in both templates' `lib.rs`

2. **Investigate harness overlay responsiveness**: The overlay panel is 320px fixed width (`w-80`). For mobile:
   - Consider making the panel full-width on small screens, toggled as a drawer/sheet
   - The game iframe would need to be full-screen underneath
   - The toggle button (CM button) already exists — could be the mobile drawer trigger
   - Check `harness/views/world/overlay.templ` for the current layout structure

3. **Consider Bevy viewport scaling on mobile**: Both templates use `fit_canvas_to_parent: true`, so the canvas should scale to its container. The key question is whether the CSS container is properly sized on mobile. Also consider touch input — the 2D template uses mouse click detection (`interaction.rs`) which may need touch event support.

4. **Test on actual mobile viewport**: Use `playwright-cli` with a mobile viewport size or Chrome DevTools device emulation to see current behavior before making changes.

## Other Notes
- The 2D room coordinate space (1280x720) is a logical coordinate system — Bevy handles scaling from logical to physical pixels. This means room placement positions should work at any screen size as long as the canvas scales proportionally.
- The harness uses Tailwind CSS — responsive utilities (`sm:`, `md:`, `lg:`) are available for breakpoint-based styling.
- Datastar's `data-show`/`data-class` can be driven by signals, so a responsive approach could use a `mobile` signal set by JavaScript `matchMedia` to toggle between mobile/desktop layouts.
- Docker container must be restarted (`just down && just up`) after changing template `index.html` or `Cargo.toml` files since Trunk's file watcher doesn't see host filesystem events through Docker bind mounts on macOS.
