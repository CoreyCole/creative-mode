---
date: 2026-02-13T08:20:24-08:00
researcher: CoreyCole
git_commit: b7b7f492ae58b6cae3cf5e73b581da6822ee8608
branch: main
repository: creative-mode
topic: "Testing Documentation Audit: Playwright CLI + Template World Testing Gap"
tags: [research, testing, playwright, e2e, template-worlds, documentation]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
---

# Research: Testing Documentation Audit — Playwright CLI + Template World Testing

**Date**: 2026-02-13T08:20:24-08:00
**Researcher**: CoreyCole
**Git Commit**: b7b7f492ae58b6cae3cf5e73b581da6822ee8608
**Branch**: main
**Repository**: creative-mode

## Research Question

How do we currently advise Claude to test with playwright-cli? We have a harness E2E playbook, but we need to improve testing docs for template worlds.

## Summary

We have **strong harness UI testing** via a well-structured E2E playbook (`harness/E2E_PLAYBOOK.md`) that covers all overlay pages (login, lobby, world view, admin, chat, auth flows). However, **template world testing is a major gap** — there is no playbook for verifying that the actual game running inside the `<iframe>` works correctly. Debug query infrastructure exists (BRP + JS bridge + harness proxy) but isn't integrated into any formalized testing workflow. The 2D template has zero testing documentation. All testing is manual via `playwright-cli` — no automated test files exist anywhere.

## Detailed Findings

### 1. Current Harness E2E Playbook — What We Have

**File**: `harness/E2E_PLAYBOOK.md` (414 lines)

The playbook is well-structured with 8 test sections:

| Section | Coverage | Status |
|---------|----------|--------|
| T1: Login Page | Dev login form, heading, sign-in link | Complete |
| T2: Pending Page | Approval-pending state, middleware blocking | Complete |
| T3: Lobby | Header, world list, create world, chat panel | Complete |
| T4: World View | Page structure, top bar, overlay toggle, checkpoint tree, chat tabs, prompt bar | Complete |
| T5: Admin Page | User list, approve/reject, role badges | Complete |
| T6: Auth Flows | Logout, protected route redirects, re-auth | Complete |
| T7: Error Cases | Invalid world ID, 404, unauthorized admin | Complete |
| T8: Cross-Session | Chat propagation, world list SSE updates | Complete |

**Strengths**:
- Named sessions (`-s=admin`, `-s=user2`) for multi-user testing
- Source file references for each section
- Datastar interop notes (fill + click works for forms, fetch() workaround for signal-bound inputs)
- Clear pass criteria per test step
- Quick reference tables (page routing, SSE events)
- Instructions for adding new tests

**Boundary**: The playbook tests everything about the harness overlay UI but **stops at the iframe boundary**. T4 verifies `#game-frame` exists and has the right structure, but never checks that the game canvas actually renders, that WASD input works, or that multiplayer functions.

### 2. Root CLAUDE.md Testing Guidance

**File**: `CLAUDE.md` (root)

Contains a `Skills > playwright-cli` section and `E2E Testing Tips`. Key advice given to Claude:

- **Workflow per page**: `snapshot` -> interact -> `console error` -> `screenshot` -> read outputs
- **Datastar + Playwright interop**: `fill` and `keyboard.type()` do NOT update Datastar signal bindings. Use `page.evaluate(fetch())` for form submission that relies on signals.
- **SSE verification**: `network` shows active SSE streams as `[GET] /events => [200] OK`
- **Cookie management**: `cookie-delete session` to simulate logged-out state
- **Snapshots**: Element refs (`[ref=e15]`) are stable within a snapshot but change between snapshots. Always `snapshot` before interacting.
- **Screenshots are images**: Use Read tool on PNG path — Claude Code is multimodal
- **Always use `--headed --persistent`** for `playwright-cli open`

This guidance is harness-focused. No template world testing advice.

### 3. Playwright-CLI Skill

**File**: `.claude/skills/playwright-cli/SKILL.md` (260 lines)

Generic playwright-cli command reference. Not project-specific. Covers:
- Core commands (open, goto, click, fill, snapshot, screenshot)
- Navigation, keyboard, mouse commands
- Storage (cookies, localStorage, sessionStorage)
- Network mocking
- DevTools (console, network, tracing, video)
- Browser sessions (`-s=name` for named sessions)
- 7 reference subdocs (test-generation, video-recording, running-code, storage-state, tracing, request-mocking, session-management)

### 4. Bevy Debug Skill

**File**: `.claude/skills/bevy-debug/SKILL.md` (153 lines)

Three-tier debug approach documented:

