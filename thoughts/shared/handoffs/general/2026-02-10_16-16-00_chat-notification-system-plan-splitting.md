---
date: 2026-02-11T00:16:00Z
researcher: Claude (Opus 4.6)
git_commit: 0c284dbf012af933bf1cb19527bb16640070348b
branch: main
repository: creative-mode
topic: "Creative Mode Plan Refinement - Chat/Notification System + Plan Splitting"
tags: [implementation, strategy, plan-review, chat, notifications, lineage, parallel-agents]
status: complete
last_updated: 2026-02-10
last_updated_by: Claude
type: implementation_strategy
---

# Handoff: Chat/Notification System Design Complete, Plan Needs Splitting for Parallel Work

## Task(s)

1. **Resolve reload/build-ready notification strategy** — COMPLETED. The user decided on a global chat/notification log as the primary mechanism for surfacing build completions. No auto-switching; users click `[▶ Play]` in the log to hop to completed builds.

2. **Design three-scope chat panel (Global / World / Lineage)** — COMPLETED. The panel has three tabs:
   - **Global**: All chat + system notifications across all worlds
   - **World**: Same types, filtered to current world
   - **Lineage**: Read-only prompt/response chain from root → current checkpoint (no chat messages). Shows Claude's work summary + auto-generated metadata for each checkpoint in the ancestry.

3. **Design checkpoint work summaries** — COMPLETED. Two sources combined:
   - Claude writes `CHANGES.txt` (instructed via `template/CLAUDE.md`) — 2-4 sentence human-readable description
   - Harness auto-generates metadata from `claude.jsonl` hook events — files edited, build duration
   - Both stored on checkpoint record (`work_summary`, `files_changed`, `build_duration_ms`)

4. **Design overlay minimize/maximize** — COMPLETED. Expanded (full chrome with tabbed panel) and minimized (floating corner button with unread badge count).

5. **Split the plan for parallel agent work** — NOT STARTED. The user explicitly asked for this as the next step. The plan is large (~2500 lines) and covers 6 phases that have parallelizable components.

## Critical References

