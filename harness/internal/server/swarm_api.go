package server

import (
	"database/sql"
	"errors"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarm"
	"creative-mode/harness/internal/swarmorch"
)

const defaultLearningsLimit = 50

// handleSwarmStart creates a new swarm workflow and spawns the first session.
func (s *Server) handleSwarmStart(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	var req struct {
		TicketID           string `json:"ticket_id"`            //nolint:tagliatelle // API field name
		WorkflowType       string `json:"workflow_type"`        //nolint:tagliatelle // API field name
		TicketURL          string `json:"ticket_url"`           //nolint:tagliatelle // API field name
		PreviousWorkflowID string `json:"previous_workflow_id"` //nolint:tagliatelle // API field name
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
		req.PreviousWorkflowID,
	)
	if err != nil {
		s.Logger.Error("failed to start swarm workflow", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{
		"workflow_id": wfID,
		"status":      string(swarm.StatusRunning),
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

	return c.JSON(
		http.StatusOK,
		map[string]string{"status": string(swarm.StatusCanceled)},
	)
}

// handleSwarmSessionLog returns the JSONL log file for a session.
func (s *Server) handleSwarmSessionLog(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	sessionID := c.Param("id")

	// Look up session to get ticketID for the log path.
	session, err := s.DB.GetSwarmSession(c.Request().Context(), sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "session not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session")
	}

	// Get ticketID from the workflow.
	wf, wfErr := s.DB.GetSwarmWorkflow(c.Request().Context(), session.WorkflowID)
	if wfErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get workflow")
	}

	logPath := swarmorch.LogPath(s.SwarmManager.LogsDir(), wf.TicketID, sessionID)
	if _, statErr := os.Stat(logPath); os.IsNotExist(statErr) {
		return echo.NewHTTPError(http.StatusNotFound, "log not found")
	}

	return c.File(logPath)
}

// handleSwarmMetrics returns aggregate metrics for the given period.
func (s *Server) handleSwarmMetrics(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	period := c.QueryParam("period")
	if period == "" {
		period = swarmorch.DefaultPeriod
	}

	metrics, err := s.SwarmManager.GetMetrics(c.Request().Context(), period)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, metrics)
}

// handleSwarmHealth returns the current health status of the swarm system.
func (s *Server) handleSwarmHealth(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	health, err := s.SwarmManager.GetHealth(c.Request().Context())
	if err != nil {
		s.Logger.Error("failed to get swarm health", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get health")
	}

	return c.JSON(http.StatusOK, health)
}

// handleSwarmSessionStatus returns the status and context pressure for a session.
func (s *Server) handleSwarmSessionStatus(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	sessionID := c.Param("id")

	session, err := s.DB.GetSwarmSession(c.Request().Context(), sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "session not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session")
	}

	ctxPressure := s.SwarmManager.GetContextPressure(sessionID)

	return c.JSON(http.StatusOK, map[string]any{
		"session_id":       session.ID,
		"workflow_id":      session.WorkflowID,
		"phase":            string(session.Phase),
		"skill":            session.Skill,
		"result":           string(session.Result),
		"started_at":       session.StartedAt,
		"completed_at":     session.CompletedAt.String,
		"context_pressure": ctxPressure,
	})
}

// handleSwarmLearnings returns filtered learnings.
func (s *Server) handleSwarmLearnings(c echo.Context) error {
	ctx := c.Request().Context()

	// Filter by ticket if provided.
	if ticketID := c.QueryParam("ticket"); ticketID != "" {
		learnings, err := s.DB.ListSwarmLearningsByTicket(ctx, ticketID)
		if err != nil {
			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to list learnings",
			)
		}

		return c.JSON(http.StatusOK, learnings)
	}

	// Filter by phase if provided.
	if phase := c.QueryParam("phase"); phase != "" {
		learnings, err := s.DB.ListTopSwarmLearningsByPhase(
			ctx,
			sqlc.ListTopSwarmLearningsByPhaseParams{
				Phase: sql.NullString{String: phase, Valid: true},
				Limit: defaultLearningsLimit,
			},
		)
		if err != nil {
			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to list learnings",
			)
		}

		return c.JSON(http.StatusOK, learnings)
	}

	// Default: recent learnings.
	learnings, err := s.DB.ListRecentSwarmLearnings(ctx, "")
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to list learnings",
		)
	}

	return c.JSON(http.StatusOK, learnings)
}

