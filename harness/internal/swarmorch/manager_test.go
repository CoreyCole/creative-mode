package swarmorch

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/swarm"
)

// swarmFullTestSchema contains all CREATE TABLE statements needed for
// Manager tests (workflows, sessions, events, tickets, config, learnings).
const swarmFullTestSchema = `
CREATE TABLE IF NOT EXISTS swarm_workflows (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    workflow_type        TEXT NOT NULL,
    phase                TEXT NOT NULL,
    status               TEXT NOT NULL,
    attempt              INTEGER NOT NULL DEFAULT 1,
    previous_workflow_id TEXT,
    branch_name          TEXT,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_sessions (
    id            TEXT PRIMARY KEY,
    workflow_id   TEXT NOT NULL REFERENCES swarm_workflows(id),
    session_name  TEXT NOT NULL,
    skill         TEXT NOT NULL,
    phase         TEXT NOT NULL,
    result        TEXT,
    detail        TEXT,
    duration_sec  INTEGER,
    total_tokens  INTEGER,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at  TEXT
);
CREATE TABLE IF NOT EXISTS swarm_learnings (
    id                 TEXT PRIMARY KEY,
    source_workflow_id TEXT REFERENCES swarm_workflows(id),
    source_session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id          TEXT NOT NULL,
    category           TEXT NOT NULL,
    phase              TEXT,
    severity           TEXT NOT NULL DEFAULT 'info',
    title              TEXT NOT NULL,
    content            TEXT NOT NULL,
    doc_path           TEXT,
    tags               TEXT,
    relevance_score    REAL NOT NULL DEFAULT 1.0,
    referenced_count   INTEGER NOT NULL DEFAULT 0,
    archived_at        TEXT,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_events (
    id          TEXT PRIMARY KEY,
    workflow_id TEXT REFERENCES swarm_workflows(id),
    session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id   TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    phase       TEXT,
    detail      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_tickets (
    id          TEXT PRIMARY KEY,
    identifier  TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT '',
    priority    INTEGER,
    assignee    TEXT,
    labels      TEXT,
    parent_id   TEXT,
    project_id  TEXT,
    url         TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    synced_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_config (
    id     TEXT PRIMARY KEY,
    config TEXT NOT NULL DEFAULT '{}'
);
INSERT OR IGNORE INTO swarm_config (id, config) VALUES ('default', '{}');
CREATE TABLE IF NOT EXISTS swarm_ticket_comments (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL,
    body       TEXT NOT NULL,
    author     TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    synced_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_learning_digests (
    id              TEXT PRIMARY KEY,
    digest_type     TEXT NOT NULL,
    period_start    TEXT NOT NULL,
    period_end      TEXT NOT NULL,
    learning_count  INTEGER NOT NULL DEFAULT 0,
    summary         TEXT NOT NULL DEFAULT '',
    action_items    TEXT,
    doc_path        TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_project_milestones (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    project_id   TEXT,
    name         TEXT NOT NULL,
    criteria     TEXT NOT NULL,
    status       TEXT NOT NULL CHECK(status IN ('pending', 'passed', 'failed')),
    verified_at  TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS swarm_ticket_dependencies (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    depends_on_ticket_id TEXT NOT NULL,
    project_id           TEXT NOT NULL,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(ticket_id, depends_on_ticket_id)
);
`

