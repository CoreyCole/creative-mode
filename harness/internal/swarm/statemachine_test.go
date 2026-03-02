package swarm

import "testing"

func TestDetermineNextPhase(t *testing.T) {
	t.Parallel()

	config := DefaultConfig

	tests := []struct {
		name         string
		workflowType WorkflowType
		phase        Phase
		attempt      int
		result       SessionResult
		wantPhase    Phase
		wantRetry    bool
		wantFailed   bool
	}{
		// No session yet — starts at research for all types.
		{
			name:         "new code workflow starts at research",
			workflowType: WorkflowTypeCode,
			result:       "",
			wantPhase:    PhaseResearch,
		},
		{
			name:         "new project workflow starts at research",
			workflowType: WorkflowTypeProject,
			result:       "",
			wantPhase:    PhaseResearch,
		},
		{
			name:         "new research workflow starts at research",
			workflowType: WorkflowTypeResearch,
			result:       "",
			wantPhase:    PhaseResearch,
		},

		// Standalone research workflow: research → done.
		{
			name:         "research success (standalone) → done",
			workflowType: WorkflowTypeResearch,
			phase:        PhaseResearch,
			result:       ResultSuccess,
			wantPhase:    PhaseDone,
		},

		// Code workflow happy path.
		{
			name:         "research success → code_plan",
			workflowType: WorkflowTypeCode,
			phase:        PhaseResearch,
			result:       ResultSuccess,
			wantPhase:    PhaseCodePlan,
		},
		{
			name:         "code_plan success → plan_review",
			workflowType: WorkflowTypeCode,
			phase:        PhaseCodePlan,
			result:       ResultSuccess,
			wantPhase:    PhasePlanReview,
		},
		{
			name:         "plan_review approve → implement",
			workflowType: WorkflowTypeCode,
			phase:        PhasePlanReview,
			result:       ResultSuccess,
			wantPhase:    PhaseImplement,
		},
		{
			name:         "implement success → verify",
			workflowType: WorkflowTypeCode,
			phase:        PhaseImplement,
			result:       ResultSuccess,
			wantPhase:    PhaseVerify,
		},
		{
			name:         "verify success → pr",
			workflowType: WorkflowTypeCode,
			phase:        PhaseVerify,
			result:       ResultSuccess,
			wantPhase:    PhasePR,
		},
		{
			name:         "pr success → human_review",
			workflowType: WorkflowTypeCode,
			phase:        PhasePR,
			result:       ResultSuccess,
			wantPhase:    PhaseHumanReview,
		},
		{
			name:         "human_review success → done",
			workflowType: WorkflowTypeCode,
			phase:        PhaseHumanReview,
			result:       ResultSuccess,
			wantPhase:    PhaseDone,
		},

		// Plan review revise cycle.
		{
			name:         "plan_review revise under max → code_plan retry",
			workflowType: WorkflowTypeCode,
			phase:        PhasePlanReview,
			attempt:      1,
			result:       ResultLogicFailure,
			wantPhase:    PhaseCodePlan,
			wantRetry:    true,
		},
		{
			name:         "plan_review revise at max → failed",
			workflowType: WorkflowTypeCode,
			phase:        PhasePlanReview,
			attempt:      3,
			result:       ResultLogicFailure,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},

		// Verify retry cycle.
		{
			name:         "verify logic_failure under max → implement retry",
			workflowType: WorkflowTypeCode,
			phase:        PhaseVerify,
			attempt:      1,
			result:       ResultLogicFailure,
			wantPhase:    PhaseImplement,
			wantRetry:    true,
		},
		{
			name:         "verify logic_failure at max → failed",
			workflowType: WorkflowTypeCode,
			phase:        PhaseVerify,
			attempt:      3,
			result:       ResultLogicFailure,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},

		// Infra failure retries.
		{
			name:         "infra_failure attempt 1 → same phase retry",
			workflowType: WorkflowTypeCode,
			phase:        PhaseImplement,
			attempt:      1,
			result:       ResultInfraFailure,
			wantPhase:    PhaseImplement,
			wantRetry:    true,
		},
		{
			name:         "infra_failure attempt 2 → failed",
			workflowType: WorkflowTypeCode,
			phase:        PhaseImplement,
			attempt:      2,
			result:       ResultInfraFailure,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},

		// Timeout always terminal.
		{
			name:         "timeout → failed",
			workflowType: WorkflowTypeCode,
			phase:        PhaseCodePlan,
			result:       ResultTimeout,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},

		// Context limit — resume same phase, no retry flag.
		{
			name:         "context_limit → same phase (no retry)",
			workflowType: WorkflowTypeCode,
			phase:        PhaseImplement,
			attempt:      1,
			result:       ResultContextLimit,
			wantPhase:    PhaseImplement,
		},

		// Project workflow transitions.
		{
			name:         "research success (project) → project_decompose",
			workflowType: WorkflowTypeProject,
			phase:        PhaseResearch,
			result:       ResultSuccess,
			wantPhase:    PhaseProjectDecompose,
		},
		{
			name:         "project_decompose success → project_plan",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectDecompose,
			result:       ResultSuccess,
			wantPhase:    PhaseProjectPlan,
		},
		{
			name:         "project_decompose logic_failure → failed",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectDecompose,
			result:       ResultLogicFailure,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},
		{
			name:         "project_plan success → project_review",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectPlan,
			result:       ResultSuccess,
			wantPhase:    PhaseProjectReview,
		},
		{
			name:         "project_review approve → project_verify",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectReview,
			result:       ResultSuccess,
			wantPhase:    PhaseProjectVerify,
		},
		{
			name:         "project_review revise under max → project_plan retry",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectReview,
			attempt:      1,
			result:       ResultLogicFailure,
			wantPhase:    PhaseProjectPlan,
			wantRetry:    true,
		},
		{
			name:         "project_review revise at max → failed",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectReview,
			attempt:      3,
			result:       ResultLogicFailure,
			wantPhase:    PhaseFailed,
			wantFailed:   true,
		},
		{
			name:         "project_verify success → done",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectVerify,
			result:       ResultSuccess,
			wantPhase:    PhaseDone,
		},
		{
			name:         "project_verify milestones failed → retry",
			workflowType: WorkflowTypeProject,
			phase:        PhaseProjectVerify,
			result:       ResultLogicFailure,
			wantPhase:    PhaseProjectVerify,
			wantRetry:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DetermineNextPhase(
				tt.workflowType,
				tt.phase,
				tt.attempt,
				tt.result,
				config,
			)
			if got.NextPhase != tt.wantPhase {
				t.Errorf("NextPhase = %q, want %q", got.NextPhase, tt.wantPhase)
			}
			if got.Retry != tt.wantRetry {
				t.Errorf("Retry = %v, want %v", got.Retry, tt.wantRetry)
			}
			if got.Failed != tt.wantFailed {
				t.Errorf("Failed = %v, want %v", got.Failed, tt.wantFailed)
			}
		})
	}
}

