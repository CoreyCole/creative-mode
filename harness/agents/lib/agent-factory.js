import { Agent } from '@mariozechner/pi-agent-core';
import { getModel } from '@mariozechner/pi-ai';
import { createReadOnlyTools } from '@mariozechner/pi-coding-agent';
import { createAskOrchestratorTool, createSubmitArtifactTool } from './orchestrator-tools.js';
import { createSearchContextTool } from './search-context.js';
import { initProtocol, readLine, sendEvent, startHeartbeat } from './protocol.js';
import { getCodexAccessToken } from './codex-auth.js';
import { join } from 'path';

export async function runAgent({
  artifactSchema,
  validate,
  systemPrompt,
  prompt,
  repoRoot,
  skillsDir,
  withFileTools = true,
  withSearchContext = true,
}) {
  initProtocol();

  // Read start message from Go orchestrator
  const startMsg = await readLine();
  const task = startMsg.task;
  const finalSystemPrompt = startMsg.systemPrompt || systemPrompt;
  const cwd = repoRoot || task.repoRoot;
  const finalSkillsDir = skillsDir || join(cwd, 'harness', 'agents', 'skills');

  const model = getModel('openai-codex', 'gpt-5.3-codex');

  const tools = [];
  if (withFileTools) {
    tools.push(...createReadOnlyTools(cwd));
  }
  if (withSearchContext) {
    tools.push(createSearchContextTool(finalSkillsDir, withFileTools ? [cwd] : []));
  }
  tools.push(createAskOrchestratorTool());
  tools.push(createSubmitArtifactTool(artifactSchema, validate));

  const agent = new Agent({
    getApiKey: () => getCodexAccessToken(),
  });
  agent.setModel(model);
  agent.setSystemPrompt(finalSystemPrompt);
  agent.setTools(tools);

  // Stream tool events to Go for span creation + SSE dashboard
  agent.subscribe(event => {
    if (event.type === 'tool_execution_start') {
      sendEvent('tool_execution_start', event.toolName, event.args, event.toolCallId);
    } else if (event.type === 'tool_execution_end') {
      sendEvent('tool_execution_end', event.toolName, event.result, event.toolCallId);
    }
  });

  // Start periodic heartbeat for Temporal liveness detection
  startHeartbeat();

  // Build user prompt from task data
  const userPrompt = typeof prompt === 'function' ? prompt(task) : prompt;
  await agent.prompt(userPrompt);
}
