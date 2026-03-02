# Overstory-Inspired Swarm Improvements — Implementation Plan

## Overview

Adopt the top patterns from the Overstory architecture comparison to improve the Creative Mode swarm orchestrator. These changes make swarm sessions more resilient (checkpoints with observable state extraction), more secure (guards), more observable (CLI tools with real cost pricing), smarter about past experience (structured learnings with auto-insight extraction), better at self-healing (progressive health with ZFC reconciliation), and more configurable (per-workflow overrides + incremental SSE).

**Research**: `thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md`
**Review**: `thoughts/CoreyCole/reviews/2026-03-01_21-49-06_overstory-swarm-improvements_review.md`

## Phase Dependencies

Phases can be implemented in parallel except where noted:

```
Phase 1 (Security Guards)      ──┐
Phase 2 (Session Checkpoints)  ──┤── all independent
Phase 3 (Structured Learnings) ──┤
Phase 5 (CLI Observability)    ──┘
Phase 4 (Progressive Health)   ──── independent
Phase 6 (Operational Polish)   ──── depends on Phase 3 for auto-insight wiring
```

Migration numbering: Phase 2 uses `009_session_checkpoints.sql`, Phase 3 uses `010_structured_learnings.sql`. If Phase 6 adds schema changes, use `011_`.

## Current State Analysis

The swarm orchestrator (`internal/swarmorch/`) runs Linear tickets through multi-phase workflows via Claude Code sessions in tmux. Key gaps identified by the Overstory comparison:

1. **Security**: Only 4 bash deny patterns (`cargo build/clippy/check`, `go build`, `templ generate`, `just generate`). No blocking of interactive tools, destructive git commands, or path boundaries.
2. **Session continuity**: `context_limit` resumes same phase but with zero context about what the crashed session accomplished. New session starts from scratch.
3. **Learnings**: Flat category/severity model with blunt 0.95x decay. No domain separation, no classification tiers, no outcome tracking.
4. **Health monitoring**: Binary stall detection (running vs dead, 45min threshold). No graduated response, no tmux nudge, no AI triage.
5. **CLI observability**: Zero `just swarm-*` recipes. All debugging requires the web dashboard or raw DB queries.

### Key Discoveries:
- Hook config generated per-session at `hooks.go:61-174` — easy to extend with new matchers
- PreToolUse currently only matches `Bash` tool (`hooks.go:96`) — need new matchers for `AskUserQuestion`, `EnterPlanMode`, `EnterWorktree`
- PreCompact hook already fires and tracks pressure (`swarm_hooks.go:153-189`) — checkpoint save point is already wired
- `swarm_learnings` table has `tags TEXT` column that's unused — new `domain` and `classification` columns will be added via ALTER TABLE instead (tags stays as-is for future use)
- `watchSession()` goroutine at `manager.go:398-475` is the right place for graduated health checks
- Test schema at `manager_test.go:21-144` needs updating when we add columns
- Migration numbering has a collision: two `007_*` files exist. New migrations use explicit numbering: `009_session_checkpoints.sql`, `010_structured_learnings.sql`

## Desired End State

After implementation:
1. Swarm sessions cannot run interactive tools, destructive git commands, or write outside the project
2. Sessions hitting context limits resume with a checkpoint document describing files modified, progress summary, and pending work
3. Learnings are classified by domain and reliability tier, with outcome tracking and tier-based expiry
4. Stalled sessions get graduated responses: warning → file-based nudge (via PostToolUse hook injection) → optional AI triage → kill
5. Developers can run `just swarm trace`, `just swarm costs`, and `just swarm doctor` from the terminal
6. Dashboard SSE verifies tmux liveness before rendering active workflows (ZFC reconciliation)
7. CLI costs command uses real per-model pricing from `transcript.EstimateCost()` (opus/sonnet/haiku rates already exist)
8. Session completion automatically extracts tool usage profiles into structured learnings

### Verification:
- `just check` passes (Go + Rust compile)
- All existing `manager_test.go` tests pass
- New unit tests pass for each phase
- Manual verification via the swarm dashboard shows new features

## What We're NOT Doing

- **Agent hierarchy** (coordinator→lead→specialist) — our sequential model is sufficient
- **SQLite mail** — our HTTP hooks + EventBus handle coordination
- **4-tier merge resolution** — Graphite stacking handles our branch strategy
- **Runtime adapters** — we only use Claude Code
- **AI triage implementation** — we'll add the hook point but not the AI call (too expensive without data showing it's needed)
- **Per-agent identity** — no persistent agent identity across sessions
- **Dashboard UI changes** — phases focus on backend; dashboard SSE gets ZFC reconciliation (Phase 4) and incremental events (Phase 6) but no new UI components

---

## Phase 1: Security Guards

### Overview
Expand the PreToolUse hook system to block interactive tools (`AskUserQuestion`, `EnterPlanMode`, `EnterWorktree`), dangerous bash commands (`git push --force`, `git reset --hard`, `rm -rf`), and path boundary violations. This is the highest-impact, lowest-effort change.

### Changes Required:

#### 1. Expand tool deny patterns
**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: Add bash danger patterns and a tool name deny list

```go
// Add after existing swarmDenyPatterns (line 22-27):

// swarmDangerousBashPatterns blocks destructive commands.
var swarmDangerousBashPatterns = []*regexp.Regexp{
    regexp.MustCompile(`git\s+push\s+.*--force`),
    regexp.MustCompile(`git\s+push\s+-f\b`),
    regexp.MustCompile(`git\s+reset\s+--hard`),
    regexp.MustCompile(`git\s+clean\s+-f`),
    regexp.MustCompile(`rm\s+-rf\s+/`),          // absolute path rm -rf
    regexp.MustCompile(`rm\s+-rf\s+\.\./`),       // parent traversal rm -rf
    regexp.MustCompile(`rm\s+-rf\s+~`),            // home dir rm -rf
}

// swarmBlockedTools are tools that hang headless sessions or bypass orchestration.
var swarmBlockedTools = map[string]string{
    "AskUserQuestion": "Interactive tool blocks headless swarm sessions",
    "EnterPlanMode":   "Interactive tool blocks headless swarm sessions",
    "EnterWorktree":   "Worktree creation bypasses swarm branch management",
}

// MatchesDangerPattern returns true if the bash command is destructive.
func MatchesDangerPattern(command string) bool {
    for _, pat := range swarmDangerousBashPatterns {
        if pat.MatchString(command) {
            return true
        }
    }
    return false
}

// IsBlockedTool returns the reason if the tool should be blocked, empty string if allowed.
func IsBlockedTool(toolName string) string {
    return swarmBlockedTools[toolName]
}
```

