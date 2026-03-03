package swarm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseResultFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantResult  SessionResult
		wantPhase   Phase
		wantHandoff string
		wantSummary string
	}{
		{
			name: "success",
			content: `RESULT: success
Phase: research
Handoff: thoughts/swarm/handoffs-research/2026-02-28_CM-123_findings.md

Summary: Completed research with 3 key findings`,
			wantResult:  ResultSuccess,
			wantPhase:   PhaseResearch,
			wantHandoff: "thoughts/swarm/handoffs-research/2026-02-28_CM-123_findings.md",
			wantSummary: "Completed research with 3 key findings",
		},
		{
			name: "logic_failure",
			content: `RESULT: logic_failure
Phase: plan_review
Handoff: thoughts/swarm/handoffs-plan-reviews/2026-02-28_CM-123_revise.md

Summary: Plan v1 needs revision`,
			wantResult:  ResultLogicFailure,
			wantPhase:   PhasePlanReview,
			wantHandoff: "thoughts/swarm/handoffs-plan-reviews/2026-02-28_CM-123_revise.md",
			wantSummary: "Plan v1 needs revision",
		},
		{
			name: "infra_failure",
			content: `RESULT: infra_failure
Phase: verify

Summary: just check failed to run`,
			wantResult:  ResultInfraFailure,
			wantPhase:   PhaseVerify,
			wantSummary: "just check failed to run",
		},
		{
			name: "context_limit",
			content: `RESULT: context_limit
Phase: implement

Summary: Hit context window limit mid-implementation`,
			wantResult:  ResultContextLimit,
			wantPhase:   PhaseImplement,
			wantSummary: "Hit context window limit mid-implementation",
		},
		{
			name: "timeout",
			content: `RESULT: timeout
Phase: code_plan

Summary: Session timed out`,
			wantResult:  ResultTimeout,
			wantPhase:   PhaseCodePlan,
			wantSummary: "Session timed out",
		},
		{
			name: "minimal_success",
			content: `RESULT: success
Phase: pr

Summary: Opened PR`,
			wantResult:  ResultSuccess,
			wantPhase:   PhasePR,
			wantSummary: "Opened PR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "result.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write result file: %v", err)
			}

			data, err := ParseResultFile(path)
			if err != nil {
				t.Fatalf("ParseResultFile: %v", err)
			}

			if data.Result != tt.wantResult {
				t.Errorf("Result = %q, want %q", data.Result, tt.wantResult)
			}
			if data.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", data.Phase, tt.wantPhase)
			}
			if data.HandoffPath != tt.wantHandoff {
				t.Errorf("HandoffPath = %q, want %q", data.HandoffPath, tt.wantHandoff)
			}
			if data.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", data.Summary, tt.wantSummary)
			}
		})
	}
}

func TestParseResultFileMissing(t *testing.T) {
	t.Parallel()

	data, err := ParseResultFile("/nonexistent/path/result.txt")
	if err != nil {
		t.Fatalf("ParseResultFile: %v", err)
	}

	if data.Result != ResultInfraFailure {
		t.Errorf("Result = %q, want %q", data.Result, ResultInfraFailure)
	}
	if data.Summary == "" {
		t.Error("expected non-empty summary for missing file")
	}
}

func TestParseResultFileEmpty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "result.txt")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := ParseResultFile(path)
	if err != nil {
		t.Fatalf("ParseResultFile: %v", err)
	}

	if data.Result != ResultInfraFailure {
		t.Errorf("Result = %q, want %q", data.Result, ResultInfraFailure)
	}
	if data.Summary != "result file missing RESULT field" {
		t.Errorf("Summary = %q, want missing RESULT field message", data.Summary)
	}
}

func TestParseResultFileMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{"no_colon", "RESULT success\nPhase research"},
		{"invalid_result", "RESULT: not_a_valid_result\nPhase: research"},
		{"only_whitespace", "   \n  \n  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "result.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}

			data, err := ParseResultFile(path)
			if err != nil {
				t.Fatalf("ParseResultFile: %v", err)
			}

			if data.Result != ResultInfraFailure {
				t.Errorf(
					"Result = %q, want %q for malformed input",
					data.Result,
					ResultInfraFailure,
				)
			}
		})
	}
}

func TestParseResultFileInProgress(t *testing.T) {
	t.Parallel()

	content := "RESULT: in_progress\nPhase: research\n\nSummary: Starting research phase...\n"
	path := filepath.Join(t.TempDir(), "result.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := ParseResultFile(path)
	if err != nil {
		t.Fatalf("ParseResultFile: %v", err)
	}

	if data.Result != ResultInfraFailure {
		t.Errorf(
			"Result = %q, want %q (in_progress treated as crash)",
			data.Result,
			ResultInfraFailure,
		)
	}
	if data.Phase != PhaseResearch {
		t.Errorf("Phase = %q, want %q", data.Phase, PhaseResearch)
	}
	if data.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestParseResultFileInProgressNoSummary(t *testing.T) {
	t.Parallel()

	content := "RESULT: in_progress\nPhase: implement\n"
	path := filepath.Join(t.TempDir(), "result.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := ParseResultFile(path)
	if err != nil {
		t.Fatalf("ParseResultFile: %v", err)
	}

	if data.Result != ResultInfraFailure {
		t.Errorf("Result = %q, want %q", data.Result, ResultInfraFailure)
	}
	if data.Summary != "session crashed mid-execution" {
		t.Errorf("Summary = %q, want 'session crashed mid-execution'", data.Summary)
	}
}

func TestParseResultFileMissingOptionalFields(t *testing.T) {
	t.Parallel()

	content := "RESULT: success\n"
	path := filepath.Join(t.TempDir(), "result.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := ParseResultFile(path)
	if err != nil {
		t.Fatalf("ParseResultFile: %v", err)
	}

	if data.Result != ResultSuccess {
		t.Errorf("Result = %q, want %q", data.Result, ResultSuccess)
	}
	if data.Phase != "" {
		t.Errorf("Phase = %q, want empty", data.Phase)
	}
	if data.HandoffPath != "" {
		t.Errorf("HandoffPath = %q, want empty", data.HandoffPath)
	}
	if data.Summary != "" {
		t.Errorf("Summary = %q, want empty", data.Summary)
	}
}
