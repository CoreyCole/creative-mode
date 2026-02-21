package pages

import "fmt"

// mayorPageSignals returns the initial data-signals JSON for the mayor page.
// When worldName and mayorName are set (world is ready), world_creating starts
// as true so the dialog opens and inputs are disabled without flicker.
func mayorPageSignals(worldName, mayorName string) string {
	worldCreating := worldName != "" && mayorName != ""
	return fmt.Sprintf(`{"mayor_input": "", "world_creating": %t}`, worldCreating)
}

// ChatMessage represents a pre-rendered chat message for the mayor page template.
type ChatMessage struct {
	ID          string   // unique DOM element ID
	Role        string   // "user" or "assistant"
	HTMLContent string   // markdown-rendered HTML (assistant messages)
	Content     string   // plain text (user messages)
	AvatarURL   string   // Discord avatar URL (user messages)
	ImageURLs   []string // image URLs for user messages with attachments
}

// GraphData holds SVG path data for the metrics line graph.
type GraphData struct {
	MemoryPath string // SVG path d attribute for memory usage line
	VisitsPath string // SVG path d attribute for page views line
	MemoryLast string // last memory reading, e.g. "4.2 GB"
	VisitsLast string // last page view count, e.g. "1234"
	MemoryMax  string // top of Y-axis, e.g. "4.5 GB"
	MemoryMid  string // midpoint of Y-axis
	MemoryMin  string // bottom of Y-axis, e.g. "3.8 GB"
	VisitsMax  int64
	VisitsMin  int64
	PointCount int    // number of data points rendered
	TimeLabel  string // e.g. "Last Hour", "Last 24 Hours"
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
