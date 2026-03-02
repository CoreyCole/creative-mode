package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"creative-mode/harness/internal/swarm"
)

// Metrics constants.
const (
	metricsCacheTTL = 60 * time.Second
	DefaultPeriod   = "24h"
)

// SwarmMetrics holds aggregate metrics for the swarm system.
type SwarmMetrics struct {
	Period    string                  `json:"period"`
	Workflows WorkflowMetrics         `json:"workflows"`
	Phases    map[string]PhaseMetrics `json:"phases"`
	Retries   RetryMetrics            `json:"retries"`
	Learnings LearningMetrics         `json:"learnings"`
	Sessions  SessionMetrics          `json:"sessions"`
	CachedAt  time.Time               `json:"cached_at"` //nolint:tagliatelle // API field name
}

// WorkflowMetrics holds workflow-level aggregates.
type WorkflowMetrics struct {
	Total          int     `json:"total"`
	Completed      int     `json:"completed"`
	Failed         int     `json:"failed"`
	Running        int     `json:"running"`
	Canceled       int     `json:"canceled"`
	AwaitingReview int     `json:"awaiting_review"` //nolint:tagliatelle // API field name
	CompletionRate float64 `json:"completion_rate"` //nolint:tagliatelle // API field name
}

// PhaseMetrics holds per-phase aggregates.
type PhaseMetrics struct {
	Count          int     `json:"count"`
	AvgDurationSec float64 `json:"avg_duration_sec"` //nolint:tagliatelle // API field name
	FailureRate    float64 `json:"failure_rate"`     //nolint:tagliatelle // API field name
}

// RetryMetrics holds retry statistics.
type RetryMetrics struct {
	PlanRevisions int `json:"plan_revisions"` //nolint:tagliatelle // API field name
	VerifyRetries int `json:"verify_retries"` //nolint:tagliatelle // API field name
	InfraRetries  int `json:"infra_retries"`  //nolint:tagliatelle // API field name
}

// LearningMetrics holds learning capture stats.
type LearningMetrics struct {
	Total      int            `json:"total"`
	ByCategory map[string]int `json:"by_category"` //nolint:tagliatelle // API field name
}

// SessionMetrics holds session-level aggregates.
type SessionMetrics struct {
	Total            int     `json:"total"`
	TotalDurationSec int64   `json:"total_duration_sec"` //nolint:tagliatelle // API field name
	AvgDurationSec   float64 `json:"avg_duration_sec"`   //nolint:tagliatelle // API field name
}

type metricsCache struct {
	mu      sync.RWMutex
	entries map[string]*metricsCacheEntry
}

type metricsCacheEntry struct {
	metrics *SwarmMetrics
	expires time.Time
}

func newMetricsCache() *metricsCache {
	return &metricsCache{entries: make(map[string]*metricsCacheEntry)}
}

func (c *metricsCache) get(period string) (*SwarmMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[period]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}

	return entry.metrics, true
}

func (c *metricsCache) set(period string, m *SwarmMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[period] = &metricsCacheEntry{
		metrics: m,
		expires: time.Now().Add(metricsCacheTTL),
	}
}

// parsePeriod converts a period string to a SQL datetime filter value.
// Valid periods: "24h", "7d", "30d", "all".
func parsePeriod(period string) (string, error) {
	switch period {
	case "24h":
		return "datetime('now', '-24 hours')", nil
	case "7d":
		return "datetime('now', '-7 days')", nil
	case "30d":
		return "datetime('now', '-30 days')", nil
	case "all", "":
		return "datetime('1970-01-01')", nil
	default:
		return "", fmt.Errorf("invalid period: %q (valid: 24h, 7d, 30d, all)", period)
	}
}

// GetMetrics returns aggregate swarm metrics for the given period.
// Results are cached for 60 seconds.
func (m *Manager) GetMetrics(ctx context.Context, period string) (*SwarmMetrics, error) {
	if period == "" {
		period = DefaultPeriod
	}

	if m.metricsCache != nil {
		if cached, ok := m.metricsCache.get(period); ok {
			return cached, nil
		}
	}

	sinceExpr, err := parsePeriod(period)
	if err != nil {
		return nil, err
	}

	rawDB := m.db.SQLDB()
	metrics := &SwarmMetrics{
		Period:   period,
		Phases:   make(map[string]PhaseMetrics),
		CachedAt: time.Now(),
	}

	if err := queryWorkflowMetrics(
		ctx,
		rawDB,
		sinceExpr,
		&metrics.Workflows,
	); err != nil {
		return nil, fmt.Errorf("workflow metrics: %w", err)
	}

	if err := queryPhaseMetrics(ctx, rawDB, sinceExpr, metrics.Phases); err != nil {
		return nil, fmt.Errorf("phase metrics: %w", err)
	}

	if err := queryRetryMetrics(ctx, rawDB, sinceExpr, &metrics.Retries); err != nil {
		return nil, fmt.Errorf("retry metrics: %w", err)
	}

	if err := queryLearningMetrics(
		ctx,
		rawDB,
		sinceExpr,
		&metrics.Learnings,
	); err != nil {
		return nil, fmt.Errorf("learning metrics: %w", err)
	}

	if err := querySessionMetrics(ctx, rawDB, sinceExpr, &metrics.Sessions); err != nil {
		return nil, fmt.Errorf("session metrics: %w", err)
	}

	if m.metricsCache != nil {
		m.metricsCache.set(period, metrics)
	}

	return metrics, nil
}

