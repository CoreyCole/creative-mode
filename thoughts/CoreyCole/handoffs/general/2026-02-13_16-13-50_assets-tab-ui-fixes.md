---
date: 2026-02-13T16:13:50-08:00
researcher: CoreyCole
git_commit: 0e53c591490942b92581db8b536e627e099ed015
branch: main
repository: creative-mode
topic: "Assets Tab UI Fixes — Layout Order + File Tree"
tags: [implementation, imagegen, asset-tree, datastar, templ]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Assets Tab UI Fixes + Next Steps (Garden Image Placement)

## Task(s)

### Completed: Assets Tab UI Fixes
Three issues were fixed in the Assets tab:

1. **Layout order** — The input bar was a separate bottom bar in `chat.templ`. Moved it inline into `imagegen.templ` so the panel flows: aspect ratio → transparent bg toggle → [input] [Generate] → result area → collapsible file tree.

2. **Redundant text** — Removed `@ImageGenIdle()` call which duplicated the input placeholder text. Replaced with an empty `#image-gen-content` div that SSE fragments fill during generate/save flows.

3. **Flat list → collapsible folder tree** — Rewrote `asset_tree.templ` to show a `▼ generated/ (N)` folder node that collapses/expands via `$assets_gen_open` signal. Files are indented under the folder.

All changes verified: build passes, tree loads on tab switch, tree refreshes after save with correct count.

### Planned: Use Generated Images in Garden Room
The user wants to place the generated images (mushroom, willow tree, etc.) into the 2D garden room as sprites/hotspots. The generated images **do have proper alpha transparency** — verified by parsing PNG headers and counting transparent pixels (mushroom: 99.1%, willow: 97.8%, cat: 100%). The chromakey green removal post-processing in `gemini.go` is working correctly.

## Critical References
- `harness/CLAUDE.md` — Full server architecture, Datastar patterns, SSE patterns
- `templates/2d/CLAUDE.md` — 2D Bevy room-based template docs (for understanding how rooms/hotspots/sprites work)

## Recent changes

- `harness/views/chat/chat.templ:31-33` — Removed the separate bottom bar div for assets tab
- `harness/views/imagegen/imagegen.templ:36-41` — Replaced `@ImageGenIdle()` with inline input+generate form and empty `#image-gen-content` div
- `harness/views/imagegen/asset_tree.templ` — Full rewrite: collapsible `generated/` folder node with toggle via `$assets_gen_open` signal, files indented under folder
- `harness/views/world/signals.go:20,33` — Added `AssetsGenOpen bool` signal (default `true`)

## Learnings

- **Chromakey removal works correctly**: `gemini.go:216-266` implements green background removal. It requests `#00FF00` chromakey background via prompt suffix (`gemini.go:28-31`), then detects green pixels in HSV space and sets alpha to 0. All three saved images have proper RGBA with majority transparent pixels.
- **HSV thresholds**: Green detection uses hue center 120deg +-30deg, saturation min 0.50, value min 0.30 (`gemini.go:34-37`). Dilate radius is 1 pixel for edge cleanup.
- **Generated assets are stored at**: `{DataDir}/shared-assets/generated/` inside Docker → `/app/data/shared-assets/generated/`. Served at `/assets/generated/{filename}`.
- **Save handler refreshes tree**: `imagegen.go:169-175` sends both `ImageGenSaved` and `AssetTree` SSE patches after save, so the tree updates inline without needing a separate fetch.
- **Room structure**: The garden room is loaded from `/assets/rooms/lobby.room.json` (or similar). Rooms have hotspots that can reference sprite images. Need to investigate `templates/2d/` to understand how to add new hotspots/sprites programmatically.

## Artifacts
- `harness/views/imagegen/imagegen.templ` — Restructured panel layout
- `harness/views/imagegen/asset_tree.templ` — New collapsible tree component
- `harness/views/chat/chat.templ` — Simplified (removed assets bottom bar)
- `harness/views/world/signals.go` — Added `assets_gen_open` signal
- `harness/views/imagegen/types.go` — `AssetFileInfo` struct (unchanged, for reference)
- `harness/views/imagegen/expressions.go` — Image gen expressions (unchanged, for reference)
- `harness/internal/gemini/gemini.go` — Chromakey removal implementation (unchanged, for reference)
- `harness/internal/server/imagegen.go` — Save/tree handlers (unchanged, for reference)

## Action Items & Next Steps

1. **Investigate 2D room/hotspot system** — Read `templates/2d/CLAUDE.md` and the room JSON format to understand how sprites and hotspots are defined. Check `data/shared-assets/rooms/` for existing room JSON files.
2. **Build workflow to place generated images in rooms** — The user wants to take a generated image (e.g., mushroom with transparent background) and place it as a sprite/hotspot in the garden room. This likely involves:
   - Adding the image path to a room's JSON definition
   - Positioning it (x, y coordinates, scale)
   - Making it a clickable hotspot or just a decorative sprite
3. **Consider a "Place in Room" action** — After saving an image, add a UI flow to place it directly into the current room (select position, scale, optionally make it a hotspot with an ID).

## Other Notes

- The garden room's dark green background visible in the game canvas is the room's CSS/canvas background color, NOT residual green from chromakey. The transparency is working.
- Existing room data is in `/app/data/shared-assets/rooms/` inside Docker. The garden currently has an "Old Tree" hotspot and a "Lobby" navigation hotspot.
- The 2D template uses Bevy for rendering — sprites loaded from room JSON are rendered as Bevy entities with transforms. Understanding the Bevy-side room loader will be important for placing new sprites.
- Docker container working dir is `/app/harness`. DataDir resolves to `/app/data`. Shared assets at `/app/data/shared-assets/`.
- All changes are unstaged — not committed yet. Run `git diff` to see the full diff.
