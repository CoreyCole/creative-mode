package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/db/sqlc"
	swarmview "creative-mode/harness/views/swarm"
)

const swarmHeartbeatInterval = 30 * time.Second

// swarmTaskDetail holds the data needed to render the detail pane.
type swarmTaskDetail struct {
	Spans     []sqlc.GetSwarmSpanTreeRow
	Artifacts []sqlc.SwarmArtifact
	Messages  []sqlc.SwarmTaskMessage
}

// loadTaskDetail fetches spans, artifacts, and messages for a task, logging any failures.
func (s *Server) loadTaskDetail(
	ctx context.Context,
	taskID, label string,
) swarmTaskDetail {
	var d swarmTaskDetail
	var err error
	if d.Spans, err = s.DB.GetSwarmSpanTree(ctx, taskID); err != nil {
		s.Logger.Warn(label+": failed to load spans", "taskID", taskID, "error", err)
	}
	if d.Artifacts, err = s.DB.GetSwarmArtifactsByTask(ctx, taskID); err != nil {
		s.Logger.Warn(label+": failed to load artifacts", "taskID", taskID, "error", err)
	}
	if d.Messages, err = s.DB.GetSwarmTaskMessages(ctx, taskID); err != nil {
		s.Logger.Warn(label+": failed to load messages", "taskID", taskID, "error", err)
	}
	return d
}

// handleSwarmDashboard renders the swarm agent dashboard.
func (s *Server) handleSwarmDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	tasks, err := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	if err != nil {
		s.Logger.Warn("failed to list swarm tasks", "error", err)
	}

	data := swarmview.DashboardData{
		Tasks: tasks,
	}

	// Select task: from query param, or first in list.
	taskID := c.QueryParam("task")
	if taskID == "" && len(tasks) > 0 {
		taskID = tasks[0].ID
	}

	if taskID != "" {
		task, err := s.DB.GetSwarmTask(ctx, taskID)
		if err == nil {
			data.SelectedTask = &task
			d := s.loadTaskDetail(ctx, taskID, "dashboard")
			data.Spans = d.Spans
			data.Artifacts = d.Artifacts
			data.Messages = d.Messages
		}
	}

	return render(c, swarmview.Page(data))
}

// firstActiveTaskID returns the ID of the first running/pending task, or the first task.
func firstActiveTaskID(tasks []sqlc.SwarmTask) string {
	for _, t := range tasks {
		if t.Status == sqlc.TaskStatusRunning || t.Status == sqlc.TaskStatusPending {
			return t.ID
		}
	}
	if len(tasks) > 0 {
		return tasks[0].ID
	}
	return ""
}

// patchSwarmSidebar patches the sidebar element via SSE. Returns error if client disconnected.
func patchSwarmSidebar(
	sse *datastar.ServerSentEventGenerator,
	tasks []sqlc.SwarmTask,
	selectedID string,
) error {
	return sse.PatchElementTempl(
		swarmview.Sidebar(tasks, selectedID),
		datastar.WithSelectorID("swarm-sidebar"),
	)
}

// patchSwarmDetail patches the detail pane via SSE. Returns error if client disconnected.
func patchSwarmDetail(
	sse *datastar.ServerSentEventGenerator,
	task sqlc.SwarmTask,
	messages []sqlc.SwarmTaskMessage,
	spans []sqlc.GetSwarmSpanTreeRow,
	artifacts []sqlc.SwarmArtifact,
) error {
	return sse.PatchElementTempl(
		swarmview.TaskDetailTabs(task, messages, spans, artifacts),
		datastar.WithSelectorID("swarm-detail"),
	)
}

