import { runAgent } from './lib/agent-factory.js';
import { Type } from '@mariozechner/pi-ai';

const SELF_REFLECTION = `Before starting work:
1. Reflect on what aspects of this codebase you need to understand for this task
2. Call search_context with your requirements to discover relevant skills and patterns
3. Use read to load the full content of any matched skills
4. Then proceed with your classification`;

await runAgent({
  artifactSchema: Type.Object({
    planners: Type.Array(Type.Object({
      type: Type.String({ description: 'Specialist domain: database, api, temporal, ui, or general' }),
      focus: Type.String({ description: 'What this specialist should focus on' }),
    })),
  }),
  validate: (artifact) => {
    const errors = [];
    const validTypes = ['database', 'api', 'temporal', 'ui', 'general'];
    if (!artifact.planners || artifact.planners.length === 0) errors.push('Must select at least one planner');
    if (artifact.planners && artifact.planners.length > 4) errors.push('Maximum 4 planners');
    for (const p of artifact.planners || []) {
      if (!validTypes.includes(p.type)) errors.push(`Invalid planner type: ${p.type}. Must be one of: ${validTypes.join(', ')}`);
      if (!p.focus) errors.push('Each planner must have a focus description');
    }
    return errors;
  },
  systemPrompt: `You are a plan orchestrator. Your job is to classify a change request into specialist planner domains.

${SELF_REFLECTION}

Available domains:
- **database**: Schema changes, migrations, SQLC queries, data modeling
- **api**: HTTP routes, middleware, request/response handling, SSE
- **temporal**: Workflow definitions, activities, task queues, signals
- **ui**: templ components, Datastar bindings, frontend rendering
- **general**: Cross-cutting concerns, refactoring, documentation, config

Select 1-4 domains based on what the change actually touches. Read the research document to understand scope. Each planner gets a focus string describing what they should plan for.

When done, call submit_artifact with your planners array.`,
  prompt: (task) => {
    let p = `Classify this change request into specialist domains:\n\n${task.requestText}`;
    if (task.researchDocPath) {
      p += `\n\nResearch document: ${task.researchDocPath} (use read to load it)`;
    }
    return p;
  },
});
