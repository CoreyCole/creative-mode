---
date: 2026-02-16T08:43:21+0000
researcher: CoreyCole
git_commit: 59c61aab63c7df584cf0d6c2508836274933afe3
branch: main
repository: creative-mode
topic: "2D World Demo Readiness Review & Build Pipeline End-to-End Diagnosis"
tags: [research, codebase, 2d-template, build-pipeline, demo, nano-banana, image-generation]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: 2D World Demo Readiness Review & Build Pipeline End-to-End Diagnosis

**Date**: 2026-02-16T08:43:21+0000
**Researcher**: CoreyCole
**Git Commit**: 59c61aab63c7df584cf0d6c2508836274933afe3
**Branch**: main
**Repository**: creative-mode

## Research Question

1. Thoroughly review the 2D world template and identify areas for improvement — this is the main demo showing Nano Banana image generation and real-time world building.
2. Thoroughly review the build prompt pipeline that builds a new world in a tmux session — it's not working end-to-end.

## Summary

### 2D World: Demo-Ready with Polish Needed

The 2D template works for the core demo flow (generate image → place in room → see it appear). The recent iframe reload fix (commit `59c61aa`) resolved the biggest blocker — images now auto-reload after placement. However, several UX issues would hurt a live demo: no dialog backdrop (text hard to read on images), labels overlaying image hotspots, hotspot ID collisions on duplicate names, and the full iframe reload being heavy (~several MB WASM re-download per placement).

### Build Pipeline: 3 Likely Root Causes for End-to-End Failure

The build pipeline has a **critical silent failure mode**: hook scripts that fire when Claude Code stops do NOT include the `X-Hook-Secret` header, so if `CM_HOOK_SECRET` is set on the VPS, all hook callbacks are silently rejected with 403. The `on-stop.sh` event never reaches the harness, `BuildCheckpoint` is never called, and the checkpoint stays stuck in "building" forever. Two additional failure modes: the boardgame template has no `.claude/hooks/` directory at all, and the fire-and-forget curl in hook scripts can be lost during harness hot-reloads.

---

## Part 1: 2D World Template — Detailed Findings

### Architecture Overview

The 2D template is a **client-only** Bevy WASM app (no game server). It renders data-driven rooms from JSON files, with interactive hotspots that trigger dialogs, room navigation, or cross-world navigation via a postMessage bridge. The harness overlay provides image generation (Nano Banana / Gemini), placement, and reload controls.

**Core files:**
- `templates/2d/src/lib.rs:11-43` — App bootstrap, 1280x720 window, all plugins
- `templates/2d/src/room.rs` — Room loading, JSON schema, entity spawning
- `templates/2d/src/camera.rs` — Pan/zoom, touch support, fit-to-window
- `templates/2d/src/interaction.rs` — Hotspot hover/click, dialog spawning
- `templates/2d/src/bridge.rs` — postMessage to harness parent frame
- `templates/2d/src/debug.rs` — Debug query system (WASM-only)

### Demo Flow (Working Path)

```
1. User clicks Assets tab → loads asset tree from data/shared-assets/generated/
2. User types prompt, clicks Generate → Gemini 2.5 Flash generates image
3. (If transparent BG) → chromakey removes #00FF00 background → PNG
4. User clicks Save → image written to data/shared-assets/generated/{timestamp}-{slug}.png
5. User clicks Place on saved asset → room list loaded
6. User selects room → hotspot targets shown (background / existing hotspot / new hotspot)
7. User picks target → room JSON updated on disk → iframe auto-reloaded via SSE ExecuteScript
8. Bevy WASM restarts → fetches fresh room JSON (no-cache header) → renders placed image
```

### Issues Affecting Demo Quality

#### P0 — Demo Blockers

**1. Dialog text has no backdrop** (`interaction.rs:112-124`, `debug.rs:152-160`)
Dialog text is raw white `Text2d` with no background panel. When placed over a background image or image hotspot, it becomes unreadable. For a demo showing off placed images, this is immediately visible.

**2. Labels render on top of image hotspots** (`room.rs:313-323`)
Text labels are spawned for ALL hotspots at z=2.0, including those with images. If you place a character sprite as a hotspot, the label "Cat" will overlay the cat image. For a visual demo, this looks broken.

#### P1 — Noticeable Polish Issues

