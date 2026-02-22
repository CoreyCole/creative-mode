---
date: 2026-02-22T10:20:37-08:00
researcher: CoreyCole
git_commit: de59d1879069783037dfb86ff0368a164599495e
branch: main
repository: creative-mode
topic: "Mobile Chat Layout + Deploy Webhook Investigation"
tags: [mobile, css, layout, chat, deploy, webhook, templ]
status: complete
last_updated: 2026-02-22
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Mobile Chat Layout Fix + Deploy Webhook Bug

## Task(s)

### 1. Split Chat Layout from Root Layout (COMPLETED)
Extracted shared components (Head, Header, Footer) from `root.templ` and created a purpose-built `ChatLayout` for the mayor chat page that prevents document-level scrolling on mobile.

### 2. Fix Mobile Scrolling on Android Brave (COMPLETED — code correct, deploy stale)
Through multiple iterations, arrived at the proven layered approach for preventing document scroll on Android Chromium browsers. The code works correctly on localhost:3000 but the deployed site at creative-mode.ai is still serving old HTML.

### 3. Investigate Deploy Webhook (NOT STARTED — next step)
The deployed site has the correct git hash (`de59d18`) in the footer but is rendering the OLD `Root` layout instead of `ChatLayout`. This strongly suggests the webhook rebuild is **not regenerating templ templates** before compiling the Go binary. The templates are compiled into the binary at build time via `templ generate`, so if that step is skipped, the binary contains stale template code even though the `.templ` source files are updated by `git pull`.

## Critical References
- `site/CLAUDE.md` — "Mobile Layout Patterns" section documents the proven CSS pattern and what doesn't work
- `site/internal/webhook/` — GitHub push webhook handler that auto-deploys the site on EC2

## Recent changes

New files created:
- `site/layouts/head.templ` — shared `<head>` component
- `site/layouts/header.templ` — shared header/nav component
- `site/layouts/footer.templ` — shared footer component
- `site/layouts/chat.templ` — fixed-viewport chat layout (the key file)

Modified files:
- `site/layouts/root.templ` — simplified to compose Head + Header + Footer
- `site/pages/mayor.templ:6` — uses `ChatLayout` instead of `Root`, removed `<style>` block with `!important` overrides
- `site/pages/mayor.templ:42` — added `touch-pan-y` to `#chat-messages`
- `site/CLAUDE.md` — added "Mobile Layout Patterns" documentation
- `memory/MEMORY.md` — added "Mobile Layout Gotchas" section

## Learnings

### Mobile CSS — what works and what doesn't

**Android Chromium ignores `overflow: hidden` on html/body** — this is a documented browser behavior (whatwg/compat#79). Use `overflow: clip` instead.

**`position: fixed` on body/html is unreliable on mobile** — but `position: fixed; inset: 0` on a wrapper `<div>` DOES work to remove content from document flow.

**`touch-action: none` on ancestors overrides descendants** — per CSS spec, effective touch-action is the intersection of the element and all its ancestors. So `touch-action: none` on a wrapper prevents `touch-action: pan-y` on a child from working.

**The proven layered pattern** (see `site/layouts/chat.templ`):
- `overflow-clip` on html + body
- `fixed inset-0 overflow-clip` on wrapper div
- `flex-1 min-h-0 overflow-clip` on container
- `flex-1 min-h-0 overflow-y-auto overscroll-y-contain touch-pan-y` on the scrollable messages div
- `shrink-0` on pinned banner and input

### Deploy Webhook Issue

The deployed HTML shows the correct git hash `de59d18` but renders the old `Root` layout with `min-h-screen` and `<main class="flex-1">` wrapping the mayor page. The mayor-container even has classes (`relative flex flex-col h-[calc(100dvh-3.5rem)]`) that don't exist in the current source code at all — these must be from an older version of the templates.

**Root cause hypothesis**: The webhook handler pulls new code and rebuilds the Go binary, but does NOT run `templ generate` first. Since templ compiles `.templ` files into `_templ.go` files, and the Go compiler only sees `_templ.go` files, the binary ends up with stale template code. The `_templ.go` files in git ARE up to date (they're generated locally before commit), but if the webhook runs `go build` without first running `templ generate`, it might use cached build artifacts.

## Artifacts
- `site/layouts/chat.templ` — the chat layout (key implementation)
- `site/layouts/head.templ` — shared head component
- `site/layouts/header.templ` — shared header component
- `site/layouts/footer.templ` — shared footer component
- `site/layouts/root.templ` — simplified root layout
- `site/pages/mayor.templ` — mayor page using ChatLayout
- `site/CLAUDE.md` — mobile layout documentation (lines 76-130)
- `memory/MEMORY.md` — mobile layout gotchas

## Action Items & Next Steps

1. **Investigate the deploy webhook** — Read `site/internal/webhook/` to understand the build pipeline. Check if it runs `templ generate` and/or `just build` (which should include templ generation). The webhook handler likely needs to run `templ generate` before `go build`, or use `just build` which includes that step.

2. **Verify the site's `justfile` build target** — Check `site/justfile` for the `build` recipe. It should run `templ generate` + `tailwindcss` + `go build`. If the webhook calls `go build` directly instead of `just build`, that's the bug.

3. **After fixing the webhook**, trigger a redeploy and test on Android Brave:
   - Banner stays pinned when scrolling messages
   - Input stays at bottom, never hidden
   - Only messages area scrolls on touch
   - No document-level page scroll

4. **Test normal pages** (`/`, `/invite`, `/status`) to confirm they still render correctly with the new Root layout composition.

## Other Notes

- The site auto-deploys via GitHub webhook: `POST /webhook/github` on EC2. The handler is in `site/internal/webhook/`.
- EC2 deployment topology: Route 53 -> Caddy:443 -> localhost:3000
- The site runs as a native Go binary under systemd (not Docker), unlike the harness which uses Docker.
- All templ-generated `_templ.go` files ARE committed to git and up to date. The issue is likely build caching or the webhook not triggering a clean rebuild.
- The `just check` script from project root verifies everything compiles — it passes locally.
