---
question: How are interactive question/answer exchanges implemented between JS agents and Go (question IDs, blocking waits, and answer routing)?
confidence: high
filesReferenced:
  - harness/internal/swarmorch/types.go
  - harness/internal/swarmorch/agent.go
  - harness/internal/server/debug.go
---

The JS↔Go interactive Q/A flow for swarm agents is implemented as a **JSONL stdin/stdout protocol** with explicit correlation IDs and synchronous turn-taking on the JS side.

- `harness/internal/swarmorch/types.go:24-49` defines the wire contract:

  - Agent→Go question message: `AgentMessage{type:"question", id, text}`
  - Go→Agent answer message: `AnswerMessage{type:"answer", id, text}`
  - The shared `id` field is the correlation key for routing the answer back to the originating question.

- `harness/internal/swarmorch/agent.go:252-314` shows Go’s stdout read loop:

  - Go continuously scans agent stdout line-by-line, parses each JSONL message, and dispatches by `msg.Type`.
  - On `"question"`, it calls `handleQuestion(...)` inline in the same loop (`agent.go:305-307`), i.e. question handling is part of the main processing path.

- `harness/internal/swarmorch/agent.go:448-489` is the core Q/A exchange:

  - Go creates a `question` span for observability (`SpanTypeQuestion`) with input JSON containing the question text.
  - Go computes an answer via `answerQuestion(repoRoot, msg.Text, ...)`.
  - Go completes the question span with truncated answer output.
  - Go writes `AnswerMessage{Type:"answer", ID: msg.ID, Text: answer}` to the agent stdin.
  - This preserves the original `msg.ID` so the JS agent can match response to request.

- **Blocking-wait behavior**:

  - On the Go side, handling is serialized in the scanner loop; each incoming question is processed before moving on.
  - The stronger “blocking wait” semantics are implied by protocol shape: JS must issue a question with an ID and wait for a matching `answer` message on stdin to continue its own flow. Go does not keep a question-wait map for swarm agents; correlation is delegated to ID echoing (`agent.go:480-485`).

- **Answer routing**:

  - Routing is direct and point-to-point through the subprocess pipes: question arrives on stdout from one agent process, answer goes back to that same process’s stdin.
  - No global broker is used for swarm-agent Q/A; routing correctness comes from per-process stdin/stdout pairing plus ID passthrough.

Related pattern (not swarm-agent protocol, but same architecture idea of ID + pending wait map):

- `harness/internal/server/debug.go:16-127` implements browser debug proxying with:
  - generated short query IDs,
  - `pendingQueries` map from ID→channel,
  - blocking `select` wait with timeout,
  - response handler that routes by query ID and sends to the waiting channel.
- This demonstrates the codebase’s other explicit implementation of “question ID + blocking wait + answer routing,” but for HTTP/SSE/browser debug queries rather than JS swarm subprocess agents.
