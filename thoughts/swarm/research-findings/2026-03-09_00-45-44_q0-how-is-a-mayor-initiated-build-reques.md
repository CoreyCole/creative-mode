---
question: How is a mayor-initiated build request routed from the API entry point through the harness backend, and which handlers/services are responsible at each step?
confidence: high
filesReferenced:
  - harness/internal/server/server.go
  - harness/internal/server/mayor_api.go
  - harness/internal/mayor/skills.go
---

A mayor-initiated build enters via the dedicated mayor API group and then flows through auth, world selection, checkpoint selection, and orchestration.

- `harness/internal/server/server.go:149-154` — route registration creates `mayorGroup := e.Group("/api/mayor")`, applies `s.mayorAuthMiddleware`, and binds `POST /build` to `s.handleMayorBuild`. This is the HTTP entry point and middleware chain for mayor-triggered builds.

- `harness/internal/server/mayor_api.go:81-101` — `mayorAuthMiddleware` validates `X-Mayor-Secret` by querying `s.DB.GetWorldByMayorSecret(...)`. On success it stores the resolved world in Echo context under `"mayor_world"`. This middleware is the world-scoping/auth layer for all mayor API calls.

- `harness/internal/server/mayor_api.go:104-111` — `requireMayorWorld` pulls `*sqlc.World` from context. `handleMayorBuild` uses this to identify which world the mayor is allowed to build for.

- `harness/internal/server/mayor_api.go:114-135` — `handleMayorBuild` parses request JSON (`prompt` required), checks that `s.Orchestrator` exists, and returns `503` if orchestration is unavailable. This handler is the main API controller for mayor build requests.

- `harness/internal/server/mayor_api.go:137-153` — the handler loads checkpoint lineage via `s.DB.GetCheckpointTree(ctx, w.ID)` and selects a source checkpoint: newest `ready` checkpoint if present, otherwise the latest checkpoint. This is the checkpoint selection step before build kickoff.

- `harness/internal/server/mayor_api.go:155-160` — it derives `userID` from `w.CreatedBy` (falls back to empty string) so the build can be attributed to the world creator when available.

- `harness/internal/server/mayor_api.go:162-170` — kickoff happens through `s.Orchestrator.HandlePrompt(ctx, w.ID, sourceCPID, req.Prompt, userID)`. This delegates from API layer to orchestration service, which starts the prompt/build pipeline.

- `harness/internal/server/mayor_api.go:172-179` — on success, handler responds `202 Accepted` with `{status:"building", checkpoint_id, world_id}`; on failure it logs and returns `500`.

Related invocation source:

- `harness/internal/mayor/skills.go:19-43` — mayor workspace skill generation writes a `world-build` skill containing a curl command that calls `POST {harnessURL}/api/mayor/build` with `X-Mayor-Secret` and JSON prompt body. This is how the mayor agent is instructed to trigger the endpoint in practice.

So the routing chain is: **Echo route (`/api/mayor/build`) → `mayorAuthMiddleware` (secret→world lookup) → `handleMayorBuild` (validate + choose checkpoint) → `Orchestrator.HandlePrompt` (start build pipeline)**, with DB lookups (`GetWorldByMayorSecret`, `GetCheckpointTree`) supporting auth and source-checkpoint resolution.