#### 2. Add new PreToolUse matchers to hooks config
**File**: `harness/internal/swarmorch/hooks.go`
**Changes**: In `WriteHooksConfig()`, add PreToolUse entries for blocked tools. Claude Code's hook matcher field supports exact tool name matching.

```go
// In the settings.Hooks map, add additional PreToolUse matchers after the existing Bash one:
"PreToolUse": {
    {
        Matcher: "Bash",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars,
            Timeout: hookTimeoutDefault,
        }},
    },
    {
        Matcher: "AskUserQuestion",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars,
            Timeout: hookTimeoutDefault,
        }},
    },
    {
        Matcher: "EnterPlanMode",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars,
            Timeout: hookTimeoutDefault,
        }},
    },
    {
        Matcher: "EnterWorktree",
        Hooks: []hookHandler{{
            Type: "http", URL: baseURL + "/pre-tool-use",
            Headers: authHeaders, AllowedEnvVars: allowedVars,
            Timeout: hookTimeoutDefault,
        }},
    },
},
```

#### 3. Update server hook handler
**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: In `handleSwarmHookPreToolUse`, check tool name against blocked tools before checking bash patterns, and add danger pattern check.

```go
func (s *Server) handleSwarmHookPreToolUse(c echo.Context) error {
    // ... existing decode ...

    // Check blocked tools first (AskUserQuestion, EnterPlanMode, EnterWorktree).
    if reason := swarmorch.IsBlockedTool(payload.ToolName); reason != "" {
        // log + deny
        return c.JSON(http.StatusOK, preToolUseResponse{...deny...})
    }

    // Existing Bash deny + new danger patterns.
    if payload.ToolName == "Bash" {
        command, _ := payload.ToolInput["command"].(string)
        if command != "" {
            if swarmorch.MatchesDenyPattern(command) || swarmorch.MatchesDangerPattern(command) {
                // log + deny
            }
        }
    }
    // ...
}
```

#### 4. Add tests
**File**: `harness/internal/swarmorch/hooks_test.go` (new file)
**Changes**: Unit tests for `MatchesDenyPattern`, `MatchesDangerPattern`, `IsBlockedTool`

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New `hooks_test.go` tests pass: `go test ./harness/internal/swarmorch/ -run TestMatchesDangerPattern`
- [ ] New `hooks_test.go` tests pass: `go test ./harness/internal/swarmorch/ -run TestIsBlockedTool`
- [ ] Existing `manager_test.go` tests pass: `go test ./harness/internal/swarmorch/`

#### Manual Verification:
- [ ] Start a swarm session and verify `AskUserQuestion` is blocked in the JSONL log
- [ ] Verify `git push --force` is blocked in the JSONL log

---

## Phase 2: Session Checkpoints

### Overview
Save session state on PreCompact hook by extracting observable state from the session's working directory and JSONL log — not by relying on the LLM to write a checkpoint file. The harness does the heavy lifting: `git diff --name-only HEAD` for files_modified (guaranteed accurate), last N JSONL log entries for progress context. The session can optionally write a one-line progress summary to a known file, but the checkpoint does not depend on it. When a `context_limit` session resumes, the checkpoint is loaded as additional context in the prompt. This is the biggest reliability improvement — currently a session hitting context limits loses all in-progress work context.

### Changes Required:

#### 1. New DB migration for checkpoints
**File**: `harness/internal/db/migrations/009_session_checkpoints.sql` (new)
**Changes**: Create `swarm_session_checkpoints` table

```sql
CREATE TABLE IF NOT EXISTS swarm_session_checkpoints (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES swarm_sessions(id),
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    phase        TEXT NOT NULL,
    progress     TEXT NOT NULL,       -- markdown summary of what's been done
    files_modified TEXT,              -- JSON array of file paths
    pending_work TEXT,                -- what remains to be done
    compact_count INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_session_checkpoints_session ON swarm_session_checkpoints(session_id);
CREATE INDEX idx_session_checkpoints_workflow ON swarm_session_checkpoints(workflow_id);
```

#### 2. SQL queries for checkpoints
**File**: `harness/internal/db/queries/swarm_checkpoints.sql` (new)

```sql
-- name: CreateSwarmSessionCheckpoint :exec
INSERT INTO swarm_session_checkpoints (id, session_id, workflow_id, phase, progress, files_modified, pending_work, compact_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLatestCheckpointByWorkflow :one
SELECT id, session_id, workflow_id, phase, progress, files_modified, pending_work, compact_count, created_at
FROM swarm_session_checkpoints
WHERE workflow_id = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteCheckpointsBySession :exec
DELETE FROM swarm_session_checkpoints WHERE session_id = ?;
```

#### 3. Add checkpoint env var
**File**: `harness/internal/swarm/env.go`
**Changes**: Add `CheckpointPath` field to `SwarmEnv`

```go
// Add to SwarmEnv struct:
CheckpointPath string `envconfig:"CM_SWARM_CHECKPOINT_PATH"`
```

#### 4. Extract checkpoint from observable state in PreCompact handler
**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: Extend `handleSwarmHookPreCompact` to extract and save checkpoint data after incrementing pressure. The `CWD` field in the hook payload gives us the session's working directory.

