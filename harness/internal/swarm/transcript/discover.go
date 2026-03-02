package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultHomeFallback = "/root"

// DiscoverTranscript finds the most recent transcript JSONL file for a project.
// It looks in {baseDir}/{projectKey}/ for files matching *.jsonl, sorted by
// modification time descending, and returns the first one created after sessionStart.
func DiscoverTranscript(
	baseDir, projectKey string,
	sessionStart time.Time,
) (string, error) {
	dir := filepath.Join(baseDir, projectKey)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read transcript dir %s: %w", dir, err)
	}

	type candidate struct {
		path    string
		modTime time.Time
	}

	var candidates []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}

		// Only consider files modified after the session started.
		if info.ModTime().Before(sessionStart) {
			continue
		}

		candidates = append(candidates, candidate{
			path:    filepath.Join(dir, e.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf(
			"no transcript found in %s after %s",
			dir,
			sessionStart.Format(time.RFC3339),
		)
	}

	// Sort by mod time descending — most recent first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	return candidates[0].path, nil
}

// ProjectKeyFromPath converts a filesystem path to a Claude Code project key.
// e.g., "/Users/foo/project" -> "-Users-foo-project"
func ProjectKeyFromPath(workDir string) string {
	return strings.ReplaceAll(workDir, "/", "-")
}

// DefaultBaseDir returns the default Claude Code projects directory.
// Overridable via CLAUDE_PROJECTS_DIR env var.
func DefaultBaseDir() string {
	if dir := os.Getenv("CLAUDE_PROJECTS_DIR"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = defaultHomeFallback
	}
	return filepath.Join(home, ".claude", "projects")
}
