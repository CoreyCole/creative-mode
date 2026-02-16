---
date: 2026-02-16T11:15:27+00:00
researcher: CoreyCole
git_commit: fb10b146424d0b68635bc39ebef701d8a925f4a1
branch: main
repository: creative-mode
topic: "Documentation audit — verify all CLAUDE.md and README.md files are correct, complete, and concise"
tags: [research, documentation, audit, claude-md, readme]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Documentation Audit

**Date**: 2026-02-16T11:15:27+00:00
**Researcher**: CoreyCole
**Git Commit**: fb10b146424d0b68635bc39ebef701d8a925f4a1
**Branch**: main
**Repository**: creative-mode

## Research Question

Verify all documentation (CLAUDE.md files, README.md) is correct, up to date, and sufficiently detailed without being overly verbose.

## Summary

Audited 7 CLAUDE.md files (root, harness, site, 3d, 2d, boardgame, and a data-dir world) plus the root README.md. Found **19 actionable issues**: 5 inaccuracies, 8 missing items, 4 incomplete sections, and 2 broken references. The documentation is generally high quality — most issues are incremental drift from recent feature additions (boardgame template, camera.rs, touch features, cover art, marketing site bootstrap).

## Detailed Findings

### Root CLAUDE.md — Errors & Gaps

1. **Missing `templates/boardgame/`** in project structure table — actively built/checked/formatted
2. **Missing debug CLI commands**: `list`, `resource`, `client-query`, `components` exist in `scripts/debug.sh` but not documented
3. **`scripts/` description too narrow** — says "Build, format, and setup scripts" but also contains `vps-bootstrap.sh`, `marketing-site-bootstrap.sh`, `debug.sh`, `harness-run.sh`, `sync-thoughts.sh`
4. **`pkg/` description in system prompt differs from CLAUDE.md** — system prompt only mentions `worldchannel`, CLAUDE.md correctly lists all four packages

### Harness CLAUDE.md — Errors & Gaps

5. **`GetRecentMessages(ctx, limit)` does not exist** — actual query is `GetRecentMessagesWithUser(ctx, limit int64)` (returns a join with user data)
6. **`just generate` description incomplete** — docs say `sqlc + templ generate` but it also runs `just build-tailwind`
7. **Trunk session naming `cm-trunk-{worldID}-{cpID}` undocumented** in Session Naming section
8. **`internal/claude/` description understated** — says "session management" but it's the full prompt-to-build orchestrator

### Site CLAUDE.md — Errors

9. **`internal/markdown/` does not exist** — the markdown renderer is at `pkg/markdown/` (shared package), not a site-internal package

### Templates/3d CLAUDE.md — Errors

10. **`register_component` API signature wrong** — docs show `ChannelDirection::ServerToClient` parameter but Lightyear 0.26 API takes no arguments (see `shared/src/protocol.rs:101-107`)

### Templates/2d CLAUDE.md — Gaps

11. **`camera.rs` missing from Structure table** — exists, registered in `lib.rs:35`, handles auto-fit, touch pan/zoom, scroll-wheel zoom
12. **`"touch"` Bevy feature missing from docs** — actual `Cargo.toml` has `features = ["2d", "default_font", "touch"]`

### README.md — Errors

13. **Broken LICENSE link** — `./LICENSE.md` should be `./LICENSE` (file has no `.md` extension)

### Cross-Cutting Gaps (Not Blocking, But Worth Noting)

14. `flake.nix` exists but undocumented — defines full dev toolchain
15. `data/` directory structure undocumented (gitignored, so reasonable omission)
16. `bevy-debug` skill exists in `.claude/skills/` but not mentioned in CLAUDE.md
17. Several env vars from `.env.example` files not in the root env var table (Discord OAuth, Gemini, DEV_MODE)
18. `scripts/vps-bootstrap.sh` and `scripts/marketing-site-bootstrap.sh` are significant but undocumented
19. President and trunk tmux session naming patterns undocumented

## Recommended Fixes

### Priority 1 — Factual Errors (fix now)

| # | File | Fix |
|---|------|-----|
| 13 | `README.md` | Change `./LICENSE.md` to `./LICENSE` |
| 5 | `harness/CLAUDE.md` | Change `GetRecentMessages(ctx, limit)` to `GetRecentMessagesWithUser(ctx, limit)` |
| 9 | `site/CLAUDE.md` | Remove `internal/markdown/` from directory table, note it's at `pkg/markdown/` |
| 10 | `templates/3d/CLAUDE.md` | Remove `ChannelDirection::ServerToClient` from `register_component` example |

### Priority 2 — Missing Content (add)

| # | File | Fix |
|---|------|-----|
| 1 | `CLAUDE.md` | Add `templates/boardgame/` row to project structure table |
| 11 | `templates/2d/CLAUDE.md` | Add `camera.rs` to Structure table and architecture diagram |
| 12 | `templates/2d/CLAUDE.md` | Add `"touch"` to Bevy features listing |
| 2 | `CLAUDE.md` | Add `list`, `resource`, `client-query`, `components` to debug CLI table |
| 6 | `harness/CLAUDE.md` | Add Tailwind build step to `just generate` description |

### Priority 3 — Improvements (optional, keeps docs accurate)

| # | File | Fix |
|---|------|-----|
| 3 | `CLAUDE.md` | Update scripts/ description to mention bootstrap/deploy scripts |
| 7 | `harness/CLAUDE.md` | Add `cm-trunk-{worldID}-{cpID}` to Session Naming |
| 8 | `harness/CLAUDE.md` | Update `internal/claude/` description to "Claude Code orchestrator" |

## Code References

- `scripts/debug.sh:9-38` — All debug subcommands
- `harness/internal/db/sqlc/querier.go:43` — `GetRecentMessagesWithUser`
- `harness/justfile:17-19` — `just generate` recipe
- `harness/internal/world/game_server.go:63-65` — Trunk session naming
- `templates/3d/shared/src/protocol.rs:101-107` — Actual `register_component` usage
- `templates/2d/src/camera.rs` — Camera plugin (undocumented)
- `templates/2d/Cargo.toml:10` — Actual Bevy features with "touch"
- `LICENSE` — File name (no `.md`)
- `site/internal/mayor/client.go:14` → `pkg/mayorchat/client.go:14` — Markdown import chain

## Architecture Insights

The documentation is well-structured as a hierarchy: root CLAUDE.md provides project overview + cross-cutting concerns, subdirectory CLAUDE.md files provide deep-dive details. The main drift comes from rapid feature additions in the Feb 13-16 sprint (boardgame template, camera.rs, touch support, bootstrap scripts, cover art) that were committed without corresponding doc updates.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/handoffs/general/2026-02-13_21-07-42_master-plan-review-and-readme.md` — README rewrite for hackathon
- `thoughts/CoreyCole/handoffs/general/2026-02-14_00-58-15_vps-bootstrap-and-server-setup-guide.md` — VPS bootstrap creation
- `thoughts/CoreyCole/handoffs/general/2026-02-14_11-55-50_aws-marketing-site-setup.md` — EC2/AWS infrastructure
- `thoughts/CoreyCole/handoffs/general/2026-02-13_18-35-56_responsive-touch-pan-zoom.md` — Touch/camera features added to 2D template