**3. Full iframe reload is heavy** (`overlay.templ:50`, `rooms.go:142,219,354`)
Every placement triggers a complete WASM reload — the entire Bevy app re-initializes, re-downloads the ~5MB WASM binary. On a live demo with multiple placements, this creates a 2-3 second delay each time. The in-place `reload-room` postMessage path exists (`room.rs:204-222`) but doesn't actually re-fetch the asset due to Bevy's asset caching.

**4. Hotspot ID uniqueness is weak** (`rooms.go:302-308`)
When creating new hotspots, only appends "-2" on first collision. If "cat" and "cat-2" both exist, creating another "cat" would duplicate "cat-2". In a demo with multiple placements, this can produce unexpected behavior.

**5. Image cache expires in 30 minutes** (`gemini/gemini.go:22`)
If a user generates an image, takes a break, then comes back to save it, the cache entry is gone. Preview image 404s simultaneously with save error. During a demo, this 30-minute window is tight if there are interruptions.

**6. `open-embed` action is unimplemented** (`game-loader.js:24`)
If a room JSON happens to have an `open_embed` action, clicking the hotspot just logs to console. Not a demo issue unless someone creates one.

#### P2 — Technical Debt

**7. Code duplication between interaction.rs and debug.rs** — Dialog spawning and action dispatch logic is duplicated. Changes to one must be manually mirrored.

**8. Bevy asset cache prevents in-place room reload** (`room.rs:176-177`) — `asset_server.load()` returns cached handles. The `reload-room` postMessage path is effectively broken. The full iframe reload workaround works but is heavy.

**9. No error feedback for missing images** — If a hotspot references a non-existent image file, Bevy shows a white rectangle with no user-facing error.

**10. Touch drag conflicts with tap** (`camera.rs:198-205`) — 10px threshold means small hotspots can be hard to tap on mobile.

### Overlay UI Issues

**11. `unread_count` signal is never incremented** — The badge in the minimized overlay will never show. Dead feature.

**12. Global and World tabs show identical content** (`chat.templ:38`) — Both tabs display the same `#chat-log`. No filtering. Switching tabs has no visible effect.

**13. `loadCheckpoint` ignores the checkpoint ID** (`game-loader.js:39-41`) — Always navigates to `/world/{worldID}` and uses whatever checkpoint the server selects. Clicking "Load" on a specific checkpoint may land on a different one.

**14. Mayor tab has no message history load** — Only shows live messages. Opening the Mayor tab shows empty content even if historical messages exist in DB.

**15. Chat input on Mayor tab posts to global chat** (`chat_input.templ:19`) — User on Mayor tab might expect their message goes to the mayor, but it goes to global.

**16. `rate_limit_retry_at` signal is set but never displayed** — No countdown or timer shown. User sees raw "rate_limited" text.

---

## Part 2: Build Pipeline — End-to-End Diagnosis

### Pipeline Flow

```
POST /world/:worldID/prompt (server.go:403)
  → Orchestrator.HandlePrompt (claude.go:73)
    → ForkCheckpoint (manager.go:224) — copy files, clone build cache, DB status="building"
    → updateMemory — append prompt to MEMORY.md
    → ConnectDev (3D only) — start cargo watch dev server
    → tmux.Create — session cm-{worldID}-{cpID} with CM_* env vars
    → tmux.SendPrompt — claude --dangerously-skip-permissions --input-file

Claude Code runs in tmux session...
  → on-tool-use.sh fires on each tool use → POST /api/claude-event
  → on-stop.sh fires when Claude stops → POST /api/claude-event (type: session_stopped)

POST /api/claude-event (server.go:640)
  → if event == "claude.session_stopped":
    → go BuildCheckpoint(worldID, cpID) (claude.go:139)
      → Kill dev server + Claude tmux session
      → builder.Build — cargo build (3D) + trunk build --release
      → PostBuild — extract CHANGES.txt summary
      → DB status="ready", update WasmPath
      → Start prod game server (3D only)
      → EventBus: build.completed → SSE → iframe reload
```

### Root Cause #1: Hook Scripts Don't Send `X-Hook-Secret` (MOST LIKELY)

**The smoking gun.** All three hook scripts (`on-stop.sh`, `on-tool-use.sh`, `on-notification.sh`) in `templates/3d/.claude/hooks/` and `templates/2d/.claude/hooks/` POST to `$CM_HARNESS_URL/api/claude-event` without an `X-Hook-Secret` header:

```bash
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
```

The `/api/claude-event` route at `server.go:237` is registered with `hookSecretMiddleware()` at `server.go:619-636`:

