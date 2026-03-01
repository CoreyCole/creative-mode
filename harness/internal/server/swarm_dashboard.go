package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarmorch"
	swarmview "creative-mode/harness/views/swarm"
)

const swarmEventLimit = 30

// handleSwarmDashboard renders the swarm dashboard page.
func (s *Server) handleSwarmDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	workflows, _ := s.DB.ListAllSwarmWorkflows(ctx, defaultQueryLimit)
	recentEvents, _ := s.DB.ListRecentSwarmEvents(ctx, swarmEventLimit)

	data := swarmview.DashboardData{
		Workflows: workflows,
		Events:    recentEvents,
	}

	// Fetch metrics + health if swarm manager is available.
	if s.SwarmManager != nil {
		if metrics, err := s.SwarmManager.GetMetrics(
			ctx,
			swarmorch.DefaultPeriod,
		); err == nil {
			data.Metrics = metrics
		}

		if health, err := s.SwarmManager.GetHealth(ctx); err == nil {
			data.Health = health
		}
	}

	// Fetch recent learnings.
	if learnings, err := s.DB.ListRecentSwarmLearnings(ctx, ""); err == nil {
		data.Learnings = learnings
	}

	// Fetch latest digest.
	if digest, err := s.DB.GetLatestSwarmLearningDigest(ctx); err == nil {
		data.Digest = &digest
	}

	return render(c, swarmview.Page(data))
}

// handleSwarmWorkflowDetail renders the detail page for a single workflow.
func (s *Server) handleSwarmWorkflowDetail(c echo.Context) error {
	ctx := c.Request().Context()
	workflowID := c.Param("id")

	wf, err := s.DB.GetSwarmWorkflow(ctx, workflowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "workflow not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get workflow")
	}

	sessions, _ := s.DB.ListSwarmSessionsByWorkflow(ctx, workflowID)
	wfEvents, _ := s.DB.ListSwarmEventsByWorkflow(ctx, sql.NullString{
		String: workflowID, Valid: true,
	})
	milestones, _ := s.DB.ListSwarmMilestonesByWorkflow(ctx, workflowID)

	data := swarmview.WorkflowDetailData{
		Workflow:   wf,
		Sessions:   sessions,
		Events:     wfEvents,
		Milestones: milestones,
	}

	return render(c, swarmview.WorkflowPage(data))
}

// handleSwarmDashboardSSE provides live updates for the swarm dashboard.
func (s *Server) handleSwarmDashboardSSE(c echo.Context) error {
	r := c.Request()
	sse := datastar.NewSSE(c.Response().Writer, r)

	globalCh := s.EventBus.SubscribeGlobal()
	defer s.EventBus.UnsubscribeGlobal(globalCh)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()

	for {
		select {
		case event := <-globalCh:
			e, ok := event.(map[string]any)
			if !ok {
				continue
			}
			eventType, _ := e["event"].(string)
			if !strings.HasPrefix(eventType, "swarm.") {
				continue
			}

			// Live tool activity feed.
			if eventType == "swarm.tool_use" {
				ticketID, _ := e["ticket_id"].(string)
				phase, _ := e["phase"].(string)
				tool, _ := e["tool"].(string)
				file, _ := e["file"].(string)

				if err := sse.PatchElementTempl(
					swarmview.ToolActivityItem(ticketID, phase, tool, file),
					datastar.WithSelectorID("swarm-tool-activity"),
					datastar.WithModeAppend(),
				); err != nil {
					return nil
				}

				continue
			}

			// Refresh workflow + events tabs.
			workflows, _ := s.DB.ListAllSwarmWorkflows(ctx, defaultQueryLimit)
			recentEvents, _ := s.DB.ListRecentSwarmEvents(ctx, swarmEventLimit)

			if err := sse.PatchElementTempl(
				swarmview.WorkflowsTab(workflows),
				datastar.WithSelectorID("swarm-workflows-tab"),
			); err != nil {
				return nil
			}
			if err := sse.PatchElementTempl(
				swarmview.EventsTab(recentEvents),
				datastar.WithSelectorID("swarm-events-tab"),
			); err != nil {
				return nil
			}

			// Refresh metrics + health tabs on workflow events.
			s.patchMetricsAndLearnings(ctx, sse)

		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// patchMetricsAndLearnings pushes updated metrics, health, and learnings tabs via SSE.
func (s *Server) patchMetricsAndLearnings(
	ctx context.Context,
	sse *datastar.ServerSentEventGenerator,
) {
	var metrics *swarmorch.SwarmMetrics
	var health *swarmorch.SwarmHealth

	if s.SwarmManager != nil {
		metrics, _ = s.SwarmManager.GetMetrics(ctx, swarmorch.DefaultPeriod)
		health, _ = s.SwarmManager.GetHealth(ctx)
	}

	_ = sse.PatchElementTempl(
		swarmview.MetricsTab(metrics, health),
		datastar.WithSelectorID("swarm-metrics-tab"),
	)

	var learnings []sqlc.SwarmLearning
	learnings, _ = s.DB.ListRecentSwarmLearnings(ctx, "")

	var digest *sqlc.SwarmLearningDigest
	if d, err := s.DB.GetLatestSwarmLearningDigest(ctx); err == nil {
		digest = &d
	}

	_ = sse.PatchElementTempl(
		swarmview.LearningsTab(learnings, digest),
		datastar.WithSelectorID("swarm-learnings-tab"),
	)
}

// handleSwarmDashboardCancel cancels a workflow from the dashboard.
func (s *Server) handleSwarmDashboardCancel(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	workflowID := c.Param("id")
	if err := s.SwarmManager.CancelWorkflow(
		c.Request().Context(),
		workflowID,
	); err != nil {
		s.Logger.Error("failed to cancel swarm workflow", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Re-fetch and return updated workflow detail as SSE patch.
	ctx := c.Request().Context()
	wf, err := s.DB.GetSwarmWorkflow(ctx, workflowID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get workflow")
	}

	sessions, _ := s.DB.ListSwarmSessionsByWorkflow(ctx, workflowID)
	wfEvents, _ := s.DB.ListSwarmEventsByWorkflow(ctx, sql.NullString{
		String: workflowID, Valid: true,
	})
	milestones, _ := s.DB.ListSwarmMilestonesByWorkflow(ctx, workflowID)

	data := swarmview.WorkflowDetailData{
		Workflow:   wf,
		Sessions:   sessions,
		Events:     wfEvents,
		Milestones: milestones,
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	return sse.PatchElementTempl(swarmview.WorkflowPage(data))
}
