# Playwright CLI Integration for Autonomous Browser Debugging

## Overview

Integrate `@playwright/cli` (Microsoft's token-efficient browser automation CLI for AI agents) to give Claude Code autonomous debugging capabilities for the harness UI. Claude can navigate pages, take screenshots, check console errors, inspect network requests, interact with elements, and iterate — all without filling the context window.

## Current State Analysis

### What Exists

- **Docker dev environment** — harness server runs in Docker, exposed at `localhost:8080` (`harness/docker-compose.yml`)
- **Hot-reload system** — file watcher triggers rebuilds/CSS reloads via SSE (`harness/internal/server/dev.go`)
- **No Node.js at project root** — Go project with no `package.json` (only in gitignored `context/` reference code)
- **No browser testing** — no `e2e/`, `tests/`, or `*_test.go` files
- **No `.claude/skills/` directory** — no existing Claude Code skills
- **`.gitignore` already includes** `node_modules/`
- **Permissions allowlist** in `.claude/settings.local.json` — Bash commands must be explicitly allowed

### Key Constraints

| Constraint | Source | Impact |
|-----------|--------|--------|
| No `package.json` at root | Go project | Global npm install preferred over adding Node.js dep management |
| Harness in Docker | `harness/docker-compose.yml` | Browser runs on host, connects to Docker-exposed port |
| Port 8080 | `harness/internal/server/server.go` | Playwright targets `http://localhost:8080` |
| Stop hook | `.claude/settings.json` | Runs `scripts/check.sh` — playwright setup must not interfere |
| Bash permission gating | `.claude/settings.local.json` | Must add `Bash(playwright-cli:*)` for auto-approval |

## Desired End State

After this plan is implemented:

1. `just setup-playwright` installs the CLI, browser, and Claude Code skill
2. Claude Code auto-discovers the playwright-cli skill from `.claude/skills/playwright-cli/SKILL.md`
3. Claude can run commands like `playwright-cli open http://localhost:8080` without permission prompts
4. Autonomous debugging workflow: navigate → snapshot → interact → screenshot → check console → iterate
5. Screenshots and traces saved to `/tmp/playwright-cli/` (not in project tree)
6. Headed mode by default — developer can watch Claude debug in real-time

### Architecture

```
Host (macOS)
============

Claude Code
  │
  ├── playwright-cli open http://localhost:8080
  │   └── Chromium browser launches (headed)
  │       └── connects to localhost:8080
  │
  ├── playwright-cli snapshot
  │   └── returns compact element refs (e15, e21, e42...)
  │
  ├── playwright-cli click e21
  │   └── clicks element, returns new page state
  │
  ├── playwright-cli screenshot
  │   └── saves PNG to /tmp/playwright-cli/
  │       └── Claude reads image file (multimodal)
  │
  ├── playwright-cli console error
  │   └── returns JS errors from browser console
  │
  └── playwright-cli network
      └── returns failed API calls, 404s, etc.

Docker Container (Debian)
=========================
  harness server on :8080 ◄── Chromium connects via localhost
```

### Why CLI Over MCP

| Factor | MCP Server (`@playwright/mcp`) | CLI (`@playwright/cli`) |
|--------|-------------------------------|------------------------|
| Per-interaction overhead | Full accessibility tree + 26 tool schemas | Compact element refs (e15, e21) |
| Context consumption | Thousands of tokens per action | Minimal — browser state stays external |
| Long sessions | Context fills quickly | Designed for long sessions |
| Tool decision paralysis | 26 tools → Claude wastes tokens deliberating | Agent selects CLI commands directly |
| Setup | MCP server process | Bash commands |

## What We're NOT Doing

- **Not adding `package.json`** — global npm install only, no Node.js dependency management
- **Not modifying Docker/Dockerfile** — Chromium runs on host, not in container
- **Not creating an `e2e/` test suite** — this is for ad-hoc autonomous debugging, not structured testing
- **Not using MCP** — CLI chosen for token efficiency
- **Not adding `@playwright/test`** — the test runner is a separate concern; we only need the CLI

## Implementation Approach

Global install of `@playwright/cli` via npm, with a justfile recipe for reproducibility. The `playwright-cli install --skills` command auto-generates a `SKILL.md` that teaches Claude Code the full command set. A `playwright-cli.json` config file at project root sets sensible defaults (headed mode, error-level console, `/tmp/` output). Bash permissions are pre-approved in `.claude/settings.local.json`.

---

## Phase 1: Installation Infrastructure

### Overview

Add the justfile recipe and run the installation. This installs the CLI globally, downloads Chromium, and generates the Claude Code skill file.

### Changes Required

#### 1. Root Justfile
**File**: `justfile`
**Action**: Modify — add `setup-playwright` recipe, wire into `setup`

Current:
```just
setup:
    ./scripts/setup.sh
```

New:
```just
# Install playwright-cli for autonomous browser debugging
setup-playwright:
    npm install -g @playwright/cli@latest
    playwright-cli install-browser
    playwright-cli install --skills

setup:
    ./scripts/setup.sh
    just setup-playwright
```

**Notes:**
- `npm install -g` installs the CLI globally — no `package.json` needed
- `install-browser` downloads Chromium to `~/.cache/ms-playwright/` (~200MB, one-time)
- `install --skills` creates `.claude/skills/playwright-cli/SKILL.md` in the project

#### 2. Run Installation
**Action**: Execute from project root

```bash
just -f /Users/coreycole/cdev/creative-mode/justfile setup-playwright
```

### Success Criteria

#### Automated Verification:
- [ ] `which playwright-cli` returns a path
- [ ] `playwright-cli --help` prints command reference
- [ ] `.claude/skills/playwright-cli/SKILL.md` exists and is non-empty

---

## Phase 2: Configuration

### Overview

Create the CLI config, add Bash permissions, and update `.gitignore`.

### Changes Required

#### 1. Playwright CLI Config
**File**: `playwright-cli.json`
**Action**: Create at project root

```json
{
  "browserName": "chromium",
  "launchOptions": {
    "headless": false
  },
  "outputDir": "/tmp/playwright-cli",
  "outputMode": "file",
  "console": {
    "level": "error"
  },
  "timeouts": {
    "action": 5000,
    "navigation": 30000
  }
}
```

**Design decisions:**
- **`headless: false`** — Headed mode so the developer can watch Claude navigate. Override per-command with `--headless` if needed.
- **`outputDir: "/tmp/playwright-cli"`** — Artifacts stay out of project tree. macOS `/tmp/` clears on reboot.
- **`outputMode: "file"`** — Screenshots saved as PNG files that Claude reads via its multimodal file reading (more token-efficient than base64 stdout).
- **`console.level: "error"`** — Focused signal by default. Claude can use `playwright-cli console warning` or `console info` for broader capture.
- **`timeouts.navigation: 30000`** — 30s accommodates Docker container cold start and Go build time.

#### 2. Claude Code Permissions
**File**: `.claude/settings.local.json`
**Action**: Modify — add `Bash(playwright-cli:*)` to permissions allow list

```json
{
  "permissions": {
    "allow": [
      ... existing entries ...,
      "Bash(playwright-cli:*)"
    ]
  }
}
```

This pre-approves all `playwright-cli` Bash commands so Claude doesn't prompt on every invocation.

#### 3. Gitignore
**File**: `.gitignore`
**Action**: Modify — add defensive entries

```
# Playwright CLI artifacts (output goes to /tmp/ by config,
# but guard against overrides)
playwright-report/
test-results/
```

### Success Criteria

#### Automated Verification:
- [ ] `cat playwright-cli.json | python3 -m json.tool` validates JSON
- [ ] `grep "playwright-cli" .claude/settings.local.json` finds the permission entry
- [ ] `grep "playwright-report" .gitignore` finds the gitignore entry

---

## Phase 3: Verification

### Overview

Test the full chain: CLI → Chromium → harness UI → screenshot → console capture.

### Testing Steps

#### Prerequisite: Harness Running
```bash
just -f /Users/coreycole/cdev/creative-mode/harness/justfile live
```

#### Smoke Tests:
1. `playwright-cli open http://localhost:8080` — browser window opens, navigates to login page
2. `playwright-cli snapshot` — returns compact element refs for the page
3. `playwright-cli screenshot` — PNG saved to `/tmp/playwright-cli/`
4. `playwright-cli console error` — returns any JS errors (expect none on login page)
5. `playwright-cli network` — shows network requests
6. `playwright-cli close` — browser closes cleanly

#### Interaction Test:
1. `playwright-cli open http://localhost:8080` — navigate to login
2. `playwright-cli snapshot` — identify login button element ref
3. `playwright-cli click <ref>` — click the GitHub OAuth link
4. `playwright-cli screenshot` — capture resulting page

#### Session Test:
1. `playwright-cli -s=debug open http://localhost:8080` — named session
2. `playwright-cli -s=debug snapshot` — verify session persistence
3. `playwright-cli -s=debug close` — clean up

#### Claude Code Integration Test:
1. Start a new Claude Code session in the project
2. Verify Claude mentions playwright-cli skill availability
3. Ask Claude to "take a screenshot of the login page at localhost:8080"
4. Verify Claude uses `playwright-cli` commands autonomously

### Success Criteria

#### Automated Verification:
- [ ] `playwright-cli open http://localhost:8080 && playwright-cli screenshot && playwright-cli close` completes without errors
- [ ] Screenshot file exists in `/tmp/playwright-cli/`

#### Manual Verification:
- [ ] Browser window is visible (headed mode working)
- [ ] Screenshot accurately shows the harness login page
- [ ] Console capture works (no false errors)
- [ ] Claude Code discovers and uses the skill in a new session

---

## Key CLI Commands Reference

| Command | Purpose |
|---------|---------|
| `playwright-cli open <url>` | Launch browser and navigate |
| `playwright-cli snapshot` | Get page structure as compact element refs |
| `playwright-cli screenshot [ref]` | Full page or element screenshot |
| `playwright-cli click <ref>` | Click element by ref |
| `playwright-cli fill <ref> <text>` | Fill form field |
| `playwright-cli type <text>` | Type into focused element |
| `playwright-cli console [level]` | Browser console messages |
| `playwright-cli network` | List network requests |
| `playwright-cli tracing-start/stop` | Record browser trace |
| `playwright-cli -s=<name> <cmd>` | Named session (persistent state) |
| `playwright-cli close` | Close browser |

## Performance Considerations

- **First run**: Chromium download is ~200MB (one-time, cached in `~/.cache/ms-playwright/`)
- **Browser launch**: ~1-2s to start Chromium
- **Snapshot**: ~100ms — compact text, minimal tokens
- **Screenshot**: ~200ms capture + file write; Claude reads the PNG file
- **Token efficiency**: Each CLI call returns only the minimal needed output, unlike MCP which sends full accessibility trees

## References

- [Microsoft playwright-cli GitHub](https://github.com/microsoft/playwright-cli)
- [@playwright/cli npm](https://www.npmjs.com/package/@playwright/cli)
- [SKILL.md source](https://github.com/microsoft/playwright-cli/blob/main/skills/playwright-cli/SKILL.md)
- Docker dev hot-reload plan: `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md`
- Harness architecture: `harness/CLAUDE.md`
