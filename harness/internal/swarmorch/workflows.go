package swarmorch

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Workflow timeout and retry configuration.
const (
	agentStartToCloseTimeout = 20 * time.Minute
	agentHeartbeatTimeout    = 20 * time.Minute
	agentMaxRetries          = 3
	agentRetryInterval       = 5 * time.Second

	infraStartToCloseTimeout = 30 * time.Second
	infraMaxRetries          = 3

	maxResearchQuestions = 5
)

// ResearchWorkflowInput is the input for the research workflow.
type ResearchWorkflowInput struct {
	TaskID      string `json:"taskID"`
	RequestText string `json:"requestText"`
	RepoRoot    string `json:"repoRoot"`
}

// CodeChangePlanWorkflowInput is the input for the code change plan workflow.
type CodeChangePlanWorkflowInput struct {
	TaskID      string `json:"taskID"`
	RequestText string `json:"requestText"`
	RepoRoot    string `json:"repoRoot"`
}

// agentActivityOpts returns activity options for long-running agent activities.
func agentActivityOpts() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: agentStartToCloseTimeout,
		HeartbeatTimeout:    agentHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: agentMaxRetries,
			InitialInterval: agentRetryInterval,
		},
	}
}

// infraActivityOpts returns activity options for short infrastructure activities.
func infraActivityOpts() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: infraStartToCloseTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: infraMaxRetries,
		},
	}
}

// newSpanID generates a deterministic-safe UUID via workflow.SideEffect.
func newSpanID(ctx workflow.Context) string {
	var id string
	encoded := workflow.SideEffect(ctx, func(_ workflow.Context) any {
		return uuid.NewString()
	})
	_ = encoded.Get(&id)
	return id
}

// deferredCleanup runs cleanup activities when a workflow fails or is canceled.
// Must be called from a deferred closure that reads workflowErr at execution time.
func deferredCleanup(
	ctx workflow.Context,
	a *SwarmActivities,
	spanID string,
	taskID string,
	workflowErr error,
) {
	if workflowErr == nil {
		return
	}
	cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
	cleanupInfra := workflow.WithActivityOptions(cleanupCtx, infraActivityOpts())

	status := "failed"
	event := "task.failed"
	if ctx.Err() != nil {
		status = "canceled"
		event = "task.canceled"
	}

	_ = workflow.ExecuteActivity(
		cleanupInfra, a.FailSpanActivity,
		spanID, workflowErr.Error(),
	).Get(cleanupCtx, nil)
	_ = workflow.ExecuteActivity(
		cleanupInfra, a.UpdateTaskStatus,
		taskID, status,
	).Get(cleanupCtx, nil)
	_ = workflow.ExecuteActivity(
		cleanupInfra, a.EmitEvent,
		taskID, event, workflowErr.Error(),
	).Get(cleanupCtx, nil)
}

// researchStepsResult holds outputs from the shared research pipeline.
type researchStepsResult struct {
	findings   []ResearchFinding
	synthesize SynthesizeResult
	outputPath string
}

