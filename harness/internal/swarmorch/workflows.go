package swarmorch

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/linear"
)

// Workflow timeout and retry configuration.
const (
	agentStartToCloseTimeout = 20 * time.Minute
	agentHeartbeatTimeout    = 60 * time.Second
	agentMaxRetries          = 3
	agentRetryInterval       = 5 * time.Second

	infraStartToCloseTimeout = 30 * time.Second
	infraMaxRetries          = 3

	maxResearchQuestions = 5
)

// ResearchWorkflowInput is the input for the research workflow.
type ResearchWorkflowInput struct {
	TaskID        string `json:"taskID"`
	RequestText   string `json:"requestText"`
	RepoRoot      string `json:"repoRoot"`
	ParentSpanID  string `json:"parentSpanID,omitempty"`  // set when called as child workflow
	LinearIssueID string `json:"linearIssueID,omitempty"` // ticket ID for filename prefix
	HarnessURL    string `json:"harnessURL,omitempty"`
	TicketContext string `json:"ticketContext,omitempty"` // formatted ticket data for agent context
}

// CodeChangePlanWorkflowInput is the input for the code change plan workflow.
type CodeChangePlanWorkflowInput struct {
	TaskID        string `json:"taskID"`
	RequestText   string `json:"requestText"`
	RepoRoot      string `json:"repoRoot"`
	LinearIssueID string `json:"linearIssueID,omitempty"` // ticket ID for filename prefix
	HarnessURL    string `json:"harnessURL,omitempty"`
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
	if err := encoded.Get(&id); err != nil {
		workflow.GetLogger(ctx).Warn("failed to generate span ID", "error", err)
	}
	return id
}

// deferredCleanup runs cleanup activities when a workflow fails or is canceled.
// Must be called from a deferred closure that reads workflowErr at execution time.
// When isChild is true, task-level status updates and events are skipped
// (the parent workflow manages those).
func deferredCleanup(
	ctx workflow.Context,
	a *SwarmActivities,
	spanID string,
	taskID string,
	isChild bool,
	workflowErr error,
) {
	if workflowErr == nil {
		return
	}
	cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
	cleanupInfra := workflow.WithActivityOptions(cleanupCtx, infraActivityOpts())
	logger := workflow.GetLogger(ctx)

	if err := workflow.ExecuteActivity(
		cleanupInfra, a.FailSpanActivity,
		spanID, workflowErr.Error(),
	).Get(cleanupCtx, nil); err != nil {
		logger.Warn("cleanup: failed to fail span", "spanID", spanID, "error", err)
	}
	// Close all orphaned running spans for this task.
	if err := workflow.ExecuteActivity(
		cleanupInfra, a.FailRunningSpansByTask,
		taskID, workflowErr.Error(),
	).Get(cleanupCtx, nil); err != nil {
		logger.Warn(
			"cleanup: failed to fail running spans",
			"taskID",
			taskID,
			"error",
			err,
		)
	}

	if isChild {
		return
	}

	status := sqlc.TaskStatusFailed
	event := "task.failed"
	if ctx.Err() != nil {
		status = sqlc.TaskStatusCanceled
		event = "task.canceled"
	}
	if err := workflow.ExecuteActivity(
		cleanupInfra, a.UpdateTaskStatus,
		taskID, status,
	).Get(cleanupCtx, nil); err != nil {
		logger.Warn(
			"cleanup: failed to update task status",
			"taskID",
			taskID,
			"error",
			err,
		)
	}
	if err := workflow.ExecuteActivity(
		cleanupInfra, a.EmitEvent,
		taskID, event, workflowErr.Error(),
	).Get(cleanupCtx, nil); err != nil {
		logger.Warn(
			"cleanup: failed to emit event",
			"taskID",
			taskID,
			"event",
			event,
			"error",
			err,
		)
	}
}

// pst is the America/Los_Angeles timezone used for output file prefixes.
var pst = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("PST", -8*60*60) //nolint:mnd // UTC-8 fallback
	}
	return loc
}()

// swarmOutputPath builds a path under thoughts/swarm/ for a given artifact category.
// The datetime prefix uses PST for human-readable filenames.
func swarmOutputPath(
	category, slug, ext string,
	startTime time.Time,
	ticketID string,
) string {
	prefix := startTime.In(pst).Format("2006-01-02_15-04-05")
	if ticketID != "" {
		prefix += "_" + ticketID
	}
	prefix += "_" + sanitizeSlug(slug)
	return filepath.Join("thoughts", "swarm", category, prefix+ext)
}

