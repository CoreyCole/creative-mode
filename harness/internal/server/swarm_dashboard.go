package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/db/sqlc"
	swarmview "creative-mode/harness/views/swarm"
)

const (
	swarmHeartbeatInterval = 30 * time.Second
	swarmTypeResearch      = "research"
	swarmTypeCodePlan      = "code_change_plan"
	swarmStatusRunning     = "running"
	swarmStatusPending     = "pending"
)

// handleSwarmDashboard renders the swarm agent dashboard.
func (s *Server) handleSwarmDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	tasks, _ := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)

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
			data.Spans, _ = s.DB.GetSwarmSpanTree(ctx, taskID)
			data.Artifacts, _ = s.DB.GetSwarmArtifactsByTask(ctx, taskID)
			data.Messages, _ = s.DB.GetSwarmTaskMessages(ctx, taskID)
		}
	}

	return render(c, swarmview.Page(data))
}

// firstActiveTaskID returns the ID of the first running/pending task, or the first task.
func firstActiveTaskID(tasks []sqlc.SwarmTask) string {
	for _, t := range tasks {
		if t.Status == swarmStatusRunning || t.Status == swarmStatusPending {
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
			tasks, _ := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)

			detailTaskID := firstActiveTaskID(tasks)

			if err := patchSwarmSidebar(sse, tasks, detailTaskID); err != nil {
				return nil // client disconnected
			}

			if detailTaskID != "" {
				task, err := s.DB.GetSwarmTask(ctx, detailTaskID)
				if err == nil {
					spans, _ := s.DB.GetSwarmSpanTree(ctx, detailTaskID)
					artifacts, _ := s.DB.GetSwarmArtifactsByTask(ctx, detailTaskID)
					messages, _ := s.DB.GetSwarmTaskMessages(ctx, detailTaskID)
					if err := patchSwarmDetail(
						sse,
						task,
						messages,
						spans,
						artifacts,
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
			tasks, _ := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)

			selectedID := firstActiveTaskID(tasks)

			if err := patchSwarmSidebar(sse, tasks, selectedID); err != nil {
				return nil // client disconnected
			}

			if selectedID != "" {
				task, err := s.DB.GetSwarmTask(ctx, selectedID)
				if err == nil &&
					(task.Status == swarmStatusRunning || task.Status == swarmStatusPending) {
					spans, _ := s.DB.GetSwarmSpanTree(ctx, selectedID)
					artifacts, _ := s.DB.GetSwarmArtifactsByTask(ctx, selectedID)
					messages, _ := s.DB.GetSwarmTaskMessages(ctx, selectedID)
					if err := patchSwarmDetail(
						sse,
						task,
						messages,
						spans,
						artifacts,
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
		NewTaskType string `json:"new_task_type"` //nolint:tagliatelle // Datastar signal name
		NewTaskText string `json:"new_task_text"` //nolint:tagliatelle // Datastar signal name
	}
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}
	if signals.NewTaskText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "request text is required")
	}

	primitiveType := signals.NewTaskType
	if primitiveType != swarmTypeResearch && primitiveType != swarmTypeCodePlan {
		primitiveType = swarmTypeResearch
	}

	// Create task.
	taskID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.CreateSwarmTask(ctx, sqlc.CreateSwarmTaskParams{
		ID:            taskID,
		PrimitiveType: primitiveType,
		RequestText:   signals.NewTaskText,
		Status:        swarmStatusPending,
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
	if primitiveType == swarmTypeResearch {
		workflowID, wfErr = s.SwarmManager.StartResearch(ctx, taskID, signals.NewTaskText)
	} else {
		workflowID, wfErr = s.SwarmManager.StartCodePlan(ctx, taskID, signals.NewTaskText)
	}
	if wfErr != nil {
		s.Logger.Error("failed to start workflow",
			"type", primitiveType, "taskID", taskID, "error", wfErr)
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

	_ = s.DB.UpdateSwarmTaskWorkflowID(ctx, sqlc.UpdateSwarmTaskWorkflowIDParams{
		WorkflowID: sql.NullString{String: workflowID, Valid: true},
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		ID:         taskID,
	})

	// SSE response: clear form and select new task.
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	_ = sse.MarshalAndPatchSignals(map[string]any{
		"new_task_text":    "",
		"show_new_form":    false,
		"selected_task_id": taskID,
		"active_tab":       "chat",
	})

	// Patch sidebar + detail.
	tasks, _ := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	_ = patchSwarmSidebar(sse, tasks, taskID)

	task, _ := s.DB.GetSwarmTask(ctx, taskID)
	spans, _ := s.DB.GetSwarmSpanTree(ctx, taskID)
	artifacts, _ := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	messages, _ := s.DB.GetSwarmTaskMessages(ctx, taskID)
	_ = patchSwarmDetail(sse, task, messages, spans, artifacts)

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
	tasks, _ := s.DB.ListSwarmTasks(ctx, defaultQueryLimit)
	_ = patchSwarmSidebar(sse, tasks, taskID)

	updatedTask, _ := s.DB.GetSwarmTask(ctx, taskID)
	spans, _ := s.DB.GetSwarmSpanTree(ctx, taskID)
	artifacts, _ := s.DB.GetSwarmArtifactsByTask(ctx, taskID)
	messages, _ := s.DB.GetSwarmTaskMessages(ctx, taskID)
	_ = patchSwarmDetail(sse, updatedTask, messages, spans, artifacts)

	return nil
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
		Role:      "user",
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
