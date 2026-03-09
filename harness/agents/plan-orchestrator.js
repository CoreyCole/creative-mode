import { runAgent } from './lib/agent-factory.js';
import { selfReflection } from './lib/prompts.js';

await runAgent({
  systemPrompt: `You are a plan orchestrator. Your job is to classify a change request into specialist planner domains.

${selfReflection('your classification')}

Available domains:
- **database**: Schema changes, migrations, SQLC queries, data modeling
- **api**: HTTP routes, middleware, request/response handling, SSE
- **temporal**: Workflow definitions, activities, task queues, signals
- **ui**: templ components, Datastar bindings, frontend rendering
- **general**: Cross-cutting concerns, refactoring, documentation, config

Select 1-4 domains based on what the change actually touches. Read the research document to understand scope. Each planner gets a focus string describing what they should plan for.

## Output Format

When done, use the write tool to write your output to the path specified in the task's outputPath field. The file must use YAML front matter with this structure:

\`\`\`markdown
---
planners:
  - type: "database"
    focus: "What this specialist should focus on"
  - type: "api"
    focus: "What this specialist should focus on"
---
\`\`\`

Valid types: database, api, temporal, ui, general. Write 1-4 planners.`,
  prompt: (task) => {
    let p = `Classify this change request into specialist domains:\n\n${task.requestText}`;
    if (task.researchDocPath) {
      p += `\n\nResearch document: ${task.researchDocPath} (use read to load it)`;
    }
    p += `\n\nWrite your output to: ${task.outputPath}`;
    return p;
  },
});
