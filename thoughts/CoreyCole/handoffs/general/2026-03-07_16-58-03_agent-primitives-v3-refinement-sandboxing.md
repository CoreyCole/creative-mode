---
date: 2026-03-08T00:58:03-08:00
researcher: CoreyCole
git_commit: b373a024604b86471ace03da8bcaa65a0f1f7321
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives v3 — Plan Refinement: Sandboxing, Model Provider, Open Questions"
tags: [implementation, strategy, swarm, temporal, pi-mono, agent-primitives, sandboxing, bubblewrap]
status: in_progress
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives v3 — Plan Refinement (Sandboxing & Resolved Questions)

## Task(s)

**Continuing to refine the v3 agent primitives plan before implementation.** Status: **plan updated with major resolutions, 4 open questions remain**.

- Resumed from previous handoff (`2026-03-07_16-22-48_agent-primitives-v3-conversational-agents.md`)
- Resolved 9 of 13 open questions from the v3 plan
- Added Sandboxing Strategy section with verified bwrap results
- Fixed critical model provider bug (`openai-codex` → `openai`)
- Added `bubblewrap` and `temporal-cli` to `flake.nix` (both verified installed)
- **No code written** — still in plan refinement phase

## Critical References

1. **v3 Plan (authoritative, updated this session)**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
2. **Previous handoff (full context chain)**: `thoughts/CoreyCole/handoffs/general/2026-03-07_16-22-48_agent-primitives-v3-conversational-agents.md`

## Recent Changes

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — major updates:
  - Fixed "Corrections From v2" section: `getModel('openai', 'gpt-5.3-codex')` replaces `getModel('openai-codex', ...)`
  - Fixed `agent-factory.js` code sample: same model provider fix
  - Added `createReadOnlyTools` path traversal warning
  - Added entire "Sandboxing Strategy" section (DirectRunner, BwrapRunner, production path)
  - Replaced "Open Questions" section with "Resolved Questions" (9 answers) + "Remaining Open Questions" (4)
  - Updated Implementation Phases: added `temporal-cli` and `OPENAI_API_KEY` to Phase 1, added Phase 6 (Hardening)
- `flake.nix` — added `bubblewrap` and `temporal-cli` to packages

## Learnings

### Critical: Model Provider Bug
- `getModel('openai-codex', 'gpt-5.3-codex')` does NOT use `OPENAI_API_KEY`. It routes through ChatGPT OAuth at `chatgpt.com/backend-api`, requiring interactive browser login via PKCE flow. Completely unsuitable for subprocess agents.
- **Fix**: `getModel('openai', 'gpt-5.3-codex')` — same model, routes through standard `api.openai.com/v1`, reads `OPENAI_API_KEY` from env.
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-ai/dist/env-api-keys.js:73-89` (env var map) and `/opt/openclaw/node_modules/@mariozechner/pi-ai/dist/providers/openai-codex-responses.js` (OAuth flow).

### createReadOnlyTools Has No Path Traversal Protection
- `createReadOnlyTools(cwd)` only uses `cwd` as a base for resolving relative paths. Absolute paths pass through unchanged, `../../` traversal works.
- Source: `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/tools/path-utils.js:47-80`
- Truncation limits: Read = 2000 lines / 50KB, Grep = 100 matches / 500 chars per line / 50KB, Find = 1000 results / 50KB, Ls = 500 entries / 50KB.

### Bubblewrap (bwrap) Works But Needs Sudo on Ubuntu 24.04
- VPS has `apparmor_restrict_unprivileged_userns=1`, blocking unprivileged user namespace creation
- `sudo bwrap` works perfectly: tested bidirectional JSONL stdin/stdout, read-only repo, `.env` hidden via `--ro-bind /dev/null`, database hidden via `--tmpfs`
- Startup overhead: ~5-10ms, memory overhead: zero
- Options for unprivileged: sudoers rule, AppArmor profile for bwrap, or disable sysctl
- **Decision**: Use `DirectRunner` (plain exec) for v1. bwrap is Phase 6 hardening.

### Temporal Cloud Architecture
- Temporal Cloud is managed **server** only. Workers still run on user infrastructure.
- Moving to Temporal Cloud only changes connection config (mTLS certs, namespace endpoint). Activities, workflows, agent scripts are identical.
- If workers run in containers (ECS/K8s), the container IS the sandbox — no bwrap needed.
- **Decision**: Design `AgentRunner` interface so sandboxing is pluggable, not coupled to core.

### AgentTool / AgentEvent Types (Verified)
- `AgentTool`: `{ name, description, parameters (TypeBox), label, execute(toolCallId, params, signal?, onUpdate?) => Promise<{content, details}> }`
- `AgentEvent` discriminated union: `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_execution_start`, `tool_execution_update`, `tool_execution_end`
- For SSE dashboard we care about: `tool_execution_start` (toolName, args), `tool_execution_end` (result, isError)

### VPS Resources
- 31GB RAM total, 29GB available — 5 concurrent Node.js agents (~1GB) is no concern
- Node.js: `/home/deploy/.nix-profile/bin/node` v22.22.0 (Nix-managed)
- User namespaces: `max_user_namespaces=127656` (kernel supports it, AppArmor blocks unprivileged)

## Artifacts

- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md` — updated v3 plan (Sandboxing Strategy section, Resolved Questions, fixed model provider)
- `flake.nix` — added bubblewrap + temporal-cli

## Action Items & Next Steps

1. **Resolve remaining 4 open questions** (plan refinement, no code yet):
   - **Exact system prompt text** for each of the 6 agent scripts (research-questions, research-agent, research-synthesizer, plan-orchestrator, specialist-planner, plan-synthesizer)
   - **Skill file content** — write the actual `harness/agents/skills/*.md` files (project-structure, database-conventions, api-conventions, ui-conventions, temporal-conventions, build-system, agent-hierarchy)
   - **Temporal activity heartbeat pattern** — during long `ask_orchestrator` waits, Go must heartbeat Temporal. Should heartbeat on every JSONL message from agent.
   - **Dashboard templ component design** — live tool activity indicator UX for SSE updates

2. **When ready to implement**, follow 6 phases in v3 plan:
   - Phase 1: Foundation (DB, Temporal SDK, agent libs, skills, `OPENAI_API_KEY`)
   - Phase 2: Agent Scripts (6 scripts + standalone testing)
   - Phase 3: Temporal Workflows + Activities
   - Phase 4: HTTP API + SSE
   - Phase 5: Dashboard
   - Phase 6: Hardening (bwrap, container isolation)

## Other Notes

### Key Infrastructure Status
- `temporal-cli` v1.5.1 installed (Nix): `/home/deploy/.nix-profile/bin/temporal`
- `bubblewrap` v0.11.0 installed (Nix): `/home/deploy/.nix-profile/bin/bwrap`
- Temporal dev server running: `temporal-dev.service`, ports 7233/8233, namespace `swarm`
- No `OPENAI_API_KEY` in `.env` yet — needs to be added before Phase 2 testing
- No swarm Go code exists on this branch (`internal/swarm/`, `internal/temporal/` don't exist)
- Old branch reference: `git show feature/agent-swarm:<path>` for state machine, enums, handoffs patterns

### Document Evolution
v1 → v1 review → v2 → v2 handoff → v3 → v3 handoff → **v3 refinement (this session)** → v3 refinement handoff (this document)
