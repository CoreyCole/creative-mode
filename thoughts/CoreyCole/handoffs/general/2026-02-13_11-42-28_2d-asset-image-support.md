---
date: 2026-02-13T11:42:28-08:00
researcher: CoreyCole
git_commit: 4bfe1cc095dec1a0f439348f3c4e11d961054778
branch: main
repository: creative-mode
topic: "2D Asset & Image Support Implementation"
tags: [implementation, 2d-template, asset-pipeline, image-support, room-loading]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: 2D Asset & Image Support

## Task(s)

**Implementation of the 2D asset pipeline, runtime room loading, image fields, upload endpoint, and reload trigger** — based on the plan at `thoughts/CoreyCole/plans/2026-02-13_10-12-01_2d-asset-image-support.md`.

All 3 phases are **completed** and verified (clippy clean, go build clean, lint clean):

1. **Phase 1: Wire up 2D asset pipeline** — COMPLETED
2. **Phase 2: Runtime room loading + image fields** — COMPLETED
3. **Phase 3: Upload endpoint + reload trigger** — COMPLETED

**Next task (not started):** E2E testing with playwright-cli to verify:
- Asset pipeline works (rooms load via HTTP, not compiled in)
- Image upload endpoint works with folder organization
- Uploaded images render in rooms (background_image and hotspot image fields)
- Reload button triggers room refresh without WASM rebuild

## Critical References

- Plan document: `thoughts/CoreyCole/plans/2026-02-13_10-12-01_2d-asset-image-support.md`
- 2D template CLAUDE.md (updated): `templates/2d/CLAUDE.md`
- Bevy custom asset loader reference: `context/bevy/examples/asset/custom_asset.rs`

## Recent changes

- `templates/2d/Trunk.toml:9-10` — Added `[[proxy]]` block forwarding `/assets/` to harness `:8080`
- `templates/2d/Cargo.toml:13` — Added `thiserror = "2"` dependency
- `templates/2d/src/lib.rs:27` — Set `AssetPlugin { file_path: "/assets" }`
- `templates/2d/src/room.rs` — **Full rewrite**: `RoomAsset` (Bevy Asset), `RoomAssetLoader` (custom `.room.json` loader), async `RoomLoadState` state machine, `HasImage` marker component, `check_reload_request` WASM system, `spawn_room()` with image support
- `templates/2d/src/interaction.rs:18-60` — Split `hotspot_hover` into two queries: `Without<HasImage>` (opacity) and `With<HasImage>` (brighten tint)
- `templates/2d/rooms/lobby.json` → `lobby.room.json`, `garden.json` → `garden.room.json` — Renamed for custom loader extension matching
- `templates/2d/index.html:29-33` — Added `reload-room` message handler in the postMessage listener
- `harness/main.go:50-75` — Room JSON seeding from `templates/2d/rooms/` to `data/shared-assets/rooms/`
- `harness/internal/server/assets.go` — **New file**: `handleAssetUpload` multipart handler (MIME validation, path traversal protection, duplicate rejection)
- `harness/internal/server/server.go:157` — Added `POST /api/assets/upload` route under approved group
- `harness/views/world/overlay.templ:46-48` — Added "Reload" button next to "Tree" that sends `postMessage({type:'reload-room'})` to game iframe
- `templates/2d/CLAUDE.md` — Updated room schema docs, adding new room instructions, image documentation

## Learnings

- **Custom asset loader extension**: Using `.room.json` as the extension avoids conflicts with Bevy's default JSON handling. Bevy uses type inference from `Handle<T>` to dispatch to the correct loader, but a unique extension is cleaner.
- **gosec nolint placement**: The `//nolint:gosec` directive must be on the same line as the `os.ReadFile()` call. When golines reformats, it can move the directive to the closing paren line where gosec doesn't see it. Keep the comment short enough to avoid golines wrapping: `//nolint:gosec // trusted path`.
- **golines max line length**: The harness linter enforces line length. When adding nolint directives, use short explanations to stay within limits.
- **Room seeding**: The harness seeds room files on startup but skips existing files. This means user edits to `data/shared-assets/rooms/` are preserved across restarts.

## Artifacts

- `templates/2d/Trunk.toml` — Trunk proxy config
- `templates/2d/Cargo.toml` — Added thiserror dep
- `templates/2d/src/lib.rs` — AssetPlugin file_path
- `templates/2d/src/room.rs` — Complete rewrite (RoomAsset, loader, state machine, image support)
- `templates/2d/src/interaction.rs` — Split hover for image vs color hotspots
- `templates/2d/rooms/lobby.room.json` — Renamed from .json
- `templates/2d/rooms/garden.room.json` — Renamed from .json
- `templates/2d/index.html` — reload-room handler
- `harness/main.go` — Room seeding logic
- `harness/internal/server/assets.go` — NEW: upload handler
- `harness/internal/server/server.go` — Upload route
- `harness/views/world/overlay.templ` — Reload button
- `templates/2d/CLAUDE.md` — Updated docs

## Action Items & Next Steps

1. **Start the harness**: `just live` from `harness/` to get Docker running
2. **Open a 2D world** with playwright-cli: `playwright-cli open http://localhost:8080 --headed --persistent`
3. **Verify asset pipeline**: Check browser network tab for requests to `/assets/rooms/lobby.room.json` (not compiled in). Use `playwright-cli network` or browser devtools.
4. **Verify rooms render**: Existing solid-color rooms should look identical to before (lobby + garden).
5. **Test image upload**:
   ```bash
   # Get a test image (or use any local PNG)
   # Upload to rooms/ folder:
   COOKIE=$(playwright-cli cookie-get session)
   curl -F "file=@test.png" -F "folder=rooms" -b "session=$COOKIE" http://localhost:8080/api/assets/upload
   ```
6. **Test folder organization**: Upload to nested folders, verify directory creation works.
7. **Test path traversal rejection**: `curl -F "file=@test.png" -F "folder=../etc" ...` should return 400.
8. **Test duplicate rejection**: Re-upload same file → should return 409.
9. **Test image in room**: Edit a room JSON in `data/shared-assets/rooms/` to add `"background_image"` or `"image"` on a hotspot referencing the uploaded file. Click "Reload" in the overlay.
10. **Verify reload button**: Confirm the "Reload" button in the overlay triggers room re-fetch without WASM rebuild.
11. **Verify image hotspot hover**: Image hotspots should brighten (not change opacity) on hover.

## Other Notes

- The upload endpoint is at `POST /api/assets/upload` under the `approved` auth group — requires a valid session cookie.
- Accepted MIME types: `image/png`, `image/jpeg`, `image/webp`, `image/gif`.
- Asset paths in room JSON are relative to `/assets/` (which maps to `data/shared-assets/`). So an uploaded file at `rooms/test.png` is referenced as `"rooms/test.png"` in JSON.
- The `templ generate` step must run before `go build` since the overlay button was added to a `.templ` file. `just generate` handles this.
- Changes are uncommitted — all files are unstaged.
