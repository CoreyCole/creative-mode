package linear

// Issue represents a Linear issue returned by the CLI.
type Issue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       State  `json:"state"`
	Labels      Labels `json:"labels"`
	Priority    int    `json:"priority"`
}

// State is the workflow state of an issue.
type State struct {
	Name string `json:"name"`
	Type string `json:"type"` // backlog, unstarted, started, completed, canceled
}

// Labels wraps the nested nodes array from linear-cli JSON output.
type Labels struct {
	Nodes []Label `json:"nodes"`
}

// Label is a single label on an issue.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Relation represents a relationship between two issues.
type Relation struct {
	Type       string `json:"type"`       // blocks, blocked-by, related
	Identifier string `json:"identifier"` // the related issue's identifier
}

// CreateOpts holds optional fields for creating an issue.
type CreateOpts struct {
	Team        string
	Priority    int
	Labels      []string
	State       string
	Description string
}

// SearchResult represents an issue returned by search.
type SearchResult struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	State      State  `json:"state"`
}
