---
ticket: CRE-9
workflow: 12ae2656
session: d27d0b79
timestamp: 2026-03-02T06:10:00Z
---

# Research: Go File Splitting — Dependency Analysis

## Questions

1. What are the internal dependencies of the 4 largest Go files?
2. Where are the safe split boundaries?
3. What interface extraction points exist?

## Findings

### File 1: `harness/internal/swarmorch/manager.go` (1981 lines)

#### Current Structure

The `Manager` struct (20 fields) is defined here with 51 functions/methods. The swarmorch package has already undergone significant splitting — alerts, events, health, hooks, learnings, metrics, digest, registry, sessionlog, jsonllog, temporal, and workflows are separate files. What remains in `manager.go` is the core orchestration logic.

#### Manager Fields and Fan-Out

| Field | Methods Using It | Coupling Level |
|-------|-----------------|----------------|
| `m.db` | 20+ methods | Highest — universal |
| `m.logger` | 19 methods | High — logging everywhere |
| `m.eventBus` | 8 methods | Medium |
| `m.mu` | 3 methods (`advanceWorkflow`, `ApproveGate`, `RejectGate`) | Critical — serialization point |
| `m.linearClient` | 6 methods | Medium — nil-guarded |
| `m.completionReg` / `m.startReg` | 3 methods each | Low — session lifecycle |
| `m.jsonlWriters` / `m.jsonlMu` | 3 methods | Low — logging lifecycle |
| `m.alertMgr` | 4 methods | Low — nil-guarded |
| `m.temporalRuntime` | 6 methods | Medium — nil-guarded |

#### Natural Groupings (10 identified)

**Group A — Workflow Lifecycle** (6 methods, ~400 lines)
- `StartWorkflow`, `CancelWorkflow`, `RecoverWorkflows`, `advanceWorkflow`, `completeWorkflow`, `GetWorkflow`
- Key coupling: `advanceWorkflow` (278 lines!) is the most entangled method — calls into Groups B, C, E, F, and project.go
- State: `m.db`, `m.mu`, `m.eventBus`, `m.temporalRuntime`, `m.linearClient`

**Group B — Session Management** (6 methods + 6 helpers, ~350 lines)
- `spawnSession`, `watchSession`, `handleSessionComplete`, `captureTokens`, `createTmuxSession`, `sendSkillPrompt`
- Helpers: `isTmuxSessionAlive`, `TokenFilePath`, `ResultFilePath`, `LearningFilePath`, `SessionName`, `ListActiveSessions`
- State: `m.db`, `m.baseDir`, `m.logsDir`, `m.completionReg`, `m.startReg`, `m.ctxPressure`, `m.jsonlWriters`

**Group C — Learning Capture Bridge** (1 method, ~35 lines)
- `captureLearnings` — routes to methods in `learnings.go`
- Could move to learnings.go trivially

**Group D — Environment Building** (4 methods, ~200 lines)
- `buildEnv`, `populatePreviousContext`, `populateStackContext`, `resolveTicketDescription`
- State: `m.db`, `m.baseDir`, `m.harnessURL`, `m.linearClient`, `m.graphiteClient`

**Group E — Event/Linear Integration** (3 methods, ~100 lines)
- `emitEvent`, `linearComment`, `linearUpdateStatus`
- Most widely called (12+ callers for `emitEvent`, 8+ for `linearComment`)
- State: `m.db`, `m.linearClient`

**Group F — Human Review Gates** (5 methods, ~340 lines)
- `enterGate`, `requireAwaitingReview`, `ApproveGate`, `RejectGate`, `advanceFromGate`
- State: `m.db`, `m.mu`, `m.eventBus`, `m.alertMgr`, `m.linearClient`, `m.temporalRuntime`

**Group G — JSONL Logging** (2 methods, ~20 lines)
- `closeJSONLWriter`, `WriteJSONLEvent`
- State: `m.jsonlMu`, `m.jsonlWriters`

**Group H — Hook Signal API** (4 methods, ~20 lines)
- `SignalStart`, `SignalCompletion`, `IncrementContextPressure`, `GetContextPressure`
- Thin wrappers around types in registry.go and hooks.go

