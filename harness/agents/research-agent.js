import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

const SELF_REFLECTION = `Before starting work:
1. Reflect on what aspects of this codebase you need to understand for this task
2. Call search_context with your requirements to discover relevant skills and patterns
3. Use read to load the full content of any matched skills
4. Then proceed with your investigation`;

await runAgent({
  artifactSchema: Type.Object({
    question: Type.String({ description: 'The question that was investigated' }),
    findings: Type.String({ description: 'Compressed findings with file:line references' }),
    filesReferenced: Type.Array(Type.String(), { description: 'All files read during investigation' }),
    confidence: Type.Union([
      Type.Literal('high'),
      Type.Literal('medium'),
      Type.Literal('low'),
    ], { description: 'Confidence in the completeness of findings' }),
  }),
  validate: (artifact) => {
    const errors = [];
    if (!artifact.question) errors.push('Must include the original question');
    if (!artifact.findings || artifact.findings.length < 50) errors.push('Findings must be substantive (50+ chars)');
    if (!artifact.filesReferenced || artifact.filesReferenced.length === 0) errors.push('Must reference at least one file');
    if (!['high', 'medium', 'low'].includes(artifact.confidence)) errors.push('Confidence must be high, medium, or low');
    return errors;
  },
  systemPrompt: `You are a codebase researcher. Your job is to investigate a single focused question by reading source code and producing compressed findings.

${SELF_REFLECTION}

Guidelines:
- Use file tools (read, grep, find, ls) to explore the codebase
- Produce compressed findings with file:line references (e.g., "db.go:42 — migrations registered in migrationFiles slice")
- Do NOT include raw file contents — summarize what you find
- Track all files you read in filesReferenced
- Use ask_orchestrator for cross-cutting context you can't find in code
- Set confidence based on how completely you answered the question

When done, call submit_artifact with your findings.`,
  prompt: (task) => `Investigate this question:

${task.question}`,
});
