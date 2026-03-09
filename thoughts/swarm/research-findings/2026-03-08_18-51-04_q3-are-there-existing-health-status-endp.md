---
question: Are there existing health/status endpoint patterns in the repo (site health checks, world status, monitor polling) that define preferred field naming and semantics for component-level health reporting?
confidence: high
filesReferenced:
  - site/main.go
  - site/internal/monitor/handler.go
  - site/internal/monitor/middleware.go
  - site/internal/webhook/handler.go
  - site/pages/types.go
  - site/pages/status_fragments.templ
---

Yes—there are clear existing patterns, but they are split across **two layers**: a minimal machine health endpoint and richer monitor/status UI polling models.

- `site/main.go:171-179` — routes define the pattern split:
  - `/health` (simple service health)
  - `/status`, `/status/events`, `/status/graph` (monitor UI + SSE + signal-driven updates)

## 1) Canonical simple health endpoint pattern

- `site/internal/webhook/handler.go:37-40` — `HandleHealth` returns JSON `{ "status": "ok" }` with HTTP 200.
- `site/internal/monitor/handler.go:522-557` — harness polling treats health as:
  - transport failure => error
  - non-200 => error
  - 200 => ok
  - no response body fields are inspected; semantics are HTTP + reachability.

**Implication:** for component-level machine endpoints, preferred minimal contract appears to be top-level string `status` with value `"ok"` plus HTTP status code semantics.

## 2) Preferred status vocabulary in monitor components

- `site/pages/status_fragments.templ:5-13` — UI status enum is effectively:
  - `"ok"` (healthy)
  - `"error"` (unhealthy)
  - fallback/default interpreted as `"checking"` (in-progress)
- `site/internal/monitor/handler.go:283-311`, `523-557` — DB and harness cards are patched using exactly those strings.

**Implication:** component cards should emit/consume `ok|error|checking` semantics; avoid alternate labels like `healthy`, `degraded`, etc. unless the UI is extended.

## 3) Field naming conventions for detailed/world status payloads

- `site/internal/monitor/handler.go:560-575` — expected upstream JSON for mayor/world status uses **snake_case JSON keys**:
  - `world_id`, `world_name`, `mayor_name`, `template_type`, `checkpoint_count`, `latest_status`, `game_server_running`, `recent_builds`, plus top-level `worlds`, `timestamp`.
- `site/pages/types.go:38-48` and `site/internal/monitor/handler.go:628-638` — transformed into Go/Presentation struct fields (`WorldID`, `LatestStatus`, `GameRunning`, etc.).
- `site/pages/status_fragments.templ:78-123` — component-level rendering semantics currently use:
  - `GameRunning` as the operational indicator (green/gray dot)
  - counts for `Checkpoints`/`RecentBuilds`
  - `LatestStatus` is carried through but not currently displayed in table cells.

**Implication:** for API payloads feeding status views, repo convention favors snake_case JSON with explicit boolean/action fields (`*_running`) and numeric counters; UI-facing Go structs are CamelCase.

## 4) Monitor polling cadence + semantics pattern

- `site/internal/monitor/handler.go:178-234` — SSE loop with different poll intervals by domain:
  - system/db/stats: every 2s
  - harness health: every 10s
  - world status: every 2s (if `PRESIDENT_SECRET` configured)
- immediate first push on connect (`site/internal/monitor/handler.go:201-209`).
- error handling pattern is **degrade per widget**, not fail whole stream:
  - DB/Harness patch error cards
  - World status patches `WorldStatusError("Harness unreachable"|"HTTP N"|"Invalid response")` (`site/internal/monitor/handler.go:589-626`, `site/pages/status_fragments.templ:242-251`).

## 5) Extra health-related semantics present in instrumentation

- `site/internal/monitor/middleware.go:37-40` — `/health` intentionally excluded from page-view analytics; treated as infra traffic.

## Bottom line

There is an established pattern you can reuse:

1. **Machine health endpoint**: `GET /health` => `200` + `{ "status": "ok" }`.
1. **Component/UI health states**: normalize to `ok`, `error`, and `checking`.
1. **Richer status payloads**: snake_case JSON fields with explicit booleans/counters, then map to CamelCase view models.
1. **Polling semantics**: SSE fan-out with per-component polling intervals and isolated error rendering per card.

So yes, the repo already defines preferred naming and semantics for component-level health reporting; the strongest conventions are `status: "ok"` for simple checks and `ok/error/checking` for monitor cards.
