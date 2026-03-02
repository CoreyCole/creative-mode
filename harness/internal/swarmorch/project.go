package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	temporalClient "go.temporal.io/sdk/client"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/linear"
	"creative-mode/harness/internal/swarm"
)

// ProjectPlanTicket represents a parsed ticket from a project plan document.
type ProjectPlanTicket struct {
	Num          int
	Type         swarm.WorkflowType // "research" or "code"
	Title        string
	Dependencies []int // references to other ticket nums in the plan
}

// ProjectPlanMilestone represents a parsed milestone from a project plan document.
type ProjectPlanMilestone struct {
	Name     string
	Criteria string
}

// CreateProjectTicketsFromPlan is called when a project workflow transitions from
// project_plan to project_review. It creates child tickets and milestones from the
// plan but does NOT spawn any workflows — that happens after human approval via
// SpawnProjectWorkflows.
func (m *Manager) CreateProjectTicketsFromPlan(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
) error {
	planPath, tickets, milestones, err := m.readProjectPlan(wf)
	if err != nil {
		return err
	}

	_ = planPath

	if len(tickets) == 0 {
		m.logger.Warn("project plan has no tickets", "workflow_id", wf.ID)

		return nil
	}

	projectID := wf.ID

	numToID := make(map[int]string, len(tickets))

	for _, pt := range tickets {
		childID := uuid.New().String()
		identifier := fmt.Sprintf("%s-%d", wf.TicketID, pt.Num)

		if upsertErr := m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         childID,
			Identifier: identifier,
			Title:      pt.Title,
			Status:     linear.StatusTodo,
			ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Url:        "",
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		}); upsertErr != nil {
			return fmt.Errorf("upsert child ticket %d: %w", pt.Num, upsertErr)
		}

		numToID[pt.Num] = identifier

		if m.linearClient == nil {
			continue
		}

		linearID, linearErr := m.linearClient.CreateTicket(ctx, pt.Title, "", nil, "")
		if linearErr != nil {
			m.logger.Warn("linear create child ticket",
				"num", pt.Num, "error", linearErr)

			continue
		}

		numToID[pt.Num] = linearID

		_ = m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         childID,
			Identifier: linearID,
			Title:      pt.Title,
			Status:     linear.StatusTodo,
			ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Url:        "",
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		})
	}

	m.createDependencyEdges(ctx, tickets, numToID, projectID)
	m.createMilestones(ctx, wf.ID, projectID, milestones)

	m.logger.Info("project tickets created from plan",
		"workflow_id", wf.ID,
		"tickets", len(tickets),
		"milestones", len(milestones),
	)

	m.linearComment(
		wf.TicketID,
		fmt.Sprintf(
			"📋 Project planned: %d child tickets created, awaiting review",
			len(tickets),
		),
	)

	return nil
}

// ReconcileProjectTickets cleans up child tickets and dependencies when a project
// plan is rejected and loops back to project_plan. Linear tickets are left as-is
// (they can be updated or canceled separately).
func (m *Manager) ReconcileProjectTickets(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
) error {
	projectID := wf.ID

	if err := m.db.DeleteSwarmDependenciesByProject(ctx, projectID); err != nil {
		m.logger.Warn("reconcile: delete dependencies", "error", err)
	}

	if err := m.db.DeleteSwarmTicketsByProject(
		ctx,
		sql.NullString{String: projectID, Valid: true},
	); err != nil {
		m.logger.Warn("reconcile: delete tickets", "error", err)
	}

	m.logger.Info("project tickets reconciled",
		"workflow_id", wf.ID,
		"project_id", projectID,
	)

	return nil
}

