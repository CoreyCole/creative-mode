package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectKeyFromPath(t *testing.T) {
	t.Parallel()

	got := ProjectKeyFromPath("/Users/foo/project")
	want := "-Users-foo-project"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiscoverTranscript_NoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := DiscoverTranscript(dir, "test-project", time.Now().Add(-time.Hour))
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestDiscoverTranscript_FindsRecent(t *testing.T) {
	t.Parallel()

	// Create temp dir with projectKey subdirectory and a .jsonl file.
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "test-project")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := filepath.Join(projectDir, "abc123.jsonl")
	if err := os.WriteFile(f, []byte("{\"type\":\"assistant\"}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := DiscoverTranscript(dir, "test-project", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != f {
		t.Errorf("got %q, want %q", got, f)
	}
}
