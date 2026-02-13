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
)