// newManagerTestDB creates an in-memory SQLite DB with the full swarm schema
// and returns a db.DB wrapper suitable for Manager tests.
func newManagerTestDB(t *testing.T) *db.DB {
	t.Helper()

	rawDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	rawDB.SetMaxOpenConns(1) // SQLite in-memory DBs are per-connection; limit to 1.
	t.Cleanup(func() { _ = rawDB.Close() })

	if _, err := rawDB.ExecContext(t.Context(), swarmFullTestSchema); err != nil {
		t.Fatalf("exec schema: %v", err)
	}

	return db.NewForTest(rawDB, sqlc.New(rawDB))
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()
	baseDir := t.TempDir()

	mgr := NewManager(
		database,
		testLogger(),
		bus,
		baseDir,
		t.TempDir(),
		"http://localhost:8080",
	)

	ctx := t.Context()

	// Create a ticket for URL lookup.
	_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         "CM-42",
		Identifier: "CM-42",
		Title:      "Test Ticket",
		Status:     "In Progress",
		Url:        "https://linear.app/cm/issue/CM-42",
		CreatedAt:  "2026-01-01T00:00:00Z",
		UpdatedAt:  "2026-01-01T00:00:00Z",
	})

	wf := sqlc.SwarmWorkflow{
		ID:           "wf-001",
		TicketID:     "CM-42",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
		BranchName:   sql.NullString{String: "swarm/CM-42/test", Valid: true},
	}

	env, cleanup := mgr.buildEnv(ctx, wf, "sess-001")
	defer cleanup()

	// Verify required env vars.
	checks := map[string]string{
		"CM_SWARM_TICKET_ID":   "CM-42",
		"CM_SWARM_WORKFLOW_ID": "wf-001",
		"CM_SWARM_SESSION_ID":  "sess-001",
		"CM_SWARM_PHASE":       "research",
		"CM_SWARM_ATTEMPT":     "1",
		"CM_SWARM_BRANCH":      "swarm/CM-42/test",
		"CM_SWARM_TICKET_URL":  "https://linear.app/cm/issue/CM-42",
		"CM_HARNESS_URL":       "http://localhost:8080",
	}

	for key, want := range checks {
		if got := env[key]; got != want {
			t.Errorf("env[%q] = %q, want %q", key, got, want)
		}
	}

	// Verify result path is set.
	if env["CM_SWARM_RESULT_PATH"] == "" {
		t.Error("CM_SWARM_RESULT_PATH not set")
	}
}

func TestBuildEnvNoBranch(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(
		database,
		testLogger(),
		nil,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	wf := sqlc.SwarmWorkflow{
		ID:           "wf-002",
		TicketID:     "CM-99",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseResearch,
		Attempt:      1,
	}

	env, cleanup := mgr.buildEnv(t.Context(), wf, "sess-002")
	defer cleanup()

	if _, ok := env["CM_SWARM_BRANCH"]; ok {
		t.Error("CM_SWARM_BRANCH should not be set when branch is null")
	}
}

func TestAdvanceWorkflowTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		workflowType swarm.WorkflowType
		phases       []struct {
			phase  swarm.Phase
			result swarm.SessionResult
		}
		wantFinalStatus swarm.WorkflowStatus
		wantFinalPhase  swarm.Phase
	}{
		{
			name:         "research_only_success",
			workflowType: swarm.WorkflowTypeResearch,
			phases: []struct {
				phase  swarm.Phase
				result swarm.SessionResult
			}{
				{swarm.PhaseResearch, swarm.ResultSuccess},
			},
			wantFinalStatus: swarm.StatusComplete,
			wantFinalPhase:  swarm.PhaseResearch,
		},
		{
			name:         "timeout_fails",
			workflowType: swarm.WorkflowTypeCode,
			phases: []struct {
				phase  swarm.Phase
				result swarm.SessionResult
			}{
				{swarm.PhaseResearch, swarm.ResultTimeout},
			},
			wantFinalStatus: swarm.StatusFailed,
			wantFinalPhase:  swarm.PhaseResearch,
		},
		{
			name:         "code_research_to_plan",
			workflowType: swarm.WorkflowTypeCode,
			phases: []struct {
				phase  swarm.Phase
				result swarm.SessionResult
			}{
				{swarm.PhaseResearch, swarm.ResultSuccess},
			},
			// Should advance to code_plan but we can't spawn sessions in test,
			// so just verify the workflow phase was updated.
			wantFinalStatus: swarm.StatusRunning,
			wantFinalPhase:  swarm.PhaseCodePlan,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			database := newManagerTestDB(t)
			bus := events.NewEventBus()
			mgr := NewManager(
				database,
				testLogger(),
				bus,
				t.TempDir(),
				t.TempDir(),
				"http://localhost:8080",
			)

			ctx := t.Context()

			// Create workflow.
			wfID := "wf-" + tt.name
			err := database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
				ID:           wfID,
				TicketID:     "TKT-" + tt.name,
				WorkflowType: tt.workflowType,
				Phase:        tt.phases[0].phase,
				Status:       swarm.StatusRunning,
				Attempt:      1,
			})
			if err != nil {
				t.Fatalf("create workflow: %v", err)
			}

			for _, p := range tt.phases {
				wf, wfErr := database.GetSwarmWorkflow(ctx, wfID)
				if wfErr != nil {
					t.Fatalf("get workflow: %v", wfErr)
				}

				result := &swarm.SessionResultData{
					Result:  p.result,
					Phase:   p.phase,
					Summary: "test",
				}

				// Call advanceWorkflow directly (skip session spawning).
				mgr.advanceWorkflow(ctx, wf, result)
			}

			// Verify final state.
			finalWf, err := database.GetSwarmWorkflow(ctx, wfID)
			if err != nil {
				t.Fatalf("get final workflow: %v", err)
			}

			if finalWf.Status != tt.wantFinalStatus {
				t.Errorf("status = %q, want %q", finalWf.Status, tt.wantFinalStatus)
			}

			if tt.wantFinalPhase != "" && finalWf.Phase != tt.wantFinalPhase {
				t.Errorf("phase = %q, want %q", finalWf.Phase, tt.wantFinalPhase)
			}
		})
	}
}

