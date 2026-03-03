---
ticket: CRE-8-r3
phase: research
result: success
session: 002adab5
workflow: 5a3b4fb6
timestamp: 2026-03-03T00:36:10Z
---

## BLUF
Completed code deduplication research: mapped all duplicated implementations across harness/site (Discord auth, Datastar utils, Bevy BridgePlugin, chat streaming), identified exact overlaps vs intentional divergences, and recommended 4-priority extraction plan starting with `pkg/discordauth`.

## What Was Done
- Verified CRE-11 research findings are still current (no code changes since audit)
- Deep-analyzed Discord OAuth duplication: 5 identical functions, 5 near-identical, with divergent storage/auth models
- Analyzed Datastar utility duplication: expressions identical, SignalManager intentionally divergent (flat vs namespaced)
- Analyzed Bevy BridgePlugin: 2D/boardgame byte-for-byte identical, 3D has no BridgePlugin
- Analyzed debug query infrastructure: I/O scaffolding identical but query engines completely template-specific
- Cataloged minor duplications (Trunk.toml, Cargo.toml deps, SQLite init) and explained why not worth extracting
- Wrote comprehensive research document with side-by-side comparisons, risk analysis, and prioritized recommendations

## What Was NOT Done
- No code changes made (research-only phase)
- No Linear comment posted (no ticket URL available)

## Key Files
- `thoughts/swarm/research/2026-03-03_00-36-10_CRE-8-r3_code-deduplication-shared-packages.md` — Full research document
- `thoughts/swarm/research/2026-03-02_06-08-46_CRE-11_code-deduplication-shared-packages.md` — Prior CRE-11 research (still accurate)
- `harness/internal/auth/auth.go` — Harness Discord OAuth (primary extraction target)
- `site/internal/auth/auth.go` — Site Discord OAuth (primary extraction target)
- `harness/views/dsutil/` — Harness Datastar utilities
- `site/internal/ui/utils/` — Site Datastar utilities
- `templates/2d/src/bridge.rs` — 2D BridgePlugin (identical to boardgame)
- `templates/boardgame/src/bridge.rs` — Boardgame BridgePlugin (identical to 2D)

## Gotchas
- Previous research attempts for CRE-8 child tickets failed because result files weren't written — must write `$CM_SWARM_RESULT_PATH`
- SignalManager unification is NOT recommended — flat vs namespaced is intentional design divergence
- Bevy shared crate extraction requires build pipeline changes to copy crate alongside forked templates
- `Color::srgba()` may not work in `const` contexts in Bevy 0.15

## Next Steps
- Project plan should create ticket for `pkg/discordauth` extraction (highest priority, ~200 lines saved)
- Consider `pkg/dsutil` for expressions only (NOT SignalManager)
- Bevy shared crate is lower priority due to build pipeline dependency
- Chat streaming extraction should be deferred until shared packages stabilize
