package swarmorch

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarm"
)

func TestParseDecomposeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    int // expected number of topics
	}{
		{
			name: "valid decompose output",
			content: `# Research Decomposition

## Research Topics

| # | Topic | Description |
|---|-------|-------------|
| 1 | State machine routing | How does the current state machine handle project workflows? |
| 2 | Skill architecture | What patterns exist for swarm skills? |
| 3 | Test infrastructure | How are swarm tests structured? |
`,
			want: 3,
		},
		{
			name:    "empty content",
			content: "",
			want:    0,
		},
		{
			name: "no table",
			content: `# Research Decomposition

Some text without a table.
`,
			want: 0,
		},
		{
			name: "table with header only",
			content: `## Research Topics

| # | Topic | Description |
|---|-------|-------------|
`,
			want: 0,
		},
		{
			name: "single topic",
			content: `| # | Topic | Description |
|---|-------|-------------|
| 1 | Single topic | Just one research area |
`,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			topics := ParseDecomposeOutput(tt.content)
			if len(topics) != tt.want {
				t.Errorf(
					"ParseDecomposeOutput() returned %d topics, want %d",
					len(topics),
					tt.want,
				)
			}
		})
	}
}

func TestParseDecomposeOutputFields(t *testing.T) {
	t.Parallel()

	content := `| # | Topic | Description |
|---|-------|-------------|
| 1 | State machine routing | How does the current state machine handle project workflows? |
| 2 | Skill architecture | What patterns exist for swarm skills? |
`

	topics := ParseDecomposeOutput(content)
	if len(topics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(topics))
	}

	if topics[0].Num != 1 {
		t.Errorf("topic[0].Num = %d, want 1", topics[0].Num)
	}

	if topics[0].Title != "State machine routing" {
		t.Errorf("topic[0].Title = %q, want %q", topics[0].Title, "State machine routing")
	}

	if topics[0].Description != "How does the current state machine handle project workflows?" {
		t.Errorf("topic[0].Description = %q", topics[0].Description)
	}

	if topics[1].Num != 2 {
		t.Errorf("topic[1].Num = %d, want 2", topics[1].Num)
	}

	if topics[1].Title != "Skill architecture" {
		t.Errorf("topic[1].Title = %q, want %q", topics[1].Title, "Skill architecture")
	}
}

func TestParseProjectPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		content        string
		wantTickets    int
		wantMilestones int
		checkTickets   func(t *testing.T, tickets []ProjectPlanTicket)
		checkMS        func(t *testing.T, milestones []ProjectPlanMilestone)
	}{
		{
			name: "basic_plan",
			content: `# Project Plan

## Ticket Decomposition

| # | Type | Title | Dependencies | Notes |
|---|------|-------|-------------|-------|
| 1 | research | Investigate auth patterns | none | first |
| 2 | code | Implement auth middleware | 1 | depends on research |
| 3 | code | Add login endpoint | 1, 2 | depends on both |

## Milestones

- [ ] M1: Auth foundation — All auth middleware and endpoints are functional
- [ ] M2: Integration complete — Login flow works end-to-end
`,
			wantTickets:    3,
			wantMilestones: 2,
			checkTickets: func(t *testing.T, tickets []ProjectPlanTicket) {
				t.Helper()
				if tickets[0].Type != swarm.WorkflowTypeResearch {
					t.Errorf("ticket 1 type = %q, want research", tickets[0].Type)
				}
				if tickets[1].Type != swarm.WorkflowTypeCode {
					t.Errorf("ticket 2 type = %q, want code", tickets[1].Type)
				}
				if len(tickets[0].Dependencies) != 0 {
					t.Errorf("ticket 1 deps = %v, want none", tickets[0].Dependencies)
				}
				if len(tickets[1].Dependencies) != 1 || tickets[1].Dependencies[0] != 1 {
					t.Errorf("ticket 2 deps = %v, want [1]", tickets[1].Dependencies)
				}
				if len(tickets[2].Dependencies) != 2 {
					t.Errorf("ticket 3 deps = %v, want [1, 2]", tickets[2].Dependencies)
				}
			},
			checkMS: func(t *testing.T, milestones []ProjectPlanMilestone) {
				t.Helper()
				if milestones[0].Name != "Auth foundation" {
					t.Errorf("milestone 1 name = %q", milestones[0].Name)
				}
				if milestones[1].Criteria != "Login flow works end-to-end" {
					t.Errorf("milestone 2 criteria = %q", milestones[1].Criteria)
				}
			},
		},
		{
			name:           "empty_plan",
			content:        "# Empty Project Plan\n\nNo tickets here.",
			wantTickets:    0,
			wantMilestones: 0,
		},
		{
			name: "plan_with_checked_milestones",
			content: `| # | Type | Title | Dependencies | Notes |
|---|------|-------|-------------|-------|
| 1 | code | Fix bug | none | quick fix |

- [x] M1: Bug fixed — No more crashes
`,
			wantTickets:    1,
			wantMilestones: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tickets, milestones := ParseProjectPlan(tt.content)

			if len(tickets) != tt.wantTickets {
				t.Errorf("tickets count = %d, want %d", len(tickets), tt.wantTickets)
			}
			if len(milestones) != tt.wantMilestones {
				t.Errorf("milestones count = %d, want %d", len(milestones), tt.wantMilestones)
			}

			if tt.checkTickets != nil {
				tt.checkTickets(t, tickets)
			}
			if tt.checkMS != nil {
				tt.checkMS(t, milestones)
			}
		})
	}
}