// createStageSpan creates a stage span and returns its ID and startedAt timestamp.
func createStageSpan(
	ctx workflow.Context,
	a *SwarmActivities,
	taskID, parentSpanID, name string,
) (string, string) {
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	stageID := newSpanID(ctx)
	startedAt := workflow.Now(ctx).UTC().Format(time.RFC3339)
	if err := workflow.ExecuteActivity(infraCtx, a.CreateSpanActivity, SpanParams{
		ID:           stageID,
		TaskID:       taskID,
		ParentSpanID: parentSpanID,
		SpanType:     sqlc.SpanTypeStage,
		Name:         name,
		StartedAt:    startedAt,
	}).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to create stage span", "name", name, "error", err)
	}
	return stageID, startedAt
}

// completeStageSpan marks a stage span as completed.
func completeStageSpan(
	ctx workflow.Context,
	a *SwarmActivities,
	spanID, startedAt string,
) {
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(
		infraCtx, a.CompleteSpanActivity,
		spanID, startedAt, "",
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to complete stage span", "spanID", spanID, "error", err)
	}
}

// artifactURL builds the harness URL for an artifact, suitable for Linear attachment links.
func artifactURL(harnessURL, artifactID string) string {
	if harnessURL == "" {
		return "/swarm/artifacts/" + artifactID + "/view"
	}
	return strings.TrimRight(harnessURL, "/") + "/swarm/artifacts/" + artifactID + "/view"
}

// runLinearActivity is a fire-and-forget helper for Linear integration activities.
// It logs warnings on failure but does not fail the workflow. The activity itself
// no-ops when ticketID is empty or LinearClient is nil.
func runLinearActivity(
	ctx workflow.Context,
	activityFn any,
	args ...any,
) {
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(infraCtx, activityFn, args...).
		Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("linear activity failed (non-fatal)", "error", err)
	}
}

// fetchTicketContext fetches the Linear ticket and returns formatted context
// for injection into agent prompts. Returns empty string if ticketID is empty
// or fetch fails.
func fetchTicketContext(
	ctx workflow.Context,
	a *SwarmActivities,
	ticketID string,
) (string, linear.Issue) {
	if ticketID == "" {
		return "", linear.Issue{}
	}
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	var issue linear.Issue
	if err := workflow.ExecuteActivity(
		infraCtx, a.FetchLinearTicket, ticketID,
	).Get(ctx, &issue); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to fetch ticket context", "ticketID", ticketID, "error", err)
		return "", linear.Issue{}
	}
	if issue.Title == "" {
		return "", issue
	}
	return formatTicketContext(issue), issue
}

// formatTicketContext formats a Linear issue into a markdown context string
// suitable for injection into agent system prompts.
func formatTicketContext(issue linear.Issue) string {
	var b strings.Builder
	b.WriteString("## Linear Ticket Context\n\n")
	b.WriteString("**" + issue.Identifier + "**: " + issue.Title + "\n")
	b.WriteString("**Status**: " + issue.State.Name + "\n")
	if len(issue.Labels.Nodes) > 0 {
		names := make([]string, 0, len(issue.Labels.Nodes))
		for _, l := range issue.Labels.Nodes {
			names = append(names, l.Name)
		}
		b.WriteString("**Labels**: " + strings.Join(names, ", ") + "\n")
	}
	if issue.Description != "" {
		b.WriteString("\n### Description\n\n" + issue.Description + "\n")
	}
	return b.String()
}

