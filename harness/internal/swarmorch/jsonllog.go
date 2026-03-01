package swarmorch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONLWriter appends JSON log entries to a per-session JSONL file at
// data/swarm/logs/{ticketID}/{sessionID}.jsonl.
type JSONLWriter struct {
	file *os.File
	mu   sync.Mutex
}

// NewJSONLWriter creates (or opens) the JSONL log file for a session.
func NewJSONLWriter(logsDir, ticketID, sessionID string) (*JSONLWriter, error) {
	dir := filepath.Join(logsDir, "swarm", "logs", ticketID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	path := filepath.Join(dir, sessionID+".jsonl")

	cleanPath := filepath.Clean(path)

	f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &JSONLWriter{file: f}, nil
}

// Write appends a JSON log entry with a timestamp.
func (w *JSONLWriter) Write(event map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if event["ts"] == nil {
		event["ts"] = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}

	data = append(data, '\n')

	_, err = w.file.Write(data)

	return err
}

// Close closes the underlying file.
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.file.Close()
}

// LogPath returns the JSONL log file path for a session (without opening it).
func LogPath(logsDir, ticketID, sessionID string) string {
	return filepath.Join(logsDir, "swarm", "logs", ticketID, sessionID+".jsonl")
}
