---
date: 2026-02-13T15:26:37-0800
researcher: CoreyCole
git_commit: f551b0e
branch: main
repository: creative-mode
topic: "Mayor Prompt Attenuation + Memory Inspector Implementation Strategy"
tags: [implementation, strategy, imagegen, mayor, openclaw, prompt-engineering, gemini, datastar]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Mayor Prompt Attenuation + Memory Inspector

## Task(s)

1. **Research (completed)** — Explored the idea of "prompt attenuation" where the OpenClaw mayor agent suggests improved image generation prompts based on its personality (SOUL.md) and world knowledge (MEMORY.md). Produced a research document.

2. **Implementation plan (completed)** — Created a detailed two-phase plan:
   - **Phase 1: Prompt Attenuation** — "Suggest" button on Assets tab sends user's prompt to the mayor via OpenClaw hooks API. Mayor enhances it using its full context, calls back via a new `prompt-enhance` skill. Harness patches editable suggestion preview via SSE.
   - **Phase 2: Memory Inspector** — New "Mayor" tab in chat panel for browsing/editing mayor workspace files. Bootstrap files (SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md) are editable; skill files are read-only.

3. **Implementation (not started)** — No code has been written yet.

### Key Design Decisions Made With User
- **Route through OpenClaw agent** (not direct Gemini text API). The mayor participates in the conversation and remembers suggestions. This requires an async callback pattern since OpenClaw's hooks API is fire-and-forget.
- **All bootstrap files editable + skills read-only** in the memory inspector.
- **World style stored in MEMORY.md** (maintained by OpenClaw autonomously), not a separate DB column.

## Critical References

- `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — The implementation plan (this is the primary document to follow)
- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — The mayor infrastructure plan (dependency; provides context on workspace files, skills, hooks API, agent provisioning)
- `thoughts/CoreyCole/research/2026-02-13_14-46-53_mayor-prompt-attenuation.md` — Research document with Gemini API findings, prompt engineering best practices, and architecture exploration

## Recent changes

No code changes were made. This session was research and planning only.

## Learnings

- **OpenClaw hooks API is fire-and-forget**: `POST /hooks/agent` sends a message but returns no response body. To get a synchronous response, use the callback pattern — the mayor calls a harness endpoint via a skill (same as `world-build` skill calling `POST /api/mayor/build`).

- **Async callback pattern**: The suggest handler holds an SSE connection open, creates a Go channel keyed by requestID, sends the prompt to the mayor via hooks, blocks on the channel (30s timeout), and unblocks when the callback handler resolves it. This is new infrastructure (`SuggestionTracker` in `suggest.go`).

- **Mayor messages use text prefixes**: `[ENHANCE PROMPT]`, `[BUILD COMPLETE]`, `[BUILD FAILED]` — not Discord @mentions. The AGENTS.md template instructs the mayor to watch for these.

- **ReadSignals MUST be called before NewSSE**: This is documented in `MEMORY.md` and in the code at `imagegen.go:44-45`. NewSSE flushes response headers which invalidates the request body.

- **Tab system in chat panel**: Currently 4 tabs (Global, World, Lineage, Assets) at `chat.templ:11-24`. Adding Mayor makes 5. Each tab uses `CE.SelectTab("name")` + `CE.TabActiveClass("name")` expressions, and content divs use `data-show="$active_tab === 'name'"`.

- **Image gen state machine**: All fragments share `id="image-gen-content"` and are swapped via `PatchElementTempl`. States: Idle, Generating, Done, Saved, Error. We add: Suggesting, Suggested.

- **Skill files contain X-Mayor-Secret**: The allowlist in the memory inspector must separate editable vs read-only to prevent exposing secrets in editable textareas. Read-only display is fine since the user is the world creator.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — Full implementation plan with phase breakdown, code snippets, file inventory, and success criteria
- `thoughts/CoreyCole/research/2026-02-13_14-46-53_mayor-prompt-attenuation.md` — Research document covering Gemini API capabilities, prompt engineering best practices, current codebase analysis, and architecture options
- `/Users/coreycole/.claude/plans/silly-wondering-mccarthy.md` — Claude Code plan file (duplicate of the thoughts plan, used during planning session)

## Action Items & Next Steps

1. **Read the implementation plan** at `thoughts/CoreyCole/plans/2026-02-13_15-20-45_mayor-prompt-attenuation.md` — it has the full phase breakdown and implementation sequence.

2. **Implement Phase 1 (Prompt Attenuation)** following the 9-step implementation sequence in the plan:
   - Add signals to `harness/views/world/signals.go`
   - Create `SuggestionTracker` in `harness/internal/server/suggest.go`
   - Add templ fragments (`ImageGenSuggesting`, `ImageGenSuggested`) and update `ImageGenInputBar` in `harness/views/imagegen/imagegen.templ`
   - Add `handleImageSuggest`, `handleImageIdle`, `handleSuggestCallback` handlers in `harness/internal/server/imagegen.go`
   - Add `prompt-enhance` skill template in `harness/internal/mayor/skills.go` and update `AGENTS.md` template
   - Wire routes in `harness/internal/server/server.go` and read env vars in `harness/main.go`

3. **Implement Phase 2 (Memory Inspector)**:
   - Create `harness/views/mayor/` package with `mayor.templ` and `types.go`
   - Add Mayor tab to `harness/views/chat/chat.templ`
   - Create file handlers in `harness/internal/server/mayor.go`
   - Wire routes

4. **Build and verify**: `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`

5. **Note**: The mayor infrastructure (Phases 1-3 of `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`) must be deployed first. The prompt attenuation code can be written and compiled now, but cannot be tested end-to-end until OpenClaw is running with a provisioned mayor agent.

## Other Notes

- **Key files to understand before implementing**:
  - `harness/internal/server/imagegen.go` — Existing image gen handlers; follow the same SSE pattern
  - `harness/views/imagegen/imagegen.templ` — Fragment state machine to extend
  - `harness/views/chat/chat.templ` — Tab system to add Mayor tab to
  - `harness/views/world/signals.go` — Signal definitions
  - `harness/internal/server/server.go:523-556` — File serving security pattern to reuse

- **The callback endpoint (`POST /api/mayor/suggest-callback`) is NOT in the approved auth group** — it uses `X-Mayor-Secret` header auth instead of session cookies, since it's called by the mayor agent, not a browser.

- **MEMORY.md may not exist** until the mayor's first run (OpenClaw creates it when the agent first writes to memory). The file list handler should show it as "Not yet created" and the suggest handler should work with just SOUL.md content if MEMORY.md is absent.
