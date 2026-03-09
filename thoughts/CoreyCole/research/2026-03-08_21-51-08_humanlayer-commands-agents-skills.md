---
date: 2026-03-08T21:51:08-07:00
researcher: CoreyCole
git_commit: bdea199cec94a1605d2a0de42309d67a14dafdf2
branch: main
repository: humanlayer
topic: "Commands, Agents, and Skills in HumanLayer — Architecture for Building a Compaction System"
tags: [research, codebase, commands, agents, skills, compaction, context-management, sessions]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
---

# Research: Commands, Agents, and Skills in HumanLayer

**Date**: 2026-03-08T21:51:08-07:00
**Researcher**: CoreyCole
**Git Commit**: bdea199cec94a1605d2a0de42309d67a14dafdf2
**Branch**: main
**Repository**: humanlayer

## Research Question
Fully understand the commands, agents, and skills system in the HumanLayer project. We are building our own frequent intentional compaction system.

## Summary

HumanLayer (CodeLayer) is a session management layer around Claude Code with four components: `hld` (Go daemon), `hlyr` (TypeScript CLI/MCP server), `humanlayer-wui` (Tauri+React desktop UI), and `claudecode-go` (Go SDK for launching Claude Code subprocesses). The system has **no built-in compaction or context summarization** — it explicitly blocks `/compact` and instead provides a **fork/continue** mechanism as the primary escape valve when context fills up. Context usage is tracked via token counts and displayed with 60%/90% warning thresholds.

The "commands" system operates at three levels: (1) JSON-RPC methods between hlyr↔hld, (2) HTTP REST endpoints between WUI↔hld, and (3) Claude Code slash commands from `.claude/commands/` markdown files. The "agents" are sub-agent definitions in `.claude/agents/` markdown files with YAML frontmatter specifying tools, model, and persona. There is no runtime "skills" concept in the daemon — the term maps to slash commands and agent definitions distributed via `humanlayer claude init`.

---

## Detailed Findings

### 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        User                                  │
│  ┌───────────────┐    ┌──────────────────────────────────┐  │
│  │  humanlayer    │    │  humanlayer-wui (CodeLayer)      │  │
│  │  CLI (hlyr)    │    │  Tauri + React desktop app       │  │
│  │  TypeScript    │    │  TypeScript/React                │  │
│  └───────┬───────┘    └──────────────┬───────────────────┘  │
│          │ JSON-RPC                  │ HTTP REST             │
│          │ (Unix socket)             │ (localhost:port)      │
│          ▼                           ▼                       │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  hld (Go daemon)                                      │   │
│  │  ├── rpc/     — JSON-RPC server (Unix socket)         │   │
│  │  ├── api/     — HTTP REST server (OpenAPI-generated)  │   │
│  │  ├── session/ — Session manager + process lifecycle   │   │
│  │  ├── store/   — SQLite persistence                    │   │
│  │  ├── bus/     — In-process event bus (pub/sub)        │   │
│  │  ├── mcp/     — MCP HTTP server for approval routing  │   │
│  │  └── daemon/  — Orchestration + startup               │   │
│  └──────────────────────┬───────────────────────────────┘   │
│                         │ subprocess (exec.Cmd)              │
│                         ▼                                    │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Claude Code CLI binary                               │   │
│  │  --print --output-format stream-json --verbose        │   │
│  │  --mcp-config <tempfile>                              │   │
│  │  --permission-prompt-tool mcp__codelayer__request_... │   │
│  │  [--resume <id> [--fork-session]]                     │   │
│  │  -- <query>                                           │   │
│  └──────────────────────┬───────────────────────────────┘   │
│                         │ MCP (stdin/stdout)                 │
│                         ▼                                    │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  hlyr MCP server (humanlayer mcp claude_approvals)    │   │
│  │  Exposes: request_permission tool                     │   │
│  │  Polls hld for approval decisions via JSON-RPC        │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 2. Commands System (Three Layers)

#### Layer 1: JSON-RPC Commands (hlyr ↔ hld)

Transport: Newline-delimited JSON-RPC 2.0 over Unix domain socket (`~/.humanlayer/daemon.sock`).

