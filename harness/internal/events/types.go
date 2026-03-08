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

	// Swarm span lifecycle events (published to EventBus("swarm"))
	EventSpanStarted        = "span.started"
	EventSpanCompleted      = "span.completed"
	EventSpanFailed         = "span.failed"
	EventSwarmTaskCompleted = "swarm.task.completed"
	EventSwarmTaskFailed    = "swarm.task.failed"
)
