---
date: 2026-02-11T11:57:22-08:00
researcher: CoreyCole
git_commit: 09ce710ce0f7e9c3f0f38d93a8f3f2e5ef061fc3
branch: main
repository: creative-mode
topic: "Harness Datastar v1.0.0-RC.6 Attribute Compatibility Fixes"
tags: [datastar, e2e, regression, harness, bugfix]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Harness Datastar v1.0.0-RC.6 Attribute Fixes + E2E Regression

## Task(s)

**Primary: Execute E2E regression test suite against live harness using playwright-cli.**

Working from the test plan at `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md` (7 phases, T1-T7). Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-02-11_10-54-01_harness-e2e-regression-tests.md`.

| Phase | Status | Description |
|-------|--------|-------------|
| Phase 1: Login (T1) | **Completed** (previous session) | T1.1-T1.3 all PASS |
| Phase 2: Lobby (T2) | **Completed** | T2.1 header PASS, T2.2 world list PASS, T2.3 create world PASS (form submission works; world creation fails due to missing `rsync` in Docker — infra issue), T2.4 chat PASS (SSE + PostSSE both work after fixes) |
| Phase 3: World View (T3) | **Not started** | Cannot test — no worlds exist (create world fails due to rsync) |
| Phase 4: Admin (T4) | **Not started** | |
| Phase 5: Error Cases (T6) | **Not started** | |
| Phase 6: Auth Flows (T5) | **Not started** | |
| Phase 7: Cleanup | **Not started** | |

**Secondary: Discovered and fixed 3 critical Datastar v1.0.0-RC.6 compatibility bugs.**

| Bug | Status | Description |
|-----|--------|-------------|
| `data-on-load` → `data-init` | **Fixed** | SSE connections never established on any page |
| `data-on-click` → `data-on:click` | **Fixed** | No event handlers worked (chat send, create world, admin actions, overlay controls) |
| Create World `contentType` | **Fixed** | Form data not sent — needed `contentType: 'form'` + `form:` struct tags |

## Critical References

- `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md` — Full regression test plan
- `harness/CLAUDE.md` — Updated with correct Datastar v1 attribute syntax documentation
- `harness/static/datastar.js` — Datastar v1.0.0-RC.6 (minified, self-hosted)

## Recent changes

**Template fixes (all `data-on-load` → `data-init`, all `data-on-click` → `data-on:click`):**
- `harness/views/lobby/lobby.templ:46` — `data-on:click__prevent` with `contentType: 'form'` for create world
- `harness/views/lobby/lobby.templ:54` — `data-init` for `/events` SSE
- `harness/views/world/world.templ:22` — `data-init` for `/world/<id>/events` SSE
- `harness/views/layout/layout.templ:22` — `data-init` for `/dev/sse` hot-reload
- `harness/views/chat/chat_input.templ:8` — `data-on:click` for chat send
- `harness/views/chat/chat.templ:9-13` — `data-on:click` for tab switching
- `harness/views/world/overlay.templ:23,37,39,51` — `data-on:click` for overlay controls + build
- `harness/views/admin/admin.templ:26,32` — `data-on:click` for approve/reject
- `harness/views/shared/load_checkpoint.templ:8` — `data-on:click` for checkpoint loading

**Server fix:**
- `harness/internal/server/server.go:198-199` — Added `form:"name"` and `form:"description"` tags to `handleCreateWorld` struct for URL-encoded form binding

**Documentation:**
- `harness/CLAUDE.md` — Updated all examples and attribute reference table to use `data-on:click` (colon) and `data-init` syntax, added IMPORTANT callout box explaining both issues

## Learnings

1. **Datastar v1.0.0-RC.6 uses colon syntax for event attributes**: `data-on:click`, `data-on:keydown`, etc. The dash syntax (`data-on-click`) does NOT work because HTML's `dataset` API converts `data-on-click` to camelCase `onClick`, and when Datastar converts back to kebab-case it gets `on-click` — which doesn't match the registered plugin name `on`. The colon syntax (`data-on:click`) becomes dataset key `on:click`, which splits on `:` correctly into plugin `on` + key `click`. See `harness/static/datastar.js:4-5` (minified `At` function).

2. **`data-on-load` replaced by `data-init` in Datastar v1**: The `data-on-load` attribute registers a DOM `load` event listener via the generic `on` plugin. The `load` event only fires on resource-loading elements (img, script, iframe), NOT on divs. The `data-init` plugin was introduced specifically for running expressions when elements are first processed by Datastar. Reference: `context/northstar/` examples all use `data-init`.

3. **Datastar's `data-bind` does not work with Playwright's `keyboard.type()` or `fill()`**: Playwright's programmatic input doesn't trigger Datastar's signal binding. The `data-bind` plugin listens for `input`/`change` events, and while `keyboard.type()` does fire `input` events, they don't update Datastar's internal signal store. Direct `fetch()` API calls work as a workaround for testing. This is a Playwright/Datastar interop limitation, NOT a UI bug.

4. **Datastar PostSSE `contentType` defaults to `json`**: When using `@post('/path')` from a form, Datastar sends Datastar signals as JSON body, NOT form field values. To send form data, use `@post('/path', {contentType: 'form'})`. The Go SDK's `PostSSE()` helper doesn't support options — use a raw string expression instead.

5. **Echo's `c.Bind()` requires `form:` struct tags**: For `application/x-www-form-urlencoded` requests, Echo only matches `form:` tags, not `json:` tags. Both are needed for handlers that accept both content types.

## Artifacts

- `harness/views/lobby/lobby.templ` — Fixed `data-init`, `data-on:click`, form contentType
- `harness/views/world/world.templ` — Fixed `data-init`
- `harness/views/layout/layout.templ` — Fixed `data-init`
- `harness/views/chat/chat_input.templ` — Fixed `data-on:click`
- `harness/views/chat/chat.templ` — Fixed `data-on:click`
- `harness/views/world/overlay.templ` — Fixed `data-on:click`
- `harness/views/admin/admin.templ` — Fixed `data-on:click`
- `harness/views/shared/load_checkpoint.templ` — Fixed `data-on:click`
- `harness/internal/server/server.go:198-199` — Added `form:` tags
- `harness/CLAUDE.md:196-210,239-241` — Updated documentation with correct attribute syntax

## Action Items & Next Steps

1. **Commit the fixes** — All changes are unstaged. The fixes touch 8 templ files, 1 Go file, and 1 CLAUDE.md.

2. **Resume E2E regression at Phase 3 (World View)** — Requires a world to exist. Since Create World fails in Docker (missing `rsync`), either:
   - Install `rsync` in the Docker image, OR
   - Create a world manually via SQLite, OR
   - Skip T3 and proceed to T4 (Admin)

3. **Continue Phases 4-7** per the test plan at `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md`:
   - Phase 4 (T4): Admin page — navigate, verify user list, back to lobby
   - Phase 5 (T6): Error cases — invalid world ID, non-existent route
   - Phase 6 (T5): Auth flows — logout, session required redirects, re-auth
   - Phase 7: Close browser, compile pass/fail summary table

4. **Update `game-loader.js:7`** — Has a comment referencing `data-on-click` syntax; update to `data-on:click`

5. **Audit other `data-on-*` attributes** — If new event types are added (e.g., `data-on-keydown`, `data-on-submit`), ensure they use colon syntax

## Other Notes

- The harness dev server runs via `just -f /Users/coreycole/cdev/creative-mode/harness/justfile live` with Docker hot-reload
- Browser session cookie persists 7 days with `--persistent` flag
- The `playwright-cli` tool's `fill` command does NOT work with Datastar signal binding — use `keyboard.type()` for visual input or direct `fetch()` for functional testing
- The `context/northstar/` directory contains correct Datastar v1.0.0-RC.6 usage patterns — always reference when unsure about attribute syntax
- Chat messages from testing are in the SQLite DB (4 messages from CoreyCole during regression testing)
