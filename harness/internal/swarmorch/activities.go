package swarmorch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
)

// DBActivities is intended to expose sqlc query methods as Temporal activities.
// NOTE: Cannot embed *sqlc.Queries directly because Temporal's RegisterActivity
// panics on WithTx() — its return signature (*Queries) doesn't match the
// expected activity pattern (context.Context, ...) -> (T, error).
// To use this pattern, add individual methods that delegate to sqlc queries.
type DBActivities struct {
	Queries *sqlc.Queries
}

// SwarmActivities groups all Temporal activity implementations.
type SwarmActivities struct {
	db        *db.DB
	eventBus  *events.EventBus
	repoRoot  string
	agentsDir string
	runner    AgentRunner
	logger    *slog.Logger
}

// NewSwarmActivities creates a new SwarmActivities instance.
func NewSwarmActivities(
	database *db.DB,
	eventBus *events.EventBus,
	repoRoot string,
	agentsDir string,
	runner AgentRunner,
	logger *slog.Logger,
) *SwarmActivities {
	return &SwarmActivities{
		db:        database,
		eventBus:  eventBus,
		repoRoot:  repoRoot,
		agentsDir: agentsDir,
		runner:    runner,
		logger:    logger,
	}
}

// --- Agent activities (delegate to runAgent) ---

