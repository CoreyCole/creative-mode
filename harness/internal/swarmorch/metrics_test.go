package swarmorch

import (
	"database/sql"
	"testing"
	"time"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/swarm"
)

func TestGetMetricsEmpty(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	metrics, err := mgr.GetMetrics(t.Context(), "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if metrics.Period != "all" {
		t.Errorf("period = %q, want all", metrics.Period)
	}
	if metrics.Workflows.Total != 0 {
		t.Errorf("total workflows = %d, want 0", metrics.Workflows.Total)
	}
	if metrics.Sessions.Total != 0 {
		t.Errorf("total sessions = %d, want 0", metrics.Sessions.Total)
	}
}

func TestGetMetricsWithData(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// Seed workflows.
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-m1", TicketID: "TKT-M1",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseDone, Status: swarm.StatusComplete, Attempt: 1,
	})
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-m2", TicketID: "TKT-M2",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseResearch, Status: swarm.StatusFailed, Attempt: 1,
	})
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-m3", TicketID: "TKT-M3",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseImplement, Status: swarm.StatusRunning, Attempt: 1,
	})

	// Seed sessions with durations.
	_ = database.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID: "sess-m1", WorkflowID: "wf-m1",
		SessionName: "cm-swarm-TKT-M1-research", Skill: "swarm-research",
		Phase: swarm.PhaseResearch,
	})
	_ = database.CompleteSwarmSession(ctx, sqlc.CompleteSwarmSessionParams{
		ID: "sess-m1", Result: swarm.ResultSuccess,
		DurationSec: sql.NullInt64{Int64: 120, Valid: true},
	})

	_ = database.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID: "sess-m2", WorkflowID: "wf-m2",
		SessionName: "cm-swarm-TKT-M2-research", Skill: "swarm-research",
		Phase: swarm.PhaseResearch,
	})
	_ = database.CompleteSwarmSession(ctx, sqlc.CompleteSwarmSessionParams{
		ID: "sess-m2", Result: swarm.ResultInfraFailure,
		DurationSec: sql.NullInt64{Int64: 60, Valid: true},
	})

	_ = database.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID: "sess-m3", WorkflowID: "wf-m1",
		SessionName: "cm-swarm-TKT-M1-implement", Skill: "swarm-code",
		Phase: swarm.PhaseImplement,
	})
	_ = database.CompleteSwarmSession(ctx, sqlc.CompleteSwarmSessionParams{
		ID: "sess-m3", Result: swarm.ResultSuccess,
		DurationSec: sql.NullInt64{Int64: 300, Valid: true},
	})

	metrics, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	// Workflow counts.
	if metrics.Workflows.Total != 3 {
		t.Errorf("total = %d, want 3", metrics.Workflows.Total)
	}
	if metrics.Workflows.Completed != 1 {
		t.Errorf("completed = %d, want 1", metrics.Workflows.Completed)
	}
	if metrics.Workflows.Failed != 1 {
		t.Errorf("failed = %d, want 1", metrics.Workflows.Failed)
	}
	if metrics.Workflows.Running != 1 {
		t.Errorf("running = %d, want 1", metrics.Workflows.Running)
	}

	// Session counts.
	if metrics.Sessions.Total != 3 {
		t.Errorf("total sessions = %d, want 3", metrics.Sessions.Total)
	}

	// Phase metrics.
	research, ok := metrics.Phases["research"]
	if !ok {
		t.Fatal("missing research phase metrics")
	}
	if research.Count != 2 {
		t.Errorf("research count = %d, want 2", research.Count)
	}
	if research.FailureRate == 0 {
		t.Error("research failure rate should be > 0 (1 infra_failure out of 2)")
	}

	impl, ok := metrics.Phases["implement"]
	if !ok {
		t.Fatal("missing implement phase metrics")
	}
	if impl.Count != 1 {
		t.Errorf("implement count = %d, want 1", impl.Count)
	}
}

