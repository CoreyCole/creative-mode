package swarmorch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
	tlog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/events"
)

// Temporal service configuration.
const (
	temporalHostPort = "localhost:7233"
	temporalNS       = "swarm"
	taskQueue        = "swarm-agents"
	nodePath         = "/home/deploy/.nix-profile/bin/node"

	researchWorkflowTimeout = 1 * time.Hour
	codePlanWorkflowTimeout = 2 * time.Hour
)

// SwarmManager manages the Temporal client and worker for swarm workflows.
type SwarmManager struct {
	client     client.Client
	worker     worker.Worker
	activities *SwarmActivities
	repoRoot   string
	logger     *slog.Logger
}

// NewSwarmManager creates a SwarmManager, connects to Temporal, registers
// workflows and activities, and starts the worker.
func NewSwarmManager(
	database *db.DB,
	eventBus *events.EventBus,
	repoRoot string,
	agentsDir string,
	logger *slog.Logger,
) (*SwarmManager, error) {
	c, err := client.Dial(client.Options{
		HostPort:  temporalHostPort,
		Namespace: temporalNS,
		Logger:    tlog.NewStructuredLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("dial temporal: %w", err)
	}

	runner := &DirectRunner{
		NodePath:  nodePath,
		AgentsDir: agentsDir,
	}

	activities := NewSwarmActivities(
		database, eventBus, repoRoot, agentsDir, runner, logger,
	)

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(ResearchWorkflow)
	w.RegisterWorkflow(CodeChangePlanWorkflow)
	w.RegisterActivity(activities)

	if startErr := w.Start(); startErr != nil {
		c.Close()
		return nil, fmt.Errorf("start temporal worker: %w", startErr)
	}

	logger.Info("swarm temporal worker started",
		"namespace", temporalNS,
		"queue", taskQueue,
	)

	return &SwarmManager{
		client:     c,
		worker:     w,
		activities: activities,
		repoRoot:   repoRoot,
		logger:     logger,
	}, nil
}

// StartResearch starts a research workflow for the given task.
func (m *SwarmManager) StartResearch(
	ctx context.Context,
	taskID string,
	requestText string,
) (string, error) {
	workflowID := "swarm-research-" + taskID
	opts := client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                taskQueue,
		WorkflowExecutionTimeout: researchWorkflowTimeout,
	}

	_, err := m.client.ExecuteWorkflow(ctx, opts, ResearchWorkflow,
		ResearchWorkflowInput{
			TaskID:      taskID,
			RequestText: requestText,
			RepoRoot:    m.repoRoot,
		},
	)
	if err != nil {
		return "", fmt.Errorf("start research workflow: %w", err)
	}

	m.logger.Info("started research workflow",
		"workflowID", workflowID,
		"taskID", taskID,
	)
	return workflowID, nil
}

// StartCodePlan starts a code change plan workflow for the given task.
func (m *SwarmManager) StartCodePlan(
	ctx context.Context,
	taskID string,
	requestText string,
) (string, error) {
	workflowID := "swarm-codeplan-" + taskID
	opts := client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                taskQueue,
		WorkflowExecutionTimeout: codePlanWorkflowTimeout,
	}

	_, err := m.client.ExecuteWorkflow(ctx, opts, CodeChangePlanWorkflow,
		CodeChangePlanWorkflowInput{
			TaskID:      taskID,
			RequestText: requestText,
			RepoRoot:    m.repoRoot,
		},
	)
	if err != nil {
		return "", fmt.Errorf("start code plan workflow: %w", err)
	}

	m.logger.Info("started code plan workflow",
		"workflowID", workflowID,
		"taskID", taskID,
	)
	return workflowID, nil
}

// CancelTask cancels a running workflow by its ID.
func (m *SwarmManager) CancelTask(ctx context.Context, workflowID string) error {
	return m.client.CancelWorkflow(ctx, workflowID, "")
}

// Stop gracefully shuts down the worker and closes the Temporal client.
func (m *SwarmManager) Stop() {
	m.worker.Stop()
	m.client.Close()
	m.logger.Info("swarm temporal worker stopped")
}
