# Harness E2E Regression Test Plan

## Overview

Comprehensive browser-based regression test suite for the Creative Mode harness UI, executed via `playwright-cli`. Covers every user-facing page, interactive element, SSE-driven update, and error state. Designed to be run by a Claude Code session against a live harness instance.

## Prerequisites

### 1. Start the Harness

```bash
just -f /Users/coreycole/cdev/creative-mode/harness/justfile live
```

Wait for the server to be healthy:

```bash
curl -s http://localhost:8080/health
# Expected: {"status":"ok"} (200)
```

### 2. Open Browser

```bash
playwright-cli open http://localhost:8080
```

### 3. Authenticate

The browser opens to the login page. Complete GitHub OAuth manually:

1. `playwright-cli snapshot` — find the "Sign in with GitHub" link ref
2. `playwright-cli click <ref>` — initiates OAuth flow
3. Complete GitHub authorization in the browser
4. After callback, you'll land on the lobby (admin) or pending page (new user)

### 4. Testing Protocol

Before each test section:
- `playwright-cli snapshot` — get current element refs
- `playwright-cli console` — note baseline error count

After each action:
- `playwright-cli console` — check for new JS errors
- `playwright-cli screenshot` — visually verify the result

**Acceptable baseline errors**: `favicon.ico` 404 (no favicon configured).

---

## Test Suite

### T1: Login Page

**URL**: `http://localhost:8080` (unauthenticated)

| # | Step | Command | Expected |
|---|------|---------|----------|
| T1.1 | Take snapshot | `playwright-cli snapshot` | Page contains "Creative Mode" heading, "Collaborative game development with AI" text, "Sign in with GitHub" link |
| T1.2 | Screenshot | `playwright-cli screenshot` | Login page renders correctly — centered container with heading and sign-in button |
| T1.3 | Console check | `playwright-cli console` | No errors (except favicon.ico 404) |

**Source**: `harness/views/login/login.templ`

---

### T2: Lobby Page

**URL**: `http://localhost:8080` (authenticated, approved user)

#### T2.1: Header

| # | Step | Command | Expected |
|---|------|---------|----------|
| T2.1.1 | Take snapshot | `playwright-cli snapshot` | Header contains: avatar image (if set), username text, "Admin" button (admin only), "Logout" button |
| T2.1.2 | Screenshot | `playwright-cli screenshot` | Header renders with user info and navigation buttons |

#### T2.2: World List

| # | Step | Command | Expected |
|---|------|---------|----------|
| T2.2.1 | Verify worlds section | `playwright-cli snapshot` | "Worlds" heading visible. Each world is a clickable card with name and optional description |
| T2.2.2 | Screenshot | `playwright-cli screenshot` | World cards render in the worlds panel |

#### T2.3: Create World

| # | Step | Command | Expected |
|---|------|---------|----------|
| T2.3.1 | Snapshot form | `playwright-cli snapshot` | Create world form has: name input (required), description input, "Create World" button |
| T2.3.2 | Fill name | `playwright-cli fill <name-ref> "Regression Test World"` | Input populated |
| T2.3.3 | Fill description | `playwright-cli fill <desc-ref> "Created by E2E regression test"` | Input populated |
| T2.3.4 | Click Create | `playwright-cli click <create-btn-ref>` | Button shows loading state (disabled), then page redirects to `/world/<newID>` |
| T2.3.5 | Screenshot | `playwright-cli screenshot` | Now on world view page — overlay visible with world name "Regression Test World" |
| T2.3.6 | Navigate back | `playwright-cli navigate http://localhost:8080` | Returns to lobby |
| T2.3.7 | Verify world appears | `playwright-cli snapshot` | "Regression Test World" now in the world list |

**Source**: `harness/views/lobby/lobby.templ:42-51` — form with `data-on-click__prevent={ datastar.PostSSE("/world/create") }`

#### T2.4: Chat Panel

| # | Step | Command | Expected |
|---|------|---------|----------|
| T2.4.1 | Verify chat panel | `playwright-cli snapshot` | "Global Chat" heading, `#chat-log` div, chat input with placeholder "Type a message...", "Send" button |
| T2.4.2 | Check SSE connection | `playwright-cli network` | GET request to `/events` (SSE stream) established |
| T2.4.3 | Type message | `playwright-cli fill <chat-input-ref> "regression test message"` | Input populated |
| T2.4.4 | Send message | `playwright-cli click <send-btn-ref>` | Button briefly disabled (fetching indicator) |
| T2.4.5 | Verify message | `playwright-cli snapshot` | New message appears in `#chat-log` with username, content "regression test message", and timestamp |
| T2.4.6 | Screenshot | `playwright-cli screenshot` | Chat message visible in the chat panel |