// handleSwarmCreateLearning creates a new learning from a skill session.
func (s *Server) handleSwarmCreateLearning(c echo.Context) error {
	var req struct {
		TicketID   string `json:"ticket_id"` //nolint:tagliatelle // API field name
		Category   string `json:"category"`
		Phase      string `json:"phase"`
		Severity   string `json:"severity"`
		Title      string `json:"title"`
		Content    string `json:"content"`
		WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // API field name
		SessionID  string `json:"session_id"`  //nolint:tagliatelle // API field name
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.TicketID == "" || req.Category == "" || req.Title == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"ticket_id, category, and title are required",
		)
	}

	if req.Severity == "" {
		req.Severity = string(swarm.SeverityInfo)
	}

	id := uuid.New().String()[:8]

	if err := s.DB.CreateSwarmLearning(
		c.Request().Context(),
		sqlc.CreateSwarmLearningParams{
			ID: id,
			SourceWorkflowID: sql.NullString{
				String: req.WorkflowID,
				Valid:  req.WorkflowID != "",
			},
			SourceSessionID: sql.NullString{
				String: req.SessionID,
				Valid:  req.SessionID != "",
			},
			TicketID: req.TicketID,
			Category: swarm.LearningCategory(req.Category),
			Phase:    sql.NullString{String: req.Phase, Valid: req.Phase != ""},
			Severity: swarm.LearningSeverity(req.Severity),
			Title:    req.Title,
			Content:  req.Content,
		},
	); err != nil {
		s.Logger.Error("failed to create learning", "error", err)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create learning",
		)
	}

	return c.JSON(http.StatusOK, map[string]string{"id": id, "status": "created"})
}

// handleSwarmApproveGate approves a workflow at a human review gate.
func (s *Server) handleSwarmApproveGate(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	workflowID := c.Param("id")

	var req struct {
		Reviewer string `json:"reviewer"`
	}
	_ = c.Bind(&req)

	if err := s.SwarmManager.ApproveGate(
		c.Request().Context(),
		workflowID,
		req.Reviewer,
	); err != nil {
		s.Logger.Error("failed to approve gate", "workflow_id", workflowID, "error", err)

		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "approved"})
}

// handleSwarmRejectGate rejects a workflow at a human review gate.
func (s *Server) handleSwarmRejectGate(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	workflowID := c.Param("id")

	var req struct {
		Reviewer       string `json:"reviewer"`
		Feedback       string `json:"feedback"`
		RevisionTarget string `json:"revision_target"` //nolint:tagliatelle // API field name
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	if req.Feedback == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"feedback is required for rejection",
		)
	}

	target := swarm.RevisionTarget(req.RevisionTarget)
	if !target.Valid() {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid revision_target")
	}

	if err := s.SwarmManager.RejectGate(
		c.Request().Context(),
		workflowID,
		req.Reviewer,
		req.Feedback,
		target,
	); err != nil {
		s.Logger.Error("failed to reject gate", "workflow_id", workflowID, "error", err)

		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "rejected"})
}

// handleSwarmListGated returns workflows awaiting human review.
func (s *Server) handleSwarmListGated(c echo.Context) error {
	workflows, err := s.DB.ListAwaitingReviewSwarmWorkflows(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to list gated workflows",
		)
	}

	return c.JSON(http.StatusOK, workflows)
}

// handleSwarmCreateProject creates a Linear ticket and starts a project workflow.
func (s *Server) handleSwarmCreateProject(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.Title == "" || req.Description == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"title and description are required",
		)
	}

	ctx := c.Request().Context()

	// Create the project ticket (Linear + DB).
	ticketID, ticketURL, err := s.SwarmManager.CreateProjectTicket(
		ctx, req.Title, req.Description,
	)
	if err != nil {
		s.Logger.Error("failed to create project ticket", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Start the project workflow.
	wfID, wfErr := s.SwarmManager.StartWorkflow(
		ctx,
		ticketID,
		swarm.WorkflowTypeProject,
		ticketURL,
		"",
	)
	if wfErr != nil {
		s.Logger.Error("failed to start project workflow", "error", wfErr)

		return echo.NewHTTPError(http.StatusInternalServerError, wfErr.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{
		"workflow_id": wfID,
		"ticket_id":   ticketID,
		"ticket_url":  ticketURL,
	})
}

// handleSwarmDoctor runs diagnostic checks and returns a health report.
func (s *Server) handleSwarmDoctor(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	return c.JSON(http.StatusOK, s.SwarmManager.RunDoctor(c.Request().Context()))
}

// handleSwarmLatestDigest returns the most recent learning digest.
func (s *Server) handleSwarmLatestDigest(c echo.Context) error {
	digest, err := s.DB.GetLatestSwarmLearningDigest(c.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "no digests available")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get digest")
	}

	return c.JSON(http.StatusOK, digest)
}
