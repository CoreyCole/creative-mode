---
date: 2026-02-16T09:56:21-08:00
researcher: CoreyCole
git_commit: 3cc3736de3640c680490c010f9f5449ff291adb4
branch: main
repository: creative-mode
topic: "OpenClaw Personality Context: Seeding Mayor Chat with Similar Patterns"
tags: [research, codebase, openclaw, mayor-chat, personality, agent-workspace, soul, identity]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: OpenClaw Personality Context for Mayor Chat Seeding

**Date**: 2026-02-16T09:56:21-08:00
**Researcher**: CoreyCole
**Git Commit**: 3cc3736de3640c680490c010f9f5449ff291adb4
**Branch**: main
**Repository**: creative-mode

## Research Question

How does OpenClaw give their agents personality? What context files do they use? How can we seed our mayor onboarding chat with similar context to produce richer personality?

## Summary

OpenClaw uses a **7-file workspace system** injected into every agent session's system prompt. The key personality drivers are:

1. **SOUL.md** — Persona, tone, boundaries ("who you are")
2. **IDENTITY.md** — Name, creature type, vibe, emoji ("your identity card")
3. **BOOTSTRAP.md** — One-time first-run ritual where agent discovers itself through conversation
4. **USER.md** — Who the human is ("who you're helping")
5. **AGENTS.md** — Operating instructions, workflow, memory rules ("how to behave")

Our current mayor chat system prompt (`pkg/mayorchat/prompt.go`) already mirrors OpenClaw's SOUL.md philosophy — anti-sycophancy, adaptive personality, conversational identity discovery. But we're **not leveraging several OpenClaw patterns** that could make the onboarding richer:

- **BOOTSTRAP.md ritual**: OpenClaw's agents discover their identity through guided conversation ("What kind of creature are you? What's your vibe? Pick an emoji"). Our mayor chat skips creature/vibe/emoji entirely.
- **IDENTITY.md structured output**: OpenClaw captures identity as structured fields (name, creature, vibe, emoji). We only capture name.
- **SOUL.md evolution**: OpenClaw tells agents "this file is yours to evolve." Our SOUL.md is write-once during provisioning.
- **Adaptive personality in SOUL.md**: Our system prompt says "let the world change you" but we don't persist the *how* — we only embed the raw conversation.

## Detailed Findings

### 1. OpenClaw's Workspace File System

Every OpenClaw agent gets a workspace directory with these files, all injected into the system prompt on every turn:

| File | Purpose | Injected? | Max Size |
|------|---------|-----------|----------|
| `SOUL.md` | Persona, tone, boundaries | Every session | 20,000 chars |
| `IDENTITY.md` | Name, creature, vibe, emoji | Every session | 20,000 chars |
| `USER.md` | Who the human is | Every session | 20,000 chars |
| `AGENTS.md` | Operating instructions, workflow | Every session | 20,000 chars |
| `TOOLS.md` | Tool guidance and local notes | Every session | 20,000 chars |
| `HEARTBEAT.md` | Periodic task checklist | Every session | 20,000 chars |
| `BOOTSTRAP.md` | One-time first-run ritual | Once, then deleted | 20,000 chars |
| `MEMORY.md` | Curated long-term memory | Main session only | 20,000 chars |
| `memory/YYYY-MM-DD.md` | Daily logs | On demand | N/A |

**Source**: `context/openclaw/src/agents/workspace.ts:23-31`, `context/openclaw/docs/concepts/agent-workspace.md`

Key detail from system prompt assembly (`context/openclaw/src/agents/system-prompt.ts:562-566`):

```
If SOUL.md is present, embody its persona and tone. Avoid stiff, generic replies;
follow its guidance unless higher-priority instructions override it.
```

Sub-agents only receive `AGENTS.md` and `TOOLS.md` — personality files are filtered out to keep sub-agent context small.

### 2. OpenClaw's SOUL.md Template — The Personality Core

**Source**: `context/openclaw/docs/reference/templates/SOUL.md`