// SpawnProjectWorkflows is called when a project workflow transitions from
// project_review to project_verify (after human approval). It uses the existing
// child tickets (created during CreateProjectTicketsFromPlan) and spawns Wave 1
// child workflows.
func (m *Manager) SpawnProjectWorkflows(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
) error {
	projectID := wf.ID

	graph := m.buildProjectGraph(ctx, projectID)
	if len(graph.Tickets) == 0 {
		m.logger.Warn("no child tickets found for project", "workflow_id", wf.ID)

		return nil
	}

	readyTickets := graph.ReadyTickets(map[string]bool{})

	for _, rt := range readyTickets {
		if _, startErr := m.StartWorkflow(
			ctx,
			rt.TicketID,
			rt.WorkflowType,
			"",
			"",
		); startErr != nil {
			m.logger.Warn("start child workflow",
				"ticket", rt.TicketID, "error", startErr)
		}
	}

	m.logger.Info("project workflows spawned",
		"workflow_id", wf.ID,
		"total_tickets", len(graph.Tickets),
		"wave1", len(readyTickets),
	)

	m.linearComment(
		wf.TicketID,
		fmt.Sprintf(
			"🚀 Project approved: %d child tickets, %d in Wave 1",
			len(graph.Tickets),
			len(readyTickets),
		),
	)

	if m.temporalRuntime != nil {
		m.startProjectOrchestrator(ctx, wf)
	}

	return nil
}

// readProjectPlan reads and parses the project plan document.
func (m *Manager) readProjectPlan(
	wf sqlc.SwarmWorkflow,
) (string, []ProjectPlanTicket, []ProjectPlanMilestone, error) {
	planPath, err := swarm.ResolvePlanPath(m.baseDir, wf.TicketID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve plan path: %w", err)
	}

	if planPath == "" {
		return "", nil, nil, fmt.Errorf(
			"no project plan found for ticket %s",
			wf.TicketID,
		)
	}

	//nolint:gosec // path from trusted resolver
	planBytes, readErr := os.ReadFile(planPath)
	if readErr != nil {
		return "", nil, nil, fmt.Errorf("read plan: %w", readErr)
	}

	tickets, milestones := ParseProjectPlan(string(planBytes))

	return planPath, tickets, milestones, nil
}

// createDependencyEdges stores dependency edges in DB and optionally in Linear.
func (m *Manager) createDependencyEdges(
	ctx context.Context,
	tickets []ProjectPlanTicket,
	numToID map[int]string,
	projectID string,
) {
	for _, pt := range tickets {
		for _, depNum := range pt.Dependencies {
			depID, ok := numToID[depNum]
			if !ok {
				continue
			}

			ticketID := numToID[pt.Num]

			if createErr := m.db.CreateSwarmDependency(
				ctx,
				sqlc.CreateSwarmDependencyParams{
					ID:                uuid.New().String(),
					TicketID:          ticketID,
					DependsOnTicketID: depID,
					ProjectID:         projectID,
				},
			); createErr != nil {
				m.logger.Warn("create dependency",
					"ticket", ticketID, "depends_on", depID, "error", createErr)
			}

			if m.linearClient != nil {
				_ = m.linearClient.AddDependency(ctx, ticketID, depID)
			}
		}
	}
}

// createMilestones stores project milestones in DB.
func (m *Manager) createMilestones(
	ctx context.Context,
	workflowID, projectID string,
	milestones []ProjectPlanMilestone,
) {
	for _, ms := range milestones {
		if msErr := m.db.CreateSwarmMilestone(ctx, sqlc.CreateSwarmMilestoneParams{
			ID:         uuid.New().String(),
			WorkflowID: workflowID,
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Name:       ms.Name,
			Criteria:   ms.Criteria,
			Status:     swarm.MilestoneStatusPending,
		}); msErr != nil {
			m.logger.Warn("create milestone", "name", ms.Name, "error", msErr)
		}
	}
}

// startProjectOrchestrator starts a long-lived Temporal workflow that
// manages a project's child ticket lifecycle.
func (m *Manager) startProjectOrchestrator(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
) {
	if m.temporalRuntime == nil || m.temporalRuntime.client == nil {
		return
	}

	orchID := "project-orch-" + wf.ID
	params := ProjectOrchestratorParams{
		WorkflowID: wf.ID,
		ProjectID:  wf.ID, // project ID == workflow ID
		TicketID:   wf.TicketID,
	}

	_, err := m.temporalRuntime.client.ExecuteWorkflow(
		ctx,
		temporalClient.StartWorkflowOptions{
			ID:        orchID,
			TaskQueue: QueueOps,
		},
		ProjectOrchestratorWorkflow,
		params,
	)
	if err != nil {
		m.logger.Warn("start project orchestrator",
			"workflow_id", wf.ID, "error", err)

		return
	}

	m.logger.Info("project orchestrator started",
		"orchestrator_id", orchID, "project_id", wf.ID)
}

