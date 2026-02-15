package mayor

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// Message represents a single conversation message.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// transientState holds per-user state that intentionally resets on restart.
type transientState struct {
	LastMessage time.Time
	Scripted    bool
}

// ConversationManager manages per-user conversation state.
// Messages are persisted in SQLite; rate limits and scripted flags are in-memory.
type ConversationManager struct {
	db        *sql.DB
	mu        sync.RWMutex
	transient map[string]*transientState // keyed by Discord user ID
	rateLimit time.Duration
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager(db *sql.DB) *ConversationManager {
	cm := &ConversationManager{
		db:        db,
		transient: make(map[string]*transientState),
		rateLimit: 2 * time.Second,
	}
	go cm.cleanupLoop()
	return cm
}

// AddMessage adds a message to a user's conversation.
func (cm *ConversationManager) AddMessage(userID, role, content string) {
	_, _ = cm.db.Exec(
		`INSERT INTO conversation_messages (discord_id, role, content) VALUES (?, ?, ?)`,
		userID, role, content,
	)

	cm.mu.Lock()
	ts, ok := cm.transient[userID]
	if !ok {
		ts = &transientState{}
		cm.transient[userID] = ts
	}
	ts.LastMessage = time.Now()
	cm.mu.Unlock()
}

// GetMessages returns the conversation history for a user.
func (cm *ConversationManager) GetMessages(userID string) []Message {
	rows, err := cm.db.Query(
		`SELECT role, content FROM conversation_messages WHERE discord_id = ? ORDER BY id ASC`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// SetScripted marks a user's conversation as using the scripted fallback.
func (cm *ConversationManager) SetScripted(userID string, val bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	ts, ok := cm.transient[userID]
	if !ok {
		ts = &transientState{}
		cm.transient[userID] = ts
	}
	ts.Scripted = val
}

// IsScripted returns whether the user's conversation is in scripted mode.
func (cm *ConversationManager) IsScripted(userID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ts, ok := cm.transient[userID]
	if !ok {
		return false
	}
	return ts.Scripted
}

// CheckRateLimit returns an error if the user is sending messages too fast.
func (cm *ConversationManager) CheckRateLimit(userID string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ts, ok := cm.transient[userID]
	if !ok {
		return nil
	}

	if time.Since(ts.LastMessage) < cm.rateLimit {
		return fmt.Errorf("please wait a moment before sending another message")
	}

	return nil
}

// cleanupLoop removes stale conversations (older than 24 hours) and transient state.
func (cm *ConversationManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		_, _ = cm.db.Exec(`DELETE FROM conversation_messages WHERE created_at < datetime('now', '-24 hours')`)

		cm.mu.Lock()
		for id, ts := range cm.transient {
			if time.Since(ts.LastMessage) > 24*time.Hour {
				delete(cm.transient, id)
			}
		}
		cm.mu.Unlock()
	}
}