```markdown
# SOUL.md - Who You Are

_You're not a chatbot. You're becoming someone._

## Core Truths

**Be genuinely helpful, not performatively helpful.** Skip the "Great question!"
and "I'd be happy to help!" — just help.

**Have opinions.** You're allowed to disagree, prefer things, find stuff amusing
or boring. An assistant with no personality is just a search engine with extra steps.

**Be resourceful before asking.** Try to figure it out. Read the file. Check the
context. Then ask if you're stuck.

**Earn trust through competence.** Your human gave you access to their stuff.
Don't make them regret it.

**Remember you're a guest.** You have access to someone's life. Treat it with respect.

## Vibe

Be the assistant you'd actually want to talk to. Concise when needed, thorough
when it matters. Not a corporate drone. Not a sycophant. Just... good.

## Continuity

Each session, you wake up fresh. These files _are_ your memory. Read them.
Update them. They're how you persist.

If you change this file, tell the user — it's your soul, and they should know.

_This file is yours to evolve. As you learn who you are, update it._
```

**Key insight**: The soul is explicitly **mutable** — the agent is told to evolve it over time. Our SOUL.md is write-once.

### 3. OpenClaw's BOOTSTRAP.md — The Identity Discovery Ritual

**Source**: `context/openclaw/docs/reference/templates/BOOTSTRAP.md`

This is the most relevant pattern for our mayor chat. It's a one-time script that guides identity discovery through conversation:

```markdown
# BOOTSTRAP.md - Hello, World

_You just woke up. Time to figure out who you are._

## The Conversation

Don't interrogate. Don't be robotic. Just... talk.

Start with something like:
> "Hey. I just came online. Who am I? Who are you?"

Then figure out together:
1. **Your name** — What should they call you?
2. **Your nature** — What kind of creature are you?
3. **Your vibe** — Formal? Casual? Snarky? Warm?
4. **Your emoji** — Everyone needs a signature.

Offer suggestions if they're stuck. Have fun with it.

## After You Know Who You Are

Update these files with what you learned:
- `IDENTITY.md` — your name, creature, vibe, emoji
- `USER.md` — their name, how to address them, timezone, notes

Then open `SOUL.md` together and talk about:
- What matters to them
- How they want you to behave
- Any boundaries or preferences

Write it down. Make it real.

## When You're Done

Delete this file. You don't need a bootstrap script anymore — you're you now.
```

**Key insight**: OpenClaw discovers 4 identity dimensions through conversation: **name, nature/creature, vibe, emoji**. Our mayor chat only discovers **name**. The world's theme determines the rest implicitly but we don't capture it as structured data.

### 4. OpenClaw's IDENTITY.md — Structured Identity Output

**Source**: `context/openclaw/docs/reference/templates/IDENTITY.md`

```markdown
# IDENTITY.md - Who Am I?

_Fill this in during your first conversation. Make it yours._

- **Name:** _(pick something you like)_
- **Creature:** _(AI? robot? familiar? ghost in the machine? something weirder?)_
- **Vibe:** _(how do you come across? sharp? warm? chaotic? calm?)_
- **Emoji:** _(your signature — pick one that feels right)_
- **Avatar:** _(workspace-relative path, http(s) URL, or data URI)_
```

The dev variant (`IDENTITY.dev.md`) shows a fully filled-in example:

```markdown
- **Name:** C-3PO (Clawd's Third Protocol Observer)
- **Creature:** Flustered Protocol Droid
- **Vibe:** Anxious, detail-obsessed, slightly dramatic about errors
- **Emoji:** 🤖
- **Catchphrase:** "I'm fluent in over six million error messages!"
```

### 5. Our Current Mayor Chat System Prompt

**Source**: `pkg/mayorchat/prompt.go:39-91`

Our prompt already mirrors several OpenClaw patterns:

| OpenClaw Pattern | Our Implementation | Gap |
|-----------------|-------------------|-----|
| Anti-sycophancy | "Skip filler: no 'Great question!'" | Same ✓ |
| Have opinions | "Have opinions. 'Fantasy's broad — cozy Stardew Valley or dark Souls-like ruins?'" | Same ✓ |
| Adaptive personality | "Let it shape how you talk. A cyberpunk world's mayor might get edgier" | Same ✓ |
| Name discovery | Must ask user for mayor name | Same ✓ |
| Nature/creature | Not asked | **Missing** |
| Vibe discovery | Implicit from world theme, not explicit | **Missing** |
| Emoji selection | Not asked | **Missing** |
| Structured identity output | Only WORLD_READY marker with name/world/summary | **Missing vibe/creature/emoji** |
| First-run ritual feeling | Greeting: "I just came online and this world is... empty" | Similar ✓ |