// runPostProcessor runs the linear-context-processor agent after a stage
// completion, posts its comment to Linear, and creates follow-up tickets.
// No-ops if ticketID is empty. Non-fatal — logs warnings but does not fail the workflow.
func runPostProcessor(
	ctx workflow.Context,
	a *SwarmActivities,
	input postProcessorInput,
) {
	if input.ticketID == "" {
		return
	}
	logger := workflow.GetLogger(ctx)
	startTime := workflow.Now(ctx).UTC()

	// Re-fetch ticket for latest comments.
	_, freshIssue := fetchTicketContext(ctx, a, input.ticketID)
	ticketJSON, jsonErr := json.Marshal(freshIssue)
	if jsonErr != nil {
		ticketJSON = []byte("{}")
	}

	// Run post-processor agent.
	outputPath := swarmOutputPath(
		"linear-context", input.artifactType+"-context", ".yaml",
		startTime, input.ticketID,
	)

	agentCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var result LinearContextOutput
	if err := workflow.ExecuteActivity(
		agentCtx, a.RunLinearContextProcessor,
		input.taskID, input.parentSpanID,
		LinearContextInput{
			TaskID:          input.taskID,
			TicketID:        input.ticketID,
			TicketData:      string(ticketJSON),
			ArtifactType:    input.artifactType,
			ArtifactContent: input.artifactContent,
			OutputPath:      outputPath,
			RepoRoot:        input.repoRoot,
		},
	).Get(ctx, &result); err != nil {
		logger.Warn("post-processor failed, using fallback comment", "error", err)
		// Fallback to the simple comment.
		runLinearActivity(ctx, a.AddLinearComment, input.ticketID, input.fallbackComment)
		return
	}

	// Post the structured comment.
	runLinearActivity(ctx, a.AddLinearComment, input.ticketID, result.Comment)

	// Create follow-up tickets (with dedup).
	for _, followup := range result.Followups {
		// Search for duplicates first.
		infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
		var existing []linear.SearchResult
		if err := workflow.ExecuteActivity(
			infraCtx, a.SearchLinearIssues, followup.Title,
		).Get(ctx, &existing); err != nil {
			logger.Warn(
				"failed to search for duplicate tickets",
				"title",
				followup.Title,
				"error",
				err,
			)
			continue
		}
		if len(existing) > 0 {
			logger.Info(
				"skipping duplicate follow-up",
				"title",
				followup.Title,
				"existing",
				existing[0].Identifier,
			)
			continue
		}

		var newID string
		if err := workflow.ExecuteActivity(
			infraCtx, a.CreateLinearFollowup,
			input.ticketID, followup.Title, followup.Description, followup.Relation,
		).Get(ctx, &newID); err != nil {
			logger.Warn(
				"failed to create follow-up ticket",
				"title",
				followup.Title,
				"error",
				err,
			)
		} else if newID != "" {
			logger.Info(
				"created follow-up ticket",
				"identifier",
				newID,
				"relation",
				followup.Relation,
			)
		}
	}
}

// postProcessorInput bundles parameters for the runPostProcessor helper.
type postProcessorInput struct {
	taskID          string
	ticketID        string
	parentSpanID    string
	artifactType    string // "research_doc" or "plan_doc"
	artifactContent string
	repoRoot        string
	fallbackComment string // used if post-processor fails
}

// ResearchStepsResult holds outputs from the shared research pipeline.
// Fields are exported for Temporal serialization across child workflow boundaries.
type ResearchStepsResult struct {
	Findings   []ResearchFinding `json:"findings"`
	Synthesize SynthesizeResult  `json:"synthesize"`
	OutputPath string            `json:"outputPath"`
}