1. **Phase 3 — Client via Harness (preferred)**: SSE round-trip `POST /world/<id>/client-debug` with `{"type": "resource|query|list", ...}`. Requires a browser with the world page open. 5s timeout.
2. **Phase 2 — Server via BRP**: JSON-RPC 2.0 `POST /world/<id>/debug`. Full type paths (`shared::protocol::PlayerPosition`). Requires running game server.
3. **Phase 1 — Client via Playwright (fallback)**: Direct `window.__debugRequest`/`__debugResponse` JS bridge through `run-code`.

Includes debugging workflows:
- Compare client vs server state
- Track movement over time (5 samples at 500ms intervals)
- Investigate input issues (minimize overlay, force-click iframe, hold keys)

**Key gotcha**: `FlyCameraState` is rotation only (yaw/pitch). For position, query `Transform` on the `FlyCamera` entity.

### 5. Template CLAUDE.md Files — Testing Content

**3D template** (`templates/3d/CLAUDE.md`, 238 lines):
- Detailed architecture docs (client-server, prediction, replication)
- Building instructions (server, client WASM, workspace)
- Debug query system docs (BRP + JS bridge)
- No testing playbook or E2E test steps
- No guidance on how to verify a world works end-to-end

**2D template** (`templates/2d/CLAUDE.md`, 95 lines):
- Minimal docs: structure, room JSON schema, building
- Zero testing content
- Zero debug content
- No guidance on verifying rooms load or hotspots work

### 6. Template World Testing — The Gap

**What exists as scattered plan fragments (not formalized):**

The plan `thoughts/CoreyCole/plans/2026-02-11_15-18-38_template-world-playable.md` contains a Phase 5 "Playwright E2E Verification" (lines 393-517) that was never turned into a playbook. It describes:

- T9: Template World Auto-Load
  - Login and verify redirect to world
  - Wait for build + game server (polling loop, up to 5 min)
  - Verify canvas renders (screenshot — bloom spheres visible)
  - Verify player movement (before/after screenshot comparison)
  - Verify multiplayer (second player sees capsule)
  - Console error check

**What debug infrastructure exists but isn't in any playbook:**

| Capability | API | Documented Where |
|-----------|-----|-----------------|
| Query server ECS state | `POST /world/<id>/debug` (BRP JSON-RPC) | bevy-debug skill, 3d CLAUDE.md |
| Query client ECS state | `POST /world/<id>/client-debug` | bevy-debug skill |
| Query client via playwright | `window.__debugRequest` JS bridge | bevy-debug skill, 3d CLAUDE.md |
| List queryable types | `{"type": "list"}` / `world.list_resources` | bevy-debug skill |
| Compare client vs server | Side-by-side curl commands | bevy-debug skill |
| Track movement over time | Repeated position queries | bevy-debug skill |

**What's completely missing:**

1. **Game canvas rendering verification** — Is the canvas black? Does WebGL/WebGPU work? Are textures loading?
2. **WASM loading verification** — Did Trunk-built WASM actually load? Are there WASM panics?
3. **WebSocket connection verification** — Did the client connect to the game server? Is Lightyear netcode working?
4. **Self-signed cert acceptance** — How to handle the WSS cert interstitial in automated testing
5. **2D template testing** — Room loading, hotspot interaction, room navigation, postMessage bridge
6. **Build pipeline verification** — Did the initial build complete? Is the game server process alive?
7. **Systematic input testing** — WASD, mouse look, sprint, camera modes

### 7. No Automated Test Files Exist

| Location | Test Files Found |
|----------|-----------------|
| `harness/` | Zero `_test.go` files (justfile has `test: go test ./...` but nothing to run) |
| `templates/3d/` | Zero `#[test]` or `#[cfg(test)]` blocks |
| `templates/2d/` | Zero `#[test]` or `#[cfg(test)]` blocks |
| Root | Zero Playwright test scripts (`.spec.ts`) |

All testing is manual, executed via `playwright-cli` commands in a Claude Code session.

## Code References

- `harness/E2E_PLAYBOOK.md` — Primary E2E test playbook (harness UI only)
- `CLAUDE.md:43-85` — Playwright-cli skill reference and E2E testing tips
- `.claude/skills/playwright-cli/SKILL.md` — Generic playwright-cli command reference
- `.claude/skills/bevy-debug/SKILL.md` — Bevy ECS debug query workflows
- `templates/3d/CLAUDE.md:135-184` — Debug query system docs (BRP + JS bridge)
- `templates/2d/CLAUDE.md` — Minimal docs, no testing content
- `playwright-cli.json` — Config: chromium, 30s nav timeout, error-level console, `.playwright-cli/` output
- `justfile:15-22` — `setup-playwright` recipe
- `harness/justfile:10-11` — `test: go test ./...` (no test files)

## Architecture Insights

### Testing Hierarchy

