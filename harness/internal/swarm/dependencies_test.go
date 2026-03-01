package swarm

import (
	"testing"
)

func node(id string) TicketNode {
	return TicketNode{
		TicketID:     id,
		WorkflowType: WorkflowTypeCode,
		Title:        id,
		Status:       "pending",
	}
}

func TestComputeWaves_NoEdges(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B"), node("C")},
	}

	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}

	if len(waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(waves))
	}

	if len(waves[0]) != 3 {
		t.Fatalf("expected 3 tickets in wave 0, got %d", len(waves[0]))
	}
}

func TestComputeWaves_LinearChain(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B"), node("C")},
		Edges: []DependencyEdge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
		},
	}

	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}

	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}

	if waves[0][0].TicketID != "A" {
		t.Errorf("wave 0: expected A, got %s", waves[0][0].TicketID)
	}

	if waves[1][0].TicketID != "B" {
		t.Errorf("wave 1: expected B, got %s", waves[1][0].TicketID)
	}

	if waves[2][0].TicketID != "C" {
		t.Errorf("wave 2: expected C, got %s", waves[2][0].TicketID)
	}
}

func TestComputeWaves_Diamond(t *testing.T) {
	t.Parallel()

	// A → B, A → C, B → D, C → D
	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B"), node("C"), node("D")},
		Edges: []DependencyEdge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "B", To: "D"},
			{From: "C", To: "D"},
		},
	}

	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}

	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}

	// Wave 0: [A]
	if len(waves[0]) != 1 || waves[0][0].TicketID != "A" {
		t.Errorf("wave 0: expected [A], got %v", ticketIDs(waves[0]))
	}

	// Wave 1: [B, C] (parallel)
	if len(waves[1]) != 2 {
		t.Errorf("wave 1: expected 2 tickets, got %d", len(waves[1]))
	}

	// Wave 2: [D]
	if len(waves[2]) != 1 || waves[2][0].TicketID != "D" {
		t.Errorf("wave 2: expected [D], got %v", ticketIDs(waves[2]))
	}
}

func TestComputeWaves_CycleDetection(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B")},
		Edges: []DependencyEdge{
			{From: "A", To: "B"},
			{From: "B", To: "A"},
		},
	}

	_, err := g.ComputeWaves()
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestComputeWaves_Empty(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{}

	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}

	if waves != nil {
		t.Fatalf("expected nil waves, got %v", waves)
	}
}

func TestReadyTickets(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B"), node("C"), node("D")},
		Edges: []DependencyEdge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "B", To: "D"},
		},
	}

	// Nothing completed — only A is ready.
	ready := g.ReadyTickets(map[string]bool{})
	if len(ready) != 1 || ready[0].TicketID != "A" {
		t.Errorf("expected [A], got %v", ticketIDs(ready))
	}

	// A completed — B and C are ready.
	ready = g.ReadyTickets(map[string]bool{"A": true})
	ids := ticketIDSet(ready)

	if len(ready) != 2 {
		t.Fatalf("expected 2 ready, got %d", len(ready))
	}

	if !ids["B"] || !ids["C"] {
		t.Errorf("expected B and C ready, got %v", ticketIDs(ready))
	}

	// A and B completed — C and D are ready.
	ready = g.ReadyTickets(map[string]bool{"A": true, "B": true})
	ids = ticketIDSet(ready)

	if len(ready) != 2 {
		t.Fatalf("expected 2 ready, got %d", len(ready))
	}

	if !ids["C"] || !ids["D"] {
		t.Errorf("expected C and D ready, got %v", ticketIDs(ready))
	}
}

func TestAllComplete(t *testing.T) {
	t.Parallel()

	g := &DependencyGraph{
		Tickets: []TicketNode{node("A"), node("B")},
	}

	if g.AllComplete(map[string]bool{}) {
		t.Error("expected not all complete")
	}

	if g.AllComplete(map[string]bool{"A": true}) {
		t.Error("expected not all complete with only A")
	}

	if !g.AllComplete(map[string]bool{"A": true, "B": true}) {
		t.Error("expected all complete")
	}
}

func ticketIDs(tickets []TicketNode) []string {
	ids := make([]string, len(tickets))
	for i, t := range tickets {
		ids[i] = t.TicketID
	}

	return ids
}

func ticketIDSet(tickets []TicketNode) map[string]bool {
	m := make(map[string]bool, len(tickets))
	for _, t := range tickets {
		m[t.TicketID] = true
	}

	return m
}