func TestGetMetricsRetries(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()
	mgr := NewManager(database, testLogger(), bus, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// Seed a workflow.
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-retry-m", TicketID: "TKT-RETRY-M",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhasePlanReview, Status: swarm.StatusRunning, Attempt: 1,
	})

	// Emit retry events using the manager's emitEvent.
	mgr.emitEvent(ctx, "wf-retry-m", "", "TKT-RETRY-M",
		swarm.EventRetryTriggered, swarm.PhaseCodePlan, "attempt 2")
	mgr.emitEvent(ctx, "wf-retry-m", "", "TKT-RETRY-M",
		swarm.EventRetryTriggered, swarm.PhaseVerify, "attempt 2")

	metrics, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if metrics.Retries.PlanRevisions != 1 {
		t.Errorf("plan revisions = %d, want 1", metrics.Retries.PlanRevisions)
	}
	if metrics.Retries.VerifyRetries != 1 {
		t.Errorf("verify retries = %d, want 1", metrics.Retries.VerifyRetries)
	}
}

func TestGetMetricsLearnings(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// Seed learnings via the Manager capture methods.
	_ = mgr.capturePlanIssue(ctx, "", "", "TKT-L1", "plan issue 1")
	_ = mgr.capturePlanIssue(ctx, "", "", "TKT-L2", "plan issue 2")
	_ = mgr.captureCodeBug(ctx, "", "", "TKT-L3", "code bug 1")

	metrics, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if metrics.Learnings.Total != 3 {
		t.Errorf("total learnings = %d, want 3", metrics.Learnings.Total)
	}
	if metrics.Learnings.ByCategory["plan_issue"] != 2 {
		t.Errorf(
			"plan_issue count = %d, want 2",
			metrics.Learnings.ByCategory["plan_issue"],
		)
	}
	if metrics.Learnings.ByCategory["code_bug"] != 1 {
		t.Errorf("code_bug count = %d, want 1", metrics.Learnings.ByCategory["code_bug"])
	}
}

func TestGetMetricsCaching(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// First call populates cache.
	m1, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	// Add a workflow after the first call.
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-cache", TicketID: "TKT-CACHE",
		WorkflowType: swarm.WorkflowTypeResearch,
		Phase:        swarm.PhaseResearch, Status: swarm.StatusRunning, Attempt: 1,
	})

	// Second call should return cached (stale) data.
	m2, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if m2.Workflows.Total != m1.Workflows.Total {
		t.Errorf(
			"cached result changed: got %d, want %d",
			m2.Workflows.Total, m1.Workflows.Total,
		)
	}
}

func TestParsePeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		period  string
		wantErr bool
	}{
		{"24h", false},
		{"7d", false},
		{"30d", false},
		{"all", false},
		{"", false},
		{"invalid", true},
		{"1y", true},
	}

	for _, tt := range tests {
		_, err := parsePeriod(tt.period)
		if (err != nil) != tt.wantErr {
			t.Errorf(
				"parsePeriod(%q): err = %v, wantErr = %v",
				tt.period,
				err,
				tt.wantErr,
			)
		}
	}
}

func TestGetMetricsDefaultPeriod(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	metrics, err := mgr.GetMetrics(t.Context(), "")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if metrics.Period != DefaultPeriod {
		t.Errorf("default period = %q, want %s", metrics.Period, DefaultPeriod)
	}
}

func TestGetMetricsCompletionRate(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	// 2 completed, 2 failed = 50% completion rate.
	for i, status := range []swarm.WorkflowStatus{
		swarm.StatusComplete, swarm.StatusComplete,
		swarm.StatusFailed, swarm.StatusFailed,
	} {
		id := "wf-cr-" + string(rune('a'+i))
		_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
			ID: id, TicketID: "TKT-CR",
			WorkflowType: swarm.WorkflowTypeCode,
			Phase:        swarm.PhaseDone, Status: status, Attempt: 1,
		})
	}

	metrics, err := mgr.GetMetrics(ctx, "all")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	want := 0.5
	if metrics.Workflows.CompletionRate != want {
		t.Errorf("completion_rate = %f, want %f", metrics.Workflows.CompletionRate, want)
	}
}

