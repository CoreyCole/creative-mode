# Demo Video Brainstorm — Hackathon Submission

**Format**: Podcast-style conversation between Corey + friend
**Constraint**: 3-minute cut for submission + link to full podcast version

---

## 3-MINUTE CUT — Submission Script

Every second counts. This is the version judges see. Front matter is compressed hard — the build and multiplayer are the stars.

---

### 0:00–0:10 — Hook (Marketing site hero on screen)

Screen: Marketing site homepage, hero visible.

**Corey**: "Game development has always been the most technical, most lonely creative task. What if it wasn't? This is Creative Mode."

---

### 0:10–0:25 — What It Is (Feature cards flash by)

Screen: Quick scroll through feature cards. Don't linger.

**Corey**: "An open-source multiplayer game world builder powered by Claude Opus. You describe what you want, your AI mayor writes the code, compiles to WebAssembly, and deploys live in the browser. A game engine for everyone who has an idea."

---

### 0:25–0:50 — Meet the Mayor (Onboarding)

Screen: Friend clicks "Meet the Mayor." Discord OAuth → invite code → conversation begins.

**Friend** types naturally. Mayor greets: "What kind of world have you been dreaming about?" Friend describes their vision in a couple messages. Mayor synthesizes a world summary card.

**Corey** (over the conversation): "The onboarding is the product. You're not filling out a form — you're starting a relationship with the AI that builds your world."

---

### 0:50–2:15 — The Build (Money shot — 85 seconds)

Screen: Friend enters the world. Starter scene loads. Friend uploads a couple pre-staged assets, then types the first prompt.

**Build cycle 1** (~40s):
1. Friend types a visually dramatic prompt — "Create a cozy tavern room with a fireplace and warm lighting"
2. Mayor responds, build kicks off
3. Progress visible in overlay (time-lapse the wait if >30s)
4. Browser hot-reloads — friend reacts

**Corey narrates** during the build: "Right now Opus is editing Bevy ECS code — entities, components, systems. Real Rust, real game architecture. It compiles to WASM and the browser picks it up. This isn't generating images — it's writing game code."

**Build cycle 2** (~30s):
1. Friend iterates — "Add a door on the left wall that leads to a garden" or "Put a character by the fireplace I can talk to"
2. Build kicks off, result appears
3. Friend explores the change

> **Key line** (during build wait): "Game development is traditionally lonely — one person in an IDE for months. Creative Mode flips that. It's multiplayer from day one."

**Build cycle 3 — optional** (~15s): Quick one if time allows, or skip to multiplayer.

---

### 2:15–2:40 — The Multiplayer Moment

Screen: Split view or second browser — Corey joins the friend's world.

**Corey** appears in the world. Both are in the same scene, walking around what the friend just built.

**Friend**: Reacts to seeing Corey in their world.

**Corey**: "Your friends can hop in, test ideas, combine their creativity with yours. The best games aren't built by one person — they're built by friends riffing on each other's ideas."

Maybe: Corey suggests a change from inside the world. Shows collaborative building is real.

---

### 2:40–2:55 — The Vision

**Corey**: "What you're seeing today is the floor, not the ceiling. With the Opus released six months from now, the mayors get dramatically better. Creative Mode is open source — the games you build are yours."

---

### 2:55–3:00 — Close

**Friend**: One authentic line.

**Corey**: "We didn't just build a tool. We built the first mayor — and the mayors are going to get very, very good."

Screen: Creative Mode logo + GitHub link + "Full conversation: [podcast link]"

---

## Timing Budget

| Section | Duration | Purpose |
|---------|----------|---------|
| Hook | 10s | Set the thesis |
| What It Is | 15s | Context — fast and confident |
| Meet the Mayor | 25s | Onboarding magic |
| **The Build** | **85s** | **The money shot — 2-3 prompt cycles** |
| **Multiplayer** | **25s** | **Friend joins, collaborative moment** |
| Vision | 15s | Future + open source |
| Close | 5s | Memorable ending |
| **Total** | **3:00** | |

---

## Profound Statements — Placement Guide

| Time | Statement | Theme |
|------|-----------|-------|
| 0:00 | Game dev is the most technical, most lonely creative task. What if it wasn't? | Hook |
| 0:20 | A game engine for everyone who has an idea. | Democratization |
| 0:35 | The onboarding IS the product. | Product design |
| 1:30 | This isn't generating images — it's writing game code. | Technical credibility |
| 1:50 | Game dev is lonely. Creative Mode makes it multiplayer from day one. | Collaboration |
| 2:25 | Best games are built by friends riffing on each other's ideas. | Community |
| 2:45 | What you're seeing is the floor, not the ceiling. | Future vision |
| 2:55 | We built the first mayor — and the mayors are going to get very, very good. | Closing hook |

---

## FULL PODCAST VERSION — Extended Agenda

Link this from the 3-minute video description. This is the uncut conversation — can run 7-10 minutes.

### 0:00–0:30 — Cold Open
Same hook as the 3-minute cut, but let the conversation breathe.

### 0:30–2:00 — What Is Creative Mode?
Walk through all six feature cards. Explain:
- The fork model (every prompt = a branch, git for game worlds)
- Mayor personality and memory system
- OpenClaw agent architecture
- Discord-first design (build from your phone)

### 2:00–2:30 — The README & Tech Stack
Flash the GitHub repo. Corey talks through the stack: Bevy, Lightyear, OpenClaw, Datastar, templ, Gemini Nano Banana, SQLite, tmux.

"Like the jump from assembly to C — we develop at a higher level of abstraction."

### 2:30–4:00 — Meet the Mayor (Full onboarding)
Let the conversation play out naturally. Don't rush. Show the full back-and-forth between friend and mayor. Let friend ask their own questions.

### 4:00–5:00 — Seeding the World
Upload assets. Explain checkpoints. Show the lobby and world grid.

"Every time you ask for something, a new checkpoint is created. You can always go back."

### 5:00–7:00 — Building from Scratch
Multiple prompt cycles. Show iteration. Let the friend drive.

If a build fails, show recovery — "the mayor is learning, and we can always fork back."

### 7:00–8:00 — Multiplayer Moment
Corey joins the friend's world. Both in the same scene. Talk about World Hop, Discord integration, collaborative building.

"The best games aren't built by one person — they're built by friends riffing on each other's ideas."

### 8:00–9:00 — Where It's Heading
- Better Opus = more sophisticated building
- Asset generation improvements
- Phone-first via Discord
- Community of world builders

### 9:00–9:30 — Closing
"Creative Mode is open source under the Elastic License. Games you build are yours."

Friend: authentic final reaction.

Corey: "We didn't just build a tool. We built the first mayor."

---

## Production Notes

- **Screen recording**: OBS, browser + picture-in-picture webcam
- **Audio**: Good mics, podcast energy — two friends excited about what they built, not a corporate demo
- **3-minute cut**: Pre-record everything, then edit ruthlessly. The build cycle is the star — give it a full 60 seconds
- **Speed-up option**: If builds take >30s, speed up the build wait with a time-lapse effect (show the terminal scrolling fast) and cut to the result
- **Friend's role**: Genuinely naive. Don't script reactions. Authenticity > polish
- **Pre-staging**: Have assets ready to upload. Have a working world the friend can build from. Test the exact prompts beforehand to know they produce good results
- **Backup plan**: If something breaks, that's content — "the mayor is learning on the job" is literally a feature card
- **End card**: GitHub URL, Discord invite, link to full podcast
