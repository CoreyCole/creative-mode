package swarmorch

import (
	"database/sql"
	"testing"

	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarm"
)

func TestDetectPatternsCodeBugs(t *testing.T) {
	t.Parallel()

	grouped := map[string][]sqlc.SwarmLearning{
		"code_bug": {
			{
				Category: swarm.LearningCodeBug,
				Tags:     sql.NullString{String: "lint", Valid: true},
			},
			{
				Category: swarm.LearningCodeBug,
				Tags:     sql.NullString{String: "lint", Valid: true},
			},
		},
	}

	actions := DetectPatterns(grouped)

	found := false
	for _, a := range actions {
		if contains(a, "swarm-code-verify") && contains(a, "lint") {
			found = true
		}
	}

	if !found {
		t.Errorf("expected action for repeated lint tag, got: %v", actions)
	}
}

func TestDetectPatternsCodeBugsNoTags(t *testing.T) {
	t.Parallel()

	grouped := map[string][]sqlc.SwarmLearning{
		"code_bug": {
			{Category: swarm.LearningCodeBug},
			{Category: swarm.LearningCodeBug},
		},
	}

	actions := DetectPatterns(grouped)

	found := false
	for _, a := range actions {
		if contains(a, "swarm-code-verify") {
			found = true
		}
	}

	if !found {
		t.Errorf("expected action for code bugs without tags, got: %v", actions)
	}
}

func TestDetectPatternsPlanIssues(t *testing.T) {
	t.Parallel()

	grouped := map[string][]sqlc.SwarmLearning{
		"plan_issue": {
			{Category: swarm.LearningPlanIssue},
			{Category: swarm.LearningPlanIssue},
			{Category: swarm.LearningPlanIssue},
		},
	}

	actions := DetectPatterns(grouped)

	found := false
	for _, a := range actions {
		if contains(a, "swarm-code-plan") {
			found = true
		}
	}

	if !found {
		t.Errorf("expected action for plan issues, got: %v", actions)
	}
}

func TestDetectPatternsPostMortems(t *testing.T) {
	t.Parallel()

	grouped := map[string][]sqlc.SwarmLearning{
		"post_mortem": {
			{Category: swarm.LearningPostMortem},
		},
	}

	actions := DetectPatterns(grouped)

	found := false
	for _, a := range actions {
		if contains(a, "SwarmConfig") {
			found = true
		}
	}

	if !found {
		t.Errorf("expected action for post-mortems, got: %v", actions)
	}
}

func TestDetectPatternsNoPatterns(t *testing.T) {
	t.Parallel()

	grouped := map[string][]sqlc.SwarmLearning{
		"pattern": {
			{Category: swarm.LearningPattern},
		},
	}

	actions := DetectPatterns(grouped)
	if len(actions) != 0 {
		t.Errorf("expected no actions for single pattern, got: %v", actions)
	}
}

func TestGenerateDigestEmpty(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	baseDir := t.TempDir()

	// No learnings → should return nil (nothing to digest).
	err := GenerateDigest(t.Context(), database, baseDir)
	if err != nil {
		t.Fatalf("GenerateDigest with no data: %v", err)
	}
}

func TestGenerateDigestWithLearnings(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	baseDir := t.TempDir()

	ctx := t.Context()

	// Seed learnings via the raw SQL helper (same as existing tests).
	_ = swarm.CapturePlanIssue(ctx, database.SQLDB(), "", "", "TKT-D1", "plan issue 1")
	_ = swarm.CapturePlanIssue(ctx, database.SQLDB(), "", "", "TKT-D2", "plan issue 2")
	_ = swarm.CaptureCodeBug(ctx, database.SQLDB(), "", "", "TKT-D3", "code bug 1")

	err := GenerateDigest(ctx, database, baseDir)
	if err != nil {
		t.Fatalf("GenerateDigest: %v", err)
	}

	// Verify digest was stored in DB.
	digest, getErr := database.GetLatestSwarmLearningDigest(ctx)
	if getErr != nil {
		t.Fatalf("GetLatestSwarmLearningDigest: %v", getErr)
	}

	if digest.LearningCount != 3 {
		t.Errorf("learning_count = %d, want 3", digest.LearningCount)
	}

	if digest.DigestType != digestType {
		t.Errorf("digest_type = %q, want %q", digest.DigestType, digestType)
	}

	if !digest.DocPath.Valid {
		t.Error("doc_path should be set")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || s != "" && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
