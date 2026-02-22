---
date: 2026-02-20T21:58:45-08:00
reviewer: Claude (Staff Eng Review)
git_commit: a9ad7f5a52eabdedeff48af51c45921e3b258def
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md
status: complete
type: plan_review
---

# Plan Review: OpenClaw Setup + Omnipresent Mayor Widget

### Summary

This is a well-researched, thorough plan with accurate current-state analysis and clear phase separation. However, it has a critical gap around OpenClaw session/conversation management that will cause the chat widget to lose context between messages, and several SSE lifecycle concerns that need resolution before implementation.

### Critical Issues (Must Address Before Implementation)

These issues could cause significant problems if not resolved:

1. **OpenClaw session key management is completely unaddressed**
   - Problem: The plan describes calling `/v1/chat/completions` with `stream: true` but never discusses how conversation continuity works. Verified in `context/openclaw/src/gateway/http-utils.ts:65-79`: if no `x-openclaw-session-key` header and no `user` field are provided, OpenClaw generates a random UUID session key per request. This means **every chat message would start a fresh conversation with no memory of prior messages**.
   - Risk: The mayor will respond to each message in isolation, ignoring everything previously discussed. This completely breaks the chat experience.
   - Suggestion: The `StreamChat` client must send a consistent `x-openclaw-session-key` header (e.g., `webchat-user:{userID}:world:{worldID}`) AND/OR set the `user` field in the request body to the harness user ID. Add a `SessionKey` or `User` field to `ChatCompletionRequest`. Document the session key format and verify it matches OpenClaw's `buildAgentMainSessionKey` expectations.

2. **SSE connection lifecycle on world selection change is undefined**
   - Problem: When the user changes the selected world via the dropdown, the plan says `@post('/api/mayor-widget/load')` returns patches including a `data-init` for the live SSE connection. But Datastar's `data-init` only fires once — when the element is first processed. If the `#mayor-chat-log` div is replaced via patch, the new `data-init` may or may not fire depending on how Datastar handles morphed elements with `data-init`.
   - Risk: After switching worlds, the SSE connection may still be subscribed to the old world's events (or not connected at all), causing stale/missing messages.
   - Suggestion: Either (a) use `data-on:change` on the world selector to explicitly tear down and re-establish the SSE via a script, (b) use a single SSE endpoint that reads `selected_world_id` from signals and switches internally, or (c) verify with a prototype that patching in a new `data-init` element actually re-establishes the connection. Option (b) is closest to the existing world SSE pattern.

3. **Agent ID verification — `X-OpenClaw-Agent` header confirmed, but `model` field is a simpler alternative**
   - Problem: The plan says "verify this header exists in OpenClaw source" for `X-OpenClaw-Agent`. I verified it works (`http-utils.ts:25-33` accepts both `x-openclaw-agent-id` and `x-openclaw-agent`). However, the plan's `ChatCompletionRequest` struct has a `Model` field that could achieve the same thing more elegantly — OpenClaw resolves agent IDs from model strings like `openclaw/world-abc12345` or `agent:world-abc12345` (`http-utils.ts:36-50`).
   - Risk: Minor — both approaches work. Using the header is fine.
   - Suggestion: Use the `model` field set to `"openclaw/world-" + worldID` instead of a custom header. This is cleaner and follows OpenAI-compatible conventions. If using the header, note that the exact header name is `x-openclaw-agent-id` or `x-openclaw-agent` (lowercase, not `X-OpenClaw-Agent`).

### Concerns (Should Address)

These warrant attention but aren't blockers:

