package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	apiURL         = "https://api.linear.app/graphql"
	requestTimeout = 15 * time.Second

	// issueUpdateMutation is reused for status, label, and parent updates.
	issueUpdateMutation = `mutation($id: String!, $input: IssueUpdateInput!) {
		issueUpdate(id: $id, input: $input) { success }
	}`
)

// errCreateFailed is returned when the Linear API reports success=false.
var errCreateFailed = errors.New("create ticket: API returned success=false")

// Client is a thin Go wrapper around the Linear GraphQL API.
// All mutations are serialized to respect the 1500 req/hr rate limit.
type Client struct {
	apiKey  string
	teamID  string // Linear team UUID (resolved from teamKey on first use)
	teamKey string // e.g. "CM"
	logger  *slog.Logger
	client  *http.Client
	mu      sync.Mutex // serializes mutations
}

// Ticket represents a Linear issue.
type Ticket struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"` // e.g. "CM-123"
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Labels struct {
		Nodes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Parent *struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	} `json:"parent"`
}

// NewClient creates a Linear API client.
// teamKey is the short team prefix (e.g. "CM").
func NewClient(apiKey, teamKey string, logger *slog.Logger) *Client {
	return &Client{
		apiKey:  apiKey,
		teamKey: teamKey,
		logger:  logger,
		client:  &http.Client{Timeout: requestTimeout},
	}
}

// resolveTeamID looks up the team UUID from the team key on first use.
func (c *Client) resolveTeamID(ctx context.Context) (string, error) {
	if c.teamID != "" {
		return c.teamID, nil
	}

	query := `query($key: String!) {
		teams(filter: { key: { eq: $key } }) {
			nodes { id key }
		}
	}`

	var resp struct {
		Data struct {
			Teams struct {
				Nodes []struct {
					ID  string `json:"id"`
					Key string `json:"key"`
				} `json:"nodes"`
			} `json:"teams"`
		} `json:"data"`
	}

	if err := c.doQuery(ctx, query, map[string]any{"key": c.teamKey}, &resp); err != nil {
		return "", fmt.Errorf("resolve team: %w", err)
	}

	if len(resp.Data.Teams.Nodes) == 0 {
		return "", fmt.Errorf("team %q not found", c.teamKey)
	}

	c.teamID = resp.Data.Teams.Nodes[0].ID

	return c.teamID, nil
}

// CreateTicket creates a new Linear issue and returns its identifier (e.g. "CM-123").
func (c *Client) CreateTicket(
	ctx context.Context,
	title, description string,
	labelIDs []string,
	parentID string,
) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	teamID, err := c.resolveTeamID(ctx)
	if err != nil {
		return "", err
	}

	input := map[string]any{
		"title":       title,
		"description": description,
		"teamId":      teamID,
	}
	if len(labelIDs) > 0 {
		input["labelIds"] = labelIDs
	}
	if parentID != "" {
		input["parentId"] = parentID
	}

	query := `mutation($input: IssueCreateInput!) {
		issueCreate(input: $input) {
			success
			issue { id identifier title url }
		}
	}`

	var resp struct {
		Data struct {
			IssueCreate struct {
				Success bool `json:"success"`
				Issue   struct {
					ID         string `json:"id"`
					Identifier string `json:"identifier"`
					Title      string `json:"title"`
					URL        string `json:"url"`
				} `json:"issue"`
			} `json:"issueCreate"`
		} `json:"data"`
	}

	if err := c.doQuery(ctx, query, map[string]any{"input": input}, &resp); err != nil {
		return "", fmt.Errorf("create ticket: %w", err)
	}

	if !resp.Data.IssueCreate.Success {
		return "", errCreateFailed
	}

	c.logger.Info("linear ticket created",
		"identifier", resp.Data.IssueCreate.Issue.Identifier,
		"title", title)

	return resp.Data.IssueCreate.Issue.Identifier, nil
}

// UpdateStatus moves a ticket to a workflow state by name (e.g. "In Progress", "Done").
func (c *Client) UpdateStatus(ctx context.Context, issueID, stateName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Resolve state ID from name.
	teamID, err := c.resolveTeamID(ctx)
	if err != nil {
		return err
	}

	stateID, err := c.resolveStateID(ctx, teamID, stateName)
	if err != nil {
		return fmt.Errorf("resolve state %q: %w", stateName, err)
	}

	var resp struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
	}

	vars := map[string]any{
		"id":    issueID,
		"input": map[string]any{"stateId": stateID},
	}

	if err := c.doQuery(ctx, issueUpdateMutation, vars, &resp); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// PostComment adds a comment to a Linear issue.
func (c *Client) PostComment(ctx context.Context, issueID, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	query := `mutation($input: CommentCreateInput!) {
		commentCreate(input: $input) { success }
	}`

	var resp struct {
		Data struct {
			CommentCreate struct {
				Success bool `json:"success"`
			} `json:"commentCreate"`
		} `json:"data"`
	}

	vars := map[string]any{
		"input": map[string]any{
			"issueId": issueID,
			"body":    body,
		},
	}

	if err := c.doQuery(ctx, query, vars, &resp); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}

	return nil
}

