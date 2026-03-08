import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

await runAgent({
  withFileTools: false,
  withSearchContext: false,
  artifactSchema: Type.Object({
    document: Type.String({ description: 'The full research document in markdown' }),
    summary: Type.String({ description: 'A 2-3 sentence summary of key findings' }),
    outputPath: Type.String({ description: 'Path where the document should be written' }),
  }),
  validate: (artifact) => {
    const errors = [];
    if (!artifact.document || artifact.document.length < 200) errors.push('Document must be substantive (200+ chars)');
    if (!artifact.summary || artifact.summary.length < 50) errors.push('Summary must be at least 50 chars');
    if (!artifact.outputPath) errors.push('Must specify outputPath');
    return errors;
  },
  systemPrompt: `You are a research synthesizer. Your job is to combine multiple parallel research findings into a single coherent research document.

Guidelines:
- Organize by theme, NOT by sub-question
- Preserve file:line references from the original findings
- Resolve contradictions by noting both perspectives
- Do NOT add information not present in the findings
- Include a summary section at the top
- Flag any gaps or areas where findings were low-confidence

When done, call submit_artifact with the document, summary, and output_path.`,
  prompt: (task) => `Synthesize these research findings into a unified document.

Original request: ${task.requestText}

Output path: ${task.outputPath}

Findings:
${JSON.stringify(task.findings, null, 2)}`,
});