func TestAdvanceWorkflowInfraRetry(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(
		database,
		testLogger(),
		nil,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	ctx := t.Context()

	err := database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-retry",
		TicketID:     "TKT-RETRY",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	wf, _ := database.GetSwarmWorkflow(ctx, "wf-retry")

	// First infra failure should retry (attempt < maxInfraRetries=2).
	result := &swarm.SessionResultData{
		Result:  swarm.ResultInfraFailure,
		Summary: "tmux crash",
	}
	mgr.advanceWorkflow(ctx, wf, result)

	updated, _ := database.GetSwarmWorkflow(ctx, "wf-retry")
	if updated.Status != swarm.StatusRunning {
		t.Errorf("status after first infra failure = %q, want running", updated.Status)
	}
	if updated.Attempt != 2 {
		t.Errorf("attempt after first infra failure = %d, want 2", updated.Attempt)
	}
}

func TestAdvanceWorkflowContextLimit(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(
		database,
		testLogger(),
		nil,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	ctx := t.Context()

	err := database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-ctx",
		TicketID:     "TKT-CTX",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseImplement,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	wf, _ := database.GetSwarmWorkflow(ctx, "wf-ctx")
	result := &swarm.SessionResultData{
		Result:  swarm.ResultContextLimit,
		Summary: "hit context limit",
	}
	mgr.advanceWorkflow(ctx, wf, result)

	updated, _ := database.GetSwarmWorkflow(ctx, "wf-ctx")
	if updated.Phase != swarm.PhaseImplement {
		t.Errorf(
			"phase = %q, want %q (should stay same)",
			updated.Phase,
			swarm.PhaseImplement,
		)
	}
	if updated.Attempt != 1 {
		t.Errorf("attempt = %d, want 1 (no increment for context_limit)", updated.Attempt)
	}
}

func TestCaptureLearningsRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		phase        swarm.Phase
		result       swarm.SessionResult
		wantCategory string
		wantCount    int
	}{
		{
			name:         "plan_review_logic_failure",
			phase:        swarm.PhasePlanReview,
			result:       swarm.ResultLogicFailure,
			wantCategory: "plan_issue",
			wantCount:    1,
		},
		{
			name:         "verify_logic_failure",
			phase:        swarm.PhaseVerify,
			result:       swarm.ResultLogicFailure,
			wantCategory: "code_bug",
			wantCount:    1,
		},
		{
			name:         "infra_failure",
			phase:        swarm.PhaseResearch,
			result:       swarm.ResultInfraFailure,
			wantCategory: "post_mortem",
			wantCount:    1,
		},
		{
			name:      "success_no_learning",
			phase:     swarm.PhaseResearch,
			result:    swarm.ResultSuccess,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			database := newManagerTestDB(t)
			mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

			ticketID := "TKT-" + tt.name
			wfID := "wf-" + tt.name
			sessID := "sess-" + tt.name

			// Insert prerequisite records.
			_ = database.CreateSwarmWorkflow(t.Context(), sqlc.CreateSwarmWorkflowParams{
				ID:           wfID,
				TicketID:     ticketID,
				WorkflowType: swarm.WorkflowTypeCode,
				Phase:        tt.phase,
				Status:       swarm.StatusRunning,
				Attempt:      1,
			})
			_ = database.CreateSwarmSession(t.Context(), sqlc.CreateSwarmSessionParams{
				ID:          sessID,
				WorkflowID:  wfID,
				SessionName: "cm-swarm-test",
				Skill:       "test-skill",
				Phase:       tt.phase,
			})

			wf := sqlc.SwarmWorkflow{ID: wfID, TicketID: ticketID}
			session := sqlc.SwarmSession{ID: sessID, Phase: tt.phase}
			result := &swarm.SessionResultData{Result: tt.result, Summary: "test detail"}

			mgr.captureLearnings(t.Context(), wf, session, result)

			// Query learnings.
			rawDB := database.SQLDB()
			rows, err := rawDB.QueryContext(t.Context(),
				`SELECT category FROM swarm_learnings WHERE ticket_id = ?`, ticketID)
			if err != nil {
				t.Fatalf("query learnings: %v", err)
			}
			defer func() { _ = rows.Close() }()

			var categories []string
			for rows.Next() {
				var cat string
				if err := rows.Scan(&cat); err != nil {
					t.Fatalf("scan: %v", err)
				}
				categories = append(categories, cat)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows err: %v", err)
			}

			if len(categories) != tt.wantCount {
				t.Errorf("learning count = %d, want %d", len(categories), tt.wantCount)
			}

			if tt.wantCount > 0 && categories[0] != tt.wantCategory {
				t.Errorf("category = %q, want %q", categories[0], tt.wantCategory)
			}
		})
	}
}