```
Layer 1: Harness UI (COVERED by E2E_PLAYBOOK.md)
  ├── Login/Auth flows
  ├── Lobby (world list, create, chat)
  ├── World overlay (tabs, chat, checkpoint tree, prompt bar)
  ├── Admin page
  └── Multi-user SSE events

Layer 2: Game Canvas Integration (PARTIALLY COVERED)
  ├── iframe loads with correct src         → T4.1 checks structure only
  ├── WASM binary loads without panic       → NOT TESTED
  ├── WebGL/WebGPU context acquired         → NOT TESTED
  └── Game server WebSocket connects        → NOT TESTED

Layer 3: Gameplay (NOT COVERED — needs template world playbook)
  ├── Scene renders (not black/blank)       → Screenshot verification
  ├── Player input works (WASD)             → Before/after screenshots + debug queries
  ├── Camera controls work (mouse look)     → Debug query FlyCameraState
  ├── Multiplayer (other players visible)   → Multi-session + debug queries
  ├── 2D room loading + hotspot interaction → New test approach needed
  └── Client-server state consistency       → BRP vs client-debug comparison
```

### Template-Specific Testing Needs

**3D Template** (Bevy + Lightyear multiplayer):
- Build verification (cargo + trunk + wasm-bindgen)
- Game server process alive (tmux session + port allocation)
- WSS self-signed cert acceptance
- Client connects to server (Lightyear WebSocket)
- Scene renders (bloom spheres, ground plane)
- WASD movement (PlayerPosition changes via debug query)
- Mouse look (FlyCameraState yaw/pitch changes)
- Multiplayer: second player joins, appears as capsule
- Server ECS state matches client ECS state (BRP vs client-debug)

**2D Template** (Bevy WASM, client-only):
- Build verification (trunk only, no server)
- WASM loads in iframe
- Room JSON loads correctly (rooms/*.json parsed)
- Hotspot rendering (translucent rectangles with labels)
- Hotspot click detection
- Room navigation (`navigate_room` action)
- Dialog display (`dialog` action)
- postMessage bridge to harness parent frame
- No game server — no multiplayer to test

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-11_15-18-38_template-world-playable.md` — Phase 5 has the only existing draft of template world E2E tests (never formalized into a playbook)
- `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — Debug API design, E2E testing section at bottom
- `thoughts/CoreyCole/plans/2026-02-11_10-31-25_harness-e2e-regression-tests.md` — Original E2E regression plan (led to E2E_PLAYBOOK.md)
- `thoughts/CoreyCole/handoffs/general/2026-02-11_20-48-14_debug-apis-e2e-verified.md` — Debug queries verified with live client, documented learning about `#[reflect(Component)]`
- `thoughts/CoreyCole/handoffs/general/2026-02-12_15-17-55_fix-controls-and-movement.md` — Documents real gameplay bugs found via manual testing (janky movement, D-key not registering, controls stopping)

## Recommendations for Improving Testing Docs

### 1. Create a Template World E2E Playbook

A new file (e.g., `harness/TEMPLATE_WORLD_PLAYBOOK.md` or `GAME_TESTING_PLAYBOOK.md`) that covers Layer 2 and Layer 3 testing. This should be a sibling to `E2E_PLAYBOOK.md`, not merged into it — the harness playbook tests UI, the template playbook tests gameplay.

### 2. Integrate Debug Queries into the Playbook

The bevy-debug skill has all the tools but they're documented as debugging aids, not as systematic test steps. A testing playbook should use debug queries as **assertions** (e.g., "after pressing W for 1 second, query PlayerPosition and verify Z decreased").

### 3. Document 2D Template Testing Separately

The 2D template is fundamentally different (no server, no multiplayer, data-driven rooms). It needs its own testing approach focused on room loading, hotspot interaction, and the postMessage bridge.

### 4. Add Testing Section to Template CLAUDE.md Files

Both `templates/3d/CLAUDE.md` and `templates/2d/CLAUDE.md` should have a "Testing" section that tells Claude how to verify changes work. Currently, they have "Building" sections but no testing guidance.

### 5. Consider Where Docs Live

Current distribution:
- Root `CLAUDE.md` — playwright-cli setup + generic E2E tips (good)
- `harness/E2E_PLAYBOOK.md` — harness UI tests (good)
- `.claude/skills/bevy-debug/SKILL.md` — debug query reference (good as skill)
- Template CLAUDE.md files — architecture + building only (needs testing sections)

The template world testing playbook should probably live at the root level since it spans harness (iframe loading, game server management) and templates (WASM gameplay).

## Open Questions

1. Should the template world playbook include build wait times (5-10 min cold builds), or should we assume pre-built worlds?
2. Should we create separate playbooks per template type (3D vs 2D), or one playbook with template-specific sections?
3. Should the playbook include WSS cert acceptance steps, or should we automate that away (e.g., `--ignore-certificate-errors` Chrome flag)?
4. Should debug query assertions be "soft" (log the result for human review) or "hard" (fail if value outside expected range)?
5. Do we want to keep all testing as manual playwright-cli sessions, or start writing automated Playwright test scripts (`.spec.ts`)?