**Session commands** (`hld/rpc/handlers.go:762-776`):
| Method | Purpose |
|---|---|
| `launchSession` | Create new session (draft or live) |
| `listSessions` | List all sessions |
| `getSessionLeaves` | List leaf sessions with counts |
| `getConversation` | Fetch conversation events |
| `getSessionState` | Get single session state + context tokens |
| `continueSession` | Fork/continue from a session |
| `interruptSession` | Send SIGINT to running session |
| `getSessionSnapshots` | Get file snapshots for a session |
| `updateSessionSettings` | Update session config |
| `updateSessionTitle` | Set session title |
| `getRecentPaths` | Recent working directories |
| `archiveSession` | Archive a session |
| `bulkArchiveSessions` | Bulk archive |

**Approval commands** (`hld/rpc/approval_handlers.go:206-211`):
| Method | Purpose |
|---|---|
| `createApproval` | Create a pending approval |
| `fetchApprovals` | List approvals for session |
| `getApproval` | Get single approval status |
| `sendDecision` | Approve or deny |

**Subscription** (`hld/rpc/subscription_handlers.go:27-29`):
| Method | Purpose |
|---|---|
| `Subscribe` | Real-time event stream (separate connection) |

Handler signature: `func(ctx context.Context, params json.RawMessage) (interface{}, error)`

Registration at `hld/daemon/daemon.go:216-226`:
```go
subscriptionHandlers := rpc.NewSubscriptionHandlers(d.eventBus)
sessionHandlers := rpc.NewSessionHandlers(d.sessions, d.store, d.approvals)
approvalHandlers := rpc.NewApprovalHandlers(d.approvals, d.sessions)
```

#### Layer 2: HTTP REST Endpoints (WUI ↔ hld)

OpenAPI-generated strict interface (`hld/api/handlers/sessions.go`). Key endpoints:
- `POST /api/v1/sessions` → create session
- `GET /api/v1/sessions` → list sessions
- `GET /api/v1/sessions/{id}` → get session
- `POST /api/v1/sessions/{id}/continue` → fork/continue
- `POST /api/v1/sessions/{id}/interrupt` → interrupt
- `GET /api/v1/sessions/{id}/messages` → conversation events
- `GET /api/v1/slash-commands` → discover slash commands
- `POST /api/v1/agents/discover` → discover agents

#### Layer 3: Claude Code Slash Commands (`.claude/commands/`)

Markdown files with YAML frontmatter discovered from two directories:
- **Local**: `{workingDir}/.claude/commands/` (project-level)
- **Global**: `~/.config/claude-code/commands/` (user-level)

Discovery logic at `hld/api/handlers/sessions.go:1601-1760`:
1. Walk both directories for `.md` files
2. Strip `.md` extension, replace `/` with `:` in path, prepend `/`
3. Global commands override local when names collide
4. Optional fuzzy search via `github.com/sahilm/fuzzy`

**28 commands defined** in `.claude/commands/`:

| Category | Commands |
|---|---|
| Planning | `create_plan`, `create_plan_nt`, `create_plan_generic`, `iterate_plan`, `iterate_plan_nt`, `implement_plan`, `validate_plan`, `oneshot_plan` |
| PR/Commit | `commit`, `ci_commit`, `describe_pr`, `describe_pr_nt`, `ci_describe_pr` |
| Research | `research_codebase`, `research_codebase_nt`, `research_codebase_generic` |
| Handoff | `create_handoff`, `resume_handoff` |
| Automation ("Ralph") | `ralph_plan`, `ralph_impl`, `ralph_research`, `oneshot` |
| Tickets | `linear`, `founder_mode` |
| Utility | `debug`, `local_review`, `create_worktree` |

**Variant patterns**:
- `_nt` = "No Thoughts" — strips `thoughts/` directory integration and `humanlayer thoughts sync`
- `_generic` = uses `thoughts/shared/` paths instead of user-specific
- `ci_` = non-interactive, no user confirmation prompts

### 3. Agents System

Six sub-agent definitions in `.claude/agents/`, each a markdown file with YAML frontmatter:

| Agent | Tools | Model | Role |
|---|---|---|---|
| `codebase-locator` | Grep, Glob, LS | sonnet | Find WHERE files are (no reading) |
| `codebase-analyzer` | Read, Grep, Glob, LS | sonnet | Analyze HOW code works (file:line references) |
| `codebase-pattern-finder` | Grep, Glob, Read, LS | sonnet | Find similar implementations/patterns |
| `thoughts-locator` | Grep, Glob, LS | sonnet | Discover docs in `thoughts/` directory |
| `thoughts-analyzer` | Read, Grep, Glob, LS | sonnet | Deep-read `thoughts/` for insights |
| `web-search-researcher` | WebSearch, WebFetch, TodoWrite, Read, Grep, Glob, LS | sonnet | External web research |