// CheckProjectProgress checks all running project workflows to advance
// child ticket waves and detect completion.
func (m *Manager) CheckProjectProgress(ctx context.Context) {
	workflows, err := m.db.ListRunningSwarmWorkflows(ctx)
	if err != nil {
		m.logger.Warn("check project progress: list workflows", "error", err)

		return
	}

	for _, wf := range workflows {
		if wf.WorkflowType != swarm.WorkflowTypeProject {
			continue
		}

		if wf.Phase != swarm.PhaseProjectVerify &&
			wf.Phase != swarm.PhaseProjectDecompose {
			continue
		}

		m.advanceProject(ctx, wf)
	}
}

// advanceProject checks a single project's child status and either starts
// new waves or triggers the next phase when all children are complete.
func (m *Manager) advanceProject(ctx context.Context, wf sqlc.SwarmWorkflow) {
	if wf.Phase == swarm.PhaseProjectDecompose {
		m.advanceProjectDecompose(ctx, wf)

		return
	}

	m.advanceProjectVerify(ctx, wf)
}

// advanceProjectDecompose checks research children spawned during the decompose
// phase. When all complete, aggregates their findings and advances to project_plan.
func (m *Manager) advanceProjectDecompose(ctx context.Context, wf sqlc.SwarmWorkflow) {
	projectID := wf.ID
	graph := m.buildProjectGraph(ctx, projectID)

	if len(graph.Tickets) == 0 {
		return
	}

	completed := m.completedChildTickets(ctx, graph)
	if !graph.AllComplete(completed) {
		// Not all research children done — start newly-unblocked ones.
		ready := graph.ReadyTickets(completed)

		for _, rt := range ready {
			existing, existErr := m.db.GetSwarmWorkflowsByTicket(ctx, rt.TicketID)
			if existErr == nil && len(existing) > 0 {
				continue
			}

			if _, startErr := m.StartWorkflow(
				ctx, rt.TicketID, rt.WorkflowType, "", "",
			); startErr != nil {
				m.logger.Warn("start unblocked research child",
					"ticket", rt.TicketID, "error", startErr)
			}
		}

		return
	}

	// All research children complete — aggregate findings.
	aggregatedPath, aggErr := m.aggregateResearchFindings(ctx, wf, graph)
	if aggErr != nil {
		m.logger.Error("aggregate research findings",
			"workflow_id", wf.ID, "error", aggErr)
	}

	// Advance to project_plan.
	if phaseErr := m.db.UpdateSwarmWorkflowPhase(ctx, sqlc.UpdateSwarmWorkflowPhaseParams{
		ID:      wf.ID,
		Phase:   swarm.PhaseProjectPlan,
		Attempt: 1,
	}); phaseErr != nil {
		m.logger.Error("advance decompose to project_plan",
			"workflow_id", wf.ID, "error", phaseErr)

		return
	}

	m.emitEvent(ctx, wf.ID, "", wf.TicketID, swarm.EventPhaseComplete, wf.Phase, "")
	m.emitEvent(
		ctx,
		wf.ID,
		"",
		wf.TicketID,
		swarm.EventPhaseStarted,
		swarm.PhaseProjectPlan,
		"",
	)

	m.linearComment(
		wf.TicketID,
		"➡️ All research children complete — advancing to `project_plan`\nAggregated research: "+aggregatedPath,
	)

	// Refresh and spawn project_plan session.
	updatedWf, getErr := m.db.GetSwarmWorkflow(ctx, wf.ID)
	if getErr != nil {
		m.logger.Error("get updated workflow for project_plan", "error", getErr)

		return
	}

	if spawnErr := m.spawnSession(ctx, updatedWf); spawnErr != nil {
		m.logger.Error("spawn project_plan session",
			"workflow_id", wf.ID, "error", spawnErr)
	}
}

