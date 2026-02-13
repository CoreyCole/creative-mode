---
date: 2026-02-13T14:12:13-08:00
researcher: CoreyCole
git_commit: 8910e0342a92e75eaf295a8919382f344758eb24
branch: main
repository: creative-mode
topic: "Image Gen E2E Testing & Configurable Chromakey Pipeline"
tags: [implementation, testing, image-generation, gemini, chromakey, datastar]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Image Gen E2E Testing & Configurable Chromakey

## Task(s)

### Completed: Datastar `data-bind` Signal Binding Bug Fix
All `data-bind-{signal}` attributes (dash syntax) were broken in Datastar v1.0.0-RC.6. The HTML dataset API converts dashes to camelCase (`data-bind-image_prompt` → `bindImage_prompt`), which mangles the plugin lookup so the `bind` plugin is never found. Fixed by changing to colon syntax (`data-bind:image_prompt`), matching the pattern used for `data-on:click`. This affected **all three inputs**: chat, image gen prompt, and build prompt.

### Completed: Gemini Image Generation Feature (commits from prior session)
Full implementation of AI image generation via Google Gemini API as a new "Image" tab in the overlay sidebar. Includes chromakey green background removal pipeline.

### Completed: Datastar Best Practices Documentation
Added comprehensive "Tao of Datastar" best practices section to `harness/CLAUDE.md`.

### Planned: E2E Testing of Image Gen Pipeline
Need to test the full flow: type prompt → generate → preview → download → save. Requires a billing-enabled Gemini API key (free tier has `limit: 0` for image generation).

### Planned: Configurable Chromakey (Background vs Foreground Mode)
Currently `chromakeySuffix` is **always** appended to every prompt (`gemini.go:28-31`). This forces a green background for transparency removal. But if the user is generating a **background image** (e.g., a room background), they don't want chromakey — they want the full image as-is. Need to add a toggle so the user can choose between:
- **Foreground mode** (default): appends chromakey green instruction, runs background removal → outputs PNG with transparency
- **Background mode**: sends prompt as-is, returns the raw image without chromakey processing

## Critical References
- `harness/CLAUDE.md` — Datastar Best Practices section (new, at bottom of file)
- `harness/internal/gemini/gemini.go` — Gemini client with chromakey pipeline
- `harness/internal/server/imagegen.go` — HTTP handlers for generate/preview/save

## Recent changes

### Commit `e0a6c8d`: Fix data-bind signal binding bug
- `harness/views/imagegen/imagegen.templ:80` — `data-bind-image_prompt` → `data-bind:image_prompt`
- `harness/views/chat/chat_input.templ:14` — `data-bind-chat_text` → `data-bind:chat_text`
- `harness/views/world/overlay.templ:66` — `data-bind-prompt_text` → `data-bind:prompt_text`
- `harness/CLAUDE.md:207-208` — Updated attribute syntax warning to cover all plugins, not just `data-on`
- `harness/CLAUDE.md:237` — Fixed `data-bind` example in attribute table
- `harness/CLAUDE.md:414-522` — Added Datastar Best Practices section

### Commit `4b68d2e`: Add Gemini image generation
- `harness/internal/gemini/gemini.go` — Full Gemini client with chromakey pipeline (new file)
- `harness/internal/server/imagegen.go` — HTTP handlers: generate, preview, save (new file)
- `harness/internal/server/assets.go` — Asset upload handler (new file)
- `harness/internal/server/server.go:22,49,160-165` — Gemini import, field, route registration
- `harness/main.go:19,226-232,240` — Gemini client initialization
- `harness/views/chat/chat.templ` — Image tab button, imagegen panel + input bar
- `harness/views/imagegen/imagegen.templ` — Image gen panel UI (new file)
- `harness/views/imagegen/expressions.go` — Datastar expression helpers (new file)
- `harness/views/world/signals.go:17-23,32-33` — 7 new image gen signals + defaults
- `harness/docker-compose.yml:21` — GEMINI_API_KEY env passthrough

### Commit `8910e03`: Runtime room loading & overlay toggle
- `templates/2d/src/room.rs` — Rooms load via HTTP asset loader, not compiled-in
- `templates/2d/index.html` — Backtick forwarding, reload-room message bridge
- `templates/3d/client/src/main.rs` — Tab → Backquote for overlay toggle

