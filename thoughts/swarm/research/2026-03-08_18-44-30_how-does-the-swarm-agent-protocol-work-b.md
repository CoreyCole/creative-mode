# Research Document

## Protocol shape and directionality

The Go↔JS swarm-agent protocol is JSONL over subprocess stdio, with strict directionality and typed envelopes (`harness/internal/swarmorch/types.go:16-49`, `harness/internal/swarmorch/agent.go:249-301`).

- **Go → JS**

  - `start`: sent once at startup; includes `task` (raw JSON), optional `systemPrompt`, optional `config` (`harness/internal/swarmorch/types.go:16-22`, `harness/internal/swarmorch/agent.go:120-126`).
  - `answer`: sent only in response to JS questions; includes echoed `id` and `text` (`harness/internal/swarmorch/types.go:24-30`, `harness/internal/swarmorch/agent.go:454-463`).

- **JS → Go**

  - `event`: includes `event`, optional `tool`, optional `data`, optional `toolCallID` (`harness/internal/swarmorch/types.go:33-47`, `harness/internal/swarmorch/agent.go:291-301`).
  - `question`: includes `id`, `text` (`harness/internal/swarmorch/types.go:48-49`, `harness/internal/swarmorch/agent.go:305-307`).
  - `heartbeat`: explicit no-op payload from JS perspective, but still a parsed message type in Go (`harness/internal/swarmorch/agent.go:296-298`).

Wire framing is one JSON object per line (`writeJSONLine`) and scanner-based line consumption (`harness/internal/swarmorch/agent.go:613-625`, `harness/internal/swarmorch/agent.go:267-269`).

## Runtime orchestration lifecycle

Go `runAgent` supervises the JS subprocess end-to-end (`harness/internal/swarmorch/agent.go:65-231`): create agent span first, launch process, send `start`, run stdout read/dispatch loop, wait for process exit, then read final output artifact from file.

Key operational details:

- Creates agent span before process launch (`harness/internal/swarmorch/agent.go:68-76`).
- Sets env including `NODE_NO_WARNINGS=1` and captures stderr buffer for diagnostics (`harness/internal/swarmorch/agent.go:91-112`).
- Parses stdout JSONL lines, dispatches by `msg.Type`, skips malformed lines non-fatally (`harness/internal/swarmorch/agent.go:281-301`).
- Treats EOF as normal for protocol loop; final result is expected via output file, not terminal stdout message (`harness/internal/swarmorch/agent.go:260-263`, `harness/internal/swarmorch/agent.go:315`, `harness/internal/swarmorch/agent.go:196-215`).

## Event-to-observability mapping (tools and inference)

JS emits lifecycle events from subscription hooks and Go converts them to child spans under the agent span.

- JS emission:

  - `tool_execution_start` sends `event.args`; `tool_execution_end` sends `event.result`; both include `event.toolCallId` (`harness/agents/lib/agent-factory.js:49-63`).
  - Envelope key is `toolCallID` (capital `ID`) in serialized JSONL (`harness/agents/lib/protocol.js:30-32`).

- Go handling:

  - Decodes `AgentMessage` and routes `type:"event"` to event handler (`harness/internal/swarmorch/agent.go:272-334`).
  - On tool start: increments counters/limits, opens tool span keyed by `toolCallID`, stores `InputJSON=msg.Data` (`harness/internal/swarmorch/agent.go:343-375`).
  - On tool end: closes matching span, writes truncated output JSON, removes map entry (`harness/internal/swarmorch/agent.go:377-383`).
  - On inference start/end: manages single open inference span semantics, usage/cost accumulation, and completion metadata (`harness/internal/swarmorch/agent.go:392-451`).

Span side effects:

- `createSpan` persists running span and emits `span.started` (`harness/internal/swarmorch/agent.go:557-595`).
- `completeSpan` persists completion and emits `span.completed` (`harness/internal/swarmorch/agent.go:597-632`).

## Interactive question/answer flow

Q/A is ID-correlated JSONL turn-taking on the same subprocess pipes.

- JS sends `type:"question"` with `id` and `text`; Go handles inline in main scanner loop (`harness/internal/swarmorch/agent.go:252-314`).
- Go creates a question span, computes answer via `answerQuestion(...)`, completes span, and writes `type:"answer"` echoing the same `id` (`harness/internal/swarmorch/agent.go:448-504`, `harness/internal/swarmorch/types.go:24-30`).
- Routing is per-process stdin/stdout pairing; no global broker or pending-question map in swarm-agent path (`harness/internal/swarmorch/agent.go:480-485`).

## Liveness, robustness, and failure semantics

Liveness and error handling are asymmetric by design.

- **Heartbeat/liveness**

  - JS emits explicit `{type:"heartbeat"}` every 15s; timer is `unref()` (`harness/agents/lib/protocol.js:40-49`).
  - Go records heartbeat progress on **every received stdout line**, not just heartbeat messages (`harness/internal/swarmorch/agent.go:276-279`).

- **Malformed JSON**

  - Go: malformed JS→Go stdout lines are logged/truncated and skipped (non-fatal) (`harness/internal/swarmorch/agent.go:281-287`).
  - JS: JSON parse on Go→JS stdin line has no try/catch in `readLine`, so invalid JSON propagates as failure (`harness/agents/lib/protocol.js:20`).

- **Timeouts and blocking**

  - No protocol idle timeout in Go read loop (`harness/internal/swarmorch/agent.go:267-318`).
  - 5s timeout exists for internal grep helper in answer generation, not transport (`harness/internal/swarmorch/agent.go:557-565`).

- **Crash/failure detection**

  - Go treats loop errors / write failures / wait failures / missing output artifact as run failure and marks spans accordingly, including orphan child-span failure cleanup (`harness/internal/swarmorch/agent.go:154-215`, `harness/internal/swarmorch/agent.go:770-806`).
  - JS detects stdin close and fails pending reads fast with explicit close-path rejection (`harness/agents/lib/protocol.js:7-27`).

## Contradictions, caveats, and gaps

- **Potentially surprising but not contradictory:** explicit `heartbeat` messages exist, yet Go liveness heartbeat is line-driven for all message types; `type:"heartbeat"` is effectively semantically redundant for Go progress signaling (`harness/internal/swarmorch/agent.go:276-279`, `harness/internal/swarmorch/agent.go:296-298`, `harness/agents/lib/protocol.js:40-49`).
- **Data-loss caveat:** tool end without known/matching `toolCallID` results in no span completion path (effectively ignored), so correlation integrity depends on ID continuity (`harness/internal/swarmorch/agent.go:377-383`).
- **Protocol strictness asymmetry:** Go is tolerant to malformed incoming JSONL from JS, JS appears non-tolerant to malformed incoming JSONL from Go (`harness/internal/swarmorch/agent.go:281-287`, `harness/agents/lib/protocol.js:20`).
- **Low-confidence gaps:** none indicated in provided findings; all findings reported high confidence. The only scoped boundary noted is that `harness/internal/server/debug.go` shows a related ID+pending-map pattern but is explicitly not the swarm-agent subprocess protocol.