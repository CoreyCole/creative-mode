package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile(t *testing.T) {
	t.Parallel()

	// Create a temp JSONL file simulating a Claude Code transcript.
	content := `{"type":"system","message":"system init"}
{"type":"human","message":{"content":"hello"}}
{"type":"assistant","model":"claude-sonnet-4-20250514","message":{"model":"claude-sonnet-4-20250514","usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":200,"cache_creation_input_tokens":100}}}
{"type":"assistant","model":"claude-sonnet-4-20250514","message":{"model":"claude-sonnet-4-20250514","usage":{"input_tokens":800,"output_tokens":300,"cache_read_input_tokens":150,"cache_creation_input_tokens":50}}}
{"type":"result","result":"done"}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if summary.InputTokens != 1800 {
		t.Errorf("InputTokens = %d, want 1800", summary.InputTokens)
	}
	if summary.OutputTokens != 800 {
		t.Errorf("OutputTokens = %d, want 800", summary.OutputTokens)
	}
	if summary.CacheReadTokens != 350 {
		t.Errorf("CacheReadTokens = %d, want 350", summary.CacheReadTokens)
	}
	if summary.CacheCreationTokens != 150 {
		t.Errorf("CacheCreationTokens = %d, want 150", summary.CacheCreationTokens)
	}
	if summary.TotalTokens != 3100 {
		t.Errorf("TotalTokens = %d, want 3100", summary.TotalTokens)
	}
	if summary.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", summary.Model)
	}
	if summary.EstimatedCostUSD <= 0 {
		t.Error("EstimatedCostUSD should be > 0")
	}
}

func TestParseFile_TopLevelUsage(t *testing.T) {
	t.Parallel()

	// Some transcript formats put usage at the top level.
	content := `{"type":"assistant","model":"claude-opus-4-20250514","usage":{"input_tokens":500,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if summary.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", summary.InputTokens)
	}
	if summary.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", summary.OutputTokens)
	}
	if summary.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q, want claude-opus-4-20250514", summary.Model)
	}
}

func TestParseFile_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", summary.TotalTokens)
	}
}

func TestParseFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ParseFile("/nonexistent/path.jsonl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