```go
secret := os.Getenv("CM_HOOK_SECRET")
if secret != "" && c.Request().Header.Get("X-Hook-Secret") != secret {
    return echo.NewHTTPError(http.StatusForbidden, "invalid hook secret")
}
```

**If `CM_HOOK_SECRET` is set in the harness environment, ALL hook callbacks are silently rejected with 403.** Since curl output is discarded (`&>/dev/null`), the rejection is invisible. The `on-stop.sh` event never arrives, `BuildCheckpoint` is never called, and the checkpoint stays stuck in "building" forever.

**Fix**: Either remove `CM_HOOK_SECRET` from the VPS env, or add `CM_HOOK_SECRET` to the tmux session env vars and update hook scripts to include `-H "X-Hook-Secret: $CM_HOOK_SECRET"`.

### Root Cause #2: Boardgame Template Has No Hook Scripts

The `templates/boardgame/` directory has NO `.claude/` directory. This means:
- Claude Code sessions for boardgame worlds have no hook scripts
- `on-stop.sh` never fires → `session_stopped` event never posted → build never triggered
- Checkpoint stays stuck in "building" forever

**Fix**: Copy `.claude/` from `templates/2d/` to `templates/boardgame/`.

### Root Cause #3: Fire-and-Forget Curl Lost During Harness Restart

All hook scripts run curl with `&>/dev/null &` (backgrounded, output discarded). If `air` hot-reloads the harness at the exact moment Claude stops, the POST is permanently lost. No retry logic exists.

**Fix**: Add retry logic to `on-stop.sh` (at minimum — this is the critical hook). Something like:
```bash
for i in 1 2 3; do
  curl -sf -X POST "$HARNESS_URL/api/claude-event" ... && break
  sleep 2
done
```

### Additional Build Pipeline Issues

**4. No global build serialization** — Two simultaneous WASM builds will OOM (10GB VPS, ~5GB per wasm-bindgen). The rate limiter (`rate_limit.go`) only limits per-user, not globally. Two different users building simultaneously would crash the server.

**5. Template type defaults to "3d" on DB error** (`claude.go:88-91, 159-162`) — If the DB lookup fails for any reason, the code silently defaults to "3d". A 2D world would have a server build attempted (`cargo build -p server`), which fails because 2D templates don't have a `server` package.

**6. Initial world creation sets status "ready" before build** (`manager.go:126`) — The checkpoint is created with status "ready" even though the background build hasn't started. User sees a "ready" checkpoint with no WASM, resulting in a broken iframe.

**7. Reaper can kill sessions prematurely** (`claude.go:333`) — Every 5 minutes, kills Claude tmux sessions whose checkpoints aren't in "building" status. If DB gets updated by concurrent operations, the session dies early.

**8. Fork directory path divergence** — Initial checkpoint: `data/worlds/{slug}_{timestamp}_{worldID}/{cpID}`. Forked checkpoint: `data/worlds/{worldID}/{newID}`. Different parent directories. Works because paths are stored in DB, but filesystem layout is inconsistent.

### Environment Variable Chain

| Variable | Set Where | Used Where | Issue? |
|----------|-----------|------------|--------|
| `CM_WORLD_ID` | tmux session create | Hook scripts | OK |
| `CM_CHECKPOINT_ID` | tmux session create | Hook scripts | OK |
| `CM_HARNESS_URL` | tmux session create | Hook scripts (curl target) | OK |
| `CM_LOG_DIR` | tmux session create | Hook scripts (log path) | OK |
| `CM_GAME_PORT` | HandlePrompt extra env | Claude Code CLAUDE.md | 3D only, non-fatal |
| `CM_BRP_PORT` | HandlePrompt extra env | Claude Code CLAUDE.md | 3D only, non-fatal |
| `CM_HOOK_SECRET` | **NOT PASSED** | Hook scripts need it for auth | **BROKEN** |

---

## Part 3: Nano Banana Image Generation Flow

### Working Well

- Gemini 2.5 Flash model generates quality images
- Chromakey green-screen removal works (HSV-based, 30-degree hue tolerance, 1px dilation)
- In-memory cache with 30min TTL and max 50 items is reasonable
- Placement UI offers three sensible modes (background, existing hotspot, new hotspot)
- Room JSON round-tripping preserves formatting (`json.MarshalIndent`)
- Saved images get timestamped filenames preventing collisions
- Asset tree refreshes after save
- Iframe auto-reloads after placement (newly fixed in commit `59c61aa`)
- Room JSON has `no-cache` header ensuring fresh data on reload

