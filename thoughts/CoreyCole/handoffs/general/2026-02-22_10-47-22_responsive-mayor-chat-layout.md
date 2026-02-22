---
date: 2026-02-22T10:47:22-08:00
researcher: CoreyCole
git_commit: 580cb53916ad5cd1c0d24b7292022d41ecf1e559
branch: main
repository: creative-mode
topic: "Responsive Mayor Chat Layout"
tags: [responsive, layout, chat, mayor, css, tailwind, datastarui]
status: complete
last_updated: 2026-02-22
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Responsive Mayor Chat Layout

## Task(s)

### 1. Fix deploy webhook binary path mismatch (COMPLETED)
The site's GitHub webhook was building the binary to `/tmp/creative-mode-site` but systemd was running `/usr/local/bin/creative-mode-site`. After SIGTERM + restart, systemd always started the old binary. Fixed by changing both to `/home/ubuntu/bin/creative-mode-site`.

### 2. Make mayor chat responsive for desktop (NOT STARTED — next step)
The mayor chat page (`/mayor`) currently uses a `ChatLayout` optimized for mobile (fixed viewport, overflow-clip pattern). On desktop it looks weird — the chat fills the entire viewport width with no max-width constraint or visual structure. Need to make it responsive so it looks good on both mobile and desktop.

Should investigate `context/datastarui` for patterns on how they handle mobile vs desktop layouts, and consider adopting or improving on their approach.

## Critical References
- `site/CLAUDE.md` — "Mobile Layout Patterns" section (lines 76-124) documents the proven CSS pattern for mobile chat
- `site/layouts/chat.templ` — the current fixed-viewport chat layout
- `context/datastarui/` — reference implementation for responsive layouts with Datastar

## Recent changes

- `site/internal/webhook/handler.go:201` — changed atomic rename destination from `/tmp/creative-mode-site` to `/home/ubuntu/bin/creative-mode-site`
- `site/creative-mode-site.service:12` — changed `ExecStart` to `/home/ubuntu/bin/creative-mode-site`
- `site/CLAUDE.md:155-158` — updated setup instructions to use `~/bin/` instead of `/usr/local/bin/`

## Learnings

### Deploy webhook root cause
The git hash in the footer was determined at **runtime** via `git rev-parse --short HEAD` (`site/main.go:39-44`), not embedded at compile time. This is why the hash showed correctly even when the binary was stale — `git reset --hard` updated the repo, so the runtime check returned the latest hash, but the compiled templates in the binary were old.

### Mobile CSS layered pattern
The `ChatLayout` uses a proven layered defense for mobile scroll prevention (see `site/CLAUDE.md` lines 76-124). When making the layout responsive, this mobile pattern must be preserved — any desktop enhancements should be additive (e.g., max-width container, centered layout) without breaking the mobile scroll prevention.

## Artifacts
- `site/layouts/chat.templ` — current chat layout (14 lines, very minimal)
- `site/layouts/root.templ` — root layout used by other pages (for comparison)
- `site/pages/mayor.templ` — mayor page that uses ChatLayout
- `site/CLAUDE.md:76-124` — mobile layout documentation
- `context/datastarui/` — reference code for responsive Datastar layouts

## Action Items & Next Steps

1. **Research `context/datastarui/` responsive patterns** — Look at how datastarui handles mobile vs desktop layouts, particularly any chat or full-viewport UIs. Identify reusable patterns.

2. **Design responsive chat layout** — The chat should:
   - On mobile: keep the current fixed-viewport pattern (overflow-clip, touch-pan-y, etc.)
   - On desktop: constrain chat width (max-w), center it, possibly add visual chrome (card/panel styling, sidebar space)
   - Use Tailwind responsive breakpoints (`sm:`, `md:`, `lg:`) for the transition

3. **Update `ChatLayout` in `site/layouts/chat.templ`** — Apply responsive classes while preserving the mobile scroll prevention pattern

4. **Test on both mobile and desktop** — Use playwright-cli for desktop verification, and the Android Brave test from the previous session for mobile

## Other Notes

- The `context/` directory is gitignored reference code — it's available locally but not in the repo
- The site runs on EC2 (not Docker), so testing the deployed version requires pushing to main and waiting for the webhook deploy
- After the webhook fix, the EC2 instance needs a one-time manual setup: `mkdir -p ~/bin && cp /usr/local/bin/creative-mode-site ~/bin/creative-mode-site && sudo cp ~/creative-mode/site/creative-mode-site.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl restart creative-mode-site`
- The mayor page route is `GET /mayor` in `site/main.go`