// runResearchSteps executes the question-generation, parallel-research, and
// synthesis pipeline shared by both workflows.
func runResearchSteps(
	ctx workflow.Context,
	input ResearchWorkflowInput,
	parentSpanID string,
) (researchStepsResult, error) {
	var a *SwarmActivities
	var zero researchStepsResult

	// Stage: question_generation
	qgenCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var questions QuestionArtifact
	if err := workflow.ExecuteActivity(
		qgenCtx, a.GenerateResearchQuestions,
		input.TaskID, parentSpanID,
		GenerateQuestionsInput{
			TaskID:       input.TaskID,
			RequestText:  input.RequestText,
			RepoRoot:     input.RepoRoot,
			MaxQuestions: maxResearchQuestions,
		},
	).Get(ctx, &questions); err != nil {
		return zero, fmt.Errorf("generate research questions: %w", err)
	}

	// Stage: parallel_research — fan-out one agent per question.
	resultCh := workflow.NewChannel(ctx)
	for i, q := range questions.Questions {
		qIdx := i
		qText := q.Question
		workflow.Go(ctx, func(gCtx workflow.Context) {
			agentCtx := workflow.WithActivityOptions(gCtx, agentActivityOpts())
			var finding ResearchFinding
			err := workflow.ExecuteActivity(
				agentCtx, a.RunResearchAgent,
				input.TaskID, parentSpanID,
				ResearchAgentInput{
					TaskID:     input.TaskID,
					Question:   qText,
					RepoRoot:   input.RepoRoot,
					AgentIndex: qIdx,
				},
			).Get(gCtx, &finding)
			if err != nil {
				finding = ResearchFinding{
					Question:   qText,
					Findings:   fmt.Sprintf("error: %v", err),
					Confidence: "low",
				}
			}
			resultCh.Send(gCtx, finding)
		})
	}

	// Collect all findings.
	findings := make([]ResearchFinding, 0, len(questions.Questions))
	for range questions.Questions {
		var finding ResearchFinding
		resultCh.Receive(ctx, &finding)
		findings = append(findings, finding)
	}

	// Stage: synthesis
	slug := sanitizeSlug(input.RequestText)
	outputPath := filepath.Join(
		"thoughts", "swarm", "research", slug+".md",
	)

	synthCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var synthesize SynthesizeResult
	if err := workflow.ExecuteActivity(
		synthCtx, a.SynthesizeResearchDoc,
		input.TaskID, parentSpanID,
		SynthesizeInput{
			TaskID:      input.TaskID,
			RequestText: input.RequestText,
			Findings:    findings,
			OutputPath:  outputPath,
		},
	).Get(ctx, &synthesize); err != nil {
		return zero, fmt.Errorf("synthesize research doc: %w", err)
	}

	return researchStepsResult{
		findings:   findings,
		synthesize: synthesize,
		outputPath: outputPath,
	}, nil
}

// ResearchWorkflow orchestrates a research task: question generation,
// parallel research agents, and synthesis into a document.
func ResearchWorkflow(ctx workflow.Context, input ResearchWorkflowInput) error {
	var a *SwarmActivities
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())

	// Create workflow span.
	spanID := newSpanID(ctx)
	startedAt := workflow.Now(ctx).UTC().Format(time.RFC3339)

	if err := workflow.ExecuteActivity(
		infraCtx, a.CreateSpanActivity,
		SpanParams{
			ID:        spanID,
			TaskID:    input.TaskID,
			SpanType:  "workflow",
			Name:      "research",
			InputJSON: marshal(input),
			StartedAt: startedAt,
		},
	).Get(ctx, nil); err != nil {
		return fmt.Errorf("create workflow span: %w", err)
	}

	var workflowErr error
	defer func() { deferredCleanup(ctx, a, spanID, input.TaskID, workflowErr) }()

	// Mark task running.
	if err := workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, "running",
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("update task status: %w", err)
		return workflowErr
	}
	_ = workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "research.started", "",
	).Get(ctx, nil)

	// Run research pipeline.
	result, err := runResearchSteps(ctx, input, spanID)
	if err != nil {
		workflowErr = err
		return workflowErr
	}

	// Write document to disk.
	writeCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(
		writeCtx, a.WriteDocument,
		result.outputPath, result.synthesize.Document,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("write research document: %w", err)
		return workflowErr
	}

	// Persist artifact reference.
	if err := workflow.ExecuteActivity(
		infraCtx, a.PersistArtifact,
		input.TaskID, "research_doc", result.outputPath,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("persist artifact: %w", err)
		return workflowErr
	}

	// Complete workflow.
	_ = workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, "completed",
	).Get(ctx, nil)
	_ = workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "task.completed", "",
	).Get(ctx, nil)
	_ = workflow.ExecuteActivity(
		infraCtx, a.CompleteSpanActivity,
		spanID, startedAt, truncateJSON(result.synthesize),
	).Get(ctx, nil)

	return nil
}

