package swarmorch

import (
	"testing"

	"creative-mode/harness/internal/swarm"
)

func TestDeriveLearningTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase    swarm.Phase
		category swarm.LearningCategory
		want     string
	}{
		{swarm.PhaseImplement, swarm.LearningCodeBug, "implement,code_bug"},
		{swarm.PhaseResearch, swarm.LearningPattern, "research,pattern"},
		{"", swarm.LearningConvention, "convention"},
		{swarm.PhaseVerify, swarm.LearningPostMortem, "verify,post_mortem"},
	}

	for _, tt := range tests {
		result := deriveLearningTags(tt.phase, tt.category)
		if !result.Valid || result.String != tt.want {
			t.Errorf("deriveLearningTags(%q, %q) = %q, want %q",
				tt.phase, tt.category, result.String, tt.want)
		}
	}
}
