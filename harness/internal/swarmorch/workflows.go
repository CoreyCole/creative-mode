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

// HeartbeatWorkflow runs maintenance tasks and spawns sessions for pending work.
// Executed every 2min by the Temporal schedule on the ops queue.
func HeartbeatWorkflow(ctx workflow.Context) error {
	opsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           QueueOps,
		StartToCloseTimeout: maintenanceActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	// Run maintenance activities sequentially.
	if err := workflow.ExecuteActivity(opsCtx, "DetectStalls").Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("DetectStalls failed", "error", err)
	}

	if err := workflow.ExecuteActivity(opsCtx, "ReapSessions").Get(ctx, nil); err != nil {
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

	if err := workflow.ExecuteActivity(opsCtx, "CheckProjectProgress").
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("CheckProjectProgress failed", "error", err)
	}

	// Read pending work and spawn child workflows.
	var spawns []SpawnRequest
	if err := workflow.ExecuteActivity(opsCtx, "ReadTicketQueue").
		Get(ctx, &spawns); err != nil {
		return fmt.Errorf("ReadTicketQueue: %w", err)
	}

	for _, sp := range spawns {
		params := SessionParams(sp)

		// Route verify phase to the verify queue; everything else to general.
		taskQueue := QueueGeneral
		if sp.Phase == swarm.PhaseVerify || sp.Phase == swarm.PhaseProjectVerify {
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
