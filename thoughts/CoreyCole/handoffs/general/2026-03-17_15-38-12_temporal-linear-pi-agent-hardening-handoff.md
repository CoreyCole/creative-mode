---
date: 2026-03-17T15:38:15-0700
researcher: CoreyCole
git_commit: f4bdccb9ef662efaaa28d4bb87d353adef487b26
branch: feat/agent-primitives
repository: creative-mode
topic: "Temporal + Linear + Pi Agent Hardening Implementation Strategy"
tags: [implementation, strategy, temporal, linear, swarm, pi-agent]
status: complete
last_updated: 2026-03-17
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: general temporal-linear-pi-agent-hardening

## Task(s)
- **Completed:** Full branch review request for the temporal+linear+pi agent setup used for research and plan creation.
- **Completed:** Identified and validated 5 actionable findings (3 P1, 2 P2) on `feat/agent-primitives`.
- **Completed:** Produced a phased implementation plan covering all 5 findings.
- **Current phase status:** Planning complete; implementation has not started in this session.

Primary plan document:
- `thoughts/CoreyCole/plans/2026-03-17_15-36-03_temporal-linear-pi-agent-hardening.md`

## Critical References
- `thoughts/CoreyCole/plans/2026-03-17_15-36-03_temporal-linear-pi-agent-hardening.md`
- `harness/internal/swarmorch/agent.go:158-161`
- `harness/internal/server/server.go:168,698-706`

## Recent changes
- Added implementation plan: `thoughts/CoreyCole/plans/2026-03-17_15-36-03_temporal-linear-pi-agent-hardening.md:1`
- Added this handoff: `thoughts/CoreyCole/handoffs/general/2026-03-17_15-38-12_temporal-linear-pi-agent-hardening-handoff.md:1`
- No application source code was edited in this session.

## Learnings
- Agent runner has a real hang risk: `readAgentLoop` can error (tool limit path) while process is still alive, then `cmd.Wait()` blocks without prior termination handling (`harness/internal/swarmorch/agent.go:158-161,359-361`).
- Swarm API auth is currently misconfiguration-sensitive and fail-open because `hookSecretMiddleware` allows all requests when `CM_HOOK_SECRET` is unset (`harness/internal/server/server.go:168,698-706`).
- Branch currently fails harness Go test/build checks due to generated templ/datastar calls using non-constant format strings (representative failure in `harness/views/swarm/dashboard_templ.go:176`; similar in mayor/admin/imagegen/world generated files).
- Orphan span cleanup is format-fragile: RFC3339 `started_at` compared against SQLite `datetime(...)` string in cleanup query (`harness/internal/db/queries/swarm.sql:126-132`).
- Codex auth path mismatch exists between login writer and runtime reader (`harness/agents/lib/codex-login.js:20,62` vs `harness/agents/lib/codex-auth.js:12`).

## Artifacts
- Plan artifact:
  - `thoughts/CoreyCole/plans/2026-03-17_15-36-03_temporal-linear-pi-agent-hardening.md`
- Handoff artifact:
  - `thoughts/CoreyCole/handoffs/general/2026-03-17_15-38-12_temporal-linear-pi-agent-hardening-handoff.md`
- Review output from delegated reviewer (inline findings used; file output was not useful):
  - `review.md`
- Verification command output (not persisted as file) run during session:
  - `cd harness && go test ./...` (failed with non-constant format string errors in generated templ files)

## Action Items & Next Steps
1. Implement **Phase 1** from plan: fix templ/datastar usage in templ sources and regenerate templates until `cd harness && go test ./...` passes.
2. Implement **Phase 2**: make `/api/swarm` auth fail-closed outside dev when `CM_HOOK_SECRET` is missing/invalid.
3. Implement **Phase 3**: enforce deterministic subprocess teardown on `loopErr` before/with `cmd.Wait()`.
4. Implement **Phase 4**:
   - normalize orphan span cleanup timestamp comparison,
   - unify Codex token file path between login + runtime auth.
5. Re-run full verification matrix from the plan, then do a final review pass focused on regressions in swarm dashboard/SSE and task lifecycle.

## Other Notes
- User explicitly confirmed: include both P1 and P2 in the plan scope.
- The immediate blocker for confidence is Phase 1 because branch is currently red on `go test ./...` in `harness`.
- No branch/commit changes were made by this session; handoff is planning-only.
