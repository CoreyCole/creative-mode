package swarmorch

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"creative-mode/harness/internal/swarm"
)

func TestSessionLogCorrelationFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	sl := NewSessionLog(base, "TICK-1", "wf-001", "sess-01", swarm.PhaseResearch)
	sl.Info("test message", "extra", "value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	checks := map[string]string{
		"subsystem":   "swarm",
		"ticket_id":   "TICK-1",
		"workflow_id": "wf-001",
		"session_id":  "sess-01",
		"phase":       "research",
		"extra":       "value",
		"msg":         "test message",
	}
	for key, want := range checks {
		got, _ := entry[key].(string)
		if got != want {
			t.Errorf("key %q = %q; want %q", key, got, want)
		}
	}
}

func TestSessionLogLevels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(
		slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)

	sl := NewSessionLog(base, "T", "W", "S", swarm.PhaseImplement)

	tests := []struct {
		fn    func(string, ...any)
		level string
	}{
		{sl.Info, "INFO"},
		{sl.Warn, "WARN"},
		{sl.Error, "ERROR"},
	}

	for _, tt := range tests {
		buf.Reset()
		tt.fn("msg")

		var entry map[string]any
		if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		got, _ := entry["level"].(string)
		if got != tt.level {
			t.Errorf("level = %q; want %q", got, tt.level)
		}
	}
}
