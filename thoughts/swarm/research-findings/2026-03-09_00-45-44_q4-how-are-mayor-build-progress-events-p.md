---
question: How are mayor build progress/events propagated to user-facing surfaces (Discord messages, EventBus, SSE streams, and mayor dashboard UI), and what payload shapes are used?
confidence: high
filesReferenced:
  - harness/internal/events/types.go
  - harness/internal/server/server.go
  - harness/internal/server/events.go
  - harness/internal/server/mayor_dashboard.go
  - harness/internal/server/mayor_api.go
  - harness/internal/discord/listener.go
  - harness/views/mayor/dashboard.templ
---

Mayor build/event propagation uses a world-scoped EventBus fanout model, with different producers and consumers for build lifecycle vs mayor chat.

## 1) Event producers and ingress points

- `POST /api/mayor/build` starts a mayor-triggered build and returns immediate API status JSON (`status`, `checkpoint_id`, `world_id`) after calling `Orchestrator.HandlePrompt(...)` (`harness/internal/server/mayor_api.go:113-175`).
- Claude Code hook scripts POST event JSON to `POST /api/claude-event`; `handleClaudeEvent` decodes into `map[string]any`, reads `worldID`, `cpID`, `event`, and publishes the full map to `EventBus.Publish(worldID, event)` (`harness/internal/server/server.go:707-727`).
- Discord channel messages are mirrored by the gateway listener: each message is persisted as `mayor_messages`, then published as an EventBus payload with explicit fields (`event`, `worldID`, `author_type`, `author_name`, `content`, `message_id`) (`harness/internal/discord/listener.go:110-156`).

## 2) Event type vocabulary

World-facing event string constants include:

- `build.started`, `build.completed`, `build.failed`
- `claude.tool_use.pre`, `claude.session_stopped`, `claude.rate_limited`
- `execute_script`, `mayor.message` (`harness/internal/events/types.go:3-16`).

## 3) EventBus propagation shape

- Build/Claude hook events: arbitrary JSON map from hook body is forwarded unchanged to world channel subscribers (`harness/internal/server/server.go:709-727`).
- Discord-mirrored mayor message events: normalized payload shape:
  - `event: "mayor.message"`
  - `worldID: <string>`
  - `author_type: <sqlc.AuthorType>`
  - `author_name: <string>`
  - `content: <string>`
  - `message_id: <short id>` (`harness/internal/discord/listener.go:145-153`).

## 4) SSE consumers and user-facing updates

### World page SSE handling (chat/build surface)

`events.go` switches on `e["event"]` and patches UI/chat state (`harness/internal/server/events.go:327+ and shown section 260-380`):

- `build.completed`:
  - patches signals: `build_status="ready"`, `current_checkpoint_id=<cpID>`
  - appends build-ready chat notification using `worldID`, `cpID`, `worldName`
  - optional iframe reload script using `serverPort` for 3D or without port for 2D (`harness/internal/server/events.go:260-305`).
- `build.failed`:
  - patches signal `build_status="failed"`
  - appends system chat notification from `error` (`harness/internal/server/events.go:307-319`).
- `claude.rate_limited`:
  - patches signal `build_status="rate_limited"` (`harness/internal/server/events.go:320-325`).
- `mayor.message`:
  - reads `author_type`, `author_name`, `content`
  - builds transient `sqlc.MayorMessage` and appends templ fragment to `#mayor-chat-log` (`harness/internal/server/events.go:326-342`).
- `execute_script`:
  - executes `script` if present (`harness/internal/server/events.go:343-349`).

### Mayor dashboard SSE handling

- Dashboard page initializes SSE via `data-init=@get('/mayor/:worldID/events')` (`harness/views/mayor/dashboard.templ:66-70`).
- SSE endpoint subscribes to world EventBus and, on **any** event receipt, re-queries DB and re-renders three tab containers:
  - `#mayor-builds-tab` via `BuildsTab(builds)`
  - `#mayor-activity-tab` via `ActivityTab(activity)`
  - `#mayor-messages-tab` via `MessagesTab(messages)` (`harness/internal/server/mayor_dashboard.go:73-118`).

This means dashboard does not parse per-event payload fields; it uses event arrival as invalidation trigger and refreshes from persisted tables.

## 5) Discord surface linkage

- Discord is both source and destination in the broader flow:
  - Incoming channel messages are mirrored into DB + EventBus (`harness/internal/discord/listener.go:110-156`).
  - Those mirrored payloads drive live SSE message updates (world page) and dashboard tab refreshes (dashboard SSE).
- Message author typing used in payload/UI comes from `classifyMessage`: non-bot=`user`; bot prefixes `[BUILD`/`[SYSTEM`=`system`; otherwise=`mayor` (`harness/internal/discord/listener.go:159-169`).

## 6) User-visible payloads returned by mayor API

- Build trigger response payload shape is:
  - `{"status":"building","checkpoint_id":...,"world_id":...}` (`harness/internal/server/mayor_api.go:170-174`).
- Mayor status endpoint provides current aggregate state (world metadata, checkpoint summary, optional game server block), used by mayor tools/integrations rather than SSE stream (`harness/internal/server/mayor_api.go:177-224`).
