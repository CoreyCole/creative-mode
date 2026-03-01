package swarmorch

import (
	"sync"

	"creative-mode/harness/internal/swarm"
)

// SessionResult carries the outcome of a Claude Code session back through
// the CompletionRegistry channel.
type SessionResult struct {
	Result  swarm.SessionResult
	Summary string
}

// CompletionRegistry provides channel-based signaling for session completion.
// The watchSession goroutine registers a channel before spawning, then blocks
// on it. Hook endpoints (Stop or SessionEnd) signal the channel with the result.
type CompletionRegistry struct {
	mu       sync.RWMutex
	channels map[string]chan SessionResult
}

// NewCompletionRegistry creates a new CompletionRegistry.
func NewCompletionRegistry() *CompletionRegistry {
	return &CompletionRegistry{
		channels: make(map[string]chan SessionResult),
	}
}

// Register creates and returns a buffered channel for the given session ID.
func (r *CompletionRegistry) Register(sessionID string) chan SessionResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan SessionResult, 1)
	r.channels[sessionID] = ch

	return ch
}

// Signal sends a result to the registered channel. Returns true if a listener
// was waiting, false if the session was not registered or already signaled.
func (r *CompletionRegistry) Signal(sessionID string, result SessionResult) bool {
	r.mu.RLock()
	ch, ok := r.channels[sessionID]
	r.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case ch <- result:
		return true
	default:
		// Already signaled (buffer full).
		return false
	}
}

// Unregister removes the channel for a session ID.
func (r *CompletionRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.channels, sessionID)
}

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
