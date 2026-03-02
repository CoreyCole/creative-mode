# Overstory Additional Swarm Improvements — Implementation Plan

## Overview

Wire 15 additional improvements identified from the Overstory deep dive into the Creative Mode swarm orchestrator. These range from connecting already-built but disconnected infrastructure (~30 lines each) to new security guards, observability patterns, health monitoring enhancements, and workflow flexibility. All changes are additive to the existing 6-phase plan and can be implemented as independent tickets.

**Research**: `thoughts/CoreyCole/research/2026-03-01_23-00-00_overstory-additional-improvements.md`
**Existing plan**: `thoughts/CoreyCole/plans/2026-03-01_21-37-45_overstory-swarm-improvements.md`
**Architecture comparison**: `thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md`

## Phase Dependencies

All phases are independent unless noted. Recommended implementation order follows ROI:

```
Phase A (Wire Disconnected Code)     ── independent, highest ROI
Phase B (Security Hardening)         ── independent, highest safety impact
Phase C (Health Monitoring)          ── independent
Phase D (Learning Enhancements)      ── independent
Phase E (Event Observability)        ── independent
Phase F (Doctor Health Check)        ── independent
Phase G (Workflow Overrides)         ── independent
Phase H (Auto-Insight Extraction)    ── depends on Phase A (transcript wiring)
```

Migration numbering: Phase A uses no new migrations (columns already exist from 007). The existing plan claims migrations `009_session_checkpoints.sql`, `010_structured_learnings.sql`, and reserves `011`. The highest existing migration is `008_human_gates.sql`. This plan's migrations start after the existing plan's range:

- Phase C: `012_activity_tracking.sql`
- Phase D: `013_learning_classification.sql`
- Phase G: `014_workflow_overrides.sql`

**Note**: SQLite does not support `ALTER TABLE ADD COLUMN IF NOT EXISTS`, so migrations must not be run twice. The migration runner should track applied migrations (e.g., via a `schema_migrations` table).

## Current State Analysis

The swarm orchestrator (`internal/swarmorch/`) runs Linear tickets through multi-phase workflows via Claude Code sessions in tmux. Key gaps:

1. **Disconnected infrastructure**: The `transcript/` package (ParseFile, DiscoverTranscript, EstimateCost) and `prompt/` package (RenderPrompt, ContentHash) are fully implemented and tested but never called from production code. Migration 007 added 7 token/cost columns to `swarm_sessions` that are never written. The `swarm_prompt_versions` table and sqlc queries exist but are never used.

2. **Minimal security**: Only 4 bash deny patterns (`hooks.go:22-27`). No path boundary enforcement for Write/Edit. No secret redaction. PreToolUse hook only matches `Bash` tool.

3. **Flat stall detection**: Single 45-minute threshold (`health.go:18`) for all phases. Uses only `updated_at` which doesn't reflect tool activity. Dashboard trusts DB state without verifying tmux liveness.

4. **Blunt learning decay**: Flat 0.95 multiplier for all learnings regardless of category. No classification tiers. Tags column never populated.

5. **Raw event payloads**: PostToolUse events include full `tool_input` (can be 20KB+). No `phase` or `file` fields — dashboard reads them but they're always empty strings (`swarm_dashboard.go:129-131`). Typed struct events are silently dropped by SSE handler (`swarm_dashboard.go:117-119`).

