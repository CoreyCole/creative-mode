package swarmorch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/swarm"
)

const (
	digestType            = "daily"
	minLearningsForDigest = 1
	repeatedBugThreshold  = 2
)

// GenerateDigest queries learnings since the last digest, groups by category,
// detects patterns, writes a markdown file to thoughts/swarm/digests/, and
// stores a record in swarm_learning_digests.
func GenerateDigest(ctx context.Context, database *db.DB, baseDir string) error {
	// Determine period start: last digest end or 24h ago.
	periodStart := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	latest, latestErr := database.GetLatestSwarmLearningDigest(ctx)
	if latestErr == nil && latest.PeriodEnd != "" {
		periodStart = latest.PeriodEnd
	}

	periodEnd := time.Now().UTC().Format(time.RFC3339)

	// Query learnings since period start.
	learnings, err := database.ListRecentSwarmLearnings(ctx, periodStart)
	if err != nil {
		return fmt.Errorf("list recent learnings: %w", err)
	}

	if len(learnings) < minLearningsForDigest {
		return nil // Nothing to digest.
	}

	// Group by category.
	grouped := groupByCategory(learnings)

	// Detect patterns and generate action items.
	actionItems := DetectPatterns(grouped)

	// Build summary.
	summary := buildDigestSummary(learnings, grouped, periodStart, periodEnd)
	actionItemsStr := strings.Join(actionItems, "\n")

	// Write digest file.
	digestDir := filepath.Join(baseDir, "thoughts", "swarm", "digests")
	if mkErr := os.MkdirAll(digestDir, 0o750); mkErr != nil {
		return fmt.Errorf("create digest dir: %w", mkErr)
	}

	date := time.Now().UTC().Format("2006-01-02")
	digestPath := filepath.Join(digestDir, date+"_digest.md")
	fullContent := buildDigestMarkdown(
		summary,
		actionItems,
		learnings,
		periodStart,
		periodEnd,
	)

	if writeErr := os.WriteFile(digestPath, []byte(fullContent), 0o600); writeErr != nil {
		return fmt.Errorf("write digest file: %w", writeErr)
	}

	// Store in DB.
	digestID := uuid.New().String()[:8]

	return database.CreateSwarmLearningDigest(ctx, sqlc.CreateSwarmLearningDigestParams{
		ID:            digestID,
		DigestType:    digestType,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		LearningCount: int64(len(learnings)),
		Summary:       summary,
		ActionItems: sql.NullString{
			String: actionItemsStr,
			Valid:  actionItemsStr != "",
		},
		DocPath: sql.NullString{String: digestPath, Valid: true},
	})
}

func groupByCategory(learnings []sqlc.SwarmLearning) map[string][]sqlc.SwarmLearning {
	grouped := make(map[string][]sqlc.SwarmLearning)
	for _, l := range learnings {
		cat := string(l.Category)
		grouped[cat] = append(grouped[cat], l)
	}

	return grouped
}

// DetectPatterns applies deterministic rules to identify recurring issues.
func DetectPatterns(grouped map[string][]sqlc.SwarmLearning) []string {
	var actions []string

	// Rule 1: >=2 code bugs with same tag → update swarm-code-verify skill.
	if codeBugs, ok := grouped[string(swarm.LearningCodeBug)]; ok &&
		len(codeBugs) >= repeatedBugThreshold {
		tagCounts := countTags(codeBugs)
		for tag, count := range tagCounts {
			if count >= repeatedBugThreshold {
				actions = append(
					actions,
					fmt.Sprintf(
						"- [ ] Update `swarm-code-verify` SKILL.md: repeated code bug tag `%s` (%d occurrences)",
						tag,
						count,
					),
				)
			}
		}

		if len(tagCounts) == 0 && len(codeBugs) >= repeatedBugThreshold {
			actions = append(
				actions,
				fmt.Sprintf(
					"- [ ] Review `swarm-code-verify` SKILL.md: %d code bugs in period",
					len(codeBugs),
				),
			)
		}
	}

	// Rule 2: >=2 plan issues → update swarm-code-plan skill.
	if planIssues, ok := grouped[string(swarm.LearningPlanIssue)]; ok &&
		len(planIssues) >= repeatedBugThreshold {
		actions = append(
			actions,
			fmt.Sprintf(
				"- [ ] Update `swarm-code-plan` SKILL.md: %d plan issues in period",
				len(planIssues),
			),
		)
	}

	// Rule 3: Any post-mortems → review SwarmConfig.
	if postMortems, ok := grouped[string(swarm.LearningPostMortem)]; ok &&
		len(postMortems) > 0 {
		actions = append(
			actions,
			fmt.Sprintf(
				"- [ ] Review SwarmConfig changes: %d post-mortem(s) recorded",
				len(postMortems),
			),
		)
	}

	return actions
}

func countTags(learnings []sqlc.SwarmLearning) map[string]int {
	counts := make(map[string]int)
	for _, l := range learnings {
		if !l.Tags.Valid || l.Tags.String == "" {
			continue
		}

		for _, tag := range strings.Split(l.Tags.String, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				counts[tag]++
			}
		}
	}

	return counts
}

func buildDigestSummary(
	learnings []sqlc.SwarmLearning,
	grouped map[string][]sqlc.SwarmLearning,
	periodStart, periodEnd string,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Period: %s to %s\n", periodStart, periodEnd)
	fmt.Fprintf(&b, "Total learnings: %d\n", len(learnings))

	categories := []string{
		string(swarm.LearningPostMortem),
		string(swarm.LearningCodeBug),
		string(swarm.LearningPlanIssue),
		string(swarm.LearningConvention),
		string(swarm.LearningPattern),
	}
	for _, cat := range categories {
		if items, ok := grouped[cat]; ok {
			fmt.Fprintf(&b, "%s: %d\n", cat, len(items))
		}
	}

	return b.String()
}

func buildDigestMarkdown(
	summary string,
	actionItems []string,
	learnings []sqlc.SwarmLearning,
	periodStart, periodEnd string,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Swarm Learning Digest\n\n")
	fmt.Fprintf(&b, "**Period**: %s to %s\n\n", periodStart, periodEnd)
	fmt.Fprintf(&b, "## Summary\n\n%s\n", summary)

	if len(actionItems) > 0 {
		b.WriteString("## Action Items\n\n")
		for _, item := range actionItems {
			b.WriteString(item)
			b.WriteString("\n")
		}

		b.WriteString("\n")
	}

	b.WriteString("## Learnings\n\n")

	grouped := groupByCategory(learnings)
	categories := []string{
		string(swarm.LearningPostMortem),
		string(swarm.LearningCodeBug),
		string(swarm.LearningPlanIssue),
		string(swarm.LearningConvention),
		string(swarm.LearningPattern),
	}

	for _, cat := range categories {
		items, ok := grouped[cat]
		if !ok {
			continue
		}

		fmt.Fprintf(&b, "### %s (%d)\n\n", cat, len(items))

		for _, l := range items {
			fmt.Fprintf(&b, "- **[%s]** %s: %s\n", l.Severity, l.Title, l.Content)
		}

		b.WriteString("\n")
	}

	return b.String()
}