func TestCreateProjectTicketsFromPlan(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, baseDir, t.TempDir(), "")

	ctx := t.Context()

	// Create a project workflow.
	wfID := "wf-proj-create"
	ticketID := "CRE-10"
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           wfID,
		TicketID:     ticketID,
		WorkflowType: swarm.WorkflowTypeProject,
		Phase:        swarm.PhaseProjectPlan,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	// Write a project plan file that ResolvePlanPath will find.
	planDir := filepath.Join(baseDir, "thoughts", "swarm", "project-plans")
	if err := os.MkdirAll(planDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	planContent := `# Project Plan for CRE-10

## Ticket Decomposition

| # | Type | Title | Dependencies | Notes |
|---|------|-------|-------------|-------|
| 1 | research | Research API patterns | none | first |
| 2 | code | Implement new endpoint | 1 | needs research |
| 3 | code | Add tests | 2 | after impl |

## Milestones

- [ ] M1: API designed — Research complete and plan approved
- [ ] M2: Implementation done — Endpoint working with tests
`
	planFile := filepath.Join(planDir, "2026-03-01_12-00-00_CRE-10_plan.md")
	if err := os.WriteFile(planFile, []byte(planContent), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	wf, _ := database.GetSwarmWorkflow(ctx, wfID)

	err := mgr.CreateProjectTicketsFromPlan(ctx, wf)
	if err != nil {
		t.Fatalf("CreateProjectTicketsFromPlan: %v", err)
	}

	// Verify child tickets were created.
	tickets, err := database.ListSwarmTicketsByProject(ctx, sql.NullString{String: wfID, Valid: true})
	if err != nil {
		t.Fatalf("list tickets: %v", err)
	}
	if len(tickets) != 3 {
		t.Fatalf("tickets count = %d, want 3", len(tickets))
	}

	// Verify ticket identifiers follow the pattern {parentTicketID}-{num}.
	for i, tk := range tickets {
		wantID := ticketID + "-" + []string{"1", "2", "3"}[i]
		if tk.Identifier != wantID {
			t.Errorf("ticket %d identifier = %q, want %q", i, tk.Identifier, wantID)
		}
	}

	// Verify dependencies were created.
	deps, err := database.ListSwarmDependenciesByProject(ctx, wfID)
	if err != nil {
		t.Fatalf("list deps: %v", err)
	}
	// Ticket 2 depends on 1, ticket 3 depends on 2 = 2 edges.
	if len(deps) != 2 {
		t.Errorf("dependencies count = %d, want 2", len(deps))
	}

	// Verify milestones were created.
	rawDB := database.SQLDB()
	var milestoneCount int
	row := rawDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM swarm_project_milestones WHERE workflow_id = ?`, wfID)
	if err := row.Scan(&milestoneCount); err != nil {
		t.Fatalf("count milestones: %v", err)
	}
	if milestoneCount != 2 {
		t.Errorf("milestones count = %d, want 2", milestoneCount)
	}
}

func TestCreateProjectTicketsFromPlanNoPlan(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, baseDir, t.TempDir(), "")

	wf := sqlc.SwarmWorkflow{
		ID:           "wf-noplan",
		TicketID:     "CRE-NOPLAN",
		WorkflowType: swarm.WorkflowTypeProject,
		Phase:        swarm.PhaseProjectPlan,
	}

	err := mgr.CreateProjectTicketsFromPlan(t.Context(), wf)
	if err == nil {
		t.Fatal("expected error when no plan file exists")
	}
}

func TestReconcileProjectTickets(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()
	projectID := "wf-proj-reconcile"

	// Seed child tickets.
	for i, id := range []string{"child-1", "child-2"} {
		_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
			ID:         id,
			Identifier: "CRE-R-" + []string{"1", "2"}[i],
			Title:      "Child " + []string{"1", "2"}[i],
			Status:     "Todo",
			ProjectID:  sql.NullString{String: projectID, Valid: true},
			CreatedAt:  nowUTC(),
			UpdatedAt:  nowUTC(),
		})
	}

	// Seed a dependency.
	_ = database.CreateSwarmDependency(ctx, sqlc.CreateSwarmDependencyParams{
		ID:                "dep-1",
		TicketID:          "CRE-R-2",
		DependsOnTicketID: "CRE-R-1",
		ProjectID:         projectID,
	})

	// Verify data exists before reconcile.
	ticketsBefore, _ := database.ListSwarmTicketsByProject(ctx, sql.NullString{String: projectID, Valid: true})
	if len(ticketsBefore) != 2 {
		t.Fatalf("setup: expected 2 tickets, got %d", len(ticketsBefore))
	}
	depsBefore, _ := database.ListSwarmDependenciesByProject(ctx, projectID)
	if len(depsBefore) != 1 {
		t.Fatalf("setup: expected 1 dep, got %d", len(depsBefore))
	}

	wf := sqlc.SwarmWorkflow{
		ID:           projectID,
		TicketID:     "CRE-R",
		WorkflowType: swarm.WorkflowTypeProject,
	}

	err := mgr.ReconcileProjectTickets(ctx, wf)
	if err != nil {
		t.Fatalf("ReconcileProjectTickets: %v", err)
	}

	// Verify tickets were deleted.
	ticketsAfter, _ := database.ListSwarmTicketsByProject(ctx, sql.NullString{String: projectID, Valid: true})
	if len(ticketsAfter) != 0 {
		t.Errorf("tickets after reconcile = %d, want 0", len(ticketsAfter))
	}

	// Verify dependencies were deleted.
	depsAfter, _ := database.ListSwarmDependenciesByProject(ctx, projectID)
	if len(depsAfter) != 0 {
		t.Errorf("deps after reconcile = %d, want 0", len(depsAfter))
	}
}

func TestSpawnProjectWorkflowsNoTickets(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	wf := sqlc.SwarmWorkflow{
		ID:           "wf-proj-empty",
		TicketID:     "CRE-EMPTY",
		WorkflowType: swarm.WorkflowTypeProject,
		Phase:        swarm.PhaseProjectVerify,
	}

	// Should return nil with no tickets (logs warning, does nothing).
	err := mgr.SpawnProjectWorkflows(t.Context(), wf)
	if err != nil {
		t.Fatalf("SpawnProjectWorkflows with no tickets: %v", err)
	}
}

func TestSpawnProjectWorkflowsCreatesChildWorkflows(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "http://localhost:8080")

	ctx := t.Context()
	projectID := "wf-proj-spawn"

	// Create project workflow.
	_ = database.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           projectID,
		TicketID:     "CRE-SPAWN",
		WorkflowType: swarm.WorkflowTypeProject,
		Phase:        swarm.PhaseProjectVerify,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	})

	// Seed child tickets — ticket 2 depends on ticket 1.
	_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         "spawn-child-1",
		Identifier: "CRE-S-1",
		Title:      "Research: API patterns",
		Status:     "Todo",
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		CreatedAt:  nowUTC(),
		UpdatedAt:  nowUTC(),
	})
	_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         "spawn-child-2",
		Identifier: "CRE-S-2",
		Title:      "Implement endpoint",
		Status:     "Todo",
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		CreatedAt:  nowUTC(),
		UpdatedAt:  nowUTC(),
	})
	_ = database.CreateSwarmDependency(ctx, sqlc.CreateSwarmDependencyParams{
		ID:                "dep-spawn-1",
		TicketID:          "CRE-S-2",
		DependsOnTicketID: "CRE-S-1",
		ProjectID:         projectID,
	})

	wf, _ := database.GetSwarmWorkflow(ctx, projectID)

	// SpawnProjectWorkflows will try to start child workflows via StartWorkflow,
	// which will fail at spawnSession (no tmux). But the workflow records should
	// still be created for Wave 1 (only unblocked tickets).
	err := mgr.SpawnProjectWorkflows(ctx, wf)
	// Error expected from session spawning, but function itself should not error.
	if err != nil {
		t.Fatalf("SpawnProjectWorkflows: %v", err)
	}

	// Only CRE-S-1 (no deps) should have a workflow created (Wave 1).
	// CRE-S-2 depends on CRE-S-1 so should NOT be started yet.
	wfs1, err := database.GetSwarmWorkflowsByTicket(ctx, "CRE-S-1")
	if err != nil {
		t.Fatalf("get workflows for CRE-S-1: %v", err)
	}
	if len(wfs1) != 1 {
		t.Errorf("CRE-S-1 workflows = %d, want 1", len(wfs1))
	}

	wfs2, _ := database.GetSwarmWorkflowsByTicket(ctx, "CRE-S-2")
	if len(wfs2) != 0 {
		t.Errorf("CRE-S-2 workflows = %d, want 0 (blocked by CRE-S-1)", len(wfs2))
	}
}

func TestBuildProjectGraph(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	mgr := NewManager(database, testLogger(), nil, t.TempDir(), t.TempDir(), "")

	ctx := t.Context()
	projectID := "proj-graph"

	// Seed tickets.
	_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         "g-1",
		Identifier: "CRE-G-1",
		Title:      "Research: something",
		Status:     "Todo",
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		CreatedAt:  nowUTC(),
		UpdatedAt:  nowUTC(),
	})
	_ = database.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         "g-2",
		Identifier: "CRE-G-2",
		Title:      "Implement feature",
		Status:     "Todo",
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		CreatedAt:  nowUTC(),
		UpdatedAt:  nowUTC(),
	})
	_ = database.CreateSwarmDependency(ctx, sqlc.CreateSwarmDependencyParams{
		ID:                "dep-g-1",
		TicketID:          "CRE-G-2",
		DependsOnTicketID: "CRE-G-1",
		ProjectID:         projectID,
	})

	graph := mgr.buildProjectGraph(ctx, projectID)

	if len(graph.Tickets) != 2 {
		t.Fatalf("graph tickets = %d, want 2", len(graph.Tickets))
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("graph edges = %d, want 1", len(graph.Edges))
	}

	// "Research:" in title should infer research type.
	for _, tk := range graph.Tickets {
		if tk.TicketID == "CRE-G-1" && tk.WorkflowType != swarm.WorkflowTypeResearch {
			t.Errorf("CRE-G-1 type = %q, want research", tk.WorkflowType)
		}
		if tk.TicketID == "CRE-G-2" && tk.WorkflowType != swarm.WorkflowTypeCode {
			t.Errorf("CRE-G-2 type = %q, want code", tk.WorkflowType)
		}
	}

	// Wave 1 should only include CRE-G-1 (no deps).
	ready := graph.ReadyTickets(map[string]bool{})
	if len(ready) != 1 {
		t.Fatalf("ready tickets = %d, want 1", len(ready))
	}
	if ready[0].TicketID != "CRE-G-1" {
		t.Errorf("ready ticket = %q, want CRE-G-1", ready[0].TicketID)
	}
}
