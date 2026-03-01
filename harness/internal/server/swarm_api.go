package server

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/swarm"
)

// handleSwarmStart creates a new swarm workflow and spawns the first session.
func (s *Server) handleSwarmStart(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	var req struct {
		TicketID     string `json:"ticket_id"`     //nolint:tagliatelle // API field name
		WorkflowType string `json:"workflow_type"` //nolint:tagliatelle // API field name
		TicketURL    string `json:"ticket_url"`    //nolint:tagliatelle // API field name
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.TicketID == "" || req.WorkflowType == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"ticket_id and workflow_type are required",
		)
	}

	wfID, err := s.SwarmManager.StartWorkflow(
		c.Request().Context(),
		req.TicketID,
		swarm.WorkflowType(req.WorkflowType),
		req.TicketURL,
	)
	if err != nil {
		s.Logger.Error("failed to start swarm workflow", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{
		"workflow_id": wfID,
		"status":      "running",
	})
}

// handleSwarmStatus returns the current state of a swarm workflow.
func (s *Server) handleSwarmStatus(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	workflowID := c.Param("id")

	wf, session, err := s.SwarmManager.GetWorkflow(c.Request().Context(), workflowID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "workflow not found")
	}

	result := map[string]any{
		"workflow_id":   wf.ID,
		"ticket_id":     wf.TicketID,
		"workflow_type": string(wf.WorkflowType),
		"phase":         string(wf.Phase),
		"status":        string(wf.Status),
		"attempt":       wf.Attempt,
		"created_at":    wf.CreatedAt,
		"updated_at":    wf.UpdatedAt,
	}

	if session != nil {
		result["latest_session"] = map[string]any{
			"session_id":   session.ID,
			"session_name": session.SessionName,
			"skill":        session.Skill,
			"phase":        string(session.Phase),
			"result":       string(session.Result),
			"started_at":   session.StartedAt,
			"completed_at": session.CompletedAt.String,
		}
	}

	return c.JSON(http.StatusOK, result)
}

// handleSwarmCancel cancels an active swarm workflow.
func (s *Server) handleSwarmCancel(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	var req struct {
		WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // API field name
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.WorkflowID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "workflow_id is required")
	}

	if err := s.SwarmManager.CancelWorkflow(
		c.Request().Context(),
		req.WorkflowID,
	); err != nil {
		s.Logger.Error("failed to cancel swarm workflow", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "canceled"})
}