func TestResultFilePath(t *testing.T) {
	t.Parallel()

	path := ResultFilePath("abc123")
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}

	dir := filepath.Dir(path)
	want := filepath.Clean(os.TempDir())
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
}

func TestSessionName(t *testing.T) {
	t.Parallel()

	name := SessionName("CM-123", swarm.PhaseResearch)
	if name != "cm-swarm-CM-123-research" {
		t.Errorf("SessionName = %q, want %q", name, "cm-swarm-CM-123-research")
	}
}

func TestBuildEnvLearningContextWritten(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)

	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	// Seed a learning so getLearningContext returns content.
	_ = mgr.capturePlanIssue(
		t.Context(),
		"",
		"",
		"CM-LC",
		"Test learning for context",
	)

	wf := sqlc.SwarmWorkflow{
		ID:       "wf-lc",
		TicketID: "CM-LC",
		Phase:    swarm.PhasePlanReview,
		Attempt:  1,
	}

	env, cleanup := mgr.buildEnv(t.Context(), wf, "sess-lc")
	defer cleanup()

	lcPath := env["CM_SWARM_LEARNING_CONTEXT_PATH"]
	if lcPath == "" {
		t.Fatal("CM_SWARM_LEARNING_CONTEXT_PATH not set")
	}

	content, err := os.ReadFile(lcPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read learning context: %v", err)
	}

	if len(content) == 0 {
		t.Error("learning context file is empty")
	}

	// Cleanup should remove the file.
	cleanup()
	if _, err := os.Stat(lcPath); !os.IsNotExist(err) {
		t.Error("cleanup did not remove learning context file")
	}
}