**Group I — Maintenance** (4 methods, ~80 lines)
- `StartMaintenance`, `StopMaintenance`, `maintenanceLoop`, `detectAndAlertStalls`
- State: `m.maintenanceCancel`, `m.temporalRuntime`, `m.alertMgr`

**Group J — Config/Setters** (7 methods, ~60 lines)
- `SetAlertManager`, `SetLinearClient`, `SetGraphiteClient`, `SetTemporalRuntime`, `loadConfig`, `classifyTicket`, `LogsDir`

#### Recommended Split

| New File | Groups | Est. Lines | Risk |
|----------|--------|-----------|------|
| `sessions.go` | B + G + H | ~390 | Low — self-contained lifecycle |
| `gates.go` | F | ~340 | Medium — shares `m.mu` with `advanceWorkflow` |
| `env.go` → rename to `session_env.go` | D | ~200 | Low — pure builders |
| `maintenance.go` | I | ~80 | Low — background loops |
| Keep in `manager.go` | A + C + E + J + struct | ~600 | N/A |

Move `captureLearnings` (Group C) to existing `learnings.go`. Move `emitEvent`/`linearComment`/`linearUpdateStatus` (Group E) to a new `integration.go` or keep in manager.go since they're small and widely called.

#### Critical Constraint

The `m.mu` mutex is acquired by `advanceWorkflow` (Group A), `ApproveGate` (Group F), and `RejectGate` (Group F). This shared lock prevents race conditions between hook-driven completion and human gate actions. Any split must document this lock discipline. Since all files share the `swarmorch` package, the mutex remains accessible — no interface extraction needed.

---

### File 2: `harness/internal/server/create.go` (1046 lines)

#### Current Structure

Implements the "Create World" onboarding flow — conversational chat with a mayor AI, cover art generation, world creation, and Discord channel setup. Defines `createChatSignals` and `InMemoryMessageStore`.

#### Natural Groupings (4 identified)

**Group A — In-Memory Message Store** (lines 44-86, ~40 lines)
- `NewInMemoryMessageStore`, `AddMessage`, `GetMessages`, `DeleteOlderThan`, `DeleteUserMessages`
- Completely self-contained, implements `mayorchat.MessageStore` interface
- Zero dependency on `Server`

**Group B — Chat Conversation Handlers** (lines 89-591, ~500 lines)
- `handleCreatePage`, `handleCreateChat`, `handleCreateScriptedResponse`, `handleCreateScriptedPostRefusal`, `handleCreateScriptedForceCreate`
- Dominant state: `s.CreateConvMgr` (all 5), `s.CreateMDRenderer` (all 5), `s.CreateClaudeClient` (1)
- Internal: all scripted handlers eventually call `prepareCreateCoverArtAndHatch` (Group C)

**Group C — World Creation Pipeline** (lines 594-849, ~255 lines)
- `prepareCreateCoverArtAndHatch`, `hatchCreateWorld`, `saveCoverArtToWorld`, `createDiscordChannelForWorld`
- State: `s.GeminiClient`, `s.WorldManager`, `s.MayorManager`, `s.DB`, `s.DataDir`
- Clear funnel: all conversation paths converge at `prepareCreateCoverArtAndHatch`

**Group D — Cover Art HTTP Handlers** (lines 852-1046, ~195 lines)
- `handleCreateCoverPreview`, `handleCreateGenerateCover`, `handleCreateHatch`
- State: `s.CreateConvMgr`, `s.GeminiClient`
- Also calls `hatchCreateWorld` (Group C)

#### Recommended Split

| New File | Groups | Est. Lines | Risk |
|----------|--------|-----------|------|
| `create_store.go` | A | ~40 | None — zero coupling |
| `create_chat.go` | B + constants + signals struct | ~530 | Low |
| `create_hatch.go` | C + D | ~450 | Low |

All files stay in `package server` and share the `Server` receiver. No interface extraction needed. The call chain `Group B → Group C ← Group D` works naturally through shared methods on `*Server`.

---

### File 3: `harness/internal/server/server.go` (914 lines)

#### Current Structure

Defines the `Server` struct (17 fields), `New` constructor, `RegisterRoutes` (the central routing hub), and ~20 handler methods. `RegisterRoutes` references ~70 handler methods defined across 15 different files in the package.

#### Natural Groupings (6 identified in server.go itself)

