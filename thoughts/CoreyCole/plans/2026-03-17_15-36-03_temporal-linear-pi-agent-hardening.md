# Temporal + Linear + Pi Agent Hardening Plan

## Overview

Stabilize and secure the branch’s research/plan orchestration path by fixing all identified P1 and P2 issues: process shutdown correctness, swarm API auth hardening, build-breaking templ/datastar usage, orphaned span cleanup reliability, and Codex token path consistency.

## Current State Analysis

The branch introduces major orchestration functionality, but currently has five concrete issues:

- Agent subprocess lifecycle can hang when `readAgentLoop` returns early (`harness/internal/swarmorch/agent.go:158-161`, tool-limit path at `:359-361`).
- `/api/swarm` is protected by middleware that allows all requests when `CM_HOOK_SECRET` is unset (`harness/internal/server/server.go:168`, `:698-706`).
- `go test ./...` fails due to generated templ/datastar calls using non-constant format strings (example failures include `harness/views/swarm/dashboard_templ.go:176`, `harness/views/mayor/dashboard_templ.go:171`).
- Orphaned span cleanup compares differently formatted timestamps (`harness/internal/db/queries/swarm.sql:126-132`).
- Codex login helper writes tokens to a different path than runtime reader uses (`harness/agents/lib/codex-login.js:11,20,62` vs `harness/agents/lib/codex-auth.js:12`).

## Desired End State

Research/plan orchestration can be safely run in production and local dev:

1. Agent subprocesses always terminate cleanly (or are forcibly terminated) on loop errors.
2. Swarm task APIs are fail-closed outside dev when hook secret is missing/invalid.
3. Harness Go build/test passes with no templ/datastar vet/build failures.
4. Orphan span recovery consistently marks stale running spans after restart.
5. Codex login + runtime auth use a single compatible token location.

### Key Discoveries
- Early loop error currently precedes `cmd.Wait()` with no guaranteed process termination (`harness/internal/swarmorch/agent.go:158-161`).
- Swarm routes are using shared webhook middleware and inherit fail-open behavior (`harness/internal/server/server.go:168`, `:698-706`).
- Datastar helper usage in templ sources currently generates code rejected by Go vet/build (`harness/views/swarm/dashboard.templ:145`, similar patterns in admin/mayor/imagegen/world templates).
- Cleanup query uses lexical comparison against mixed datetime formats (`harness/internal/db/queries/swarm.sql:126-132`).
- Token file mismatch makes successful login not reliably usable by runtime (`harness/agents/lib/codex-login.js`, `harness/agents/lib/codex-auth.js`).

## What We’re NOT Doing

- No redesign of swarm dashboard UX or task orchestration semantics.
- No expansion of auth model beyond hardening existing `/api/swarm` gate.
- No migration to a new OAuth provider or token schema.
- No broad refactor of all timestamp handling outside swarm span lifecycle.

## Implementation Approach

Apply targeted, minimal-risk fixes in small phases with immediate verification. Keep behavior unchanged except where security/failure handling requires stricter guarantees.

---

## Phase 1: Restore Green Build for templ/datastar

### Overview
Fix templ source patterns that generate non-constant format string calls and regenerate templ output.

### Changes Required

#### 1. Normalize datastar helper usage in templ sources
**Files**:
- `harness/views/swarm/dashboard.templ`
- `harness/views/mayor/dashboard.templ`
- `harness/views/admin/admin.templ`
- `harness/views/imagegen/placement.templ`
- `harness/views/world/overlay.templ`

**Changes**:
- Replace string-concatenation / dynamic expression patterns in `datastar.GetSSE`/`PostSSE` calls with the project’s accepted constant-safe pattern.
- Regenerate `*_templ.go` outputs.

### Success Criteria

#### Automated Verification:
- [ ] Templ generation succeeds: `cd harness && just generate`
- [ ] Harness tests/build checks succeed: `cd harness && go test ./...`

#### Manual Verification:
- [ ] `/swarm` loads and opens SSE stream without console errors.
- [ ] Mayor/admin/imagegen interactions still fire expected requests.

---

## Phase 2: Make Swarm API Auth Fail-Closed

### Overview
Prevent unauthenticated swarm API access when secrets are misconfigured.

### Changes Required

#### 1. Introduce strict swarm auth middleware behavior
**File**: `harness/internal/server/server.go`

**Changes**:
- Add swarm-specific middleware behavior that requires `CM_HOOK_SECRET` for `/api/swarm` when not in dev mode.
- If secret is unset in non-dev, return explicit server error for swarm API requests (and log startup warning/error).
- Keep current local-dev ergonomics when `DEV_MODE=true`.

#### 2. Update environment docs
**Files**:
- `harness/.env.example`
- `harness/CLAUDE.md` (if needed for operator guidance)

