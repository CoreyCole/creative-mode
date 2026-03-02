package events

const (
	EventChatMessage       = "chat.message"
	EventPlayerJoined      = "player.joined"
	EventPlayerLeft        = "player.left"
	EventBuildStarted      = "build.started"
	EventBuildCompleted    = "build.completed"
	EventBuildFailed       = "build.failed"
	EventClaudeToolUsePre  = "claude.tool_use.pre"
	EventClaudeSessionStop = "claude.session_stopped"
	EventClaudeRateLimited = "claude.rate_limited"
	EventExecuteScript     = "execute_script"
	EventMayorMessage      = "mayor.message"

	// Swarm orchestrator events.
	EventSwarmWorkflowStarted  = "swarm.workflow_started"
	EventSwarmWorkflowComplete = "swarm.workflow_completed"
	EventSwarmWorkflowFailed   = "swarm.workflow_failed"
	EventSwarmSessionSpawned   = "swarm.session_spawned"
	EventSwarmSessionComplete  = "swarm.session_completed"
	EventSwarmToolUse          = "swarm.tool_use"
	EventSwarmContextPressure  = "swarm.context_pressure"
	EventSwarmGateReached      = "swarm.gate_reached"
	EventSwarmGateApproved     = "swarm.gate_approved"
	EventSwarmGateRejected     = "swarm.gate_rejected"
)