// handleSwarmDashboardSSE provides live updates for the swarm dashboard.
func (s *Server) handleSwarmDashboardSSE(c echo.Context) error {
	r := c.Request()
	sse := datastar.NewSSE(c.Response().Writer, r)

	swarmCh := s.EventBus.Subscribe("swarm")
	defer s.EventBus.Unsubscribe("swarm", swarmCh)

	heartbeat := time.NewTicker(swarmHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-swarmCh:
			ctx := r.Context()
			tasks, err := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
			if err != nil {
				s.Logger.Warn("SSE: failed to list tasks", "error", err)
			}

			detailTaskID := firstActiveTaskID(tasks)

			if err := patchSwarmSidebar(sse, tasks, detailTaskID); err != nil {
				return nil // client disconnected
			}

			if detailTaskID != "" {
				task, err := s.DB.GetSwarmTask(ctx, detailTaskID)
				if err == nil {
					d := s.loadTaskDetail(ctx, detailTaskID, "SSE")
					if err := patchSwarmDetail(
						sse,
						task,
						d.Messages,
						d.Spans,
						d.Artifacts,
					); err != nil {
						return nil // client disconnected
					}
					if err := sse.MarshalAndPatchSignals(map[string]string{
						"selected_task_id": detailTaskID,
					}); err != nil {
						return nil // client disconnected
					}
				}
			}

		case <-heartbeat.C:
			ctx := r.Context()
			tasks, err := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
			if err != nil {
				s.Logger.Warn("SSE heartbeat: failed to list tasks", "error", err)
			}

			selectedID := firstActiveTaskID(tasks)

			if err := patchSwarmSidebar(sse, tasks, selectedID); err != nil {
				return nil // client disconnected
			}

			if selectedID != "" {
				task, err := s.DB.GetSwarmTask(ctx, selectedID)
				if err == nil &&
					(task.Status == sqlc.TaskStatusRunning || task.Status == sqlc.TaskStatusPending) {
					d := s.loadTaskDetail(ctx, selectedID, "SSE heartbeat")
					if err := patchSwarmDetail(
						sse,
						task,
						d.Messages,
						d.Spans,
						d.Artifacts,
					); err != nil {
						return nil // client disconnected
					}
				}
			}

		case <-r.Context().Done():
			return nil
		}
	}
}

// handleSwarmStartTaskDashboard starts a new swarm task from the dashboard form.
func (s *Server) handleSwarmStartTaskDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	if s.SwarmManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"swarm manager not configured",
		)
	}

	var signals struct {
		NewTaskType   string `json:"new_task_type"`   //nolint:tagliatelle // Datastar signal name
		NewTaskText   string `json:"new_task_text"`   //nolint:tagliatelle // Datastar signal name
		NewTaskTicket string `json:"new_task_ticket"` //nolint:tagliatelle // Datastar signal name
	}
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}
	if signals.NewTaskText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "request text is required")
	}

	primitiveType := sqlc.PrimitiveType(signals.NewTaskType)
	if primitiveType != sqlc.PrimitiveTypeResearch &&
		primitiveType != sqlc.PrimitiveTypeCodeChangePlan {
		primitiveType = sqlc.PrimitiveTypeResearch
	}

	// Create task.
	taskID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	ticketID := strings.TrimSpace(signals.NewTaskTicket)

	if err := s.DB.CreateSwarmTask(ctx, sqlc.CreateSwarmTaskParams{
		ID:            taskID,
		PrimitiveType: primitiveType,
		RequestText:   signals.NewTaskText,
		Status:        sqlc.TaskStatusPending,
		LinearIssueID: sql.NullString{String: ticketID, Valid: ticketID != ""},
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		s.Logger.Error("failed to create swarm task", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create task")
	}

	// Start workflow.
	var (
		workflowID string
		wfErr      error
	)
	if primitiveType == sqlc.PrimitiveTypeResearch {
		workflowID, wfErr = s.SwarmManager.StartResearch(
			ctx,
			taskID,
			signals.NewTaskText,
			ticketID,
		)
	} else {
		workflowID, wfErr = s.SwarmManager.StartCodePlan(
			ctx,
			taskID,
			signals.NewTaskText,
			ticketID,
		)
	}
	if wfErr != nil {
		s.Logger.Error("failed to start workflow",
			"type", primitiveType, "taskID", taskID, "error", wfErr)
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

	// SSE response: clear form and select new task.
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	_ = sse.MarshalAndPatchSignals(map[string]any{
		"new_task_text":    "",
		"new_task_ticket":  "",
		"show_new_form":    false,
		"selected_task_id": taskID,
		"active_tab":       "chat",
	})

	// Patch sidebar + detail.
	tasks, tasksErr := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	if tasksErr != nil {
		s.Logger.Warn("failed to list tasks after creation", "error", tasksErr)
	}
	_ = patchSwarmSidebar(sse, tasks, taskID) // SSE — fine to ignore

	task, taskErr := s.DB.GetSwarmTask(ctx, taskID)
	if taskErr != nil {
		s.Logger.Warn(
			"failed to get task after creation",
			"taskID",
			taskID,
			"error",
			taskErr,
		)
	}
	spans, spansErr := s.DB.GetSwarmSpanTree(ctx, taskID)
	if spansErr != nil {
		s.Logger.Warn(
			"failed to load spans after creation",
			"taskID",
			taskID,
			"error",
			spansErr,
		)
	}
	artifacts, artifactsErr := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	if artifactsErr != nil {
		s.Logger.Warn(
			"failed to load artifacts after creation",
			"taskID",
			taskID,
			"error",
			artifactsErr,
		)
	}
	messages, msgsErr := s.DB.GetSwarmTaskMessages(ctx, taskID)
	if msgsErr != nil {
		s.Logger.Warn(
			"failed to load messages after creation",
			"taskID",
			taskID,
			"error",
			msgsErr,
		)
	}
	_ = patchSwarmDetail(sse, task, messages, spans, artifacts) // SSE — fine to ignore

	return nil
}

// handleSwarmCancelTaskDashboard cancels a task from the dashboard.
func (s *Server) handleSwarmCancelTaskDashboard(c echo.Context) error {
	ctx := c.Request().Context()
	taskID := c.QueryParam("task")

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

	if task.WorkflowID.Valid && task.WorkflowID.String != "" {
		if cancelErr := s.SwarmManager.CancelTask(
			ctx,
			task.WorkflowID.String,
		); cancelErr != nil {
			s.Logger.Error("failed to cancel workflow",
				"taskID", taskID, "error", cancelErr)
			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to cancel workflow",
			)
		}
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Re-fetch and patch.
	tasks, tasksErr := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	if tasksErr != nil {
		s.Logger.Warn("failed to list tasks after cancel", "error", tasksErr)
	}
	_ = patchSwarmSidebar(sse, tasks, taskID) // SSE — fine to ignore

	updatedTask, taskErr := s.DB.GetSwarmTask(ctx, taskID)
	if taskErr != nil {
		s.Logger.Warn(
			"failed to get task after cancel",
			"taskID",
			taskID,
			"error",
			taskErr,
		)
	}
	spans, spansErr := s.DB.GetSwarmSpanTree(ctx, taskID)
	if spansErr != nil {
		s.Logger.Warn(
			"failed to load spans after cancel",
			"taskID",
			taskID,
			"error",
			spansErr,
		)
	}
	artifacts, artifactsErr := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	if artifactsErr != nil {
		s.Logger.Warn(
			"failed to load artifacts after cancel",
			"taskID",
			taskID,
			"error",
			artifactsErr,
		)
	}
	messages, msgsErr := s.DB.GetSwarmTaskMessages(ctx, taskID)
	if msgsErr != nil {
		s.Logger.Warn(
			"failed to load messages after cancel",
			"taskID",
			taskID,
			"error",
			msgsErr,
		)
	}
	_ = patchSwarmDetail(
		sse,
		updatedTask,
		messages,
		spans,
		artifacts,
	) // SSE — fine to ignore

	return nil
}

// handleSwarmArtifactView serves an artifact file by ID.
func (s *Server) handleSwarmArtifactView(c echo.Context) error {
	ctx := c.Request().Context()
	artifactID := c.Param("id")

	artifact, err := s.DB.GetSwarmArtifact(ctx, artifactID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "artifact not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get artifact")
	}

	// Resolve file path relative to the repo root (one level up from DataDir).
	repoRoot := filepath.Dir(s.DataDir)
	fullPath := filepath.Join(repoRoot, artifact.FilePath)

	// Security: ensure resolved path stays within repo root.
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve path")
	}
	absRoot, _ := filepath.Abs(repoRoot)
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		return echo.NewHTTPError(http.StatusForbidden, "path traversal")
	}

	return c.File(absPath)
}