```go
// After existing pressure increment and event logging:
// Save checkpoint from observable state (async — don't block the hook response).
go s.SwarmManager.SaveCheckpointFromState(
    context.Background(), sessionID, payload.CWD)
```

The checkpoint extraction runs in a goroutine so `git diff` execution doesn't block the PreCompact hook response (10s timeout).

#### 5. Checkpoint data types and extraction logic
**File**: `harness/internal/swarmorch/checkpoint.go` (new)

```go
package swarmorch

// CheckpointData is assembled from observable session state, not LLM self-reports.
type CheckpointData struct {
    Progress      string   `json:"progress"`       // from optional summary file OR JSONL tail
    FilesModified []string `json:"files_modified"`  // from git diff --name-only HEAD
    PendingWork   string   `json:"pending_work"`    // from optional summary file
    CompactCount  int      `json:"compact_count"`
}

// SummaryFilePath returns the path where the session can optionally write a
// one-line progress summary. This is advisory — the checkpoint works without it.
func SummaryFilePath(sessionID string) string {
    return filepath.Join(os.TempDir(), "swarm-summary-"+sessionID+".txt")
}

// SaveCheckpointFromState extracts checkpoint data from git and JSONL logs.
func (m *Manager) SaveCheckpointFromState(ctx context.Context, sessionID, cwd string) {
    // 1. Get files modified via git diff (guaranteed accurate).
    filesModified := getGitDiffFiles(cwd)

    // 2. Read optional summary file (one-line progress note from session).
    progress := ""
    if data, err := os.ReadFile(SummaryFilePath(sessionID)); err == nil {
        progress = strings.TrimSpace(string(data))
    }

    // 3. If no summary file, extract last tool calls from JSONL log.
    if progress == "" {
        progress = m.extractProgressFromJSONL(sessionID)
    }

    // 4. Store in DB.
    m.saveCheckpoint(ctx, sessionID, CheckpointData{
        Progress:      progress,
        FilesModified: filesModified,
        CompactCount:  m.ctxPressure.Get(sessionID),
    })
}

func getGitDiffFiles(cwd string) []string {
    cmd := exec.Command("git", "diff", "--name-only", "HEAD")
    cmd.Dir = cwd
    out, err := cmd.Output()
    if err != nil { return nil }
    // Also check staged files.
    cmd2 := exec.Command("git", "diff", "--name-only", "--cached")
    cmd2.Dir = cwd
    out2, _ := cmd2.Output()

    combined := strings.TrimSpace(string(out)) + "\n" + strings.TrimSpace(string(out2))
    var files []string
    seen := map[string]bool{}
    for _, l := range strings.Split(combined, "\n") {
        l = strings.TrimSpace(l)
        if l != "" && !seen[l] { files = append(files, l); seen[l] = true }
    }
    return files
}

// extractProgressFromJSONL reads the last 10 tool_use events from the session's
// JSONL log and summarizes them as "Used {tools} on {files}".
func (m *Manager) extractProgressFromJSONL(sessionID string) string {
    // Read JSONL log, extract last N tool events, format as summary.
    // Implementation uses m.jsonlLogDir and sessionID to find the file.
}
```

#### 6. Load checkpoint into session env
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: In `buildEnv()`, when the workflow phase matches the last checkpoint's phase (i.e., we're resuming after `context_limit`), write the checkpoint to a temp file and set `CM_SWARM_CHECKPOINT_PATH`.

```go
// After handoff resolution in buildEnv():
checkpoint, cpErr := m.db.GetLatestCheckpointByWorkflow(ctx, wf.ID)
if cpErr == nil && string(checkpoint.Phase) == string(wf.Phase) {
    cpContent := formatCheckpointMarkdown(checkpoint)
    cpPath := filepath.Join(os.TempDir(), "swarm-cp-"+sessionID+".md")
    if writeErr := os.WriteFile(cpPath, []byte(cpContent), 0o600); writeErr == nil {
        se.CheckpointPath = cpPath
        cleanups = append(cleanups, cpPath)
    }
}
```

#### 7. Update prompt templates to use checkpoint
**File**: `harness/internal/swarm/prompt/templates/base.md.tmpl`
**Changes**: Add a "Previous Session Checkpoint" section that renders when `CheckpointPath` is set (read via env var in PromptContext).

```
{{- if .CheckpointContent }}
## Previous Session Checkpoint

This session is resuming after the previous session hit the context limit. Here is what the previous session accomplished:

{{ .CheckpointContent }}

**Important**: Do NOT redo work listed above. Continue from where the previous session left off.
{{- end }}
```

#### 8. Update PromptContext
**File**: `harness/internal/swarm/prompt/context.go`
**Changes**: Add `CheckpointContent string` field

#### 9. Simplified prompt template for progress tracking
**File**: `harness/internal/swarm/prompt/templates/base.md.tmpl`
**Changes**: Instead of asking the session to write full JSON, ask for a one-line summary (simpler, more likely to be followed). The orchestrator handles the rest via git diff extraction.

```
## Progress Tracking

Periodically write a one-line progress summary to help the next session if context is compacted:

    echo 'Implemented X, fixed Y tests, remaining: Z' > /tmp/swarm-summary-{{ .SessionID }}.txt

Update this as you make progress. The orchestrator combines this with git state to build a full checkpoint automatically.
```

#### 10. Update test schema
**File**: `harness/internal/swarmorch/manager_test.go`
**Changes**: Add `swarm_session_checkpoints` table to `swarmFullTestSchema`

### Success Criteria:

#### Automated Verification:
- [ ] Migration applies cleanly (check schema with in-memory DB in test)
- [ ] `just check` passes
- [ ] New `checkpoint_test.go` tests pass: git diff extraction, JSONL progress extraction, save/load/format
- [ ] Existing `manager_test.go` tests pass with updated schema
- [ ] `buildEnv` test verifies `CM_SWARM_CHECKPOINT_PATH` is set when checkpoint exists

