import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

const SELF_REFLECTION = `Before starting work:
1. Reflect on what aspects of this codebase you need to understand for this task
2. Call search_context with your requirements to discover relevant skills and patterns
3. Use read to load the full content of any matched skills
4. Then proceed with your planning`;

await runAgent({
  artifactSchema: Type.Object({
    domain: Type.String({ description: 'The specialist domain this plan covers' }),
    planSection: Type.String({ description: 'Detailed implementation plan in markdown' }),
    filesAffected: Type.Array(Type.String(), { description: 'Files that will be created or modified' }),
    verificationChecks: Type.Array(Type.String(), { description: 'Commands or checks to verify the implementation' }),
    risks: Type.Array(Type.String(), { description: 'Known risks or concerns' }),
    dependencies: Type.Array(Type.String(), { description: 'Dependencies on other domains or external systems' }),
  }),
  validate: (artifact) => {
    const errors = [];
    if (!artifact.domain) errors.push('Must specify domain');
    if (!artifact.planSection || artifact.planSection.length < 100) errors.push('Plan section must be substantive (100+ chars)');
    if (!artifact.filesAffected || artifact.filesAffected.length === 0) errors.push('Must list affected files');
    if (!artifact.verificationChecks || artifact.verificationChecks.length === 0) errors.push('Must include verification checks');
    return errors;
  },
  systemPrompt: `You are a specialist planner. Your job is to produce a detailed implementation plan for your assigned domain.

${SELF_REFLECTION}

Guidelines:
- Load the skill file for your domain (e.g., search for "database conventions" or "api conventions")
- Read actual source code to verify your assumptions — do not guess file structures
- Include specific file paths, function names, and line references
- List verification checks (commands to run, tests to pass)
- Note dependencies on other domains
- Flag risks that could affect the plan

When done, call submit_artifact with your plan section and metadata.`,
  prompt: (task) => `Create a detailed implementation plan for the **${task.domain}** domain.

Focus: ${task.focus}

Change request: ${task.requestText}

Research context:
${task.researchDoc}`,
});
