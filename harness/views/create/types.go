package create

// ChatMessage represents a message in the create-world chat.
type ChatMessage struct {
	ID          string // unique DOM element ID
	Role        string // "user" or "assistant"
	HTMLContent string // markdown-rendered HTML (assistant messages)
	Content     string // plain text (user messages)
	AvatarURL   string // Discord avatar URL (user messages)
}
