package world

import (
	"sync"
	"time"

	"creative-mode/harness/internal/db"
)

// RateLimiter prevents prompt spam by enforcing a cooldown between submissions
// and limiting concurrent builds per user.
type RateLimiter struct {
	db            *db.DB
	mu            sync.Mutex
	lastSubmit    map[string]time.Time // userID -> last prompt time
	cooldown      time.Duration
	maxCPPerWorld int
}

// NewRateLimiter creates a rate limiter with a 30-second cooldown.
func NewRateLimiter(database *db.DB) *RateLimiter {
	return &RateLimiter{
		db:            database,
		lastSubmit:    make(map[string]time.Time),
		cooldown:      30 * time.Second,
		maxCPPerWorld: 50,
	}
}

// Check returns an error if the user is rate-limited.
func (r *RateLimiter) Check(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check cooldown.
	if last, ok := r.lastSubmit[userID]; ok {
		remaining := r.cooldown - time.Since(last)
		if remaining > 0 {
			return &RateLimitError{
				Message:       "Please wait before submitting another prompt",
				RetryAfterSec: int(remaining.Seconds()) + 1,
			}
		}
	}

	// Check for active builds.
	activeBuilds, err := r.db.CountActiveBuilds(userID)
	if err == nil && activeBuilds > 0 {
		return &RateLimitError{
			Message: "You already have a build in progress",
		}
	}

	r.lastSubmit[userID] = time.Now()
	return nil
}

// RateLimitError is returned when a user exceeds rate limits.
type RateLimitError struct {
	Message       string
	RetryAfterSec int
}

func (e *RateLimitError) Error() string { return e.Message }
