package swarm

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// swarmTestSchema contains the minimum CREATE TABLE statements needed for
// swarm_learnings (from migration 006, FK tables included for completeness).
const swarmTestSchema = `
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
`

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() {
		if cErr := db.Close(); cErr != nil {
			t.Logf("close db: %v", cErr)
		}
	})

	if _, err := db.ExecContext(t.Context(), swarmTestSchema); err != nil {
		t.Fatalf("exec schema: %v", err)
	}

	return db
}

// getLearningsByTicket is a test helper that queries all learnings for a ticket.
func getLearningsByTicket(t *testing.T, db *sql.DB, ticketID string) []learningRow {
	t.Helper()

	rows, err := db.QueryContext(t.Context(),
		`SELECT id, category, severity, title, content, referenced_count
		FROM swarm_learnings WHERE ticket_id = ? ORDER BY created_at DESC`, ticketID)
	if err != nil {
		t.Fatalf("query learnings: %v", err)
	}
	defer func() {
		if cErr := rows.Close(); cErr != nil {
			t.Logf("close rows: %v", cErr)
		}
	}()

	var result []learningRow

	for rows.Next() {
		var r learningRow
		if err := rows.Scan(
			&r.ID,
			&r.Category,
			&r.Severity,
			&r.Title,
			&r.Content,
			&r.ReferencedCount,
		); err != nil {
			t.Fatalf("scan learning: %v", err)
		}

		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	return result
}

func TestCapturePlanIssue(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	err := CapturePlanIssue(
		ctx,
		db,
		"",
		"",
		"TKT-1",
		"Plan missing error handling\nSecond line",
	)
	if err != nil {
		t.Fatalf("CapturePlanIssue: %v", err)
	}

	learnings := getLearningsByTicket(t, db, "TKT-1")
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d", len(learnings))
	}

	l := learnings[0]
	if l.Category != LearningPlanIssue {
		t.Errorf("category = %q, want %q", l.Category, LearningPlanIssue)
	}
	if l.Severity != SeverityInfo {
		t.Errorf("severity = %q, want %q", l.Severity, SeverityInfo)
	}
	if l.Title != "Plan missing error handling" {
		t.Errorf("title = %q, want first line", l.Title)
	}
}

func TestCaptureCodeBug(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	err := CaptureCodeBug(ctx, db, "", "", "TKT-2", "nil pointer in handler")
	if err != nil {
		t.Fatalf("CaptureCodeBug: %v", err)
	}

	learnings := getLearningsByTicket(t, db, "TKT-2")
	if len(learnings) != 1 {
		t.Fatalf("expected 1, got %d", len(learnings))
	}

	if learnings[0].Severity != SeverityWarning {
		t.Errorf("severity = %q, want %q", learnings[0].Severity, SeverityWarning)
	}
	if learnings[0].Category != LearningCodeBug {
		t.Errorf("category = %q, want %q", learnings[0].Category, LearningCodeBug)
	}
}

func TestCaptureTerminalFailure(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	err := CaptureTerminalFailure(
		ctx,
		db,
		"",
		"",
		"TKT-3",
		PhaseVerify,
		"build failed 3 times",
	)
	if err != nil {
		t.Fatalf("CaptureTerminalFailure: %v", err)
	}

	learnings := getLearningsByTicket(t, db, "TKT-3")
	if len(learnings) != 1 {
		t.Fatalf("expected 1, got %d", len(learnings))
	}

	l := learnings[0]
	if l.Severity != SeverityCritical {
		t.Errorf("severity = %q, want %q", l.Severity, SeverityCritical)
	}
	if l.Category != LearningPostMortem {
		t.Errorf("category = %q, want %q", l.Category, LearningPostMortem)
	}
	if l.Title != "Terminal failure in verify" {
		t.Errorf("title = %q, want terminal failure message", l.Title)
	}
}

func TestCaptureSuccessPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hadRetries bool
		wantSev    LearningSeverity
		wantTitle  string
	}{
		{"clean", false, SeverityInfo, "Clean success"},
		{"retries", true, SeverityWarning, "Success after retries"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			ctx := t.Context()

			err := CaptureSuccessPattern(ctx, db, "", "TKT-4-"+tt.name, tt.hadRetries)
			if err != nil {
				t.Fatalf("CaptureSuccessPattern: %v", err)
			}

			learnings := getLearningsByTicket(t, db, "TKT-4-"+tt.name)
			if len(learnings) != 1 {
				t.Fatalf("expected 1, got %d", len(learnings))
			}

			l := learnings[0]
			if l.Severity != tt.wantSev {
				t.Errorf("severity = %q, want %q", l.Severity, tt.wantSev)
			}
			if l.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", l.Title, tt.wantTitle)
			}
		})
	}
}

func TestGetLearningContext(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	// Populate learnings for different phases and severities
	_ = CapturePlanIssue(ctx, db, "", "", "TKT-5", "Plan issue detail")
	_ = CaptureCodeBug(ctx, db, "", "", "TKT-5", "Code bug detail")
	_ = CaptureTerminalFailure(ctx, db, "", "", "TKT-5", PhaseVerify, "Critical failure")

	result, err := GetLearningContext(ctx, db, "TKT-5", PhasePlanReview)
	if err != nil {
		t.Fatalf("GetLearningContext: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty context")
	}

	if !strings.Contains(result, "Phase Learnings") {
		t.Error("missing Phase Learnings section")
	}
	if !strings.Contains(result, "Critical Learnings") {
		t.Error("missing Critical Learnings section")
	}
	if !strings.Contains(result, "Ticket History") {
		t.Error("missing Ticket History section")
	}
}

func TestGetLearningContextEmpty(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	result, err := GetLearningContext(ctx, db, "TKT-NONE", PhaseResearch)
	if err != nil {
		t.Fatalf("GetLearningContext: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGetLearningContextIncrementsReferences(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := t.Context()

	_ = CapturePlanIssue(ctx, db, "", "", "TKT-REF", "Reference test")

	// Get the learning
	learnings := getLearningsByTicket(t, db, "TKT-REF")
	if len(learnings) == 0 {
		t.Fatal("no learnings found")
	}

	initialCount := learnings[0].ReferencedCount

	// Call GetLearningContext which should increment references
	_, err := GetLearningContext(ctx, db, "TKT-REF", PhasePlanReview)
	if err != nil {
		t.Fatalf("GetLearningContext: %v", err)
	}

	// Verify reference count was bumped
	updated := getLearningsByTicket(t, db, "TKT-REF")
	if len(updated) == 0 {
		t.Fatal("learning not found after update")
	}

	if updated[0].ReferencedCount <= initialCount {
		t.Errorf(
			"referenced_count = %d, want > %d",
			updated[0].ReferencedCount,
			initialCount,
		)
	}
}

func TestExtractTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single_line", "Hello world", "Hello world"},
		{"multiline", "First line\nSecond line\nThird", "First line"},
		{"long_string", strings.Repeat("a", 150), strings.Repeat("a", maxTitleLen)},
		{
			"long_multiline",
			strings.Repeat("x", 150) + "\nsecond",
			strings.Repeat("x", maxTitleLen),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := extractTitle(tt.input)
			if got != tt.want {
				t.Errorf("extractTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
