---
ticket: CRE-8-4
phase: research
result: success
session: 3aa1795c
workflow: e3a5ec12
timestamp: 2026-03-03T00:50:11Z
---

## BLUF
Identified 20 pure functions across 4 pkg/ packages requiring ~10 test files. All functions have clear inputs/outputs suitable for table-driven tests following existing codebase conventions.

## What Was Done
- Analyzed all 4 pkg/ packages: worldchannel (5 functions), mayorchat (13 functions), imagegen (1 function), markdown (1 method)
- Verified existing test conventions from harness/internal/ test files
- Cataloged every pure function with signatures, file locations, and test strategies
- Documented constraints: separate go.mod per package, unexported function access, markdown renderer setup
- Wrote comprehensive research document to `thoughts/swarm/research/2026-03-03_00-50-11_CRE-8-4_pkg-pure-function-tests.md`

## What Was NOT Done
- Linear comment posting (CRE-8-4 is a virtual sub-ticket ID that fails Linear's `parseIdentifier` — team "CRE" + number "8-4" can't be parsed)
- No code was written — this is research only

## Key Files
- `pkg/worldchannel/sanitize.go` — SanitizeChannelName (complex edge cases, high value)
- `pkg/worldchannel/uniqueness.go` — FormatChannelTopic, ParseMayorName (round-trip testable)
- `pkg/worldchannel/onboarding.go` — extractJSON, splitConversation (unexported, need same-package tests)
- `pkg/mayorchat/message.go` — CountUserMessages, NthUserMessage, LastUserMessage, Truncate
- `pkg/mayorchat/scripted.go` — IsMayorNameRefusal, ScriptedStage, ScriptedResponseForStage, ScriptedExtractWorldInfo
- `pkg/mayorchat/stream.go` — ParseWorldReady (most complex parser), StripWorldReadyMarker
- `pkg/mayorchat/prompt.go` — BuildSystemPrompt (parameterized output)
- `pkg/mayorchat/cover.go` — BuildCoverArtPrompt, MimeToExt
- `pkg/imagegen/client.go` — DetectMIMEType (magic byte detection)
- `pkg/markdown/renderer.go` — MarkdownBytesToHTML (method on Renderer)

## Gotchas
- Each pkg/ is a separate Go module — run `go test ./...` per package, not from project root
- `extractJSON` and `splitConversation` are unexported — test files must use `package worldchannel` not `package worldchannel_test`
- `Truncate` operates on byte length, not rune count — tests should document this with multi-byte strings
- `MarkdownBytesToHTML` requires `NewRenderer()` setup — use `strings.Contains` for HTML assertions, not exact match
- `IsBillingOrOverloadError` excluded — depends on constructing `*anthropic.Error` values, tightly coupled to SDK
- `check.sh` does NOT run `go test` yet (that's Ticket #5) — verify tests manually

## Next Steps
- Code planning phase should create 10 test files following the function inventory and test strategy in the research doc
- Implementation should follow existing conventions exactly: `t.Parallel()`, table-driven, `t.Errorf("Func() = %v; want %v", got, want)`
- Verification: `cd pkg/<name> && go test ./...` for each package
