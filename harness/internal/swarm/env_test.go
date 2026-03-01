package swarm

import "testing"

func TestSwarmEnvToMap(t *testing.T) {
	t.Parallel()

	env := SwarmEnv{
		TicketID:   "CM-123",
		WorkflowID: "abc123",
		SessionID:  "sess1",
		Phase:      "research",
		Attempt:    "1",
		ResultPath: "/tmp/result.txt",
		HarnessURL: "http://localhost:8080",
	}

	m := env.ToMap()

	if m["CM_SWARM_TICKET_ID"] != "CM-123" {
		t.Errorf("CM_SWARM_TICKET_ID = %q, want CM-123", m["CM_SWARM_TICKET_ID"])
	}
	if m["CM_SWARM_PHASE"] != "research" {
		t.Errorf("CM_SWARM_PHASE = %q, want research", m["CM_SWARM_PHASE"])
	}
	if m["CM_HARNESS_URL"] != "http://localhost:8080" {
		t.Errorf("CM_HARNESS_URL = %q, want http://localhost:8080", m["CM_HARNESS_URL"])
	}

	// Zero-value fields should be omitted.
	if _, ok := m["CM_SWARM_BRANCH"]; ok {
		t.Error("CM_SWARM_BRANCH should be omitted when empty")
	}
	if _, ok := m["CM_SWARM_PREVIOUS_WORKFLOW_ID"]; ok {
		t.Error("CM_SWARM_PREVIOUS_WORKFLOW_ID should be omitted when empty")
	}
}

func TestSwarmEnvToMapPreviousContext(t *testing.T) {
	t.Parallel()

	env := SwarmEnv{
		TicketID:            "CM-456",
		WorkflowID:          "def456",
		SessionID:           "sess2",
		Phase:               "research",
		Attempt:             "1",
		ResultPath:          "/tmp/result.txt",
		HarnessURL:          "http://localhost:8080",
		PreviousWorkflowID:  "prev123",
		PreviousBranch:      "swarm/CM-OLD/fix",
		PreviousHandoffPath: "/path/to/handoff.md",
	}

	m := env.ToMap()

	if m["CM_SWARM_PREVIOUS_WORKFLOW_ID"] != "prev123" {
		t.Errorf("CM_SWARM_PREVIOUS_WORKFLOW_ID = %q, want prev123", m["CM_SWARM_PREVIOUS_WORKFLOW_ID"])
	}
	if m["CM_SWARM_PREVIOUS_BRANCH"] != "swarm/CM-OLD/fix" {
		t.Errorf("CM_SWARM_PREVIOUS_BRANCH = %q, want swarm/CM-OLD/fix", m["CM_SWARM_PREVIOUS_BRANCH"])
	}

	// PreviousResearchPath not set — should be omitted.
	if _, ok := m["CM_SWARM_PREVIOUS_RESEARCH_PATH"]; ok {
		t.Error("CM_SWARM_PREVIOUS_RESEARCH_PATH should be omitted when empty")
	}
}

func TestEnvKey(t *testing.T) {
	t.Parallel()

	if got := EnvKey("TicketID"); got != "CM_SWARM_TICKET_ID" {
		t.Errorf("EnvKey(TicketID) = %q, want CM_SWARM_TICKET_ID", got)
	}
	if got := EnvKey("PreviousWorkflowID"); got != "CM_SWARM_PREVIOUS_WORKFLOW_ID" {
		t.Errorf("EnvKey(PreviousWorkflowID) = %q, want CM_SWARM_PREVIOUS_WORKFLOW_ID", got)
	}
}

func TestEnvKeyPanicsOnInvalid(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid field name")
		}
	}()

	EnvKey("NonexistentField")
}
