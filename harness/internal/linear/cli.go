package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Client wraps the linear-cli binary for Linear API operations.
type Client struct {
	binaryPath string
	teamKey    string // e.g. "CRE"
}

// NewClient creates a new Linear CLI client. Returns nil if binaryPath is empty.
func NewClient(binaryPath, teamKey string) *Client {
	if binaryPath == "" {
		return nil
	}
	return &Client{
		binaryPath: binaryPath,
		teamKey:    teamKey,
	}
}

// run executes a linear-cli command and returns stdout bytes.
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(
			"linear-cli %s: %w (stderr: %s)",
			strings.Join(args, " "),
			err,
			stderr.String(),
		)
	}
	return stdout.Bytes(), nil
}

// runJSON executes a linear-cli command with JSON output and unmarshals into T.
func runJSON[T any](c *Client, ctx context.Context, args ...string) (T, error) {
	var zero T
	args = append(args, "--output", "json", "--compact", "--quiet")
	data, err := c.run(ctx, args...)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(data, &zero); err != nil {
		return zero, fmt.Errorf(
			"parse linear-cli output: %w (raw: %s)",
			err,
			string(data),
		)
	}
	return zero, nil
}

// GetIssue fetches a single issue by identifier (e.g. "CRE-15").
func (c *Client) GetIssue(ctx context.Context, identifier string) (*Issue, error) {
	issue, err := runJSON[Issue](c, ctx, "issues", "get", identifier)
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// UpdateStatus changes the workflow state on an issue.
func (c *Client) UpdateStatus(ctx context.Context, identifier, status string) error {
	_, err := c.run(ctx, "issues", "update", identifier, "--state", status, "--quiet")
	return err
}

// UpdateLabels sets labels on an issue. The -l flag can be specified multiple times.
func (c *Client) UpdateLabels(
	ctx context.Context,
	identifier string,
	labels []string,
) error {
	args := make([]string, 0, 4+2*len(labels)) //nolint:mnd // base args + label pairs
	args = append(args, "issues", "update", identifier, "--quiet")
	for _, l := range labels {
		args = append(args, "-l", l)
	}
	_, err := c.run(ctx, args...)
	return err
}

// AddComment posts a markdown comment to an issue.
func (c *Client) AddComment(ctx context.Context, identifier, body string) error {
	_, err := c.run(ctx, "comments", "create", identifier, "--body", body, "--quiet")
	return err
}

// CreateAttachment links a URL to an issue as an attachment.
func (c *Client) CreateAttachment(
	ctx context.Context,
	identifier, title, url string,
) error {
	_, err := c.run(
		ctx,
		"attachments",
		"create",
		identifier,
		"--title",
		title,
		"--url",
		url,
		"--quiet",
	)
	return err
}

// CreateIssue creates a new issue and returns its identifier.
func (c *Client) CreateIssue(
	ctx context.Context,
	title string,
	opts CreateOpts,
) (string, error) {
	args := []string{"issues", "create", title}
	team := opts.Team
	if team == "" {
		team = c.teamKey
	}
	if team != "" {
		args = append(args, "--team", team)
	}
	if opts.Priority > 0 {
		args = append(args, "--priority", strconv.Itoa(opts.Priority))
	}
	if opts.State != "" {
		args = append(args, "--state", opts.State)
	}
	for _, l := range opts.Labels {
		args = append(args, "-l", l)
	}
	if opts.Description != "" {
		args = append(args, "--description", opts.Description)
	}
	args = append(args, "--id-only", "--quiet")

	data, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	// --id-only returns the identifier on stdout
	return strings.TrimSpace(string(data)), nil
}

// SearchIssues searches for issues matching a query string.
func (c *Client) SearchIssues(ctx context.Context, query string) ([]SearchResult, error) {
	results, err := runJSON[[]SearchResult](c, ctx, "search", "issues", query)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// AddRelation creates a relationship between two issues.
// relationType must be "blocks", "blocked-by", or "related".
func (c *Client) AddRelation(ctx context.Context, from, relationType, to string) error {
	_, err := c.run(
		ctx,
		"relations",
		"add",
		from,
		to,
		"--relation",
		relationType,
		"--quiet",
	)
	return err
}

// ListRelations returns all relationships for an issue.
func (c *Client) ListRelations(
	ctx context.Context,
	identifier string,
) ([]Relation, error) {
	return runJSON[[]Relation](c, ctx, "relations", "list", identifier)
}
