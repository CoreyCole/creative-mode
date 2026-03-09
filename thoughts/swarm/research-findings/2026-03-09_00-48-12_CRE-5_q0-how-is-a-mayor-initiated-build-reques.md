---
question: How is a mayor-initiated build request received and authenticated at the harness API (including route, middleware chain, request payload, and secret validation)?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/server/mayor_api.go
  - harness/internal/mayor/skills.go
  - harness/agents/skills/api-conventions.md
  - harness/agents/skills/agent-hierarchy.md
---

Mayor-initiated builds are received on the Mayor API group route and authenticated with a per-world secret header.

- `harness/internal/server/server.go:149-153` — the server registers a grouped Mayor API at `"/api/mayor"`, applies `s.mayorAuthMiddleware` to the whole group, and maps `POST "/build"` to `s.handleMayorBuild`. This makes the full endpoint `POST /api/mayor/build`.
- `harness/internal/server/mayor_api.go:112-114` — handler comment documents the same contract: `POST /api/mayor/build — Auth: X-Mayor-Secret`.

Authentication and middleware chain for this endpoint:

1. Global Echo middleware runs first (`RequestLogger`, `Recover`) from `harness/internal/server/server.go:102-118`.
1. Route group middleware `mayorAuthMiddleware` is then applied for all `/api/mayor/*` routes (`server.go:150-152`).
1. `mayorAuthMiddleware` behavior (`harness/internal/server/mayor_api.go:83-100`):
   - Reads `X-Mayor-Secret` request header (`mayor_api.go:85`).
   - If header is empty: returns `401 Unauthorized` with message `"missing mayor secret"` (`mayor_api.go:86-88`).
   - Looks up world by secret via DB query `GetWorldByMayorSecret` using `sql.NullString{Valid: true}` (`mayor_api.go:91-94`).
   - If lookup fails: returns `403 Forbidden` with `"invalid mayor secret"` (`mayor_api.go:95-97`).
   - On success: stores authenticated world object in context key `"mayor_world"` and calls next handler (`mayor_api.go:98-99`).
1. Inside the handler, `requireMayorWorld` extracts `c.Get("mayor_world")` as `*sqlc.World`; if absent/type mismatch it returns `401 Unauthorized` (`mayor_api.go:103-110`).

Request payload and validation in build handler:

- `harness/internal/server/mayor_api.go:121-127` — request body is bound as JSON struct with one field:
  - `prompt` (string)
- `harness/internal/server/mayor_api.go:125-127` — validation requires bind success and non-empty `prompt`; otherwise `400 Bad Request` with `"prompt is required"`.

What authenticated build request does after validation:

- Uses authenticated world from middleware context (`mayor_api.go:117-120`).
- Requires orchestrator configured or returns `503 Service Unavailable` (`mayor_api.go:129-135`).
- Loads checkpoint tree for that world and selects source checkpoint (latest ready else last) (`mayor_api.go:137-153`).
- Uses world creator (`w.CreatedBy`) as user ID fallback to empty string (`mayor_api.go:155-160`).
- Calls `s.Orchestrator.HandlePrompt(ctx, w.ID, sourceCPID, req.Prompt, userID)` (`mayor_api.go:162`).
- Returns `202 Accepted` JSON on success with `status`, `checkpoint_id`, `world_id` (`mayor_api.go:172-176`).

How mayors are instructed to call it:

- `harness/internal/mayor/skills.go:40` — mayor skill content shows curl usage with:
  - `POST {harnessURL}/api/mayor/build`
  - Header `X-Mayor-Secret: <mayorSecret>`
  - Header `Content-Type: application/json`
  - Body `{"prompt": "YOUR BUILD PROMPT HERE"}`

Supporting docs match implementation:

- `harness/agents/skills/api-conventions.md:26` — documents mayor auth as `X-Mayor-Secret` validated against per-world DB secrets and context key `mayor_world`.
- `harness/agents/skills/agent-hierarchy.md:25,53` — documents mayors triggering builds via `POST /api/mayor/build` in the agent flow.