//nolint:gosec // sinceExpr comes from parsePeriod, not user input
func queryWorkflowMetrics(
	ctx context.Context,
	db *sql.DB,
	sinceExpr string,
	out *WorkflowMetrics,
) error {
	query := fmt.Sprintf(
		`SELECT
		COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN status = '%s' THEN 1 ELSE 0 END), 0) AS completed,
		COALESCE(SUM(CASE WHEN status = '%s' THEN 1 ELSE 0 END), 0) AS failed,
		COALESCE(SUM(CASE WHEN status = '%s' THEN 1 ELSE 0 END), 0) AS running,
		COALESCE(SUM(CASE WHEN status = '%s' THEN 1 ELSE 0 END), 0) AS canceled,
		COALESCE(SUM(CASE WHEN status = '%s' THEN 1 ELSE 0 END), 0) AS awaiting_review
	FROM swarm_workflows WHERE created_at >= %s`,
		swarm.StatusComplete,
		swarm.StatusFailed,
		swarm.StatusRunning,
		swarm.StatusCanceled,
		swarm.StatusAwaitingReview,
		sinceExpr,
	)

	row := db.QueryRowContext(ctx, query)
	if err := row.Scan(
		&out.Total,
		&out.Completed,
		&out.Failed,
		&out.Running,
		&out.Canceled,
		&out.AwaitingReview,
	); err != nil {
		return err
	}

	if out.Total > 0 {
		out.CompletionRate = float64(out.Completed) / float64(out.Total)
	}

	return nil
}

//nolint:gosec // sinceExpr comes from parsePeriod, not user input
func queryPhaseMetrics(
	ctx context.Context,
	db *sql.DB,
	sinceExpr string,
	out map[string]PhaseMetrics,
) error {
	query := fmt.Sprintf(`SELECT
		phase,
		COUNT(*) AS count,
		COALESCE(AVG(duration_sec), 0) AS avg_duration,
		COALESCE(SUM(CASE WHEN result IN ('%s', '%s', '%s') THEN 1 ELSE 0 END), 0) AS failures
	FROM swarm_sessions
	WHERE started_at >= %s
	GROUP BY phase`,
		swarm.ResultLogicFailure, swarm.ResultInfraFailure, swarm.ResultTimeout,
		sinceExpr)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var phase string
		var pm PhaseMetrics
		var failures int

		if err := rows.Scan(
			&phase,
			&pm.Count,
			&pm.AvgDurationSec,
			&failures,
		); err != nil {
			return err
		}

		if pm.Count > 0 {
			pm.FailureRate = float64(failures) / float64(pm.Count)
		}

		out[phase] = pm
	}

	return rows.Err()
}

//nolint:gosec // sinceExpr comes from parsePeriod, not user input
func queryRetryMetrics(
	ctx context.Context,
	db *sql.DB,
	sinceExpr string,
	out *RetryMetrics,
) error {
	query := fmt.Sprintf(`SELECT
		COALESCE(SUM(CASE WHEN event_type = '%s' AND phase IN ('%s', '%s') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN event_type = '%s' AND phase IN ('%s', '%s') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN event_type = '%s' AND detail LIKE '%%infra%%' THEN 1 ELSE 0 END), 0)
	FROM swarm_events WHERE created_at >= %s`,
		swarm.EventRetryTriggered, swarm.PhaseCodePlan, swarm.PhasePlanReview,
		swarm.EventRetryTriggered, swarm.PhaseVerify, swarm.PhaseImplement,
		swarm.EventRetryTriggered,
		sinceExpr)

	row := db.QueryRowContext(ctx, query)

	return row.Scan(&out.PlanRevisions, &out.VerifyRetries, &out.InfraRetries)
}

//nolint:gosec // sinceExpr comes from parsePeriod, not user input
func queryLearningMetrics(
	ctx context.Context,
	db *sql.DB,
	sinceExpr string,
	out *LearningMetrics,
) error {
	out.ByCategory = make(map[string]int)

	query := `SELECT category, COUNT(*) AS count
	FROM swarm_learnings WHERE created_at >= ` + sinceExpr + `
	GROUP BY category`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var category string
		var count int

		if err := rows.Scan(&category, &count); err != nil {
			return err
		}

		out.ByCategory[category] = count
		out.Total += count
	}

	return rows.Err()
}

//nolint:gosec // sinceExpr comes from parsePeriod, not user input
func querySessionMetrics(
	ctx context.Context,
	db *sql.DB,
	sinceExpr string,
	out *SessionMetrics,
) error {
	query := `SELECT
		COUNT(*) AS total,
		COALESCE(SUM(duration_sec), 0) AS total_duration,
		COALESCE(AVG(duration_sec), 0) AS avg_duration
	FROM swarm_sessions WHERE started_at >= ` + sinceExpr

	row := db.QueryRowContext(ctx, query)

	return row.Scan(&out.Total, &out.TotalDurationSec, &out.AvgDurationSec)
}
