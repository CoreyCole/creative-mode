# Creative Mode - Component Specs for Parallel Implementation

Split from the monolithic plan at `thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md`.

## Component Overview

| # | Component | Files | Dependencies | Can Start |
|---|-----------|-------|--------------|-----------|
| 1 | [Harness Server + DB](component-1-harness-server-db.md) | `harness/main.go`, `internal/db/`, `internal/server/`, `internal/logging/` | None | Immediately |
| 2 | [Auth + Admin](component-2-auth-admin.md) | `internal/auth/` | #1 | After #1 |
| 3 | [World Management + Build](component-3-world-management-build.md) | `internal/world/`, `internal/build/` | #1 | After #1 |
| 4 | [Bevy Game Template](component-4-bevy-game-template.md) | `template/` (entire Rust workspace) | None | Immediately |
| 5 | [Claude Integration + tmux](component-5-claude-integration-tmux.md) | `internal/tmux/`, `internal/claude/`, `internal/events/` | #3, #4 | After #3, #4 |
| 6 | [UI Overlay + Chat](component-6-ui-overlay-chat.md) | `views/`, `static/`, `internal/server/events.go` | #2, #3, #5 | After #2, #3, #5 |
| 7 | [Integration + Docker](component-7-integration-docker.md) | `Dockerfile`, `docker-compose.yml`, `scripts/` | All | Last |

## Dependency Graph

```
#1 Harness Server + DB ──────┬──> #2 Auth + Admin ──────────────┐
  (start immediately)        │                                   │
                             ├──> #3 World + Build ──┐           │
                             │                       ├──> #5 Claude + tmux ──┐
#4 Bevy Template ────────────┘──────────────────────┘                        │
  (start immediately)                                                        ├──> #6 UI Overlay
                                                                             │
                                                                             ├──> #7 Integration
```

## Parallel Execution Strategy

**Wave 1** (start immediately, in parallel):
- Component 1: Go harness server + DB
- Component 4: Bevy game template

**Wave 2** (after Wave 1 completes):
- Component 2: Auth + admin (needs #1)
- Component 3: World management + build (needs #1)

**Wave 3** (after Wave 2 completes):
- Component 5: Claude integration + tmux (needs #3, #4)

**Wave 4** (after Wave 3 completes):
- Component 6: UI overlay + chat (needs #2, #3, #5)

**Wave 5** (after everything):
- Component 7: End-to-end integration + Docker

## Notes

- Each spec is self-contained with enough context for an independent agent
- Code snippets in specs are illustrative — agents should adapt to actual Lightyear/Datastar APIs
- The original monolithic plan remains the authoritative source for design decisions
- Staff review concerns not addressed: see Component 7 "Deferred Items" section