**Source**: `harness/views/chat/chat_input.templ` — `data-bind-chat_text`, `data-on-click={ datastar.PostSSE("/api/chat") }`

---

### T3: World View Page

**URL**: `/world/<worldID>` (navigate to the world created in T2.3, or any existing world)

#### T3.1: Page Structure

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.1.1 | Take snapshot | `playwright-cli snapshot` | Page has: `#game-frame` iframe, `#harness-overlay` div with overlay content |
| T3.1.2 | Check SSE | `playwright-cli network` | GET request to `/world/<id>/events` (SSE stream) established |
| T3.1.3 | Screenshot | `playwright-cli screenshot` | Overlay visible over game area — top bar, chat panel, bottom bar |
| T3.1.4 | Console check | `playwright-cli console` | No new JS errors |

**Source**: `harness/views/world/world.templ`

#### T3.2: Top Bar

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.2.1 | Verify top bar | `playwright-cli snapshot` | Contains: "Creative Mode" brand, world name, "Tree" button, "Lobby" link, "—" (minimize) button |

**Source**: `harness/views/world/overlay.templ:30-42`

#### T3.3: Overlay Minimize / Expand

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.3.1 | Click minimize | `playwright-cli click <minimize-btn-ref>` | Expanded overlay hides, minimized "CM" button appears |
| T3.3.2 | Screenshot | `playwright-cli screenshot` | Only small "CM" button visible, game area unobstructed |
| T3.3.3 | Click expand | `playwright-cli click <cm-btn-ref>` | Full overlay reappears |
| T3.3.4 | Screenshot | `playwright-cli screenshot` | Overlay fully visible again |

**Source**: `harness/views/world/overlay.templ:12-27` — `data-show="$overlay_expanded"` / `data-show="!$overlay_expanded"`

#### T3.4: Checkpoint Tree

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.4.1 | Verify tree hidden | `playwright-cli snapshot` | Checkpoint tree div has `style="display: none;"` (default hidden) |
| T3.4.2 | Click Tree button | `playwright-cli click <tree-btn-ref>` | Checkpoint tree becomes visible |
| T3.4.3 | Snapshot tree | `playwright-cli snapshot` | Tree shows: "Checkpoints" heading, tree nodes with status dots, checkpoint names/prompts |
| T3.4.4 | Screenshot | `playwright-cli screenshot` | Checkpoint tree visible with at least a "Root" node |
| T3.4.5 | Toggle tree off | `playwright-cli click <tree-btn-ref>` | Tree hides again |

**Source**: `harness/views/world/checkpoint_tree.templ` — `data-show="$show_checkpoint_tree"`

#### T3.5: Chat Tabs

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.5.1 | Verify tabs | `playwright-cli snapshot` | Three tabs: "Global", "World", "Lineage". Chat log visible. Chat input visible. |
| T3.5.2 | Click World tab | `playwright-cli click <world-tab-ref>` | World tab gets `tab-active` class |
| T3.5.3 | Click Lineage tab | `playwright-cli click <lineage-tab-ref>` | Lineage tab active. `#lineage-view` shown. Chat input bar hidden. |
| T3.5.4 | Screenshot | `playwright-cli screenshot` | Lineage view visible (may be empty or show ancestry) |
| T3.5.5 | Click Global tab | `playwright-cli click <global-tab-ref>` | Back to global chat. Chat input visible again. |

**Source**: `harness/views/chat/chat.templ:8-21` — tab buttons with `CE.SelectTab()`, `CE.SelectLineageTab()`

#### T3.6: Chat in World View

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.6.1 | Type message | `playwright-cli fill <chat-input-ref> "world chat test"` | Input populated |
| T3.6.2 | Send message | `playwright-cli click <send-btn-ref>` | Message sent |
| T3.6.3 | Verify message | `playwright-cli snapshot` | Message appears in `#chat-log` |

#### T3.7: Prompt Bar

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.7.1 | Verify prompt bar | `playwright-cli snapshot` | Bottom bar has: prompt input ("Describe what to build..."), "Build" button, build status display |
| T3.7.2 | Verify initial status | `playwright-cli snapshot` | Build status shows "idle" |
| T3.7.3 | Fill prompt | `playwright-cli fill <prompt-input-ref> "make a spinning cube"` | Input populated |
| T3.7.4 | Screenshot | `playwright-cli screenshot` | Prompt input filled, Build button enabled |

**Note**: Clicking Build triggers a real Claude session and WASM build. Only click if you intend to test the full build pipeline.