func TestSkillForPhase(t *testing.T) {
	t.Parallel()

	actionPhases := []Phase{
		PhaseResearch, PhaseCodePlan, PhasePlanReview, PhaseImplement,
		PhaseVerify, PhasePR, PhaseProjectDecompose, PhaseProjectPlan,
		PhaseProjectReview, PhaseProjectVerify,
	}
	for _, p := range actionPhases {
		if skill := SkillForPhase(p); skill == "" {
			t.Errorf("SkillForPhase(%q) returned empty string", p)
		}
	}

	// Terminal/gate phases should return empty.
	for _, p := range []Phase{PhaseHumanReview, PhaseDone, PhaseFailed} {
		if skill := SkillForPhase(p); skill != "" {
			t.Errorf("SkillForPhase(%q) = %q, want empty", p, skill)
		}
	}
}

func TestLogicFailureFallthroughPhases(t *testing.T) {
	t.Parallel()

	config := DefaultConfig

	// Phases where logic_failure has no special handling — should fail the workflow.
	fallthroughPhases := []struct {
		name  string
		phase Phase
	}{
		{"research logic_failure → failed", PhaseResearch},
		{"code_plan logic_failure → failed", PhaseCodePlan},
		{"implement logic_failure → failed", PhaseImplement},
		{"pr logic_failure → failed", PhasePR},
		{"project_plan logic_failure → failed", PhaseProjectPlan},
	}

	for _, tt := range fallthroughPhases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DetermineNextPhase(
				WorkflowTypeCode,
				tt.phase,
				1,
				ResultLogicFailure,
				config,
			)
			if got.NextPhase != PhaseFailed {
				t.Errorf("NextPhase = %q, want %q", got.NextPhase, PhaseFailed)
			}
			if !got.Failed {
				t.Error("Failed = false, want true")
			}
		})
	}
}