- `thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md` — The main implementation plan (fully refined, all design decisions resolved, ~2500 lines)
- `thoughts/CoreyCole/reviews/2026-02-10_12-36-08_creative-mode-implementation_review.md` — Staff engineer review (all items addressed except low-priority concerns #3, #4, #8, #9, #10)

## Recent changes

All changes were to the plan document (`thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md`):

- **"What We're NOT Doing"**: Changed "Voice chat or text chat" to "Voice chat (text chat is supported via the global notification log)"
- **Database schema**: Added `messages` table (chat + system notifications with type, user_id, world_id, checkpoint_id, content). Added `work_summary TEXT`, `files_changed TEXT`, `build_duration_ms INTEGER`, `created_by TEXT` columns to `checkpoints` table.
- **New architecture section**: "Chat/Notification Panel (Three Scopes)" — comprehensive design for the tabbed panel with Global/World/Lineage tabs, checkpoint work summaries, overlay states, SSE delivery model.
- **Serving model switching mechanism**: Rewritten for notification-driven flow. Builds never auto-switch. `loadCheckpoint()` detects same-world vs cross-world navigation.
- **Routes**: Added `GET /events` (global SSE for lobby), `POST /api/chat` (send chat), `GET /world/:worldID/lineage/:cpID` (checkpoint ancestry for Lineage tab).
- **Phase 4 SSE handler**: Rewritten to subscribe to both global event bus and world event bus. Added `handleChatMessage()`, `createAndPublishMessage()`. EventBus now has `SubscribeGlobal()`/`PublishGlobal()` in addition to per-world pub/sub.
- **Phase 4 handleClaudeEvent**: Now creates `build.started` messages in the DB and publishes to global bus. Build manager creates `build.completed`/`build.failed` messages similarly.
- **Phase 5 OverlaySignals**: Added `ActiveTab string`, `ChatText string` signals.
- **Phase 5 overlay layout**: Complete redesign with tabbed chat panel (Global/World/Lineage), lineage view template, `loadLineage()` JS function.
- **Phase 5 CSS**: Added tab bar styles, lineage view styles (entry, header, summary, cursor), message log styles.
- **Phase 5 success criteria**: Added 8 new manual verification items for chat, notifications, minimize, lineage.
- **CLAUDE.md template**: Added `CHANGES.txt` requirement — Claude must write a 2-4 sentence summary before finishing.
- **Testing steps**: Expanded from 15 to 22 steps including chat, minimize/badge, cross-world navigation.
- **Resolved questions**: Added 4 new Q&As (chat scopes, lineage content, work summaries, panel tabs).

## Learnings

- **Lineage is NOT a live feed** — it's a static view of the checkpoint ancestry, populated by a dedicated `GET /world/:worldID/lineage/:cpID` endpoint rather than SSE. Re-fetched when user switches checkpoints or selects the tab.
- **SSE handler subscribes to two buses** — global (chat, system notifications visible to all) and world-specific (build progress, claude activity for the current world). This means the World tab is filtered client-side from the same SSE stream.
- **`CHANGES.txt` pattern** for Claude summaries — simple file-based approach. Claude writes it before finishing; harness reads it after `claude.session_stopped` event. Fallback: if Claude doesn't write it, the auto-generated metadata still provides useful context in the Lineage tab.
- **Cross-world navigation from notifications** requires a full page load (new SSE connection, new overlay state), while same-world checkpoint switching is just an iframe src swap.
- **The plan is ~2500 lines** and covers 6 tightly described phases. It's ready for implementation but should be split into component-level specs for parallel agent work. Natural split points: (1) Go harness server + DB, (2) Auth + admin, (3) World/build/tmux pipeline, (4) Bevy game template, (5) UI overlay + SSE, (6) Chat/notification system.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md` — Fully refined implementation plan (all design questions answered)
- `README.md` — Updated project README (from prior session)
- `thoughts/CoreyCole/reviews/2026-02-10_12-36-08_creative-mode-implementation_review.md` — Staff review (reference, not modified this session)

## Action Items & Next Steps

The user's explicit next step is to **split the plan into component-level specs for parallel agent work**. The monolithic plan is thorough but too large for a single agent to implement efficiently.

### Suggested split strategy:

The plan has natural component boundaries that can be developed in parallel:

1. **Go harness server skeleton + DB layer** (Phase 1 core)
   - `harness/go.mod`, `harness/main.go`, `harness/internal/db/`, `harness/internal/server/server.go`, `harness/internal/logging/`
   - SQLite schema, migrations, query methods, WAL mode
   - Echo router setup, graceful shutdown, static file serving
   - Dependencies: none (foundation layer)

2. **Auth + admin approval system** (Phase 1 auth)
   - `harness/internal/auth/`, `harness/views/login.templ`, `harness/views/pending.templ`, `harness/views/admin_users.templ`
   - GitHub OAuth flow, session management, role-based middleware
   - Dependencies: DB layer (#1)

3. **World management + build pipeline** (Phase 3)
   - `harness/internal/world/`, `harness/internal/build/`, `scripts/build-game.sh`
   - World creation, checkpoint forking, hardlink cache, Trunk builds, port allocation, game server lifecycle, rate limiting
   - Dependencies: DB layer (#1)

4. **Bevy game template** (Phase 2)
   - `template/` — entire Rust workspace (shared, server, client)
   - Cargo workspace, Lightyear protocol, headless server, WASM client, Trunk config, CLAUDE.md, MEMORY.md
   - Dependencies: none (can develop independently and verify with manual `cargo build` + `trunk build`)

5. **Claude Code integration + tmux** (Phase 4)
   - `harness/internal/tmux/`, `harness/internal/claude/`, `template/.claude/` (hooks)
   - tmux session management, prompt delivery via `--input-file`, hook scripts, JSONL logging, MEMORY.md management
   - Dependencies: World management (#3), game template (#4)

6. **UI overlay + chat/notification system** (Phase 5 + relevant Phase 6)
   - `harness/views/` (layout, overlay, chat_panel, checkpoint_tree, build_log, lobby), `harness/static/`, `harness/static/game-loader.js`
   - Tabbed chat panel (Global/World/Lineage), overlay expand/minimize, SSE event handlers, lineage endpoint, iframe switching
   - Dependencies: Auth (#2), world management (#3), SSE events from Claude integration (#5)

7. **End-to-end integration + Docker** (Phase 6)
   - `Dockerfile`, `docker-compose.yml`, `scripts/setup.sh`, integration testing
   - Wire all components, Docker image, setup script, pre-build template deps
   - Dependencies: all of the above

Components #1, #4 can start immediately in parallel. #2, #3 can start once #1 is done. #5 can start once #3 and #4 are done. #6 can start once #2 and #3 are done. #7 is the final integration pass.

### Lower-priority remaining items from the staff review:

These were deferred and can be addressed during implementation or as follow-ups:
- Concern #3: shared `CARGO_HOME` race conditions under concurrent builds
- Concern #4: input sanitization on world/checkpoint names
- Concern #8: clarify MEMORY.md per-checkpoint semantics
- Concern #9: verify Claude Code hook payload format against actual schema
- Concern #10: health check mechanism for game servers

## Other Notes

- The repo is still essentially empty — only `README.md`, `.claude/settings.local.json`, and `thoughts/` directory exist. No code has been written yet.
- The plan's Phase 6 "Game server connection string" section still has one `WebTransport` reference at line ~2350 (`Lightyear natively via Lightyear`) — minor inconsistency, should say WebSocket.
- The review's "Suggestions (Nice to Have)" section has 6 optional improvements not addressed: `just verify` command, wasm-opt `-Oz`, playground mode without OAuth, structured game server logging, claude output format. These are nice-to-haves for after initial implementation.
- The `loadLineage()` function in `game-loader.js` uses a plain `fetch()` rather than Datastar's `@get()` because the Lineage tab is a one-shot render, not an SSE stream. This is an intentional design choice.
- The EventBus implementation shown in the plan is illustrative — the actual implementation should handle unsubscription properly (remove channel from slice, close channel).