// CodeChangePlanWorkflow orchestrates a code change plan: research, domain
// classification, specialist planning, and synthesis.
func CodeChangePlanWorkflow(
	ctx workflow.Context,
	input CodeChangePlanWorkflowInput,
) error {
	var a *SwarmActivities
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())

	// Create workflow span.
	spanID := newSpanID(ctx)
	startedAt := workflow.Now(ctx).UTC().Format(time.RFC3339)

	if err := workflow.ExecuteActivity(
		infraCtx, a.CreateSpanActivity,
		SpanParams{
			ID:        spanID,
			TaskID:    input.TaskID,
			SpanType:  "workflow",
			Name:      "code_change_plan",
			InputJSON: marshal(input),
			StartedAt: startedAt,
		},
	).Get(ctx, nil); err != nil {
		return fmt.Errorf("create workflow span: %w", err)
	}

	var workflowErr error
	defer func() { deferredCleanup(ctx, a, spanID, input.TaskID, workflowErr) }()

	// Mark task running.
	if err := workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, "running",
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("update task status: %w", err)
		return workflowErr
	}
	_ = workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "code_plan.started", "",
	).Get(ctx, nil)

	// Inline research steps (reuse shared pipeline, NOT child workflow).
	research, err := runResearchSteps(ctx, ResearchWorkflowInput(input), spanID)
	if err != nil {
		workflowErr = err
		return workflowErr
	}

	// Classify domains.
	classifyCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var classifyResult ClassifyResult
	if err := workflow.ExecuteActivity(
		classifyCtx, a.ClassifyPlanDomains,
		input.TaskID, spanID,
		ClassifyInput{
			TaskID:          input.TaskID,
			RequestText:     input.RequestText,
			ResearchDocPath: research.outputPath,
			RepoRoot:        input.RepoRoot,
		},
	).Get(ctx, &classifyResult); err != nil {
		workflowErr = fmt.Errorf("classify plan domains: %w", err)
		return workflowErr
	}

	// Fan-out specialist planners.
	planCh := workflow.NewChannel(ctx)
	for _, spec := range classifyResult.Planners {
		domain := spec.Type
		focus := spec.Focus
		workflow.Go(ctx, func(gCtx workflow.Context) {
			agentCtx := workflow.WithActivityOptions(gCtx, agentActivityOpts())
			var output PlannerOutput
			planErr := workflow.ExecuteActivity(
				agentCtx, a.RunSpecialistPlanner,
				input.TaskID, spanID,
				SpecialistInput{
					TaskID:      input.TaskID,
					Domain:      domain,
					Focus:       focus,
					RequestText: input.RequestText,
					ResearchDoc: research.synthesize.Summary,
					RepoRoot:    input.RepoRoot,
				},
			).Get(gCtx, &output)
			if planErr != nil {
				output = PlannerOutput{
					Domain:      domain,
					PlanSection: fmt.Sprintf("error: %v", planErr),
				}
			}
			planCh.Send(gCtx, output)
		})
	}

	plannerOutputs := make([]PlannerOutput, 0, len(classifyResult.Planners))
	for range classifyResult.Planners {
		var output PlannerOutput
		planCh.Receive(ctx, &output)
		plannerOutputs = append(plannerOutputs, output)
	}

	// Synthesize plan document.
	slug := sanitizeSlug(input.RequestText)
	planOutputPath := filepath.Join(
		"thoughts", "swarm", "project-plans", slug+".md",
	)

	synthCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var planResult PlanSynthesizeResult
	if err := workflow.ExecuteActivity(
		synthCtx, a.SynthesizePlanDoc,
		input.TaskID, spanID,
		PlanSynthesizeInput{
			TaskID:             input.TaskID,
			RequestText:        input.RequestText,
			ResearchDocSummary: research.synthesize.Summary,
			PlannerOutputs:     plannerOutputs,
			OutputPath:         planOutputPath,
		},
	).Get(ctx, &planResult); err != nil {
		workflowErr = fmt.Errorf("synthesize plan doc: %w", err)
		return workflowErr
	}

	// Write document to disk.
	writeCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(
		writeCtx, a.WriteDocument,
		planOutputPath, planResult.Document,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("write plan document: %w", err)
		return workflowErr
	}

	// Persist artifact reference.
	if err := workflow.ExecuteActivity(
		infraCtx, a.PersistArtifact,
		input.TaskID, "code_change_plan", planOutputPath,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("persist artifact: %w", err)
		return workflowErr
	}

	// Complete workflow.
	_ = workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, "completed",
	).Get(ctx, nil)
	_ = workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "task.completed", "",
	).Get(ctx, nil)
	_ = workflow.ExecuteActivity(
		infraCtx, a.CompleteSpanActivity,
		spanID, startedAt, truncateJSON(planResult),
	).Get(ctx, nil)

	return nil
}
