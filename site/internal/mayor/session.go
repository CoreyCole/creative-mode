package mayor

import (
	"fmt"
	"sync"
	"time"
)

// Message represents a single conversation message.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Conversation holds the message history for a user.
type Conversation struct {
	Messages    []Message
	LastMessage time.Time
	Scripted    bool // API unavailable, using scripted fallback flow
}

// ConversationManager manages per-user conversation state with rate limiting.
type ConversationManager struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation // keyed by Discord user ID
	rateLimit     time.Duration
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager() *ConversationManager {
	cm := &ConversationManager{
		conversations: make(map[string]*Conversation),
		rateLimit:     2 * time.Second,
	}
	go cm.cleanupLoop()
	return cm
}

// AddMessage adds a message to a user's conversation.
func (cm *ConversationManager) AddMessage(userID, role, content string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, ok := cm.conversations[userID]
	if !ok {
		conv = &Conversation{}
		cm.conversations[userID] = conv
	}

	conv.Messages = append(conv.Messages, Message{Role: role, Content: content})
	conv.LastMessage = time.Now()
}

// GetMessages returns the conversation history for a user.
func (cm *ConversationManager) GetMessages(userID string) []Message {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conv, ok := cm.conversations[userID]
	if !ok {
		return nil
	}

	// Return a copy to avoid data races.
	msgs := make([]Message, len(conv.Messages))
	copy(msgs, conv.Messages)
	return msgs
}

// SetScripted marks a user's conversation as using the scripted fallback.
func (cm *ConversationManager) SetScripted(userID string, val bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, ok := cm.conversations[userID]
	if !ok {
		conv = &Conversation{}
		cm.conversations[userID] = conv
	}
	conv.Scripted = val
}

// IsScripted returns whether the user's conversation is in scripted mode.
func (cm *ConversationManager) IsScripted(userID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conv, ok := cm.conversations[userID]
	if !ok {
		return false
	}
	return conv.Scripted
}

// CheckRateLimit returns an error if the user is sending messages too fast.
func (cm *ConversationManager) CheckRateLimit(userID string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conv, ok := cm.conversations[userID]
	if !ok {
		return nil
	}

	if time.Since(conv.LastMessage) < cm.rateLimit {
		return fmt.Errorf("please wait a moment before sending another message")
	}

	return nil
}

// cleanupLoop removes stale conversations (older than 24 hours).
func (cm *ConversationManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cm.mu.Lock()
		for id, conv := range cm.conversations {
			if time.Since(conv.LastMessage) > 24*time.Hour {
				delete(cm.conversations, id)
			}
		}
		cm.mu.Unlock()
	}
}
