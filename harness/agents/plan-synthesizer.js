import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

await runAgent({
  withFileTools: false,
  withSearchContext: false,
  artifactSchema: Type.Object({
    document: Type.String({ description: 'The full implementation plan in markdown' }),
    summary: Type.String({ description: 'A 2-3 sentence summary of the plan' }),
    phaseOrder: Type.Array(Type.String(), { description: 'Ordered list of implementation phases' }),
    outputPath: Type.String({ description: 'Path where the plan should be written' }),
  }),
  validate: (artifact) => {
    const errors = [];
    if (!artifact.document || artifact.document.length < 300) errors.push('Plan document must be substantive (300+ chars)');
    if (!artifact.summary || artifact.summary.length < 50) errors.push('Summary must be at least 50 chars');
    if (!artifact.phaseOrder || artifact.phaseOrder.length === 0) errors.push('Must define phase ordering');
    if (!artifact.outputPath) errors.push('Must specify outputPath');
    return errors;
  },
  systemPrompt: `You are a plan synthesizer. Your job is to merge specialist domain plans into a unified implementation plan.

Guidelines:
- Resolve cross-domain dependencies (e.g., database migration must precede API routes)
- Order phases based on the dependency graph
- Preserve verification checks from each specialist
- Combine risks and flag cross-domain concerns
- Include a summary with phase ordering at the top
- Each phase should be independently committable

When done, call submit_artifact with the document, summary, phase_order, and output_path.`,
  prompt: (task) => `Merge these specialist plans into a unified implementation plan.

Original request: ${task.requestText}

Research summary: ${task.researchDocSummary}

Output path: ${task.outputPath}

Specialist plans:
${JSON.stringify(task.plannerOutputs, null, 2)}`,
});