func TestBuildEnvHandoffResolution(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Create a handoff directory with a file for our ticket.
	handoffDir := filepath.Join(baseDir, "thoughts", "swarm", "handoffs-research")
	if err := os.MkdirAll(handoffDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	handoffFile := filepath.Join(handoffDir, "2026-02-28_15-00-00_CM-HO_findings.md")
	if err := os.WriteFile(handoffFile, []byte("# Handoff"), 0o600); err != nil {
		t.Fatalf("write handoff: %v", err)
	}

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, baseDir, t.TempDir(), "")

	wf := sqlc.SwarmWorkflow{
		ID:       "wf-ho",
		TicketID: "CM-HO",
		Phase:    swarm.PhaseCodePlan,
		Attempt:  1,
	}

	env, cleanup := mgr.buildEnv(t.Context(), wf, "sess-ho")
	defer cleanup()

	if env["CM_SWARM_HANDOFF_PATH"] != handoffFile {
		t.Errorf(
			"CM_SWARM_HANDOFF_PATH = %q, want %q",
			env["CM_SWARM_HANDOFF_PATH"],
			handoffFile,
		)
	}
}

func TestLoadConfigDefault(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	config := mgr.loadConfig(t.Context())

	if config.MaxSessions != swarm.DefaultConfig.MaxSessions {
		t.Errorf(
			"MaxSessions = %d, want %d",
			config.MaxSessions,
			swarm.DefaultConfig.MaxSessions,
		)
	}
}

func TestLoadConfigFromDB(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	// Update config in DB.
	err := database.UpdateSwarmConfig(
		t.Context(),
		`{"maxSessions": 8, "maxPlanRevisions": 5}`,
	)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}

	config := mgr.loadConfig(t.Context())

	if config.MaxSessions != 8 {
		t.Errorf("MaxSessions = %d, want 8", config.MaxSessions)
	}
	if config.MaxPlanRevisions != 5 {
		t.Errorf("MaxPlanRevisions = %d, want 5", config.MaxPlanRevisions)
	}
}

func TestEmitEvent(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()
	mgr.emitEvent(
		ctx,
		"wf-1",
		"sess-1",
		"TKT-1",
		swarm.EventWorkflowStarted,
		swarm.PhaseResearch,
		"test detail",
	)

	evts, err := database.ListRecentSwarmEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}

	evt := evts[0]
	if evt.EventType != swarm.EventWorkflowStarted {
		t.Errorf("event_type = %q, want %q", evt.EventType, swarm.EventWorkflowStarted)
	}
	if evt.TicketID != "TKT-1" {
		t.Errorf("ticket_id = %q, want TKT-1", evt.TicketID)
	}
}

func TestAdvanceWorkflowAttemptIncrement(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	err := database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-attempt",
		TicketID:     "TKT-ATTEMPT",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhasePlanReview,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	wf, _ := database.GetSwarmWorkflow(ctx, "wf-attempt")

	// Logic failure in plan_review should loop back to code_plan with attempt++.
	result := &swarm.SessionResultData{
		Result:  swarm.ResultLogicFailure,
		Summary: "needs revision",
	}
	mgr.advanceWorkflow(ctx, wf, result)

	updated, _ := database.GetSwarmWorkflow(ctx, "wf-attempt")
	if updated.Phase != swarm.PhaseCodePlan {
		t.Errorf("phase = %q, want %q", updated.Phase, swarm.PhaseCodePlan)
	}
	if updated.Attempt != 2 {
		t.Errorf("attempt = %d, want 2", updated.Attempt)
	}
}

