# E2E Regression Test Playbook

Runnable browser-based regression tests for the harness UI, executed via `playwright-cli`. Designed for Claude Code sessions to run against a live harness with `DEV_MODE=true`.

## How to Use This Playbook

1. Start the harness (`just live` or `just dev` with `DEV_MODE=true`)
2. Walk through each test section in order
3. After every navigation or action: check `console error` for regressions
4. Record any failures with screenshots and console output
5. When adding features, add a new test section to this playbook

**Acceptable baseline errors**: `favicon.ico` 404 (no favicon configured).

---

## Setup

### Start Harness

```bash
just -f /Users/coreycole/cdev/creative-mode/harness/justfile live
```

Wait for healthy:

```bash
curl -s http://localhost:8080/health
# Expected: {"status":"ok"}
```

### Open Browser Sessions

Dev auth enables multi-user testing without GitHub OAuth. Use named sessions for isolation.

```bash
# Admin session
playwright-cli -s=admin open http://localhost:8080 --headed --persistent

# Second user session (for multi-user tests)
playwright-cli -s=user2 open http://localhost:8080 --headed --persistent
```

### Authenticate via Dev Login

For each session, fill the dev login form (only visible in `DEV_MODE`). The form has a username input and a role dropdown (admin/user/pending). The role defaults to "user".

```bash
# Session: admin — use role=admin
playwright-cli -s=admin snapshot
playwright-cli -s=admin fill <username-ref> "test-admin"
playwright-cli -s=admin selectOption <role-ref> "admin"
playwright-cli -s=admin click <login-btn-ref>

# Session: user2 — default role creates a pending user
playwright-cli -s=user2 snapshot
playwright-cli -s=user2 fill <username-ref> "test-user2"
playwright-cli -s=user2 selectOption <role-ref> "pending"
playwright-cli -s=user2 click <login-btn-ref>
```

After login, admin lands on lobby; user2 (pending role) lands on pending page (needs admin approval).

### Approve user2 (from admin session)

```bash
playwright-cli -s=admin goto http://localhost:8080/admin/users
playwright-cli -s=admin snapshot
playwright-cli -s=admin click <approve-btn-ref>   # approve test-user2
playwright-cli -s=user2 goto http://localhost:8080  # user2 now sees lobby
```

---

## T1: Login Page

**URL**: `http://localhost:8080` (unauthenticated)
**Source**: `views/login/login.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T1.1 | Snapshot | `snapshot` | "Creative Mode" heading, "Sign in with GitHub" link visible |
| T1.2 | Dev form visible | `snapshot` | Dev login form with username input, role dropdown (admin/user/pending), "Dev Sign In" button |
| T1.3 | Screenshot | `screenshot` | Login page renders — centered container, heading, sign-in link, dev form |
| T1.4 | Console check | `console error` | No errors (except favicon 404) |

**To reach this state**: delete session cookie or use a fresh session:
```bash
playwright-cli -s=fresh open http://localhost:8080 --headed --persistent
```

---

## T2: Pending Page

**URL**: `http://localhost:8080` (authenticated, pending role)
**Source**: `views/pending/pending.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T2.1 | Login as new user | dev login with unique username + "pending" role | Redirects to pending page |
| T2.2 | Snapshot | `snapshot` | Shows approval-pending message |
| T2.3 | Navigate to lobby | `goto http://localhost:8080` | Still shows pending page (middleware blocks) |
| T2.4 | Console check | `console error` | No errors |

---

## T3: Lobby Page

**URL**: `http://localhost:8080` (authenticated, approved)
**Source**: `views/lobby/lobby.templ`

### T3.1: Header

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T3.1.1 | Snapshot | `snapshot` | Username visible, "Admin" button (admin only), "Logout" button |
| T3.1.2 | Screenshot | `screenshot` | Header renders with user info and nav buttons |

