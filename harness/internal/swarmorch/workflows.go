package swarmorch

import (
	"fmt"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"creative-mode/harness/internal/swarm"
)

const (
	// Activity timeouts.
	maintenanceActivityTimeout = 30 * time.Second
	sessionActivityTimeout     = 65 * time.Minute
	projectActivityTimeout     = 60 * time.Second

	// ProjectOrchestratorWorkflow polling interval and ContinueAsNew threshold.
	projectPollInterval     = 2 * time.Minute
	projectContinueAsNewMax = 100

	// Retry limits for project activities.
	projectActivityRetries = 2
)

// SessionParams identifies a session to spawn.
type SessionParams struct {
	WorkflowID string
	TicketID   string
	Phase      swarm.Phase
	Attempt    int64
}

// SessionWorkflowResult is the output of SessionWorkflow.
type SessionWorkflowResult struct {
	SessionID string
	Result    swarm.SessionResult
	Summary   string
}

// SpawnRequest is returned by ReadTicketQueue — one per workflow needing a session.
type SpawnRequest struct {
	WorkflowID string
	TicketID   string
	Phase      swarm.Phase
	Attempt    int64
}

// SessionWorkflow wraps a single Claude Code session as a child workflow.
func SessionWorkflow(
	ctx workflow.Context,
	params SessionParams,
) (SessionWorkflowResult, error) {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           workflow.GetInfo(ctx).TaskQueueName,
		StartToCloseTimeout: sessionActivityTimeout,
		HeartbeatTimeout:    heartbeatInterval,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	var result SessionWorkflowResult
	if err := workflow.ExecuteActivity(activityCtx, "RunClaudeSession", params).
		Get(ctx, &result); err != nil {
		// Activity failed — return infra failure but complete the workflow cleanly.
		return SessionWorkflowResult{
			Result:  swarm.ResultInfraFailure,
			Summary: fmt.Sprintf("activity error: %v", err),
		}, nil
	}

	return result, nil
}

// ProjectOrchestratorParams identifies a project to orchestrate.
type ProjectOrchestratorParams struct {
	WorkflowID string
	ProjectID  string
	TicketID   string
}

// ProjectProgressResult is the output of AdvanceProject.
type ProjectProgressResult struct {
	AllComplete    bool
	TotalTickets   int
	CompletedCount int
	StartedCount   int
}

// ProjectHealthStatus summarizes a single project's health.
type ProjectHealthStatus struct {
	WorkflowID     string
	TicketID       string
	Phase          swarm.Phase
	TotalTickets   int
	CompletedCount int
	AllComplete    bool
}

// ProjectUpdateParams identifies a project for status updates.
type ProjectUpdateParams struct {
	TicketID       string
	TotalTickets   int
	CompletedCount int
}

// ProjectOrchestratorWorkflow manages a single project's lifecycle.
// Runs as a long-lived workflow, waking every 2 minutes to:
// 1. Check child ticket statuses
// 2. Advance ready tickets (start next wave)
// 3. Post Linear progress comment when wave advances
// 4. Complete when all children are done and project_verify passes
// Uses ContinueAsNew every 100 iterations to avoid history buildup.
func ProjectOrchestratorWorkflow(
	ctx workflow.Context,
	params ProjectOrchestratorParams,
) error {
	logger := workflow.GetLogger(ctx)

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           QueueOps,
		StartToCloseTimeout: projectActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: projectActivityRetries,
		},
	})

	for range projectContinueAsNewMax {
		// Sleep for the poll interval.
		if err := workflow.Sleep(ctx, projectPollInterval); err != nil {
			return err
		}

		// Advance the project: check children, start next wave.
		var progress ProjectProgressResult
		if err := workflow.ExecuteActivity(
			actCtx, "AdvanceProject", params,
		).Get(ctx, &progress); err != nil {
			logger.Warn("AdvanceProject failed", "error", err)

			continue
		}

		// Post a Linear update when new tickets were started or project completed.
		if progress.StartedCount > 0 || progress.AllComplete {
			_ = workflow.ExecuteActivity(actCtx, "PostProjectUpdate",
				ProjectUpdateParams{
					TicketID:       params.TicketID,
					TotalTickets:   progress.TotalTickets,
					CompletedCount: progress.CompletedCount,
				},
			).Get(ctx, nil)
		}

		if progress.AllComplete {
			logger.Info("project complete",
				"project_id", params.ProjectID,
				"total", progress.TotalTickets)

			return nil
		}
	}

	// ContinueAsNew to avoid workflow history buildup.
	return workflow.NewContinueAsNewError(ctx, ProjectOrchestratorWorkflow, params)
}

// LeadFDEWorkflow is the global overseer. Replaces HeartbeatWorkflow with
// added project orchestrator health checks. Runs on the ops queue every
// 2 minutes via the heartbeat schedule.
func LeadFDEWorkflow(ctx workflow.Context) error {
	opsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           QueueOps,
		StartToCloseTimeout: maintenanceActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	// Run maintenance activities sequentially.
	if err := workflow.ExecuteActivity(opsCtx, "DetectStalls").
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("DetectStalls failed", "error", err)
	}

	if err := workflow.ExecuteActivity(opsCtx, "ReapSessions").
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("ReapSessions failed", "error", err)
	}

	if err := workflow.ExecuteActivity(opsCtx, "DecayLearnings").
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("DecayLearnings failed", "error", err)
	}

	if err := workflow.ExecuteActivity(opsCtx, "GenerateDigest").
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("GenerateDigest failed", "error", err)
	}

	// Check project health — log warnings for stalled projects.
	// Individual project orchestrator workflows handle advancement;
	// this is the oversight layer that detects stalled orchestrators.
	var projects []ProjectHealthStatus
	if err := workflow.ExecuteActivity(opsCtx, "CheckProjectHealth").
		Get(ctx, &projects); err != nil {
		workflow.GetLogger(ctx).Warn(
			"CheckProjectHealth failed",
			"error", err,
		)
	}

	// Read pending work and spawn child workflows for non-project work.
	var spawns []SpawnRequest
	if err := workflow.ExecuteActivity(opsCtx, "ReadTicketQueue").
		Get(ctx, &spawns); err != nil {
		return fmt.Errorf("ReadTicketQueue: %w", err)
	}

	for _, sp := range spawns {
		params := SessionParams(sp)

		// Route verify phase to the verify queue; everything else to general.
		taskQueue := QueueGeneral
		if sp.Phase == swarm.PhaseVerify ||
			sp.Phase == swarm.PhaseProjectVerify {
			taskQueue = QueueVerify
		}

		childOpts := workflow.ChildWorkflowOptions{
			WorkflowID: fmt.Sprintf(
				"session-%s-%s-%d",
				sp.WorkflowID,
				sp.Phase,
				sp.Attempt,
			),
			TaskQueue:         taskQueue,
			ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
		}
		childCtx := workflow.WithChildOptions(ctx, childOpts)

		// Fire-and-forget: start but don't wait for completion.
		workflow.ExecuteChildWorkflow(childCtx, SessionWorkflow, params)
	}

	return nil
}
