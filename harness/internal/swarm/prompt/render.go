package prompt

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"text/template"

	"creative-mode/harness/internal/swarm"
)

//go:embed templates/*.md.tmpl
var templateFS embed.FS

// RenderResult holds the rendered prompt content and its content hash.
type RenderResult struct {
	Content     string
	ContentHash string
}

// phaseTemplateFile maps each phase to its template filename.
// Only phases that have prompt templates are included; terminal and
// project phases are not rendered.
var phaseTemplateFile = map[swarm.Phase]string{ //nolint:exhaustive // terminal/project phases have no templates
	swarm.PhaseResearch:   "templates/research.md.tmpl",
	swarm.PhaseCodePlan:   "templates/code_plan.md.tmpl",
	swarm.PhasePlanReview: "templates/plan_review.md.tmpl",
	swarm.PhaseImplement:  "templates/code.md.tmpl",
	swarm.PhaseVerify:     "templates/code_verify.md.tmpl",
	swarm.PhasePR:         "templates/code_pr.md.tmpl",
}

// RenderPrompt renders the complete prompt for a given phase and context.
// It loads the base template + phase-specific template, renders them,
// and computes a SHA-256 content hash of the output.
func RenderPrompt(phase swarm.Phase, ctx PromptContext) (*RenderResult, error) {
	phaseFile, ok := phaseTemplateFile[phase]
	if !ok {
		return nil, fmt.Errorf("no template for phase %q", phase)
	}

	funcMap := template.FuncMap{
		"dec": func(n int64) int64 { return n - 1 },
	}

	tmpl, err := template.New("base.md.tmpl").Funcs(funcMap).ParseFS(
		templateFS,
		"templates/base.md.tmpl",
		phaseFile,
	)
	if err != nil {
		return nil, fmt.Errorf("parse templates for %s: %w", phase, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("render %s: %w", phase, err)
	}

	content := buf.String()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	return &RenderResult{
		Content:     content,
		ContentHash: hash,
	}, nil
}
