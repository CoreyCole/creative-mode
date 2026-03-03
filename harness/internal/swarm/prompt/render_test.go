package prompt

import (
	"strings"
	"testing"

	"creative-mode/harness/internal/swarm"
)

func TestRenderPrompt_AllPhases(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:   "CM-42",
		WorkflowID: "wf-abc",
		SessionID:  "sess-123",
		Phase:      "research",
		Attempt:    1,
		ResultPath: "thoughts/swarm/results/sess-123.md",
		TicketURL:  "https://linear.app/cm/issue/CM-42",
		DryRun:     false,
	}

	phases := []struct {
		phase       swarm.Phase
		phaseName   string
		mustContain []string
	}{
		{
			swarm.PhaseResearch, "research",
			[]string{
				"Research",
				"CM-42",
				"Explore the codebase",
				"Write research document",
			},
		},
		{
			swarm.PhaseCodePlan, "code_plan",
			[]string{"Read research", "Create implementation plan", "File Inventory"},
		},
		{
			swarm.PhasePlanReview, "plan_review",
			[]string{"READ-ONLY", "Review checklist", "Render verdict"},
		},
		{
			swarm.PhaseImplement, "implement",
			[]string{
				"Read the approved plan",
				"Implement step by step",
				"Build Constraints",
			},
		},
		{
			swarm.PhaseVerify, "verify",
			[]string{"READ-ONLY", "Run `just check`", "Do NOT fix failures"},
		},
		{
			swarm.PhasePR, "pr",
			[]string{"Determine branch name", "Create GitHub PR", "git push"},
		},
	}

	for _, tt := range phases {
		t.Run(string(tt.phase), func(t *testing.T) {
			t.Parallel()

			pctx := ctx
			pctx.Phase = tt.phaseName

			result, err := RenderPrompt(tt.phase, pctx)
			if err != nil {
				t.Fatal(err)
			}

			if result.Content == "" {
				t.Error("rendered content is empty")
			}

			if len(result.ContentHash) != 64 {
				t.Errorf(
					"content hash length = %d, want 64 (SHA-256 hex)",
					len(result.ContentHash),
				)
			}

			// Check that shared sections are present.
			for _, section := range []string{
				"CM-42", "wf-abc", "sess-123",
				"Session Initialization", "Result File Output", "Error Handling",
			} {
				if !strings.Contains(result.Content, section) {
					t.Errorf("rendered content missing expected section %q", section)
				}
			}

			// Check phase-specific content.
			for _, s := range tt.mustContain {
				if !strings.Contains(result.Content, s) {
					t.Errorf("rendered content missing phase-specific content %q", s)
				}
			}
		})
	}
}

func TestRenderPrompt_DeterministicHash(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:   "CM-1",
		WorkflowID: "wf-1",
		SessionID:  "s-1",
		Phase:      "research",
		Attempt:    1,
		ResultPath: "thoughts/swarm/results/test.md",
	}

	r1, err := RenderPrompt(swarm.PhaseResearch, ctx)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := RenderPrompt(swarm.PhaseResearch, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if r1.ContentHash != r2.ContentHash {
		t.Errorf("hashes differ for same input: %s vs %s", r1.ContentHash, r2.ContentHash)
	}
}

func TestRenderPrompt_DryRunSection(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:   "CM-1",
		WorkflowID: "wf-1",
		SessionID:  "s-1",
		Phase:      "research",
		Attempt:    1,
		ResultPath: "thoughts/swarm/results/test.md",
		DryRun:     true,
	}

	result, err := RenderPrompt(swarm.PhaseResearch, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "DRY-RUN") {
		t.Error("dry-run content not present when DryRun=true")
	}
}

func TestRenderPrompt_NoDryRunByDefault(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:   "CM-1",
		WorkflowID: "wf-1",
		SessionID:  "s-1",
		Phase:      "research",
		Attempt:    1,
		ResultPath: "thoughts/swarm/results/test.md",
		DryRun:     false,
	}

	result, err := RenderPrompt(swarm.PhaseResearch, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Content, "DRY-RUN") {
		t.Error("dry-run content present when DryRun=false")
	}
}

func TestRenderPrompt_HandoffContent(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:       "CM-1",
		WorkflowID:     "wf-1",
		SessionID:      "s-1",
		Phase:          "code_plan",
		Attempt:        2,
		ResultPath:     "/tmp/result.txt",
		HandoffContent: "## BLUF\nPrevious session found 3 key files.",
	}

	result, err := RenderPrompt(swarm.PhaseCodePlan, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "Previous session found 3 key files") {
		t.Error("handoff content not inlined")
	}
	if !strings.Contains(result.Content, "Handoff Context") {
		t.Error("handoff section header missing")
	}
}

func TestRenderPrompt_LearningContent(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID:        "CM-1",
		WorkflowID:      "wf-1",
		SessionID:       "s-1",
		Phase:           "implement",
		Attempt:         1,
		ResultPath:      "/tmp/result.txt",
		LearningContent: "## Phase Learnings\n- Always run just check",
	}

	result, err := RenderPrompt(swarm.PhaseImplement, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "Always run just check") {
		t.Error("learning content not inlined")
	}
	if !strings.Contains(result.Content, "Learning Context") {
		t.Error("learning section header missing")
	}
}

func TestRenderPrompt_UnknownPhase(t *testing.T) {
	t.Parallel()

	ctx := PromptContext{
		TicketID: "CM-1",
	}

	_, err := RenderPrompt(swarm.Phase("nonexistent"), ctx)
	if err == nil {
		t.Error("expected error for unknown phase")
	}
}
