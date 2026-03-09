---
question: What request/authorization checks and world-agent ownership validations are applied before accepting POST /api/mayor/build, and how are failures represented?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/server/mayor_api.go
---

`POST /api/mayor/build` is mounted under the `/api/mayor` Echo group, and that group applies `s.mayorAuthMiddleware` before the handler runs (`harness/internal/server/server.go:150-153`).

Authorization and ownership validation flow:

- `mayorAuthMiddleware` requires `X-Mayor-Secret` request header (`harness/internal/server/mayor_api.go:83-90`).
- If the header is present, it performs DB lookup via `s.DB.GetWorldByMayorSecret(...)` using that secret (`harness/internal/server/mayor_api.go:92-99`).
- Successful lookup yields a specific `sqlc.World`; middleware stores it in context as `"mayor_world"` (`harness/internal/server/mayor_api.go:101-103`).
- `handleMayorBuild` then calls `requireMayorWorld(c)` and only proceeds with that authenticated world (`harness/internal/server/mayor_api.go:117-121`).

This means world-agent ownership is enforced by secret→world binding in the database, then by exclusively using the context world ID (`w.ID`) for checkpoint selection and orchestration (`harness/internal/server/mayor_api.go:135, 160`). No world ID is accepted from client JSON for this endpoint; request body only binds `prompt` (`harness/internal/server/mayor_api.go:123-130`).

Failure representation (all via `echo.NewHTTPError` unless noted):

- Missing `X-Mayor-Secret` → `401 Unauthorized`, message `"missing mayor secret"` (`harness/internal/server/mayor_api.go:86-89`).
- Secret not found/invalid in DB lookup → `403 Forbidden`, message `"invalid mayor secret"` (`harness/internal/server/mayor_api.go:92-99`).
- Context missing authenticated world (`requireMayorWorld` type assertion failure) → `401 Unauthorized`, message `"mayor world not found"` (`harness/internal/server/mayor_api.go:108-110`).
- Invalid request payload or empty prompt → `400 Bad Request`, message `"prompt is required"` (`harness/internal/server/mayor_api.go:123-130`).
- Orchestrator unavailable (`s.Orchestrator == nil`) → `503 Service Unavailable`, message `"orchestrator not configured"` (`harness/internal/server/mayor_api.go:132-137`).
- No checkpoint tree / checkpoint retrieval error or empty set → `404 Not Found`, message `"no checkpoints found for world"` (`harness/internal/server/mayor_api.go:140-143`).
- Build orchestration failure (`HandlePrompt` error) → `500 Internal Server Error`, message prefixed `"build failed: " + err.Error()`; also logged with world ID (`harness/internal/server/mayor_api.go:160-167`).

Success representation:

- On accepted build trigger, handler returns `202 Accepted` JSON: `{ "status": "building", "checkpoint_id": cp.ID, "world_id": w.ID }` (`harness/internal/server/mayor_api.go:170-174`).
