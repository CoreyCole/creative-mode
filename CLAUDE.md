# Creative Mode

Multiplayer creative sandbox — Go harness server + Bevy/WASM game client.

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `harness/` | Go server (Echo + SQLite + Datastar + templ) — see `harness/CLAUDE.md` |
| `template/` | Bevy/Rust game template — see `template/CLAUDE.md` |
| `scripts/` | Build, format, and setup scripts |
| `context/` | Reference code (gitignored) |
| `thoughts/` | Plans, reviews, and notes |

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

## Build & Check

```bash
just check          # verify Go + Rust + WASM all compile
just fmt            # format all code
just setup          # run setup (includes playwright-cli)
just harness        # run harness dev server
```
