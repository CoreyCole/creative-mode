---
question: How does the Go runner (`runAgent` path) orchestrate JS subprocess lifecycle and protocol IO, including stdin writes, stdout reads, and parsing/dispatch of incoming JSONL messages?
confidence: high
filesReferenced:
  - harness/internal/swarmorch/agent.go
  - harness/internal/swarmorch/types.go
---

`runAgent` is the end-to-end supervisor for a Node/JS agent process, with a JSONL request/response protocol over stdio and artifact handoff via output file.

- **Subprocess setup + lifecycle** (`harness/internal/swarmorch/agent.go:65`):

  - Creates an **agent span** first (DB + event bus) before launching process (`:68-76`).
  - Marshals task input once (`:78-89`), builds command via injected runner with inherited env + `NODE_NO_WARNINGS=1` (`:91-95`).
  - Opens `stdin`/`stdout` pipes and captures `stderr` into buffer for postmortem metadata (`:97-112`).
  - Starts process with `cmd.Start()` (`:114-117`).

- **Stdin write protocol (Go → JS)**:

  - Sends one initial `StartMessage` (`type:"start"`) containing raw `task`, optional `systemPrompt`, optional runtime `config` (`agent.go:120-126`, `types.go:17-22`).
  - Serialization/wire framing is centralized in `writeJSONLine`: JSON marshal + append `\n` + single write (`agent.go:613-625`).
  - During runtime, Go may also send `AnswerMessage` (`type:"answer"`, correlated by question `id`) in response to agent questions (`agent.go:454-463`, `types.go:25-30`).

- **Stdout read loop + JSONL parsing (JS → Go)**:

  - `readAgentLoop` uses `bufio.Scanner` on stdout with 1MB max token to tolerate large JSON payloads (`agent.go:267-269`, const `scannerBufferSize` at `:27`).
  - Each scanned line triggers optional heartbeat callback (Temporal-friendly progress) (`:276-279`).
  - Each line is unmarshaled into `AgentMessage`; malformed JSONL is logged/truncated and skipped (non-fatal) (`:281-287`).
  - Dispatch is by `msg.Type` (`:291-301`, schema in `types.go:42-50`):
    - `"event"` → `handleToolEvent`
    - `"question"` → `handleQuestion`
    - `"heartbeat"` → no-op (line-read already heartbeats)
  - EOF is treated as normal; output is expected in file, not a terminal stdout result message (`:260-263`, `:315`).

- **Event dispatch semantics (`type:"event"`)** (`agent.go:336-434`, event names in `types.go:34-39`):

  - `tool_execution_start`:
    - increments counters (`toolCallCount`, aggregate on params), warns at half-limit, errors at hard limit (`:347-364`).
    - creates tool-call child span keyed by `toolCallID` in `openToolSpans` map (`:366-381`).
  - `tool_execution_end`:
    - closes matching open tool span and removes from map (`:383-389`).
  - `inference_start`:
    - force-closes prior dangling inference span if any (`:392-397`).
    - creates new LLM-call span; model name extracted from `msg.Data` metadata when present (`:398-423`).
  - `inference_end`:
    - increments llm count, parses usage/cost metadata, accumulates agent totals, completes inference span with/without metadata (`:425-451`).
  - Unknown event names are ignored (`:452-453`).

- **Question/answer sub-protocol (`type:"question"`)**:

  - Creates a question span with truncated question text (`agent.go:470-481`).
  - Computes answer via local search helper `answerQuestion` (keyword extraction + grep in skills/harness) (`:483`, `:466-582`).
  - Completes question span with truncated answer payload (`:485-495`).
  - Writes JSONL `AnswerMessage` back to subprocess stdin (`:497-504`).

- **Termination + result handling**:

  - After loop exits, Go always `cmd.Wait()` and inspects stderr (`agent.go:161-163`).
  - If loop or wait fails: marks agent span failed **with aggregate metadata + stderr**, then fails running child spans as orphan cleanup (`:165-193`, `:719-775`, `:804-840`).
  - On success: reads artifact bytes from `outputPath` written by JS (`:196-215`), completes agent span with aggregate usage/tool metadata (`:218-228`), returns raw artifact JSON (`:230-231`).

In short: `runAgent` is a **line-oriented JSONL reactor** over stdout with correlated stdin responses, wrapped in strong lifecycle control (`Start` → loop/dispatch → `Wait`), plus robust observability/error cleanup via hierarchical spans.
