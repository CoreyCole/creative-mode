package swarm

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// SessionResultData holds the parsed output from a skill session's RESULT file.
type SessionResultData struct {
	Result      SessionResult
	Phase       Phase
	HandoffPath string
	Summary     string
}

// ParseResultFile reads and parses a RESULT file written by a skill session.
// The expected format is:
//
//	RESULT: success
//	Phase: research
//	Handoff: thoughts/swarm/handoffs-research/2026-02-28_CM-123_findings.md
//
//	Summary: Completed research with 3 key findings
//
// Returns ResultInfraFailure data if the file is missing or unparseable.
func ParseResultFile(path string) (*SessionResultData, error) {
	f, err := os.Open(path) //nolint:gosec // trusted internal path
	if err != nil {
		return &SessionResultData{
			Result:  ResultInfraFailure,
			Summary: "result file missing: " + path,
		}, nil
	}
	defer func() { _ = f.Close() }()

	data := &SessionResultData{}
	scanner := bufio.NewScanner(f)
	foundResult := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "RESULT":
			data.Result = SessionResult(value)
			foundResult = true
		case "Phase":
			data.Phase = Phase(value)
		case "Handoff":
			data.HandoffPath = value
		case "Summary":
			data.Summary = value
		}
	}

	if err := scanner.Err(); err != nil {
		return &SessionResultData{
			Result:  ResultInfraFailure,
			Summary: fmt.Sprintf("error reading result file: %v", err),
		}, nil
	}

	if !foundResult {
		return &SessionResultData{
			Result:  ResultInfraFailure,
			Summary: "result file missing RESULT field",
		}, nil
	}

	if !data.Result.Valid() {
		return &SessionResultData{
			Result:  ResultInfraFailure,
			Summary: fmt.Sprintf("invalid result value: %q", data.Result),
		}, nil
	}

	return data, nil
}