// advanceProjectVerify checks code children spawned during the verify phase
// and triggers project_verify when all children are complete.
func (m *Manager) advanceProjectVerify(ctx context.Context, wf sqlc.SwarmWorkflow) {
	projectID := wf.ID
	graph := m.buildProjectGraph(ctx, projectID)

	if len(graph.Tickets) == 0 {
		return
	}

	// Determine which children are complete.
	completed := m.completedChildTickets(ctx, graph)

	if graph.AllComplete(completed) {
		// All children done — check if a project_verify session is needed.
		session, sessionErr := m.db.GetLatestSwarmSession(ctx, wf.ID)
		if sessionErr != nil || session.Phase != swarm.PhaseProjectVerify {
			// No verify session yet — spawn one.
			if spawnErr := m.spawnSession(ctx, wf); spawnErr != nil {
				m.logger.Warn("spawn project verify",
					"workflow_id", wf.ID, "error", spawnErr)
			}
		}

		return
	}

	// Not all complete — start newly-unblocked tickets.
	ready := graph.ReadyTickets(completed)

	for _, rt := range ready {
		// Check if a workflow already exists for this ticket.
		existing, existErr := m.db.GetSwarmWorkflowsByTicket(ctx, rt.TicketID)
		if existErr == nil && len(existing) > 0 {
			continue // Already started.
		}

		if _, startErr := m.StartWorkflow(
			ctx, rt.TicketID, rt.WorkflowType, "", "",
		); startErr != nil {
			m.logger.Warn("start unblocked child",
				"ticket", rt.TicketID, "error", startErr)
		}
	}
}

// buildProjectGraph constructs a DependencyGraph from DB state for a project.
func (m *Manager) buildProjectGraph(
	ctx context.Context,
	projectID string,
) *swarm.DependencyGraph {
	tickets, err := m.db.ListSwarmTicketsByProject(
		ctx,
		sql.NullString{String: projectID, Valid: true},
	)
	if err != nil {
		m.logger.Warn("build project graph: list tickets", "error", err)

		return &swarm.DependencyGraph{}
	}

	deps, depErr := m.db.ListSwarmDependenciesByProject(ctx, projectID)
	if depErr != nil {
		m.logger.Warn("build project graph: list deps", "error", depErr)
	}

	graph := &swarm.DependencyGraph{
		Tickets: make([]swarm.TicketNode, 0, len(tickets)),
		Edges:   make([]swarm.DependencyEdge, 0, len(deps)),
	}

	for _, t := range tickets {
		wfType := swarm.WorkflowTypeCode // default

		if strings.Contains(strings.ToLower(t.Title), "research") {
			wfType = swarm.WorkflowTypeResearch
		}

		graph.Tickets = append(graph.Tickets, swarm.TicketNode{
			TicketID:     t.Identifier,
			WorkflowType: wfType,
			Title:        t.Title,
			Status:       t.Status,
		})
	}

	for _, d := range deps {
		graph.Edges = append(graph.Edges, swarm.DependencyEdge{
			From: d.DependsOnTicketID,
			To:   d.TicketID,
		})
	}

	return graph
}

// completedChildTickets returns a set of ticket IDs that have completed workflows.
func (m *Manager) completedChildTickets(
	ctx context.Context,
	graph *swarm.DependencyGraph,
) map[string]bool {
	completed := make(map[string]bool)

	for _, t := range graph.Tickets {
		wfs, err := m.db.GetSwarmWorkflowsByTicket(ctx, t.TicketID)
		if err != nil {
			continue
		}

		for _, wf := range wfs {
			if wf.Status == swarm.StatusComplete {
				completed[t.TicketID] = true

				break
			}
		}
	}

	return completed
}

// DecomposeResearchTopic represents a parsed research topic from a decompose output document.
type DecomposeResearchTopic struct {
	Num         int
	Title       string
	Description string
}

