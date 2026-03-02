package swarm

// Phase is a typed enum for workflow phases.
type Phase string

const (
	PhaseResearch      Phase = "research"
	PhaseCodePlan      Phase = "code_plan"
	PhasePlanReview    Phase = "plan_review"
	PhaseImplement     Phase = "implement"
	PhaseVerify        Phase = "verify"
	PhasePR            Phase = "pr"
	PhaseProjectPlan   Phase = "project_plan"
	PhaseProjectReview Phase = "project_review"
	PhaseProjectVerify Phase = "project_verify"
	PhaseHumanReview   Phase = "human_review"
	PhaseDone          Phase = "done"
	PhaseFailed        Phase = "failed"
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseResearch,
		PhaseCodePlan,
		PhasePlanReview,
		PhaseImplement,
		PhaseVerify,
		PhasePR,
		PhaseProjectPlan,
		PhaseProjectReview,
		PhaseProjectVerify,
		PhaseHumanReview,
		PhaseDone,
		PhaseFailed:
		return true
	default:
		return false
	}
}

// SessionResult is a typed enum for session outcomes.
type SessionResult string

const (
	ResultSuccess      SessionResult = "success"
	ResultLogicFailure SessionResult = "logic_failure"
	ResultInfraFailure SessionResult = "infra_failure"
	ResultTimeout      SessionResult = "timeout"
	ResultContextLimit SessionResult = "context_limit"
)

func (r SessionResult) Valid() bool {
	switch r {
	case ResultSuccess,
		ResultLogicFailure,
		ResultInfraFailure,
		ResultTimeout,
		ResultContextLimit:
		return true
	default:
		return false
	}
}

// WorkflowStatus is a typed enum for workflow status.
type WorkflowStatus string

const (
	StatusPending        WorkflowStatus = "pending"
	StatusRunning        WorkflowStatus = "running"
	StatusComplete       WorkflowStatus = "completed"
	StatusFailed         WorkflowStatus = "failed"
	StatusCanceled       WorkflowStatus = "canceled"
	StatusAwaitingReview WorkflowStatus = "awaiting_review"
)

func (s WorkflowStatus) Valid() bool {
	switch s {
	case StatusPending,
		StatusRunning,
		StatusComplete,
		StatusFailed,
		StatusCanceled,
		StatusAwaitingReview:
		return true
	default:
		return false
	}
}

// WorkflowType is a typed enum for workflow types.
type WorkflowType string

const (
	WorkflowTypeResearch WorkflowType = "research"
	WorkflowTypeCode     WorkflowType = "code"
	WorkflowTypeProject  WorkflowType = "project"
)

func (w WorkflowType) Valid() bool {
	switch w {
	case WorkflowTypeResearch, WorkflowTypeCode, WorkflowTypeProject:
		return true
	default:
		return false
	}
}

// EventType is a typed enum for swarm event types.
type EventType string

const (
	EventWorkflowStarted   EventType = "workflow_started"
	EventWorkflowComplete  EventType = "workflow_completed"
	EventWorkflowFailed    EventType = "workflow_failed"
	EventWorkflowCanceled  EventType = "workflow_canceled"
	EventPhaseStarted      EventType = "phase_started"
	EventPhaseComplete     EventType = "phase_completed"
	EventSessionSpawned    EventType = "session_spawned"
	EventSessionComplete   EventType = "session_completed"
	EventPlanReviewVerdict EventType = "plan_review_verdict"
	EventVerifyResult      EventType = "verify_result"
	EventMilestonePassed   EventType = "milestone_passed"
	EventMilestoneFailed   EventType = "milestone_failed"
	EventRetryTriggered    EventType = "retry_triggered"
	EventStallDetected     EventType = "stall_detected"
	EventSessionReaped     EventType = "session_reaped"
	EventTicketSynced      EventType = "ticket_synced"
	EventTerminalFailure   EventType = "terminal_failure"
	EventGateReached       EventType = "gate_reached"
	EventGateApproved      EventType = "gate_approved"
	EventGateRejected      EventType = "gate_rejected"
)

func (e EventType) Valid() bool {
	switch e {
	case EventWorkflowStarted,
		EventWorkflowComplete,
		EventWorkflowFailed,
		EventWorkflowCanceled,
		EventPhaseStarted,
		EventPhaseComplete,
		EventSessionSpawned,
		EventSessionComplete,
		EventPlanReviewVerdict,
		EventVerifyResult,
		EventMilestonePassed,
		EventMilestoneFailed,
		EventRetryTriggered,
		EventStallDetected,
		EventSessionReaped,
		EventTicketSynced,
		EventTerminalFailure,
		EventGateReached,
		EventGateApproved,
		EventGateRejected:
		return true
	default:
		return false
	}
}

// MilestoneStatus is a typed enum for project milestone status.
type MilestoneStatus string

const (
	MilestoneStatusPending MilestoneStatus = "pending"
	MilestoneStatusPassed  MilestoneStatus = "passed"
	MilestoneStatusFailed  MilestoneStatus = "failed"
)

func (m MilestoneStatus) Valid() bool {
	switch m {
	case MilestoneStatusPending, MilestoneStatusPassed, MilestoneStatusFailed:
		return true
	default:
		return false
	}
}

// LearningCategory is a typed enum for learning categories.
type LearningCategory string

const (
	LearningPlanIssue  LearningCategory = "plan_issue"
	LearningCodeBug    LearningCategory = "code_bug"
	LearningPattern    LearningCategory = "pattern"
	LearningPostMortem LearningCategory = "post_mortem"
	LearningConvention LearningCategory = "convention"
)

func (l LearningCategory) Valid() bool {
	switch l {
	case LearningPlanIssue,
		LearningCodeBug,
		LearningPattern,
		LearningPostMortem,
		LearningConvention:
		return true
	default:
		return false
	}
}

// LearningSeverity is a typed enum for learning severity levels.
type LearningSeverity string

const (
	SeverityCritical LearningSeverity = "critical"
	SeverityWarning  LearningSeverity = "warning"
	SeverityInfo     LearningSeverity = "info"
)

func (s LearningSeverity) Valid() bool {
	switch s {
	case SeverityCritical, SeverityWarning, SeverityInfo:
		return true
	default:
		return false
	}
}
