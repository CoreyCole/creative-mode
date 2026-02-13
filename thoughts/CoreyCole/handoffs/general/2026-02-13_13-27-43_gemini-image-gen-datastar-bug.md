---
date: 2026-02-13T13:27:43-08:00
researcher: CoreyCole
git_commit: b9ff0ff0181873170bde73830f83dd23a92a0c47
branch: main
repository: creative-mode
topic: "Gemini Image Generation — Datastar Signal Binding Bug & Best Practices"
tags: [implementation, bug, datastar, image-generation, gemini]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Gemini Image Generation — Datastar Signal Bug Fix

## Task(s)

### Completed: Nano Banana Image Generation Integration
Full implementation of AI image generation via Google Gemini API as a new "Image" tab in the overlay sidebar. All code compiles and lints clean. Phases 1-5 of the plan are complete.

Plan document: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` (referenced at session start but the actual plan was provided inline)

### Completed: Chromakey Transparency Post-Processing
Since Gemini cannot generate transparent PNGs natively, implemented a chromakey green background removal pipeline:
- Appends green background instructions to every prompt
- Post-processes returned images: decodes (handles JPEG-masquerading-as-PNG bug), converts to NRGBA, detects green pixels in HSV color space, dilates mask, zeroes alpha, re-encodes as real PNG

### Bug (Work in Progress): "Please enter a description" on Generate click
When a user types a prompt in the Image tab input and clicks Generate, the server receives an empty `image_prompt` signal and returns "Please enter a description". The input uses `data-bind-image_prompt` but the signal value is not being sent to the backend.

### Planned: Update harness/CLAUDE.md with Datastar best practices
User wants guidance added to the developer guide about proper Datastar signal usage patterns.

## Critical References
- Datastar actions reference: https://data-star.dev/reference/actions#options
- Tao of Datastar guide: https://data-star.dev/guide/the_tao_of_datastar/
- `harness/CLAUDE.md` — the developer guide that needs updated Datastar best practices section

## Recent changes

### New files:
- `harness/internal/gemini/gemini.go` — Gemini API client with in-memory cache + chromakey green removal pipeline
- `harness/internal/server/imagegen.go` — Three HTTP handlers: `handleImageGenerate` (SSE), `handleImagePreview` (raw image), `handleImageSave` (SSE)
- `harness/views/imagegen/imagegen.templ` — Image gen panel UI (aspect ratio selector, preview with download links, status, saved path display) + input bar
- `harness/views/imagegen/expressions.go` — Datastar expression helpers for status colors and aspect ratio buttons

### Modified files:
- `harness/internal/server/server.go:22` — Added gemini import
- `harness/internal/server/server.go:49` — Added `GeminiClient *gemini.Client` field to Server struct
- `harness/internal/server/server.go:160-162` — Registered 3 image gen routes under approved group
- `harness/main.go:19` — Added gemini import
- `harness/main.go:226-232` — Initialize Gemini client from env var
- `harness/main.go:239` — Wire GeminiClient to server
- `harness/views/world/signals.go:17-23` — Added 7 image gen signals (ImagePrompt, ImageGenStatus, ImageGenID, ImagePreviewURL, ImageSavedPath, ImageErrorMsg, ImageAspectRatio)
- `harness/views/world/signals.go:32-33` — Added defaults (ImageGenStatus: "idle", ImageAspectRatio: "1:1")
- `harness/views/chat/chat.templ` — Added Image tab button, conditional visibility for chat-log/input, imagegen panel + input bar
- `harness/docker-compose.yml:21` — Pass GEMINI_API_KEY env var
- `harness/.env:3` — Added GEMINI_API_KEY (user has added their key)

## Learnings

### The Signal Binding Bug — Root Cause
The `data-bind-image_prompt` attribute on the input correctly binds the input value to the `$image_prompt` signal on the client. However, when `datastar.PostSSE("/api/images/generate")` fires, the server-side `datastar.ReadSignals()` reads the signal value from the request. The issue is likely that the signal isn't being included in the SSE POST request body.

Key insight from user: **Datastar's philosophy is to favor fetching current state from the backend rather than pre-loading and assuming frontend state is current.** Signals should primarily be used for:
1. User interactions (toggling visibility)
2. Sending new state to the backend (binding to form inputs)

The current pattern of having many server-patched signals (`image_gen_status`, `image_gen_id`, `image_preview_url`, `image_saved_path`, `image_error_msg`) may be over-managing state on the frontend. The Datastar way would be to have the backend render HTML fragments via `PatchElementTempl` rather than patching individual signal values.

Review these URLs for the correct approach:
- https://data-star.dev/reference/actions#options — how `@post` sends signals
- https://data-star.dev/guide/the_tao_of_datastar/ — philosophy of server-driven state

### Working chat input pattern for comparison
The chat input (`harness/views/chat/chat_input.templ`) uses `data-bind-chat_text` and `datastar.PostSSE("/api/chat")` — and it works. The server reads `chat_text` via `datastar.ReadSignals()` at `harness/internal/server/server.go:604`. Compare this working pattern against the broken image gen pattern to find the difference.

### Gemini API Notes
- Model name: `gemini-2.5-flash-image` (resolves to `gemini-2.5-flash-preview-image` internally)
- Free tier API keys may have `limit: 0` for image generation — need billing enabled
- Gemini sometimes returns JPEG bytes despite claiming PNG MIME type — the code handles this
- No native transparency support — chromakey green removal is the standard workaround

## Artifacts

- `harness/internal/gemini/gemini.go` — Full Gemini client with chromakey pipeline
- `harness/internal/server/imagegen.go` — HTTP handlers for generate/preview/save
- `harness/views/imagegen/imagegen.templ` — UI components
- `harness/views/imagegen/expressions.go` — Datastar expression helpers
- `harness/views/world/signals.go` — Signal definitions
- `harness/views/chat/chat.templ` — Modified to include Image tab

## Action Items & Next Steps

1. **Fix the signal binding bug**: The `$image_prompt` signal is empty when `@post('/api/images/generate')` fires. Compare with the working chat input pattern. Read the Datastar action options docs to understand how signals are sent with `@post`. It may be that the signal needs to be explicitly included, or the issue is with how `datastar.ReadSignals` parses the request on the server side. Consider whether the approach should shift from signal-patching to HTML fragment patching (`PatchElementTempl`).

2. **Refactor to follow Datastar best practices**: The current implementation patches many signals from the server (`image_gen_status`, `image_preview_url`, etc.). Per the Tao of Datastar, this is an anti-pattern — the server should drive UI via HTML fragments. Consider refactoring `handleImageGenerate` and `handleImageSave` to use `sse.PatchElementTempl()` to render updated panel state, rather than `sse.MarshalAndPatchSignals()` for every piece of state.

3. **Update harness/CLAUDE.md**: Add a "Datastar Best Practices" section documenting:
   - Favor `PatchElementTempl` over `MarshalAndPatchSignals` for complex UI state
   - Use signals only for user input binding and simple UI toggles
   - Don't manage complex server state (status, URLs, paths) as client-side signals
   - Reference the Tao of Datastar principles

4. **Test with a billing-enabled API key**: The current key has `limit: 0` on the free tier for image generation. Once billing is enabled, test the full generate → preview → download → save flow.

## Other Notes

- The `data-bind-*` attribute creates two-way binding between an input and a signal. When used with `@post`, the signal value should be included in the request automatically — but verify this is happening by checking network requests in the browser.
- The existing chat system (`handleChatMessage` + `handleGlobalSSE`) is a good reference for how Datastar SSE patterns work in this codebase. Chat uses `PatchElementTempl` for messages and `MarshalAndPatchSignals` only for clearing the input.
- The server is running via `just up` with GEMINI_API_KEY set. Hot-reload is active.
- Chromakey HSV thresholds: hue within 30deg of 120deg (green), saturation >= 50%, value >= 30%. Dilate radius = 1px. These may need tuning after real generation tests.