// runResearchSteps executes the question-generation, parallel-research, and
// synthesis pipeline. Each phase is wrapped in a stage span for hierarchy visibility.
func runResearchSteps(
	ctx workflow.Context,
	input ResearchWorkflowInput,
	parentSpanID string,
) (ResearchStepsResult, error) {
	var a *SwarmActivities
	var zero ResearchStepsResult
	startTime := workflow.Now(ctx).UTC()

	// ── Stage: question_generation ──
	qgenStageID, qgenStartedAt := createStageSpan(
		ctx, a, input.TaskID, parentSpanID, "question_generation",
	)

	qgenCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var questions QuestionArtifact
	if err := workflow.ExecuteActivity(
		qgenCtx, a.GenerateResearchQuestions,
		input.TaskID, qgenStageID,
		GenerateQuestionsInput{
			TaskID:        input.TaskID,
			RequestText:   input.RequestText,
			RepoRoot:      input.RepoRoot,
			MaxQuestions:  maxResearchQuestions,
			TicketContext: input.TicketContext,
			OutputPath: swarmOutputPath(
				"research-questions", input.RequestText, ".yaml",
				startTime, input.LinearIssueID,
			),
		},
	).Get(ctx, &questions); err != nil {
		return zero, fmt.Errorf("generate research questions: %w", err)
	}

	completeStageSpan(ctx, a, qgenStageID, qgenStartedAt)

	// Narrative: questions generated.
	narrativeInfra := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(
		narrativeInfra,
		a.PostNarrativeMessage,
		input.TaskID,
		fmt.Sprintf(
			"Decomposed into %d research questions. Starting parallel research...",
			len(questions.Questions),
		),
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to post narrative message", "taskID", input.TaskID, "error", err)
	}

	// ── Stage: parallel_research ──
	researchStageID, researchStartedAt := createStageSpan(
		ctx, a, input.TaskID, parentSpanID, "parallel_research",
	)

	resultCh := workflow.NewChannel(ctx)
	for i, q := range questions.Questions {
		qIdx := i
		qText := q.Question
		workflow.Go(ctx, func(gCtx workflow.Context) {
			agentCtx := workflow.WithActivityOptions(gCtx, agentActivityOpts())
			var finding ResearchFinding
			err := workflow.ExecuteActivity(
				agentCtx, a.RunResearchAgent,
				input.TaskID, researchStageID,
				ResearchAgentInput{
					TaskID:        input.TaskID,
					Question:      qText,
					RepoRoot:      input.RepoRoot,
					AgentIndex:    qIdx,
					TicketContext: input.TicketContext,
					OutputPath: swarmOutputPath(
						"research-findings",
						fmt.Sprintf("q%d-%s", qIdx, qText),
						".md",
						startTime, input.LinearIssueID,
					),
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

	completeStageSpan(ctx, a, researchStageID, researchStartedAt)

	// Narrative: research complete.
	if err := workflow.ExecuteActivity(
		narrativeInfra,
		a.PostNarrativeMessage,
		input.TaskID,
		fmt.Sprintf(
			"All %d research agents finished. Synthesizing findings...",
			len(findings),
		),
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to post narrative message", "taskID", input.TaskID, "error", err)
	}

	// ── Stage: synthesis ──
	synthStageID, synthStartedAt := createStageSpan(
		ctx, a, input.TaskID, parentSpanID, "synthesis",
	)

	outputPath := swarmOutputPath(
		"research", input.RequestText, ".md",
		startTime, input.LinearIssueID,
	)

	synthCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var synthesize SynthesizeResult
	if err := workflow.ExecuteActivity(
		synthCtx, a.SynthesizeResearchDoc,
		input.TaskID, synthStageID,
		SynthesizeInput{
			TaskID:      input.TaskID,
			RequestText: input.RequestText,
			Findings:    findings,
			OutputPath:  outputPath,
		},
	).Get(ctx, &synthesize); err != nil {
		return zero, fmt.Errorf("synthesize research doc: %w", err)
	}

	completeStageSpan(ctx, a, synthStageID, synthStartedAt)

	return ResearchStepsResult{
		Findings:   findings,
		Synthesize: synthesize,
		OutputPath: outputPath,
	}, nil
}

// ResearchWorkflow orchestrates a research task: question generation,
// parallel research agents, and synthesis into a document.
// When called as a child workflow (ParentSpanID is set), it skips task-level
// status management and nests its span tree under the parent.
func ResearchWorkflow(
	ctx workflow.Context,
	input ResearchWorkflowInput,
) (ResearchStepsResult, error) {
	var a *SwarmActivities
	var zero ResearchStepsResult
	isChild := input.ParentSpanID != ""
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())

	// Create workflow span — with parent if child workflow.
	spanID := newSpanID(ctx)
	startedAt := workflow.Now(ctx).UTC().Format(time.RFC3339)

	spanParams := SpanParams{
		ID:        spanID,
		TaskID:    input.TaskID,
		SpanType:  sqlc.SpanTypeWorkflow,
		Name:      "research",
		InputJSON: marshal(input),
		StartedAt: startedAt,
	}
	if isChild {
		spanParams.ParentSpanID = input.ParentSpanID
	}

	if err := workflow.ExecuteActivity(
		infraCtx, a.CreateSpanActivity, spanParams,
	).Get(ctx, nil); err != nil {
		return zero, fmt.Errorf("create workflow span: %w", err)
	}

	var workflowErr error
	defer func() { deferredCleanup(ctx, a, spanID, input.TaskID, isChild, workflowErr) }()

	// Only manage task status if standalone (not child).
	if !isChild {
		if err := workflow.ExecuteActivity(
			infraCtx, a.UpdateTaskStatus,
			input.TaskID, sqlc.TaskStatusRunning,
		).Get(ctx, nil); err != nil {
			workflowErr = fmt.Errorf("update task status: %w", err)
			return zero, workflowErr
		}
		if err := workflow.ExecuteActivity(
			infraCtx, a.EmitEvent,
			input.TaskID, "research.started", "",
		).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).
				Warn("failed to emit event", "taskID", input.TaskID, "event", "research.started", "error", err)
		}

		// Fetch ticket context for agent injection (standalone only).
		if input.TicketContext == "" && input.LinearIssueID != "" {
			ticketCtx, _ := fetchTicketContext(ctx, a, input.LinearIssueID)
			input.TicketContext = ticketCtx
		}

		// Linear: set status and labels at start (standalone only).
		runLinearActivity(ctx, a.UpdateLinearStatus, input.LinearIssueID, "In Progress")
		runLinearActivity(
			ctx,
			a.UpdateLinearLabels,
			input.LinearIssueID,
			[]string{"type:research", "swarm:research"},
		)
	}

	// Run research pipeline.
	result, err := runResearchSteps(ctx, input, spanID)
	if err != nil {
		workflowErr = err
		return zero, workflowErr
	}

	// Write document to disk.
	writeCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	if err := workflow.ExecuteActivity(
		writeCtx, a.WriteDocument,
		result.OutputPath, result.Synthesize.Document,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("write research document: %w", err)
		return zero, workflowErr
	}

	// Persist artifact reference.
	var researchArtifactID string
	if err := workflow.ExecuteActivity(
		infraCtx, a.PersistArtifact,
		input.TaskID, sqlc.ArtifactTypeResearchDoc, result.OutputPath,
	).Get(ctx, &researchArtifactID); err != nil {
		workflowErr = fmt.Errorf("persist artifact: %w", err)
		return zero, workflowErr
	}

	// Narrative: complete.
	if err := workflow.ExecuteActivity(
		infraCtx, a.PostNarrativeMessage,
		input.TaskID,
		"Research complete. Document written to "+result.OutputPath,
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to post narrative message", "taskID", input.TaskID, "error", err)
	}

	// Complete workflow span.
	if err := workflow.ExecuteActivity(
		infraCtx, a.CompleteSpanActivity,
		spanID, startedAt, truncateJSON(result.Synthesize),
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to complete workflow span", "spanID", spanID, "error", err)
	}

	// Only manage task status if standalone (not child).
	if !isChild {
		// Linear: run post-processor, link artifact, remove swarm label, mark done.
		runPostProcessor(ctx, a, postProcessorInput{
			taskID:          input.TaskID,
			ticketID:        input.LinearIssueID,
			parentSpanID:    spanID,
			artifactType:    "research_doc",
			artifactContent: result.Synthesize.Document,
			repoRoot:        input.RepoRoot,
			fallbackComment: "## Research Complete\n\nDocument: `" + result.OutputPath + "`\n\nSummary: " + result.Synthesize.Summary,
		})
		if researchArtifactID != "" {
			runLinearActivity(ctx, a.LinkArtifactToLinear, input.LinearIssueID,
				"Research Doc", artifactURL(input.HarnessURL, researchArtifactID))
		}
		runLinearActivity(
			ctx,
			a.UpdateLinearLabels,
			input.LinearIssueID,
			[]string{"type:research"},
		)
		runLinearActivity(ctx, a.UpdateLinearStatus, input.LinearIssueID, "Done")

		if err := workflow.ExecuteActivity(
			infraCtx, a.UpdateTaskStatus,
			input.TaskID, sqlc.TaskStatusCompleted,
		).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).
				Warn("failed to update task status to completed", "taskID", input.TaskID, "error", err)
		}
		if err := workflow.ExecuteActivity(
			infraCtx, a.EmitEvent,
			input.TaskID, "task.completed", "",
		).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).
				Warn("failed to emit task.completed event", "taskID", input.TaskID, "error", err)
		}
	}

	return result, nil
}

