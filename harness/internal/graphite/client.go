package graphite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

const defaultBin = "gt"

// StackEntry represents a branch in a Graphite stack.
type StackEntry struct {
	Branch string `json:"branch"`
	PR     string `json:"pr,omitempty"` // PR URL if submitted
}

// Client wraps the Graphite `gt` CLI for branch stacking operations.
// All mutating operations are serialized via mutex.
type Client struct {
	binPath string
	repoDir string
	logger  *slog.Logger
	mu      sync.Mutex
}

// NewClient creates a Graphite CLI client.
// binPath defaults to "gt" if empty.
func NewClient(binPath, repoDir string, logger *slog.Logger) *Client {
	if binPath == "" {
		binPath = defaultBin
	}

	return &Client{
		binPath: binPath,
		repoDir: repoDir,
		logger:  logger,
	}
}

// CreateBranch creates a new stacked branch on top of the current branch.
// It stages all changes and commits with the given message.
func (c *Client) CreateBranch(ctx context.Context, branchName, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	args := []string{"branch", "create", branchName, "--no-interactive"}
	if message != "" {
		args = append(args, "-m", message)
	}

	_, err := c.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("create branch %q: %w", branchName, err)
	}

	c.logger.Info("graphite branch created", "branch", branchName)

	return nil
}

// TrackBranch tells Graphite to track an existing git branch, optionally
// setting its parent. If parentBranch is empty, Graphite infers the parent.
func (c *Client) TrackBranch(ctx context.Context, branchName, parentBranch string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	args := []string{"branch", "track", "--no-interactive"}
	if parentBranch != "" {
		args = append(args, "--parent", parentBranch)
	}

	_, err := c.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("track branch %q: %w", branchName, err)
	}

	c.logger.Info("graphite branch tracked", "branch", branchName, "parent", parentBranch)

	return nil
}

// StackOnto rebases the current branch (and its children) onto a new parent.
func (c *Client) StackOnto(ctx context.Context, parentBranch string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.run(ctx, "upstack", "onto", parentBranch, "--no-interactive")
	if err != nil {
		return fmt.Errorf("stack onto %q: %w", parentBranch, err)
	}

	c.logger.Info("graphite stack rebased", "parent", parentBranch)

	return nil
}

// SubmitStack creates or updates PRs for the entire stack.
// Returns the combined stdout from gt submit.
func (c *Client) SubmitStack(ctx context.Context, draft bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	args := []string{"stack", "submit", "--no-interactive", "--no-edit"}
	if draft {
		args = append(args, "--draft")
	} else {
		args = append(args, "--publish")
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("submit stack: %w", err)
	}

	c.logger.Info("graphite stack submitted", "draft", draft)

	return out, nil
}

// LogStack returns the current stack as a list of branch names.
// Uses `gt log short` and parses the output.
func (c *Client) LogStack(ctx context.Context) ([]StackEntry, error) {
	out, err := c.run(ctx, "log", "short", "--no-interactive")
	if err != nil {
		return nil, fmt.Errorf("log stack: %w", err)
	}

	var entries []StackEntry

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// gt log short lines look like:
		//   ◉ branch-name (PR #123)
		//   ◯ branch-name
		// Strip leading markers and extract branch name.
		branch := cleanLogLine(line)
		if branch == "" {
			continue
		}

		entries = append(entries, StackEntry{Branch: branch})
	}

	return entries, nil
}

// RepoSync pulls trunk and deletes merged branches.
func (c *Client) RepoSync(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.run(ctx, "repo", "sync", "--no-interactive")
	if err != nil {
		return fmt.Errorf("repo sync: %w", err)
	}

	return nil
}

// Version returns the gt CLI version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "--version")
	if err != nil {
		return "", fmt.Errorf("version: %w", err)
	}

	return strings.TrimSpace(out), nil
}

// run executes a gt command and returns combined stdout.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.binPath, args...)
	cmd.Dir = c.repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	c.logger.Debug("graphite exec", "args", args)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

// cleanLogLine extracts the branch name from a gt log short line.
func cleanLogLine(line string) string {
	// Strip leading whitespace first, then Unicode markers.
	line = strings.TrimSpace(line)

	for _, prefix := range []string{"◉", "◯", "●", "○", "◆", "◇", "▸", "▹", "→"} {
		line = strings.TrimPrefix(line, prefix)
	}

	line = strings.TrimSpace(line)

	// Remove trailing PR reference like "(PR #123)" or "(https://...)".
	if idx := strings.LastIndex(line, "("); idx > 0 {
		line = strings.TrimSpace(line[:idx])
	}

	return line
}

// MarshalJSON implements json.Marshaler for StackEntry.
func (s StackEntry) MarshalJSON() ([]byte, error) {
	type alias StackEntry

	return json.Marshal(alias(s))
}
