package swarmorch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLWriter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	w, err := NewJSONLWriter(dir, "TICK-1", "sess-01")
	if err != nil {
		t.Fatalf("NewJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Write two entries.
	writeErr := w.Write(map[string]any{
		"event":   "session_started",
		"session": "sess-01",
	})
	if writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	writeErr = w.Write(map[string]any{
		"event":   "tool_use",
		"tool":    "Bash",
		"session": "sess-01",
	})
	if writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	_ = w.Close()

	// Verify file contents.
	path := filepath.Join(dir, "swarm", "logs", "TICK-1", "sess-01.jsonl")
	f, err := os.Open(path) //nolint:gosec // test file with known path
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var entries []map[string]any

	for scanner.Scan() {
		var entry map[string]any
		if unmarshalErr := json.Unmarshal(scanner.Bytes(), &entry); unmarshalErr != nil {
			t.Fatalf("unmarshal line: %v", unmarshalErr)
		}

		entries = append(entries, entry)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries; want 2", len(entries))
	}

	// Both should have timestamps.
	for i, e := range entries {
		if _, ok := e["ts"]; !ok {
			t.Errorf("entry %d missing 'ts' field", i)
		}
	}

	if entries[0]["event"] != "session_started" {
		t.Errorf("entry 0 event = %v; want session_started", entries[0]["event"])
	}
	if entries[1]["event"] != "tool_use" {
		t.Errorf("entry 1 event = %v; want tool_use", entries[1]["event"])
	}
}

func TestLogPath(t *testing.T) {
	t.Parallel()

	got := LogPath("/data", "TICK-1", "sess-01")
	want := "/data/swarm/logs/TICK-1/sess-01.jsonl"
	if got != want {
		t.Errorf("LogPath = %q; want %q", got, want)
	}
}