// GenerateResearchQuestions runs research-questions.js to decompose a task.
func (a *SwarmActivities) GenerateResearchQuestions(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input GenerateQuestionsInput,
) (QuestionArtifact, error) {
	return runAgentActivity[QuestionArtifact](ctx, a, agentActivityInput{
		script:       "research-questions.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// RunResearchAgent runs research-agent.js for a single research question.
func (a *SwarmActivities) RunResearchAgent(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input ResearchAgentInput,
) (ResearchFinding, error) {
	return runAgentActivity[ResearchFinding](ctx, a, agentActivityInput{
		script:       "research-agent.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// SynthesizeResearchDoc runs research-synthesizer.js to combine findings.
func (a *SwarmActivities) SynthesizeResearchDoc(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input SynthesizeInput,
) (SynthesizeResult, error) {
	return runAgentActivity[SynthesizeResult](ctx, a, agentActivityInput{
		script:       "research-synthesizer.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// ClassifyPlanDomains runs plan-orchestrator.js to classify domains.
func (a *SwarmActivities) ClassifyPlanDomains(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input ClassifyInput,
) (ClassifyResult, error) {
	return runAgentActivity[ClassifyResult](ctx, a, agentActivityInput{
		script:       "plan-orchestrator.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// RunSpecialistPlanner runs specialist-planner.js for a domain.
func (a *SwarmActivities) RunSpecialistPlanner(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input SpecialistInput,
) (PlannerOutput, error) {
	return runAgentActivity[PlannerOutput](ctx, a, agentActivityInput{
		script:       "specialist-planner.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// SynthesizePlanDoc runs plan-synthesizer.js to combine planner outputs.
func (a *SwarmActivities) SynthesizePlanDoc(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input PlanSynthesizeInput,
) (PlanSynthesizeResult, error) {
	return runAgentActivity[PlanSynthesizeResult](ctx, a, agentActivityInput{
		script:       "plan-synthesizer.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
	})
}

// agentActivityInput bundles the parameters common to all agent activities.
type agentActivityInput struct {
	script       string
	taskID       string
	parentSpanID string
	input        any
}

// runAgentActivity is a generic helper that runs an agent and unmarshals
// the artifact into the target type T.
func runAgentActivity[T any](
	ctx context.Context,
	a *SwarmActivities,
	p agentActivityInput,
) (T, error) {
	var zero T

	result, err := runAgent(ctx, runAgentParams{
		script:       p.script,
		taskID:       p.taskID,
		parentSpanID: p.parentSpanID,
		input:        p.input,
		repoRoot:     a.repoRoot,
		runner:       a.runner,
		database:     a.db,
		eventBus:     a.eventBus,
		logger:       a.logger,
		heartbeat: func(details any) {
			activity.RecordHeartbeat(ctx, details)
		},
	})
	if err != nil {
		return zero, fmt.Errorf("%s: %w", p.script, err)
	}

	var artifact T
	if unmarshalErr := json.Unmarshal(
		result.ArtifactJSON,
		&artifact,
	); unmarshalErr != nil {
		return zero, fmt.Errorf("unmarshal %s artifact: %w", p.script, unmarshalErr)
	}

	return artifact, nil
}

// --- Infrastructure activities ---

// UpdateTaskStatus updates a swarm task's status in the DB.
func (a *SwarmActivities) UpdateTaskStatus(
	ctx context.Context,
	taskID string,
	status string,
) error {
	return a.db.UpdateSwarmTaskStatus(ctx, sqlc.UpdateSwarmTaskStatusParams{
		Status:    status,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		ID:        taskID,
	})
}

// PersistArtifact records an artifact reference in the DB.
func (a *SwarmActivities) PersistArtifact(
	ctx context.Context,
	taskID string,
	artifactType string,
	filePath string,
) error {
	return a.db.CreateSwarmArtifact(ctx, sqlc.CreateSwarmArtifactParams{
		ID:           uuid.NewString()[:8],
		TaskID:       taskID,
		ArtifactType: artifactType,
		FilePath:     filePath,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

// EmitEvent publishes a swarm event to the EventBus.
func (a *SwarmActivities) EmitEvent(
	ctx context.Context,
	taskID string,
	eventType string,
	detail string,
) error {
	if a.eventBus != nil {
		a.eventBus.Publish("swarm", map[string]any{
			"event":  eventType,
			"taskID": taskID,
			"detail": detail,
		})
	}
	return nil
}

// CreateSpanActivity creates a span record (for workflow/stage spans
// that are not created inside runAgent).
func (a *SwarmActivities) CreateSpanActivity(
	ctx context.Context,
	p SpanParams,
) error {
	return a.db.CreateSwarmSpan(ctx, sqlc.CreateSwarmSpanParams{
		ID:           p.ID,
		TaskID:       p.TaskID,
		ParentSpanID: toNullString(p.ParentSpanID),
		SpanType:     p.SpanType,
		Name:         p.Name,
		Status:       "running",
		InputJSON:    sqlNullJSON(p.InputJSON),
		StartedAt:    p.StartedAt,
	})
}

// CompleteSpanActivity marks a span as completed.
func (a *SwarmActivities) CompleteSpanActivity(
	ctx context.Context,
	spanID string,
	startedAt string,
	outputJSON string,
) error {
	started, parseErr := time.Parse(time.RFC3339, startedAt)
	if parseErr != nil {
		started = time.Now().UTC()
	}
	now := time.Now().UTC()
	durationMs := now.Sub(started).Milliseconds()

	return a.db.CompleteSwarmSpan(ctx, sqlc.CompleteSwarmSpanParams{
		OutputJSON: toNullString(outputJSON),
		EndedAt:    toNullString(now.Format(time.RFC3339)),
		DurationMs: sql.NullInt64{Int64: durationMs, Valid: true},
		ID:         spanID,
	})
}

// FailSpanActivity marks a span as failed.
func (a *SwarmActivities) FailSpanActivity(
	ctx context.Context,
	spanID string,
	errMsg string,
) error {
	now := time.Now().UTC()
	return a.db.FailSwarmSpan(ctx, sqlc.FailSwarmSpanParams{
		ErrorMessage: toNullString(errMsg),
		EndedAt:      toNullString(now.Format(time.RFC3339)),
		DurationMs:   sql.NullInt64{Int64: 0, Valid: true},
		ID:           spanID,
	})
}

// WriteDocument writes content to a file on disk, creating parent dirs.
func (a *SwarmActivities) WriteDocument(
	ctx context.Context,
	path string,
	content string,
) error {
	fullPath := filepath.Join(a.repoRoot, path)
	if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o750); mkdirErr != nil {
		return fmt.Errorf("mkdir for document: %w", mkdirErr)
	}
	if writeErr := os.WriteFile(fullPath, []byte(content), 0o600); writeErr != nil {
		return fmt.Errorf("write document: %w", writeErr)
	}
	return nil
}