### Key Discoveries:
- `handleSessionComplete` (`manager.go:479-569`) never calls transcript parsing — it calls `CompleteSwarmSession` (`swarm.sql:54-57`) with only `result, detail, duration_sec, id` — the `TotalTokens` field is left at zero value, so even `total_tokens` is never actually populated
- `spawnSession` (`manager.go:295-394`) never calls `RenderPrompt` or `UpsertSwarmPromptVersion` — it invokes skills directly via tmux send-keys
- `createLearning` (`learnings.go:22-41`) takes 8 params (after receiver + ctx): `workflowID, sessionID, ticketID, category, phase, severity, title, content`. The sqlc-generated `CreateSwarmLearningParams` struct has a `Tags sql.NullString` field, but `createLearning` never sets it (zero-valued → SQL NULL)
- EventBus SSE handler (`swarm_dashboard.go:117`) type-asserts to `map[string]any` — typed structs (SessionSpawnedEvent, WorkflowCompleteEvent, etc.) are silently dropped
- PostToolUse events (`swarm_hooks.go:141-147`) publish `input` (full tool args) but no `phase` or `file` — dashboard expects both at `swarm_dashboard.go:129-131`
- `detectAndAlertStalls` exists at `manager.go:1648-1673` (the review's concern was unfounded)
- `CreateSwarmLearning` SQL (`swarm_learnings.sql:3-5`) already accepts `tags` as the 11th parameter

## Desired End State

After all phases complete:
- Every completed session has per-bucket token counts, model, estimated cost, and prompt version ID in the DB
- 20+ bash deny patterns block destructive commands (git push, rm -rf, sudo, etc.)
- Write/Edit tools are path-restricted to the project root
- Secrets are redacted from JSONL logs and EventBus events
- Stall thresholds are per-phase (20min for PR, 75min for research/implement)
- Tool activity timestamps prevent false stall alerts during long active sessions
- Dashboard verifies tmux liveness during SSE ticks (ZFC reconciliation)
- Learnings have classification tiers with differentiated decay rates
- Event payloads are filtered to ~200 bytes (file_path only, no content)
- SSE supports Last-Event-ID reconnection
- `/api/swarm/doctor` verifies DB, tmux, Linear, Graphite, Discord, disk
- Workflows can skip phases/gates via per-workflow overrides
- Transcripts are auto-analyzed for tool profiles and hot files

### Verification:
- `just check` passes (Go + Rust compile)
- All existing `manager_test.go` tests pass
- New unit tests for each phase pass
- Dashboard SSE shows real-time tool activity with phase and file info
- Token costs visible in session detail on dashboard

## What We're NOT Doing

- **Agent hierarchy** (coordinator→lead→specialist) — sequential phases are sufficient
- **SQLite mail system** — HTTP hooks + EventBus handle coordination
- **4-tier merge resolution** — Graphite stacking handles our branch strategy
- **Runtime adapter abstraction** — we only use Claude Code
- **AI triage for stalled sessions** — not worth the token cost for our volume
- **Dashboard UI redesign** — existing templ components are extended, not rewritten
- **Temporal integration changes** — all changes work in both goroutine and Temporal modes

## Implementation Approach

Group the 15 items into 8 phases ordered by ROI. Low-effort items that wire existing code go first. Each phase is a single Linear ticket suitable for a swarm `code` workflow.

---

## Phase A: Wire Disconnected Infrastructure

### Overview
Connect the transcript token extraction, prompt version tracking, and learning tags that are already built but never called. This is the highest-ROI work — ~60 lines of new code to activate hundreds of lines of existing infrastructure.

### Changes Required:

#### A.1 Wire Transcript Token Extraction + Cost Tracking

**File**: `harness/internal/db/queries/swarm.sql`
**Changes**: Add a new query that updates the token columns added by migration 007.

```sql
-- name: CompleteSwarmSessionWithTokens :exec
UPDATE swarm_sessions
SET result = ?, detail = ?, duration_sec = ?, total_tokens = ?,
    input_tokens = ?, output_tokens = ?, cache_read_tokens = ?,
    cache_creation_tokens = ?, model_used = ?, estimated_cost_usd = ?,
    prompt_version_id = ?, completed_at = datetime('now')
WHERE id = ?;
```

**File**: `harness/internal/swarmorch/manager.go`
**Changes**: In `handleSessionComplete` (after line 523 where `CompleteSwarmSession` is called), add transcript discovery and parsing. Replace `CompleteSwarmSession` with `CompleteSwarmSessionWithTokens`.

```go
// Replace CompleteSwarmSession (lines 510-515) with CompleteSwarmSessionWithTokens.
// The existing code already parses StartedAt at lines 502-508:
//   startedAt, parseErr := time.Parse("2006-01-02 15:04:05", session.StartedAt)
// Reuse that parsed time for transcript discovery.

// Discover and parse transcript for token/cost tracking.
var tokenSummary *transcript.TokenSummary
projectKey := transcript.ProjectKeyFromPath(m.baseDir)
// startedAt is already parsed above (lines 502-508) — reuse it.
if startedAt.IsZero() {
    startedAt = session.StartedAt // fallback: DiscoverTranscript will fail gracefully
}
transcriptPath, discoverErr := transcript.DiscoverTranscript(
    transcript.DefaultBaseDir(), projectKey, startedAt,
)
if discoverErr == nil {
    tokenSummary, _ = transcript.ParseFile(transcriptPath)
}

// Use CompleteSwarmSessionWithTokens instead of CompleteSwarmSession
// to populate ALL token columns from migration 007, including total_tokens
// (which was never populated by the original CompleteSwarmSession — the
// TotalTokens field was left at zero value).
```

**Note on types**: `session.StartedAt` is a `string` (sqlc maps SQLite `TEXT` datetime columns to `string`). The existing code at lines 502-508 already parses it with `time.Parse("2006-01-02 15:04:05", session.StartedAt)`. We reuse that parsed `startedAt` variable — no additional parsing needed.

**File**: `harness/internal/swarmorch/manager.go` (imports)
**Changes**: Add import for `"creative-mode/harness/internal/swarm/transcript"`

#### A.2 Wire Prompt Version Tracking

**File**: `harness/internal/swarmorch/manager.go`
**Changes**: In `spawnSession` (around line 297-303 where the skill is resolved), add prompt rendering and version upsert.

```go
// After resolving the skill (line 297), add:
var promptVersionID string
rendered, renderErr := prompt.RenderPrompt(wf.Phase, prompt.PromptContext{
    TicketID:   wf.TicketID,
    WorkflowID: wf.ID,
    SessionID:  sessionID,
    Phase:      string(wf.Phase),
    Attempt:    wf.Attempt,
    ResultPath: swarm.ResultFilePath(sessionID),
    TicketURL:  ticketURL,     // from buildEnv
    BranchName: wf.Branch,
})
if renderErr == nil {
    versionID, upsertErr := m.db.UpsertSwarmPromptVersion(ctx, sqlc.UpsertSwarmPromptVersionParams{
        ID:          uuid.New().String()[:8],
        Phase:       string(wf.Phase),
        ContentHash: rendered.ContentHash,
    })
    if upsertErr == nil {
        promptVersionID = versionID
    }
}
```

Pass `promptVersionID` through to the session creation and completion flow. Store it on the session record.

**File**: `harness/internal/swarmorch/manager.go` (imports)
**Changes**: Add import for `"creative-mode/harness/internal/swarm/prompt"`

#### A.3 Populate Learning Tags

**File**: `harness/internal/swarmorch/learnings.go`
**Changes**: Update `createLearning` to derive and pass tags. Currently the function calls `CreateSwarmLearning` with an empty `tags` field (the 11th parameter is never set). The SQL already accepts `tags` as a TEXT column.

```go
// Add a helper to derive tags from context:
func deriveLearningTags(phase swarm.Phase, category swarm.LearningCategory) string {
    var tags []string
    if phase != "" {
        tags = append(tags, string(phase))
    }
    tags = append(tags, string(category))
    return strings.Join(tags, ",")
}
```

Update `createLearning` to call `deriveLearningTags` and pass the result as the `Tags` field in `CreateSwarmLearningParams`.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes from project root
- [ ] Existing tests pass: `go test ./harness/internal/swarmorch/...`
- [ ] Existing tests pass: `go test ./harness/internal/swarm/transcript/...`
- [ ] Existing tests pass: `go test ./harness/internal/swarm/prompt/...`
- [ ] New test: `TestHandleSessionCompleteWithTokens` — mock session completion writes token data (including `total_tokens` which was previously always zero)
- [ ] New test: `TestSpawnSessionTracksPromptVersion` — verify prompt version upsert (`:one` query returns ID via `RETURNING id`)
- [ ] New test: `TestDeriveLearningTags` — verify tag generation from phase/category
- [ ] New test: `TestDiscoverTranscript` — verify transcript discovery by timestamp matching (currently untested — `discover.go` has zero test coverage)
- [ ] sqlc generate runs cleanly after adding new query

#### Manual Verification:
- [ ] Run a swarm workflow end-to-end, verify `swarm_sessions` rows have non-zero token columns
- [ ] Verify `swarm_prompt_versions` table has entries after sessions run
- [ ] Verify `swarm_learnings` rows have populated `tags` column
- [ ] Check dashboard workflow detail shows token/cost info per session

---

## Phase B: Security Hardening

### Overview
Expand the bash deny list from 4 to 20+ patterns, add Write/Edit path boundary guards, and add secret redaction to JSONL logs and EventBus events.

### Changes Required:

#### B.1 Expanded Bash Deny Patterns

**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: Expand `swarmDenyPatterns` (lines 22-27) with additional patterns.

```go
var swarmDenyPatterns = []*regexp.Regexp{
    // Existing: project-specific build guards
    regexp.MustCompile(`cargo\s+(build|clippy|check)`),
    regexp.MustCompile(`go\s+build`),
    regexp.MustCompile(`templ\s+generate`),
    regexp.MustCompile(`just\s+generate`),

    // Destructive git operations
    regexp.MustCompile(`git\s+push`),
    regexp.MustCompile(`git\s+reset\s+--hard`),
    regexp.MustCompile(`git\s+clean\s+-[fd]`),
    regexp.MustCompile(`git\s+checkout\s+--\s`),

    // File destruction
    regexp.MustCompile(`\brm\s+-r`),
    regexp.MustCompile(`\bsudo\s`),

    // Package managers (could modify lock files, install malicious deps)
    regexp.MustCompile(`\bnpm\s+install\b`),
    regexp.MustCompile(`\bbun\s+(install|add)\b`),
    regexp.MustCompile(`\bpip\s+install\b`),

    // Runtime eval (bypass shell guards)
    regexp.MustCompile(`\bnode\s+-e\b`),
    regexp.MustCompile(`\bpython3?\s+-c\b`),
    regexp.MustCompile(`curl\s+.*\|\s*(sh|bash)\b`),

    // Secret exfiltration
    regexp.MustCompile(`curl\b.*\$(ANTHROPIC_API_KEY|LINEAR_API_KEY|GRAPHITE_TOKEN|DISCORD_BOT_TOKEN)`),
    regexp.MustCompile(`\becho\b.*\$(ANTHROPIC_API_KEY|LINEAR_API_KEY|GRAPHITE_TOKEN|DISCORD_BOT_TOKEN)`),
}
```

**Note on `git push`**: The PR phase uses Graphite CLI (`gt stack submit`) which internally calls `git push`. This is safe because the `swarm-code-pr` skill invokes `gt` directly — the deny pattern `git\s+push` won't match `gt stack submit`. If a session ever tries raw `git push` it will be blocked, which is the intended behavior (all pushes should go through Graphite).

#### B.2 Write/Edit Path Boundary Guards

**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: Add a new function to check file paths against the project root.

```go
// IsPathOutOfBounds returns true if a file_path is outside the allowed project root.
// Uses EvalSymlinks to resolve symlinks (prevents symlink-based path traversal).
// Falls back to filepath.Clean for non-existent files (common with Write tool).
func IsPathOutOfBounds(filePath, projectRoot string) bool {
    // Resolve project root (should always exist).
    absRoot, err := filepath.EvalSymlinks(projectRoot)
    if err != nil {
        absRoot = filepath.Clean(projectRoot)
    }

    // Resolve file path. EvalSymlinks fails on non-existent files
    // (common for Write tool creating new files), so fall back to
    // cleaning the absolute path to resolve ".." segments.
    absPath, err := filepath.EvalSymlinks(filePath)
    if err != nil {
        absPath = filepath.Clean(filepath.Join(absRoot, filePath))
        // If the path was already absolute, Clean preserves that.
        if filepath.IsAbs(filePath) {
            absPath = filepath.Clean(filePath)
        }
    }

    // Check path is within project root.
    return !strings.HasPrefix(absPath, absRoot+"/") && absPath != absRoot
}
```

**Security note**: `filepath.Abs` does NOT resolve symlinks — an attacker could create a symlink inside the project root pointing to `/etc/passwd`. `filepath.EvalSymlinks` resolves the full chain. The fallback for non-existent files uses `filepath.Clean` which resolves `..` segments but can't resolve symlinks (acceptable since the file doesn't exist yet and can't be a symlink). TOCTOU race between check and write is a known limitation but acceptable given the threat model (Claude Code sessions, not adversarial users).

**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: Update `WriteHooksConfig` to add additional PreToolUse matchers for `Write` and `Edit`.

```go
"PreToolUse": {
    {
        Matcher: "Bash",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars, Timeout: hookTimeoutDefault,
        }},
    },
    {
        Matcher: "Write",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars, Timeout: hookTimeoutDefault,
        }},
    },
    {
        Matcher: "Edit",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars, Timeout: hookTimeoutDefault,
        }},
    },
},
```

**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: Extend `handleSwarmHookPreToolUse` (line 83) to handle Write/Edit tools.

```go
// After the Bash check (line 96-114), add:
if payload.ToolName == "Write" || payload.ToolName == "Edit" {
    filePath, _ := payload.ToolInput["file_path"].(string)
    if filePath != "" && swarmorch.IsPathOutOfBounds(filePath, payload.CWD) {
        // Log and deny
        s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
            "event":     "tool_denied",
            "tool":      payload.ToolName,
            "file_path": filePath,
            "reason":    "path outside project root",
        })
        return c.JSON(http.StatusOK, preToolUseResponse{
            HookSpecificOutput: &preToolUseOutput{
                HookEventName:            "PreToolUse",
                PermissionDecision:       "deny",
                PermissionDecisionReason: "Write outside project root: " + filePath,
            },
        })
    }
}
```

**Note**: `payload.CWD` is set by Claude Code and represents the session's working directory (the project root). This is already present in `hookPayload` at `swarm_hooks.go:19`.

#### B.3 Secret Redaction in Logs and Events

**File**: `harness/internal/swarmorch/sanitize.go` (NEW)
**Changes**: Create a sanitizer for redacting secrets from log output.

```go
package swarmorch

import "regexp"

var secretPatterns = []*regexp.Regexp{
    regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]+`),
    regexp.MustCompile(`github_pat_[a-zA-Z0-9_]+`),
    regexp.MustCompile(`ghp_[a-zA-Z0-9]+`),
    regexp.MustCompile(`Bearer\s+[a-zA-Z0-9._-]{20,}`),
    regexp.MustCompile(`(ANTHROPIC_API_KEY|LINEAR_API_KEY|GRAPHITE_TOKEN|DISCORD_BOT_TOKEN|CM_HOOK_SECRET)=[^\s]+`),
}