**Source**: `harness/views/world/overlay.templ:44-63` — `data-bind-prompt_text`, `data-on-click={ datastar.PostSSE("/world/<id>/prompt") }`

#### T3.8: Build Status Lifecycle (SSE-driven)

> **Note**: These status transitions are driven by server-side events from the Claude orchestrator and build pipeline. To trigger them in testing, you need either:
> - A real Claude session (submit a prompt via the Build button)
> - Manual event injection via the EventBus (requires code modification or API endpoint)
>
> The expected lifecycle is: `idle` → `editing` → `compiling` → `ready` or `failed`

| Status | CSS Class | Triggered By | Visual |
|--------|-----------|-------------|--------|
| `idle` | `status-idle` | Default / page load | Neutral |
| `editing` | `status-editing` | `claude.tool_use.pre` event | Active indicator |
| `compiling` | `status-compiling` | `claude.session_stopped` event | Progress indicator |
| `ready` | `status-ready` | `build.completed` event | Success + "Build ready" notification in chat with Play button |
| `failed` | `status-failed` | `build.failed` event | Error + "Build failed: ..." notification in chat |
| `rate_limited` | `status-rate-limited` | `claude.rate_limited` event | Warning |

**Source**: `harness/internal/server/events.go:234-296` — `handleWorldEvent` switch on event types

#### T3.9: Lobby Navigation

| # | Step | Command | Expected |
|---|------|---------|----------|
| T3.9.1 | Click Lobby link | `playwright-cli click <lobby-link-ref>` | Navigates to `/` — lobby page |
| T3.9.2 | Verify lobby | `playwright-cli snapshot` | Back on lobby with world list and chat |

---

### T4: Admin Page

**URL**: `/admin/users` (admin role required)

#### T4.1: Navigation

| # | Step | Command | Expected |
|---|------|---------|----------|
| T4.1.1 | From lobby, click Admin | `playwright-cli click <admin-btn-ref>` | Navigates to `/admin/users` |
| T4.1.2 | Snapshot | `playwright-cli snapshot` | "Admin — User Management" heading, "Back to Lobby" link, user list |
| T4.1.3 | Screenshot | `playwright-cli screenshot` | Admin page renders with user rows |

#### T4.2: User List

| # | Step | Command | Expected |
|---|------|---------|----------|
| T4.2.1 | Verify user rows | `playwright-cli snapshot` | Each user row: avatar (if set), username, role badge (`admin`/`user`/`pending`) |
| T4.2.2 | Verify admin user | `playwright-cli snapshot` | Current admin user shows with `role-admin` badge class |

#### T4.3: Approve/Reject (if pending users exist)

| # | Step | Command | Expected |
|---|------|---------|----------|
| T4.3.1 | Verify pending buttons | `playwright-cli snapshot` | Pending users have "Approve" (btn-primary) and "Reject" (btn-danger) buttons |
| T4.3.2 | Click Approve | `playwright-cli click <approve-btn-ref>` | User's role changes to "user", approve/reject buttons disappear |
| T4.3.3 | Screenshot | `playwright-cli screenshot` | Role badge updated |

**Note**: Reject deletes the user and all their data (sessions, positions, prompt history, messages). Use caution.

**Source**: `harness/views/admin/admin.templ:24-37`

#### T4.4: Back to Lobby

| # | Step | Command | Expected |
|---|------|---------|----------|
| T4.4.1 | Click Back | `playwright-cli click <back-btn-ref>` | Navigates to `/` |
| T4.4.2 | Verify lobby | `playwright-cli snapshot` | Back on lobby page |

---

### T5: Auth Flows

#### T5.1: Logout

| # | Step | Command | Expected |
|---|------|---------|----------|
| T5.1.1 | From lobby, snapshot | `playwright-cli snapshot` | Find Logout button |
| T5.1.2 | Click Logout | `playwright-cli click <logout-btn-ref>` | Form POST to `/auth/logout`, redirects to login page |
| T5.1.3 | Verify login page | `playwright-cli snapshot` | Login page with "Sign in with GitHub" link |
| T5.1.4 | Screenshot | `playwright-cli screenshot` | Login page rendered |

**Source**: `harness/views/lobby/lobby.templ:24-26` — `<form method="POST" action="/auth/logout">`

#### T5.2: Session Required

| # | Step | Command | Expected |
|---|------|---------|----------|
| T5.2.1 | While logged out, navigate to lobby | `playwright-cli navigate http://localhost:8080` | Redirects to login page (no session cookie) |
| T5.2.2 | Navigate to world | `playwright-cli navigate http://localhost:8080/world/test` | Redirects to login page |
| T5.2.3 | Navigate to admin | `playwright-cli navigate http://localhost:8080/admin/users` | Redirects to login page |

