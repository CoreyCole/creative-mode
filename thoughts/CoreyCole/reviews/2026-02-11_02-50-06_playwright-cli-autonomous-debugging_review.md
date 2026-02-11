---
date: 2026-02-11T02:50:06-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 93f9c1e17fc41738df4c6b9e151e0c296e14949d
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-11_02-47-40_playwright-cli-autonomous-debugging.md
status: complete
type: plan_review
---

# Plan Review: Playwright CLI Integration for Autonomous Browser Debugging

### Summary

The plan is well-structured with clear phases, success criteria, and a good "What We're NOT Doing" section. However, it builds on a **2-week-old alpha-quality npm package** (v0.1.0, depending on `playwright@1.59.0-alpha`) without disclosing this risk, and several configuration details appear to be assumed rather than verified against the tool's actual schema.

### Critical Issues (Must Address Before Implementation)

These issues could cause significant problems if not resolved:

1. **Alpha-quality dependency risk is undisclosed**

   - Problem: `@playwright/cli` v0.1.0 was first released January 31, 2026 (~11 days ago). It depends on `playwright@1.59.0-alpha`. The plan presents this as a stable, production-ready tool without mentioning it is pre-release software.
   - Risk: Breaking changes, missing features, or regressions are expected in alpha software. The API surface (commands, config schema, `install --skills` output) could change at any time. Installing `@latest` in the justfile means builds can break unpredictably when new versions are pushed.
   - Suggestion: (a) Explicitly acknowledge the alpha status in the plan. (b) Pin to a specific version (`npm install -g @playwright/cli@0.1.0`) instead of `@latest` for reproducibility. (c) Add a note about expected instability and plan for handling breaking changes.

2. **Unverified `playwright-cli.json` config schema**

   - Problem: The plan proposes specific config properties (`launchOptions.headless`, `outputDir`, `outputMode`, `console.level`, `timeouts.action`, `timeouts.navigation`) but the web search could not independently verify these exact property names against the tool's actual schema. The README confirms that `playwright-cli.json` exists as a config file, but the detailed schema is unclear.
   - Risk: Phase 2 could fail silently or produce unexpected behavior if the config keys don't match what the tool expects. An invalid config might be ignored entirely without error.
   - Suggestion: Before writing the config file, run `playwright-cli --help` or check the tool's actual config schema (e.g., `playwright-cli config --help` or the source code). Verify each property name against the documentation or source. Consider starting with a minimal config and adding properties incrementally.

3. **Claude Code skill auto-discovery mechanism unverified**

   - Problem: The plan assumes that placing a file at `.claude/skills/playwright-cli/SKILL.md` will cause Claude Code to "auto-discover" the skill. The `install --skills` command is confirmed to generate this file, but the plan doesn't verify how Claude Code discovers and activates skills from `.claude/skills/`.
   - Risk: If Claude Code doesn't automatically discover skills from this directory (or requires additional configuration), the core value proposition — autonomous debugging without prompting — won't work.
   - Suggestion: Verify the Claude Code skill discovery mechanism. Check Claude Code documentation or test with a minimal SKILL.md to confirm auto-discovery works. The most recent commit to `microsoft/playwright-cli` (Feb 10, 2026) clarifies that `install --skills` should be run from the project root, suggesting this feature is still being refined.

### Concerns (Should Address)

These warrant attention but aren't blockers:

1. **Reference project uses MCP, not CLI — no analysis of why**

   - Observation: The team's own reference project (`context/datastarui/`) has a mature Playwright integration using `@playwright/mcp` with an `mcp__playwright__*` tool set, a dedicated `playwright-component-tester` agent, and comprehensive documentation (`docs/playwright.md`, `docs/playwright-commands.md`). The plan argues CLI is better than MCP but doesn't explain why the team's battle-tested MCP pattern is being abandoned.
   - Suggestion: Add a brief section explaining why the CLI approach is preferred for this project specifically, acknowledging the existing MCP experience. Consider whether a hybrid approach (MCP for structured testing, CLI for ad-hoc debugging) might be appropriate.

2. **No error handling in justfile recipe**

   - Observation: The `setup-playwright` recipe runs three commands sequentially but doesn't check for prerequisites or handle failures:
     ```just
     setup-playwright:
         npm install -g @playwright/cli@latest
         playwright-cli install-browser
         playwright-cli install --skills
     ```
   - Suggestion: Add a guard for Node.js/npm availability (`command -v npm >/dev/null 2>&1 || { echo "npm required"; exit 1; }`). Consider separating `install-browser` (200MB download) from the base setup to avoid surprises. The recipe is also wired into `setup`, meaning every `just setup` invocation will attempt a global npm install — this may be unexpected for Go developers.