// SanitizeSecrets replaces known secret patterns with [REDACTED].
func SanitizeSecrets(s string) string {
    for _, pat := range secretPatterns {
        s = pat.ReplaceAllString(s, "[REDACTED]")
    }
    return s
}
```

**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: Apply `SanitizeSecrets` to JSONL log entries in `handleSwarmHookPostToolUse` (line 133-138). Sanitize the `tool_input` before writing to JSONL and publishing to EventBus.

```go
// Before writing JSONL (line 133), sanitize tool input:
sanitizedInput := sanitizeToolInput(payload.ToolInput)

// Helper:
func sanitizeToolInput(input map[string]any) map[string]any {
    sanitized := make(map[string]any, len(input))
    for k, v := range input {
        if s, ok := v.(string); ok {
            sanitized[k] = swarmorch.SanitizeSecrets(s)
        } else {
            sanitized[k] = v
        }
    }
    return sanitized
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New test: `TestMatchesDenyPattern_ExpandedPatterns` — verify all new patterns match
- [ ] New test: `TestIsPathOutOfBounds` — verify path boundary logic including: `../../etc/passwd` traversal, absolute paths outside root (`/etc/passwd`), paths within root, symlink resolution (create temp symlink pointing outside root), and non-existent file paths (Write tool creating new files)
- [ ] New test: `TestSanitizeSecrets` — verify all secret patterns are redacted
- [ ] Existing tests pass: `go test ./harness/internal/swarmorch/...`

#### Manual Verification:
- [ ] Start a swarm session and verify `git push` is blocked
- [ ] Verify Write to `/etc/passwd` is blocked
- [ ] Check JSONL logs don't contain API keys after sessions that read env vars

---

## Phase C: Health Monitoring Enhancements

### Overview
Add per-phase stall thresholds, activity-based staleness tracking from PostToolUse hooks, and ZFC health reconciliation in the dashboard SSE handler.

### Changes Required:

#### C.1 Per-Phase Stall Thresholds

**File**: `harness/internal/swarmorch/health.go`
**Changes**: Replace the flat `stallCheckMinutes = 45` constant (line 18) with a per-phase map.

```go
// phaseStallThresholds defines the maximum expected duration per phase.
// Phases not listed use the default of 45 minutes.
var phaseStallThresholds = map[string]time.Duration{
    "research":       75 * time.Minute,
    "code_plan":      45 * time.Minute,
    "plan_review":    20 * time.Minute,
    "implement":      75 * time.Minute,
    "verify":         30 * time.Minute,
    "pr":             20 * time.Minute,
    "project_plan":   60 * time.Minute,
    "project_review": 20 * time.Minute,
    "project_verify": 30 * time.Minute,
}

const defaultStallThreshold = 45 * time.Minute

func stallThresholdForPhase(phase string) time.Duration {
    if d, ok := phaseStallThresholds[phase]; ok {
        return d
    }
    return defaultStallThreshold
}
```

Update `queryActiveWorkflows` (line 183) to use `stallThresholdForPhase(wf.Phase)` instead of `stallCheckMinutes * time.Minute`.

#### C.2 Activity-Based Staleness Signal

**File**: `harness/internal/db/migrations/012_activity_tracking.sql` (NEW)
**Changes**: Add `last_tool_activity` column to `swarm_sessions`.

```sql
ALTER TABLE swarm_sessions ADD COLUMN last_tool_activity TEXT;
```

**File**: `harness/internal/db/queries/swarm.sql`
**Changes**: Add a new query to update tool activity timestamp.

```sql
-- name: UpdateSwarmSessionToolActivity :exec
UPDATE swarm_sessions SET last_tool_activity = datetime('now') WHERE id = ?;
```

Also update `GetSwarmSession` and `ListSwarmSessionsByWorkflow` to include `last_tool_activity` in SELECT.

**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: In `handleSwarmHookPostToolUse` (line 120), call the new query after logging.

```go
// After writing JSONL and publishing (line 148), update activity:
_ = s.DB.UpdateSwarmSessionToolActivity(c.Request().Context(), sessionID)
```

**File**: `harness/internal/swarmorch/health.go`
**Changes**: Update `queryActiveWorkflows` (a package-level function at line 144 with inline raw SQL — not sqlc-generated) to join with `swarm_sessions` and use `MAX(wf.updated_at, COALESCE(s.last_tool_activity, wf.updated_at))` for stall calculations. Also update the `ActiveWorkflowInfo` struct to include a `LastActivity` field.

```sql
SELECT w.id, w.ticket_id, w.phase, w.attempt, w.status, w.created_at, w.updated_at,
       MAX(w.updated_at, COALESCE(s.last_tool_activity, w.updated_at)) AS last_activity
FROM swarm_workflows w
LEFT JOIN swarm_sessions s ON s.workflow_id = w.id AND s.completed_at IS NULL
WHERE w.status IN (?, ?)
ORDER BY w.created_at DESC
```

Use `last_activity` instead of `w.updated_at` for stall comparison.

#### C.3 ZFC Health Reconciliation in Dashboard SSE

**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Add `ReconcileStaleSession` method with internal debouncing. This runs on the Manager (not per-SSE-connection) to avoid duplicate tmux checks when multiple dashboard clients are connected.

```go
// ReconcileStaleSession checks running sessions for tmux death and updates state.
// Internally debounced — safe to call from multiple SSE handlers concurrently.
// Uses sync.Once pattern reset on a timer to ensure tmux checks run at most
// once per 30s regardless of how many dashboard clients are connected.
func (m *Manager) ReconcileStaleSession(ctx context.Context) {
    // Debounce: skip if we reconciled recently (within 30s).
    m.reconcileMu.Lock()
    if time.Since(m.lastReconcile) < 30*time.Second {
        m.reconcileMu.Unlock()
        return
    }
    m.lastReconcile = time.Now()
    m.reconcileMu.Unlock()

    sessions, err := m.db.ListActiveSwarmSessions(ctx)
    if err != nil {
        return
    }
    for _, s := range sessions {
        // isTmuxSessionAlive is a package-level function (manager.go:1061-1065),
        // not a method on Manager.
        if !isTmuxSessionAlive(s.SessionName) {
            // Tmux is dead but DB says running — trigger completion.
            // handleSessionComplete has a double-fire guard at line 494
            // (returns early if already completed), so concurrent calls
            // from ZFC + normal hook completion are safe.
            m.Logger.Warn("ZFC reconciliation: tmux dead for running session",
                "session_id", s.ID, "session_name", s.SessionName)
            go m.handleSessionComplete(ctx, s.ID)
        }
    }
}
```

Add `reconcileMu sync.Mutex` and `lastReconcile time.Time` fields to the `Manager` struct.

**File**: `harness/internal/server/swarm_dashboard.go`
**Changes**: During the heartbeat tick (line 164-167), call the debounced reconciliation.

```go
case <-heartbeat.C:
    // ZFC reconciliation: check tmux liveness for running sessions.
    // ReconcileStaleSession is internally debounced — safe to call from
    // every SSE handler without multiplying tmux checks per client.
    if s.SwarmManager != nil {
        s.SwarmManager.ReconcileStaleSession(ctx)
    }
    if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
        return nil
    }
```

**File**: `harness/internal/db/queries/swarm.sql`
**Changes**: Add query for active sessions.

```sql
-- name: ListActiveSwarmSessions :many
SELECT id, workflow_id, session_name, skill, phase
FROM swarm_sessions WHERE completed_at IS NULL;
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New migration applies cleanly
- [ ] sqlc generates cleanly after query changes
- [ ] New test: `TestStallThresholdForPhase` — verify correct thresholds per phase
- [ ] New test: `TestActivityBasedStaleness` — verify tool activity updates prevent false stalls
- [ ] Existing tests pass: `go test ./harness/internal/swarmorch/...`

#### Manual Verification:
- [ ] Research sessions (long-running) don't trigger false stall alerts
- [ ] PR sessions that stall are detected within 20 minutes
- [ ] Dashboard shows correct state even if tmux dies without hooks firing

---

## Phase D: Learning System Enhancements

### Overview
Add domain-based learning classification with tiered decay rates. Foundational learnings (conventions) stop decaying. Tactical learnings use the current rate. Observational learnings decay faster.

### Changes Required:

#### D.1 Domain-Based Learning Classification

**File**: `harness/internal/db/migrations/013_learning_classification.sql` (NEW)

```sql
ALTER TABLE swarm_learnings ADD COLUMN classification TEXT DEFAULT 'tactical'
    CHECK(classification IN ('foundational', 'tactical', 'observational'));
```

**File**: `harness/internal/swarm/enums.go`
**Changes**: Add classification enum.

```go
// LearningClassification is a typed enum for learning decay tiers.
type LearningClassification string

const (
    ClassificationFoundational LearningClassification = "foundational"
    ClassificationTactical     LearningClassification = "tactical"
    ClassificationObservational LearningClassification = "observational"
)
```

**File**: `harness/internal/swarmorch/learnings.go`
**Changes**: Add auto-classification based on category.

```go
// classifyLearning determines the decay tier from the learning category.
func classifyLearning(category swarm.LearningCategory) swarm.LearningClassification {
    switch category {
    case swarm.LearningConvention:
        return swarm.ClassificationFoundational
    case swarm.LearningPattern, swarm.LearningPostMortem:
        return swarm.ClassificationTactical
    case swarm.LearningCodeBug, swarm.LearningPlanIssue:
        return swarm.ClassificationObservational
    default:
        return swarm.ClassificationTactical
    }
}
```

Update `createLearning` to call `classifyLearning` and pass the result to the DB.

**File**: `harness/internal/db/queries/swarm_learnings.sql`
**Changes**: Update `CreateSwarmLearning` to accept `classification` parameter. Update `DecaySwarmLearningRelevance` to use tiered rates:

```sql
-- name: DecaySwarmLearningRelevance :exec
UPDATE swarm_learnings
SET relevance_score = CASE
    WHEN classification = 'foundational' THEN relevance_score  -- no decay
    WHEN classification = 'observational' THEN relevance_score * 0.90  -- fast decay
    ELSE relevance_score * 0.95  -- tactical (default)
END,
updated_at = datetime('now')
WHERE archived_at IS NULL AND relevance_score > 0.1;
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New migration applies cleanly
- [ ] sqlc generates cleanly
- [ ] New test: `TestClassifyLearning` — verify category→classification mapping
- [ ] New test: `TestDecayRespectsClassification` — verify foundational learnings don't decay
- [ ] Existing tests pass

#### Manual Verification:
- [ ] Convention learnings retain their relevance score over time
- [ ] Code bug learnings decay faster than pattern learnings

---

## Phase E: Event Observability

### Overview
Filter tool arg payloads to reduce SSE event sizes, add missing `phase` and `file` fields, and implement incremental SSE with Last-Event-ID reconnection support.

### Changes Required:

#### E.1 Tool Arg Filtering for Events

**File**: `harness/internal/swarmorch/filter.go` (NEW)
**Changes**: Create a tool arg filter that keeps only identifying fields per tool type.

```go
package swarmorch

// FilterToolArgs reduces tool input to identifying fields only,
// dropping large content like file bodies and code diffs.
func FilterToolArgs(toolName string, input map[string]any) map[string]any {
    switch toolName {
    case "Bash":
        return filterKeys(input, "command", "description")
    case "Read":
        return filterKeys(input, "file_path", "offset", "limit")
    case "Write":
        return filterKeys(input, "file_path")
    case "Edit":
        return filterKeys(input, "file_path")
    case "Grep":
        return filterKeys(input, "pattern", "path", "glob", "type")
    case "Glob":
        return filterKeys(input, "pattern", "path")
    case "Agent":
        return filterKeys(input, "description", "subagent_type")
    default:
        return filterKeys(input, "file_path", "pattern", "path")
    }
}

func filterKeys(input map[string]any, keys ...string) map[string]any {
    result := make(map[string]any, len(keys))
    for _, k := range keys {
        if v, ok := input[k]; ok {
            // Truncate long strings (e.g., Bash command)
            if s, ok := v.(string); ok && len(s) > 120 {
                result[k] = s[:120] + "..."
            } else {
                result[k] = v
            }
        }
    }
    return result
}
```

**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: In `handleSwarmHookPostToolUse` (line 140-148), apply filtering and add `phase` and `file` fields.

```go
// Extract phase from session (look up workflow via DB or pass via header).
// Simplest approach: add X-Swarm-Phase header to hooks config.
phase := c.Request().Header.Get("X-Swarm-Phase")

// Extract file_path from filtered input.
filtered := swarmorch.FilterToolArgs(payload.ToolName, payload.ToolInput)
filePath, _ := filtered["file_path"].(string)

if s.EventBus != nil {
    s.EventBus.PublishGlobal(map[string]any{
        "event":      events.EventSwarmToolUse,
        "session_id": sessionID,
        "ticket_id":  ticketID,
        "phase":      phase,
        "tool":       payload.ToolName,
        "file":       filePath,
        "input":      filtered,  // filtered, not raw
    })
}
```

**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: Add `X-Swarm-Phase` header to `WriteHooksConfig` (line 71-76).

```go
authHeaders := map[string]string{
    "X-Hook-Secret":   "$" + swarm.EnvKey("HookSecret"),
    "X-Swarm-Session": sessionID,
    "X-Swarm-Ticket":  ticketID,
    "X-Swarm-Phase":   phase,  // NEW: pass phase for event filtering
    "Content-Type":    "application/json",
}
```

Update `WriteHooksConfig` signature from `(sessionID, ticketID, harnessURL, hookSecret string)` to `(sessionID, ticketID, harnessURL, hookSecret, phase string)`. The phase is known at session spawn time and doesn't change during the session, so a static header is correct.

**Callsite update**: `spawnSession` (manager.go:318) calls `WriteHooksConfig` — update it to pass `string(wf.Phase)` as the new `phase` argument. The `wf` variable is already in scope (the function parameter).

#### E.2 Incremental SSE with Last-Event-ID

**File**: `harness/internal/events/bus.go`
**Changes**: Add sequence numbers and a ring buffer to EventBus.

```go
type numberedEvent struct {
    ID    int64
    Event any
}

// Add to EventBus struct:
type EventBus struct {
    // ... existing fields ...
    seq       int64           // atomic counter
    ringBuf   []numberedEvent // fixed-size ring buffer
    ringSize  int
    ringMu    sync.RWMutex
}

func NewEventBus() *EventBus {
    return &EventBus{
        // ... existing init ...
        ringBuf:  make([]numberedEvent, 0, 1000),
        ringSize: 1000,
    }
}
```

Update `PublishGlobal` to assign sequence numbers and store in ring buffer.

**File**: `harness/internal/server/swarm_dashboard.go`
**Changes**: Read `Last-Event-ID` header on SSE connection and replay missed events.

```go
func (s *Server) handleSwarmDashboardSSE(c echo.Context) error {
    r := c.Request()
    sse := datastar.NewSSE(c.Response().Writer, r)

    // Replay missed events on reconnect.
    if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
        if id, err := strconv.ParseInt(lastID, 10, 64); err == nil {
            for _, evt := range s.EventBus.EventsSince(id) {
                // Process each replayed event...
            }
        }
    }

    // ... existing subscribe + loop ...
}
```

Add `id:` field to SSE events sent to the client.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New test: `TestFilterToolArgs` — verify each tool type keeps only expected keys
- [ ] New test: `TestEventBusSequencing` — verify sequence numbers increment
- [ ] New test: `TestEventBusReplay` — verify EventsSince returns correct events
- [ ] Existing tests pass

#### Manual Verification:
- [ ] Dashboard tool activity shows phase and file info
- [ ] SSE reconnection replays missed events (simulate by disconnecting network briefly)
- [ ] Event payloads in browser devtools are <500 bytes (not 20KB+)

---

## Phase F: Doctor Health Check API

### Overview
Add a comprehensive `/api/swarm/doctor` endpoint that verifies DB, tmux, Linear auth, Graphite, Discord, orphaned sessions, and disk space.

### Changes Required:

**File**: `harness/internal/swarmorch/doctor.go` (NEW)

```go
package swarmorch

type DoctorCheck struct {
    Name    string `json:"name"`
    Status  string `json:"status"` // "ok", "warn", "fail"
    Detail  string `json:"detail,omitempty"`
}

type DoctorReport struct {
    Overall string        `json:"overall"`
    Checks  []DoctorCheck `json:"checks"`
}

func (m *Manager) RunDoctor(ctx context.Context) *DoctorReport {
    var checks []DoctorCheck

    // 1. DB connectivity
    checks = append(checks, m.checkDB(ctx))
    // 2. tmux availability
    checks = append(checks, m.checkTmux())
    // 3. Linear API key
    checks = append(checks, m.checkLinear(ctx))
    // 4. Graphite token
    checks = append(checks, m.checkGraphite())
    // 5. Discord channel
    checks = append(checks, m.checkDiscord())
    // 6. Orphaned tmux sessions
    checks = append(checks, m.checkOrphanedSessions(ctx))
    // 7. Disk space for logs
    checks = append(checks, m.checkDiskSpace())
    // 8. Prompt template integrity
    checks = append(checks, m.checkPromptTemplates())

    overall := "healthy"
    for _, c := range checks {
        if c.Status == "fail" { overall = "unhealthy"; break }
        if c.Status == "warn" { overall = "degraded" }
    }

    return &DoctorReport{Overall: overall, Checks: checks}
}
```

Each check function is a simple probe with a **5-second timeout** (`context.WithTimeout`) to prevent the endpoint from hanging if a network check stalls:

```go
func (m *Manager) runCheck(ctx context.Context, name string, fn func(context.Context) DoctorCheck) DoctorCheck {
    checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    return fn(checkCtx)
}
```

Individual checks:
- `checkDB`: `SELECT 1` query (uses check ctx)
- `checkTmux`: `exec.CommandContext(ctx, "tmux", "-V")`
- `checkLinear`: `m.linearClient != nil && m.linearClient.Ping(ctx)` (network call — timeout critical)
- `checkGraphite`: `os.Getenv("GRAPHITE_TOKEN") != ""` (local, no timeout needed)
- `checkDiscord`: `os.Getenv("DISCORD_SWARM_CHANNEL_ID") != ""` (local, no timeout needed)
- `checkOrphanedSessions`: Compare `ListActiveSwarmSessions` vs `tmux list-sessions` (uses check ctx)
- `checkDiskSpace`: `syscall.Statfs` on logs directory (local)
- `checkPromptTemplates`: Try `RenderPrompt` for each phase with dummy context (local, CPU-bound)

**File**: `harness/internal/server/swarm_api.go`
**Changes**: Add route `GET /api/swarm/doctor` calling `m.RunDoctor()`.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New test: `TestDoctorAllChecksPass` — verify clean doctor report
- [ ] New test: `TestDoctorDetectsOrphaned` — verify orphaned session detection
- [ ] API returns 200 with JSON report

#### Manual Verification:
- [ ] `/api/swarm/doctor` returns meaningful results on VPS
- [ ] Removing env vars (e.g., GRAPHITE_TOKEN) causes corresponding check to fail

---

## Phase G: Workflow Overrides

### Overview
Allow per-workflow configuration overrides to skip phases or gates for simple tickets.

### Changes Required:

**File**: `harness/internal/db/migrations/014_workflow_overrides.sql` (NEW)

```sql
ALTER TABLE swarm_workflows ADD COLUMN overrides TEXT;  -- JSON
```

**File**: `harness/internal/swarm/statemachine.go`
**Changes**: Add `WorkflowOverrides` struct and integrate into `DetermineNextPhase`.

```go
type WorkflowOverrides struct {
    SkipPhases []Phase `json:"skip_phases,omitempty"`
    SkipGates  []Phase `json:"skip_gates,omitempty"`
}

// ShouldSkipPhase returns true if the phase is in the skip list.
func (o *WorkflowOverrides) ShouldSkipPhase(phase Phase) bool {
    if o == nil { return false }
    for _, p := range o.SkipPhases {
        if p == phase { return true }
    }
    return false
}
```

Update `DetermineNextPhase` to check `ShouldSkipPhase` and advance past skipped phases. Add `overrides *WorkflowOverrides` parameter to the function signature (or embed in `SwarmConfig` — embedding is cleaner since `SwarmConfig` already passes through).

**Critical: handle the entry point case.** When `lastResult` is empty (first call for a new workflow), `DetermineNextPhase` unconditionally returns `PhaseResearch` (line 53-54). If `PhaseResearch` is in `skip_phases`, the caller must loop. Implement this as a wrapper rather than modifying the core function:

```go
// DetermineNextPhaseWithOverrides calls DetermineNextPhase in a loop,
// advancing past any phases in the skip list by treating them as
// instant successes. Prevents infinite loops with a max-iterations guard.
func DetermineNextPhaseWithOverrides(
    workflowType WorkflowType,
    currentPhase Phase,
    attempt int,
    lastResult SessionResult,
    config SwarmConfig,
    overrides *WorkflowOverrides,
) Transition {
    t := DetermineNextPhase(workflowType, currentPhase, attempt, lastResult, config)
    for i := 0; i < 10 && overrides.ShouldSkipPhase(t.NextPhase); i++ {
        // Treat skipped phase as instant success and advance.
        t = DetermineNextPhase(workflowType, t.NextPhase, 0, ResultSuccess, config)
    }
    // Also check gate skipping for gated transitions.
    if overrides != nil && overrides.ShouldSkipGate(t.NextPhase) {
        // Advance past the gate phase.
        t = DetermineNextPhase(workflowType, t.NextPhase, 0, ResultSuccess, config)
    }
    return t
}

// ShouldSkipGate returns true if the gate phase is in the skip list.
func (o *WorkflowOverrides) ShouldSkipGate(phase Phase) bool {
    if o == nil { return false }
    for _, p := range o.SkipGates {
        if p == phase { return true }
    }
    return false
}
```

This keeps the core `DetermineNextPhase` unchanged (existing tests pass) and adds override logic as a layer on top. The `advanceWorkflow` callsite switches to calling `DetermineNextPhaseWithOverrides`.

**File**: `harness/internal/server/swarm_api.go`
**Changes**: Accept optional `overrides` field in the start workflow API request body.

```go
type startWorkflowRequest struct {
    TicketID string                   `json:"ticket_id"`
    Type     string                   `json:"type,omitempty"`
    Overrides *swarm.WorkflowOverrides `json:"overrides,omitempty"`
}
```

**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Update `StartWorkflow` to accept and persist overrides. Update `advanceWorkflow` to load overrides and pass to `DetermineNextPhase`.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New migration applies cleanly
- [ ] New test: `TestDetermineNextPhaseWithOverridesSkipsPhase` — verify phase skipping via the wrapper
- [ ] New test: `TestDetermineNextPhaseWithOverridesSkipsEntryPoint` — verify skipping `PhaseResearch` (the entry point) advances to `code_plan` without infinite loop
- [ ] New test: `TestDetermineNextPhaseWithOverridesSkipsGate` — verify gate skipping
- [ ] New test: `TestDetermineNextPhaseWithOverridesMaxIterations` — verify the max-iterations guard prevents infinite loops if all phases are skipped
- [ ] New test: `TestWorkflowOverridesFromAPI` — verify overrides persist and apply
- [ ] Existing state machine tests pass (core `DetermineNextPhase` is unchanged)

#### Manual Verification:
- [ ] Start a workflow with `{"overrides": {"skip_phases": ["research"]}}` — verify it goes directly to `code_plan`
- [ ] Start a workflow with `{"overrides": {"skip_gates": ["plan_review"]}}` — verify it skips the gate

---

## Phase H: Auto-Insight Extraction from Transcripts

### Overview
Extend the transcript parser to extract tool statistics and hot files from completed sessions, generating structured learnings automatically. Depends on Phase A (transcript wiring).

### Changes Required:

**File**: `harness/internal/swarm/transcript/insights.go` (NEW)

**Important: data source distinction.** There are two different JSONL files per session:
1. **Claude Code transcript** (`~/.claude/projects/{projectKey}/{uuid}.jsonl`) — contains `type: "assistant"` entries with token usage. This is what `ParseFile` reads (Phase A). It does NOT contain tool invocations.
2. **Swarm hook JSONL** — written by `WriteJSONLEvent` in the PostToolUse hook handler. Contains tool names, inputs, and timing. This is the correct source for `ExtractInsights`.

The swarm hook JSONL path is managed by the `jsonlWriter` created in `spawnSession` (lines 336-352). We need to pass this path through to `handleSessionComplete` or store it on the session record.

**File**: `harness/internal/swarm/transcript/insights.go` (NEW)

```go
package transcript

// ToolStat tracks usage of a single tool type in a session.
type ToolStat struct {
    Tool       string `json:"tool"`
    Count      int    `json:"count"`
    ErrorCount int    `json:"error_count"`
}

// HotFile tracks a file with many edits in a session.
type HotFile struct {
    Path      string `json:"path"`
    EditCount int    `json:"edit_count"`
}

// SessionInsights holds auto-extracted insights from a transcript.
type SessionInsights struct {
    ToolStats  []ToolStat `json:"tool_stats"`
    HotFiles   []HotFile  `json:"hot_files"`
    TotalTools int        `json:"total_tools"`
    ErrorRate  float64    `json:"error_rate"`
}

// ExtractInsights scans a swarm hook JSONL file (NOT the Claude Code
// transcript) for tool usage patterns. The hook JSONL contains entries
// with "tool", "input", and optionally "error" fields written by
// handleSwarmHookPostToolUse.
func ExtractInsights(hookJSONLPath string) (*SessionInsights, error) {
    // Parse hook JSONL (not Claude Code transcript JSONL).
    // Each line is a JSON object with: tool, input (map), session_id, etc.
    // Count tool invocations per tool type.
    // Track file_path from Write/Edit/Read inputs for hot files.
    // Count entries where "error" field is present for error rate.
    // Return sorted by count descending.
}
```

**File**: `harness/internal/swarmorch/manager.go`
**Changes**: In `handleSessionComplete`, after transcript parsing (added in Phase A), call `ExtractInsights` using the **hook JSONL path** (not the Claude Code transcript path). The hook JSONL writer path needs to be recoverable — either from the `jsonlWriters` map on Manager or stored as a field on the session record.

```go
// After token extraction (Phase A), extract insights from hook JSONL:
if hookJSONLPath := m.getHookJSONLPath(sessionID); hookJSONLPath != "" {
    insights, insightErr := transcript.ExtractInsights(hookJSONLPath)
    if insightErr == nil && insights.TotalTools > 0 {
        hotFiles := formatHotFiles(insights.HotFiles) // helper to join paths
        summary := fmt.Sprintf("Session %s: %d tools, %.0f%% error rate, hot files: %s",
            sessionID, insights.TotalTools, insights.ErrorRate*100, hotFiles)
        m.createLearning(ctx, workflowID, sessionID, ticketID,
            swarm.LearningPattern, phase, swarm.SeverityInfo,
            "Session insights: "+sessionID[:8], summary)
    }
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New test: `TestExtractInsights` — verify tool stat and hot file extraction from hook JSONL format (not Claude Code transcript format)
- [ ] New test: `TestExtractInsightsEmptyFile` — verify graceful handling of empty/missing hook JSONL
- [ ] New test: `TestInsightLearningCreation` — verify learnings are generated from insights
- [ ] Existing tests pass

#### Manual Verification:
- [ ] After workflow completion, verify insight learnings appear in `/api/swarm/learnings`
- [ ] Hot files (3+ edits) are correctly identified
- [ ] Error rate calculation matches manual hook JSONL inspection
- [ ] Verify `ExtractInsights` is called with the hook JSONL path, not the Claude Code transcript path

---

## Testing Strategy

### Unit Tests:
- Phase A: Token extraction wiring, prompt version upsert, learning tags
- Phase B: Expanded deny patterns, path boundary checks, secret redaction
- Phase C: Per-phase stall thresholds, activity staleness, ZFC reconciliation
- Phase D: Learning classification mapping, tiered decay
- Phase E: Tool arg filtering, EventBus sequencing, SSE replay
- Phase F: Doctor checks (DB, tmux, orphaned sessions)
- Phase G: Phase skipping in state machine, overrides persistence
- Phase H: Hook JSONL insight extraction (distinct from Claude Code transcript parsing in Phase A)

### Key Edge Cases:
- Transcript not found (session crashed before writing anything) — `DiscoverTranscript` returns error, token extraction silently skipped
- Hook JSONL not found (session crashed before any tool use) — `ExtractInsights` returns error, insights silently skipped
- Empty tool input maps in filtering — `FilterToolArgs` returns empty map, not nil
- Path traversal attempts (`../../etc/passwd`) in boundary checks — `filepath.Clean` resolves `..` segments
- Symlink-based path traversal — `filepath.EvalSymlinks` resolves symlink chains; fallback to `filepath.Clean` for non-existent files (Write tool)
- Race conditions in ZFC reconciliation (session completes between check and action) — `handleSessionComplete` has a double-fire guard at line 494 (returns early if already completed)
- ZFC deduplication across multiple SSE clients — `ReconcileStaleSession` is debounced with `sync.Mutex` + 30s cooldown on Manager
- Ring buffer wraparound in incremental SSE — use modular index into fixed-size slice
- Phase skip infinite loop — `DetermineNextPhaseWithOverrides` has max-iterations guard (10) to prevent looping if all phases are skipped
- `UpsertSwarmPromptVersion` conflict — the `ON CONFLICT DO UPDATE SET id = id` clause returns the existing ID on hash collision (no-op update triggers `RETURNING`)

### Manual Testing Steps:
1. Run a full `code` workflow end-to-end and verify all new data is populated
2. Attempt destructive commands (git push, rm -rf) and verify they're blocked
3. Monitor dashboard during long research session — verify no false stall alerts
4. Kill tmux session manually and verify dashboard updates within 30s (ZFC)
5. Call `/api/swarm/doctor` and verify all checks report correctly

## Performance Considerations

- **Transcript parsing** (Phase A): Runs once per session completion. Typical transcripts are 1-5MB. ParseFile uses buffered scanning with 1MiB initial / 10MiB max — no concern for VPS.
- **Tool arg filtering** (Phase E): Runs on every PostToolUse hook (~100-1000x per session). The filter is a simple map lookup + string truncation — negligible overhead.
- **Ring buffer** (Phase E): Fixed 1000-event capacity. Memory cost: ~100KB. Lock contention: minimal (RWMutex, writes are rare relative to reads).
- **ZFC reconciliation** (Phase C): Debounced on Manager — runs at most once per 30s regardless of how many dashboard SSE clients are connected. One `tmux has-session` exec per active session. At 4 max sessions, this is 4 shell execs per 30s — negligible.
- **Secret redaction** (Phase B): Regex replacement on every JSONL write. 5 patterns × typical 200-byte input = negligible.
- **Tiered decay** (Phase D): SQL CASE expression in existing hourly decay query. No additional queries needed.

## Migration Notes

- **Phase A**: No new migration needed — columns exist from 007. Only new SQL queries required.
- **Phase C**: Migration 012 adds one nullable column (`last_tool_activity`) to `swarm_sessions`. Compatible with existing data.
- **Phase D**: Migration 013 adds one column (`classification`) with default value to `swarm_learnings`. Compatible with existing data.
- **Phase G**: Migration 014 adds one nullable JSON column (`overrides`) to `swarm_workflows`. Compatible with existing data.
- **Test schema** (`manager_test.go:21-144`): Must be updated to include new columns from migrations 012-014 and the new SQL queries.
- All migrations are forward-only additive ALTER TABLE — no destructive changes, no down migrations needed.
- SQLite does not support `ALTER TABLE ADD COLUMN IF NOT EXISTS` — the migration runner must track which migrations have been applied to avoid duplicate runs.

## References

- Research: `thoughts/CoreyCole/research/2026-03-01_23-00-00_overstory-additional-improvements.md`
- Architecture comparison: `thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md`
- Existing plan: `thoughts/CoreyCole/plans/2026-03-01_21-37-45_overstory-swarm-improvements.md`
- Plan review: `thoughts/CoreyCole/reviews/2026-03-01_21-49-06_overstory-swarm-improvements_review.md`
- Transcript package: `harness/internal/swarm/transcript/` (parse.go, discover.go, pricing.go)
- Prompt package: `harness/internal/swarm/prompt/` (render.go, context.go)
- Manager: `harness/internal/swarmorch/manager.go`
- Hooks: `harness/internal/swarmorch/hooks.go` + `harness/internal/server/swarm_hooks.go`
- Health: `harness/internal/swarmorch/health.go`
- Learnings: `harness/internal/swarmorch/learnings.go`
- Dashboard SSE: `harness/internal/server/swarm_dashboard.go`
- EventBus: `harness/internal/events/bus.go`
- Swarm SQL: `harness/internal/db/queries/swarm.sql`, `swarm_learnings.sql`, `swarm_prompts.sql`
- Migration 007: `harness/internal/db/migrations/007_prompt_versions_and_tokens.sql`
