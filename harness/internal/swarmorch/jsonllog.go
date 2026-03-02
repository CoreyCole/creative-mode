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

// Write appends a JSON log entry with a timestamp. Accepts any serializable
// struct (typed event structs from events.go or map[string]any).
func (w *JSONLWriter) Write(event any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Marshal the event, then inject a "ts" field.
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}

	// Inject timestamp if not already present.
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if _, hasTSField := raw["ts"]; !hasTSField {
			tsJSON, tsErr := json.Marshal(time.Now().UTC().Format(time.RFC3339))
			if tsErr != nil {
				return fmt.Errorf("marshal timestamp: %w", tsErr)
			}

			raw["ts"] = tsJSON

			data, err = json.Marshal(raw)
			if err != nil {
				return fmt.Errorf("marshal with ts: %w", err)
			}
		}
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
