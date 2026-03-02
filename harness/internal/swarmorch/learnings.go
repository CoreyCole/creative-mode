package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarm"
)

const (
	topPhaseLearningsLimit    = 5
	topCriticalLearningsLimit = 3
	maxTitleLen               = 100
)

// createLearning inserts a new learning via sqlc.
func (m *Manager) createLearning(
	ctx context.Context,
	workflowID, sessionID, ticketID string,
	category swarm.LearningCategory,
	phase swarm.Phase,
	severity swarm.LearningSeverity,
	title, content string,
) error {
	return m.db.CreateSwarmLearning(ctx, sqlc.CreateSwarmLearningParams{
		ID:               uuid.New().String()[:8],
		SourceWorkflowID: toNullString(workflowID),
		SourceSessionID:  toNullString(sessionID),
		TicketID:         ticketID,
		Category:         category,
		Phase:            toNullString(string(phase)),
		Severity:         severity,
		Title:            title,
		Content:          content,
		Tags:             deriveLearningTags(phase, category),
	})
}

// capturePlanIssue records a plan-review issue as a learning.
func (m *Manager) capturePlanIssue(
	ctx context.Context,
	workflowID, sessionID, ticketID, detail string,
) error {
	return m.createLearning(ctx, workflowID, sessionID, ticketID,
		swarm.LearningPlanIssue, swarm.PhasePlanReview, swarm.SeverityInfo,
		extractTitle(detail), detail)
}

// captureCodeBug records a code bug found during verification.
func (m *Manager) captureCodeBug(
	ctx context.Context,
	workflowID, sessionID, ticketID, detail string,
) error {
	return m.createLearning(ctx, workflowID, sessionID, ticketID,
		swarm.LearningCodeBug, swarm.PhaseVerify, swarm.SeverityWarning,
		extractTitle(detail), detail)
}

// captureTerminalFailure records a post-mortem for a terminal workflow failure
// and writes a retrospective markdown file.
func (m *Manager) captureTerminalFailure(
	ctx context.Context,
	workflowID, sessionID, ticketID string,
	phase swarm.Phase,
	detail string,
) error {
	err := m.createLearning(ctx, workflowID, sessionID, ticketID,
		swarm.LearningPostMortem, phase, swarm.SeverityCritical,
		fmt.Sprintf("Terminal failure in %s", phase), detail)

	// Best-effort retrospective file.
	if retroErr := m.writeRetrospective(
		workflowID,
		ticketID,
		phase,
		detail,
	); retroErr != nil {
		m.logger.Warn("failed to write retrospective", "error", retroErr)
	}

	return err
}

// writeRetrospective writes a markdown retrospective file for a terminal failure.
func (m *Manager) writeRetrospective(
	workflowID, ticketID string,
	phase swarm.Phase,
	detail string,
) error {
	retroDir := filepath.Join(m.baseDir, "thoughts", "swarm", "retrospectives")
	if err := os.MkdirAll(retroDir, 0o750); err != nil {
		return fmt.Errorf("create retrospectives dir: %w", err)
	}

	now := time.Now()
	filename := fmt.Sprintf("%s-%s-%s.md", now.Format("2006-01-02"), ticketID, phase)
	retroPath := filepath.Join(retroDir, filename)

	content := fmt.Sprintf(`# Retrospective: %s — %s

**Ticket**: %s
**Phase**: %s
**Workflow**: %s
**Timestamp**: %s

## Failure Detail

%s
`, ticketID, phase, ticketID, phase, workflowID, now.Format(time.RFC3339), detail)

	return os.WriteFile(retroPath, []byte(content), 0o600)
}

// captureSuccessPattern records a successful workflow pattern.
func (m *Manager) captureSuccessPattern(
	ctx context.Context,
	workflowID, ticketID string,
	hadRetries bool,
) error {
	severity := swarm.SeverityInfo
	title := "Clean success"

	if hadRetries {
		severity = swarm.SeverityWarning
		title = "Success after retries"
	}

	return m.createLearning(ctx, workflowID, "", ticketID,
		swarm.LearningPattern, "", severity, title, title)
}

// getLearningContext builds a markdown context string from relevant learnings
// for a given ticket and phase. Returns "" if no learnings are found.
func (m *Manager) getLearningContext(
	ctx context.Context,
	ticketID string,
	phase swarm.Phase,
) (string, error) {
	phaseLearnings, err := m.db.ListTopSwarmLearningsByPhase(ctx,
		sqlc.ListTopSwarmLearningsByPhaseParams{
			Phase: toNullString(string(phase)),
			Limit: topPhaseLearningsLimit,
		})
	if err != nil {
		return "", fmt.Errorf("list phase learnings: %w", err)
	}

	criticalLearnings, err := m.db.ListTopCriticalSwarmLearnings(ctx,
		topCriticalLearningsLimit)
	if err != nil {
		return "", fmt.Errorf("list critical learnings: %w", err)
	}

	ticketLearnings, err := m.db.ListSwarmLearningsByTicket(ctx, ticketID)
	if err != nil {
		return "", fmt.Errorf("list ticket learnings: %w", err)
	}

	if len(phaseLearnings) == 0 && len(criticalLearnings) == 0 &&
		len(ticketLearnings) == 0 {
		return "", nil
	}

	// Deduplicate and track IDs for reference counting.
	seen := make(map[string]bool)
	var allIDs []string

	collectIDs := func(rows []sqlc.SwarmLearning) {
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

	// Best-effort reference counting.
	for _, id := range allIDs {
		_ = m.db.IncrementSwarmLearningReference(ctx, id)
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

// minDecayInterval is the minimum time between decay runs to prevent double-execution.
const minDecayInterval = 30 * time.Minute

// decayLearningRelevance applies severity-weighted decay to all active learning
// relevance scores, then archives old low-relevance entries.
// Skips execution if called within minDecayInterval of the last run.
func (m *Manager) decayLearningRelevance(ctx context.Context) error {
	if !m.lastDecayAt.IsZero() && time.Since(m.lastDecayAt) < minDecayInterval {
		return nil
	}

	if err := m.db.DecaySwarmLearningRelevance(ctx); err != nil {
		return fmt.Errorf("decay relevance: %w", err)
	}

	if err := m.db.ArchiveOldLowRelevanceLearnings(ctx); err != nil {
		return fmt.Errorf("archive old learnings: %w", err)
	}

	m.lastDecayAt = time.Now()

	return nil
}

func formatLearning(l sqlc.SwarmLearning) string {
	return fmt.Sprintf(
		"- **[%s] %s** (%s): %s\n",
		l.Severity,
		l.Title,
		l.Category,
		l.Content,
	)
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

// deriveLearningTags builds a comma-separated tag string from phase and category.
func deriveLearningTags(
	phase swarm.Phase,
	category swarm.LearningCategory,
) sql.NullString {
	var tags []string
	if phase != "" {
		tags = append(tags, string(phase))
	}
	tags = append(tags, string(category))
	return toNullString(strings.Join(tags, ","))
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: s, Valid: true}
}
