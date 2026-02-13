# Creative Mode

Multiplayer creative sandbox — Go harness server + Bevy/WASM game client.

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `harness/` | Go server (Echo + SQLite + Datastar + templ) — see `harness/CLAUDE.md` |
| `templates/3d/` | 3D Bevy/Lightyear game template — see `templates/3d/CLAUDE.md` |
| `templates/2d/` | 2D Bevy room-based template — see `templates/2d/CLAUDE.md` |
| `scripts/` | Build, format, and setup scripts |
| `context/` | Reference code (gitignored) |
| `thoughts/` | Plans, reviews, and notes |

## Running the Server

**IMPORTANT: Always run the harness in Docker, never directly on the host.**

Running `go run .` on the host skips `DEV_MODE=true` and killing it can destroy tmux sessions that manage game servers. Use Docker:

| Command | Purpose |
|---------|---------|
| `just live` | Docker + host file watcher + Tailwind (recommended for dev) |
| `just up` | Docker container only |
| `just down` | Stop Docker container |

All commands run from `harness/`.

## Skills

### `playwright-cli` — Autonomous Browser Debugging

The `.claude/skills/playwright-cli/` skill enables browser automation for debugging the harness UI.

**Setup**: `just setup-playwright` (installs CLI + generates skill)

**Quick reference**:
```bash
playwright-cli open http://localhost:8080 --headed --persistent  # launch browser (reuses session)
playwright-cli snapshot                      # get element refs
playwright-cli click e15                     # interact by ref
playwright-cli screenshot                    # capture to .playwright-cli/
playwright-cli console error                 # check JS errors
playwright-cli network                       # inspect requests
playwright-cli close                         # clean up
```

**Important flags** (CLI-only, not supported in config file):
- `--headed` — opens a visible browser window (config `headless: false` is ignored)
- `--persistent` — stores cookies/profile in `~/Library/Caches/ms-playwright/daemon/`. Session cookie lasts 7 days, so after one manual OAuth login, future sessions are authenticated automatically.

**Config**: `playwright-cli.json` at project root (`.playwright-cli/` output, 30s nav timeout for Docker cold starts).

### E2E Testing Tips

**Workflow per page**: `snapshot` → interact → `console error` → `screenshot` → read outputs. Always check console errors after navigation to catch regressions early.

**Datastar + Playwright interop**: Playwright's `fill` and `keyboard.type()` do NOT update Datastar signal bindings (`data-bind-*`). To test form submission, use `run-code` with `page.evaluate` to call `fetch()` directly:
```bash
playwright-cli run-code "async page => { await page.evaluate(async () => { await fetch('/api/chat', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({chat_text: 'test'})}); }); }"
```

**Verifying SSE connections**: `playwright-cli network` shows active SSE streams as `[GET] /events => [200] OK`. Check this after page load to confirm `data-init` attributes are working.

**Cookie management for auth testing**: Use `cookie-delete session` to simulate logged-out state, then navigate to verify middleware redirects. With `--persistent`, GitHub OAuth auto-completes on re-login, so you can't easily observe the login page after middleware redirects — delete cookies and navigate to `/` directly instead.

**Reading snapshots**: Element refs like `[ref=e15]` are stable within a snapshot but change between snapshots. Always `snapshot` before interacting. The `[active]` annotation on elements indicates CSS active state (e.g., selected tab).

**Screenshots are images**: `playwright-cli screenshot` saves a PNG. Use the Read tool on the PNG path to view it — Claude Code is multimodal and can visually inspect screenshots.

## Build & Check

```bash
just check          # verify Go + Rust + WASM all compile
just fmt            # format all code
just setup          # run setup (includes playwright-cli)
```
