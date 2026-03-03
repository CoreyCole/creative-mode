package swarmorch

import (
	"sync"
)

// StartRegistry provides channel-based signaling for session start events.
// Used to confirm that Claude Code has actually started (SessionStart hook fires).
type StartRegistry struct {
	mu       sync.RWMutex
	channels map[string]chan struct{}
}

// NewStartRegistry creates a new StartRegistry.
func NewStartRegistry() *StartRegistry {
	return &StartRegistry{
		channels: make(map[string]chan struct{}),
	}
}

// Register creates and returns a buffered channel for the given session ID.
func (r *StartRegistry) Register(sessionID string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan struct{}, 1)
	r.channels[sessionID] = ch

	return ch
}

// Signal sends a start notification. Returns true if a listener was waiting.
func (r *StartRegistry) Signal(sessionID string) bool {
	r.mu.RLock()
	ch, ok := r.channels[sessionID]
	r.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Unregister removes the channel for a session ID.
func (r *StartRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.channels, sessionID)
}