#### Manual Verification:
- [ ] Start a swarm session, make edits, verify `SaveCheckpointFromState` captures git-diffed files on PreCompact
- [ ] Force a `context_limit` result, verify the resumed session receives checkpoint context with accurate file list
- [ ] Verify checkpoint content appears in the rendered prompt
- [ ] Verify checkpoint works even if the session never writes a summary file (JSONL fallback)

---

## Phase 3: Structured Learnings

### Overview
Enhance the learning system with domain classification, reliability tiers, and outcome tracking. Replace the flat 0.95x decay with tier-based expiry. This makes learnings more useful for future sessions by surfacing the right knowledge at the right time.

### Changes Required:

#### 1. New domain and classification enums
**File**: `harness/internal/swarm/enums.go`
**Changes**: Add `LearningDomain` and `LearningClassification` types

```go
// LearningDomain classifies which area of the codebase a learning applies to.
type LearningDomain string

const (
    DomainArchitecture LearningDomain = "architecture"
    DomainTesting      LearningDomain = "testing"
    DomainDeployment   LearningDomain = "deployment"
    DomainTemplates    LearningDomain = "templates"
    DomainSwarm        LearningDomain = "swarm"
    DomainGeneral      LearningDomain = "general"
)

// LearningClassification determines how long a learning stays relevant.
type LearningClassification string

const (
    ClassFoundational LearningClassification = "foundational" // permanent
    ClassTactical     LearningClassification = "tactical"     // 14-day shelf life
    ClassObservational LearningClassification = "observational" // 30-day shelf life
)
```