**Group A — Core Infrastructure** (lines 51-268, ~270 lines)
- `Server` struct, `New`, `RegisterRoutes`, `registerWorldRoutes`, `handleRoot`, `handleHealth`
- Package-level: `requireUser`, `hookSecretMiddleware`
- `RegisterRoutes` is the central wiring — references every handler in the package

**Group B — World Management Handlers** (lines 341-608, ~270 lines)
- `handleCreateWorld`, `handleWorldView`, `handleCheckpointView`, `handlePrompt`, `handleSaveCheckpoint`
- State: `s.DB`, `s.WorldManager`, `s.Orchestrator`

**Group C — Static File Serving** (lines 612-708, ~100 lines)
- `handleLogStream`, `handleWASMArtifacts`, `handleSharedAssets`
- State: `s.DataDir` only

**Group D — Claude/Chat Events** (lines 726-808, ~80 lines)
- `handleClaudeEvent`, `handleChatMessage`, `handleLineage`
- State: `s.EventBus`, `s.Orchestrator`, `s.DB`

**Group E — Debug Proxy** (lines 811-902, ~90 lines)
- `handleDebugProxy`, `handleWorldStatus`
- State: `s.WorldManager`, `s.DB`

**Group F — Admin** (lines 905-914, ~10 lines)
- `handleAdminUsers`
- State: `s.DB`

#### Recommended Split

| New File | Groups | Est. Lines | Risk |
|----------|--------|-----------|------|
| `world_handlers.go` | B | ~270 | Low |
| `static.go` | C | ~100 | None — DataDir only |
| Keep in `server.go` | A + D + E + F | ~450 | N/A |

Groups D, E, and F are small enough to keep in server.go. The `RegisterRoutes` method must stay with the `Server` struct definition. Package-level utilities (`requireUser`, `hookSecretMiddleware`) stay with the struct.

**Note**: `server.go` is the least urgent split target. The file is well-organized and the handlers are largely independent. The other server package files (`create.go`, `swarm_api.go`, etc.) are already well-separated.

---

### File 4: `harness/internal/swarmorch/project.go` (851 lines)

#### Current Structure

Implements project workflow lifecycle — plan parsing, child ticket creation, dependency graph construction, wave-based scheduling, research decomposition, and Temporal orchestrator integration. All functions are methods on `*Manager` or standalone parsers.

#### Coupling with manager.go

**Manager Fields Accessed**:
- `m.db` — 10 methods
- `m.logger` — 12 methods
- `m.baseDir` — 3 methods
- `m.linearClient` — 3 methods (nil-guarded)
- `m.temporalRuntime` — 2 methods (nil-guarded)

**Manager Methods Called**:
- `m.StartWorkflow` — 4 call sites
- `m.linearComment` — 4 call sites
- `m.spawnSession` — 2 call sites
- `m.emitEvent` — 2 call sites

#### Natural Groupings (6 identified)

**Group A — Plan Creation & Reconciliation** (6 methods, ~250 lines)
- `CreateProjectTicketsFromPlan`, `readProjectPlan`, `ParseProjectPlan`, `createDependencyEdges`, `createMilestones`, `ReconcileProjectTickets`
- State: `m.db`, `m.baseDir`, `m.linearClient`

**Group B — Research Decomposition** (5 methods, ~340 lines)
- `SpawnProjectResearchChildren`, `ParseDecomposeOutput`, `advanceProjectDecompose`, `aggregateResearchFindings`, `hasResearchChildren`
- State: `m.db`, `m.baseDir`, `m.linearClient`
- Calls: `m.StartWorkflow`, `m.emitEvent`, `m.linearComment`, `m.spawnSession`

**Group C — Verification & Wave Scheduling** (4 methods, ~160 lines)
- `SpawnProjectWorkflows`, `advanceProjectVerify`, `CheckProjectProgress`, `advanceProject`
- Calls: `m.StartWorkflow`, `m.linearComment`, `m.spawnSession`, `m.temporalRuntime`

**Group D — Graph Infrastructure** (2 methods, ~75 lines)
- `buildProjectGraph`, `completedChildTickets`
- Shared by Groups B and C

**Group E — Temporal Integration** (1 method, ~35 lines)
- `startProjectOrchestrator`