// ParseDecomposeOutput extracts research topics from a decompose session output document.
// Expected format:
//
//	## Research Topics
//
//	| # | Topic | Description |
//	|---|-------|-------------|
//	| 1 | Topic title | Research question |
func ParseDecomposeOutput(content string) []DecomposeResearchTopic {
	tableRe := regexp.MustCompile(
		`(?m)^\|\s*(\d+)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|`,
	)

	matches := tableRe.FindAllStringSubmatch(content, -1)
	topics := make([]DecomposeResearchTopic, 0, len(matches))

	for _, match := range matches {
		num := 0
		_, _ = fmt.Sscanf(match[1], "%d", &num)

		if num == 0 {
			continue // skip header row
		}

		topics = append(topics, DecomposeResearchTopic{
			Num:         num,
			Title:       strings.TrimSpace(match[2]),
			Description: strings.TrimSpace(match[3]),
		})
	}

	return topics
}

// SpawnProjectResearchChildren is called when a project workflow transitions
// from project_decompose to project_plan. It:
// 1. Parses the decompose output document
// 2. Creates child research tickets (in DB and optionally in Linear)
// 3. Spawns child research workflows
func (m *Manager) SpawnProjectResearchChildren(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
) error {
	decomposePath, err := swarm.ResolveDecomposePath(m.baseDir, wf.TicketID)
	if err != nil {
		return fmt.Errorf("resolve decompose path: %w", err)
	}

	if decomposePath == "" {
		return fmt.Errorf("no decompose document found for ticket %s", wf.TicketID)
	}

	//nolint:gosec // path from trusted resolver
	decomposeBytes, readErr := os.ReadFile(decomposePath)
	if readErr != nil {
		return fmt.Errorf("read decompose: %w", readErr)
	}

	topics := ParseDecomposeOutput(string(decomposeBytes))
	if len(topics) == 0 {
		m.logger.Info("decompose produced no research topics — advancing directly",
			"workflow_id", wf.ID)

		return nil
	}

	projectID := wf.ID

	for _, topic := range topics {
		childID := uuid.New().String()
		identifier := fmt.Sprintf("%s-r%d", wf.TicketID, topic.Num)

		if upsertErr := m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         childID,
			Identifier: identifier,
			Title:      topic.Title,
			Status:     linear.StatusTodo,
			ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Url:        "",
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		}); upsertErr != nil {
			return fmt.Errorf("upsert research child ticket %d: %w", topic.Num, upsertErr)
		}

		// Create in Linear if available.
		if m.linearClient != nil {
			linearID, linearErr := m.linearClient.CreateTicket(
				ctx, topic.Title, topic.Description, nil, "",
			)
			if linearErr != nil {
				m.logger.Warn("linear create research child ticket",
					"num", topic.Num, "error", linearErr)
			} else {
				identifier = linearID
				_ = m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
					ID:         childID,
					Identifier: identifier,
					Title:      topic.Title,
					Status:     linear.StatusTodo,
					ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
					ProjectID:  sql.NullString{String: projectID, Valid: true},
					Url:        "",
					CreatedAt:  nowUTC(),
					UpdatedAt:  nowUTC(),
				})
			}
		}

		// Spawn research workflow for each topic.
		if _, startErr := m.StartWorkflow(
			ctx, identifier, swarm.WorkflowTypeResearch, "", "",
		); startErr != nil {
			m.logger.Warn("start research child workflow",
				"ticket", identifier, "error", startErr)
		}
	}

	m.logger.Info("project research children spawned",
		"workflow_id", wf.ID,
		"topics", len(topics),
	)

	m.linearComment(
		wf.TicketID,
		fmt.Sprintf(
			"🔬 Research decomposition: %d child research workflows spawned",
			len(topics),
		),
	)

	return nil
}

// hasResearchChildren returns true if the project workflow has any child tickets.
func (m *Manager) hasResearchChildren(ctx context.Context, workflowID string) bool {
	graph := m.buildProjectGraph(ctx, workflowID)

	return len(graph.Tickets) > 0
}