// CodeChangePlanWorkflow orchestrates a code change plan: research (as child
// workflow), domain classification, specialist planning, and synthesis.
func CodeChangePlanWorkflow(
	ctx workflow.Context,
	input CodeChangePlanWorkflowInput,
) error {
	var a *SwarmActivities
	infraCtx := workflow.WithActivityOptions(ctx, infraActivityOpts())
	startTime := workflow.Now(ctx).UTC()

	// Create workflow span.
	spanID := newSpanID(ctx)
	startedAt := startTime.Format(time.RFC3339)

	if err := workflow.ExecuteActivity(
		infraCtx, a.CreateSpanActivity,
		SpanParams{
			ID:        spanID,
			TaskID:    input.TaskID,
			SpanType:  sqlc.SpanTypeWorkflow,
			Name:      "code_change_plan",
			InputJSON: marshal(input),
			StartedAt: startedAt,
		},
	).Get(ctx, nil); err != nil {
		return fmt.Errorf("create workflow span: %w", err)
	}

	var workflowErr error
	defer func() { deferredCleanup(ctx, a, spanID, input.TaskID, false, workflowErr) }()

	// Mark task running.
	if err := workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, sqlc.TaskStatusRunning,
	).Get(ctx, nil); err != nil {
		workflowErr = fmt.Errorf("update task status: %w", err)
		return workflowErr
	}
	if err := workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "code_plan.started", "",
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to emit event", "taskID", input.TaskID, "event", "code_plan.started", "error", err)
	}

	// Fetch ticket context for agent injection.
	var ticketContext string
	if input.LinearIssueID != "" {
		ticketContext, _ = fetchTicketContext(ctx, a, input.LinearIssueID)
	}

	// Linear: set status and labels at start.
	runLinearActivity(ctx, a.UpdateLinearStatus, input.LinearIssueID, "In Progress")
	runLinearActivity(
		ctx,
		a.UpdateLinearLabels,
		input.LinearIssueID,
		[]string{"type:code-change", "swarm:research"},
	)

	// ── Research phase (child workflow) ──
	childOpts := workflow.ChildWorkflowOptions{
		WorkflowID:               "swarm-research-child-" + input.TaskID,
		WorkflowExecutionTimeout: researchWorkflowTimeout,
	}
	childCtx := workflow.WithChildOptions(ctx, childOpts)

	var research ResearchStepsResult
	if err := workflow.ExecuteChildWorkflow(childCtx, ResearchWorkflow, ResearchWorkflowInput{
		TaskID:        input.TaskID,
		RequestText:   input.RequestText,
		RepoRoot:      input.RepoRoot,
		ParentSpanID:  spanID,
		LinearIssueID: input.LinearIssueID,
		HarnessURL:    input.HarnessURL,
		TicketContext: ticketContext,
	}).
		Get(ctx, &research); err != nil {
		workflowErr = fmt.Errorf("research child workflow: %w", err)
		return workflowErr
	}

	// Linear: research done — run post-processor, transition to planning.
	runPostProcessor(ctx, a, postProcessorInput{
		taskID:          input.TaskID,
		ticketID:        input.LinearIssueID,
		parentSpanID:    spanID,
		artifactType:    "research_doc",
		artifactContent: research.Synthesize.Document,
		repoRoot:        input.RepoRoot,
		fallbackComment: "## Research Complete\n\nDocument: `" + research.OutputPath + "`\n\nSummary: " + research.Synthesize.Summary,
	})
	runLinearActivity(
		ctx,
		a.UpdateLinearLabels,
		input.LinearIssueID,
		[]string{"type:code-change", "swarm:planning"},
	)

	// ── Planning phase (stage span) ──
	planStageID, planStageStartedAt := createStageSpan(
		ctx, a, input.TaskID, spanID, "planning",
	)

	// Classify domains.
	classifyCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var classifyResult ClassifyResult
	if err := workflow.ExecuteActivity(
		classifyCtx, a.ClassifyPlanDomains,
		input.TaskID, planStageID,
		ClassifyInput{
			TaskID:          input.TaskID,
			RequestText:     input.RequestText,
			ResearchDocPath: research.OutputPath,
			RepoRoot:        input.RepoRoot,
			TicketContext:   ticketContext,
			OutputPath: swarmOutputPath(
				"plan-classifications", input.RequestText, ".yaml",
				startTime, input.LinearIssueID,
			),
		},
	).Get(ctx, &classifyResult); err != nil {
		workflowErr = fmt.Errorf("classify plan domains: %w", err)
		return workflowErr
	}

	// Narrative: domains classified.
	var domainNames []string
	for _, p := range classifyResult.Planners {
		domainNames = append(domainNames, p.Type)
	}
	if err := workflow.ExecuteActivity(
		infraCtx,
		a.PostNarrativeMessage,
		input.TaskID,
		fmt.Sprintf(
			"Identified %d specialist domains: %s. Running planners...",
			len(classifyResult.Planners),
			strings.Join(domainNames, ", "),
		),
	).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).
			Warn("failed to post narrative message", "taskID", input.TaskID, "error", err)
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
				input.TaskID, planStageID,
				SpecialistInput{
					TaskID:        input.TaskID,
					Domain:        domain,
					Focus:         focus,
					RequestText:   input.RequestText,
					ResearchDoc:   research.Synthesize.Document,
					RepoRoot:      input.RepoRoot,
					TicketContext: ticketContext,
					OutputPath: swarmOutputPath(
						"specialist-plans", domain, ".md",
						startTime, input.LinearIssueID,
					),
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
	planOutputPath := swarmOutputPath(
		"project-plans", input.RequestText, ".md",
		startTime, input.LinearIssueID,
	)

	synthCtx := workflow.WithActivityOptions(ctx, agentActivityOpts())
	var planResult PlanSynthesizeResult
	if err := workflow.ExecuteActivity(
		synthCtx, a.SynthesizePlanDoc,
		input.TaskID, planStageID,
		PlanSynthesizeInput{
			TaskID:             input.TaskID,
			RequestText:        input.RequestText,
			ResearchDocSummary: research.Synthesize.Summary,
			PlannerOutputs:     plannerOutputs,
			OutputPath:         planOutputPath,
		},
	).Get(ctx, &planResult); err != nil {
		workflowErr = fmt.Errorf("synthesize plan doc: %w", err)
		return workflowErr
	}

	completeStageSpan(ctx, a, planStageID, planStageStartedAt)

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
	var planArtifactID string
	if err := workflow.ExecuteActivity(
		infraCtx, a.PersistArtifact,
		input.TaskID, sqlc.ArtifactTypePlanDoc, planOutputPath,
	).Get(ctx, &planArtifactID); err != nil {
		workflowErr = fmt.Errorf("persist artifact: %w", err)
		return workflowErr
	}

	// Linear: plan complete — run post-processor, link artifact, remove swarm label, set to In Review.
	runPostProcessor(ctx, a, postProcessorInput{
		taskID:          input.TaskID,
		ticketID:        input.LinearIssueID,
		parentSpanID:    spanID,
		artifactType:    "plan_doc",
		artifactContent: planResult.Document,
		repoRoot:        input.RepoRoot,
		fallbackComment: "## Plan Complete\n\nDocument: `" + planOutputPath + "`\n\nSummary: " + planResult.Summary,
	})
	if planArtifactID != "" {
		runLinearActivity(ctx, a.LinkArtifactToLinear, input.LinearIssueID,
			"Implementation Plan", artifactURL(input.HarnessURL, planArtifactID))
	}
	runLinearActivity(
		ctx,
		a.UpdateLinearLabels,
		input.LinearIssueID,
		[]string{"type:code-change"},
	)
	runLinearActivity(ctx, a.UpdateLinearStatus, input.LinearIssueID, "In Review")

	// Narrative: complete.
	logger := workflow.GetLogger(ctx)
	if err := workflow.ExecuteActivity(
		infraCtx, a.PostNarrativeMessage,
		input.TaskID,
		"Code change plan complete. Document written to "+planOutputPath,
	).Get(ctx, nil); err != nil {
		logger.Warn(
			"failed to post narrative message",
			"taskID",
			input.TaskID,
			"error",
			err,
		)
	}

	// Complete workflow.
	if err := workflow.ExecuteActivity(
		infraCtx, a.UpdateTaskStatus,
		input.TaskID, sqlc.TaskStatusCompleted,
	).Get(ctx, nil); err != nil {
		logger.Warn(
			"failed to update task status to completed",
			"taskID",
			input.TaskID,
			"error",
			err,
		)
	}
	if err := workflow.ExecuteActivity(
		infraCtx, a.EmitEvent,
		input.TaskID, "task.completed", "",
	).Get(ctx, nil); err != nil {
		logger.Warn(
			"failed to emit task.completed event",
			"taskID",
			input.TaskID,
			"error",
			err,
		)
	}
	if err := workflow.ExecuteActivity(
		infraCtx, a.CompleteSpanActivity,
		spanID, startedAt, truncateJSON(planResult),
	).Get(ctx, nil); err != nil {
		logger.Warn("failed to complete workflow span", "spanID", spanID, "error", err)
	}

	return nil
}
