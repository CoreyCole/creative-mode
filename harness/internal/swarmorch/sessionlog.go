package swarmorch

import (
	"log/slog"

	"creative-mode/harness/internal/swarm"
)

// SessionLog wraps a slog.Logger with swarm correlation fields so every log
// call automatically includes subsystem, ticket_id, workflow_id, session_id,
// and phase.
type SessionLog struct {
	logger *slog.Logger
}

// NewSessionLog creates a SessionLog with swarm correlation fields.
func NewSessionLog(
	base *slog.Logger,
	ticketID, workflowID, sessionID string,
	phase swarm.Phase,
) *SessionLog {
	return &SessionLog{
		logger: base.With(
			"subsystem", "swarm",
			"ticket_id", ticketID,
			"workflow_id", workflowID,
			"session_id", sessionID,
			"phase", string(phase),
		),
	}
}

// Info logs at info level with correlation fields.
func (l *SessionLog) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn logs at warn level with correlation fields.
func (l *SessionLog) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error logs at error level with correlation fields.
func (l *SessionLog) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}
