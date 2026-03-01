package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"creative-mode/harness/internal/swarm"
)

// Health status constants.
const (
	HealthStatusHealthy   = "healthy"
	HealthStatusDegraded  = "degraded"
	HealthStatusUnhealthy = "unhealthy"

	stallCheckMinutes   = 45
	recentCompletionCap = 10
)

// SwarmHealth represents the overall health of the swarm system.
type SwarmHealth struct {
	Status            string               `json:"status"`
	Capacity          CapacityInfo         `json:"capacity"`
	ActiveWorkflows   []ActiveWorkflowInfo `json:"active_workflows"`   //nolint:tagliatelle // API field name
	RecentCompletions []CompletionInfo     `json:"recent_completions"` //nolint:tagliatelle // API field name
	MetricsSummary    MetricsSummaryInfo   `json:"metrics_summary"`    //nolint:tagliatelle // API field name
}

// CapacityInfo shows current vs max session capacity.
type CapacityInfo struct {
	ActiveSessions int `json:"active_sessions"` //nolint:tagliatelle // API field name
	MaxSessions    int `json:"max_sessions"`    //nolint:tagliatelle // API field name
}

// ActiveWorkflowInfo summarizes an in-progress workflow.
type ActiveWorkflowInfo struct {
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // API field name
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // API field name
	Phase      string `json:"phase"`
	Attempt    int64  `json:"attempt"`
	StartedAt  string `json:"started_at"` //nolint:tagliatelle // API field name
	Stalled    bool   `json:"stalled"`
}

// CompletionInfo summarizes a recently completed workflow.
type CompletionInfo struct {
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // API field name
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // API field name
	Status     string `json:"status"`
	UpdatedAt  string `json:"updated_at"` //nolint:tagliatelle // API field name
}

// MetricsSummaryInfo shows key aggregate metrics.
type MetricsSummaryInfo struct {
	CompletionRate float64 `json:"completion_rate"` //nolint:tagliatelle // API field name
	TotalWorkflows int     `json:"total_workflows"` //nolint:tagliatelle // API field name
	TotalSessions  int     `json:"total_sessions"`  //nolint:tagliatelle // API field name
}

// GetHealth returns the current health status of the swarm system.
func (m *Manager) GetHealth(ctx context.Context) (*SwarmHealth, error) {
	config := m.loadConfig(ctx)
	rawDB := m.db.SQLDB()

	health := &SwarmHealth{}

	// Active sessions count.
	activeCount, err := m.db.CountActiveSwarmSessions(ctx)
	if err != nil {
		activeCount = 0
	}

	health.Capacity = CapacityInfo{
		ActiveSessions: int(activeCount),
		MaxSessions:    config.MaxSessions,
	}

	// Active workflows with stall detection.
	activeWfs, err := queryActiveWorkflows(ctx, rawDB)
	if err != nil {
		return nil, err
	}
	health.ActiveWorkflows = activeWfs

	// Recent completions (last 24h).
	completions, err := queryRecentCompletions(ctx, rawDB)
	if err != nil {
		return nil, err
	}
	health.RecentCompletions = completions

	// Metrics summary from 24h window.
	metrics, metricsErr := m.GetMetrics(ctx, "24h")
	if metricsErr == nil {
		health.MetricsSummary = MetricsSummaryInfo{
			CompletionRate: metrics.Workflows.CompletionRate,
			TotalWorkflows: metrics.Workflows.Total,
			TotalSessions:  metrics.Sessions.Total,
		}
	}

	// Determine status.
	health.Status = determineHealthStatus(health)

	return health, nil
}

// determineHealthStatus computes healthy/degraded/unhealthy.
func determineHealthStatus(h *SwarmHealth) string {
	// Check for stalled workflows.
	stalledCount := 0
	for _, wf := range h.ActiveWorkflows {
		if wf.Stalled {
			stalledCount++
		}
	}

	// At capacity = degraded.
	atCapacity := h.Capacity.ActiveSessions >= h.Capacity.MaxSessions

	// No workflows progressing + recent failures = unhealthy.
	hasActive := len(h.ActiveWorkflows) > 0
	allStalled := hasActive && stalledCount == len(h.ActiveWorkflows)

	if allStalled {
		return HealthStatusUnhealthy
	}

	if stalledCount > 0 || atCapacity {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

func queryActiveWorkflows(ctx context.Context, db *sql.DB) ([]ActiveWorkflowInfo, error) {
	query := `SELECT id, ticket_id, phase, attempt, created_at, updated_at
	FROM swarm_workflows
	WHERE status = ?
	ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query, string(swarm.StatusRunning))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	now := time.Now()
	var results []ActiveWorkflowInfo

	for rows.Next() {
		var wf ActiveWorkflowInfo
		var updatedAt string

		if err := rows.Scan(
			&wf.WorkflowID, &wf.TicketID, &wf.Phase, &wf.Attempt,
			&wf.StartedAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		// Detect stalls: no update for stallCheckMinutes.
		if t, parseErr := time.Parse("2006-01-02 15:04:05", updatedAt); parseErr == nil {
			wf.Stalled = now.Sub(t) > time.Duration(stallCheckMinutes)*time.Minute
		}

		results = append(results, wf)
	}

	return results, rows.Err()
}

//nolint:gosec // status values are typed constants, not user input
func queryRecentCompletions(ctx context.Context, db *sql.DB) ([]CompletionInfo, error) {
	query := fmt.Sprintf(`SELECT id, ticket_id, status, updated_at
	FROM swarm_workflows
	WHERE status IN ('%s', '%s', '%s')
	  AND updated_at >= datetime('now', '-24 hours')
	ORDER BY updated_at DESC
	LIMIT ?`,
		swarm.StatusComplete, swarm.StatusFailed, swarm.StatusCanceled)

	rows, err := db.QueryContext(ctx, query, recentCompletionCap)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []CompletionInfo

	for rows.Next() {
		var c CompletionInfo
		if err := rows.Scan(
			&c.WorkflowID,
			&c.TicketID,
			&c.Status,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		results = append(results, c)
	}

	return results, rows.Err()
}