**Group F — Pure Parsers** (3 functions, ~70 lines)
- `ParseProjectPlan`, `ParseDecomposeOutput`, `nowUTC`
- No Manager dependency at all

#### Recommended Split

| New File | Groups | Est. Lines | Risk |
|----------|--------|-----------|------|
| `project_plan.go` | A | ~250 | Low |
| `project_decompose.go` | B + D | ~415 | Medium — calls 4 manager.go methods |
| `project_verify.go` | C + E | ~195 | Low |
| Keep `project.go` or rename | F (parsers) | ~70 | None |

No interface extraction needed — all methods are on `*Manager` within the same package. The shared graph infrastructure (Group D) should go with decompose since both decompose and verify use it, and decompose is the larger consumer.

---

## Architecture Notes

### Package-Level Split Safety

All 4 target files are within their respective packages (`swarmorch`, `server`). Go allows any file in a package to access all package-level symbols. This means:

1. **No interface extraction is required** for any of the recommended splits
2. **No import changes** are needed within the package
3. **No API surface changes** — external callers see the same types and methods
4. The only risk is **copy-paste errors** when moving methods between files

### The `advanceWorkflow` Problem

`manager.go`'s `advanceWorkflow` method (278 lines, 695-973) is the single most complex function. It implements the full state machine transition and touches 7 of 10 groupings. Options:

1. **Keep in manager.go** — it's the core orchestration logic, belongs with lifecycle
2. **Extract to `statemachine_runner.go`** — isolates the complexity but creates a confusing name
3. **Break into sub-methods** — extract phase-specific handling into private helpers

Recommendation: Keep `advanceWorkflow` in `manager.go` but extract the project-specific phase handlers (lines 800-950) into `project.go` methods that `advanceWorkflow` calls. This reduces `advanceWorkflow` by ~150 lines.

### Shared Utility Functions

Several package-level utilities are defined in unexpected files:
- `toNullString` in `learnings.go` — used by `emitEvent`, `StartWorkflow`, and 5+ other methods in manager.go
- `nowUTC` in `project.go` — used by `CreateProjectTicket` in manager.go
- `slugify` in `imagegen.go` — used by `rooms.go`

These should be consolidated into a `util.go` or similar file during the split.

## Risks and Considerations

1. **The `m.mu` lock discipline** — `advanceWorkflow`, `ApproveGate`, and `RejectGate` all acquire this mutex. If gates move to `gates.go`, the lock semantics must be well-documented. Adding a `// NOTE: acquires m.mu` comment to each method's godoc.

2. **Test file coupling** — `manager_test.go` (31KB) and `project_test.go` (15KB) test methods across all groupings. Tests don't need to be split to match source files, but some helper functions may need to move.

3. **git blame disruption** — moving methods between files loses per-line blame history. Use `git log --follow` or a rename-tracking tool.

4. **Incremental approach** — split one file at a time, run `just check` after each split. Start with the lowest-risk files (create.go, project.go) before tackling manager.go.

5. **No functional changes** — file splits should be pure moves with zero behavioral changes. No refactoring, no renaming, no interface extraction in the same PR.

## Recommendations

### Suggested Order of Implementation

1. **`server/create.go`** → 3 files (lowest risk, clearest boundaries)
   - `create_store.go`, `create_chat.go`, `create_hatch.go`

2. **`swarmorch/project.go`** → 3-4 files (medium risk, moderate coupling with manager.go)
   - `project_plan.go`, `project_decompose.go`, `project_verify.go`

3. **`swarmorch/manager.go`** → 3-4 files (highest risk, most coupling)
   - `sessions.go`, `gates.go`, `session_env.go`, keep core in `manager.go`
   - Move `captureLearnings` to `learnings.go`
   - Consolidate `toNullString`/`nowUTC` into `util.go`

4. **`server/server.go`** → 2 files (lowest priority, already well-organized)
   - `world_handlers.go`, `static.go`, keep core in `server.go`

### Each Split PR Should

- Move functions with their associated constants, types, and package-level helpers
- Preserve exact function signatures and behavior
- Run `just check` to verify compilation
- Run existing tests (no new tests needed for pure moves)
- Add `// NOTE:` comments for shared lock discipline (manager.go only)