// aggregateResearchFindings builds an aggregated research document from all
// completed child research workflows and writes it to thoughts/swarm/research-aggregated/.
func (m *Manager) aggregateResearchFindings(
	_ context.Context,
	wf sqlc.SwarmWorkflow,
	graph *swarm.DependencyGraph,
) (string, error) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Aggregated Research: %s\n\n", wf.TicketID))
	sb.WriteString(
		fmt.Sprintf(
			"Aggregated from %d child research workflows.\n\n",
			len(graph.Tickets),
		),
	)

	for _, t := range graph.Tickets {
		researchPath, err := swarm.ResolveResearchPath(m.baseDir, t.TicketID)
		if err != nil || researchPath == "" {
			sb.WriteString(
				fmt.Sprintf("## %s\n\n_No research document found._\n\n", t.Title),
			)

			continue
		}

		//nolint:gosec // path from trusted resolver
		content, readErr := os.ReadFile(researchPath)
		if readErr != nil {
			sb.WriteString(
				fmt.Sprintf(
					"## %s\n\n_Error reading research: %v_\n\n",
					t.Title,
					readErr,
				),
			)

			continue
		}

		sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", t.Title, t.TicketID))
		sb.WriteString(string(content))
		sb.WriteString("\n\n---\n\n")
	}

	// Write to thoughts/swarm/research-aggregated/.
	aggregatedDir := filepath.Join(m.baseDir, "thoughts", "swarm", "research-aggregated")
	if mkdirErr := os.MkdirAll(aggregatedDir, 0o750); mkdirErr != nil {
		return "", fmt.Errorf("create research-aggregated dir: %w", mkdirErr)
	}

	filename := swarm.FormatHandoffFilename(wf.TicketID, "aggregated")
	aggregatedPath := filepath.Join(aggregatedDir, filename)

	if writeErr := os.WriteFile(
		aggregatedPath,
		[]byte(sb.String()),
		0o600,
	); writeErr != nil {
		return "", fmt.Errorf("write aggregated research: %w", writeErr)
	}

	m.logger.Info("aggregated research written",
		"workflow_id", wf.ID,
		"path", aggregatedPath,
		"children", len(graph.Tickets),
	)

	return aggregatedPath, nil
}

// ParseProjectPlan extracts tickets and milestones from a project plan markdown document.
func ParseProjectPlan(content string) ([]ProjectPlanTicket, []ProjectPlanMilestone) {
	// Parse ticket decomposition table:
	// | # | Type | Title | Dependencies | Notes |
	// | 1 | research | Title | none | ... |
	tableRe := regexp.MustCompile(
		`(?m)^\|\s*(\d+)\s*\|\s*(research|code)\s*\|\s*([^|]+?)\s*\|\s*([^|]*?)\s*\|`,
	)

	tableMatches := tableRe.FindAllStringSubmatch(content, -1)
	tickets := make([]ProjectPlanTicket, 0, len(tableMatches))

	for _, match := range tableMatches {
		num := 0
		_, _ = fmt.Sscanf(match[1], "%d", &num)

		wfType := swarm.WorkflowTypeCode
		if strings.TrimSpace(match[2]) == "research" {
			wfType = swarm.WorkflowTypeResearch
		}

		var deps []int

		depStr := strings.TrimSpace(match[4])
		if depStr != "" && depStr != "none" {
			for _, d := range strings.Split(depStr, ",") {
				d = strings.TrimSpace(d)

				var depNum int
				if _, scanErr := fmt.Sscanf(d, "%d", &depNum); scanErr == nil {
					deps = append(deps, depNum)
				}
			}
		}

		tickets = append(tickets, ProjectPlanTicket{
			Num:          num,
			Type:         wfType,
			Title:        strings.TrimSpace(match[3]),
			Dependencies: deps,
		})
	}

	// Parse milestones:
	// - [ ] M1: Name — criteria
	milestoneRe := regexp.MustCompile(`(?m)^-\s*\[[ x]\]\s*M\d+:\s*(.+?)\s*[—–-]\s*(.+)$`)

	milestoneMatches := milestoneRe.FindAllStringSubmatch(content, -1)
	milestones := make([]ProjectPlanMilestone, 0, len(milestoneMatches))

	for _, match := range milestoneMatches {
		milestones = append(milestones, ProjectPlanMilestone{
			Name:     strings.TrimSpace(match[1]),
			Criteria: strings.TrimSpace(match[2]),
		})
	}

	return tickets, milestones
}

// nowUTC returns the current time as an RFC3339 string.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