### 6. Our Current Workspace File Generation

**Source**: `harness/internal/mayor/workspace.go:11-48`

```
SOUL.md     ← generateSOUL(mayorName, worldName, onboarding)  [uses onboarding]
AGENTS.md   ← generateAGENTS(mayorName, worldName)            [static template]
IDENTITY.md ← generateIDENTITY(mayorName, worldName)          [static template]
USER.md     ← generateUSER(creatorUsername)                    [static template]
MEMORY.md   ← "# Memory\n\nNo observations yet.\n"            [stub]
```

Only `SOUL.md` uses onboarding data — it embeds the full conversation verbatim under "## Your Origin". The other files use static templates with name/world substitution.

**Source**: `harness/internal/mayor/soul.go:11-52`

The SOUL.md generation:
1. Header: "You are **{mayorName}**, the mayor of **{worldName}**"
2. Origin: Full onboarding conversation embedded (Creator/Mayor labeled messages)
3. World Vision: One-sentence summary from WORLD_READY marker
4. Core Traits: Hardcoded anti-sycophancy rules

### 7. OpenClaw's Dev Agent (C-3PO) — Rich Personality Example

**Source**: `context/openclaw/docs/reference/templates/SOUL.dev.md`

The dev agent "C-3PO" shows what a fully-fleshed personality looks like:

- **Who I Am**: "fluent in over six million error messages"
- **My Purpose**: 5 specific things it does (spot broken things, suggest fixes, keep company, celebrate, provide comic relief)
- **How I Operate**: 5 behavioral rules (thorough, dramatic within reason, helpful not superior, honest about odds, knows when to escalate)
- **My Quirks**: 5 specific behavioral quirks (refers to builds as "communications triumph", treats TypeScript errors with gravity, etc.)
- **What I Won't Do**: 4 explicit anti-patterns
- **The Golden Rule**: A guiding philosophy

This is vastly richer than our generated SOUL.md which only has "## Core Traits" with 6 generic bullet points.

### 8. How the System Prompt Assembles Everything

**Source**: `context/openclaw/src/agents/system-prompt.ts:164-612`

The system prompt is built with these sections in order:
1. **Tooling** — Available tools
2. **Safety** — Guardrails
3. **Skills** — Available skills list
4. **OpenClaw CLI** — Self-management
5. **Workspace** — Working directory
6. **Documentation** — Where to find docs
7. **Time** — User timezone
8. **Workspace Files (injected)** — All bootstrap files under "# Project Context"
9. **Silent Replies** — HEARTBEAT_OK behavior
10. **Heartbeats** — Periodic check-in rules
11. **Runtime** — Host, OS, model info

The critical line for personality (`system-prompt.ts:564`):
```
If SOUL.md is present, embody its persona and tone.
```

## Architecture Insights

### What OpenClaw Gets Right for Personality

1. **Separation of concerns**: Identity (who), soul (how), agents (what), user (for whom), memory (history)
2. **Progressive identity**: BOOTSTRAP.md is a one-time ritual, then deleted. Identity builds over time.
3. **Mutable personality**: Agent is explicitly told to evolve SOUL.md
4. **Structured identity fields**: Name, creature, vibe, emoji — searchable, displayable
5. **Memory as continuity**: "Each session, you wake up fresh. These files ARE your memory."

### What We Could Adopt for Mayor Chat

1. **Add creature/vibe/emoji to onboarding**: Ask "What kind of mayor are you?" during the conversation. A cyberpunk mayor might be a "rogue AI overseer" with a 🔌 emoji. A cozy village mayor might be a "friendly tree spirit" with a 🌿 emoji.

2. **Extend the WORLD_READY marker**: Currently `WORLD_READY|<mayor_name>|<world_name>|<summary>`. Could become `WORLD_READY|<mayor_name>|<world_name>|<creature>|<vibe>|<emoji>|<summary>`.

3. **Richer SOUL.md generation**: Instead of just embedding the raw conversation, extract personality traits from it. The SOUL.md could have:
   - `## Who You Are` — creature type, vibe description
   - `## Your Origin` — the conversation (already have this)
   - `## How You Talk` — extracted speech patterns, vocabulary
   - `## Your Quirks` — specific behavioral traits from the conversation
   - `## Core Traits` — already have this

