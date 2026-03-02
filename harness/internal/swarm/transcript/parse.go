package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

const (
	scanBufSize    = 1 << 20  // 1 MiB initial buffer
	scanMaxBufSize = 10 << 20 // 10 MiB max line size
)

// TokenSummary holds aggregated token usage from a Claude Code transcript.
type TokenSummary struct {
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	TotalTokens         int64   `json:"totalTokens"`
	Model               string  `json:"model"`
	EstimatedCostUSD    float64 `json:"estimatedCostUsd"`
}

// transcriptEntry represents a single JSONL line from a Claude Code transcript.
type transcriptEntry struct {
	Type    string        `json:"type"`
	Message *messageEntry `json:"message,omitempty"`
	Model   string        `json:"model,omitempty"`
	Usage   *usageEntry   `json:"usage,omitempty"`
}

type messageEntry struct {
	Model string      `json:"model,omitempty"`
	Usage *usageEntry `json:"usage,omitempty"`
}

type usageEntry struct {
	InputTokens              int64 `json:"input_tokens"`                //nolint:tagliatelle // Claude API format
	OutputTokens             int64 `json:"output_tokens"`               //nolint:tagliatelle // Claude API format
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`     //nolint:tagliatelle // Claude API format
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"` //nolint:tagliatelle // Claude API format
}

// ParseFile reads a Claude Code transcript JSONL file and aggregates token usage.
func ParseFile(path string) (*TokenSummary, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from trusted transcript discovery
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	summary := &TokenSummary{}
	scanner := bufio.NewScanner(f)
	// Claude Code transcripts can have very long lines.
	scanner.Buffer(make([]byte, 0, scanBufSize), scanMaxBufSize)

	for scanner.Scan() {
		var entry transcriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // skip malformed lines
		}

		if entry.Type != "assistant" {
			continue
		}

		// Extract model from first assistant turn.
		if summary.Model == "" {
			if entry.Model != "" {
				summary.Model = entry.Model
			} else if entry.Message != nil && entry.Message.Model != "" {
				summary.Model = entry.Message.Model
			}
		}

		// Aggregate usage — check both top-level and nested message.usage.
		usage := extractUsage(entry)
		if usage == nil {
			continue
		}

		summary.InputTokens += usage.InputTokens
		summary.OutputTokens += usage.OutputTokens
		summary.CacheReadTokens += usage.CacheReadInputTokens
		summary.CacheCreationTokens += usage.CacheCreationInputTokens
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}

	summary.TotalTokens = summary.InputTokens + summary.OutputTokens +
		summary.CacheReadTokens + summary.CacheCreationTokens
	summary.EstimatedCostUSD = EstimateCost(
		summary.Model,
		summary.InputTokens,
		summary.OutputTokens,
		summary.CacheReadTokens,
		summary.CacheCreationTokens,
	)

	return summary, nil
}

// extractUsage returns the usage data from a transcript entry,
// checking both top-level and message-nested usage fields.
func extractUsage(entry transcriptEntry) *usageEntry {
	if entry.Usage != nil {
		return entry.Usage
	}
	if entry.Message != nil && entry.Message.Usage != nil {
		return entry.Message.Usage
	}
	return nil
}