// handleSwarmChatMessage handles user chat messages on the swarm dashboard.
func (s *Server) handleSwarmChatMessage(c echo.Context) error {
	ctx := c.Request().Context()

	var signals struct {
		ChatInput      string `json:"chat_input"`       //nolint:tagliatelle // Datastar signal name
		SelectedTaskID string `json:"selected_task_id"` //nolint:tagliatelle // Datastar signal name
	}
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	chatText := strings.TrimSpace(signals.ChatInput)
	if chatText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message is required")
	}
	if signals.SelectedTaskID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "no task selected")
	}

	// Verify task exists.
	task, err := s.DB.GetSwarmTask(ctx, signals.SelectedTaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task")
	}

	// Insert user message.
	msgID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.CreateSwarmTaskMessage(ctx, sqlc.CreateSwarmTaskMessageParams{
		ID:        msgID,
		TaskID:    task.ID,
		Role:      sqlc.MessageRoleUser,
		Content:   chatText,
		CreatedAt: now,
	}); err != nil {
		s.Logger.Error("failed to create swarm chat message", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save message")
	}

	// SSE response: clear input, append new message bubble.
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	_ = sse.MarshalAndPatchSignals(map[string]any{
		"chat_input": "",
	})

	// Append the new user message to the chat log.
	_ = sse.PatchElementTempl(
		swarmview.ChatBubble(swarmview.ChatMessage{
			Role:      "user",
			Content:   chatText,
			Timestamp: now,
		}),
		datastar.WithSelectorID("swarm-chat-log"),
		datastar.WithModeAppend(),
	)

	// Publish event so SSE stream picks it up for other clients.
	s.EventBus.Publish("swarm", "chat_message")

	return nil
}
