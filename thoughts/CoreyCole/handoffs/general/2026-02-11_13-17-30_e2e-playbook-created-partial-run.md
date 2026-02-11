---
date: 2026-02-11T13:17:30-08:00
researcher: CoreyCole
git_commit: 4a40882d62aea49d99509919399cc478d8d19630
branch: main
repository: creative-mode
topic: "E2E Playbook Created — Partial Run with Regressions Found"
tags: [e2e-testing, playwright, regression, playbook]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: E2E Playbook Created — Partial Regression Run

## Task(s)

| Task | Status |
|------|--------|
| Create `harness/E2E_PLAYBOOK.md` — living E2E testing playbook | **Completed** |
| Run T1: Login Page | **Completed** — PASS (with styles regression) |
| Run T2: Pending Page | **Completed** — PASS |
| Run T3: Lobby Page | **Completed** — world creation FAILS (rsync missing), chat PASS |
| Run T4: World View Page | **Completed** — PASS (overlay, tree, tabs, chat, prompt bar) |
| Run T5: Admin Page | **In Progress** — snapshot + screenshot taken, approve/reject not tested |
| Run T6: Auth Flows | **Planned** |
| Run T7: Error Cases | **Planned** |
| Run T8: Cross-Session (Multi-User) | **Planned** |

## Critical References

- `harness/E2E_PLAYBOOK.md` — the playbook created this session (new file, not yet committed)
- `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md` — original detailed test plan this playbook was based on
- `CLAUDE.md` — playwright-cli usage, E2E testing tips (Datastar form interop, SSE verification)

## Recent changes

- `harness/E2E_PLAYBOOK.md` — **New file**: structured E2E regression playbook with T1-T8 test sections, setup/teardown, quick reference tables, and "Adding New Tests" guide

## Learnings

- **`styles.css` 404 regression on ALL pages**: `harness/static/styles.css` was deleted (`D` in git status) and CSS moved to `harness/static/css/` (new untracked dir), but the layout template still references `/static/styles.css`. All pages render unstyled. The layout templ (`harness/views/layout/layout.templ`) has unstaged modifications.

- **World creation 500 — `rsync` not in Docker container**: `WorldManager.CreateWorld` shells out to `rsync` to copy the template directory. The Docker container doesn't have `rsync` installed. Server log: `"error":"copying template: : exec: \"rsync\": executable file not found in $PATH"`. The existing "Regression Test World" (`/world/d39012bc`) was created before the container was rebuilt.

- **Dev login form changed**: No longer has email field. Now has Username input + role dropdown (user/admin/pending) + "Dev Sign In" button. Selecting "pending" role creates a pending user; "user" creates an approved user; "admin" creates an admin. The playbook's Setup section needs updating to match.

- **Datastar form interop confirmed**: The world create form uses `{contentType: 'form'}` with standard `name` attributes (not signal bindings), so Playwright `fill` should work. The 500 is server-side (rsync), not a Datastar issue. Chat still requires `page.evaluate(fetch(...))` workaround since it uses `data-bind-chat_text`.

- **`playwright-cli` command is `goto` not `navigate`**: The playbook uses `navigate` but the correct command is `goto`. Update playbook references.

- **Admin page avatar rendering**: CoreyCole's GitHub avatar renders as a large image on the admin page (no CSS constraints since styles.css is missing). The admin page shows all users with username + role. Pending users have Approve/Reject buttons.

## Artifacts

- `harness/E2E_PLAYBOOK.md` — the primary deliverable (new, uncommitted)
- `.playwright-cli/` — screenshots and snapshots from this test run (gitignored)

## Action Items & Next Steps

1. **Fix `styles.css` 404 regression**: The layout template references `/static/styles.css` but the file was moved to `static/css/`. Either update the template `<link>` tag in `harness/views/layout/layout.templ` to point to the new CSS path, or restore the file. This blocks all visual testing.

2. **Fix Docker `rsync` missing**: Add `rsync` to the Docker image or change `WorldManager.CreateWorld` to use `cp -r` instead. This blocks world creation testing.

3. **Update playbook**: Fix `navigate` → `goto` throughout. Update Setup section to match current dev login form (username + role dropdown, no email field).

4. **Complete remaining tests (T5-T8)**:
   - T5: Admin — test approve/reject flow (test-pending user exists with pending status)
   - T6: Auth Flows — logout, protected route redirects, re-auth
   - T7: Error Cases — invalid world ID, non-existent routes, unauthorized admin access
   - T8: Cross-Session — chat propagation between admin and user2 sessions

5. **Commit playbook**: After fixes and full run, commit `harness/E2E_PLAYBOOK.md`

## Other Notes

- **Active browser sessions**: `admin` (authenticated, on admin page), `fresh` (authenticated as test-pending, on pending page). Both use `--persistent` cookies.
- **Existing test data**: "Regression Test World" at `/world/d39012bc`, users: CoreyCole (admin), admin (admin), testuser (admin), pending-user (user), test-pending (pending).
- **Chat messages from this session**: "e2e playbook chat test" and "world view chat test" both by admin user — visible in lobby and world chat logs.
- The harness is running via Docker on port 8080 (`just live`).
- Dev login is gated behind `DEV_MODE=true` which Docker Compose sets automatically.
