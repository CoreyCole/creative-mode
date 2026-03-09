---
date: 2026-03-09T00:22:17-07:00
reviewer: Claude (Staff Eng Review)
git_commit: d23b4e73e36134887666c4418def657f13a38a3a
branch: feat/agent-primitives
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-03-09_linear-integration-plan.md
status: complete
type: plan_review
---

# Plan Review: Swarm Linear Integration (Phases 0-6 Implementation)

### Summary

Phases 0-6 are well-structured and almost all implemented correctly. However, there is one **runtime panic bug** in `artifactURL` that will crash workflows whenever Linear integration is active and artifacts are persisted. Beyond that, the remaining issues are minor: orphaned `tags` fields, path traversal protection inconsistency, and uncalled activities.

### Critical Issues (Must Address Before Implementation)

1. **`artifactURL` nil pointer dereference will panic at runtime**
   - Problem: `artifactURL(a, researchArtifactID)` at `workflows.go:525` and `workflows.go:784` is called in workflow code where `a` is declared as `var a *SwarmActivities` (nil pointer, the standard Temporal activity-reference pattern). The function accesses `a.config.HarnessURL` at `workflows.go:222`, which dereferences the nil pointer.
   - Risk: Any successful workflow run with a non-empty artifact ID will panic. Temporal recovers the panic as a workflow failure, but the workflow cannot complete. This affects both `ResearchWorkflow` and `CodeChangePlanWorkflow` — the two core workflow types.
   - Suggestion: Pass `HarnessURL` as a field on the workflow input structs (`ResearchWorkflowInput`, `CodeChangePlanWorkflowInput`) instead of reading it from the nil `a` pointer. Then `artifactURL` becomes a pure function: `func artifactURL(harnessURL, artifactID string) string`. Alternatively, move the URL construction into the `LinkArtifactToLinear` activity itself, which has access to the real `SwarmActivities` instance.

### Concerns (Should Address)

1. **Three Linear activities have zero callers**
   - Observation: `FetchLinearTicket` (`activities.go:440`), `CreateLinearFollowup` (`activities.go:504`), and `SearchLinearIssues` (`activities.go:534`) are registered as Temporal activities but never invoked from any workflow. The plan (Phase 4) specifies that `FetchLinearTicket` should be called at workflow start to inject ticket context into agents, and that follow-ups should be created after post-processing. These calls appear to have been deferred along with Phase 3.
   - Suggestion: This is expected if Phase 3 (post-processor) will wire these in. But `FetchLinearTicket` at workflow start (for ticket context injection) was part of Phase 4, not Phase 3. Confirm whether ticket context injection is intentionally deferred or was missed.

2. **`tags` field is orphaned — agents produce it, nothing consumes it**
   - Observation: All agents (except `research-questions.js`) instruct the LLM to output a `tags` array in YAML front matter. But no Go struct has a `Tags` field — `ResearchFinding`, `SynthesizeResult`, `PlannerOutput`, `PlanSynthesizeResult` all lack it. The `unmarshalArtifact` function's first pass with `DisallowUnknownFields()` fails on `tags`, falling back to lenient parsing that silently drops it. Additionally, `tags` is missing from `yamlMultiKeyRe` (`artifact.go:24-30`).
   - Suggestion: Either add `Tags []string \`json:"tags"\`` to all four output structs (and store them in `swarm_artifacts` for future searchability), or remove the `tags` instruction from agent prompts to avoid wasted tokens. If keeping tags, also add `tags` to `yamlMultiKeyRe`.

3. **Path traversal check missing separator suffix**
   - Observation: `handleSwarmArtifactView` at `swarm_dashboard.go:483` uses `strings.HasPrefix(absPath, absRoot)` without appending a path separator to `absRoot`. A theoretical path like `/home/deploy/creative-mode-evil/foo` would pass the check. Other handlers in the same file (e.g., `handleWASMArtifacts`) append `string(os.PathSeparator)` after the root.
   - Suggestion: Change to `strings.HasPrefix(absPath, absRoot+string(os.PathSeparator))` for consistency and defense-in-depth, even though the current DB-sourced paths make this unexploitable.

4. **Hardcoded `linear-cli` binary path**
   - Observation: `main.go:529` hardcodes `/home/deploy/.cargo/bin/linear-cli`. The setup script uses `${LINEAR_CLI:-linear-cli}` (PATH lookup with env override). If the binary moves or a different machine is used, the hardcoded path silently fails on every Linear operation.
   - Suggestion: Use `exec.LookPath("linear-cli")` with a `LINEAR_CLI` env var override, matching the setup script pattern.

### Questions (Need Clarification)

1. Was `FetchLinearTicket` at workflow start (for ticket context injection into agents) intentionally deferred to Phase 3, or was it missed from Phase 4? The plan specifies it as part of Phase 4 workflow wiring.
2. Should `HARNESS_URL` be required when `LINEAR_TEAM_KEY` is set? Currently, artifact URLs fall back to relative paths if `HARNESS_URL` is unset, which would produce broken links in Linear.
3. Is the `SearchLinearIssues` activity's lack of empty-query guard intentional? Passing an empty string would execute `linear-cli search issues ""`.

### Suggestions (Nice to Have)

1. **Add `tags` column to `swarm_artifacts` table** — if tags are worth generating, they're worth persisting for future search/filtering on the dashboard.
2. **Validate `linear-cli` binary existence at startup** — `NewClient` could check `exec.LookPath` or `os.Stat` and log a warning, rather than silently failing on every call.
3. **Define string constants for `GetSwarmSpanTreeRow` comparisons** — the sqlc CTE limitation means `SpanType` and `Status` are `string` in that struct. Define package-level constants or use `string(sqlc.SpanTypeAgent)` in the 12+ view-layer comparison sites to avoid typo risk.

### What's Good

- **Fire-and-forget pattern is solid**: `runLinearActivity` correctly absorbs errors without failing workflows, while still logging warnings for observability.
- **isChild guard is well-designed**: Prevents duplicate Linear operations when `ResearchWorkflow` runs as a child of `CodeChangePlanWorkflow`, with clean label handoff between parent and child.
- **No command injection risk**: The `linear` package uses `exec.CommandContext` with argv slices throughout — no shell interpretation possible.
- **Temporal determinism is clean**: All non-deterministic operations properly wrapped in `workflow.SideEffect` or `workflow.Now`. No `time.Now()` in workflow code.
- **Type-safe enums are well-applied**: The `swarmorch/` package consistently uses typed constants for all DB operations. The sqlc column overrides are comprehensive.
- **Agent prompt improvements are consistent**: All five HumanLayer-inspired changes are present and correctly structured across all six agent files.
- **Nil-safety guards are consistent**: Every Linear activity checks both `LinearClient == nil` and `ticketID == ""` before proceeding.
- **Artifact serving has proper auth**: Behind `SessionMiddleware` + `ApprovedMiddleware`, with path traversal protection.
- **No XSS risk**: All templ output is auto-escaped, artifact files served raw via `c.File()`.

### Recommended Next Steps

1. **Fix the `artifactURL` nil pointer bug** — this is a runtime crash, must be fixed before any testing with Linear integration active.
2. **Decide on `tags` strategy** — either add Go struct fields + DB column, or remove from agent prompts.
3. **Fix path traversal separator** — one-line change for defense-in-depth.
4. **Then proceed to Phase 3** (post-processor agent) which will wire in the currently-uncalled `FetchLinearTicket`, `CreateLinearFollowup`, and `SearchLinearIssues` activities.
