import { Agent } from '@mariozechner/pi-agent-core';
import { getModel } from '@mariozechner/pi-ai';
import { createReadOnlyTools, createWriteTool } from '@mariozechner/pi-coding-agent';
import { createAskOrchestratorTool } from './orchestrator-tools.js';
import { createSearchContextTool } from './search-context.js';
import { initProtocol, readLine, sendEvent, startHeartbeat, closeProtocol } from './protocol.js';
import { getCodexAccessToken } from './codex-auth.js';
import { join } from 'path';

export async function runAgent({
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
  // Only prepend project context for agents that have file tools.
  // Synthesis agents (withFileTools=false) can't read files, so injecting
  // docs telling them to "use read to load skills" would be confusing.
  const contextPrefix = (startMsg.projectContext && withFileTools)
    ? startMsg.projectContext + '\n\n---\n\n'
    : '';
  const finalSystemPrompt = contextPrefix + (startMsg.systemPrompt || systemPrompt);
  const cwd = repoRoot || task.repoRoot || '/home/deploy/creative-mode';
  const finalSkillsDir = skillsDir || join(cwd, 'harness', 'agents', 'skills');

  const configModel = startMsg.config?.model || 'openai-codex:gpt-5.3-codex';
  const [provider, modelName] = configModel.split(':');
  const model = getModel(provider, modelName);

  const tools = [];
  if (withFileTools) {
    tools.push(...createReadOnlyTools(cwd));
  }
  // Always add write tool — agents write their output to a file
  tools.push(createWriteTool(cwd));
  if (withSearchContext) {
    tools.push(createSearchContextTool(finalSkillsDir, withFileTools ? [cwd] : []));
  }
  tools.push(createAskOrchestratorTool());

  const agent = new Agent({
    getApiKey: () => getCodexAccessToken(),
  });
  agent.setModel(model);
  agent.setSystemPrompt(finalSystemPrompt);
  agent.setTools(tools);

  // Stream agent lifecycle events to Go for span creation + SSE dashboard
  agent.subscribe(event => {
    if (event.type === 'tool_execution_start') {
      sendEvent('tool_execution_start', event.toolName, event.args, event.toolCallId);
    } else if (event.type === 'tool_execution_end') {
      sendEvent('tool_execution_end', event.toolName, event.result, event.toolCallId);
    } else if (event.type === 'message_start' && event.message?.role === 'assistant') {
      sendEvent('inference_start', 'llm', { model: event.message?.model });
    } else if (event.type === 'message_end' && event.message?.role === 'assistant') {
      const msg = event.message;
      sendEvent('inference_end', 'llm', {
        model: msg?.model,
        provider: msg?.provider,
        stopReason: msg?.stopReason,
        usage: msg?.usage,
      });
    }
  });

  // Start periodic heartbeat for Temporal liveness detection
  startHeartbeat();

  // Build user prompt from task data
  const userPrompt = typeof prompt === 'function' ? prompt(task) : prompt;
  await agent.prompt(userPrompt);

  closeProtocol();
}
