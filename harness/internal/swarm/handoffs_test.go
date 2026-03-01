package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoffDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase Phase
		want  string
	}{
		{PhaseResearch, "handoffs-research"},
		{PhaseCodePlan, "handoffs-code"},
		{PhaseImplement, "handoffs-code"},
		{PhasePR, "handoffs-code"},
		{PhasePlanReview, "handoffs-plan-reviews"},
		{PhaseVerify, "handoffs-code-reviews"},
		{PhaseProjectPlan, "handoffs-project"},
		{PhaseProjectVerify, "handoffs-project"},
		{PhaseProjectReview, "handoffs-project-reviews"},
		{PhaseDone, ""},
		{PhaseFailed, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			t.Parallel()

			got := HandoffDir(tt.phase)
			if got != tt.want {
				t.Errorf("HandoffDir(%q) = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

func TestFormatHandoffFilename(t *testing.T) {
	t.Parallel()

	name := FormatHandoffFilename("CM-123", "research summary")

	// Should contain ticket ID and sanitized detail
	if !strings.Contains(name, "CM-123") {
		t.Errorf("filename %q missing ticket ID", name)
	}
	if !strings.Contains(name, "research-summary") {
		t.Errorf("filename %q missing sanitized detail", name)
	}
	if !strings.HasSuffix(name, ".md") {
		t.Errorf("filename %q should end with .md", name)
	}

	// Format: timestamp_ticketID_detail.md
	parts := strings.SplitN(name, "_", 4)
	if len(parts) < 4 {
		t.Errorf(
			"filename %q has %d underscore-separated parts, want at least 4",
			name,
			len(parts),
		)
	}
}

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"spaces", "hello world", "hello-world"},
		{"special_chars", "foo@bar!baz", "foobarbaz"},
		{"mixed", "Hello World! #123", "hello-world-123"},
		{"truncate", strings.Repeat("abcde", 20), strings.Repeat("abcde", 10)},
		{"hyphens_preserved", "already-hyphenated", "already-hyphenated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveHandoffPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// Create directory structure
	dir := filepath.Join(base, "thoughts", "swarm", "handoffs-code")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create handoff files with different timestamps
	older := filepath.Join(dir, "2025-01-01_10-00-00_CM-42_first.md")
	newer := filepath.Join(dir, "2025-01-02_10-00-00_CM-42_second.md")

	for _, f := range []string{older, newer} {
		if err := os.WriteFile(f, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ResolveHandoffPath(base, "CM-42")
	if err != nil {
		t.Fatalf("ResolveHandoffPath: %v", err)
	}

	if got != newer {
		t.Errorf("got %q, want %q (most recent)", got, newer)
	}
}

func TestResolveHandoffPathNoMatches(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// Create the directory structure but no matching files
	dir := filepath.Join(base, "thoughts", "swarm", "handoffs-research")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveHandoffPath(base, "NONEXISTENT")
	if err != nil {
		t.Fatalf("ResolveHandoffPath: %v", err)
	}

	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestResolveHandoffPathMultipleDirs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// Create files across different handoff directories
	dirs := []string{
		filepath.Join(base, "thoughts", "swarm", "handoffs-research"),
		filepath.Join(base, "thoughts", "swarm", "handoffs-code"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	olderInResearch := filepath.Join(dirs[0], "2025-01-01_10-00-00_CM-99_research.md")
	newerInCode := filepath.Join(dirs[1], "2025-01-03_10-00-00_CM-99_implement.md")

	for _, f := range []string{olderInResearch, newerInCode} {
		if err := os.WriteFile(f, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ResolveHandoffPath(base, "CM-99")
	if err != nil {
		t.Fatalf("ResolveHandoffPath: %v", err)
	}

	if got != newerInCode {
		t.Errorf("got %q, want %q (newest across dirs)", got, newerInCode)
	}
}
