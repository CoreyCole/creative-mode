---
date: 2026-02-13T13:30:06-08:00
researcher: CoreyCole
git_commit: 6b2b699abf66bb093010833bbe8046c44fd6a610
branch: main
repository: creative-mode
topic: "Backtick Overlay Toggle — Fix Opening from Inside Iframe"
tags: [bugfix, overlay, backtick, postMessage, datastar, iframe]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Fix Backtick Overlay Toggle — Opening from Inside Iframe

## Task(s)

### Completed: postMessage forwarding from iframe to parent
The original problem was that backtick worked to **close** the overlay (parent window has focus, `data-on:keydown__window` fires) but not to **open** it from inside the iframe. After closing, `focusGameFrame()` moves focus into the iframe. Keyboard events inside a cross-origin iframe don't propagate to the parent.

**What was done:**
1. Removed the fragile `contentWindow.addEventListener` IIFE from `game-loader.js` (lines 43–64) — this approach silently failed cross-origin in dev mode (Trunk on different port).
2. Added `postMessage({ type: 'toggle-overlay' })` forwarding to `templates/2d/index.html` (gated on `!e.target.matches('input, textarea')`).
3. Added same to `templates/3d/client/index.html` (gated on `!document.pointerLockElement` to avoid double-firing with Bevy's cursor-unlock path).
4. The existing `game-loader.js` message handler at line 14 already handles `toggle-overlay` by clicking `#game-overlay-toggle-trigger`.

**Build verified:** `just generate && go build ./... && just lint` all pass.

### Remaining bug: Backtick doesn't work on FRESH page load until overlay opened once
User reports: "on a fresh page load, the overlay does not open until I press the button. once it has opened once, then backtick works to open and close it."

This happens even with the parent window focused (not just from inside the iframe). The `data-on:keydown__window` handler on `#harness-overlay` simply doesn't fire until after the CM button has been clicked once.

## Critical References
- `harness/views/world/world.templ:39-50` — The `#harness-overlay` div with `data-signals`, `data-init`, `data-on:keydown__window`, and hidden trigger buttons
- `harness/views/world/overlay.templ:12-34` — The overlay expanded/minimized states with `data-show`
- `harness/static/game-loader.js` — postMessage bridge (handles `toggle-overlay` at line 14)

## Recent changes
- `harness/static/game-loader.js` — Removed contentWindow IIFE (was lines 43–64), file now 70 lines
- `templates/2d/index.html:27-33` — Added backtick → postMessage forwarding listener
- `templates/3d/client/index.html:30-36` — Added backtick → postMessage forwarding (pointer lock gated)

## Learnings

### Root cause analysis of remaining bug
The `data-on:keydown__window` handler on `#harness-overlay` (world.templ:43) is not firing on fresh page load. It starts working only after the CM button is clicked once. This strongly suggests a **Datastar initialization issue** — the window-level keydown listener may not be registered until Datastar has processed some interaction on the element.

Key observations:
- Datastar script is loaded as `<script type="module" defer src="/static/datastar.js">` in `harness/views/layout/layout.templ:20`
- The `#harness-overlay` div has both `data-init` (SSE connection) and `data-on:keydown__window` — possible conflict or sequencing issue
- The CM button (`overlay.templ:28`) calls `OE.Expand()` which sets `$overlay_expanded = true`. After this interaction, backtick works perfectly.
- The `game-overlay-toggle-trigger` hidden button (world.templ:47-48) ALSO doesn't work on fresh load (postMessage from iframe → game-loader.js → button.click() → signal change should work, but doesn't)
- Both the parent-side keydown handler AND the programmatic button click fail — this means the issue is likely that Datastar hasn't bound the `data-on:click` / `data-on:keydown__window` handlers yet, OR signal reactivity (`data-show`) isn't wired up

### Possible root causes (in order of likelihood)
1. **Datastar lazy initialization**: `data-on:keydown__window` might not register the window listener until the element or its signals are "activated" by a user interaction. The CM button click may trigger Datastar to fully process the element tree.
2. **`data-init` SSE blocking**: The `data-init` attribute starts an SSE connection. Datastar might process `data-init` first and defer binding other attributes until the SSE responds.
3. **Signal reactivity not wired**: The signals exist (set by `data-signals`) but `data-show="$overlay_expanded"` reactivity might not be established until after first interaction.

### Suggested fixes (for next agent)
1. **Add a standalone JS keydown listener in the parent page** that doesn't depend on Datastar at all. Put it in `game-loader.js` or a new script. It would directly click `#game-overlay-toggle-trigger` on backtick, bypassing `data-on:keydown__window` entirely. This is the most reliable approach since it avoids Datastar timing issues.
2. **Alternatively, investigate Datastar's initialization lifecycle**. Check if there's a `DOMContentLoaded` or `datastar:init` event that confirms when handlers are bound. May need to look at the Datastar v1.0.0-RC.6 source or docs.
3. **Test hypothesis**: Add a `console.log` inside the `data-on:keydown__window` expression to verify it's truly not firing (vs firing but the signal change not causing a re-render).

## Artifacts
- `harness/static/game-loader.js` — Modified (removed contentWindow IIFE)
- `templates/2d/index.html` — Modified (added postMessage forwarding)
- `templates/3d/client/index.html` — Modified (added postMessage forwarding)
- `harness/views/world/world.templ` — Not modified, but key file for the remaining bug

## Action Items & Next Steps
1. **Fix the "first interaction required" bug**: The most pragmatic approach is to add a pure JS `window.addEventListener('keydown', ...)` in `game-loader.js` that clicks `#game-overlay-toggle-trigger` on backtick. This completely bypasses Datastar's event binding timing. Key consideration: this handler will fire even before Datastar initializes signals, so the trigger button's `data-on:click` handler also needs to be wired. May need to delay the listener or check if Datastar is ready.
2. **Test the full flow**: After fixing, verify: (a) fresh page load → backtick opens overlay, (b) backtick closes, (c) click in iframe → backtick reopens (postMessage path), (d) backtick in chat input doesn't toggle.
3. **Do NOT restart Docker** — other models are actively working on the server.

## Other Notes

### Overlay architecture
- Signals are defined in `harness/views/world/signals.go` — `OverlaySignals` struct, defaults set by `DefaultOverlaySignals()`
- Expression builders in `harness/views/world/expressions.go` — `OE.Expand()`, `OE.Minimize()`, etc.
- SSE handler in `harness/internal/server/events.go` — does NOT send `overlay_expanded` signal on initial connect, so no server-side race condition
- The hidden trigger buttons pattern (world.templ:45-48) is the bridge between JS `postMessage` world and Datastar's signal system

### The postMessage flow (working correctly once Datastar is initialized)
```
iframe keydown (backtick)
  → window.parent.postMessage({ type: 'toggle-overlay' })
  → game-loader.js message listener
  → document.getElementById('game-overlay-toggle-trigger').click()
  → Datastar data-on:click handler toggles $overlay_expanded
  → data-show reacts, overlay shows/hides
```

### Datastar version
Using Datastar v1.0.0-RC.6 loaded from `/static/datastar.js` as ES module with defer.

### 3D template pointer lock note
In the 3D template, when pointer is locked, Bevy handles backtick via the Rust `cursor-unlocked` postMessage path (which opens the overlay via `#game-cursor-unlock-trigger`). The new JS listener only fires when pointer is NOT locked, avoiding double-fire.