### Issues

- **Cache is in-process memory only** — If harness restarts between Generate and Save, all cached images are lost. No persistence layer.
- **Hotspot aspect ratio sizing** (`rooms.go:226-246`) — Uses larger existing dimension as constraint, which can produce unexpectedly large or small hotspots depending on the original dimensions.
- **No image re-generation with variations** — User must type a new prompt each time. No "regenerate" or "try again" button.

---

## Prioritized Recommendations

### For Demo Day (Critical Path)

1. **Verify `CM_HOOK_SECRET` status on VPS** — If set, builds are broken. Either unset it or propagate to hook scripts.
2. **Add dialog backdrop** — Dark semi-transparent panel behind dialog text so it's readable over images.
3. **Suppress labels on image hotspots** — Don't render text labels when a hotspot has an image.
4. **Fix hotspot ID collision** — Use a proper uniqueness loop or UUID suffix.

### Quick Wins

5. **Add `.claude/hooks/` to boardgame template** — Copy from `templates/2d/`.
6. **Add retry to `on-stop.sh`** — Prevent lost events during harness restarts.
7. **Load mayor message history** — Call `GetRecentMayorMessages` in SSE setup.
8. **Fix `loadCheckpoint` to use checkpoint ID** — Pass cpID to server or set user position.

### Medium-Term

9. **Implement in-place room reload** — Fix Bevy asset cache bypass so `reload-room` postMessage works without full iframe restart.
10. **Add global build mutex** — Prevent OOM from concurrent WASM builds.
11. **Differentiate Global vs World chat tabs** — Filter by world or remove the duplicate tab.
12. **Wire up `unread_count`** — Or remove the dead badge UI.

---

## Code References

- `harness/internal/server/server.go:619-636` — hookSecretMiddleware (the likely build breaker)
- `harness/internal/claude/claude.go:73-135` — HandlePrompt (orchestrator entry)
- `harness/internal/claude/claude.go:139-253` — BuildCheckpoint (async build after Claude stops)
- `harness/internal/builder/builder.go:50-158` — Build execution (cargo + trunk)
- `templates/2d/.claude/hooks/on-stop.sh` — Hook that triggers build (missing X-Hook-Secret)
- `templates/2d/src/room.rs:225-323` — Room entity spawning (labels on all hotspots)
- `templates/2d/src/interaction.rs:112-124` — Dialog spawning (no backdrop)
- `templates/2d/src/room.rs:176-177` — Bevy asset cache issue
- `harness/internal/server/rooms.go:302-308` — Weak hotspot ID uniqueness
- `harness/internal/server/events.go:259-306` — Build completion iframe reload
- `harness/static/game-loader.js:39-41` — loadCheckpoint ignores cpID
- `harness/views/chat/chat.templ:38` — Global/World tabs sharing same content

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-13_10-12-01_2d-asset-image-support.md` — Original 2D image support plan
- `thoughts/CoreyCole/plans/2026-02-15_21-54-43_wasm-build-memory-optimization.md` — WASM build OOM mitigation plan
- `thoughts/CoreyCole/plans/2026-02-10-component-5-claude-integration-tmux.md` — Claude Code + tmux integration design
- `thoughts/CoreyCole/handoffs/general/2026-02-15_22-47-27_wasm-static-phase1-complete.md` — Static WASM serving implementation
- `thoughts/CoreyCole/research/2026-02-11_22-28-57_rebuild-hot-reload-wasm-assets.md` — Rebuild system analysis
- `thoughts/CoreyCole/plans/2026-02-14_demo-video-brainstorm.md` — Demo video planning

## Open Questions

1. **Is `CM_HOOK_SECRET` currently set on the VPS?** — This would explain builds being stuck. Check with `echo $CM_HOOK_SECRET` in the harness environment.
2. **Has anyone successfully completed a build end-to-end on the VPS?** — If yes, then `CM_HOOK_SECRET` is likely unset and the issue is elsewhere (maybe the harness restarting during Claude's stop event).
3. **Should the demo use template worlds or built worlds?** — Template worlds with image placement work today. Built worlds require the pipeline to be fixed first.
4. **Do we need a lighter room reload for the demo?** — Full iframe reload works but is slow. Fixing Bevy's asset cache bypass would make placements feel instant.
