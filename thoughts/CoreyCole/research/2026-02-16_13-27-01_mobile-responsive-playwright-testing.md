---
date: 2026-02-16T13:27:01-08:00
researcher: CoreyCole
git_commit: 7abb4f2d8949b251c213244d6a723e7c4cec2083
branch: main
repository: creative-mode
topic: "Mobile responsive testing with playwright-cli for marketing site"
tags: [research, codebase, playwright-cli, mobile, responsive, marketing-site]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Mobile Responsive Testing with playwright-cli

**Date**: 2026-02-16T13:27:01-08:00
**Researcher**: CoreyCole
**Git Commit**: 7abb4f2d8949b251c213244d6a723e7c4cec2083
**Branch**: main
**Repository**: creative-mode

## Research Question
Can playwright-cli be used with mobile screen sizes to take screenshots of the marketing site for mobile responsiveness testing?

## Summary
Yes. playwright-cli supports viewport resizing via `playwright-cli resize <width> <height>` which can set any mobile viewport dimensions. Combined with `playwright-cli screenshot`, this enables capturing mobile views of any page. There is no built-in device emulation (user agent, DPR, touch events), but viewport sizing covers the primary responsive layout testing needs.

## Detailed Findings

### playwright-cli Mobile Viewport Support

The `resize` command (`SKILL.md:52`) sets the browser viewport to any arbitrary width and height:

```bash
playwright-cli resize 375 812  # iPhone X viewport
```

Combined with `screenshot`, this enables a full mobile responsive testing workflow:

```bash
playwright-cli open http://localhost:3000 --headed --persistent
playwright-cli resize 375 812          # iPhone X
playwright-cli screenshot
playwright-cli resize 768 1024         # iPad
playwright-cli screenshot
playwright-cli resize 1920 1080        # Desktop
playwright-cli screenshot
```

**Limitations:**
- No built-in `--device` flag for full device emulation profiles
- User agent, device pixel ratio, and touch support must be set manually via `run-code`
- Viewport resize is sufficient for CSS media query / responsive layout testing

### Common Mobile Breakpoints

| Device | Width x Height |
|--------|---------------|
| iPhone SE | 375 x 667 |
| iPhone X/12/13 | 375 x 812 |
| iPhone 14 Pro Max | 430 x 932 |
| Pixel 7 | 412 x 915 |
| iPad Mini | 768 x 1024 |
| iPad Pro | 1024 x 1366 |

### Marketing Site Pages to Test

The site runs on port 3000 locally (Docker via `just up` in `site/`).

**Public pages (no auth required):**
- `GET /` — home/landing page
- `GET /status` — status dashboard

**Auth-gated pages:**
- `GET /invite` — invite code entry (requires session)
- `GET /join-discord` — guild membership check (requires session)
- `GET /mayor` — onboarding chat (requires session + guild + invite code)

For auth-gated pages, use `--persistent` flag to reuse a previously authenticated session, or use `POST /dev/auth/login` in DEV_MODE.

### Site Template Structure

Page templates are in `site/pages/`:
- `home_templ.go` — landing page
- `status_templ.go` — status dashboard
- `invite_templ.go` — invite page
- `join_discord_templ.go` — join Discord page
- `mayor_templ.go` — onboarding chat
- `coming_soon_templ.go` — placeholder

Root layout: `site/layouts/root_templ.go` — wraps all pages with consistent header/footer.

## Code References
- `.claude/skills/playwright-cli/SKILL.md:52` — `resize` command documentation
- `.claude/skills/playwright-cli/SKILL.md:89` — `screenshot` command
- `.claude/skills/playwright-cli/references/running-code.md:167-170` — reading viewport size via run-code
- `site/main.go:187-194` — home page route handler
- `site/main.go:177-179` — status page routes
- `site/main.go:408-411` — server port configuration (default 3000)
- `playwright-cli.json` — project config (chromium, file output to `.playwright-cli/`)

## Architecture Insights
- playwright-cli outputs all screenshots to `.playwright-cli/` directory as timestamped PNG files
- The `--persistent` flag stores browser profile for session reuse across invocations
- The site's Docker setup with `DEV_MODE=true` enables bypass login for testing auth-gated pages
- Tailwind CSS in the site uses responsive utilities — mobile viewport testing will validate these breakpoints

## Open Questions
- Should we create a systematic mobile testing script/checklist for all key pages?
- Are there specific mobile UX issues already known that need investigation?
- Should we test landscape orientations as well as portrait?