1. **Build mode on lobby page lacks checkpoint context**
   - Observation: The lobby page has no `current_checkpoint_id` signal (that's in `OverlaySignals`, only on the world page). The `POST /api/mayor-widget/build` handler needs a checkpoint to fork from, but the plan doesn't explain how to get it when the user is on the lobby.
   - Suggestion: The build handler should look up the user's last position for the selected world via `WorldManager.GetUserPosition(ctx, userID, worldID)`, or if no position exists, use the world's latest checkpoint. Alternatively, disable Build mode on the lobby page.

2. **Two SSE connections per world page after this change**
   - Observation: The plan removes `EventMayorMessage` from `handleWorldEvent` (events.go:327-341) and adds a separate `GET /api/mayor-widget/events/:worldID` SSE endpoint. This means two concurrent SSE connections on the world page (one for the overlay, one for the widget).
   - Suggestion: This should be fine for correctness and performance (both are lightweight), but document the rationale. Consider whether the widget's SSE should be established lazily (only when panel is open) to save resources.

3. **No user attribution on chat messages**
   - Observation: The `POST /api/mayor-widget/chat` handler stores user messages in `mayor_messages` with `author_type="user"` and publishes to EventBus, but the plan doesn't show extracting the authenticated user from the session to set `author_name` properly.
   - Suggestion: The handler must call `requireUser(c)` and use `user.DiscordUsername` as the `author_name`. This is probably obvious but worth noting since the plan's pseudocode omits it.

4. **`OnProvision` callback must handle nil `discordListener`**
   - Observation: In `main.go`, `discordListener` can be nil if `DISCORD_BOT_TOKEN` is unset or listener creation failed. The `OnProvision` callback must guard against this.
   - Suggestion: The wiring in `main.go` should look like:
     ```go
     if discordListener != nil {
         mayorManager.OnProvision = func(channelID, worldID string) {
             discordListener.RegisterChannel(channelID, worldID)
         }
     }
     ```

5. **Port 18789 hardcoded throughout**
   - Observation: The OpenClaw gateway port `18789` appears in the systemd service, the Go client, and `checkGatewayHealth()`. The plan doesn't make this configurable.
   - Suggestion: Either add an `OPENCLAW_GATEWAY_URL` env var (defaulting to `http://localhost:18789`) or accept the hardcoding as reasonable for a single-server deployment. At minimum, define it as a constant in the new client package.

6. **Degraded state when OpenClaw gateway is down**
   - Observation: The plan acknowledges `checkGatewayHealth()` is informational. But the chat widget has no UX for when OpenClaw is unreachable — the user sends a message and gets... nothing? An error? A timeout?
   - Suggestion: The `StreamChat` client should return a clear error when the gateway is unreachable, and the chat handler should patch a "Mayor is offline" message into the chat log. Consider checking gateway health before attempting to stream.

7. **The research doc proposed `AuthenticatedBase` layout injection; the plan uses per-page injection instead**
   - Observation: The research (`2026-02-16_11-58-54_omnipresent-mayor-assistant.md`) proposed injecting the widget into `layout.Base` or creating `AuthenticatedBase`. The plan instead injects it per-page (lobby.templ, world.templ). Per-page injection is more explicit and avoids showing the widget on pages where it shouldn't appear (admin, create, login), but requires remembering to add it to every new authenticated page.
   - Suggestion: The per-page approach is fine for now since there are only 2 target pages. Document that new authenticated pages should include the widget if appropriate.

### Questions (Need Clarification)

These need answers before proceeding:

1. Is port 18789 OpenClaw's default gateway port, or does it need to be configured in `openclaw.json`? The systemd service and Go client both assume this port, but I don't see it set explicitly in the OpenClaw config in the plan.

2. How should the chat widget behave on page navigation? Since the harness uses full page reloads (not SPA), the widget re-initializes on every page. Will loading 50 messages from `mayor_messages` on each page load cause a noticeable flash? Should there be a loading state?

3. The plan reuses the existing `mayor_messages` table for web chat messages (the same table used for Discord-mirrored messages). This means the research doc's suggestion for a separate `mayor_chat_messages` table was rejected. Is this intentional? Mixing Discord-mirrored messages and web chat messages in the same table means the widget will show Discord messages too — is that desired?

4. What is the expected behavior when a user has NO worlds with mayors? The widget FAB still shows but the dropdown is empty. Should the FAB be hidden entirely, or should it show a prompt to create a world?

### Suggestions (Nice to Have)

Optional improvements:

1. **Lazy SSE for widget**: Only establish the widget's SSE connection when the panel is opened. Use `data-on:click` to set a signal that triggers SSE via conditional `data-init`, rather than connecting immediately on page load.

2. **Typing indicator**: While OpenClaw is streaming a response, show a pulsing dots indicator. The plan mentions a "streaming placeholder template (pulsing cursor)" — this is good, make sure it's implemented.

3. **Keyboard shortcut to toggle**: Add a keyboard shortcut (e.g., backtick or Ctrl+M) to open/close the mayor widget, following the existing postMessage bridge pattern for iframe compatibility.

4. **Consider `model` field for agent routing**: Instead of a custom header, set `model: "openclaw/world-{worldID}"` in the chat completion request. This is more portable and matches OpenAI conventions.

### What's Good

Positive observations worth noting:

- **Accurate current-state analysis**: Every claim about the codebase was verified as correct — the provisioning bug, the `harness-run.sh` OPENCLAW_HOME path, the overlay layout positions, the chat.templ Mayor tab structure. Impressive attention to detail.
- **Correctly identified the provisioning bug**: `provisionAgent()` indeed does not call `BindAgentToDiscord()`. The fix location (in `ProvisionFromWebhook` after `provisionAgent` returns) is the right place since it has access to `discordChannelID`.
- **Well-scoped phases**: Each phase is independently verifiable with clear success criteria. The two-track approach (infra phases 1-2, UI phases 3-5) means value can be delivered incrementally.
- **Good pattern reuse**: The plan correctly identifies and follows existing codebase patterns (dsutil.SignalManager, EventBus pub/sub, Datastar SSE, templ fragment patching).
- **Explicit "What we're NOT doing" section**: Clearly scopes out Discord echo, president setup, onboarding flow changes, and WebSocket RPC. This prevents scope creep.
- **The OpenClaw `/v1/chat/completions` choice is smart**: Using the HTTP API with `messageChannel: "webchat"` avoids Discord round-trips while still getting OpenClaw's full memory/compaction system.
- **Verified `X-OpenClaw-Agent` header exists**: The plan flagged this as needing verification, and it does work (`http-utils.ts:25-33`).
- **The `OnProvision` callback pattern for listener registration is clean**: Avoids tight coupling between `mayor.Manager` and `discord.Listener`.

### Recommended Next Steps

1. **Resolve session key management** (Critical #1): Design the session key format, add `User` and/or `SessionKey` fields to the client, and test that OpenClaw maintains conversation state across multiple requests with the same key.
2. **Prototype the world-switching SSE behavior** (Critical #2): Before building the full widget, test whether Datastar re-establishes `data-init` SSE connections when a parent element is morphed via `PatchElementTempl`. This determines the widget's SSE architecture.
3. **Address build mode checkpoint resolution** (Concern #1): Decide whether build mode is lobby-only, world-only, or both, and implement the checkpoint lookup accordingly.
4. **Add error handling for gateway unavailability** (Concern #6): Design the "mayor offline" UX before implementation.
5. **Proceed with Phase 1** (OpenClaw installation): This is independent of all UI concerns and can start immediately.
