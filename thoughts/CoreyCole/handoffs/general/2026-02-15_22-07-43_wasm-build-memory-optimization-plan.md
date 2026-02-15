---
date: 2026-02-15T22:07:43+00:00
researcher: CoreyCole
git_commit: cfb6a510ffe46b8eb733cbf89f806808dea4f0e9
branch: main
repository: creative-mode
topic: "WASM Build Memory Optimization - Implementation Plan Complete, Ready for Phase 1"
tags: [wasm, trunk, wasm-bindgen, memory, vps, live-reload, build-semaphore, lobby-ui]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: WASM Build Memory Optimization — Plan Complete, Implementation Starting

## Task(s)

**Plan: Complete** — Full 4-phase implementation plan written and approved.
**Implementation: Not started** — Was about to begin Phase 1 when session ended.

Resumed from previous handoff (`thoughts/CoreyCole/handoffs/general/2026-02-15_20-35-20_wasm-build-memory-optimization.md`) which was research-only. This session produced the full implementation plan after deep codebase analysis and user discussion.

### Key design decisions made with user:
1. **Template worlds should use pre-built static WASM by default** — no trunk serve on boot
2. **Trunk serve is on-demand only** — for when the president agent is editing templates
3. **Only 1 trunk serve at a time** — enabling on world B stops world A
4. **Lobby should show green "Live" indicator** on whichever template world has active trunk serve
5. **All `trunk build` invocations serialized** via global semaphore (max concurrency=1)

### Phase summary:
- **Phase 1**: Static WASM for template worlds (remove auto trunk serve, symlink dist/ → wasm-builds/)
- **Phase 2**: On-demand live reload API (admin-only toggle, max 1 at a time)
- **Phase 3**: Lobby "Live" indicator (green dot, SSE real-time updates, admin toggle buttons)
- **Phase 4**: Build semaphore (`golang.org/x/sync/semaphore` on Builder)

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-15_21-54-43_wasm-build-memory-optimization.md` — the complete plan with code snippets and success criteria
- **Original research handoff**: `thoughts/CoreyCole/handoffs/general/2026-02-15_20-35-20_wasm-build-memory-optimization.md`

## Recent changes

No code changes made. Only the implementation plan document was created.

## Learnings

- **VPS now has 31 GB RAM** (upgraded from 10 GB at time of original handoff). OOM is no longer an active crisis but the optimization is still worthwhile as preventative measure.
- **Two build paths exist**: `trunk serve` (long-running, file-watching, uncontrollable) and `trunk build` (one-shot, wrappable with semaphore). The semaphore only controls `trunk build`; limiting trunk serve is done by restricting to 1 instance.
- **Template worlds already use `trunk serve`, user forks already use `trunk build`** — the code paths are already separated correctly. The change is just to make trunk serve opt-in instead of auto-started.
- **Pre-built `dist/` directories already exist** in both templates: `templates/2d/dist/` (122 MB) and `templates/3d/client/dist/` (185 MB). These are committed to git. They can be symlinked into `data/wasm-builds/{worldID}/{cpID}/` for `handleWASMArtifacts` to serve.
- **Template world checkpoints point directly to template dirs** (`DirPath = templateDir`, no file copy) and have `WasmPath.Valid = false` — they rely entirely on trunk serve currently.
- **No global build concurrency control exists** — `Builder.Build()` is called via fire-and-forget goroutines from `server.go:631` and `manager.go:153`. The only per-user rate limiter is in `world/rate_limit.go`.
- **The `devState.buildMu.TryLock()` pattern** in `server/dev.go:152` is the closest existing pattern to a build semaphore.
- **2D iframe reload bug**: `events.go:285-295` only reloads iframe for 3D worlds (`serverPort > 0`). Phase 1 should fix this for 2D worlds too.
- **Lobby template** (`views/lobby/lobby.templ:43-54`) renders all worlds identically — no template vs user distinction currently.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-15_21-54-43_wasm-build-memory-optimization.md` — Complete implementation plan (4 phases with code snippets and success criteria)

## Action Items & Next Steps

1. **Implement Phase 1**: Static WASM for template worlds
   - Remove `StartTrunkServe` call from `startTemplateDevServers` (`manager.go:533`)
   - In `createTemplateWorldDev` and `ensureTemplateDevReady`: symlink template `dist/` → `data/wasm-builds/{worldID}/{cpID}/`, set `WasmPath` on checkpoint
   - Fix 2D iframe reload in `events.go:285-295` (add else branch for `serverPort == 0`)

2. **Implement Phase 2**: On-demand live reload
   - Add `StartLiveReload`/`StopLiveReload`/`GetLiveReloadWorldID` to `WorldManager`
   - Create `server/live_reload.go` with admin-only `POST /api/live-reload/:worldID` and `DELETE /api/live-reload`
   - Add `EventLiveReloadChanged` event type

3. **Implement Phase 3**: Lobby "Live" indicator
   - Pass `liveWorldID` from `handleRoot` to lobby template
   - Add green dot + "Live" text to world card when `w.ID == liveWorldID`
   - Extract `WorldCards` sub-component for SSE patching
   - Handle `EventLiveReloadChanged` in `handleGlobalEvent` to re-render cards
   - Add admin-only "Go Live"/"Stop Live" buttons

4. **Implement Phase 4**: Build semaphore
   - `go get golang.org/x/sync` in harness
   - Add `semaphore.Weighted` to `Builder` struct with max=1
   - Wrap `Build()` with `Acquire`/`Release`, add wait logging

5. **Build and test**: `just check` from project root, then manual verification on VPS

## Other Notes

- The plan document has detailed code snippets for each phase — follow those closely.
- `handleWASMArtifacts` at `server.go:543` serves from `data/wasm-builds/{worldID}/{cpID}/` — symlinks are transparently resolved by `os.Stat`/`c.File`.
- Port ranges: trunk serve 8081-8180 (`world/ports.go:12-13`), game servers 9001-9999 (`world/ports.go:9-10`).
- Currently 2 trunk sessions and 1 game server running: `cm-trunk-06296adf-69719ad0`, `cm-trunk-5d189ed4-e9d848f0`, `cm-server-5d189ed4-e9d848f0`.
- The `RecoveredServers()` method on `GameServerManager` returns a snapshot of all tracked servers — useful for finding which world has active trunk serve.
- Template worlds are identified by name convention: `templateWorldName()` returns `"{TYPE} Template World"` (e.g., "3D Template World", "2D Template World") — see `manager.go:352`.
