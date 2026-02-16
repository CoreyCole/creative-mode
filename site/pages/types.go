package pages

// ChatMessage represents a pre-rendered chat message for the mayor page template.
type ChatMessage struct {
	ID          string // unique DOM element ID
	Role        string // "user" or "assistant"
	HTMLContent string // markdown-rendered HTML (assistant messages)
	Content     string // plain text (user messages)
	AvatarURL   string // Discord avatar URL (user messages)
}

// WorldInfo holds world status data from the harness mayor-status API.
type WorldInfo struct {
	WorldID      string
	WorldName    string
	MayorName    string
	TemplateType string
	Checkpoints  int
	LatestStatus string
	GameRunning  bool
	RecentBuilds int
}
