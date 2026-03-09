package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/linear"
	"creative-mode/harness/internal/swarmorch"
)

// workflowStarter abstracts the SwarmManager method used to start a workflow.
type workflowStarter func(ctx context.Context, taskID, requestText, ticketID string) (string, error)

// startSwarmTask validates the request, creates a task, starts the workflow,
// and returns 202 with the taskID and workflowID.
func (s *Server) startSwarmTask(
	c echo.Context,
	primitiveType sqlc.PrimitiveType,
	start workflowStarter,
) error {
	ctx := c.Request().Context()

	var req struct {
		RequestText string `json:"requestText"`
		TicketID    string `json:"ticketID"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.RequestText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "requestText is required")
	}

	// Auto-create a Linear ticket if none was provided.
	if req.TicketID == "" && s.LinearClient != nil {
		labels := labelsForPrimitive(primitiveType)
		identifier, createErr := s.LinearClient.CreateIssue(
			ctx,
			req.RequestText,
			linear.CreateOpts{
				Labels: labels,
				State:  "In Progress",
			},
		)
		if createErr != nil {
			s.Logger.Error("failed to auto-create Linear ticket", "error", createErr)
			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to create Linear ticket",
			)
		}
		req.TicketID = identifier
		s.Logger.Info("auto-created Linear ticket",
			"ticketID", identifier, "type", primitiveType)
	}

	taskID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.CreateSwarmTask(ctx, sqlc.CreateSwarmTaskParams{
		ID:            taskID,
		PrimitiveType: primitiveType,
		RequestText:   req.RequestText,
		Status:        sqlc.TaskStatusPending,
		LinearIssueID: sql.NullString{String: req.TicketID, Valid: req.TicketID != ""},
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		s.Logger.Error("failed to create swarm task", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create task")
	}

	workflowID, err := start(ctx, taskID, req.RequestText, req.TicketID)
	if err != nil {
		s.Logger.Error("failed to start workflow",
			"type", primitiveType, "taskID", taskID, "error", err)
		// Mark the task as failed so it doesn't stay orphaned in "pending".
		if statusErr := s.DB.UpdateSwarmTaskStatus(ctx, sqlc.UpdateSwarmTaskStatusParams{
			Status:    sqlc.TaskStatusFailed,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			ID:        taskID,
		}); statusErr != nil {
			s.Logger.Error(
				"failed to mark task as failed",
				"taskID",
				taskID,
				"error",
				statusErr,
			)
		}
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to start workflow",
		)
	}

	// Store workflow ID on the task.
	if err := s.DB.UpdateSwarmTaskWorkflowID(ctx, sqlc.UpdateSwarmTaskWorkflowIDParams{
		WorkflowID: sql.NullString{String: workflowID, Valid: true},
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		ID:         taskID,
	}); err != nil {
		s.Logger.Error("failed to persist workflow ID", "taskID", taskID, "error", err)
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to persist workflow",
		)
	}

	return c.JSON(http.StatusAccepted, map[string]string{
		"taskID":     taskID,
		"workflowID": workflowID,
		"ticketID":   req.TicketID,
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
	return s.startSwarmTask(c, sqlc.PrimitiveTypeResearch, s.SwarmManager.StartResearch)
}

// handleSwarmStartCodePlan creates a code change plan task and starts the workflow.
func (s *Server) handleSwarmStartCodePlan(c echo.Context) error {
	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}
	return s.startSwarmTask(
		c,
		sqlc.PrimitiveTypeCodeChangePlan,
		s.SwarmManager.StartCodePlan,
	)
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

	artifacts, err := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to load artifacts",
		)
	}
	spans, err := s.DB.GetSwarmSpanTree(ctx, taskID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load spans")
	}

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

// handleSwarmListTasks returns all tasks as JSON.
func (s *Server) handleSwarmListTasks(c echo.Context) error {
	ctx := c.Request().Context()
	tasks, err := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list tasks")
	}
	return c.JSON(http.StatusOK, tasks)
}

// handleSwarmGetTaskSpans returns the full span tree for a task.
func (s *Server) handleSwarmGetTaskSpans(c echo.Context) error {
	ctx := c.Request().Context()
	taskID := c.Param("taskID")

	spans, err := s.DB.GetSwarmSpanTree(ctx, taskID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get spans")
	}
	return c.JSON(http.StatusOK, spans)
}

// handleSwarmGetTaskMetrics returns aggregate metrics for a task.
func (s *Server) handleSwarmGetTaskMetrics(c echo.Context) error {
	ctx := c.Request().Context()
	taskID := c.Param("taskID")

	task, err := s.DB.GetSwarmTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task")
	}

	spans, err := s.DB.GetSwarmSpanTree(ctx, taskID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load spans")
	}
	metrics := computeTaskMetricsAPI(spans)

	// Add task-level info.
	metrics["taskID"] = task.ID
	metrics["status"] = task.Status
	metrics["primitiveType"] = task.PrimitiveType

	return c.JSON(http.StatusOK, metrics)
}

// computeTaskMetricsAPI aggregates metrics from span metadata into a JSON-friendly map.
func computeTaskMetricsAPI(spans []sqlc.GetSwarmSpanTreeRow) map[string]any {
	var (
		agents     int
		tools      int
		llmCalls   int
		tokens     int
		cost       float64
		durationMs int64
	)

	for _, s := range spans {
		switch s.SpanType {
		case "agent":
			agents++
			if !s.MetadataJSON.Valid || s.MetadataJSON.String == "" {
				break
			}
			var meta swarmorch.SpanMetadata
			if json.Unmarshal([]byte(s.MetadataJSON.String), &meta) != nil {
				break
			}
			tokens += meta.TotalInputTokens + meta.TotalOutputTokens
			cost += meta.TotalCost
			tools += meta.ToolCallCount
			llmCalls += meta.LLMCallCount
		case "workflow":
			if s.DurationMs.Valid && s.DurationMs.Int64 > durationMs {
				durationMs = s.DurationMs.Int64
			}
		}
	}

	return map[string]any{
		"agents":      agents,
		"tools":       tools,
		"llmCalls":    llmCalls,
		"totalTokens": tokens,
		"totalCost":   cost,
		"durationMs":  durationMs,
	}
}

// labelsForPrimitive returns the Linear labels to apply when auto-creating a ticket.
func labelsForPrimitive(pt sqlc.PrimitiveType) []string {
	switch pt {
	case sqlc.PrimitiveTypeResearch:
		return []string{"type:research", "swarm:research"}
	case sqlc.PrimitiveTypeCodeChangePlan:
		return []string{"type:code-change", "swarm:planning"}
	default:
		return []string{fmt.Sprintf("type:%s", pt)}
	}
}
