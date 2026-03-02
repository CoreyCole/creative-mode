---
ticket: CRE-11
phase: research
result: success
session: 126df666
workflow: dce2410b
timestamp: 2026-03-02T06:08:46Z
---

## BLUF
Comprehensive research complete: identified 7 areas of code duplication across harness/site (Go) and 2D/boardgame/3D (Rust) templates, with prioritized recommendations for 4 shared packages.

## What Was Done
- Analyzed Discord OAuth duplication between harness and site (~200 lines identical)
- Analyzed Datastar expression/signal helper duplication between harness and site
- Analyzed BridgePlugin duplication between 2D and boardgame templates (byte-for-byte identical)
- Analyzed debug query infrastructure duplication across all 3 Bevy templates
- Analyzed Claude streaming chat handler duplication between harness and site
- Cataloged minor duplications (Trunk.toml, Cargo deps, camera code, SQLite init)
- Verified existing `pkg/` shared package pattern (4 packages already exist)
- Assessed Rust sharing constraints (templates are forked per-world, path deps need build pipeline support)
- Wrote research document with 4 prioritized extraction recommendations

## What Was NOT Done
- No code changes made (research only)
- Did not prototype any shared package interfaces
- Did not assess test coverage for duplicated code

## Key Files
- `thoughts/swarm/research/2026-03-02_06-08-46_CRE-11_code-deduplication-shared-packages.md` — full research document
- `harness/internal/auth/auth.go` — harness Discord OAuth (primary duplication source)
- `site/internal/auth/auth.go` — site Discord OAuth (primary duplication source)
- `harness/views/dsutil/` — harness Datastar helpers
- `site/internal/ui/utils/` — site Datastar helpers (duplicated from harness)
- `templates/2d/src/bridge.rs` — BridgePlugin (duplicated in boardgame)
- `templates/*/src/debug.rs` — debug query infrastructure (duplicated across all templates)

## Gotchas
- Templates are forked into `data/worlds/{worldID}/{checkpointID}/` — shared Rust crates need build pipeline changes to copy alongside
- SignalManager has genuinely diverged: harness uses flat `$property`, site uses namespaced `$id.property` — can't just pick one
- Site's `HandleCallback` returns 200 HTML (for Discord popup flow), not 307 redirect like harness — the shared package must not assume redirect behavior
- Both Go modules use different SQLite drivers (harness: `mattn/go-sqlite3` CGo; site: `modernc.org/sqlite` pure Go) — shared DB helpers won't work

## Next Steps
- Design `pkg/discordauth` interface (Priority 1) — extract Discord API client, crypto helpers, OAuth URL building, cookie management
- Design `pkg/dsutil` interface (Priority 2) — extract DatastarExpression builder, unified SignalManager with optional namespacing
- Evaluate build pipeline changes needed for shared Bevy crate (Priority 3)
- Implementation can proceed as separate tickets or a single code workflow
