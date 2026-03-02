package swarmorch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

const (
	// Task queue names.
	QueueGeneral = "swarm-general"
	QueueVerify  = "swarm-verify"
	QueueOps     = "swarm-ops"

	// Concurrency limits per queue.
	concurrencyGeneral = 3
	concurrencyVerify  = 1
	concurrencyOps     = 1

	// Heartbeat schedule ID and interval.
	heartbeatScheduleID = "swarm-heartbeat"
	heartbeatInterval   = 2 * time.Minute

	// Temporal namespace.
	temporalNamespace = "swarm"

	// Timeouts.
	triggerTimeout  = 5 * time.Second
	scheduleTimeout = 10 * time.Second
)

// TemporalEnabled returns true if CM_SWARM_TEMPORAL=true.
func TemporalEnabled() bool {
	return os.Getenv("CM_SWARM_TEMPORAL") == "true"
}

// TemporalRuntime holds the Temporal client, workers, and schedule handle.
type TemporalRuntime struct {
	client         client.Client
	workers        []worker.Worker
	scheduleHandle client.ScheduleHandle
	logger         *slog.Logger
}

// NewTemporalClient connects to a Temporal server with the swarm namespace.
func NewTemporalClient(addr string, logger *slog.Logger) (client.Client, error) {
	if addr == "" {
		addr = "127.0.0.1:7233"
	}

	c, err := client.Dial(client.Options{
		HostPort:  addr,
		Namespace: temporalNamespace,
		Logger:    newTemporalLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("dial temporal at %s: %w", addr, err)
	}

	return c, nil
}

// StartRuntime creates workers for each task queue, registers workflows and
// activities, starts the workers, and creates the heartbeat schedule.
func StartRuntime(
	c client.Client,
	mgr *Manager,
	logger *slog.Logger,
) (*TemporalRuntime, error) {
	activities := &Activities{mgr: mgr}

	// Create workers for each queue.
	generalWorker := worker.New(c, QueueGeneral, worker.Options{
		MaxConcurrentActivityExecutionSize: concurrencyGeneral,
	})
	generalWorker.RegisterWorkflow(HeartbeatWorkflow)
	generalWorker.RegisterWorkflow(LeadFDEWorkflow)
	generalWorker.RegisterWorkflow(SessionWorkflow)
	generalWorker.RegisterWorkflow(ProjectOrchestratorWorkflow)
	generalWorker.RegisterActivity(activities)

	verifyWorker := worker.New(c, QueueVerify, worker.Options{
		MaxConcurrentActivityExecutionSize: concurrencyVerify,
	})
	verifyWorker.RegisterWorkflow(SessionWorkflow)
	verifyWorker.RegisterActivity(activities)

	opsWorker := worker.New(c, QueueOps, worker.Options{
		MaxConcurrentActivityExecutionSize: concurrencyOps,
	})
	opsWorker.RegisterWorkflow(HeartbeatWorkflow)
	opsWorker.RegisterWorkflow(LeadFDEWorkflow)
	opsWorker.RegisterWorkflow(ProjectOrchestratorWorkflow)
	opsWorker.RegisterActivity(activities)

	workers := []worker.Worker{generalWorker, verifyWorker, opsWorker}

	for _, w := range workers {
		if err := w.Start(); err != nil {
			// Stop any already-started workers.
			for _, started := range workers {
				started.Stop()
			}

			return nil, fmt.Errorf("start worker: %w", err)
		}
	}

	// Create or update the heartbeat schedule.
	scheduleHandle, err := ensureHeartbeatSchedule(c, logger)
	if err != nil {
		for _, w := range workers {
			w.Stop()
		}

		return nil, fmt.Errorf("heartbeat schedule: %w", err)
	}

	logger.Info("temporal runtime started",
		"queues", []string{QueueGeneral, QueueVerify, QueueOps})

	return &TemporalRuntime{
		client:         c,
		workers:        workers,
		scheduleHandle: scheduleHandle,
		logger:         logger,
	}, nil
}

// TriggerHeartbeat triggers an immediate heartbeat execution.
func (rt *TemporalRuntime) TriggerHeartbeat() {
	if rt == nil || rt.scheduleHandle == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), triggerTimeout)
	defer cancel()

	if err := rt.scheduleHandle.Trigger(ctx, client.ScheduleTriggerOptions{
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	}); err != nil {
		rt.logger.Error("trigger heartbeat", "error", err)
	}
}

// Stop gracefully stops all workers and closes the client.
func (rt *TemporalRuntime) Stop() {
	if rt == nil {
		return
	}

	for _, w := range rt.workers {
		w.Stop()
	}

	rt.client.Close()
	rt.logger.Info("temporal runtime stopped")
}

// ensureHeartbeatSchedule creates or updates the heartbeat schedule.
func ensureHeartbeatSchedule(
	c client.Client,
	logger *slog.Logger,
) (client.ScheduleHandle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scheduleTimeout)
	defer cancel()

	scheduleSpec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{
			{Every: heartbeatInterval},
		},
	}

	scheduleAction := &client.ScheduleWorkflowAction{
		ID:        "swarm-heartbeat-run",
		Workflow:  LeadFDEWorkflow,
		TaskQueue: QueueOps,
	}

	// Try to create the schedule first.
	handle, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      heartbeatScheduleID,
		Spec:    scheduleSpec,
		Action:  scheduleAction,
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		// Schedule may already exist — try to get and update it.
		handle = c.ScheduleClient().GetHandle(ctx, heartbeatScheduleID)
		updateErr := handle.Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &scheduleSpec
				input.Description.Schedule.Action = scheduleAction

				return &client.ScheduleUpdate{
					Schedule: &input.Description.Schedule,
				}, nil
			},
		})
		if updateErr != nil {
			return nil, fmt.Errorf(
				"create/update schedule: create=%w, update=%w",
				err,
				updateErr,
			)
		}

		logger.Info("heartbeat schedule updated", "id", heartbeatScheduleID)

		return handle, nil
	}

	logger.Info("heartbeat schedule created",
		"id", heartbeatScheduleID, "interval", heartbeatInterval)

	return handle, nil
}

// temporalLogger adapts slog.Logger to Temporal's log.Logger interface.
type temporalLogger struct {
	logger *slog.Logger
}

func newTemporalLogger(logger *slog.Logger) log.Logger {
	return &temporalLogger{logger: logger.With("component", "temporal")}
}

func (l *temporalLogger) Debug(msg string, keyvals ...any) {
	l.logger.Debug(msg, keyvals...)
}

func (l *temporalLogger) Info(msg string, keyvals ...any) {
	l.logger.Info(msg, keyvals...)
}

func (l *temporalLogger) Warn(msg string, keyvals ...any) {
	l.logger.Warn(msg, keyvals...)
}

func (l *temporalLogger) Error(msg string, keyvals ...any) {
	l.logger.Error(msg, keyvals...)
}
