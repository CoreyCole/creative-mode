import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

const SELF_REFLECTION = `Before starting work:
1. Reflect on what aspects of this codebase you need to understand for this task
2. Call search_context with your requirements to discover relevant skills and patterns
3. Use read to load the full content of any matched skills
4. Then proceed with your investigation`;

await runAgent({
  artifactSchema: Type.Object({
    questions: Type.Array(Type.Object({
      question: Type.String({ description: 'A focused, concrete sub-question' }),
      rationale: Type.String({ description: 'Why this question matters for the overall task' }),
      suggestedFiles: Type.Array(Type.String(), { description: 'Files or directories likely to contain the answer' }),
    })),
  }),
  validate: (artifact) => {
    const errors = [];
    if (!artifact.questions || artifact.questions.length === 0) errors.push('Must produce at least one question');
    if (artifact.questions && artifact.questions.length > 5) errors.push('Maximum 5 questions');
    for (const q of artifact.questions || []) {
      if (!q.question) errors.push('Each question must have a question field');
      if (!q.rationale) errors.push('Each question must have a rationale');
    }
    return errors;
  },
  systemPrompt: `You are a research decomposer. Your job is to break down a codebase question into 2-5 focused sub-questions that can be investigated in parallel.

${SELF_REFLECTION}

Each sub-question should:
- Target specific files, patterns, or components
- Be answerable by reading code (not requiring running the system)
- Cover a distinct aspect of the overall question
- Include suggestedFiles paths where the answer likely lives

Use search_context to understand the project structure before decomposing. Do NOT include generic questions — each should be specific enough that a researcher knows exactly where to look.

When done, call submit_artifact with your questions array.`,
  prompt: (task) => `Decompose this research question into ${task.maxQuestions || '2-5'} focused sub-questions:

${task.requestText}`,
});
