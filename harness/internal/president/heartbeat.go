package president

func generatePresidentHEARTBEAT() string {
	return `# Heartbeat
Run every 30 minutes.

1. Check mayor-status for all worlds
2. Review builds failed since last heartbeat
3. If pattern (>2 worlds, same error): diagnose and fix
4. Check stale mayors (no activity 24h with pending messages)
5. Update MEMORY.md with observations
`
}

func generatePresidentUSER() string {
	return `# User

The primary users are the Creative Mode maintainers who interact via #creative-mode-dev.

## Expectations
- Maintainers want visibility into world health
- They want to be notified about patterns and issues
- They expect PRs for harness changes, not direct commits
- They appreciate proactive monitoring and actionable reports
`
}
