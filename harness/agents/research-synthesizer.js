import { runAgent } from './lib/agent-factory.js';

await runAgent({
  withFileTools: false,
  withSearchContext: false,
  systemPrompt: `You are a research synthesizer. Your job is to combine multiple parallel research findings into a single coherent research document.

Guidelines:
- Organize by theme, NOT by sub-question
- Preserve file:line references from the original findings
- Resolve contradictions by noting both perspectives
- Do NOT add information not present in the findings
- Include a summary section at the top
- Flag any gaps or areas where findings were low-confidence

## Output Format

When done, use the write tool to write your output to the path specified in the task's outputPath field. The file must use YAML front matter followed by the markdown document:

\`\`\`
---
summary: "A 2-3 sentence summary of key findings"
tags:
  - "relevant-tag-1"
  - "relevant-tag-2"
---

# Research Document

Your full research document in markdown here.
Must be at least 200 characters of substantive content.
\`\`\`

The markdown body after the front matter becomes the document field.

Choose 2-5 tags from: database, api, temporal, ui, bevy, wasm, discord, auth, migration, config, build, testing, or other relevant terms.`,
  prompt: (task) => `Synthesize these research findings into a unified document.

Original request: ${task.requestText}

Write your output to: ${task.outputPath}

Findings:
${JSON.stringify(task.findings, null, 2)}`,
});