#### 2. DB migration for learning enhancements
**File**: `harness/internal/db/migrations/010_structured_learnings.sql` (new — separate from Phase 2's `009_session_checkpoints.sql`)

```sql
-- Add columns to swarm_learnings
ALTER TABLE swarm_learnings ADD COLUMN domain TEXT NOT NULL DEFAULT 'general';
ALTER TABLE swarm_learnings ADD COLUMN classification TEXT NOT NULL DEFAULT 'tactical';
ALTER TABLE swarm_learnings ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE swarm_learnings ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE swarm_learnings ADD COLUMN expires_at TEXT;

-- Index for domain-based queries
CREATE INDEX idx_swarm_learnings_domain ON swarm_learnings(domain, archived_at);
```

#### 3. Update SQL queries
**File**: `harness/internal/db/queries/swarm_learnings.sql`
**Changes**:
- Update `CreateSwarmLearning` to include domain, classification
- Add `ListTopSwarmLearningsByDomain` query
- Update `DecaySwarmLearningRelevance` to use classification-based expiry instead of flat decay
- Add `RecordLearningOutcome` to increment success/failure counts
- Add `ExpireByClassification` query

```sql
-- name: ListTopSwarmLearningsByDomain :many
SELECT * FROM swarm_learnings
WHERE domain = ? AND archived_at IS NULL
ORDER BY relevance_score DESC
LIMIT ?;

-- name: RecordLearningSuccessOutcome :exec
UPDATE swarm_learnings
SET success_count = success_count + 1,
    relevance_score = MIN(relevance_score + 0.2, 2.0),
    updated_at = datetime('now')
WHERE id = ?;

-- name: RecordLearningFailureOutcome :exec
UPDATE swarm_learnings
SET failure_count = failure_count + 1,
    relevance_score = MAX(relevance_score - 0.3, 0.0),
    updated_at = datetime('now')
WHERE id = ?;

-- name: ExpireByClassification :exec
UPDATE swarm_learnings
SET archived_at = datetime('now'), updated_at = datetime('now')
WHERE archived_at IS NULL
  AND ((classification = 'tactical' AND created_at < datetime('now', '-14 days'))
    OR (classification = 'observational' AND created_at < datetime('now', '-30 days')));
```

#### 4. Update learning capture functions
**File**: `harness/internal/swarmorch/learnings.go`
**Changes**:
- Update `createLearning()` to accept domain and classification params
- Update all `capture*` functions to pass appropriate domain/classification
- Update `getLearningContext()` to group by domain and include classification info
- Replace `decayLearningRelevance()` with `expireByClassification()`

```go
// capturePlanIssue -> domain: DomainArchitecture, classification: ClassTactical
// captureCodeBug -> domain: DomainGeneral (or infer from file paths), classification: ClassTactical
// captureTerminalFailure -> domain: DomainSwarm, classification: ClassFoundational
// captureSuccessPattern -> domain: DomainGeneral, classification: ClassObservational
```

#### 5. Update learning context formatting
**File**: `harness/internal/swarmorch/learnings.go`
**Changes**: `getLearningContext()` now groups learnings by domain and shows classification

```markdown
## Foundational Learnings
- **[critical] Title** (architecture): Content [5 successes, 0 failures]

## Phase Learnings (implement)
- **[warning] Title** (testing, tactical): Content [2 successes, 1 failure]

## Ticket History
- ...
```

#### 6. Update maintenance loop
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Replace `decayLearningRelevance()` call in maintenance loop with `expireByClassification()`. Keep the flat decay as a secondary mechanism for anything that doesn't have a classification set.

#### 7. Update CreateSwarmLearning params
**File**: `harness/internal/db/queries/swarm_learnings.sql`
**Changes**: Update the INSERT to include new columns

#### 8. Update test schema and tests
**File**: `harness/internal/swarmorch/manager_test.go`
**Changes**: Add new columns to test schema, update `TestCaptureLearningsRouting` to verify domain/classification

#### 9. Auto-insight extraction from session transcripts
**File**: `harness/internal/swarmorch/learnings.go`
**Changes**: Add `captureTranscriptInsights()` called from `handleSessionComplete()` in `manager.go`

After session completion, parse the JSONL transcript to extract tool usage profiles and file change patterns. Feed these into the structured learnings system automatically. The transcript parser already exists at `internal/swarm/transcript/parse.go` and `discover.go`.

```go
// captureTranscriptInsights analyzes a completed session's JSONL log to
// extract structured insights as observational learnings.
func (m *Manager) captureTranscriptInsights(
    ctx context.Context, wf sqlc.SwarmWorkflow, sessionID string,
) {
    events := m.readJSONLEvents(sessionID)
    if len(events) == 0 { return }

    // 1. Tool usage profile: count by tool name.
    toolCounts := map[string]int{}
    var filePaths []string
    for _, evt := range events {
        if tool, ok := evt["tool"].(string); ok { toolCounts[tool]++ }
        if input, ok := evt["input"].(map[string]any); ok {
            if fp, ok := input["file_path"].(string); ok {
                filePaths = append(filePaths, fp)
            }
        }
    }

    // 2. Infer domain from file paths.
    domain := inferDomainFromPaths(filePaths)

    // 3. Create observational learning with tool/file profile.
    m.createLearning(ctx, wf.ID, sessionID, wf.TicketID,
        swarm.LearningPattern, swarm.Phase(wf.Phase), swarm.SeverityInfo,
        fmt.Sprintf("Session profile: %s phase", wf.Phase),
        fmt.Sprintf("Tools: %v, Files: %d touched", toolCounts, len(filePaths)),
        domain, swarm.ClassObservational)
}

func inferDomainFromPaths(paths []string) swarm.LearningDomain {
    for _, p := range paths {
        switch {
        case strings.Contains(p, "templates/"): return swarm.DomainTemplates
        case strings.Contains(p, "internal/swarm"): return swarm.DomainSwarm
        case strings.Contains(p, "_test.go"): return swarm.DomainTesting
        }
    }
    return swarm.DomainGeneral
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Migration applies cleanly
- [ ] `just check` passes
- [ ] Updated `TestCaptureLearningsRouting` passes with domain/classification assertions
- [ ] New test for `ExpireByClassification` verifies correct expiry per tier
- [ ] New test for `RecordLearningSuccessOutcome`/`RecordLearningFailureOutcome` verifies score adjustments
- [ ] `getLearningContext` test verifies grouped-by-domain output format
- [ ] New `transcript_insights_test.go`: tool usage profile extraction, domain inference from paths

#### Manual Verification:
- [ ] Trigger a workflow that captures learnings, verify domain and classification in DB
- [ ] Verify learning context in rendered prompt shows grouped format
- [ ] Verify tactical learnings expire after 14 days, observational after 30

---

## Phase 4: Progressive Health Monitoring

### Overview
Replace the binary stall detection with a graduated escalation system: warn → file-based nudge (via PostToolUse hook) → optional AI triage hook → kill. Track escalation level per workflow so we don't re-alert at the same level. Add ZFC health reconciliation to the dashboard SSE handler.

### Changes Required:

#### 1. New escalation types
**File**: `harness/internal/swarmorch/health.go`
**Changes**: Add escalation tracking types and constants

```go
// EscalationLevel represents the current health monitoring level for a workflow.
type EscalationLevel int

const (
    EscalationNone    EscalationLevel = 0
    EscalationWarn    EscalationLevel = 1  // Log warning + emit event
    EscalationNudge   EscalationLevel = 2  // Write nudge file for PostToolUse hook injection
    EscalationTriage  EscalationLevel = 3  // Hook point for AI triage (not implemented)
    EscalationKill    EscalationLevel = 4  // Kill the tmux session
)

// Thresholds in minutes since last update.
const (
    warnThresholdMinutes    = 30
    nudgeThresholdMinutes   = 45
    triageThresholdMinutes  = 60  // reserved for future AI triage
    killThresholdMinutes    = 90
)
```

#### 2. Escalation tracker
**File**: `harness/internal/swarmorch/escalation.go` (new)

```go
// EscalationTracker tracks the current escalation level per workflow.
type EscalationTracker struct {
    mu     sync.RWMutex
    levels map[string]EscalationLevel // keyed by workflow ID
}

func NewEscalationTracker() *EscalationTracker { ... }
func (et *EscalationTracker) Get(workflowID string) EscalationLevel { ... }
func (et *EscalationTracker) Set(workflowID string, level EscalationLevel) { ... }
func (et *EscalationTracker) Reset(workflowID string) { ... }
```

**Known limitation**: The in-memory escalation tracker resets on harness restart. After restart, all workflows re-enter `EscalationNone` regardless of prior state. This is acceptable because the maintenance loop recalculates staleness from DB timestamps on the next tick (2min interval). Worst case: one redundant warn/nudge cycle. If persistence is needed later, add `escalation_level TEXT` column to `swarm_workflows`.

#### 3. PostToolUse-based nudge mechanism
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Rewrite `nudgeSession()` to write a file instead of using tmux send-keys (which can corrupt Claude Code session state)

```go
// nudgeSession writes a nudge file that the PostToolUse hook will pick up
// and inject as additionalContext on the next tool call.
func (m *Manager) nudgeSession(sessionID, message string) error {
    nudgePath := filepath.Join(os.TempDir(), "swarm-nudge-"+sessionID)
    return os.WriteFile(nudgePath, []byte(message), 0o600)
}
```

**File**: `harness/internal/server/swarm_hooks.go`
**Changes**: Extend `handleSwarmHookPostToolUse` to check for nudge file and return `additionalContext`

```go
func (s *Server) handleSwarmHookPostToolUse(c echo.Context) error {
    // ... existing decode + JSONL logging + EventBus publish ...

    sessionID := swarmSessionID(c, &payload.hookPayload)

    // Check for pending nudge.
    nudgePath := filepath.Join(os.TempDir(), "swarm-nudge-"+sessionID)
    if data, err := os.ReadFile(nudgePath); err == nil {
        _ = os.Remove(nudgePath) // consume the nudge
        return c.JSON(http.StatusOK, map[string]any{
            "additionalContext": string(data),
        })
    }

    return c.NoContent(http.StatusNoContent)
}
```

This is safe because:
- PostToolUse fires after every tool call (autonomous sessions make many — nudge picked up within seconds)
- `additionalContext` is the official Claude Code mechanism for hook-injected messages
- No tmux send-keys — no risk of corrupting terminal state
- Nudge is consumed (file deleted) so it only fires once

#### 4. Replace `detectAndAlertStalls` with graduated monitoring
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Replace the existing `detectAndAlertStalls()` with `monitorWorkflowHealth()`

```go
func (m *Manager) monitorWorkflowHealth(ctx context.Context) {
    health, err := m.GetHealth(ctx)
    if err != nil { return }

    for _, wf := range health.ActiveWorkflows {
        if wf.AwaitingReview { continue }

        minutesSinceUpdate := m.minutesSinceUpdate(wf)
        currentLevel := m.escalation.Get(wf.WorkflowID)

        switch {
        case minutesSinceUpdate >= killThresholdMinutes && currentLevel < EscalationKill:
            m.escalateToKill(ctx, wf)
        case minutesSinceUpdate >= nudgeThresholdMinutes && currentLevel < EscalationNudge:
            m.escalateToNudge(ctx, wf)
        case minutesSinceUpdate >= warnThresholdMinutes && currentLevel < EscalationWarn:
            m.escalateToWarn(ctx, wf)
        }
    }
}

func (m *Manager) escalateToWarn(ctx context.Context, wf ActiveWorkflowInfo) {
    m.escalation.Set(wf.WorkflowID, EscalationWarn)
    m.emitEvent(ctx, wf.WorkflowID, "", wf.TicketID, EventStallDetected, Phase(wf.Phase),
        fmt.Sprintf("No progress for %d minutes (warn level)", warnThresholdMinutes))
    if m.alertMgr != nil {
        m.alertMgr.FireStallDetected(wf.TicketID)
    }
}

func (m *Manager) escalateToNudge(ctx context.Context, wf ActiveWorkflowInfo) {
    m.escalation.Set(wf.WorkflowID, EscalationNudge)
    // Look up active session ID for this workflow to write the nudge file.
    if sess, err := m.db.GetActiveSessionByWorkflow(ctx, wf.WorkflowID); err == nil {
        m.nudgeSession(sess.ID, "[SWARM] You've been running 45+ min. Please wrap up or report infra_failure.")
    }
    m.emitEvent(ctx, wf.WorkflowID, "", wf.TicketID, EventStallDetected, Phase(wf.Phase),
        fmt.Sprintf("Nudge sent after %d minutes", nudgeThresholdMinutes))
}

func (m *Manager) escalateToKill(ctx context.Context, wf ActiveWorkflowInfo) {
    m.escalation.Set(wf.WorkflowID, EscalationKill)
    sessionName := SessionName(wf.TicketID, Phase(wf.Phase))
    // Kill the tmux session — watchSession will detect death and handle completion
    exec.Command("tmux", "kill-session", "-t", sessionName).Run()
    m.emitEvent(ctx, wf.WorkflowID, "", wf.TicketID, EventStallDetected, Phase(wf.Phase),
        fmt.Sprintf("Session killed after %d minutes", killThresholdMinutes))
}
```

#### 5. Wire escalation tracker into Manager
**File**: `harness/internal/swarmorch/manager.go`
**Changes**:
- Add `escalation *EscalationTracker` field to Manager
- Initialize in `NewManager()`
- Reset escalation on workflow completion/advancement
- Replace `detectAndAlertStalls()` call in maintenance loop with `monitorWorkflowHealth()`

#### 6. Update stall detection in health endpoint
**File**: `harness/internal/swarmorch/health.go`
**Changes**: Update `queryActiveWorkflows()` to use graduated thresholds. Add an `EscalationLevel` field to `ActiveWorkflowInfo`.

```go
type ActiveWorkflowInfo struct {
    // ... existing fields ...
    EscalationLevel EscalationLevel `json:"escalation_level"`
}
```

**Activity-based staleness**: The PostToolUse hook already fires on every tool call and updates the JSONL log. Extend the `ContextPressure` tracker to also track `lastToolUseAt` per session. Use this as the "last update" time for stall detection instead of `updated_at` from the DB — a session actively making tool calls is not stalled even if the DB timestamp is old.

Per-phase threshold multipliers (applied to the base thresholds above):
- `research`: 1.5x (exploratory, more idle time expected)
- `implement`: 1.0x (baseline)
- `verify`: 0.75x (should be faster)
- `pr`: 0.5x (PR creation is quick)

#### 7. ZFC health reconciliation in dashboard SSE
**File**: `harness/internal/server/swarm_dashboard.go`
**Changes**: On SSE heartbeat, verify tmux session liveness for active workflows before sending fragments. If DB says "running" but tmux is dead, trigger reconciliation immediately instead of waiting for `watchSession` to detect it (up to 30s delay).

```go
// ReconcileActiveSessions checks tmux liveness for all active sessions and
// triggers handleSessionComplete for any ghost sessions.
func (m *Manager) ReconcileActiveSessions(ctx context.Context) {
    activeWfs, err := m.db.ListActiveSwarmWorkflows(ctx)
    if err != nil { return }
    for _, wf := range activeWfs {
        sess, sessErr := m.db.GetActiveSessionByWorkflow(ctx, wf.ID)
        if sessErr != nil { continue }
        if !isTmuxSessionAlive(sess.SessionName) {
            m.logger.Warn("ZFC reconciliation: tmux dead but DB says active",
                "session", sess.SessionName, "workflow", wf.ID)
            go m.handleSessionComplete(ctx, sess.ID)
        }
    }
}
```

Called from the dashboard SSE heartbeat (already runs every ~30s). Lightweight: one `tmux has-session` shell-out per active workflow. This implements Overstory's "ZFC Principle": observable state (tmux alive?) overrides recorded DB state.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] New `escalation_test.go` tests pass: Get/Set/Reset lifecycle
- [ ] New test for graduated monitoring: verify warn < nudge < kill ordering
- [ ] PostToolUse nudge delivery test: write nudge file, verify additionalContext in response, verify file deleted
- [ ] Existing health tests still pass

#### Manual Verification:
- [ ] Start a swarm session, let it stall, verify warn event at 30min
- [ ] Verify nudge file is written at 45min and picked up by PostToolUse hook
- [ ] Verify session is killed at 90min
- [ ] Verify escalation resets when workflow advances
- [ ] Dashboard SSE reconciliation: stop a tmux session externally, verify dashboard detects it immediately

---

## Phase 5: CLI Observability

### Overview
Add `just swarm trace`, `just swarm costs`, and `just swarm doctor` commands for developer debugging from the terminal. These complement the web dashboard with deeper inspection capabilities.

### Changes Required:

#### 1. Create swarm CLI tool
**File**: `harness/cmd/swarmctl/main.go` (new)
**Changes**: A small Go CLI that queries the swarm DB directly

```go
package main

import (
    "fmt"
    "os"
)

func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    switch os.Args[1] {
    case "trace":
        cmdTrace(os.Args[2:])
    case "costs":
        cmdCosts(os.Args[2:])
    case "doctor":
        cmdDoctor(os.Args[2:])
    default:
        printUsage()
        os.Exit(1)
    }
}
```

#### 2. `trace` subcommand
**File**: `harness/cmd/swarmctl/trace.go` (new)

Prints a chronological event timeline for a workflow, with color-coded phases and results.

```
$ just swarm trace wf-abc123