func TestTerminalPhasesReturnFailed(t *testing.T) {
	t.Parallel()

	config := DefaultConfig

	// Calling DetermineNextPhase on terminal phases should return PhaseFailed.
	for _, phase := range []Phase{PhaseDone, PhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()

			got := DetermineNextPhase(WorkflowTypeCode, phase, 1, ResultSuccess, config)
			if got.NextPhase != PhaseFailed {
				t.Errorf("NextPhase = %q, want %q", got.NextPhase, PhaseFailed)
			}
			if !got.Failed {
				t.Error("Failed = false, want true")
			}
		})
	}
}

func TestFirstPhaseIsResearch(t *testing.T) {
	t.Parallel()

	// All workflow types start at research.
	for _, wt := range []WorkflowType{WorkflowTypeResearch, WorkflowTypeCode, WorkflowTypeProject} {
		got := DetermineNextPhase(wt, "", 0, "", DefaultConfig)
		if got.NextPhase != PhaseResearch {
			t.Errorf(
				"DetermineNextPhase(%q, empty) = %q, want %q",
				wt,
				got.NextPhase,
				PhaseResearch,
			)
		}
	}
}

func TestIsGatedTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		phase  Phase
		result SessionResult
		config SwarmConfig
		want   bool
	}{
		{
			name:   "plan_review gated when enabled",
			phase:  PhasePlanReview,
			result: ResultSuccess,
			config: SwarmConfig{GatePlanReview: true},
			want:   true,
		},
		{
			name:   "plan_review not gated when disabled",
			phase:  PhasePlanReview,
			result: ResultSuccess,
			config: SwarmConfig{GatePlanReview: false},
			want:   false,
		},
		{
			name:   "project_review gated when enabled",
			phase:  PhaseProjectReview,
			result: ResultSuccess,
			config: SwarmConfig{GateProjectReview: true},
			want:   true,
		},
		{
			name:   "project_review not gated when disabled",
			phase:  PhaseProjectReview,
			result: ResultSuccess,
			config: SwarmConfig{GateProjectReview: false},
			want:   false,
		},
		{
			name:   "non-success result never gated",
			phase:  PhasePlanReview,
			result: ResultLogicFailure,
			config: SwarmConfig{GatePlanReview: true},
			want:   false,
		},
		{
			name:   "implement not gated",
			phase:  PhaseImplement,
			result: ResultSuccess,
			config: SwarmConfig{GatePlanReview: true, GateProjectReview: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsGatedTransition(tt.phase, tt.result, tt.config)
			if got != tt.want {
				t.Errorf(
					"IsGatedTransition(%q, %q) = %v, want %v",
					tt.phase,
					tt.result,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestGateRejectionTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		gatePhase Phase
		target    RevisionTarget
		wantPhase Phase
		wantOK    bool
	}{
		{"plan_review → code_plan", PhasePlanReview, "", PhaseCodePlan, true},
		{"project_review → project_plan", PhaseProjectReview, "", PhaseProjectPlan, true},
		{"pr → implement", PhasePR, "", PhaseImplement, true},
		{
			"human_review → implement (default)",
			PhaseHumanReview,
			"",
			PhaseImplement,
			true,
		},
		{
			"human_review → implement (explicit)",
			PhaseHumanReview,
			RevisionTargetImplement,
			PhaseImplement,
			true,
		},
		{
			"human_review → code_plan (re-plan)",
			PhaseHumanReview,
			RevisionTargetCodePlan,
			PhaseCodePlan,
			true,
		},
		{
			"pr → code_plan (re-plan)",
			PhasePR,
			RevisionTargetCodePlan,
			PhaseCodePlan,
			true,
		},
		{"research → no target", PhaseResearch, "", "", false},
		{"done → no target", PhaseDone, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPhase, gotOK := GateRejectionTarget(tt.gatePhase, tt.target)
			if gotPhase != tt.wantPhase || gotOK != tt.wantOK {
				t.Errorf("GateRejectionTarget(%q, %q) = (%q, %v), want (%q, %v)",
					tt.gatePhase, tt.target, gotPhase, gotOK, tt.wantPhase, tt.wantOK)
			}
		})
	}
}
