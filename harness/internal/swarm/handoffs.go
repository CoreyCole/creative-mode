package swarm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	handoffTimestampFormat = "2006-01-02_15-04-05"
	maxFilenameDetailLen   = 50
)

var nonAlphanumHyphen = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// ResolveHandoffPath finds the most recent handoff document for a ticket across
// all handoff directories under baseDir/thoughts/swarm/. Returns "" with nil
// error if no matches are found.
func ResolveHandoffPath(baseDir, ticketID string) (string, error) {
	pattern := filepath.Join(
		baseDir,
		"thoughts",
		"swarm",
		"handoffs-*",
		fmt.Sprintf("*_%s_*.md", ticketID),
	)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob handoffs: %w", err)
	}

	if len(matches) == 0 {
		return "", nil
	}

	// Sort by base filename descending — timestamp prefix ensures newest first
	sort.Slice(matches, func(i, j int) bool {
		return filepath.Base(matches[i]) > filepath.Base(matches[j])
	})

	return matches[0], nil
}

// ResolveResearchPath finds the most recent research document for a ticket.
// Returns "" with nil error if no matches are found.
func ResolveResearchPath(baseDir, ticketID string) (string, error) {
	pattern := filepath.Join(
		baseDir,
		"thoughts",
		"swarm",
		"research",
		fmt.Sprintf("*_%s_*.md", ticketID),
	)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob research: %w", err)
	}

	if len(matches) == 0 {
		return "", nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return filepath.Base(matches[i]) > filepath.Base(matches[j])
	})

	return matches[0], nil
}

// HandoffDir returns the handoff subdirectory name for a given phase.
func HandoffDir(phase Phase) string {
	switch phase {
	case PhaseResearch:
		return "handoffs-research"
	case PhaseCodePlan, PhaseImplement, PhasePR:
		return "handoffs-code"
	case PhasePlanReview:
		return "handoffs-plan-reviews"
	case PhaseVerify:
		return "handoffs-code-reviews"
	case PhaseProjectPlan, PhaseProjectVerify:
		return "handoffs-project"
	case PhaseProjectReview:
		return "handoffs-project-reviews"
	case PhaseDone, PhaseFailed:
		return ""
	default:
		return ""
	}
}

// FormatHandoffFilename returns a handoff filename with timestamp, ticket ID,
// and sanitized detail slug.
func FormatHandoffFilename(ticketID, detail string) string {
	ts := time.Now().UTC().Format(handoffTimestampFormat)
	slug := sanitizeFilename(detail)

	return fmt.Sprintf("%s_%s_%s.md", ts, ticketID, slug)
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, " ", "-")
	s = nonAlphanumHyphen.ReplaceAllString(s, "")
	s = strings.ToLower(s)

	if len(s) > maxFilenameDetailLen {
		return s[:maxFilenameDetailLen]
	}

	return s
}
