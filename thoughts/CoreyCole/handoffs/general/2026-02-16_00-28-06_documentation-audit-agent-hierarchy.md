---
date: 2026-02-16T00:28:06+0000
researcher: CoreyCole
git_commit: 326c63a0f31e965a7dc6ea892462ea2731d5a9c3
branch: main
repository: creative-mode
topic: "Documentation Audit: Agent Hierarchy (President → Mayors → Claude Code)"
tags: [documentation, agents, president, mayors, claude-code, architecture, audit]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Documentation Audit — Agent Hierarchy Docs Update

## Task(s)

**Completed: Comprehensive documentation audit** — Research phase complete. Spawned 4 parallel codebase analysis agents to compare all CLAUDE.md files against actual code. Produced a detailed research document with findings, prioritized fix list, and recommended updates.

**Planned: Documentation updates** — 16 specific documentation changes identified and prioritized across root CLAUDE.md, harness CLAUDE.md, site CLAUDE.md, and template docs. No code changes have been made yet — this is purely a research/audit handoff.

## Critical References

1. **Research document (primary artifact)**: `thoughts/CoreyCole/research/2026-02-15_23-42-38_documentation-audit-agent-hierarchy.md` — Contains full audit results, code references, and prioritized fix list
2. **Master plan**: `thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md` — 6-phase mayor implementation plan (Docker-centric, partially outdated)
3. **Latest VPS review**: `thoughts/CoreyCole/reviews/2026-02-15_07-39-39_world-mayors-master-plan_review.md` — Identifies VPS migration invalidating Phase 1

## Recent changes

No code changes were made. Only the research document was created.

## Learnings

### Single Bot Architecture (not two)
The codebase uses ONE `DISCORD_BOT_TOKEN` for all Discord operations, not two bots as the master plan proposed. Three separate `discordgo.Session` instances share the same token:
- REST-only session in `pkg/worldchannel/client.go:29` (channel ops)
- REST-only session in `site/internal/auth/auth.go` (guild membership checks)
- Gateway session in `harness/internal/discord/listener.go` (message mirroring)

### Harness runs via `air` in production
Root CLAUDE.md says "native binary under systemd" but `scripts/harness-run.sh:37` calls `exec air`. The `.air.toml` builds to `/tmp/harness` and watches for file changes. This is a significant documentation inaccuracy.

### `internal/build/` was renamed to `internal/builder/`
Commit 326c63a renamed this package (to fix revive lint error). Harness CLAUDE.md still references `internal/claude/` as the build package, but `internal/builder/builder.go` is the actual build pipeline.

### 2D template `.claude/` hooks still missing
`templates/2d/.claude/` does not exist. Without hooks, the Claude Code build pipeline can't report events for 2D worlds. This has been flagged in every review but remains unresolved.

### Site CLAUDE.md has several omissions
- `internal/db/` (SQLite persistence, added in commit f666372)
- `internal/webhook/` (GitHub push webhook for self-rebuild)
- `internal/ui/` (shared templ components)
- `CM_HOOK_SECRET` documented as "required" but absent from both env example files

### `scripts/setup.sh` is a no-op
Line 5 prints "not yet implemented". The `just setup` recipe only effectively runs `just setup-playwright`.

## Artifacts

- `thoughts/CoreyCole/research/2026-02-15_23-42-38_documentation-audit-agent-hierarchy.md` — Full research document with all findings

## Action Items & Next Steps

### Priority 1: Fix Inaccuracies
1. **Root CLAUDE.md**: Change "runs as a native binary under systemd" → "runs via air hot-reload under systemd"
2. **Root CLAUDE.md**: Add note about `internal/builder/` package (renamed from `internal/build/`)
3. **Harness CLAUDE.md**: Update Key Packages table — add `internal/builder/`, `internal/logging/`, `internal/gemini/`, `internal/tmux/`
4. **Site CLAUDE.md**: Add `internal/db/`, `internal/webhook/`, `internal/ui/` to architecture table
5. **Site CLAUDE.md**: Add `CM_HOOK_SECRET` to env example files OR document as optional

### Priority 2: Fill Documentation Gaps
6. **Root CLAUDE.md**: Add "WASM Build Constraints" section (5 GB RAM per `wasm-bindgen`)
7. **Root CLAUDE.md**: Add `pkg/worldchannel` to Project Structure table
8. **Root CLAUDE.md**: Document site ↔ harness webhook bridge (`POST /api/world-hatched`)
9. **Root CLAUDE.md**: Note `scripts/setup.sh` is a placeholder
10. **Harness**: Create `.env.example` with all required/optional env vars
11. **2D Template**: Create `templates/2d/.claude/` hooks (copy from 3D)

### Priority 3: Improve for Engineers & Mayors
12. **Root CLAUDE.md**: Add "Architecture: Agent Hierarchy" diagram (President → Mayors → Claude Code)
13. **Root CLAUDE.md**: Document single-bot architecture decision explicitly
14. **Root CLAUDE.md**: Add "Deployment: Site ↔ Harness" section for EC2 + VPS topology
15. **Templates CLAUDE.md**: Ensure mayor-triggered builds are documented (partially done)
16. **Site CLAUDE.md**: Document Claude model used (`anthropic.ModelClaudeSonnet4_5_20250929`)

## Other Notes

### Key File Locations for Documentation Updates
- `CLAUDE.md` (root) — lines 1-155
- `harness/CLAUDE.md` — lines 1-645, Key Packages table at lines 9-24
- `site/CLAUDE.md` — lines 1-120, Architecture table at lines 6-16
- `templates/3d/CLAUDE.md` — lines 1-233
- `templates/2d/CLAUDE.md` — lines 1-396
- `harness/main.go:330-402` — Mayor + President initialization (single bot pattern)
- `pkg/worldchannel/` — Shared Discord channel management used by site and harness
- `scripts/harness-run.sh` — VPS startup sequence

### Latest Plans (post-audit)
The latest plan superseding the original master plan is at `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md`. The original master plan at `2026-02-13_16-05-13` is Docker-centric and partially outdated for the VPS deployment.

### Open Questions from Research
1. Should the master plan be updated in place or should the latest VPS plan be treated as authoritative?
2. Should `DEV_MODE=true` be disabled on VPS before adding mayor functionality?
3. Is the 2D `.claude/` hooks fix blocked on anything?
4. Should there be a separate "Operations Guide" for VPS management?
