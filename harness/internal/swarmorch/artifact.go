package swarmorch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// yamlMultiKeyRe matches a bare YAML key (word + colon) that appears mid-line
// after a quoted value. This catches the LLM pattern of writing multiple
// mapping keys on one line, e.g.:
//
//   - question: "text" rationale: "text" suggestedFiles:
var yamlMultiKeyRe = regexp.MustCompile(
	`(")\s+(` +
		`question|rationale|suggestedFiles|` +
		`type|focus|` +
		`domain|filesAffected|automatedVerification|manualVerification|risks|dependencies|` +
		`confidence|filesReferenced|` +
		`summary|phaseOrder|document|` +
		`comment|followups|title|description|relation` +
		`):[ ]?`,
)

// formatArtifact normalises an agent output file before parsing:
//  1. Splits multi-key YAML lines (LLM writes keys on one line).
//  2. Runs mdformat to normalise YAML front matter + markdown.
func formatArtifact(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Fix multi-key YAML lines by inserting a newline + correct indent.
	fixed := fixYAMLMultiKeys(string(data))
	if fixed != string(data) {
		if wErr := os.WriteFile(path, []byte(fixed), 0o600); wErr != nil {
			return fmt.Errorf("write fixed YAML %s: %w", path, wErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), mdformatTimeout*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mdformat", "--wrap", "no", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mdformat %s: %w (output: %s)", path, err, string(out))
	}
	return nil
}

// fixYAMLMultiKeys splits lines where multiple YAML mapping keys appear on
// one line (a common LLM output defect). For example:
//
//   - question: "text" rationale: "text"
//
// becomes:
//
//   - question: "text"
//     rationale: "text"
func fixYAMLMultiKeys(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		// Determine the line's leading whitespace + list marker indent.
		trimmed := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(trimmed)]
		if strings.HasPrefix(trimmed, "- ") {
			// List item — child keys should be indented 2 more than "- "
			indent += "  "
		}
		// Repeatedly split at mid-line keys.
		result := yamlMultiKeyRe.ReplaceAllStringFunc(line, func(match string) string {
			// match is e.g. `" rationale: ` — keep the closing quote,
			// add newline + indent, then the key.
			parts := yamlMultiKeyRe.FindStringSubmatch(match)
			return parts[1] + "\n" + indent + parts[2] + ": "
		})
		out = append(out, result)
	}
	return strings.Join(out, "\n")
}

// Validation thresholds for agent output artifacts.
const (
	maxQuestions      = 5
	minFindingsLen    = 50
	minResearchDocLen = 200
	minSummaryLen     = 50
	maxPlanners       = 4
	minPlanSectionLen = 100
	minPlanDocLen     = 300

	mdformatTimeout = 10 // seconds
)

// parseArtifact splits a file into YAML front matter and markdown body.
// Files without front matter delimiters are treated as pure YAML.
func parseArtifact(data []byte) (map[string]any, string, error) {
	content := strings.TrimSpace(string(data))

	// Check for YAML front matter delimiters
	if strings.HasPrefix(content, "---") {
		// Find closing delimiter
		rest := content[3:]
		rest = strings.TrimLeft(rest, " \t")
		if rest != "" && rest[0] == '\n' {
			rest = rest[1:]
		} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
			rest = rest[2:]
		}

		endIdx := strings.Index(rest, "\n---")
		if endIdx == -1 {
			// No closing delimiter — treat entire content as YAML
			var fm map[string]any
			if err := yaml.Unmarshal(data, &fm); err != nil {
				return nil, "", fmt.Errorf("parse YAML: %w", err)
			}
			return fm, "", nil
		}

		yamlPart := rest[:endIdx]
		body := rest[endIdx+4:] // skip "\n---"
		body = strings.TrimLeft(body, " \t\r\n")

		var fm map[string]any
		if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
			return nil, "", fmt.Errorf("parse YAML front matter: %w", err)
		}
		return fm, body, nil
	}

	// No front matter — pure YAML
	var fm map[string]any
	if err := yaml.Unmarshal(data, &fm); err != nil {
		return nil, "", fmt.Errorf("parse YAML: %w", err)
	}
	return fm, "", nil
}