Workflow wf-abc123 (CM-42) — code — running
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
14:30:00  workflow_started     research
14:30:05  session_spawned      research  sess-aaa
14:35:22  session_completed    research  success (5m17s)
14:35:23  phase_completed      research
14:35:24  phase_started        code_plan
14:35:25  session_spawned      code_plan sess-bbb
14:42:10  session_completed    code_plan success (6m45s)
...
```

Implementation: Opens the swarm DB read-only, queries `swarm_events` by workflow ID, formats as timeline.

#### 3. `costs` subcommand
**File**: `harness/cmd/swarmctl/costs.go` (new)

Shows token usage and estimated costs by workflow and phase.

```
$ just swarm costs

Workflow     Ticket   Type  Sessions  Tokens     Est. Cost  Status
─────────────────────────────────────────────────────────────────────
wf-abc123    CM-42    code  6         1,245,000  $4.32      running
wf-def456    CM-99    research 1      320,000    $1.10      complete
─────────────────────────────────────────────────────────────────────
Total                       7         1,565,000  $5.42

$ just swarm costs wf-abc123

Phase        Sessions  Input     Output    Cache Read  Est. Cost
────────────────────────────────────────────────────────────────
research     1         50,000    30,000    0           $0.40
code_plan    2         120,000   80,000    50,000      $1.20
implement    2         200,000   150,000   100,000     $2.00
verify       1         80,000    40,000    30,000      $0.72
────────────────────────────────────────────────────────────────
Total        6         450,000   300,000   180,000     $4.32
```

Implementation: Queries `swarm_sessions` with token columns from migration 007. Uses `transcript.EstimateCost()` from `internal/swarm/transcript/pricing.go` for accurate per-model pricing (already exists: opus $15/$75, sonnet $3/$15, haiku $0.80/$4 per million tokens).

```go
import "creative-mode/harness/internal/swarm/transcript"

