---
date: 2026-03-09T14:39:59-07:00
researcher: CoreyCole
git_commit: fe1651dd2aa1cc4badfdd2c474d61503fc783e55
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Dashboard Timezone Fix + UI Redesign"
tags: [implementation, swarm-dashboard, timezone, mobile, responsive]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Dashboard Timezone Fix + Mobile-Friendly UI Redesign

## Task(s)

### Completed: Timezone Display Fix
All display timestamps in the harness were showing UTC instead of PST. Implemented a shared `tz.Display` package-level variable loaded from the `TZ` env var (default `America/Los_Angeles`). All `.Local()` calls and hardcoded PST were replaced with `.In(tz.Display)`. Storage remains UTC — only display formatting changed.

### Next: Swarm Dashboard UI Redesign
The swarm dashboard needs a responsive redesign focused on:
1. **Desktop**: Claude-desktop-app-style layout ratios for the chat interface — better use of space
2. **Mobile**: Fully functional on mobile — viewable dashboard, working chat
3. **Collapsible sidebar**: Tasks sidebar should collapse on mobile and be toggleable on desktop
4. **Dialog for "New Task"**: Replace the inline form with a dialog component

## Critical References
- `harness/views/swarm/dashboard.templ` — current swarm dashboard (all tabs, sidebar, chat)
- `site/layouts/chat.templ` + `site/pages/mayor.templ` — proven mobile-friendly chat layout pattern
- `context/datastarui/components/dialog/dialog.templ` — dialog component to use for "New Task"

## Recent changes
- `harness/internal/tz/tz.go` — new package: `var Display = time.UTC`
- `harness/main.go:96-104` — loads TZ env var, sets `tz.Display`
- `harness/views/swarm/dashboard.templ:890-908` — `formatTimestamp()` uses `tz.Display` instead of `.Local()`
- `harness/views/mayor/dashboard.templ:12-17` — `fmtTime()` uses `tz.Display`
- `harness/views/world/mayor_chat.templ:22-24` — chat times use `tz.Display` + `3:04 PM` format
- `harness/internal/server/events.go:181` — chat history timestamps use `tz.Display`
- `harness/internal/server/server.go:772` — chat event timestamps use `tz.Display`
- `harness/internal/server/imagegen.go:143,292` — filename + display timestamps use `tz.Display`
- `harness/internal/swarmorch/workflows.go:162-184` — removed hardcoded `pst` var, uses `tz.Display`
- `harness/.env.example` — added `TZ` env var documentation
- `harness/CLAUDE.md:554-556` — added timezone documentation

## Learnings

### Mobile Layout Pattern (from site/)
Android Chromium ignores `overflow: hidden` on body/html. The proven pattern is a layered defense:
- `overflow-clip` on html + body (stronger than `hidden`)
- `fixed inset-0` wrapper removes content from document flow
- `touch-pan-y overscroll-y-contain` on scrollable messages div
- `interactive-widget=resizes-content` in viewport meta for keyboard handling
- `window.visualViewport.addEventListener("resize", scrollToBottom)` for keyboard open/close

### No Signals for Mobile Detection
Responsive design is purely CSS via Tailwind breakpoints (sm:/md:/lg:), not signal-based. This is the correct approach per Datastar best practices.

### Dialog vs Sheet Components
- **Dialog** (`context/datastarui/components/dialog/`) — centered modal with backdrop, good for "New Task" form
- **Sheet** (`context/datastarui/components/sheet/`) — side-sliding panel, could work for mobile sidebar
- Both use signal-based open/close (`{open: false}`) with `data-show`

### Current Swarm Dashboard Structure
The dashboard is at `harness/views/swarm/dashboard.templ`. Key structure:
- Fixed sidebar (w-64) with task list
- Detail pane with 4 tabs: Chat (default), Agents, Spans, Artifacts
- Chat tab has messages area + input at bottom
- SSE connection in `data-init` on the main content area
- "New Task" is an inline form that shows/hides via `$show_new_form` signal

## Artifacts
- `harness/internal/tz/tz.go` — new file
- `harness/CLAUDE.md:554-556` — timezone docs
- `harness/.env.example` — TZ env var

## Action Items & Next Steps

1. **Make sidebar collapsible**: Add a toggle signal for the sidebar. On mobile (below `md:` or `lg:` breakpoint), hide by default. On desktop, show by default with a toggle button. Consider using the Sheet component for mobile sidebar overlay.

2. **Apply fixed-viewport chat layout**: The chat tab should use the `overflow-clip` + `fixed inset-0` pattern from `site/layouts/chat.templ`. The messages area needs `flex-1 min-h-0 overflow-y-auto overscroll-y-contain touch-pan-y`. The harness layout base (`harness/views/layout/base.templ`) may need a `FixedViewport` variant or the swarm page can opt into the pattern directly.

3. **Claude-desktop-app layout ratios**: On desktop, the sidebar should be narrower relative to the chat area. Currently it's `w-64` (256px) — this is fine but the chat area could be better centered with a max-width container (like `max-w-2xl mx-auto` which the chat already uses for individual messages).

4. **Replace "New Task" inline form with Dialog**: Import and use the dialog component from `context/datastarui/components/dialog/`. The dialog should contain the task type selector, request input, and ticket input. Trigger from the "+ New Task" header button.

5. **Mobile viewport meta tag**: The harness layout base may need `interactive-widget=resizes-content` in the viewport meta tag for chat keyboard handling. Check `harness/views/layout/base.templ`.

6. **Auto-scroll chat on new messages**: The site mayor chat uses JS to scroll to bottom on new messages and keyboard resize. The swarm chat should do the same (currently it doesn't auto-scroll).

7. **Test on mobile**: Use playwright-cli or a real device to verify the responsive layout works.

## Other Notes

### Key File Locations
- Swarm dashboard template: `harness/views/swarm/dashboard.templ`
- Swarm dashboard handlers: `harness/internal/server/swarm_dashboard.go`
- Swarm SSE handler: `harness/internal/server/swarm_dashboard.go` (handleSwarmDashboardSSE)
- Layout base: `harness/views/layout/base.templ`
- Site mobile chat layout: `site/layouts/chat.templ`
- Site mayor chat page: `site/pages/mayor.templ`
- Dialog component reference: `context/datastarui/components/dialog/dialog.templ`
- Sheet component reference: `context/datastarui/components/sheet/sheet.templ`

### Datastar Navigation Pattern
The swarm dashboard uses URL-based navigation (tabs and task selection are `<a>` links), which is correct per Datastar best practices. The sidebar collapse state can be a signal since it's pure UI toggle, not view state.

### Chat Input Pattern
The swarm chat input uses `data-bind:chat_input` + `data-on:keydown` for Enter-to-send. The site mayor chat additionally handles touch detection (`navigator.maxTouchPoints`) to conditionally enable Enter-submit on desktop only. Consider adopting this for mobile.