## Learnings

### Datastar `data-bind` must use colon syntax
`data-bind-signal_name` (dash) is broken because HTML's dataset API converts `data-bind-foo` → `bindFoo` (camelCase), so Datastar's plugin lookup searches for `"bind-foo"` instead of `"bind"`. The colon syntax `data-bind:signal_name` works because colons are preserved through dataset conversion. This is the same issue documented for `data-on:click` vs `data-on-click`. See `harness/CLAUDE.md:207-208` for the updated warning.

### Chromakey pipeline always runs
`gemini.go:109` always appends `chromakeySuffix` to the prompt. `gemini.go:132` always calls `removeGreenBackground()`. There's no way to opt out. For background images, this would destructively remove green areas that are part of the intended image content.

### Image gen handlers use MarshalAndPatchSignals anti-pattern
`imagegen.go` uses `sse.MarshalAndPatchSignals()` for all server responses (status updates, preview URLs, error messages, saved paths). Per the Datastar best practices in CLAUDE.md, this should be refactored to use `sse.PatchElementTempl()` for rendering server-driven HTML fragments instead. Only input clearing (`image_prompt: ""`) should use signal patching.

### Gemini API quirks
- Model: `gemini-2.5-flash-image` (resolves to preview variant internally)
- Free tier API keys may have `limit: 0` for image generation — billing must be enabled
- Sometimes returns JPEG bytes despite claiming PNG MIME type — `decodeImage()` at `gemini.go:244` handles this
- No native transparency support — chromakey is the standard workaround

## Artifacts

- `harness/internal/gemini/gemini.go` — Gemini client + chromakey pipeline
- `harness/internal/server/imagegen.go` — HTTP handlers (generate/preview/save)
- `harness/internal/server/assets.go` — Asset upload handler
- `harness/views/imagegen/imagegen.templ` — Image gen panel UI
- `harness/views/imagegen/expressions.go` — Datastar expression helpers
- `harness/views/world/signals.go` — Signal definitions including image gen signals
- `harness/views/chat/chat.templ` — Modified to include Image tab
- `harness/CLAUDE.md` — Datastar best practices section (lines 414-522)

## Action Items & Next Steps

1. **Add a "transparent background" toggle to the Image tab UI**. Add a new signal (e.g., `image_transparent_bg`, default `true`) with a checkbox or toggle button in the `ImageGenPanel`. Pass it through to the server via the `imageGenSignals` struct. In `gemini.go:Generate()`, only append `chromakeySuffix` and run `removeGreenBackground()` when this flag is true. When false, return the raw Gemini output as-is.

2. **Refactor imagegen handlers to use `PatchElementTempl`**. Replace the `MarshalAndPatchSignals` calls in `handleImageGenerate` and `handleImageSave` with `sse.PatchElementTempl()` calls that render templ components for the preview area, status text, and saved path display. Keep `MarshalAndPatchSignals` only for clearing `image_prompt`. This eliminates the need for most image gen signals (`image_gen_status`, `image_preview_url`, `image_saved_path`, `image_error_msg`, `image_gen_id`).

3. **Test E2E with a billing-enabled Gemini API key**. Set `GEMINI_API_KEY` in `harness/.env`, run `just up`, navigate to a world, open Image tab, type a prompt, click Generate. Verify: prompt is received by server (signal binding fix), image generates, preview displays, download works, save writes to `data/shared-assets/generated/`.

4. **Tune chromakey HSV thresholds if needed**. Current thresholds at `gemini.go:34-37`: hue ±30° of 120° (green), saturation ≥50%, value ≥30%, dilate radius 1px. These may need adjustment based on actual Gemini output quality.

## Other Notes

- The server runs via `just up` (Docker) with `GEMINI_API_KEY` passed through `docker-compose.yml:21`.
- The `data-bind:` colon syntax fix is critical — without it, **no input binding works** (chat, build prompt, image gen prompt all silently fail). If you see empty signal values on the server despite user input, check for dash syntax in `data-bind` attributes.
- The `data-indicator-fetching` attribute on the Generate button works with the default exclude filter (`/^_/`), since `fetching` doesn't start with underscore. The button disables during the request automatically.
- Chromakey constants are all at the top of `gemini.go` (lines 22-51) for easy tuning.