func calculateSessionCost(sess dbRow) float64 {
    model := "sonnet" // default if model not recorded
    if sess.ModelUsed != "" { model = sess.ModelUsed }
    return transcript.EstimateCost(model,
        sess.InputTokens, sess.OutputTokens,
        sess.CacheReadTokens, sess.CacheCreationTokens)
}
```

For sessions that only have `total_tokens` (no breakdown), estimate with 40/60 input/output split at sonnet pricing.

#### 4. `doctor` subcommand
**File**: `harness/cmd/swarmctl/doctor.go` (new)

Health checks for the swarm system:

```
$ just swarm doctor

Swarm Doctor — System Health Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[✓] SQLite database accessible
[✓] tmux available (tmux 3.4)
[✓] Claude Code installed (claude 1.0.x)
[✓] CM_HOOK_SECRET set
[✓] ANTHROPIC_API_KEY set
[✗] LINEAR_API_KEY not set — Linear integration disabled
[✓] No orphaned tmux sessions
[✓] No stalled workflows
[!] 2 workflows awaiting review
[✓] Disk space OK (45GB free)
[✓] Last digest: 2026-03-01 (1 learning)

Summary: 9 passed, 1 failed, 1 warning
```

Checks:
- DB connectivity (open and query swarm_config)
- tmux availability (`tmux -V`)
- Claude Code availability (`claude --version`)
- Required env vars (CM_HOOK_SECRET, ANTHROPIC_API_KEY)
- Optional env vars (LINEAR_API_KEY, GRAPHITE_TOKEN, DISCORD_SWARM_CHANNEL_ID)
- Orphaned tmux sessions (`tmux list-sessions` vs DB active sessions)
- Stalled workflows (same logic as health endpoint)
- Awaiting review count
- Disk space
- Last digest age

#### 5. Justfile recipes
**File**: `justfile` (project root)
**Changes**: Add swarm recipes

```just
# Swarm: event timeline for a workflow
swarm-trace workflowID:
    cd harness && go run ./cmd/swarmctl trace {{workflowID}}

# Swarm: token costs summary (optionally for one workflow)
swarm-costs *args='':
    cd harness && go run ./cmd/swarmctl costs {{args}}

# Swarm: system health check
swarm-doctor:
    cd harness && go run ./cmd/swarmctl doctor
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `go build ./harness/cmd/swarmctl/` compiles
- [ ] Doctor check runs without panic on a fresh DB

#### Manual Verification:
- [ ] `just swarm-trace <id>` shows a formatted timeline for an existing workflow
- [ ] `just swarm-costs` shows token summary table with real dollar amounts
- [ ] `just swarm-doctor` shows all health checks with pass/fail indicators

---

## Phase 6: Operational Polish

