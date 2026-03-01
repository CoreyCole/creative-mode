package swarm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	topPhaseLearningsLimit    = 5
	topCriticalLearningsLimit = 3
	maxTitleLen               = 100
)

// DBTX is the minimal database interface needed for learning operations.
// Satisfied by *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SQL queries for learning operations.
const (
	insertLearningSQL = `INSERT INTO swarm_learnings
		(id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	listByPhaseSQL = `SELECT id, category, severity, title, content, referenced_count
		FROM swarm_learnings
		WHERE phase = ? AND archived_at IS NULL
		ORDER BY relevance_score DESC LIMIT ?`

	listCriticalSQL = `SELECT id, category, severity, title, content, referenced_count
		FROM swarm_learnings
		WHERE severity = 'critical' AND archived_at IS NULL
		ORDER BY relevance_score DESC LIMIT ?`

	listByTicketSQL = `SELECT id, category, severity, title, content, referenced_count
		FROM swarm_learnings
		WHERE ticket_id = ? AND archived_at IS NULL
		ORDER BY created_at DESC`

	incrementRefSQL = `UPDATE swarm_learnings
		SET referenced_count = referenced_count + 1, relevance_score = relevance_score + 0.1, updated_at = datetime('now')
		WHERE id = ?`
)

// learningRow holds the fields we read from swarm_learnings queries.
type learningRow struct {
	ID              string
	Category        LearningCategory
	Severity        LearningSeverity
	Title           string
	Content         string
	ReferencedCount int64
}

func createLearning(
	ctx context.Context,
	db DBTX,
	workflowID, sessionID, ticketID string,
	category LearningCategory,
	phase Phase,
	severity LearningSeverity,
	title, content string,
) error {
	id := uuid.New().String()[:8]

	_, err := db.ExecContext(ctx, insertLearningSQL,
		id,
		toNullString(workflowID),
		toNullString(sessionID),
		ticketID,
		string(category),
		toNullString(string(phase)),
		string(severity),
		title,
		content,
	)
	if err != nil {
		return fmt.Errorf("insert learning: %w", err)
	}

	return nil
}

// CapturePlanIssue records a plan-review issue as a learning.
func CapturePlanIssue(
	ctx context.Context,
	db DBTX,
	workflowID, sessionID, ticketID, detail string,
) error {
	return createLearning(ctx, db, workflowID, sessionID, ticketID,
		LearningPlanIssue, PhasePlanReview, SeverityInfo, extractTitle(detail), detail)
}

// CaptureCodeBug records a code bug found during verification.
func CaptureCodeBug(
	ctx context.Context,
	db DBTX,
	workflowID, sessionID, ticketID, detail string,
) error {
	return createLearning(ctx, db, workflowID, sessionID, ticketID,
		LearningCodeBug, PhaseVerify, SeverityWarning, extractTitle(detail), detail)
}

// CaptureTerminalFailure records a post-mortem for a terminal workflow failure.
func CaptureTerminalFailure(
	ctx context.Context,
	db DBTX,
	workflowID, sessionID, ticketID string,
	phase Phase,
	detail string,
) error {
	return createLearning(ctx, db, workflowID, sessionID, ticketID,
		LearningPostMortem, phase, SeverityCritical,
		fmt.Sprintf("Terminal failure in %s", phase), detail)
}

// CaptureSuccessPattern records a successful workflow pattern.
func CaptureSuccessPattern(
	ctx context.Context,
	db DBTX,
	workflowID, ticketID string,
	hadRetries bool,
) error {
	severity := SeverityInfo
	title := "Clean success"

	if hadRetries {
		severity = SeverityWarning
		title = "Success after retries"
	}

	return createLearning(ctx, db, workflowID, "", ticketID,
		LearningPattern, "", severity, title, title)
}

// GetLearningContext builds a markdown context string from relevant learnings
// for a given ticket and phase. Returns "" if no learnings are found.
func GetLearningContext(
	ctx context.Context,
	db DBTX,
	ticketID string,
	phase Phase,
) (string, error) { //nolint:cyclop // assembles 3 query results into markdown
	phaseLearnings, err := queryLearnings(
		ctx,
		db,
		listByPhaseSQL,
		string(phase),
		topPhaseLearningsLimit,
	)
	if err != nil {
		return "", fmt.Errorf("list phase learnings: %w", err)
	}

	criticalLearnings, err := queryLearnings(
		ctx,
		db,
		listCriticalSQL,
		topCriticalLearningsLimit,
	)
	if err != nil {
		return "", fmt.Errorf("list critical learnings: %w", err)
	}

	ticketLearnings, err := queryLearnings(ctx, db, listByTicketSQL, ticketID)
	if err != nil {
		return "", fmt.Errorf("list ticket learnings: %w", err)
	}

	if len(phaseLearnings) == 0 && len(criticalLearnings) == 0 &&
		len(ticketLearnings) == 0 {
		return "", nil
	}

	// Deduplicate and track IDs for reference counting
	seen := make(map[string]bool)
	var allIDs []string

	collectIDs := func(rows []learningRow) {
		for _, r := range rows {
			if !seen[r.ID] {
				seen[r.ID] = true
				allIDs = append(allIDs, r.ID)
			}
		}
	}

	collectIDs(phaseLearnings)
	collectIDs(criticalLearnings)
	collectIDs(ticketLearnings)

	// Best-effort reference counting
	for _, id := range allIDs {
		_, _ = db.ExecContext(ctx, incrementRefSQL, id)
	}

	var b strings.Builder

	if len(phaseLearnings) > 0 {
		fmt.Fprintf(&b, "## Phase Learnings (%s)\n\n", phase)
		for _, l := range phaseLearnings {
			b.WriteString(formatLearning(l))
		}
	}

	if len(criticalLearnings) > 0 {
		b.WriteString("## Critical Learnings\n\n")
		for _, l := range criticalLearnings {
			b.WriteString(formatLearning(l))
		}
	}

	if len(ticketLearnings) > 0 {
		b.WriteString("## Ticket History\n\n")
		for _, l := range ticketLearnings {
			b.WriteString(formatLearning(l))
		}
	}

	return b.String(), nil
}

func queryLearnings(
	ctx context.Context,
	db DBTX,
	query string,
	args ...any,
) (_ []learningRow, retErr error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cErr := rows.Close(); cErr != nil && retErr == nil {
			retErr = cErr
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
			return nil, err
		}

		result = append(result, r)
	}

	return result, rows.Err()
}

func extractTitle(s string) string {
	if s == "" {
		return ""
	}

	line, _, _ := strings.Cut(s, "\n")
	if len(line) > maxTitleLen {
		return line[:maxTitleLen]
	}

	return line
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: s, Valid: true}
}

func formatLearning(l learningRow) string {
	return fmt.Sprintf(
		"- **[%s] %s** (%s): %s\n",
		l.Severity,
		l.Title,
		l.Category,
		l.Content,
	)
}