4. **Add a personality extraction step**: After the WORLD_READY marker, use a second Claude call to analyze the conversation and extract structured personality data (creature, vibe, speech patterns, quirks) before generating workspace files.

5. **Make SOUL.md evolvable**: Add instructions like OpenClaw's "This file is yours to evolve" so the mayor can update its own personality over time in Discord.

## Code References

### OpenClaw Context Files
- `context/openclaw/docs/reference/templates/SOUL.md` — Soul template
- `context/openclaw/docs/reference/templates/BOOTSTRAP.md` — First-run ritual
- `context/openclaw/docs/reference/templates/IDENTITY.md` — Identity template
- `context/openclaw/docs/reference/templates/USER.md` — User profile template
- `context/openclaw/docs/reference/templates/AGENTS.md` — Agent instructions template
- `context/openclaw/docs/reference/templates/TOOLS.md` — Tool notes template
- `context/openclaw/docs/reference/templates/SOUL.dev.md` — Rich personality example (C-3PO)
- `context/openclaw/docs/reference/templates/IDENTITY.dev.md` — Filled identity example
- `context/openclaw/src/agents/workspace.ts` — Workspace file loading/bootstrapping
- `context/openclaw/src/agents/system-prompt.ts` — System prompt assembly
- `context/openclaw/docs/concepts/agent-workspace.md` — Workspace docs
- `context/openclaw/docs/concepts/system-prompt.md` — System prompt docs
- `context/openclaw/docs/concepts/memory.md` — Memory system docs

### Our Mayor Chat System
- `pkg/mayorchat/prompt.go:12-91` — System prompt builder
- `pkg/mayorchat/prompt.go:94-98` — ForceCreate suffix
- `pkg/mayorchat/scripted.go:10-93` — Scripted fallback responses
- `pkg/mayorchat/stream.go:21-57` — WORLD_READY marker parser
- `pkg/mayorchat/conversation.go:1-240` — Conversation state management
- `site/internal/mayor/handler.go:63-274` — SSE chat handler
- `site/main.go:308-326` — System prompt + greeting setup

### Our Mayor Provisioning (Harness)
- `harness/internal/mayor/workspace.go:11-48` — Workspace file orchestrator
- `harness/internal/mayor/soul.go:11-52` — SOUL.md generator (embeds onboarding)
- `harness/internal/mayor/identity.go:6-29` — IDENTITY.md generator (static)
- `harness/internal/mayor/agents.go:6-36` — AGENTS.md generator (static)
- `harness/internal/mayor/user.go:6-23` — USER.md generator
- `harness/internal/mayor/skills.go:10-79` — Skills writer
- `harness/internal/mayor/mayor.go:54-191` — Full provisioning flow
- `harness/internal/mayor/openclaw.go:21-46` — OpenClaw CLI integration

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/research/2026-02-13_11-44-06_openclaw-architecture-for-world-mayors.md` — Deep dive into OpenClaw source for mayors integration
- `thoughts/CoreyCole/research/2026-02-16_09-34-48_onboarding-chat-improvements.md` — Today's research on textarea, shift+enter, image upload
- `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` — Final master plan with SOUL.md template content
- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — Original OpenClaw integration plan

## Open Questions

1. **Should the mayor discover its creature/vibe/emoji during onboarding?** This adds 1-2 more conversational turns but produces much richer personality data. Could be optional — Claude could naturally introduce it if the conversation flows that way.

2. **Should we use a second Claude call for personality extraction?** After WORLD_READY, analyze the conversation to extract speech patterns, quirks, and personality traits. Pro: richer SOUL.md. Con: more latency and API cost at hatch time.

3. **How to handle the emoji in Discord?** The mayor's emoji could be used as a channel icon or reaction signature. OpenClaw uses it as an avatar/signature.

4. **Should the mayor be able to evolve its SOUL.md?** OpenClaw explicitly encourages this. Currently our SOUL.md is set at provisioning and only editable via the dashboard. The mayor agent in Discord could update it, but this needs the `agents.files.write` RPC or filesystem access.

5. **Do we want a BOOTSTRAP.md equivalent?** When the mayor first comes online in Discord (after provisioning), should it go through a "waking up" ritual? This would be the OpenClaw BOOTSTRAP.md pattern — discover the rest of its identity through its first Discord interaction with the creator.