func TestMultipleAdvancesConcurrent(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(
		database,
		testLogger(),
		nil,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	ctx := t.Context()

	for i := range 5 {
		err := database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
			ID:           "wf-conc-" + strconv.Itoa(i),
			TicketID:     "TKT-CONC-" + strconv.Itoa(i),
			WorkflowType: swarm.WorkflowTypeResearch,
			Phase:        swarm.PhaseResearch,
			Status:       swarm.StatusRunning,
			Attempt:      1,
		})
		if err != nil {
			t.Fatalf("create workflow %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	for i := range 5 {
		go func(idx int) {
			wfID := "wf-conc-" + strconv.Itoa(idx)
			wf, _ := database.GetSwarmWorkflow(ctx, wfID)
			result := &swarm.SessionResultData{
				Result:  swarm.ResultSuccess,
				Summary: "done",
			}
			mgr.advanceWorkflow(ctx, wf, result)
			done <- struct{}{}
		}(i)
	}

	for range 5 {
		<-done
	}

	for i := range 5 {
		wf, _ := database.GetSwarmWorkflow(ctx, "wf-conc-"+strconv.Itoa(i))
		if wf.Status != swarm.StatusComplete {
			t.Errorf("workflow %d status = %q, want completed", i, wf.Status)
		}
	}
}

func TestStartWorkflowInvalidType(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	_, err := mgr.StartWorkflow(t.Context(), "TKT-1", "invalid_type", "", "")
	if err == nil {
		t.Fatal("expected error for invalid workflow type")
	}
}

func TestStartWorkflowCreatesRecords(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()
	mgr := NewManager(
		database,
		testLogger(),
		bus,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	ctx := t.Context()

	// StartWorkflow will fail at spawnSession (no tmux), but still returns wfID.
	wfID, err := mgr.StartWorkflow(
		ctx,
		"CM-NEW",
		swarm.WorkflowTypeResearch,
		"https://linear.app/CM-NEW",
		"",
	)
	if wfID == "" {
		t.Fatal("expected workflow ID even on spawn failure")
	}
	// Error is expected from spawnSession, but wfID should be returned.
	if err == nil {
		t.Log("StartWorkflow succeeded (tmux available) — unexpected in test, but ok")
	}

	// Verify workflow record was created.
	wf, wfErr := database.GetSwarmWorkflow(ctx, wfID)
	if wfErr != nil {
		t.Fatalf("workflow not found in DB: %v", wfErr)
	}
	if wf.TicketID != "CM-NEW" {
		t.Errorf("ticket_id = %q, want CM-NEW", wf.TicketID)
	}
	if wf.WorkflowType != swarm.WorkflowTypeResearch {
		t.Errorf("workflow_type = %q, want research", wf.WorkflowType)
	}
	if wf.Phase != swarm.PhaseResearch {
		t.Errorf("phase = %q, want research", wf.Phase)
	}
	if wf.Status != swarm.StatusRunning {
		t.Errorf("status = %q, want running", wf.Status)
	}

	// Verify ticket record was upserted.
	ticket, ticketErr := database.GetSwarmTicket(ctx, "CM-NEW")
	if ticketErr != nil {
		t.Fatalf("ticket not found in DB: %v", ticketErr)
	}
	if ticket.Url != "https://linear.app/CM-NEW" {
		t.Errorf("ticket url = %q, want https://linear.app/CM-NEW", ticket.Url)
	}

	// Verify workflow_started event was emitted.
	evts, _ := database.ListRecentSwarmEvents(ctx, 10)
	var foundStarted bool
	for _, e := range evts {
		if e.EventType == swarm.EventWorkflowStarted {
			foundStarted = true
		}
	}
	if !foundStarted {
		t.Error("expected workflow_started event")
	}
}

func TestCancelWorkflowStatusGuard(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// Create a completed workflow.
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-done",
		TicketID:     "TKT-DONE",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseDone,
		Status:       swarm.StatusComplete,
		Attempt:      1,
	})

	err := mgr.CancelWorkflow(ctx, "wf-done")
	if err == nil {
		t.Fatal("expected error when canceling completed workflow")
	}
}

