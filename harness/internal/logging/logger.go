package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// NewLogger creates a structured JSON logger that writes to both stderr
// and a JSONL file in the specified log directory.
func NewLogger(logDir string) (*slog.Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("creating log directory %s: %w", logDir, err)
	}

	logFile, err := os.OpenFile(
		filepath.Join(logDir, "harness.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	multiWriter := io.MultiWriter(os.Stderr, logFile)
	handler := slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(handler), nil
}
