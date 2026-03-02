---
ticket: CRE-10
phase: research
result: success
session: ab2281f1
workflow: c480b949
timestamp: 2026-03-02T06:08:27Z
---

## BLUF
Analyzed all 4 pkg/ packages (1,642 lines, 15 files) for testability — 20+ pure functions can be tested immediately with no mocking, `mayorchat.ConversationManager` already has interfaces for mock-based testing, and `worldchannel`/`imagegen` need interface extraction for full coverage.

## What Was Done
- Identified all 15 Go source files across 4 packages in `pkg/` with zero existing tests
- Analyzed every public function's testability characteristics (dependencies, I/O, interfaces)
- Catalogued existing test patterns from 18 test files in `harness/internal/`
- Identified 20+ immediately testable pure functions across all packages
- Mapped mock infrastructure needs: existing interfaces (MessageStore, ImageStore), needed interfaces (DiscordSession, GenerativeModel)
- Created prioritized 3-phase test implementation plan
- Wrote full research document to `thoughts/swarm/research/2026-03-02_06-08-27_CRE-10_testing-strategy-untested-packages.md`
- Posted RESEARCH comment to Linear ticket CRE-10

## What Was NOT Done
- No test files written (research phase only)
- No interface extraction or production code refactoring
- No investigation of `harness/internal/` packages that also lack tests (server, auth, world, claude, builder, mayor, president, discord, gemini, tmux, logging)

## Key Files
- `thoughts/swarm/research/2026-03-02_06-08-27_CRE-10_testing-strategy-untested-packages.md` — full research document
- `pkg/worldchannel/` — 6 files, 534 lines, needs DiscordSession interface for full testing
- `pkg/mayorchat/` — 7 files, 809 lines, best testability (already has MessageStore/ImageStore interfaces)
- `pkg/mayorchat/conversation.go` — ConversationManager with goroutine leak concern (cleanupLoop has no stop channel)
- `pkg/imagegen/client.go` — 109 lines, needs GenerativeModel interface extraction
- `pkg/markdown/renderer.go` — 190 lines, fully testable with no mocking
- `harness/internal/swarmorch/manager_test.go` — reference for test patterns (table-driven, t.Parallel, hand-rolled mocks)

## Gotchas
- `NewConversationManager` spawns an unstoppable goroutine (`cleanupLoop`) — tests will leak goroutines
- `var Model` and `var ScriptedResponses` in mayorchat are mutable global state — test interference risk
- `imagegen.Client` uses chained field access (`c.client.Models.GenerateContent`) making interface extraction non-trivial
- `downloadOnboardingJSON` creates its own `http.Client` internally — needs httptest, not interface injection
- Codebase uses raw `testing.T` assertions, NOT testify — only exception is Temporal workflow tests

## Next Steps
- Phase 1 (code_plan): Design test files for all pure functions across all 4 packages (~10 test files, ~30 test functions)
- Phase 2 (code_plan): Design mock implementations for MessageStore/ImageStore to test ConversationManager
- Phase 3 (separate ticket): Interface extraction for worldchannel.Client and imagegen.Client
