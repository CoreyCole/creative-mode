---
question: What liveness and failure-handling rules does the protocol implement (heartbeat cadence, stdin close behavior, timeouts, malformed JSON, and crash detection), and where are they enforced on each side?
confidence: high
filesReferenced:
  - harness/internal/swarmorch/agent.go
  - harness/agents/lib/protocol.js
---

- **Heartbeat cadence and meaning**

  - **JS agent side emits explicit heartbeats every 15s** via `startHeartbeat()` interval (`harness/agents/lib/protocol.js:40-49`), writing `{type:"heartbeat"}` JSONL records to stdout (`:45`). Timer is `unref()`'d so heartbeats do not keep a dying/idle process alive (`:48`).
  - **Go orchestrator side treats any stdout line as liveness heartbeat**, not just `type:"heartbeat"`: in `readAgentLoop`, it invokes workflow heartbeat callback on every scanned line (`harness/internal/swarmorch/agent.go:276-279`), then parses message type. `type:"heartbeat"` is explicitly a no-op because liveness was already recorded at line-read time (`:296-298`).

- **stdin close behavior (orchestrator death / pipe closure)**

  - **JS side explicitly detects stdin closure**: `initProtocol()` tracks `rl.on('close')` and sets `stdinClosed=true` (`harness/agents/lib/protocol.js:7-10`).
  - `readLine()` rejects immediately if already closed (`:15-18`), or rejects when `close` fires while waiting (`:23-27`), with message `stdin closed — orchestrator exited`.
  - This means the agent’s blocking input wait fails fast when Go exits/closes pipe, rather than hanging.
  - **Go side** does not add a separate stdin-close handler in this file; closure propagates naturally from process/pipes and write errors (e.g., `write answer`) are surfaced as loop errors (`harness/internal/swarmorch/agent.go:504-506`).

- **Timeout rules**

  - **Go read loop itself has no protocol idle timeout** in `readAgentLoop`; it blocks on scanner until output/EOF/error (`harness/internal/swarmorch/agent.go:267-318`). Liveness for long silent periods is expected to come from JS heartbeat lines.
  - **Go uses timeout for internal grep helper only** (question-answer context lookup), not protocol transport: `grepFiles` runs with `context.WithTimeout(..., 5s)` (`harness/internal/swarmorch/agent.go:557-565`).
  - No additional JSONL question/answer timeout enforcement is present in these protocol files.

- **Malformed JSON handling**

  - **Go side (reading JS stdout): malformed JSONL is non-fatal and skipped.** `json.Unmarshal(scanner.Bytes(), &msg)` failure logs warning with truncated raw line and continues loop (`harness/internal/swarmorch/agent.go:281-287`). This is resilience against noisy/bad lines.
  - **JS side (reading Go stdin): malformed JSON is fatal to that read promise unless caller catches.** `readLine()` does `resolve(JSON.parse(line))` without try/catch (`harness/agents/lib/protocol.js:20`), so invalid JSON from orchestrator throws from the line handler path and rejects/propagates as runtime error.

- **Crash/failure detection and enforcement**

  - **Go detects agent crash primarily via process wait error** after loop: `cmd.Wait()` non-nil -> marks span failed with stderr and returns error (`harness/internal/swarmorch/agent.go:154-188`).
  - **Go also treats protocol-loop failures as crash/fatal path** (e.g., write failures, tool-call-limit breach, scanner error): `loopErr` triggers span failure with metadata + orphan-child cleanup (`:156-173`).
  - **Orphan span cleanup on crash/failure** is explicit: `failOrphanedChildSpans` fails any still-running child spans of the failed agent span (`:770-806`).
  - **EOF behavior is intentionally non-crash**: scanner EOF returns nil; output is expected from output file after process exits (`:260-263`, `:315`).
  - **Output-file missing after process success is treated as failure signal**: if agent exits but artifact file missing, agent span fails and run errors (`:190-215`).

- **Side-by-side enforcement summary**

  - **JS enforces**: heartbeat emission cadence (15s), stdin-close detection, and immediate failure on invalid incoming JSON parse (`protocol.js:7-27,40-49`).
  - **Go enforces**: per-line liveness heartbeat callback, tolerant handling of malformed outgoing agent JSONL, fatal handling for loop/write/scanner/wait/output-file failures, and crash cleanup of orphan spans (`agent.go:276-318,154-215,770-806`).