func TestCancelWorkflowActive(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-cancel",
		TicketID:     "TKT-CANCEL",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	err := mgr.CancelWorkflow(ctx, "wf-cancel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wf, _ := database.GetSwarmWorkflow(ctx, "wf-cancel")
	if wf.Status != swarm.StatusCanceled {
		t.Errorf("status = %q, want canceled", wf.Status)
	}

	// Verify cancellation event.
	evts, _ := database.ListRecentSwarmEvents(ctx, 10)
	var foundCanceled bool
	for _, e := range evts {
		if e.EventType == swarm.EventWorkflowCanceled {
			foundCanceled = true
		}
	}
	if !foundCanceled {
		t.Error("expected workflow_canceled event")
	}
}

func TestCancelWorkflowNotFound(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	err := mgr.CancelWorkflow(t.Context(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetWorkflowNotFound(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	_, _, err := mgr.GetWorkflow(t.Context(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetWorkflowNoSession(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-nosess",
		TicketID:     "TKT-NOSESS",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	wf, session, err := mgr.GetWorkflow(ctx, "wf-nosess")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.ID != "wf-nosess" {
		t.Errorf("workflow ID = %q, want wf-nosess", wf.ID)
	}
	if session != nil {
		t.Error("expected nil session when none exist")
	}
}

func TestGetWorkflowWithSession(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-withsess",
		TicketID:     "TKT-WITHSESS",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	_ = database.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID:          "sess-1",
		WorkflowID:  "wf-withsess",
		SessionName: "cm-swarm-TKT-WITHSESS-research",
		Skill:       "swarm-research",
		Phase:       swarm.PhaseResearch,
	})

	wf, session, err := mgr.GetWorkflow(ctx, "wf-withsess")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.ID != "wf-withsess" {
		t.Errorf("workflow ID = %q, want wf-withsess", wf.ID)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.ID != "sess-1" {
		t.Errorf("session ID = %q, want sess-1", session.ID)
	}
}

func TestHandleSessionCompleteDoubleFireGuard(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-double",
		TicketID:     "TKT-DOUBLE",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	_ = database.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID:          "sess-double",
		WorkflowID:  "wf-double",
		SessionName: "cm-swarm-TKT-DOUBLE-research",
		Skill:       "swarm-research",
		Phase:       swarm.PhaseResearch,
	})

	// Write a result file so first completion has data.
	resultPath := ResultFilePath("sess-double")
	_ = os.WriteFile(resultPath, []byte("RESULT: success\nSUMMARY: done\n"), 0o600)
	t.Cleanup(func() { _ = os.Remove(resultPath) })

	// First call should complete the session.
	mgr.handleSessionComplete(ctx, "sess-double")

	session, _ := database.GetSwarmSession(ctx, "sess-double")
	if !session.CompletedAt.Valid {
		t.Fatal("expected session to be completed after first call")
	}

	// Count events before second call.
	eventsBefore, _ := database.ListRecentSwarmEvents(ctx, 100)
	countBefore := len(eventsBefore)

	// Second call should be a no-op (double-fire guard).
	mgr.handleSessionComplete(ctx, "sess-double")

	eventsAfter, _ := database.ListRecentSwarmEvents(ctx, 100)
	if len(eventsAfter) != countBefore {
		t.Errorf(
			"double-fire guard failed: event count changed from %d to %d",
			countBefore,
			len(eventsAfter),
		)
	}
}

func TestEmitEventTicketIDResolution(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           "wf-resolve",
		TicketID:     "TKT-RESOLVE",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	// Emit with empty ticketID — should resolve from workflow.
	mgr.emitEvent(
		ctx,
		"wf-resolve",
		"",
		"",
		swarm.EventPhaseStarted,
		swarm.PhaseResearch,
		"",
	)

	evts, _ := database.ListRecentSwarmEvents(ctx, 10)
	if len(evts) == 0 {
		t.Fatal("expected event")
	}
	if evts[0].TicketID != "TKT-RESOLVE" {
		t.Errorf("ticket_id = %q, want TKT-RESOLVE", evts[0].TicketID)
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}),
	)
}