### T3.2: World List

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T3.2.1 | Snapshot | `snapshot` | "Worlds" heading visible. Existing worlds shown as clickable cards |
| T3.2.2 | Screenshot | `screenshot` | World cards render in the worlds panel |

### T3.3: Create World

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T3.3.1 | Snapshot form | `snapshot` | Name input (required), description input, "Create World" button |
| T3.3.2 | Fill name | `fill <name-ref> "E2E Test World"` | Input populated |
| T3.3.3 | Fill description | `fill <desc-ref> "Created by E2E playbook"` | Input populated |
| T3.3.4 | Click Create | `click <create-btn-ref>` | Page redirects to `/world/<id>` |
| T3.3.5 | Screenshot | `screenshot` | Now on world view page with "E2E Test World" |
| T3.3.6 | Navigate back | `goto http://localhost:8080` | Returns to lobby |
| T3.3.7 | Verify listed | `snapshot` | "E2E Test World" appears in world list |
| T3.3.8 | Console check | `console error` | No new errors |

**Note**: World creation uses Datastar SSE (`data-on:click__prevent={ datastar.PostSSE("/world/create") }`), so `fill` + `click` works directly — the form fields are standard inputs read server-side via `ReadSignals`.

### T3.4: Chat Panel

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T3.4.1 | Snapshot | `snapshot` | "Global Chat" heading, `#chat-log`, chat input ("Type a message..."), "Send" button |
| T3.4.2 | SSE connected | `network` | Active GET to `/events` (SSE stream) |
| T3.4.3 | Send message | `fill <input-ref> "lobby chat test"` then `click <send-ref>` | Button briefly disabled |
| T3.4.4 | Verify message | `snapshot` | Message appears in `#chat-log` with username and content |
| T3.4.5 | Screenshot | `screenshot` | Chat message visible |
| T3.4.6 | Console check | `console error` | No new errors |

**Cross-session check**: After sending from admin session, verify the message appears in user2's lobby chat via snapshot.

---

## T4: World View Page

**URL**: `/world/<worldID>`
**Source**: `views/world/world.templ`, `views/world/overlay.templ`

Navigate to the world created in T3.3 (or any existing world).

### T4.1: Page Structure

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.1.1 | Snapshot | `snapshot` | `#game-frame` iframe, `#harness-overlay` with overlay content |
| T4.1.2 | SSE connected | `network` | Active GET to `/world/<id>/events` |
| T4.1.3 | Screenshot | `screenshot` | Overlay visible: top bar, chat panel, bottom bar |
| T4.1.4 | Console check | `console error` | No new errors |

### T4.2: Top Bar

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.2.1 | Snapshot | `snapshot` | "Creative Mode" brand, world name, "Tree" button, "Lobby" link, minimize "—" button |

### T4.3: Overlay Minimize / Expand

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.3.1 | Click minimize | `click <minimize-btn-ref>` | Overlay hides, small "CM" button appears |
| T4.3.2 | Screenshot | `screenshot` | Only "CM" button visible, game area unobstructed |
| T4.3.3 | Click expand | `click <cm-btn-ref>` | Full overlay reappears |
| T4.3.4 | Screenshot | `screenshot` | Overlay fully visible again |

**Source**: `views/world/overlay.templ` — `data-show="$overlay_expanded"` / `data-show="!$overlay_expanded"`

### T4.4: Checkpoint Tree

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.4.1 | Verify hidden | `snapshot` | Checkpoint tree not visible (default `show_checkpoint_tree: false`) |
| T4.4.2 | Click Tree | `click <tree-btn-ref>` | Tree becomes visible |
| T4.4.3 | Snapshot | `snapshot` | "Checkpoints" heading, tree nodes with "Root" node |
| T4.4.4 | Screenshot | `screenshot` | Checkpoint tree rendered |
| T4.4.5 | Toggle off | `click <tree-btn-ref>` | Tree hides again |

