import { Type } from '@mariozechner/pi-ai';
import { randomUUID } from 'crypto';
import { readLine, sendQuestion, sendResult } from './protocol.js';

export function createAskOrchestratorTool() {
  return {
    name: 'ask_orchestrator',
    label: 'Ask Orchestrator',
    description: 'Ask the orchestrator when you need context you cannot find with your file tools. Use for architectural questions, cross-cutting concerns, or when stuck.',
    parameters: Type.Object({
      question: Type.String({ description: 'What you need to know' })
    }),
    execute: async (_id, { question }) => {
      const qid = randomUUID();
      sendQuestion(qid, question);
      const answer = await readLine();
      return { content: [{ type: 'text', text: answer.text }], details: {} };
    }
  };
}

export function createSubmitArtifactTool(schema, validate) {
  return {
    name: 'submit_artifact',
    label: 'Submit Artifact',
    description: 'Submit your final output when your work is complete. The artifact will be validated before acceptance.',
    parameters: schema,
    execute: async (_id, artifact) => {
      const errors = validate ? validate(artifact) : [];
      if (errors.length > 0) {
        return {
          content: [{ type: 'text', text: `Validation errors:\n${errors.join('\n')}\nFix these issues and call submit_artifact again.` }],
          details: { valid: false }
        };
      }
      sendResult(artifact);
      return {
        content: [{ type: 'text', text: 'Artifact submitted successfully.' }],
        details: { valid: true }
      };
    }
  };
}
