package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	temporalClient "go.temporal.io/sdk/client"

	"creative-mode/harness/internal/db/sqlc"
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

// SpawnProjectChildren is called when a project workflow transitions from
// project_review to project_verify. It:
// 1. Parses the approved project plan
// 2. Creates child tickets (in DB and optionally in Linear)
// 3. Stores dependency edges
// 4. Creates milestones
// 5. Starts Wave 1 child workflows
func (m *Manager) SpawnProjectChildren(ctx context.Context, wf sqlc.SwarmWorkflow) error {
	// Find the project plan document.
	planPath, err := swarm.ResolvePlanPath(m.baseDir, wf.TicketID)
	if err != nil {
		return fmt.Errorf("resolve plan path: %w", err)
	}

	if planPath == "" {
		return fmt.Errorf("no project plan found for ticket %s", wf.TicketID)
	}

	//nolint:gosec // path from trusted resolver
	planBytes, readErr := os.ReadFile(planPath)
	if readErr != nil {
		return fmt.Errorf("read plan: %w", readErr)
	}

	planContent := string(planBytes)

	tickets, milestones := ParseProjectPlan(planContent)
	if len(tickets) == 0 {
		m.logger.Warn("project plan has no tickets", "workflow_id", wf.ID)

		return nil
	}

	projectID := wf.ID // Use the project workflow ID as the project grouping key.

	// Create child tickets and build num → identifier mapping.
	numToID := make(map[int]string, len(tickets))

	for _, pt := range tickets {
		childID := uuid.New().String()
		identifier := fmt.Sprintf("%s-%d", wf.TicketID, pt.Num)

		// Create in DB.
		if upsertErr := m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         childID,
			Identifier: identifier,
			Title:      pt.Title,
			Status:     "Todo",
			ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Url:        "", // Will be updated if Linear creates the ticket.
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		}); upsertErr != nil {
			return fmt.Errorf("upsert child ticket %d: %w", pt.Num, upsertErr)
		}

		numToID[pt.Num] = identifier

		// Create in Linear if available.
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
		// Update DB identifier to match Linear.
		_ = m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         childID,
			Identifier: linearID,
			Title:      pt.Title,
			Status:     "Todo",
			ParentID:   sql.NullString{String: wf.TicketID, Valid: true},
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Url:        "",
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		})
	}

	// Store dependency edges.
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

			// Also create in Linear.
			if m.linearClient != nil {
				_ = m.linearClient.AddDependency(ctx, ticketID, depID)
			}
		}
	}

	// Create milestones.
	for _, ms := range milestones {
		if msErr := m.db.CreateSwarmMilestone(ctx, sqlc.CreateSwarmMilestoneParams{
			ID:         uuid.New().String(),
			WorkflowID: wf.ID,
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			Name:       ms.Name,
			Criteria:   ms.Criteria,
			Status:     swarm.MilestoneStatusPending,
		}); msErr != nil {
			m.logger.Warn("create milestone", "name", ms.Name, "error", msErr)
		}
	}

	// Build dependency graph and start Wave 1.
	graph := m.buildProjectGraph(ctx, projectID)
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

	m.logger.Info("project children spawned",
		"workflow_id", wf.ID,
		"tickets", len(tickets),
		"milestones", len(milestones),
		"wave1", len(readyTickets),
	)

	m.linearComment(
		wf.TicketID,
		fmt.Sprintf(
			"📋 Project decomposed: %d child tickets, %d milestones, %d in Wave 1",
			len(tickets),
			len(milestones),
			len(readyTickets),
		),
	)

	// Start a ProjectOrchestratorWorkflow if Temporal is enabled.
	if m.temporalRuntime != nil {
		m.startProjectOrchestrator(ctx, wf)
	}

	return nil
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

		if wf.Phase != swarm.PhaseProjectVerify {
			continue
		}

		m.advanceProject(ctx, wf)
	}
}

// advanceProject checks a single project's child status and either starts
// new waves or triggers project_verify when all children are complete.
func (m *Manager) advanceProject(ctx context.Context, wf sqlc.SwarmWorkflow) {
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