func TestGetHealthEmpty(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	health, err := mgr.GetHealth(t.Context())
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}

	if health.Status != HealthStatusHealthy {
		t.Errorf("status = %q, want healthy", health.Status)
	}
	if health.Capacity.MaxSessions != swarm.DefaultConfig.MaxSessions {
		t.Errorf("max_sessions = %d, want %d",
			health.Capacity.MaxSessions, swarm.DefaultConfig.MaxSessions)
	}
	if len(health.ActiveWorkflows) != 0 {
		t.Errorf("active workflows = %d, want 0", len(health.ActiveWorkflows))
	}
}

func TestGetHealthWithActiveWorkflows(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-h1", TicketID: "TKT-H1",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseImplement, Status: swarm.StatusRunning, Attempt: 2,
	})

	health, err := mgr.GetHealth(ctx)
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}

	if len(health.ActiveWorkflows) != 1 {
		t.Fatalf("active workflows = %d, want 1", len(health.ActiveWorkflows))
	}

	wf := health.ActiveWorkflows[0]
	if wf.WorkflowID != "wf-h1" {
		t.Errorf("workflow_id = %q, want wf-h1", wf.WorkflowID)
	}
	if wf.Phase != "implement" {
		t.Errorf("phase = %q, want implement", wf.Phase)
	}
	if wf.Attempt != 2 {
		t.Errorf("attempt = %d, want 2", wf.Attempt)
	}
}

func TestGetHealthRecentCompletions(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()

	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID: "wf-hc1", TicketID: "TKT-HC1",
		WorkflowType: swarm.WorkflowTypeCode,
		Phase:        swarm.PhaseDone, Status: swarm.StatusComplete, Attempt: 1,
	})

	health, err := mgr.GetHealth(ctx)
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}

	if len(health.RecentCompletions) != 1 {
		t.Fatalf("recent completions = %d, want 1", len(health.RecentCompletions))
	}

	if health.RecentCompletions[0].Status != "completed" {
		t.Errorf("status = %q, want completed", health.RecentCompletions[0].Status)
	}
}

func TestDetermineHealthStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		health SwarmHealth
		want   string
	}{
		{
			name: "healthy_no_issues",
			health: SwarmHealth{
				Capacity:        CapacityInfo{ActiveSessions: 1, MaxSessions: 4},
				ActiveWorkflows: []ActiveWorkflowInfo{{Stalled: false}},
			},
			want: HealthStatusHealthy,
		},
		{
			name: "degraded_at_capacity",
			health: SwarmHealth{
				Capacity:        CapacityInfo{ActiveSessions: 4, MaxSessions: 4},
				ActiveWorkflows: []ActiveWorkflowInfo{{Stalled: false}},
			},
			want: HealthStatusDegraded,
		},
		{
			name: "degraded_one_stalled",
			health: SwarmHealth{
				Capacity: CapacityInfo{ActiveSessions: 2, MaxSessions: 4},
				ActiveWorkflows: []ActiveWorkflowInfo{
					{Stalled: true}, {Stalled: false},
				},
			},
			want: HealthStatusDegraded,
		},
		{
			name: "unhealthy_all_stalled",
			health: SwarmHealth{
				Capacity: CapacityInfo{ActiveSessions: 2, MaxSessions: 4},
				ActiveWorkflows: []ActiveWorkflowInfo{
					{Stalled: true}, {Stalled: true},
				},
			},
			want: HealthStatusUnhealthy,
		},
		{
			name: "healthy_no_active",
			health: SwarmHealth{
				Capacity:        CapacityInfo{ActiveSessions: 0, MaxSessions: 4},
				ActiveWorkflows: nil,
			},
			want: HealthStatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := determineHealthStatus(&tt.health)
			if got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMetricsCacheExpiry(t *testing.T) {
	t.Parallel()

	cache := newMetricsCache()

	m := &SwarmMetrics{Period: "all", CachedAt: time.Now()}
	cache.set("all", m)

	// Should be cached.
	got, ok := cache.get("all")
	if !ok || got != m {
		t.Error("expected cached metrics")
	}

	// Manually expire.
	cache.mu.Lock()
	cache.entries["all"].expires = time.Now().Add(-time.Second)
	cache.mu.Unlock()

	_, ok = cache.get("all")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}