#### T5.3: Re-authenticate

| # | Step | Command | Expected |
|---|------|---------|----------|
| T5.3.1 | Click sign in | `playwright-cli click <signin-ref>` | OAuth flow begins |
| T5.3.2 | Complete OAuth | (manual in browser) | Returns to lobby as authenticated user |
| T5.3.3 | Verify lobby | `playwright-cli snapshot` | Lobby page with username visible |

---

### T6: Error Cases

#### T6.1: Invalid World ID

| # | Step | Command | Expected |
|---|------|---------|----------|
| T6.1.1 | Navigate to fake world | `playwright-cli navigate http://localhost:8080/world/nonexistent-id-12345` | Error response — 404 "world not found" or error page |
| T6.1.2 | Console check | `playwright-cli console` | Check for error details |
| T6.1.3 | Screenshot | `playwright-cli screenshot` | Error state visible |

#### T6.2: Non-Existent Route

| # | Step | Command | Expected |
|---|------|---------|----------|
| T6.2.1 | Navigate to invalid URL | `playwright-cli navigate http://localhost:8080/this-does-not-exist` | 404 response |
| T6.2.2 | Screenshot | `playwright-cli screenshot` | 404 page or blank |

#### T6.3: Unauthorized Admin Access

| # | Step | Command | Expected |
|---|------|---------|----------|
| T6.3.1 | As non-admin user, navigate to admin | `playwright-cli navigate http://localhost:8080/admin/users` | 403 "admin access required" |
| T6.3.2 | Screenshot | `playwright-cli screenshot` | Access denied response |

**Note**: This test requires logging in as a non-admin user. If only one user exists (the admin), this test cannot be run.

---

### T7: Cross-Cutting Checks

Run these checks across all pages visited during the test suite.

#### T7.1: Console Errors

| # | Step | Command | Expected |
|---|------|---------|----------|
| T7.1.1 | Check after each page | `playwright-cli console error` | No JS errors beyond favicon.ico 404 |

#### T7.2: SSE Connections

| # | Step | Command | Expected |
|---|------|---------|----------|
| T7.2.1 | On lobby page | `playwright-cli network` | Active SSE connection to `/events` |
| T7.2.2 | On world page | `playwright-cli network` | Active SSE connection to `/world/<id>/events` |

#### T7.3: Loading Indicators

| # | Step | Command | Expected |
|---|------|---------|----------|
| T7.3.1 | Create world button | During submit, button shows `disabled` attribute via `data-attr-disabled="$fetching"` |
| T7.3.2 | Chat send button | During submit, button shows `disabled` attribute |
| T7.3.3 | Build button | During submit, button shows `disabled` attribute |

---

## Cleanup

After running the test suite:

```bash
playwright-cli close
```

If a test world was created ("Regression Test World"), it can be left in place or removed manually from the SQLite database.

---

## Quick Reference: Page → Route → Template

| Page | Route | Template | Auth Required |
|------|-------|----------|---------------|
| Login | `/` (no session) | `views/login/login.templ` | No |
| Pending | `/` (pending role) | `views/pending/pending.templ` | Session only |
| Lobby | `/` (approved) | `views/lobby/lobby.templ` | Approved |
| World View | `/world/:worldID` | `views/world/world.templ` | Approved |
| Admin | `/admin/users` | `views/admin/admin.templ` | Admin |

## Quick Reference: SSE Event → UI Update

| Event | Signal Change | Chat Patch |
|-------|--------------|------------|
| `chat.message` | — | `Message()` appended to `#chat-log` |
| `player.joined` | — | `SystemNotification("X joined")` |
| `player.left` | — | `SystemNotification("X left")` |
| `claude.tool_use.pre` | `build_status: "editing"` | — |
| `claude.session_stopped` | `build_status: "compiling"` | — |
| `build.completed` | `build_status: "ready"` | `BuildReadyNotification()` with Play button |
| `build.failed` | `build_status: "failed"` | `SystemNotification("Build failed: ...")` |
| `claude.rate_limited` | `build_status: "rate_limited"` | — |

## Quick Reference: Datastar Signals

**Lobby signals** (`views/lobby/signals.go`):
```json
{ "chat_text": "" }
```

**World overlay signals** (`views/world/signals.go`):
```json
{
  "current_world_id": "<worldID>",
  "current_checkpoint_id": "<cpID>",
  "build_status": "idle",
  "prompt_text": "",
  "chat_text": "",
  "overlay_expanded": true,
  "active_tab": "global",
  "show_checkpoint_tree": false,
  "unread_count": 0,
  "rate_limit_retry_at": 0
}
```
