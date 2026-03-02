package swarm

// Default configuration constants.
const (
	defaultMaxSessions      = 4
	defaultHeartbeatSeconds = 120
	defaultStallMinutes     = 45
	defaultMaxPlanRevisions = 3
	defaultMaxVerifyRetries = 3
	defaultRetryBackoffSecs = 30
	maxInfraRetries         = 2
)

// SwarmConfig holds swarm orchestration configuration.
type SwarmConfig struct {
	MaxSessions       int  `json:"maxSessions"`
	HeartbeatSeconds  int  `json:"heartbeatSeconds"`
	StallMinutes      int  `json:"stallMinutes"`
	MaxPlanRevisions  int  `json:"maxPlanRevisions"`
	MaxVerifyRetries  int  `json:"maxVerifyRetries"`
	RetryBackoffSecs  int  `json:"retryBackoffSecs"`
	GatePlanReview    bool `json:"gatePlanReview"`
	GateProjectReview bool `json:"gateProjectReview"`
}

// DefaultConfig is the default swarm configuration.
var DefaultConfig = SwarmConfig{
	MaxSessions:      defaultMaxSessions,
	HeartbeatSeconds: defaultHeartbeatSeconds,
	StallMinutes:     defaultStallMinutes,
	MaxPlanRevisions: defaultMaxPlanRevisions,
	MaxVerifyRetries: defaultMaxVerifyRetries,
	RetryBackoffSecs: defaultRetryBackoffSecs,
}

// Transition holds the result of a state machine transition.
type Transition struct {
	NextPhase Phase
	Retry     bool // true if this is a retry (same or earlier phase, attempt incremented)
	Failed    bool // true if workflow should be marked failed
}

// DetermineNextPhase computes the next phase given the current workflow state and last session result.
// If lastResult is empty, returns the first phase for the workflow type.
func DetermineNextPhase(
	workflowType WorkflowType,
	currentPhase Phase,
	attempt int,
	lastResult SessionResult,
	config SwarmConfig,
) Transition {
	// No session yet — start at first phase.
	if lastResult == "" {
		return Transition{NextPhase: PhaseResearch}
	}

	// Context limit — resume same phase, no attempt increment.
	if lastResult == ResultContextLimit {
		return Transition{NextPhase: currentPhase}
	}

	// Timeout — always terminal.
	if lastResult == ResultTimeout {
		return Transition{NextPhase: PhaseFailed, Failed: true}
	}

	// Infra failure — retry same phase up to maxInfraRetries times.
	if lastResult == ResultInfraFailure {
		if attempt < maxInfraRetries {
			return Transition{NextPhase: currentPhase, Retry: true}
		}

		return Transition{NextPhase: PhaseFailed, Failed: true}
	}

	// Success / logic_failure transitions by phase.
	return transitionByPhase(workflowType, currentPhase, attempt, lastResult, config)
}

//nolint:cyclop // phase transitions are inherently branchy
func transitionByPhase(
	workflowType WorkflowType,
	currentPhase Phase,
	attempt int,
	lastResult SessionResult,
	config SwarmConfig,
) Transition {
	switch currentPhase {
	case PhaseResearch:
		if lastResult != ResultSuccess {
			break
		}

		switch workflowType {
		case WorkflowTypeResearch:
			return Transition{NextPhase: PhaseDone}
		case WorkflowTypeCode:
			return Transition{NextPhase: PhaseCodePlan}
		case WorkflowTypeProject:
			return Transition{NextPhase: PhaseProjectPlan}
		default:
			return Transition{NextPhase: PhaseCodePlan}
		}

	case PhaseCodePlan:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhasePlanReview}
		}

	case PhasePlanReview:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseImplement}
		}

		if lastResult == ResultLogicFailure {
			if attempt < config.MaxPlanRevisions {
				return Transition{NextPhase: PhaseCodePlan, Retry: true}
			}

			return Transition{NextPhase: PhaseFailed, Failed: true}
		}

	case PhaseImplement:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseVerify}
		}

	case PhaseVerify:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhasePR}
		}

		if lastResult == ResultLogicFailure {
			if attempt < config.MaxVerifyRetries {
				return Transition{NextPhase: PhaseImplement, Retry: true}
			}

			return Transition{NextPhase: PhaseFailed, Failed: true}
		}

	case PhasePR:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseHumanReview}
		}

	case PhaseProjectPlan:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseProjectReview}
		}

	case PhaseProjectReview:
		if lastResult == ResultSuccess {
			// After review approval, transition to project_verify.
			// The orchestrator spawns child tickets on this transition
			// and the heartbeat monitors child progress before running
			// the verify session.
			return Transition{NextPhase: PhaseProjectVerify}
		}

		if lastResult == ResultLogicFailure {
			if attempt < config.MaxPlanRevisions {
				return Transition{NextPhase: PhaseProjectPlan, Retry: true}
			}

			return Transition{NextPhase: PhaseFailed, Failed: true}
		}

	case PhaseProjectVerify:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseDone}
		}

		if lastResult == ResultLogicFailure {
			return Transition{NextPhase: PhaseProjectVerify, Retry: true}
		}

	case PhaseHumanReview:
		if lastResult == ResultSuccess {
			return Transition{NextPhase: PhaseDone}
		}

	case PhaseDone, PhaseFailed:
		// Terminal phases — no transition.

	default:
		// Unknown phase — fail safe.
	}

	return Transition{NextPhase: PhaseFailed, Failed: true}
}

// SkillForPhase maps a phase to its corresponding Claude Code skill name.
func SkillForPhase(phase Phase) string {
	switch phase {
	case PhaseResearch:
		return "swarm-research"
	case PhaseCodePlan:
		return "swarm-code-plan"
	case PhasePlanReview:
		return "swarm-plan-review"
	case PhaseImplement:
		return "swarm-code"
	case PhaseVerify:
		return "swarm-code-verify"
	case PhasePR:
		return "swarm-code-pr"
	case PhaseProjectPlan:
		return "swarm-project-plan"
	case PhaseProjectReview:
		return "swarm-project-review"
	case PhaseProjectVerify:
		return "swarm-project-verify"
	case PhaseHumanReview, PhaseDone, PhaseFailed:
		return ""
	default:
		return ""
	}
}

// IsGatedTransition returns true if the phase+result+config combination should
// pause for human review instead of advancing automatically.
func IsGatedTransition(phase Phase, result SessionResult, config SwarmConfig) bool {
	if result != ResultSuccess {
		return false
	}

	switch phase { //nolint:exhaustive // only specific phases are gated
	case PhasePlanReview:
		return config.GatePlanReview
	case PhaseProjectReview:
		return config.GateProjectReview
	default:
		return false
	}
}

// GateRejectionTarget returns the phase to send the workflow back to when a gate is rejected.
func GateRejectionTarget(gatePhase Phase) (Phase, bool) {
	switch gatePhase { //nolint:exhaustive // only gate phases have rejection targets
	case PhasePlanReview:
		return PhaseCodePlan, true
	case PhaseProjectReview:
		return PhaseProjectPlan, true
	case PhasePR, PhaseHumanReview:
		return PhaseImplement, true
	default:
		return "", false
	}
}
