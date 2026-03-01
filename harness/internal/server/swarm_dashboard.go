package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

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
		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
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