**Source**: `views/world/checkpoint_tree.templ` — `data-show="$show_checkpoint_tree"`

### T4.5: Chat Tabs

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.5.1 | Snapshot | `snapshot` | Three tabs: "Global", "World", "Lineage". Chat log and input visible. |
| T4.5.2 | Click World tab | `click <world-tab-ref>` | World tab gets active state |
| T4.5.3 | Click Lineage tab | `click <lineage-tab-ref>` | Lineage tab active. `#lineage-view` shown. Chat input hidden. |
| T4.5.4 | Screenshot | `screenshot` | Lineage view visible |
| T4.5.5 | Click Global tab | `click <global-tab-ref>` | Back to global chat, input visible again |

**Source**: `views/chat/chat.templ` — tab buttons with `CE.SelectTab()`, `CE.SelectLineageTab()`

### T4.6: Chat in World View

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.6.1 | Fill message | `fill <chat-input-ref> "world chat test"` | Input populated |
| T4.6.2 | Send | `click <send-btn-ref>` | Message sent |
| T4.6.3 | Verify | `snapshot` | Message in `#chat-log` |
| T4.6.4 | Console check | `console error` | No new errors |

### T4.7: Prompt Bar

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.7.1 | Snapshot | `snapshot` | Prompt input ("Describe what to build..."), "Build" button, build status |
| T4.7.2 | Verify idle | `snapshot` | Build status shows "idle" |
| T4.7.3 | Fill prompt | `fill <prompt-ref> "make a spinning cube"` | Input populated |
| T4.7.4 | Screenshot | `screenshot` | Prompt filled, Build button visible |

**Warning**: Clicking Build triggers a real Claude session. Only click if testing the full build pipeline.

### T4.8: Navigate to Lobby

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T4.8.1 | Click Lobby | `click <lobby-link-ref>` | Navigates to `/` |
| T4.8.2 | Verify | `snapshot` | Back on lobby with world list |

---

## T5: Admin Page

**URL**: `/admin/users` (admin role required)
**Source**: `views/admin/admin.templ`

### T5.1: Navigation & Layout

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T5.1.1 | Click Admin | `click <admin-btn-ref>` | Navigates to `/admin/users` |
| T5.1.2 | Snapshot | `snapshot` | "Admin — User Management" heading, "Back to Lobby" link, user list |
| T5.1.3 | Screenshot | `screenshot` | Admin page with user rows |

### T5.2: User List

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T5.2.1 | Snapshot | `snapshot` | User rows with username, role badge (`admin`/`user`/`pending`) |
| T5.2.2 | Admin badge | `snapshot` | Current admin shows `role-admin` badge |

### T5.3: Approve / Reject

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T5.3.1 | Pending user buttons | `snapshot` | "Approve" and "Reject" buttons for pending users |
| T5.3.2 | Click Approve | `click <approve-btn-ref>` | Role changes to "user", buttons disappear |
| T5.3.3 | Screenshot | `screenshot` | Updated role badge |

**Note**: Reject deletes the user and all their data. Use caution.

### T5.4: Back to Lobby

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T5.4.1 | Click Back | `click <back-btn-ref>` | Navigates to `/` |

---

## T6: Auth Flows

### T6.1: Logout

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T6.1.1 | Snapshot lobby | `snapshot` | Logout button visible |
| T6.1.2 | Click Logout | `click <logout-btn-ref>` | POST `/auth/logout`, redirects to login page |
| T6.1.3 | Verify login page | `snapshot` | "Sign in with GitHub" link visible |
| T6.1.4 | Screenshot | `screenshot` | Login page rendered |

**Source**: `views/lobby/lobby.templ` — `<form method="POST" action="/auth/logout">`

### T6.2: Protected Routes Redirect

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T6.2.1 | Navigate to lobby | `goto http://localhost:8080` | Redirects to login (no session) |
| T6.2.2 | Navigate to world | `goto http://localhost:8080/world/any-id` | Redirects to login |
| T6.2.3 | Navigate to admin | `goto http://localhost:8080/admin/users` | Redirects to login |

