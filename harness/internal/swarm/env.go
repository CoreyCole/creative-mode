package swarm

import (
	"fmt"
	"reflect"
)

// SwarmEnv defines all environment variables passed from the orchestrator to
// Claude Code skill sessions. The envconfig tags are the canonical env var
// names — used both for reading (via envconfig.Process on the skill side) and
// writing (via ToMap on the orchestrator side).
//
// Fields with a zero value are omitted from the map.
type SwarmEnv struct {
	// Core identifiers — always set.
	TicketID   string `envconfig:"CM_SWARM_TICKET_ID"`
	WorkflowID string `envconfig:"CM_SWARM_WORKFLOW_ID"`
	SessionID  string `envconfig:"CM_SWARM_SESSION_ID"`
	Phase      string `envconfig:"CM_SWARM_PHASE"`
	Attempt    string `envconfig:"CM_SWARM_ATTEMPT"`
	ResultPath string `envconfig:"CM_SWARM_RESULT_PATH"`
	HarnessURL string `envconfig:"CM_HARNESS_URL"`

	// Optional context.
	Branch              string `envconfig:"CM_SWARM_BRANCH"`
	TicketURL           string `envconfig:"CM_SWARM_TICKET_URL"`
	HookSecret          string `envconfig:"CM_HOOK_SECRET"`
	DryRun              string `envconfig:"CM_SWARM_DRY_RUN"`
	HandoffPath         string `envconfig:"CM_SWARM_HANDOFF_PATH"`
	LearningContextPath string `envconfig:"CM_SWARM_LEARNING_CONTEXT_PATH"`

	// Previous attempt context (full restart path).
	PreviousWorkflowID   string `envconfig:"CM_SWARM_PREVIOUS_WORKFLOW_ID"`
	PreviousBranch       string `envconfig:"CM_SWARM_PREVIOUS_BRANCH"`
	PreviousHandoffPath  string `envconfig:"CM_SWARM_PREVIOUS_HANDOFF_PATH"`
	PreviousResearchPath string `envconfig:"CM_SWARM_PREVIOUS_RESEARCH_PATH"`

	// Project context (set by project orchestrator for child workflows).
	StackParent string `envconfig:"CM_SWARM_STACK_PARENT"`
	StackOrder  string `envconfig:"CM_SWARM_STACK_ORDER"`
}

// ToMap converts the struct to a map[string]string using envconfig tag names.
// Zero-value fields are omitted.
func (e *SwarmEnv) ToMap() map[string]string {
	m := make(map[string]string)

	v := reflect.ValueOf(e).Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		val := v.Field(i).String()

		if val == "" {
			continue
		}

		tag := field.Tag.Get("envconfig")
		if tag == "" {
			continue
		}

		m[tag] = val
	}

	return m
}

// EnvKey returns the environment variable name for a SwarmEnv field.
// Panics if the field name is invalid — use only with compile-time constants.
func EnvKey(fieldName string) string {
	t := reflect.TypeOf(SwarmEnv{})

	field, ok := t.FieldByName(fieldName)
	if !ok {
		panic(fmt.Sprintf("swarm.EnvKey: no field %q on SwarmEnv", fieldName))
	}

	tag := field.Tag.Get("envconfig")
	if tag == "" {
		panic(fmt.Sprintf("swarm.EnvKey: field %q has no envconfig tag", fieldName))
	}

	return tag
}
