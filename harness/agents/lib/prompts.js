/**
 * Shared prompt fragments for agent system prompts.
 */

/**
 * Self-reflection preamble — instructs the agent to leverage injected
 * project context and load relevant skills before starting work.
 * @param {string} verb - The action verb, e.g. "your investigation", "your planning"
 */
export function selfReflection(verb) {
  return `Before starting work:
1. Review the project documentation and skills manifest already provided in your context
2. Use read to load the full content of any skills relevant to your task (paths listed in the manifest)
3. Use search_context if you need to find specific source files beyond what's in the documentation
4. Think deeply about underlying patterns, connections, and architectural implications before proceeding
5. Then proceed with ${verb}`;
}
