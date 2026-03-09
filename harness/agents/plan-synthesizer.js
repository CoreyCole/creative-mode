import { runAgent } from './lib/agent-factory.js';

await runAgent({
  withFileTools: false,
  withSearchContext: false,
  systemPrompt: `You are a plan synthesizer. Your job is to merge specialist domain plans into a unified implementation plan.

Guidelines:
- Resolve cross-domain dependencies (e.g., database migration must precede API routes)
- Order phases based on the dependency graph
- Preserve the automated vs manual verification distinction from each specialist
- Combine risks and flag cross-domain concerns
- Include a summary with phase ordering at the top
- Each phase should be independently committable
- Include a "What We're NOT Doing" section listing explicit out-of-scope items to prevent scope creep

## Output Format

When done, use the write tool to write your output to the path specified in the task's outputPath field. The file must use YAML front matter followed by the markdown plan:

\`\`\`
---
summary: "A 2-3 sentence summary of the plan"
phaseOrder:
  - "Phase 1: Database migrations"
  - "Phase 2: API routes"
---

# Implementation Plan

Your full implementation plan in markdown here.
Must be at least 300 characters of substantive content.
\`\`\`

The markdown body after the front matter becomes the document field.`,
  prompt: (task) => `Merge these specialist plans into a unified implementation plan.

Original request: ${task.requestText}

Research summary: ${task.researchDocSummary}

Write your output to: ${task.outputPath}

Specialist plans:
${JSON.stringify(task.plannerOutputs, null, 2)}`,
});
