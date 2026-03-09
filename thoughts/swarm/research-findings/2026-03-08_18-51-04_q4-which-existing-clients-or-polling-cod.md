---
question: Which existing clients or polling code currently call harness /health, and what compatibility constraints do they impose on extending the response with Temporal worker status?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - site/internal/monitor/handler.go
  - scripts/debug.sh
  - scripts/vps-bootstrap.sh
---

Current concrete callers of **harness `/health`** in active code are:

- `site/internal/monitor/handler.go:548` — status page backend polls `GET {harnessURL}/health` every 10s from SSE loop (`HandleEvents` sets `harnessTicker` at `:201`, dispatch at `:246`).

  - Compatibility impact: this caller only checks **HTTP reachability and status code** (`status := "ok"` iff `resp.StatusCode == 200` at `:565-568`), and does **not decode JSON body**. So adding fields to JSON body is non-breaking for this client as long as endpoint path and 200 semantics remain stable.

- `scripts/debug.sh:173` — CLI preflight runs `curl -sf "$HARNESS_URL/health" >/dev/null`.

  - Compatibility impact: it ignores response body entirely and only requires successful HTTP status (2xx). Any response-body extension is safe; changing to non-2xx for degraded Temporal worker state would break this workflow by failing all debug commands early.

- `scripts/vps-bootstrap.sh:1069` — bootstrap health check does `curl -s -o /dev/null -w '%{http_code}' http://localhost:18789/health` and logs success based on numeric code (`:1071`).

  - Compatibility impact: body is discarded; only code matters. Extending JSON is safe. Tightening status codes could affect provisioning diagnostics/automation expectations.

Server behavior currently imposed on compatibility:

- `harness/internal/server/server.go:141` registers public `GET /health`.
- `harness/internal/server/server.go:339-341` returns fixed `200` JSON map `{"status":"ok"}`.
  - This establishes a de facto contract for existing tooling: **simple always-OK liveness probe**.

## Practical constraints for adding Temporal worker status

Given existing callers, safest extension strategy is:

1. Preserve route and baseline liveness semantics (`/health` still available, still 200 when process is up).
1. Preserve existing key `status: "ok"` for compatibility with any undocumented consumers that may string-match it.
1. Add Temporal details as **additive fields** (e.g., `temporal: {worker_running: ..., last_error: ...}`) rather than changing root shape/type.
1. Avoid making Temporal degradation flip `/health` to non-200 unless you intentionally want debug/bootstrap/monitor to treat harness as down.

In short: current known clients impose **HTTP-status compatibility**, not strict JSON-schema parsing; additive JSON fields for Temporal worker status are compatible if 200/`status:"ok"` behavior is retained for liveness.
