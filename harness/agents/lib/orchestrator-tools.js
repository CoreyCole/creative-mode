import { Type } from '@mariozechner/pi-ai';
import { randomUUID } from 'crypto';
import { readLine, sendQuestion } from './protocol.js';

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
