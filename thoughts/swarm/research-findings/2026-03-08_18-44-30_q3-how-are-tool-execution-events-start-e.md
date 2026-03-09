---
question: How are tool execution events (start/end, toolCallId, payload fields) emitted in JS and transformed into Go-side spans/observability records?
confidence: high
filesReferenced:
  - harness/agents/lib/agent-factory.js
  - harness/agents/lib/protocol.js
  - harness/internal/swarmorch/types.go
  - harness/internal/swarmorch/agent.go
---

JS emits tool execution events from the agent subscription layer, then Go consumes JSONL and maps events to child spans under the agent span.

- `harness/agents/lib/agent-factory.js:49-63` — JS subscribes to agent lifecycle events. On:

  - `tool_execution_start` => `sendEvent('tool_execution_start', event.toolName, event.args, event.toolCallId)`
  - `tool_execution_end` => `sendEvent('tool_execution_end', event.toolName, event.result, event.toolCallId)` So payload is `args` on start and `result` on end; correlation key is `event.toolCallId`.

- `harness/agents/lib/protocol.js:30-32` — `sendEvent()` serializes one JSONL record to stdout:

  - top-level fields: `type: 'event'`, `event`, `tool`, `data`, `toolCallID`
  - notable casing bridge: JS argument name is `toolCallId`, but emitted JSON key is `toolCallID` (capital `ID`) to match Go tags.

- `harness/internal/swarmorch/types.go:33-47` — Go protocol types define accepted event names and payload envelope:

  - `AgentEventToolStart = "tool_execution_start"`
  - `AgentEventToolEnd = "tool_execution_end"`
  - `AgentMessage` decodes `event`, `tool`, `data` (`json.RawMessage`), and `toolCallID` into `ToolCallID`.

- `harness/internal/swarmorch/agent.go:272-334` — main stdout JSONL loop unmarshals each line into `AgentMessage`, routes `type:"event"` to `handleToolEvent(...)`.

Tool start/end transformation to spans:

- `harness/internal/swarmorch/agent.go:343-375` (`AgentEventToolStart`):

  - increments tool counters/limits,
  - creates a new span id,
  - stores mapping `openToolSpans[msg.ToolCallID] = {spanID, startedAt}`,
  - creates DB span with:
    - `SpanTypeToolCall`
    - `Name = msg.Tool`
    - `InputJSON = msg.Data` (this is JS `event.args`)
    - parent = agent span.

- `harness/internal/swarmorch/agent.go:377-383` (`AgentEventToolEnd`):

  - looks up `openToolSpans[msg.ToolCallID]`,
  - completes that span with `output_json = truncateJSON(msg.Data)` (this is JS `event.result`),
  - removes map entry.
  - If `toolCallID` is missing/unknown, end is effectively ignored (no span completion path runs).

Observability record emission side effects:

- `harness/internal/swarmorch/agent.go:557-595` (`createSpan`) — inserts running span in DB (`CreateSwarmSpan`) and publishes event-bus message:

  - `event: span.started`, with `spanID`, `taskID`, `type`, `name`.

- `harness/internal/swarmorch/agent.go:597-632` (`completeSpan`) — marks span completed in DB (`CompleteSwarmSpan`) with duration/output and publishes:

  - `event: span.completed`, with `spanID`, `durationMs`.

Thus, for tool execution specifically, the canonical data flow is:

1. JS agent event (`tool_execution_start/end`, `toolName`, `args/result`, `toolCallId`)
1. JSONL stdout envelope (`type:event`, `event`, `tool`, `data`, `toolCallID`)
1. Go `AgentMessage` decode
1. `toolCallID`-keyed open-span correlation map
1. DB span create/complete + swarm event-bus notifications for UI/SSE consumers.
