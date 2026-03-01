package graphite

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// mockGT creates a shell script that echoes predetermined output for a given
// gt subcommand. Returns the script path (caller should defer cleanup).
func mockGT(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	bin := filepath.Join(dir, "gt")

	//nolint:gosec // test mock script needs execute permission
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	return bin
}

func TestCreateBranch(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "Created branch $3"`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	err := c.CreateBranch(t.Context(), "swarm/CM-123/add-feature", "Add feature")
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
}

func TestCreateBranchError(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "error: not a git repo" >&2; exit 1`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	err := c.CreateBranch(t.Context(), "bad-branch", "msg")
	if err == nil {
		t.Fatal("expected error from CreateBranch")
	}
}

func TestTrackBranch(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "Tracked branch"`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	err := c.TrackBranch(t.Context(), "my-branch", "main")
	if err != nil {
		t.Fatalf("TrackBranch: %v", err)
	}
}

func TestStackOnto(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "Rebased onto $3"`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	err := c.StackOnto(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubmitStack(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "https://github.com/org/repo/pull/42"`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	out, err := c.SubmitStack(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}

	if out == "" {
		t.Fatal("expected non-empty output from SubmitStack")
	}
}

func TestLogStack(t *testing.T) {
	t.Parallel()

	// Simulate gt log short output.
	bin := mockGT(t, `cat <<'EOF'
◉ swarm/CM-123/add-auth (PR #42)
◯ swarm/CM-123/add-db
◯ main
EOF`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	entries, err := c.LogStack(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Branch != "swarm/CM-123/add-auth" {
		t.Errorf(
			"expected first branch 'swarm/CM-123/add-auth', got %q",
			entries[0].Branch,
		)
	}

	if entries[1].Branch != "swarm/CM-123/add-db" {
		t.Errorf(
			"expected second branch 'swarm/CM-123/add-db', got %q",
			entries[1].Branch,
		)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	bin := mockGT(t, `echo "1.5.0"`)
	c := NewClient(bin, t.TempDir(), slog.Default())

	ver, err := c.Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if ver != "1.5.0" {
		t.Errorf("expected version '1.5.0', got %q", ver)
	}
}

func TestCleanLogLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"◉ my-branch (PR #42)", "my-branch"},
		{"◯ another-branch", "another-branch"},
		{"  ◉ indented-branch", "indented-branch"},
		{"plain-branch", "plain-branch"},
		{"→ arrow-branch", "arrow-branch"},
	}

	for _, tt := range tests {
		got := cleanLogLine(tt.input)
		if got != tt.want {
			t.Errorf("cleanLogLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
