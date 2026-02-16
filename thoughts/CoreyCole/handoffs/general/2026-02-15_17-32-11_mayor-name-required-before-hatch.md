---
date: 2026-02-15T17:32:11-08:00
researcher: CoreyCole
git_commit: 9602e42545e2390456bfd30014d9d53eed48cd20
branch: main
repository: creative-mode
topic: "Mayor Name Required Before World Hatch"
tags: [implementation, onboarding, mayor, scripted-flow, system-prompt]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Require Mayor Name Before World Creation

## Task(s)

**Planned** — Refine the onboarding experience so the mayor explicitly asks for a name before the world can be created. If the user doesn't provide a name, the mayor should confirm they want "Mayor" as the default with a self-deprecating remark ("that's a bit lame, but works for now — you can rename me at any time"). This applies to **both** the scripted fallback flow and the Opus-powered system prompt.

## Critical References

- `site/CLAUDE.md` — Full architecture of the onboarding flow, WORLD_READY marker protocol, scripted fallback design
- `site/internal/mayor/prompt.go` — System prompt for Opus-powered flow (the "what you need to learn" section)
- `site/internal/mayor/scripted.go` — Pre-written scripted responses (4-stage pipeline)

## Recent changes

- `site/internal/mayor/handler.go:543-563` — Replaced `fmt.Printf` with `h.logger.Error` in `notifyHarnessWorldHatchedWithCover`
- `site/internal/mayor/cover.go:27-30` — Added glob cleanup of old cover art files before writing new ones on regeneration
- Committed and pushed full cover art feature (28 files, 1139 insertions) at `9602e42`

## Learnings

### Onboarding Architecture

The onboarding has two parallel paths that must stay in sync:

1. **Opus-powered flow** (`handler.go` → Claude API streaming → `WORLD_READY|mayor|world|summary` marker)
   - System prompt in `prompt.go:21-57` defines what info to collect: setting, gameplay, world name, mayor name
   - The prompt says "Suggest something that fits the theme" for mayor name — currently it's one of four things to collect naturally
   - The `WORLD_READY` marker is emitted by Claude when all four are collected
   - Force-create button (`create_world` signal) appends to system prompt telling Claude to fill in missing details

2. **Scripted fallback** (`scripted.go` — used when API is unavailable/billing issues)
   - 4-stage pipeline: stage 0 = world idea → stage 1 = gameplay → stage 2 = world name → stage 3 = mayor name
   - Stage indices map to user message count (0-indexed)
   - `nthUserMessage(messages, N)` extracts user input at each stage
   - `lastUserMessage` gets mayor name (stage 3)
   - Names extracted from conversation history, not signals

### Current Mayor Name Handling

- **Scripted**: Mayor name is the 4th user message (stage 3). The mayor asks "What should I go by?" at stage 2.
- **Opus**: Claude collects it organically. No enforcement that it happens before `WORLD_READY`.
- **Greeting** (`site/main.go:239-241`): "I'm the Mayor — though I don't have a real name yet" — already hints at needing a name.
- **Force-create**: When clicked, tells Claude to "fill in any missing details" — so Claude may auto-generate a mayor name the user never approved.

### Key Constraint

The `WORLD_READY|<mayor_name>|<world_name>|<one sentence summary>` marker format is parsed in `handler.go:264-271`. The mayor name extracted here flows through to Discord channel creation, OpenClaw agent provisioning, and the harness webhook. So "Mayor" as a default must be a valid value that works downstream.

## Artifacts

No new artifacts produced — this handoff describes planned work.

## Action Items & Next Steps

### 1. Update system prompt (`site/internal/mayor/prompt.go`)

- In the "What you need to learn" section, add explicit instruction: the mayor **must** ask for a name before emitting `WORLD_READY`. If the user declines or says something like "just Mayor" or "I don't know", the mayor should confirm with a self-deprecating response (e.g., "Just 'Mayor'? That's... functional. A bit like naming your dog 'Dog.' But sure, it works — you can rename me any time.") and then proceed.
- Add instruction: **never auto-generate a mayor name** — always ask, even on force-create.

### 2. Update scripted flow (`site/internal/mayor/scripted.go`)

- The flow already asks for a mayor name at stage 2 (response: "I need a name too. What should I go by?")
- Add handling for when the user's stage-3 input looks like a refusal (empty, "idk", "no", "just mayor", "mayor", etc.)
  - Insert an extra confirmation response: acknowledge "Mayor" is lame but workable, mention rename-later option
  - Then proceed to hatch with `mayorName = "Mayor"`
- Consider: this may need a 5th stage or a branch at stage 3

### 3. Update force-create handling

- In `handler.go:152-155`, the force-create system prompt addition says "fill in any missing details" — change this to explicitly say "if the user hasn't provided a mayor name, do NOT invent one. Instead ask for it."
- In `scripted.go:75-108`, `handleScriptedForceCreate` skips to hatching if stage >= 3. If mayor name hasn't been collected (stage < 3), it should handle the name-missing case.

### 4. Validate "Mayor" works downstream

- Verify "Mayor" is acceptable in: Discord channel creation (`worldchannel.CreateChannel`), mayor name uniqueness check (`CheckMayorNameUnique`), OpenClaw workspace generation, harness webhook payload.
- Consider: if multiple users pick "Mayor", the uniqueness check will append "Mayor II", "Mayor III", etc. This is probably fine.

### 5. Update greeting (optional)

- Current greeting in `site/main.go:239-241` already says "I don't have a real name yet" — this is good framing. No change needed unless the tone should be adjusted.

## Other Notes

### File Locations

| File | Purpose |
|------|---------|
| `site/internal/mayor/prompt.go` | System prompt builder — main place to add name-required instruction |
| `site/internal/mayor/scripted.go` | Scripted fallback — 4-stage responses + name extraction logic |
| `site/internal/mayor/handler.go:60-290` | `HandleChat` — Claude streaming + WORLD_READY parsing |
| `site/internal/mayor/handler.go:292-349` | `prepareCoverArtAndHatch` — unified entry after WORLD_READY |
| `site/internal/mayor/handler.go:72-76` | Force-create signal handling |
| `site/main.go:237-243` | Greeting seed into conversation |
| `site/internal/mayor/session.go` | ConversationManager — message persistence + transient state |

### Datastar Signals

The `create_world` boolean signal is set by the "Create World" button in the UI. It's read in `HandleChat` as `signals.CreateWorld`. The `mayor_input` string signal carries user text. No new signals should be needed for this change.

### Testing Approach

- Test scripted flow by setting `ANTHROPIC_API_KEY` to empty/invalid
- Test Opus flow with valid key
- Test force-create in both modes
- Test refusal variations: "idk", "no name", "just Mayor", empty input, "Mayor"
- Verify downstream: check Discord channel name, harness webhook payload, OpenClaw workspace files
