package swarmorch

import (
	"context"
	"database/sql"
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
	"creative-mode/harness/internal/linear"
)

// DBActivities is intended to expose sqlc query methods as Temporal activities.
// NOTE: Cannot embed *sqlc.Queries directly because Temporal's RegisterActivity
// panics on WithTx() — its return signature (*Queries) doesn't match the
// expected activity pattern (context.Context, ...) -> (T, error).
// To use this pattern, add individual methods that delegate to sqlc queries.
type DBActivities struct {
	Queries *sqlc.Queries
}

// SwarmConfig holds runtime configuration for swarm agents.
type SwarmConfig struct {
	ToolCallLimit int    // default 100
	Model         string // default "openai-codex:gpt-5.3-codex" (format: provider:model)
	HarnessURL    string // base URL for artifact links (e.g. "https://harness.ts.net")
}

// SwarmActivities groups all Temporal activity implementations.
type SwarmActivities struct {
	db             *db.DB
	eventBus       *events.EventBus
	repoRoot       string
	agentsDir      string
	runner         AgentRunner
	config         SwarmConfig
	logger         *slog.Logger
	projectContext string // cached result of loadProjectContext
	LinearClient   *linear.Client
}

// NewSwarmActivities creates a new SwarmActivities instance.
func NewSwarmActivities(
	database *db.DB,
	eventBus *events.EventBus,
	repoRoot string,
	agentsDir string,
	runner AgentRunner,
	config SwarmConfig,
	logger *slog.Logger,
) *SwarmActivities {
	return &SwarmActivities{
		db:             database,
		eventBus:       eventBus,
		repoRoot:       repoRoot,
		agentsDir:      agentsDir,
		runner:         runner,
		config:         config,
		logger:         logger,
		projectContext: loadProjectContext(repoRoot),
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
	return runAgentActivity[QuestionArtifact](
		ctx,
		a,
		agentActivityInput[QuestionArtifact]{
			script:       "research-questions.js",
			taskID:       taskID,
			parentSpanID: parentSpanID,
			input:        input,
			outputPath:   input.OutputPath,
			bodyField:    "",
			validate:     validateResearchQuestions,
		},
	)
}

// RunResearchAgent runs research-agent.js for a single research question.
func (a *SwarmActivities) RunResearchAgent(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input ResearchAgentInput,
) (ResearchFinding, error) {
	return runAgentActivity[ResearchFinding](ctx, a, agentActivityInput[ResearchFinding]{
		script:       "research-agent.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
		outputPath:   input.OutputPath,
		bodyField:    "findings",
		validate:     validateResearchFinding,
	})
}

// SynthesizeResearchDoc runs research-synthesizer.js to combine findings.
func (a *SwarmActivities) SynthesizeResearchDoc(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input SynthesizeInput,
) (SynthesizeResult, error) {
	return runAgentActivity[SynthesizeResult](
		ctx,
		a,
		agentActivityInput[SynthesizeResult]{
			script:       "research-synthesizer.js",
			taskID:       taskID,
			parentSpanID: parentSpanID,
			input:        input,
			outputPath:   input.OutputPath,
			bodyField:    "document",
			validate:     validateSynthesizeResult,
		},
	)
}

// ClassifyPlanDomains runs plan-orchestrator.js to classify domains.
func (a *SwarmActivities) ClassifyPlanDomains(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input ClassifyInput,
) (ClassifyResult, error) {
	return runAgentActivity[ClassifyResult](ctx, a, agentActivityInput[ClassifyResult]{
		script:       "plan-orchestrator.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
		outputPath:   input.OutputPath,
		bodyField:    "",
		validate:     validateClassifyResult,
	})
}

// RunSpecialistPlanner runs specialist-planner.js for a domain.
func (a *SwarmActivities) RunSpecialistPlanner(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input SpecialistInput,
) (PlannerOutput, error) {
	return runAgentActivity[PlannerOutput](ctx, a, agentActivityInput[PlannerOutput]{
		script:       "specialist-planner.js",
		taskID:       taskID,
		parentSpanID: parentSpanID,
		input:        input,
		outputPath:   input.OutputPath,
		bodyField:    "planSection",
		validate:     validatePlannerOutput,
	})
}

// SynthesizePlanDoc runs plan-synthesizer.js to combine planner outputs.
func (a *SwarmActivities) SynthesizePlanDoc(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input PlanSynthesizeInput,
) (PlanSynthesizeResult, error) {
	return runAgentActivity[PlanSynthesizeResult](
		ctx,
		a,
		agentActivityInput[PlanSynthesizeResult]{
			script:       "plan-synthesizer.js",
			taskID:       taskID,
			parentSpanID: parentSpanID,
			input:        input,
			outputPath:   input.OutputPath,
			bodyField:    "document",
			validate:     validatePlanSynthesizeResult,
		},
	)
}

// agentActivityInput bundles the parameters common to all agent activities.
type agentActivityInput[T any] struct {
	script       string
	taskID       string
	parentSpanID string
	input        any
	outputPath   string
	bodyField    string
	validate     func(T) error
}

// runAgentActivity is a generic helper that runs an agent, reads its output
// file, unmarshals the artifact, and validates it.
func runAgentActivity[T any](
	ctx context.Context,
	a *SwarmActivities,
	p agentActivityInput[T],
) (T, error) {
	var zero T

	// Resolve outputPath relative to repoRoot so Go reads from the same
	// absolute path that the JS agent writes to (its cwd is repoRoot).
	absOutputPath := p.outputPath
	if !filepath.IsAbs(absOutputPath) {
		absOutputPath = filepath.Join(a.repoRoot, absOutputPath)
	}

	// Ensure output directory exists.
	if err := os.MkdirAll(filepath.Dir(absOutputPath), 0o750); err != nil {
		return zero, fmt.Errorf("create output dir: %w", err)
	}

	// Build optional agent config from SwarmConfig.
	var agentCfg *AgentConfig
	if a.config.Model != "" {
		agentCfg = &AgentConfig{Model: a.config.Model}
	}

	_, err := runAgent(ctx, runAgentParams{
		script:         p.script,
		taskID:         p.taskID,
		parentSpanID:   p.parentSpanID,
		input:          p.input,
		projectContext: a.projectContext,
		repoRoot:       a.repoRoot,
		outputPath:     absOutputPath,
		runner:         a.runner,
		database:       a.db,
		eventBus:       a.eventBus,
		logger:         a.logger,
		toolCallLimit:  a.config.ToolCallLimit,
		agentConfig:    agentCfg,
		heartbeat: func(details any) {
			activity.RecordHeartbeat(ctx, details)
		},
	})
	if err != nil {
		return zero, fmt.Errorf("%s: %w", p.script, err)
	}

	// Format output file (normalize YAML front matter + markdown).
	if fmtErr := formatArtifact(absOutputPath); fmtErr != nil {
		a.logger.Warn("mdformat failed, proceeding with raw file",
			"error", fmtErr, "path", absOutputPath)
	}

	// Parse output file.
	artifact, unmarshalErr := unmarshalArtifact[T](absOutputPath, p.bodyField)
	if unmarshalErr != nil {
		return zero, fmt.Errorf("%s: %w", p.script, unmarshalErr)
	}

	// Validate.
	if p.validate != nil {
		if valErr := p.validate(artifact); valErr != nil {
			return zero, fmt.Errorf("%s validation: %w", p.script, valErr)
		}
	}

	return artifact, nil
}

// --- Infrastructure activities ---

// UpdateTaskStatus updates a swarm task's status in the DB.
func (a *SwarmActivities) UpdateTaskStatus(
	ctx context.Context,
	taskID string,
	status sqlc.TaskStatus,
) error {
	return a.db.UpdateSwarmTaskStatus(ctx, sqlc.UpdateSwarmTaskStatusParams{
		Status:    status,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		ID:        taskID,
	})
}

// PersistArtifact records an artifact reference in the DB and returns its ID.
func (a *SwarmActivities) PersistArtifact(
	ctx context.Context,
	taskID string,
	artifactType sqlc.ArtifactType,
	filePath string,
) (string, error) {
	id := uuid.NewString()[:8]
	return id, a.db.CreateSwarmArtifact(ctx, sqlc.CreateSwarmArtifactParams{
		ID:           id,
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
		Status:       sqlc.SpanStatusRunning,
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

// PostNarrativeMessage inserts an orchestrator message into swarm_task_messages.
func (a *SwarmActivities) PostNarrativeMessage(
	ctx context.Context,
	taskID string,
	content string,
) error {
	msgID := uuid.NewString()[:8]
	now := time.Now().UTC().Format(time.RFC3339)

	if err := a.db.CreateSwarmTaskMessage(ctx, sqlc.CreateSwarmTaskMessageParams{
		ID:        msgID,
		TaskID:    taskID,
		Role:      sqlc.MessageRoleOrchestrator,
		Content:   content,
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("post narrative: %w", err)
	}

	if a.eventBus != nil {
		a.eventBus.Publish("swarm", map[string]any{
			"event":  "narrative",
			"taskID": taskID,
		})
	}
	return nil
}

// FailRunningSpansByTask closes all running spans for a task as failed.
func (a *SwarmActivities) FailRunningSpansByTask(
	ctx context.Context,
	taskID string,
	errMsg string,
) error {
	return a.db.FailRunningSpansByTask(ctx, sqlc.FailRunningSpansByTaskParams{
		ErrorMessage: sql.NullString{String: errMsg, Valid: errMsg != ""},
		TaskID:       taskID,
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

// --- Linear integration activities ---
// All Linear activities no-op when LinearClient is nil or ticketID is empty.

// FetchLinearTicket fetches the full ticket from Linear.
func (a *SwarmActivities) FetchLinearTicket(
	ctx context.Context,
	ticketID string,
) (linear.Issue, error) {
	if a.LinearClient == nil || ticketID == "" {
		return linear.Issue{}, nil
	}
	issue, err := a.LinearClient.GetIssue(ctx, ticketID)
	if err != nil {
		return linear.Issue{}, fmt.Errorf("fetch linear ticket %s: %w", ticketID, err)
	}
	return *issue, nil
}

// UpdateLinearStatus changes the workflow state on the Linear ticket.
func (a *SwarmActivities) UpdateLinearStatus(
	ctx context.Context,
	ticketID string,
	status string,
) error {
	if a.LinearClient == nil || ticketID == "" {
		return nil
	}
	return a.LinearClient.UpdateStatus(ctx, ticketID, status)
}

// UpdateLinearLabels sets labels on the Linear ticket.
func (a *SwarmActivities) UpdateLinearLabels(
	ctx context.Context,
	ticketID string,
	labels []string,
) error {
	if a.LinearClient == nil || ticketID == "" {
		return nil
	}
	return a.LinearClient.UpdateLabels(ctx, ticketID, labels)
}

// AddLinearComment posts a structured comment to the Linear ticket.
func (a *SwarmActivities) AddLinearComment(
	ctx context.Context,
	ticketID string,
	body string,
) error {
	if a.LinearClient == nil || ticketID == "" {
		return nil
	}
	return a.LinearClient.AddComment(ctx, ticketID, body)
}

// LinkArtifactToLinear attaches an artifact URL to the Linear ticket.
func (a *SwarmActivities) LinkArtifactToLinear(
	ctx context.Context,
	ticketID string,
	title string,
	url string,
) error {
	if a.LinearClient == nil || ticketID == "" {
		return nil
	}
	return a.LinearClient.CreateAttachment(ctx, ticketID, title, url)
}

// CreateLinearFollowup creates a new research ticket and links it to the parent.
func (a *SwarmActivities) CreateLinearFollowup(
	ctx context.Context,
	parentID string,
	title string,
	description string,
	relationType string,
) (string, error) {
	if a.LinearClient == nil || parentID == "" {
		return "", nil
	}
	identifier, err := a.LinearClient.CreateIssue(ctx, title, linear.CreateOpts{
		Labels:      []string{"type:research"},
		Description: description,
	})
	if err != nil {
		return "", fmt.Errorf("create follow-up issue: %w", err)
	}
	if relErr := a.LinearClient.AddRelation(
		ctx,
		parentID,
		relationType,
		identifier,
	); relErr != nil {
		a.logger.Warn("failed to add relation to follow-up",
			"parent", parentID, "followup", identifier, "error", relErr)
	}
	return identifier, nil
}

// RunLinearContextProcessor runs the linear-context-processor.js agent
// to produce structured comments and follow-up recommendations.
func (a *SwarmActivities) RunLinearContextProcessor(
	ctx context.Context,
	taskID string,
	parentSpanID string,
	input LinearContextInput,
) (LinearContextOutput, error) {
	return runAgentActivity[LinearContextOutput](
		ctx,
		a,
		agentActivityInput[LinearContextOutput]{
			script:       "linear-context-processor.js",
			taskID:       taskID,
			parentSpanID: parentSpanID,
			input:        input,
			outputPath:   input.OutputPath,
			bodyField:    "",
			validate:     validateLinearContextOutput,
		},
	)
}

// SearchLinearIssues checks if a similar ticket already exists.
func (a *SwarmActivities) SearchLinearIssues(
	ctx context.Context,
	query string,
) ([]linear.SearchResult, error) {
	if a.LinearClient == nil {
		return []linear.SearchResult{}, nil
	}
	return a.LinearClient.SearchIssues(ctx, query)
}
