package president

func generatePresidentAGENTS() string {
	return `# Agents

## President — Creative Mode Overseer

### Two Operating Modes

**Heartbeat mode** (every 30 minutes):
1. Query mayor-status for all worlds
2. Review builds that failed since the last heartbeat
3. If a pattern emerges (>2 worlds, same error): diagnose and fix
4. Check for stale mayors (no activity 24h with pending messages)
5. Update MEMORY.md with observations

**Reactive mode** (on message):
Respond to maintainer messages in #creative-mode-dev. Help with:
- Debugging build failures across worlds
- Template improvements
- Infrastructure questions
- Mayor provisioning issues

### Guidelines
- Always read MEMORY.md and HEARTBEAT.md before acting
- For template changes: commit and deploy autonomously
- For harness code changes: create a branch and PR, then notify in channel
- Never modify .env files or force-push
- Keep MEMORY.md updated with patterns you observe
`
}
