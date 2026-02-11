---
date: 2026-02-11T12:14:59-08:00
researcher: CoreyCole
git_commit: b31b30e0216391d885a740ebbb6e7a7738e86b32
branch: main
repository: creative-mode
topic: "E2E Regression Complete — Multi-User Testing & Issue Planning"
tags: [e2e, regression, testing, playwright-cli, multi-user, datastar, bugfix]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: E2E Regression Complete — Multi-User Testing & Issue Planning

## Task(s)

**Primary: Complete E2E regression test suite (Phases 3-7) against live harness using playwright-cli.**

All phases are now complete. Working from the test plan at `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md`.

| Phase | Status | Summary |
|-------|--------|---------|
| Phase 1: Login (T1) | **Completed** (earlier session) | T1.1-T1.3 all PASS |
| Phase 2: Lobby (T2) | **Completed** (earlier session) | T2.1-T2.4 all PASS |
| Phase 3: World View (T3) | **Completed** | T3.1-T3.9 all PASS (world created via SQLite workaround) |
| Phase 4: Admin (T4) | **Completed** | T4.1-T4.4 PASS, T4.3 SKIP (no pending users) |
| Phase 5: Error Cases (T6) | **Completed** | T6.1-T6.2 PASS, T6.3 SKIP (single admin user) |
| Phase 6: Auth Flows (T5) | **Completed** | T5.1-T5.3 PASS (logout bug found & fixed) |
| Phase 7: Summary | **Completed** | 22 PASS, 3 SKIP, 0 FAIL |

**Secondary: Found and fixed logout redirect loop bug.**

**Next: Plan multi-user testing strategy and address noted issues.**

## Critical References

- `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md` — Full regression test plan (T1-T7)
- `CLAUDE.md` — Updated with E2E testing tips section for playwright-cli
- `harness/CLAUDE.md` — Datastar v1 attribute syntax documentation

## Recent changes

- `harness/internal/auth/auth.go:254` — Fixed logout redirect: changed `http.StatusTemporaryRedirect` (307) to `http.StatusSeeOther` (303), and target from `/auth/github/login` to `/`
- `harness/static/game-loader.js:7` — Fixed comment: `data-on-click` to `data-on:click`
- `CLAUDE.md:40-63` — Added "E2E Testing Tips" section with Datastar/Playwright interop guidance

## Learnings

1. **Logout redirect loop root cause**: `HandleLogout` at `harness/internal/auth/auth.go:254` used HTTP 307 (Temporary Redirect), which preserves the POST method. Combined with redirecting to `/auth/github/login`, this POSTed to the OAuth endpoint, causing an infinite redirect. Fix: 303 See Other (switches to GET) + redirect to `/` (login page, not OAuth start).

2. **Playwright `fill`/`type` do NOT update Datastar signals**: Datastar's `data-bind` listens for `input`/`change` events, but even though Playwright fires these events, Datastar's internal signal store doesn't update. Workaround: use `playwright-cli run-code` with `page.evaluate(() => fetch(...))` for functional testing of form submissions.

3. **World creation can be bypassed via SQLite for testing**: The `/world/<id>` page only requires DB records (`worlds` + `checkpoints` tables) to render. No filesystem artifacts needed — the iframe just shows empty. Insert directly:
   ```sql
   INSERT INTO worlds (id, name, description, created_by) VALUES ('<id>', '<name>', '<desc>', '<user_id>');
   INSERT INTO checkpoints (id, world_id, status, dir_path, created_by) VALUES ('<cp_id>', '<world_id>', 'ready', '/app/data/worlds/<world_id>/<cp_id>', '<user_id>');
   ```

4. **Persistent profile auto-completes OAuth**: With `--persistent`, the browser profile stores GitHub's OAuth authorization. After logout, navigating to any session-protected route triggers `SessionMiddleware` → redirect to `/auth/github/login` → OAuth auto-completes → new session created immediately. This means you can't observe the login page via middleware redirects unless you manually delete the session cookie first.

5. **Duplicate chat messages in world view**: When sending a chat message from the world page, it appears twice in the chat log. This is because the world SSE handler subscribes to both global and world channels, and chat messages are published to the global channel — so both the lobby SSE and the world SSE deliver the same message.

## Artifacts

- `CLAUDE.md:40-63` — E2E testing tips for playwright-cli
- `harness/internal/auth/auth.go:254` — Logout redirect fix
- `harness/static/game-loader.js:7` — Comment fix
- Commits: `4efde46` (Datastar v1 attribute fixes), `b31b30e` (Logout redirect fix)

## Action Items & Next Steps

### 1. Plan multi-user E2E testing

The 3 skipped tests all require multiple users with different roles:
- **T4.3 (Approve/Reject)**: Needs a pending user to test approve/reject buttons
- **T6.3 (Unauthorized Admin)**: Needs a non-admin user to test 403 on `/admin/users`
- **T5.x (Session isolation)**: Verifying one user's session doesn't leak to another

Options to explore:
- **Create a test user directly in SQLite** with role `pending` or `user`, plus a fake session cookie — then use `playwright-cli cookie-set` to impersonate
- **Use a second browser session** (`playwright-cli -s=user2 open ...`) with a different GitHub account
- **Add a dev-mode endpoint** that creates test users without OAuth (behind `DEV_MODE=true` flag)

### 2. Fix duplicate chat messages in world view

Root cause: `harness/internal/server/events.go` — the world SSE handler subscribes to both `SubscribeGlobal()` and `Subscribe(worldID)`. Chat messages are published via `PublishGlobal()`, so the world SSE handler receives them on the global channel. But the chat log element `#chat-log` is shared between the lobby and world templates, so messages get patched into the same element from two different SSE connections.

Possible fixes:
- Only subscribe to global channel in the world SSE handler (don't duplicate)
- Filter chat messages out of one channel
- Use separate element IDs for global vs world chat logs

### 3. Fix Create World in Docker (rsync missing)

`harness/internal/world/manager.go:69-79` uses `rsync -a --exclude=target` to copy the template. The Docker image doesn't include rsync. Options:
- Add `rsync` to the Dockerfile (`apt-get install rsync`)
- Replace rsync with Go's `os.CopyFS` or a pure-Go copy implementation
- Use `cp -a` as a simpler alternative

### 4. Add styled HTML error pages

Currently 404/error responses return raw JSON (`{"message":"Not Found"}`). Should render a templ error page with navigation back to lobby/login.

## Other Notes

- The harness dev server runs via `just -f /Users/coreycole/cdev/creative-mode/harness/justfile live` with Docker hot-reload
- The SQLite DB is at `/Users/coreycole/cdev/creative-mode/data/creative-mode.db` (host path, bind-mounted to `/app/data/` in container)
- There is one user: CoreyCole (admin, ID `dc495687-350c-442c-a0a0-bd0d41c37b0a`)
- There is one world: "Regression Test World" (ID `d39012bc`) with one root checkpoint (`root0001`), created via SQLite
- The `context/northstar/` directory has correct Datastar v1 patterns — always reference when unsure about attribute syntax
- Previous handoff with full Datastar fix details: `thoughts/CoreyCole/handoffs/general/2026-02-11_11-57-22_harness-datastar-v1-attribute-fixes.md`