**Key design patterns**:
- Agents are invoked by commands as parallel sub-agents (e.g., `research_codebase.md` spawns 3-6 agents concurrently)
- Tool restrictions enforce separation of concerns (locator can't Read, analyzer can)
- All use `sonnet` model for cost efficiency — the parent command uses `opus` for synthesis
- Agent definitions are distributed to new projects via `humanlayer claude init`

### 4. Session Lifecycle & Process Management

**Launch flow** (`hld/session/manager.go:182-545`):
1. Generate UUID for `sessionID` and `runID`
2. Build MCP config: always inject `codelayer` MCP server entry pointing to `hlyr` binary
3. Set `PermissionPromptTool = "mcp__codelayer__request_permission"` (unless overridden)
4. Create `store.Session` in SQLite with status `starting`
5. For drafts: return immediately (no process spawned)
6. For live: `claudecode.Client.Launch(config)` → `exec.Command("claude", args...)`
7. Store process handle in `activeProcesses[sessionID]`
8. Start `monitorSession()` goroutine to stream events

**Event streaming** (`hld/session/manager.go:548-757`):
- Reads from `claudeSession.GetEvents()` channel
- Each `StreamEvent` is stored as a `ConversationEvent` in SQLite
- Token usage extracted from every assistant event: `effective = input + output + cache_read + cache_creation`
- Events: `system`, `assistant`, `user`, `result`
- Content types: `text`, `tool_use`, `tool_result`, `thinking`

**Continue/Fork** (`hld/session/manager.go:1433-1650`):
1. Validate parent session (must be completed, interrupted, running, or failed)
2. If running → interrupt and wait for exit
3. Build new config with `SessionID = parentClaudeSessionID`, `ForkSession = true`
4. CLI args: `--resume <id> --fork-session`
5. New session has `ParentSessionID` linking to the original

**Conversation retrieval** (`hld/store/sqlite.go:2278`):
- `GetSessionConversation` walks the **full parent chain** recursively
- Collects all `claude_session_id` values from root to leaf
- Returns all events ordered by chain position then sequence
- Forked sessions include complete ancestor history

### 5. Context Management & Compaction (Current State)

**What exists:**

1. **Token tracking** (`hld/session/manager.go:963-978`):
   ```go
   effective := usage.InputTokens + usage.OutputTokens +
                usage.CacheReadInputTokens + usage.CacheCreationInputTokens
   ```
   Written to `effective_context_tokens` in SQLite on every assistant event.

2. **Context limit constant** (`hld/rpc/types_constants.go:84-96`):
   - `ModelContextLimits["default"] = 168000` (200k - 32k output reserved)
   - No per-model differentiation — always returns 168000

3. **UI warning thresholds** (`humanlayer-wui/src/.../TokenUsageBadge.tsx:7-10`):
   - 60% → warning color
   - 90% → critical red + alert icon

4. **Fork as escape valve**: When context fills, users click "Fork" to branch from a checkpoint, creating a fresh context window that includes all ancestor conversation history for display but starts a new Claude Code process with `--fork-session`.

**What does NOT exist:**

- No automatic compaction or summarization of conversation history
- No pruning of old messages from context
- No "rolling window" of recent messages
- No LLM-driven summarization before sending to Claude
- The `/compact` slash command is **explicitly blocked** with message: "This command breaks session tracking. Start a fresh session for a clean context."

**Blocked commands** (`humanlayer-wui/src/constants/unsupportedCommands.ts`):
20 commands are blocked including `/compact`, `/clear`, `/help`, `/agents`, `/memory`, `/model`, `/config`, `/doctor`, `/terminal-setup`, `/review`, `/bug`, `/init`, `/logout`, `/login`, `/status`, `/listen`, `/mcp`, `/permissions`, `/cost`.

### 6. Thoughts System (Developer Notes, Not Compaction)

The `thoughts/` system (`hlyr/src/commands/thoughts/`) is a **git-backed developer notes** repository — not conversation compaction. It manages:
- Research documents, plans, handoffs, PR descriptions
- Symlinked directory structure: `thoughts/{user}/` → external git repo
- Auto-sync via git hooks (post-commit triggers `humanlayer thoughts sync`)
- Searchable index rebuilt via hard links in `thoughts/searchable/`

### 7. MCP Protocol (Approval Routing)

The MCP server in `hlyr` exposes exactly one tool: `request_permission`.

Flow:
1. Claude Code calls `mcp__codelayer__request_permission` with `{tool_name, input, tool_use_id}`
2. hlyr MCP server receives via stdin/stdout (MCP protocol over stdio)
3. hlyr calls `daemonClient.createApproval(sessionId, toolName, input, toolUseId)` via JSON-RPC to hld
4. hlyr polls `daemonClient.getApproval(approvalId)` every 1000ms
5. Human approves/denies via WUI or TUI
6. hlyr returns `{behavior: 'allow', updatedInput}` or `{behavior: 'deny', message}` to Claude Code

### 8. Distribution & Setup

`humanlayer claude init` (`hlyr/src/commands/claude/init.ts:48`) copies:
- `.claude/commands/` → project `.claude/commands/`
- `.claude/agents/` → project `.claude/agents/`
- `.claude/settings.json` → project `.claude/settings.json` (with model/thinking config merged)

The bundled assets are packed from the repo root `.claude/` into the npm package during `prepack`.

---

## Code References

### hld (Go daemon)
- `hld/cmd/hld/main.go:14-61` — Entry point
- `hld/daemon/daemon.go:216-226` — Handler registration
- `hld/rpc/server.go:14-66` — JSON-RPC server with handler maps
- `hld/rpc/server.go:83-136` — Request dispatch (10MB buffer, line-delimited JSON)
- `hld/rpc/handlers.go:762-776` — Session handler registration (13 methods)
- `hld/rpc/approval_handlers.go:206-211` — Approval handler registration (4 methods)
- `hld/rpc/subscription_handlers.go:63-202` — Subscription with heartbeat + disconnect detection
- `hld/rpc/types_constants.go:84-96` — Context limit: 168000 tokens
- `hld/session/manager.go:182-545` — LaunchSession (full lifecycle)
- `hld/session/manager.go:548-757` — monitorSession (event streaming)
- `hld/session/manager.go:944-1321` — processStreamEvent (event parsing + token tracking)
- `hld/session/manager.go:963-968` — EffectiveContextTokens computation
- `hld/session/manager.go:1433-1650` — ContinueSession (fork)
- `hld/session/summary.go:9-24` — 50-char display truncation (NOT compaction)
- `hld/store/sqlite.go:2241-2276` — AddConversationEvent
- `hld/store/sqlite.go:2278` — GetSessionConversation (recursive parent chain)
- `hld/api/handlers/sessions.go:1601-1760` — Slash command discovery
- `hld/mcp/server.go:43-87` — MCP HTTP server with `request_approval` tool
- `hld/daemon/daemon.go:341-388` — Orphan session recovery on startup

### hlyr (TypeScript CLI)
- `hlyr/src/index.ts:17-134` — CLI entry point and command registration
- `hlyr/src/mcp.ts:19-183` — MCP server: `request_permission` tool with polling loop
- `hlyr/src/daemonClient.ts:110-332` — JSON-RPC client (Unix socket, 30s timeout)
- `hlyr/src/daemonClient.ts:139-240` — Subscription protocol (separate socket)
- `hlyr/src/commands/launch.ts:21-71` — Launch command with approval config
- `hlyr/src/commands/claude/init.ts:48-371` — `claude init` (distributes commands/agents/settings)
- `hlyr/src/commands/thoughts/init.ts:188-653` — Thoughts directory setup with git hooks
- `hlyr/src/commands/thoughts/sync.ts:32-196` — Thoughts sync + searchable index rebuild

### claudecode-go (Go SDK)
- `claudecode-go/client.go:67-102` — Binary discovery (8 search paths)
- `claudecode-go/client.go:183-311` — CLI flag construction (buildArgs)
- `claudecode-go/client.go:314-419` — Launch (subprocess, pipes, event channel)
- `claudecode-go/client.go:462-543` — Stream JSON parsing (10MB buffer)
- `claudecode-go/types.go:54-56` — SessionID + ForkSession config fields
- `claudecode-go/types.go:283-299` — Session struct (Events chan, done chan, result)

### humanlayer-wui (React desktop UI)
- `humanlayer-wui/src/AppStore.ts:319-386` — Session list refresh with optimistic updates
- `humanlayer-wui/src/AppStore.ts:1002-1040` — Active session detail fetch
- `humanlayer-wui/src/lib/daemon/http-client.ts` — All hld API calls
- `humanlayer-wui/src/components/.../ResponseEditor.tsx:558-683` — Slash command TipTap extension
- `humanlayer-wui/src/components/.../SlashCommandList.tsx:47-151` — Slash command dropdown
- `humanlayer-wui/src/components/.../TokenUsageBadge.tsx:7-13` — Context usage display
- `humanlayer-wui/src/constants/unsupportedCommands.ts` — 20 blocked commands including `/compact`
- `humanlayer-wui/src/components/.../DraftLauncherForm.tsx:208-558` — Draft session lifecycle
- `humanlayer-wui/src/components/.../useSessionActions.ts:59-221` — Continue/fork action
- `humanlayer-wui/src/hooks/useConversation.ts:14-137` — 1s conversation polling
- `humanlayer-wui/src/hooks/useSubscriptions.ts:19-135` — SSE event subscription

### .claude/ configuration
- `.claude/settings.json` — Permissions, env vars (MAX_THINKING_TOKENS=32000)
- `.claude/agents/*.md` — 6 sub-agent definitions
- `.claude/commands/*.md` — 28 slash command definitions

---

## Architecture Insights

### Design Decisions Relevant to Building a Compaction System

1. **Fork-not-compact philosophy**: HumanLayer chose session forking over compaction. When context fills, a new Claude Code process is spawned with `--resume <id> --fork-session`. The full conversation history is preserved in SQLite for display, but the new process gets a fresh context window. This avoids the complexity of summarization but means context is "wasted" when it could be compressed.

2. **Full event persistence**: Every text, tool_use, tool_result, and thinking block is stored individually in SQLite. The parent-chain recursive query rebuilds the complete conversation across forks. This is the foundation any compaction system would need to operate on.

3. **Token tracking is passive**: `EffectiveContextTokens` is computed from Claude's streaming `usage` data but only used for display. There's no logic that triggers actions when thresholds are crossed — the user must manually decide to fork.

4. **Context limit is hardcoded**: A single default of 168k tokens regardless of model. A compaction system would need model-aware limits.

5. **Slash commands are declarative**: Each command is a markdown file with a `description:` frontmatter field. They are discovered at runtime by walking directories. A compaction command could be added as a new `.claude/commands/compact.md`.

6. **Sub-agents for parallel research**: The agent pattern (multiple sonnet agents doing scoped research, opus parent synthesizing) is directly applicable to a compaction system — one could use sub-agents to summarize different conversation segments in parallel.

7. **No stdin injection after launch**: `claudecode-go` passes the query at process start and reads streaming output. There's no way to inject mid-conversation commands through the Go SDK. The `continueSession` mechanism spawns a NEW process each time.

8. **Event bus for real-time updates**: `hld/bus/events.go` provides pub/sub with 5 event types, 100-event buffered channels, and filter-based subscription. A compaction system could publish `context_compacted` events.

### Patterns to Adopt for a Compaction System

- **The fork mechanism** (`--resume <id> --fork-session`) as the underlying primitive — compaction could be implemented as "summarize the first N messages, then fork with the summary as the system prompt"
- **The SQLite event store** as the source of truth — compaction decisions can query the full history
- **The token tracking** as the trigger signal — fire compaction when `effectiveContextTokens / contextLimit > threshold`
- **The slash command system** as the user-facing interface — `/compact` could be un-blocked and given actual implementation
- **The sub-agent pattern** for parallel summarization of conversation segments

---

## Open Questions

1. **Claude Code's native `--fork-session` behavior**: What exactly does Claude Code do internally when forking? Does it include a summary of prior context, or does it start completely fresh? Understanding this would determine whether external compaction is needed.

2. **Token count accuracy**: The `EffectiveContextTokens` formula includes cache tokens — is this the right metric for "context fullness"? Cache tokens don't consume context window in the same way.

3. **Compaction granularity**: Should compaction summarize tool_use/tool_result pairs (which are often the largest context consumers), or only text messages? The SQLite schema stores them separately, enabling selective compaction.

4. **Multi-fork chains**: When a session has been forked 3+ times, the recursive conversation query could return massive histories. Should compaction operate at the fork boundary?

5. **Blocked command list rationale**: Why exactly does `/compact` "break session tracking"? Is it because Claude Code's native compact changes the conversation structure in a way that `monitorSession` can't parse, or is it a token-counting issue?
