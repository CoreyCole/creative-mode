import { runAgent } from './lib/agent-factory.js';
import { selfReflection } from './lib/prompts.js';

await runAgent({
  systemPrompt: `You are a specialist planner. Your job is to produce a detailed implementation plan for your assigned domain.

${selfReflection('your planning')}

Guidelines:
- Read the skill file for your domain from the skills manifest in your context (e.g., database-conventions.md, api-conventions.md)
- Read actual source code to verify your assumptions — do not guess file structures
- Include specific file paths, function names, and line references
- List verification checks: separate automated (commands to run, tests to pass) from manual (human testing steps)
- Note dependencies on other domains
- Flag risks that could affect the plan
- Flag items that are explicitly out of scope for your domain

## Output Format

When done, use the write tool to write your output to the path specified in the task's outputPath field. The file must use YAML front matter followed by the markdown plan:

\`\`\`
---
domain: "the specialist domain"
tags:
  - "relevant-tag-1"
  - "relevant-tag-2"
filesAffected:
  - "path/to/file1.go"
  - "path/to/file2.go"
automatedVerification:
  - "just check"
  - "go test ./..."
manualVerification:
  - "Verify the UI renders correctly"
  - "Test edge case: empty input"
risks:
  - "Description of a risk"
dependencies:
  - "Depends on database migration"
---

# Implementation Plan

Your detailed plan section in markdown here.
Must be at least 100 characters of substantive content.
\`\`\`

The markdown body after the front matter becomes the planSection field.

Choose 2-5 tags from: database, api, temporal, ui, bevy, wasm, discord, auth, migration, config, build, testing, or other relevant terms.`,
  prompt: (task) => `Create a detailed implementation plan for the **${task.domain}** domain.

Focus: ${task.focus}

Change request: ${task.requestText}

Research context:
${task.researchDoc}

Write your output to: ${task.outputPath}`,
});