3. **Headed mode default will fail in headless environments**

   - Observation: The config defaults to `headless: false`. This will fail on CI, SSH sessions, and remote development environments without a display server.
   - Suggestion: Document this limitation. Consider defaulting to headless and adding a `just playwright-headed` recipe for local visual debugging, or auto-detect based on `$DISPLAY` / `$TERM`.

4. **`playwright-cli.json` committed but no `.gitignore` mention**

   - Observation: The plan creates `playwright-cli.json` at the project root and mentions adding `playwright-report/` and `test-results/` to `.gitignore`, but doesn't address whether `playwright-cli.json` itself should be committed. It contains project-specific defaults that other developers would need, so committing it makes sense — but the plan should be explicit about this.
   - Suggestion: Explicitly state that `playwright-cli.json` is committed to the repo. This is the right choice since it contains shared project configuration.

5. **The handoff suggested evaluating Go-based alternatives**

   - Observation: The preceding handoff (`2026-02-11_02-28-40`) listed "Consider: chromedp, rod — Go-based browser automation to stay in the Go ecosystem" as an action item. The plan doesn't address why these were rejected.
   - Suggestion: Add a brief note explaining why a Node.js CLI tool was chosen over Go-native options for a Go project. The token-efficiency argument for CLI vs MCP doesn't directly address CLI vs Go-native.

### Questions (Need Clarification)

These need answers before proceeding:

1. What happens when `@playwright/cli` pushes a breaking change to `@latest`? Is there a strategy for handling this beyond "pin the version"?
2. Does the `Bash(playwright-cli:*)` permission pattern work correctly with Claude Code's permission system? The existing patterns in `.claude/settings.local.json` use patterns like `Bash(go build:*)` and `Bash(just generate:*)` — is `playwright-cli` treated as a single "command" by the glob matcher, or could it match unintended commands?
3. Should `playwright-cli install --skills` be re-run after each CLI update to refresh the SKILL.md, or is it a one-time operation?
4. The plan says "no `package.json` needed" — but will `npm install -g` with nvm work correctly across shell sessions? (Verified: nvm is installed with Node v22.15.0 — this should be fine, but worth noting nvm dependency.)

### Suggestions (Nice to Have)

Optional improvements:

1. Add a `teardown-playwright` recipe for clean removal: `npm uninstall -g @playwright/cli && rm -rf ~/.cache/ms-playwright .claude/skills/playwright-cli playwright-cli.json`
2. Add a `playwright-verify` recipe that runs the smoke tests from Phase 3 as a quick health check
3. Consider adding `/tmp/playwright-cli/` to a project-level `.env.example` or setup documentation so developers know where to find artifacts
4. The "Key CLI Commands Reference" table at the end is helpful — consider making it a standalone reference doc in `thoughts/` or a comment in the justfile

### What's Good

Positive observations worth noting:

- **Clear scope boundaries**: The "What We're NOT Doing" section is excellent and prevents scope creep (no `package.json`, no Docker modifications, no `e2e/` suite, no MCP, no `@playwright/test`)
- **Architecture diagram is accurate**: Port 8080, Docker container, host-side Chromium — all verified against actual `docker-compose.yml` and server configuration
- **Token efficiency rationale is well-argued**: The CLI vs MCP comparison table addresses a real problem (context window consumption during long debugging sessions)
- **Three-phase structure is clean**: Install → Configure → Verify is a natural progression with clear success criteria at each phase
- **Design decisions are documented**: Each config property has an explicit rationale (headed mode for developer visibility, `/tmp/` for artifact isolation, error-level console for focused signal)
- **File/action pairs are explicit**: Every change specifies the file, the action (create/modify), and the exact content — no ambiguity about what to implement

### Recommended Next Steps

1. **Verify the config schema**: Run `playwright-cli --help` or read the source to confirm the exact `playwright-cli.json` property names before writing the config
2. **Pin the version**: Change `@latest` to `@0.1.0` (or whatever the latest stable version is at implementation time)
3. **Test skill discovery**: Install the CLI, run `install --skills`, and verify Claude Code actually discovers and offers the skill in a new session — before investing in Phase 2/3
4. **Add a brief "Why CLI over Go-native" note**: Address the handoff's suggestion about chromedp/rod
5. **Add prerequisite checks**: Guard the justfile recipe against missing npm
