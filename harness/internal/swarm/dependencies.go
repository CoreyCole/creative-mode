package swarm

import "fmt"

// TicketNode represents a ticket in the dependency graph.
type TicketNode struct {
	TicketID     string
	WorkflowType WorkflowType
	Title        string
	Status       string
}

// DependencyEdge represents a directed edge: From must complete before To can start.
type DependencyEdge struct {
	From string // ticket that must complete first
	To   string // ticket that depends on From
}

// DependencyGraph holds tickets and their dependency edges for wave scheduling.
type DependencyGraph struct {
	Tickets []TicketNode
	Edges   []DependencyEdge
}

// ComputeWaves groups tickets into parallel execution waves using topological sort.
// Wave N can only start when all Wave N-1 tickets are complete.
// Returns an error if the graph contains a cycle.
func (g *DependencyGraph) ComputeWaves() ([][]TicketNode, error) {
	if len(g.Tickets) == 0 {
		return nil, nil
	}

	// Build adjacency + in-degree maps.
	ticketMap := make(map[string]TicketNode, len(g.Tickets))
	inDegree := make(map[string]int, len(g.Tickets))
	dependents := make(map[string][]string) // from -> [to]

	for _, t := range g.Tickets {
		ticketMap[t.TicketID] = t
		inDegree[t.TicketID] = 0
	}

	for _, e := range g.Edges {
		inDegree[e.To]++
		dependents[e.From] = append(dependents[e.From], e.To)
	}

	// Kahn's algorithm: emit waves of nodes with in-degree 0.
	var waves [][]TicketNode
	remaining := len(g.Tickets)

	for remaining > 0 {
		var wave []TicketNode

		for id, deg := range inDegree {
			if deg == 0 {
				wave = append(wave, ticketMap[id])
			}
		}

		if len(wave) == 0 {
			return nil, fmt.Errorf(
				"dependency cycle detected (%d tickets remaining)",
				remaining,
			)
		}

		// Remove wave nodes and decrement dependents' in-degree.
		for _, node := range wave {
			delete(inDegree, node.TicketID)

			for _, dep := range dependents[node.TicketID] {
				inDegree[dep]--
			}
		}

		waves = append(waves, wave)
		remaining -= len(wave)
	}

	return waves, nil
}

// ReadyTickets returns tickets whose dependencies are all satisfied (in the completed set).
func (g *DependencyGraph) ReadyTickets(completed map[string]bool) []TicketNode {
	// Build blocked set: tickets that have at least one unfinished dependency.
	blocked := make(map[string]bool)

	for _, e := range g.Edges {
		if !completed[e.From] {
			blocked[e.To] = true
		}
	}

	var ready []TicketNode

	for _, t := range g.Tickets {
		if completed[t.TicketID] {
			continue
		}

		if blocked[t.TicketID] {
			continue
		}

		ready = append(ready, t)
	}

	return ready
}

// AllComplete returns true if every ticket in the graph is in the completed set.
func (g *DependencyGraph) AllComplete(completed map[string]bool) bool {
	for _, t := range g.Tickets {
		if !completed[t.TicketID] {
			return false
		}
	}

	return true
}