// AddLabel adds a label to an issue.
func (c *Client) AddLabel(ctx context.Context, issueID, labelID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get current labels first, then append.
	issue, err := c.getIssue(ctx, issueID)
	if err != nil {
		return err
	}

	labelIDs := make([]string, 0, len(issue.Labels.Nodes)+1)
	for _, l := range issue.Labels.Nodes {
		labelIDs = append(labelIDs, l.ID)
	}
	labelIDs = append(labelIDs, labelID)

	var resp struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
	}

	vars := map[string]any{
		"id":    issueID,
		"input": map[string]any{"labelIds": labelIDs},
	}

	return c.doQuery(ctx, issueUpdateMutation, vars, &resp)
}

// SetParent sets the parent issue for a ticket.
func (c *Client) SetParent(ctx context.Context, issueID, parentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var resp struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
	}

	vars := map[string]any{
		"id":    issueID,
		"input": map[string]any{"parentId": parentID},
	}

	return c.doQuery(ctx, issueUpdateMutation, vars, &resp)
}

// GetTicket fetches a ticket by its identifier (e.g. "CM-123").
func (c *Client) GetTicket(ctx context.Context, identifier string) (*Ticket, error) {
	query := `query($filter: IssueFilter) {
		issues(filter: $filter, first: 1) {
			nodes {
				id identifier title description url
				state { name }
				labels { nodes { id name } }
				parent { id identifier }
			}
		}
	}`

	var resp struct {
		Data struct {
			Issues struct {
				Nodes []Ticket `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
	}

	vars := map[string]any{
		"filter": map[string]any{
			"identifier": map[string]any{"eq": identifier},
		},
	}

	if err := c.doQuery(ctx, query, vars, &resp); err != nil {
		return nil, fmt.Errorf("get ticket %s: %w", identifier, err)
	}

	if len(resp.Data.Issues.Nodes) == 0 {
		return nil, fmt.Errorf("ticket %s not found", identifier)
	}

	return &resp.Data.Issues.Nodes[0], nil
}

// ListChildren returns child issues of a parent issue ID.
func (c *Client) ListChildren(ctx context.Context, parentID string) ([]Ticket, error) {
	query := `query($filter: IssueFilter) {
		issues(filter: $filter) {
			nodes {
				id identifier title description url
				state { name }
				labels { nodes { id name } }
				parent { id identifier }
			}
		}
	}`

	var resp struct {
		Data struct {
			Issues struct {
				Nodes []Ticket `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
	}

	vars := map[string]any{
		"filter": map[string]any{
			"parent": map[string]any{"id": map[string]any{"eq": parentID}},
		},
	}

	if err := c.doQuery(ctx, query, vars, &resp); err != nil {
		return nil, fmt.Errorf("list children: %w", err)
	}

	return resp.Data.Issues.Nodes, nil
}

// AddDependency creates a "blocks" relation: dependsOnID blocks issueID.
func (c *Client) AddDependency(ctx context.Context, issueID, dependsOnID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	query := `mutation($input: IssueRelationCreateInput!) {
		issueRelationCreate(input: $input) { success }
	}`

	var resp struct {
		Data struct {
			IssueRelationCreate struct {
				Success bool `json:"success"`
			} `json:"issueRelationCreate"`
		} `json:"data"`
	}

	vars := map[string]any{
		"input": map[string]any{
			"issueId":        issueID,
			"relatedIssueId": dependsOnID,
			"type":           "blocks",
		},
	}

	return c.doQuery(ctx, query, vars, &resp)
}

// getIssue fetches a single issue by ID (internal, no lock).
func (c *Client) getIssue(ctx context.Context, issueID string) (*Ticket, error) {
	query := `query($id: String!) {
		issue(id: $id) {
			id identifier title description url
			state { name }
			labels { nodes { id name } }
			parent { id identifier }
		}
	}`

	var resp struct {
		Data struct {
			Issue *Ticket `json:"issue"`
		} `json:"data"`
	}

	if err := c.doQuery(ctx, query, map[string]any{"id": issueID}, &resp); err != nil {
		return nil, err
	}

	if resp.Data.Issue == nil {
		return nil, fmt.Errorf("issue %s not found", issueID)
	}

	return resp.Data.Issue, nil
}

// resolveStateID finds a workflow state ID by name for a team (no lock — called within locked context).
func (c *Client) resolveStateID(
	ctx context.Context,
	teamID, stateName string,
) (string, error) {
	query := `query($filter: WorkflowStateFilter) {
		workflowStates(filter: $filter) {
			nodes { id name }
		}
	}`

	var resp struct {
		Data struct {
			WorkflowStates struct {
				Nodes []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"workflowStates"`
		} `json:"data"`
	}

	vars := map[string]any{
		"filter": map[string]any{
			"team": map[string]any{"id": map[string]any{"eq": teamID}},
			"name": map[string]any{"eq": stateName},
		},
	}

	if err := c.doQuery(ctx, query, vars, &resp); err != nil {
		return "", err
	}

	if len(resp.Data.WorkflowStates.Nodes) == 0 {
		return "", fmt.Errorf("state %q not found for team %s", stateName, teamID)
	}

	return resp.Data.WorkflowStates.Nodes[0].ID, nil
}

// doQuery executes a GraphQL query against the Linear API.
func (c *Client) doQuery(
	ctx context.Context,
	query string,
	variables map[string]any,
	result any,
) error {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear API %d: %s", resp.StatusCode, string(respBody))
	}

	// Check for GraphQL errors.
	var errCheck struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(respBody, &errCheck) == nil && len(errCheck.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", errCheck.Errors[0].Message)
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}
