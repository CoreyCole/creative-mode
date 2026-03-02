package swarmorch

import "creative-mode/harness/internal/swarm"

// Typed event payloads published to the EventBus. Each struct corresponds to a
// specific swarm lifecycle event. JSON tags use snake_case for backward
// compatibility with existing EventBus consumers.

// WorkflowStartedEvent is published when a new swarm workflow begins.
type WorkflowStartedEvent struct {
	Event        string `json:"event"`
	WorkflowID   string `json:"workflow_id"`   //nolint:tagliatelle // EventBus compat
	TicketID     string `json:"ticket_id"`     //nolint:tagliatelle // EventBus compat
	WorkflowType string `json:"workflow_type"` //nolint:tagliatelle // EventBus compat
}

// SessionSpawnedEvent is published when a new Claude Code session is spawned.
type SessionSpawnedEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	SessionID  string `json:"session_id"`  //nolint:tagliatelle // EventBus compat
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // EventBus compat
	Phase      string `json:"phase"`
	Skill      string `json:"skill"`
}

// SessionCompleteEvent is published when a Claude Code session finishes.
type SessionCompleteEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	SessionID  string `json:"session_id"`  //nolint:tagliatelle // EventBus compat
	Phase      string `json:"phase"`
	Result     string `json:"result"`
	Summary    string `json:"summary"`
}

// WorkflowCompleteEvent is published when a workflow reaches the done state.
type WorkflowCompleteEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // EventBus compat
}

// WorkflowFailedEvent is published when a workflow terminally fails.
type WorkflowFailedEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // EventBus compat
	Phase      string `json:"phase"`
	Reason     string `json:"reason"`
}

// GateReachedEvent is published when a workflow enters a human review gate.
type GateReachedEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // EventBus compat
	GatePhase  string `json:"gate_phase"`  //nolint:tagliatelle // EventBus compat
}

// GateReviewedEvent is published when a human approves or rejects at a gate.
type GateReviewedEvent struct {
	Event      string `json:"event"`
	WorkflowID string `json:"workflow_id"` //nolint:tagliatelle // EventBus compat
	TicketID   string `json:"ticket_id"`   //nolint:tagliatelle // EventBus compat
	GatePhase  string `json:"gate_phase"`  //nolint:tagliatelle // EventBus compat
	Action     string `json:"action"`
	Reviewer   string `json:"reviewer"`
}

// SessionJSONLEvent is the typed payload written to JSONL session logs when
// a session is first spawned.
type SessionJSONLEvent struct {
	Event      string      `json:"event"`
	SessionID  string      `json:"session_id"`  //nolint:tagliatelle // JSONL compat
	WorkflowID string      `json:"workflow_id"` //nolint:tagliatelle // JSONL compat
	TicketID   string      `json:"ticket_id"`   //nolint:tagliatelle // JSONL compat
	Phase      swarm.Phase `json:"phase"`
	Skill      string      `json:"skill"`
}