### T6.3: Re-authenticate

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T6.3.1 | Dev login | fill username, select role, click login | Returns to lobby |
| T6.3.2 | Verify lobby | `snapshot` | Lobby with username visible |

---

## T7: Error Cases

### T7.1: Invalid World ID

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T7.1.1 | Navigate | `goto http://localhost:8080/world/nonexistent-id` | Error response (404 or error page) |
| T7.1.2 | Screenshot | `screenshot` | Error state visible |

### T7.2: Non-Existent Route

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T7.2.1 | Navigate | `goto http://localhost:8080/does-not-exist` | 404 response |
| T7.2.2 | Screenshot | `screenshot` | 404 page |

### T7.3: Unauthorized Admin Access

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T7.3.1 | As non-admin, navigate to admin | `goto http://localhost:8080/admin/users` | 403 "admin access required" |
| T7.3.2 | Screenshot | `screenshot` | Access denied |

**Requires**: a non-admin session (use user2).

---

## T8: Cross-Session (Multi-User)

These tests verify real-time SSE updates across users.

### T8.1: Chat Message Propagation

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T8.1.1 | admin sends lobby message | `-s=admin fill + click` | Message sent |
| T8.1.2 | user2 sees message | `-s=user2 snapshot` | Same message in user2's `#chat-log` |

### T8.2: World List Updates

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| T8.2.1 | admin creates world | `-s=admin` create world flow | World created |
| T8.2.2 | user2 sees world | `-s=user2 snapshot` or `goto` then `snapshot` | New world in user2's list |

---

## Cleanup

```bash
playwright-cli -s=admin close
playwright-cli -s=user2 close
playwright-cli -s=fresh close   # if used
```

Test worlds and users can be left in place or cleaned up by restarting the harness (SQLite is ephemeral in Docker).

---

## Quick Reference

### Page Routing

| Page | Route | Template | Auth |
|------|-------|----------|------|
| Login | `/` (no session) | `views/login/login.templ` | None |
| Pending | `/` (pending role) | `views/pending/pending.templ` | Session |
| Lobby | `/` (approved) | `views/lobby/lobby.templ` | Approved |
| World | `/world/:worldID` | `views/world/world.templ` | Approved |
| Admin | `/admin/users` | `views/admin/admin.templ` | Admin |

### SSE Events

| Event | Signal | Chat Patch |
|-------|--------|------------|
| `chat.message` | — | Message appended to `#chat-log` |
| `player.joined` | — | System notification |
| `player.left` | — | System notification |
| `claude.tool_use.pre` | `build_status: "editing"` | — |
| `claude.session_stopped` | `build_status: "compiling"` | — |
| `build.completed` | `build_status: "ready"` | Build ready + Play button |
| `build.failed` | `build_status: "failed"` | Error notification |
| `claude.rate_limited` | `build_status: "rate_limited"` | — |

### Datastar Form Interop

Playwright `fill` + `click` works for dev login (standard HTML form) and world creation (Datastar reads signals server-side). For chat, `fill` updates the DOM input but may not sync the Datastar signal binding. If chat send fails, use `page.evaluate(fetch(...))` as a workaround:

```bash
playwright-cli run-code "async page => { await page.evaluate(async () => { await fetch('/api/chat', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({chat_text: 'test'})}); }); }"
```

---

## Adding New Tests

When a new feature is added to the harness:

1. **Add a new section** (T9, T10, etc.) following the existing format
2. **Include**: source file references, element selectors, pass criteria
3. **Add SSE events** to the quick reference if the feature uses real-time updates
4. **Add cross-session tests** in T8 if the feature involves multi-user interaction
5. **Note Datastar interop** if the feature uses signal bindings that playwright can't fill directly
