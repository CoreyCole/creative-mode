---
ticket: CRE-5
workflow: 124c06f6
session: a100526a
phase: research
timestamp: 2026-03-02T02:48:02Z
---

# Handoff: CRE-5 Research — Tech Debt Audit

## What Was Done

Completed comprehensive tech debt audit of the creative-mode codebase covering four areas:

1. **Duplicated code patterns** — Identified 16 categories of duplication across harness Go code and Rust templates
2. **Inconsistent conventions** — Found 10 areas of inconsistency in logging, error handling, HTTP responses, env var loading, naming, and timestamps
3. **Missing observability** — Documented 18 gaps including trivial health endpoint, no request IDs, no metrics, silent error swallowing, and no alerting for non-swarm subsystems
4. **Maintainability issues** — Found oversized types (Server god struct, 1822-line Manager), zero tests on auth/server/world, untyped statuses, dead code, and raw SQL bypassing sqlc

## Key Artifacts

- **Research document**: `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md`
- **Linear comment**: RESEARCH comment posted with summary

## Recommendations Summary

18 prioritized recommendations in three tiers:

**High**: Swarm nil guard middleware, standardize error handling, enrich health endpoint, add request IDs, enrich request logger

**Medium**: Extract pkg/openclaw client, shared Bevy crate, type checkpoint/template statuses, log discarded errors, extract shortID utility, consolidate mimeToExt

**Lower**: Split Manager monolith, abstract Temporal vs goroutine, add HTTP handler tests, remove dead code, consolidate path sanitization, resolve OPENCLAW_HOME once

## Next Phase Context

This is a `project` type ticket. The research should inform project plan decomposition into child tickets. Recommended groupings:

1. **Quick wins** (swarm middleware, mimeToExt, shortID, path sanitization, OPENCLAW_HOME) — single PR
2. **Observability** (health endpoint, request IDs, request logger enrichment, error logging) — single PR
3. **Convention standardization** (error handling, HTTP response codes, env var loading) — single PR
4. **Template consolidation** (shared Bevy crate) — separate PR per template
5. **Structural improvements** (Manager split, typed statuses, dead code removal) — multiple PRs
6. **Test coverage** (auth middleware, HTTP handlers, EventBus) — separate effort