// unmarshalArtifact reads an output file, parses YAML front matter + body,
// and unmarshals into type T. If bodyField is non-empty, the markdown body
// is injected into that field before unmarshaling.
func unmarshalArtifact[T any](path, bodyField string) (T, error) {
	var zero T

	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read artifact %s: %w", path, err)
	}

	fm, body, err := parseArtifact(data)
	if err != nil {
		return zero, fmt.Errorf("parse artifact %s: %w", path, err)
	}

	// Inject body into front matter map under bodyField
	if bodyField != "" && body != "" {
		if fm == nil {
			fm = make(map[string]any)
		}
		fm[bodyField] = body
	}

	// YAML map → JSON → T (to reuse json struct tags)
	jsonBytes, err := json.Marshal(fm)
	if err != nil {
		return zero, fmt.Errorf("marshal artifact to JSON: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.DisallowUnknownFields()

	var result T
	if err := dec.Decode(&result); err != nil {
		// Retry without strict mode — unknown fields from LLM are common
		if err2 := json.Unmarshal(jsonBytes, &result); err2 != nil {
			return zero, fmt.Errorf("unmarshal artifact %s: %w", path, err2)
		}
	}

	return result, nil
}

// --- Validation functions (ported from JS) ---

func validateResearchQuestions(a QuestionArtifact) error {
	if len(a.Questions) == 0 {
		return errors.New("must produce at least one question")
	}
	if len(a.Questions) > maxQuestions {
		return fmt.Errorf("maximum %d questions, got %d", maxQuestions, len(a.Questions))
	}
	for i, q := range a.Questions {
		if q.Question == "" {
			return fmt.Errorf("question[%d] missing question field", i)
		}
		if q.Rationale == "" {
			return fmt.Errorf("question[%d] missing rationale", i)
		}
	}
	return nil
}

func validateResearchFinding(a ResearchFinding) error {
	if a.Question == "" {
		return errors.New("must include the original question")
	}
	if len(a.Findings) < minFindingsLen {
		return fmt.Errorf(
			"findings must be substantive (%d+ chars), got %d",
			minFindingsLen, len(a.Findings),
		)
	}
	if len(a.FilesReferenced) == 0 {
		return errors.New("must reference at least one file")
	}
	validConf := map[string]bool{"high": true, "medium": true, "low": true}
	if !validConf[a.Confidence] {
		return fmt.Errorf("confidence must be high, medium, or low; got %q", a.Confidence)
	}
	return nil
}

func validateSynthesizeResult(a SynthesizeResult) error {
	if len(a.Document) < minResearchDocLen {
		return fmt.Errorf(
			"document must be substantive (%d+ chars), got %d",
			minResearchDocLen, len(a.Document),
		)
	}
	if len(a.Summary) < minSummaryLen {
		return fmt.Errorf(
			"summary must be at least %d chars, got %d",
			minSummaryLen,
			len(a.Summary),
		)
	}
	return nil
}

func validateClassifyResult(a ClassifyResult) error {
	if len(a.Planners) == 0 {
		return errors.New("must select at least one planner")
	}
	if len(a.Planners) > maxPlanners {
		return fmt.Errorf("maximum %d planners, got %d", maxPlanners, len(a.Planners))
	}
	validTypes := map[string]bool{
		"database": true, "api": true, "temporal": true, "ui": true, "general": true,
	}
	for i, p := range a.Planners {
		if !validTypes[p.Type] {
			return fmt.Errorf(
				"planner[%d] invalid type %q; must be database, api, temporal, ui, or general",
				i,
				p.Type,
			)
		}
		if p.Focus == "" {
			return fmt.Errorf("planner[%d] missing focus", i)
		}
	}
	return nil
}

func validatePlannerOutput(a PlannerOutput) error {
	if a.Domain == "" {
		return errors.New("must specify domain")
	}
	if len(a.PlanSection) < minPlanSectionLen {
		return fmt.Errorf(
			"plan section must be substantive (%d+ chars), got %d",
			minPlanSectionLen, len(a.PlanSection),
		)
	}
	if len(a.FilesAffected) == 0 {
		return errors.New("must list affected files")
	}
	if len(a.AutomatedVerification) == 0 && len(a.ManualVerification) == 0 {
		return errors.New(
			"must include at least one verification check (automated or manual)",
		)
	}
	return nil
}

func validateLinearContextOutput(a LinearContextOutput) error {
	if a.Comment == "" {
		return errors.New("must produce a comment")
	}
	validRelations := map[string]bool{"blocked-by": true, "relates-to": true}
	for i, f := range a.Followups {
		if f.Title == "" {
			return fmt.Errorf("followup[%d] missing title", i)
		}
		if !validRelations[f.Relation] {
			return fmt.Errorf("followup[%d] invalid relation %q", i, f.Relation)
		}
	}
	return nil
}

func validatePlanSynthesizeResult(a PlanSynthesizeResult) error {
	if len(a.Document) < minPlanDocLen {
		return fmt.Errorf(
			"plan document must be substantive (%d+ chars), got %d",
			minPlanDocLen, len(a.Document),
		)
	}
	if len(a.Summary) < minSummaryLen {
		return fmt.Errorf(
			"summary must be at least %d chars, got %d",
			minSummaryLen,
			len(a.Summary),
		)
	}
	if len(a.PhaseOrder) == 0 {
		return errors.New("must define phase ordering")
	}
	return nil
}
