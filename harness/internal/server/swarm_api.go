package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db/sqlc"
)

// workflowStarter abstracts the SwarmManager method used to start a workflow.
type workflowStarter func(ctx context.Context, taskID, requestText string) (string, error)

// startSwarmTask validates the request, creates a task, starts the workflow,
// and returns 202 with the taskID and workflowID.
func (s *Server) startSwarmTask(
	c echo.Context,
	primitiveType string,
	start workflowStarter,
) error {
	ctx := c.Request().Context()

	var req struct {
		RequestText string `json:"requestText"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.RequestText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "requestText is required")
	}

	taskID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.CreateSwarmTask(ctx, sqlc.CreateSwarmTaskParams{
		ID:            taskID,
		PrimitiveType: primitiveType,
		RequestText:   req.RequestText,
		Status:        "pending",
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		s.Logger.Error("failed to create swarm task", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create task")
	}

	workflowID, err := start(ctx, taskID, req.RequestText)
	if err != nil {
		s.Logger.Error("failed to start workflow",
			"type", primitiveType, "taskID", taskID, "error", err)
		// Mark the task as failed so it doesn't stay orphaned in "pending".
		_ = s.DB.UpdateSwarmTaskStatus(ctx, sqlc.UpdateSwarmTaskStatusParams{
			Status:    "failed",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			ID:        taskID,
		})
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to start workflow",
		)
	}

	// Store workflow ID on the task.
	_ = s.DB.UpdateSwarmTaskWorkflowID(ctx, sqlc.UpdateSwarmTaskWorkflowIDParams{
		WorkflowID: sql.NullString{String: workflowID, Valid: true},
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		ID:         taskID,
	})

	return c.JSON(http.StatusAccepted, map[string]string{
		"taskID":     taskID,
		"workflowID": workflowID,
	})
}

// handleSwarmStartResearch creates a research task and starts the workflow.
func (s *Server) handleSwarmStartResearch(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}
	return s.startSwarmTask(c, "research", s.SwarmManager.StartResearch)
}

// handleSwarmStartCodePlan creates a code change plan task and starts the workflow.
func (s *Server) handleSwarmStartCodePlan(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}
	return s.startSwarmTask(c, "code_change_plan", s.SwarmManager.StartCodePlan)
}

// handleSwarmGetTask returns a task with its artifacts and span tree.
func (s *Server) handleSwarmGetTask(c echo.Context) error {
	ctx := c.Request().Context()
	taskID := c.Param("taskID")

	task, err := s.DB.GetSwarmTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task")
	}

	artifacts, _ := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	spans, _ := s.DB.GetSwarmSpanTree(ctx, taskID)

	return c.JSON(http.StatusOK, map[string]any{
		"task":      task,
		"artifacts": artifacts,
		"spans":     spans,
	})
}

// handleSwarmCancelTask cancels a running task's workflow.
func (s *Server) handleSwarmCancelTask(c echo.Context) error {
	ctx := c.Request().Context()
	taskID := c.Param("taskID")

	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	task, err := s.DB.GetSwarmTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task")
	}

	if !task.WorkflowID.Valid || task.WorkflowID.String == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "task has no workflow to cancel")
	}

	if cancelErr := s.SwarmManager.CancelTask(
		ctx,
		task.WorkflowID.String,
	); cancelErr != nil {
		s.Logger.Error("failed to cancel workflow",
			"taskID", taskID,
			"workflowID", task.WorkflowID.String,
			"error", cancelErr,
		)
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to cancel workflow",
		)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "canceled"})
}