### Overview
Lower-priority improvements that build on earlier phases: incremental SSE to reduce dashboard re-query overhead, and per-workflow config overrides for flexible workflow management.

### Depends On: Phase 3 (for auto-insight wiring in createLearning signature)

### Changes Required:

#### 1. Incremental SSE with lastSeenEventID
**File**: `harness/internal/server/swarm_dashboard.go`
**Changes**: Track `lastSeenEventID` in the SSE loop. On initial connection, send all data. On subsequent `swarm.*` events, query only events newer than the last seen ID and append-patch them instead of refreshing the full tab.

**New SQL query**:
**File**: `harness/internal/db/queries/swarm.sql`
```sql
-- name: ListSwarmEventsSince :many
SELECT id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at
FROM swarm_events WHERE created_at > ? ORDER BY created_at ASC LIMIT 50;
```

For workflow status changes, extract workflow ID from the event payload and query only that workflow, not the full list.

#### 2. Per-workflow dispatch overrides
**File**: `harness/internal/swarm/statemachine.go`
**Changes**: Add `WorkflowOverrides` struct

```go
type WorkflowOverrides struct {
    SkipResearch   bool `json:"skipResearch,omitempty"`
    SkipPlanReview bool `json:"skipPlanReview,omitempty"`
}
```

**File**: `harness/internal/server/swarm_api.go`
**Changes**: Accept `overrides` in the start workflow request. Store as JSON in a new `overrides TEXT` column on `swarm_workflows`.

**File**: `harness/internal/swarm/statemachine.go`
**Changes**: `DetermineNextPhase()` checks overrides to skip phases. `SkipResearch` starts code workflows at `code_plan`. `SkipPlanReview` skips the plan_review gate even if globally enabled.

Use case: well-understood bug fixes skip research, simple changes skip plan review.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] Incremental SSE test: verify only new events are sent after initial load
- [ ] Override test: verify `SkipResearch` starts workflow at `code_plan` phase

#### Manual Verification:
- [ ] Open dashboard, verify events append incrementally (not full refresh)
- [ ] Start workflow with `skipResearch: true`, verify it begins at `code_plan`

---

## Testing Strategy

### Unit Tests:
- `hooks_test.go`: Pattern matching for all deny/danger patterns and blocked tools
- `checkpoint_test.go`: Git diff extraction, JSONL progress extraction, save/load/format
- `escalation_test.go`: Escalation tracker lifecycle (get/set/reset), in-memory reset behavior
- Updated `manager_test.go`: Schema changes, checkpoint in buildEnv, learning domain/classification
- `learnings_test.go`: Outcome tracking (separate success/failure queries), classification-based expiry
- `transcript_insights_test.go`: Tool usage profile extraction, domain inference from paths

### Integration Tests:
- Full workflow advancement with checkpoint save/resume cycle (mock tmux)
- Graduated health monitoring across time thresholds
- PostToolUse nudge delivery: write nudge file, verify additionalContext in response, verify file deleted
- Dashboard SSE reconciliation: mock tmux dead, verify handleSessionComplete triggered

### Manual Testing Steps:
1. Start a code workflow via dashboard, verify all new fields in JSONL logs
2. Force a session to hit context_limit, verify checkpoint is loaded by next session
3. Let a session stall, observe graduated alerts in events tab
4. Run all three CLI commands against the live DB
5. Verify blocked tools show deny in JSONL log

## Performance Considerations

- Checkpoint git diff runs in a goroutine — does not block the PreCompact hook response (10s timeout)
- PostToolUse nudge check is a single `os.ReadFile` (fast ENOENT on miss — no overhead for non-nudged sessions)
- Escalation tracker is in-memory (no DB queries per tick)
- `ReconcileActiveSessions` is one `tmux has-session` shell-out per active workflow per SSE heartbeat
- CLI tools open DB read-only — no contention with the running harness
- Classification-based expiry is a simple WHERE clause — no full table scan
- Incremental SSE reduces DB queries from O(all_events) to O(new_events) per tick
- Auto-insight extraction runs after session completion (already async in handleSessionComplete)

## Migration Notes

- **`009_session_checkpoints.sql`**: Creates `swarm_session_checkpoints` table (Phase 2)
- **`010_structured_learnings.sql`**: Adds `domain`, `classification`, `success_count`, `failure_count`, `expires_at` columns to `swarm_learnings` (Phase 3). Optionally adds `overrides TEXT` column to `swarm_workflows` (Phase 6, or defer to `011_`)
- Existing learnings get `domain='general'`, `classification='tactical'` defaults
- No data migration needed — new columns have sensible defaults
- Test schema in `manager_test.go` must be updated for new tables and columns
- The two `007_*` files are a pre-existing collision to resolve separately

## References

- Research: `thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md`
- Review: `thoughts/CoreyCole/reviews/2026-03-01_21-49-06_overstory-swarm-improvements_review.md`
- Overstory hooks: `context/overstory/src/agents/hooks-deployer.ts`
- Overstory checkpoints: `context/overstory/src/agents/checkpoint.ts`
- Overstory watchdog: `context/overstory/src/watchdog/daemon.ts`
- Overstory Mulch: `context/overstory/src/mulch/client.ts`
- Current hooks: `harness/internal/swarmorch/hooks.go`
- Current health: `harness/internal/swarmorch/health.go`
- Current learnings: `harness/internal/swarmorch/learnings.go`
- Current manager: `harness/internal/swarmorch/manager.go`
- State machine: `harness/internal/swarm/statemachine.go`
- Manager tests: `harness/internal/swarmorch/manager_test.go`
- Transcript pricing: `harness/internal/swarm/transcript/pricing.go` (per-model rates already exist)
- Transcript parsing: `harness/internal/swarm/transcript/parse.go`
- Transcript discovery: `harness/internal/swarm/transcript/discover.go`
- Prompt context: `harness/internal/swarm/prompt/context.go`
- Dashboard SSE: `harness/internal/server/swarm_dashboard.go`
- PostToolUse handler: `harness/internal/server/swarm_hooks.go`
