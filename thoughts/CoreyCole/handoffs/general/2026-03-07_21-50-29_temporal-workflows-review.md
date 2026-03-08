---
date: 2026-03-07T21:50:29-08:00
researcher: CoreyCole
git_commit: 7e6eb28cff4285b24ec1ac287ca88722b08df796
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Temporal Workflows + Manager + HTTP API Review"
tags: [implementation, temporal, swarm, workflows, review]
status: complete
last_updated: 2026-03-07
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Phase 3D+3E Temporal Workflows Implementation — Ready for Review

## Task(s)

**Completed:** Implemented Phase 3D+3E of the agent primitives plan — Temporal workflows, SwarmManager, and HTTP API.

All code compiles and passes `just check` (Go + Rust + JS linters). The implementation follows the plan but has NOT been reviewed against Temporal SDK best practices yet.

### What was implemented:
1. **workflows.go** — Two Temporal workflow functions (`ResearchWorkflow`, `CodeChangePlanWorkflow`) with shared `runResearchSteps` helper, fan-out/collect via `workflow.Go`/channels, deterministic UUIDs via `workflow.SideEffect`
2. **manager.go** — `SwarmManager` wrapping Temporal client+worker lifecycle
3. **swarm_api.go** — 4 HTTP handlers behind `hookSecretMiddleware`
4. **server.go** — `SwarmManager` field + route registration
5. **main.go** — SwarmManager init gated on `CM_SWARM_TEMPORAL=true`, graceful shutdown
6. **helpers.go** — Removed `//nolint:unused` from `sanitizeSlug`, fixed `truncateJSON` to remove unused parameter

## Critical References

- Plan document that guided this implementation: stored in the previous session transcript at `/home/deploy/.claude/projects/-home-deploy-creative-mode/9d765281-9e76-4b16-bac4-aa431012ce0f.jsonl`
- Phases 3A-3C created the swarmorch package: `harness/internal/swarmorch/` (runner.go, agent.go, helpers.go, activities.go, types.go)
- Temporal SDK v1.40.0 — `go.temporal.io/sdk` in `harness/go.mod:26`

## Recent changes

- `harness/internal/swarmorch/workflows.go` — NEW: Two workflow functions + helpers
- `harness/internal/swarmorch/manager.go` — NEW: SwarmManager struct and lifecycle
- `harness/internal/server/swarm_api.go` — NEW: 4 HTTP handlers
- `harness/internal/server/server.go:60` — Added `SwarmManager *swarmorch.SwarmManager` field
- `harness/internal/server/server.go:162-168` — Added swarm API route group
- `harness/main.go:33` — Added `swarmorch` import
- `harness/main.go:325-338` — SwarmManager initialization gated on `CM_SWARM_TEMPORAL=true`
- `harness/main.go:347` — Wired `srv.SwarmManager = swarmManager`
- `harness/main.go:354-356` — SwarmManager.Stop() in graceful shutdown
- `harness/internal/swarmorch/helpers.go:21` — `truncateJSON` no longer takes `maxLen` param
- `harness/internal/swarmorch/helpers.go:101-102` — Removed `//nolint:unused` from `sanitizeSlug`
- `harness/internal/swarmorch/agent.go:161,279,313` — Updated `truncateJSON` call sites

## Learnings

- **dupl linter**: The Go `dupl` linter is active and catches structurally similar handler functions. Fixed by extracting a `startSwarmTask` helper with a `workflowStarter` function type.
- **Nil method reference panic**: When using `s.SwarmManager.StartResearch` as a function value, the nil check must happen BEFORE the method reference (in the handler), not inside the shared helper. Otherwise dereferencing a nil pointer to get the method panics.
- **revive early-return**: The linter prefers `if condition == nil { return }` over `if condition != nil { ...long block... }` in deferred functions.
- **staticcheck S1016**: When two structs have identical field names and types (like `CodeChangePlanWorkflowInput` and `ResearchWorkflowInput`), use type conversion `ResearchWorkflowInput(input)` instead of struct literal.
- **unparam**: If a function parameter always receives the same value across all call sites, the linter flags it. Fixed `truncateJSON` by removing the `maxLen` param.
- **Temporal SDK logger**: `go.temporal.io/sdk/log.NewStructuredLogger(logger)` adapts `*slog.Logger` for Temporal's client options.

## Artifacts

- `harness/internal/swarmorch/workflows.go` — Temporal workflow definitions
- `harness/internal/swarmorch/manager.go` — SwarmManager lifecycle
- `harness/internal/server/swarm_api.go` — HTTP API handlers
- Modified: `harness/internal/server/server.go`, `harness/main.go`, `harness/internal/swarmorch/helpers.go`, `harness/internal/swarmorch/agent.go`

## Action Items & Next Steps

1. **Review Temporal best practices** — The next session should review the workflows implementation against Temporal SDK best practices:
   - Determinism rules: Are all non-deterministic calls properly wrapped in `workflow.SideEffect`?
   - Error handling: Is the deferred cleanup pattern (using `workflow.NewDisconnectedContext`) correct?
   - Activity options: Are timeouts and retry policies appropriate?
   - Fan-out pattern: Is `workflow.Go` + `workflow.NewChannel` used correctly for parallel research agents?
   - Nil activity pointer pattern: `var a *SwarmActivities` — is this the recommended way to reference activity methods?
   - Should workflows use `workflow.GetLogger()` for logging?
   - Are there any issues with the `workflow.Now(ctx)` usage for timestamps?

2. **Test manually** — Once Temporal is running:
   ```bash
   curl -X POST http://localhost:8080/api/swarm/tasks/research \
     -H "X-Hook-Secret: $CM_HOOK_SECRET" \
     -H "Content-Type: application/json" \
     -d '{"requestText":"How does the EventBus work?"}'
   ```

## Other Notes

- **Temporal service**: `temporal-dev.service` (systemd), namespace `swarm`, ports 7233 (gRPC) + 8233 (UI)
- **Task queue**: `swarm-agents`
- **Workflow IDs**: `swarm-research-{taskID}` and `swarm-codeplan-{taskID}`
- **Node path**: Hardcoded to `/home/deploy/.nix-profile/bin/node` in manager.go
- **Agent scripts dir**: `{repoRoot}/harness/agents/` — JS agent scripts that activities invoke
- The `truncateJSON` signature change affected 3 call sites in `agent.go` — all updated
- The swarm API routes are at `/api/swarm/tasks/*` (research, code-change-plan, :taskID, :taskID/cancel)
