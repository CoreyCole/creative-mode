package mayorchat

import "strings"

// ImageAttachment represents an image attached to a message.
type ImageAttachment struct {
	ID       string // unique image ID
	FilePath string // disk path to image file
	MIMEType string // e.g. "image/png"
	Filename string // original filename
}

// Message represents a single conversation message.
type Message struct {
	Role    string            // "user" or "assistant"
	Content string
	Images  []ImageAttachment // attached images (user messages only)
}

// CountUserMessages returns the number of user messages in the conversation.
func CountUserMessages(messages []Message) int {
	count := 0
	for _, m := range messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// NthUserMessage returns the nth user message (0-indexed) or empty string.
func NthUserMessage(messages []Message, n int) string {
	count := 0
	for _, m := range messages {
		if m.Role == "user" {
			if count == n {
				return strings.TrimSpace(m.Content)
			}
			count++
		}
	}
	return ""
}

// LastUserMessage returns the most recent user message or empty string.
func LastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

// Truncate returns s truncated to maxLen characters with "..." appended if needed.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
