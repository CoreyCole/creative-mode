---
question: What are the exact JSONL message types, fields, and directionality defined for Go ↔ JS communication in the current swarm agent protocol?
confidence: high
filesReferenced:
  - harness/internal/swarmorch/types.go
  - harness/internal/swarmorch/agent.go
---

The Go ↔ JS swarm agent protocol is a **line-delimited JSON (JSONL) stdin/stdout protocol** with two Go→JS message types and three JS→Go message types.

## Directionality and exact message schemas

### Go → JS (written by Go to agent stdin)

1. **`start`** message (`StartMessage`)

   - Defined in `harness/internal/swarmorch/types.go:16`
   - Sent at agent startup in `harness/internal/swarmorch/agent.go:108`
   - Fields:
     - `type` (string, required): always `"start"` (`types.go:18`, `agent.go:109`)
     - `task` (raw JSON, required): marshaled task-specific input payload (`types.go:19`, `agent.go:72-80,110`)
     - `systemPrompt` (string, optional): orchestration/system instruction (`types.go:20`, `agent.go:111`)
     - `config` (object, optional): runtime agent config (`types.go:21`)
       - `model` (string, optional): provider:model format (`types.go:11-12`)

1. **`answer`** message (`AnswerMessage`)

   - Defined in `harness/internal/swarmorch/types.go:24`
   - Sent only as response to JS `question` messages in `harness/internal/swarmorch/agent.go:450-454`
   - Fields:
     - `type` (string, required): always `"answer"` (`types.go:26`, `agent.go:451`)
     - `id` (string, required): echoes the incoming question ID (`types.go:27`, `agent.go:452`)
     - `text` (string, required): answer content (`types.go:28`, `agent.go:453`)

### JS → Go (read by Go from agent stdout)

All parsed through generic `AgentMessage` (`types.go:41`) in the scanner loop (`agent.go:259-268`).

1. **`event`** message

   - Message discriminator: `type: "event"` (`types.go:43`, `agent.go:272-275`)
   - Event-specific fields on same envelope:
     - `event` (enum string): one of:
       - `tool_execution_start`
       - `tool_execution_end`
       - `inference_start`
       - `inference_end` (`types.go:33-37`)
     - `tool` (string, optional): tool name, used esp. on tool start (`types.go:45`, `agent.go:336-343`)
     - `data` (raw JSON, optional): event payload (`types.go:46`)
     - `toolCallID` (string, optional): correlates tool start/end (`types.go:47`, `agent.go:334,347-352`)

1. **`question`** message

   - Message discriminator: `type: "question"` (`types.go:43`, `agent.go:276-278`)
   - Fields used:
     - `id` (string): question correlation ID (`types.go:48`, `agent.go:452`)
     - `text` (string): question body (`types.go:49`, `agent.go:421-427,434`)

1. **`heartbeat`** message

   - Message discriminator: `type: "heartbeat"` (`types.go:43`, `agent.go:279-281`)
   - No additional required fields in Go; explicitly treated as no-op (`agent.go:279-281`).

## Behavioral notes that constrain protocol semantics

- Messages are one JSON object per line via `writeJSONLine` (`agent.go:550-562`) and `bufio.Scanner` line reads (`agent.go:249-252`).
- Unknown `type` values are silently ignored (no default case in outer switch: `agent.go:271-282`).
- Unknown `event` values inside `type=event` are ignored (default case in `handleToolEvent`: `agent.go:406-408`).
- Protocol is asymmetric:
  - Go initiates with exactly one `start`.
  - JS can emit many `event`/`question`/`heartbeat` lines.
  - Go emits `answer` only in reaction to `question`.