**Changes**:
- Clarify that `CM_HOOK_SECRET` is mandatory for production swarm API protection.

### Success Criteria

#### Automated Verification:
- [ ] Unit/integration server tests updated or added for middleware behavior (dev vs non-dev).
- [ ] `cd harness && go test ./internal/server/...`

#### Manual Verification:
- [ ] In non-dev mode with missing secret, `/api/swarm/*` rejects requests.
- [ ] In non-dev with valid `X-Hook-Secret`, swarm endpoints work.
- [ ] In dev mode, expected local behavior remains usable.

---

## Phase 3: Ensure Agent Process Termination on Loop Error

### Overview
Eliminate hanging activities by making subprocess teardown deterministic.

### Changes Required

#### 1. Add explicit shutdown path before `cmd.Wait()` on loop errors
**File**: `harness/internal/swarmorch/agent.go`

**Changes**:
- On `loopErr != nil`, close stdin and terminate process when still running before waiting.
- Preserve stderr capture and span failure reporting.
- Ensure no zombie process remains when tool-call limit is exceeded.

#### 2. Add regression test coverage
**Files**:
- `harness/internal/swarmorch/*_test.go` (new or existing test file)

**Changes**:
- Add test harness case that simulates loop error and verifies subprocess is cleaned up.

### Success Criteria

#### Automated Verification:
- [ ] `cd harness && go test ./internal/swarmorch/...`
- [ ] Full harness test sweep still passes: `cd harness && go test ./...`

#### Manual Verification:
- [ ] Trigger task with forced tool-call-limit overflow; task fails quickly without hanging worker.
- [ ] No lingering child process from failed agent run.

---

## Phase 4: Reliability Hardening (P2)

### Overview
Fix two consistency bugs that degrade operational reliability.

### Changes Required

#### 1. Normalize orphan-span cleanup timestamp comparison
**File**: `harness/internal/db/queries/swarm.sql`

**Changes**:
- Compare timestamps in a normalized format (`datetime(started_at)` vs `datetime('now', '-15 minutes')`) or equivalent robust expression.
- Regenerate sqlc outputs.

#### 2. Unify Codex token read/write path
**Files**:
- `harness/agents/lib/codex-auth.js`
- `harness/agents/lib/codex-login.js`

**Changes**:
- Use one canonical token path compatible with runtime and login flow.
- Update error/help text accordingly.

### Success Criteria

#### Automated Verification:
- [ ] SQLC generation succeeds: `cd harness && sqlc generate`
- [ ] Swarm DB/query package compiles: `cd harness && go test ./internal/db/... ./internal/swarmorch/...`
- [ ] Agent JS lint/check passes: `cd harness/agents && npm run lint`

#### Manual Verification:
- [ ] Simulated stale running span is marked failed by cleanup query.
- [ ] Running codex login produces credentials usable immediately by runtime auth loader.

---

## Testing Strategy

### Unit Tests
- Middleware tests for swarm auth behavior in dev/non-dev and secret present/absent cases.
- Agent runner teardown test on loop error and on tool-call threshold breach.
- Query behavior test for orphaned span cleanup (integration-style with SQLite fixture if available).

### Integration Tests
- Start a swarm task end-to-end via API; verify authorization, span lifecycle, and failure handling.
- Verify dashboard SSE endpoints still operate after templ/datastar fix.

### Manual Testing Steps
1. Run `cd harness && go test ./...` and confirm no datastar format-string failures.
2. Hit `/api/swarm/tasks` with and without `X-Hook-Secret` under prod-like env.
3. Execute a task that triggers agent loop error; verify fast failure + no lingering process.
4. Insert an old running span and run cleanup path; verify status transitions to failed.
5. Run codex login script and immediately execute a flow that calls `getCodexAccessToken()`.

## Performance Considerations

- Process termination changes must avoid long waits; prefer bounded shutdown then kill.
- Auth middleware checks are constant-time header comparisons and negligible overhead.
- Timestamp normalization in cleanup query runs over running spans only and should remain cheap.

## Migration Notes

- No schema migration required for these fixes (query-only adjustment for cleanup behavior).
- Operational requirement: set `CM_HOOK_SECRET` in non-dev deployments using swarm APIs.

## References

- Review context: branch `feat/agent-primitives` vs `origin/main`
- Agent loop lifecycle: `harness/internal/swarmorch/agent.go:158-161,359-361`
- Swarm route + auth middleware: `harness/internal/server/server.go:168,698-706`
- Timestamp cleanup query: `harness/internal/db/queries/swarm.sql:126-132`
- Codex token paths: `harness/agents/lib/codex-login.js`, `harness/agents/lib/codex-auth.js`
