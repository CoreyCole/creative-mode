---
question: How is the harness /health endpoint currently implemented, and what JSON response shape/status-code conventions should be preserved when adding Temporal worker status?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
---

`/health` is registered as a public GET route in server setup and handled by `handleHealth` (`harness/internal/server/server.go:141`, `harness/internal/server/server.go:303`). The handler is intentionally minimal: it always returns HTTP **200 OK** with JSON body shape `{"status":"ok"}` via `c.JSON(http.StatusOK, map[string]string{"status":"ok"})` (`harness/internal/server/server.go:304`).

Conventions to preserve when extending this for Temporal worker status:

- Keep the endpoint path and method unchanged: **GET /health** (`server.go:141`).
- Preserve a JSON object response and the existing top-level status semantics (`status` field currently string-valued) (`server.go:304`).
- Preserve success behavior as **HTTP 200** for healthy state; if introducing degraded/unhealthy states, do so deliberately without breaking existing consumers that may only check `status == "ok"` and/or HTTP 200.
- Keep response simple and machine-readable (current implementation uses a flat JSON map with string values), so any Temporal additions should be additive/backward-compatible (e.g., additional fields rather than replacing `status`).

In short: today it is a static liveness response (`200 + {"status":"ok"}`), so Temporal status should be layered in without breaking that established contract unless coordinated across consumers.
