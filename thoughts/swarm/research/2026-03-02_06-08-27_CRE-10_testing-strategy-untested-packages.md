---
ticket: CRE-10
workflow: c480b949
session: ab2281f1
timestamp: 2026-03-02T06:08:27Z
---

# Research: Testing strategy for untested packages

## Questions

1. What packages in `pkg/` lack tests?
2. What are the testability characteristics of each package?
3. What mocking infrastructure is needed?
4. What test patterns does the codebase already use?
5. How should test infrastructure be designed?

## Findings

### Current State: Zero Tests in pkg/

All four packages in `pkg/` (1,642 lines across 15 files) have **zero test files**. The packages are:

| Package | Files | Lines | Purpose |
|---------|-------|-------|---------|
| `worldchannel` | 6 | 534 | Discord channel management (create, permissions, onboarding) |
| `mayorchat` | 7 | 809 | Onboarding conversation engine (scripted + AI chat) |
| `imagegen` | 1 | 109 | Gemini image generation |
| `markdown` | 1 | 190 | Syntax-highlighted markdown → HTML rendering |

### Existing Test Patterns (from harness/internal/)

The codebase has 18 test files with 135 test functions, all in `harness/internal/`. Established conventions:

1. **Table-driven tests** with `t.Run` and `t.Parallel()` on every test/subtest
2. **Raw `testing.T` assertions** (`t.Errorf`/`t.Fatalf`) — no testify except for Temporal tests
3. **`t.TempDir()`** for filesystem isolation
4. **`t.Context()`** (Go 1.24) for context
5. **Hand-rolled mocks** — no code generation (gomock is indirect-only from Temporal)
6. **Package-level tests** (same package, not `_test` package suffix)
7. **`httptest.NewServer`** for HTTP API mocking (used in `linear/client_test.go`)
8. **Executable replacement** for CLI tools (used in `graphite/client_test.go` — temp shell scripts)
9. **Interface at consumption point** — e.g., `DiscordSender` defined where it's used, not centrally

### Testability Analysis by Package

#### 1. `worldchannel` — Testability: MIXED

**Immediately testable (pure functions):**
- `SanitizeChannelName(name string) string` — pure regex/string transforms (`sanitize.go:13`)
- `FormatChannelTopic(mayorName, summary string) string` — pure formatting (`uniqueness.go:11`)
- `ParseMayorName(topic string) string` — pure string parsing (`uniqueness.go:21`)
- `extractJSON(content, marker string) string` — unexported, pure parsing (`onboarding.go:168`)
- `splitConversation(messages []OnboardingMessage) [][]OnboardingMessage` — unexported, pure chunking (`onboarding.go:187`)

**Requires interface extraction:**
All `*Client` methods depend on a concrete `*discordgo.Session` stored at `client.go:20`. There is no interface abstraction. Methods like `CreateChannel`, `GrantAccess`, `RevokeAccess`, `SendWelcomeMessage`, `PinOnboardingData`, `ReadOnboardingData`, `ListExistingMayors` all delegate to `discordgo.Session` methods.

**Mocking strategy:** Extract a `DiscordAPI` interface covering the ~10 session methods used:
- `ChannelMessageSend`, `ChannelMessageSendComplex`, `ChannelMessagePin`, `ChannelMessagesPinned`
- `GuildChannelCreateComplex`, `GuildChannels`, `GuildWithCounts`
- `ChannelPermissionSet`, `ChannelPermissionDelete`
- `User`, `Close`

**HTTP concern:** `downloadOnboardingJSON` (`onboarding.go:150`) creates its own `http.Client` internally — testable via `httptest.NewServer` but not via interface injection.

#### 2. `mayorchat` — Testability: HIGH (mostly)

**Immediately testable (pure functions — 14 functions):**
- `CountUserMessages`, `NthUserMessage`, `LastUserMessage`, `Truncate` (`message.go`)
- `IsMayorNameRefusal`, `ScriptedStage`, `ScriptedResponseForStage`, `ScriptedExtractWorldInfo` (`scripted.go`)
- `ParseWorldReady`, `StripWorldReadyMarker` (`stream.go`)
- `BuildSystemPrompt` (`prompt.go`)
- `BuildCoverArtPrompt`, `MimeToExt` (`cover.go`)

**Already has interfaces:** `ConversationManager` depends on two well-defined interfaces:
- `MessageStore` (`conversation.go:11-16`): `AddMessage`, `GetMessages`, `DeleteOlderThan`, `DeleteUserMessages`
- `ImageStore` (`conversation.go:19-25`): `AddImage`, `GetImages`, `GetImageByID`, `DeleteImages`, `DeleteImagesOlderThan`

Both are easily mockable with hand-rolled structs following the codebase pattern.

**Gotchas:**
1. **Goroutine leak:** `NewConversationManager` spawns `go cm.cleanupLoop()` at line 72 with no stop channel. Every test call leaks a goroutine. Fix: add `context.Context` or `done chan struct{}`.
2. **Filesystem I/O in methods:** `ResetConversation` and `ClearWorldReady` call `os.Remove` directly — testable with `t.TempDir()`.
3. **`BuildAnthropicMessages`** (`stream.go:110-138`) calls `os.ReadFile` for image files — needs real temp files or interface extraction.
4. **Global mutable state:** `var Model anthropic.Model` (`client.go:14`) and `var ScriptedResponses` (`scripted.go:10`) — package-level variables that could cause test interference.

**Filesystem functions (testable with `t.TempDir()`):**
- `SavePendingCoverArt` (`cover.go:21`) — MkdirAll, Glob, Remove, WriteFile

#### 3. `imagegen` — Testability: LOW (except one pure function)

**Immediately testable:**
- `DetectMIMEType(data []byte) string` (`client.go:98`) — pure magic-byte detection

**Requires interface extraction:**
`Client` struct stores concrete `*genai.Client` at `client.go:30`. The `Generate` method accesses `c.client.Models.GenerateContent` (chained field access at line 74), making interface extraction non-trivial — need to abstract both the `Models` field and its `GenerateContent` method.

**Mocking strategy:** Define a `GenerativeModel` interface:
```go
type GenerativeModel interface {
    GenerateContent(ctx context.Context, model string, contents ...*genai.Content) (*genai.GenerateContentResponse, error)
}
```
Then replace `c.client.Models.GenerateContent` with `c.model.GenerateContent`.

#### 4. `markdown` — Testability: HIGH

**Fully testable with no mocking:**
- `NewRenderer()` creates in-process library objects (chroma + gomarkdown) — no network, no API keys
- `MarkdownBytesToHTML(md []byte) string` is a pure input→output transformation

All dependencies are deterministic, in-process libraries. Test strategy: input markdown → assert HTML output. Test edge cases: code blocks with various languages, links, lists, headings, tables, task lists.

### Priority Matrix

| Package | Effort | Value | Priority |
|---------|--------|-------|----------|
| `mayorchat` (pure functions) | Low | High — covers onboarding logic, 14 functions | **P0** |
| `worldchannel` (pure functions) | Low | Medium — sanitization, parsing | **P0** |
| `markdown` | Low | Medium — rendering correctness | **P0** |
| `imagegen` (DetectMIMEType) | Trivial | Low | **P0** |
| `mayorchat` (ConversationManager) | Medium | High — core state management, already has interfaces | **P1** |
| `mayorchat` (filesystem fns) | Medium | Medium — cover art, temp files | **P1** |
| `worldchannel` (Client methods) | High | Medium — requires interface extraction | **P2** |
| `imagegen` (Generate) | High | Low — thin wrapper, rare changes | **P3** |

## Architecture Notes

### No Shared Test Utilities Package

The codebase has no `testutil` or `testhelper` package. Helpers are defined locally in `_test.go` files. For `pkg/` tests, this pattern should continue — each package's tests should be self-contained.

### Interface Design Pattern

The codebase defines interfaces at the consumption point (e.g., `DiscordSender` in `alerts.go`). The `worldchannel` package should follow this: define a `DiscordSession` interface within `worldchannel` covering the methods it uses, and accept it in the `Client` constructor.

### ConversationManager Goroutine Leak

`NewConversationManager` (`conversation.go:72`) spawns `cleanupLoop` with no cancellation. This is both a test concern (goroutine leak) and a production concern (no graceful shutdown). A `context.Context` parameter should be added to `NewConversationManager`.

## Risks and Considerations

1. **Scope creep on interface extraction:** Extracting a `DiscordSession` interface for `worldchannel` touches 6 files and changes the `Client` constructor. This is a refactoring task that should be separate from writing tests for pure functions.

2. **Global mutable state in `mayorchat`:** `var Model` and `var ScriptedResponses` could cause test interference if tests modify them. Tests should not mutate these, or the package should provide setters that restore defaults via `t.Cleanup`.

3. **Goroutine leak:** `NewConversationManager` spawns an unstoppable goroutine. Tests using it should be aware; fixing it requires a production code change.

4. **Anthropic SDK types in `stream.go`:** `IsBillingOrOverloadError` uses `errors.As` with `*anthropic.Error`. Testing depends on whether the SDK exposes the error struct for construction.

## Recommendations

### Phase 1: Pure Function Tests (Low Effort, Immediate Value)

Write tests for all pure functions across all four packages. Estimated 4 test files, ~30 test functions:

1. `pkg/worldchannel/sanitize_test.go` — `SanitizeChannelName` edge cases
2. `pkg/worldchannel/uniqueness_test.go` — `FormatChannelTopic`, `ParseMayorName`
3. `pkg/mayorchat/message_test.go` — `CountUserMessages`, `NthUserMessage`, `LastUserMessage`, `Truncate`
4. `pkg/mayorchat/scripted_test.go` — `IsMayorNameRefusal`, `ScriptedStage`, `ScriptedResponseForStage`, `ScriptedExtractWorldInfo`
5. `pkg/mayorchat/stream_test.go` — `ParseWorldReady`, `StripWorldReadyMarker`
6. `pkg/mayorchat/prompt_test.go` — `BuildSystemPrompt`
7. `pkg/mayorchat/cover_test.go` — `BuildCoverArtPrompt`, `MimeToExt`, `SavePendingCoverArt` (with `t.TempDir()`)
8. `pkg/imagegen/client_test.go` — `DetectMIMEType`
9. `pkg/markdown/renderer_test.go` — `MarkdownBytesToHTML` input/output tests
10. `pkg/worldchannel/onboarding_test.go` — `extractJSON`, `splitConversation` (unexported but testable within package)

### Phase 2: Mock-Based Tests for ConversationManager

Create hand-rolled mocks for `MessageStore` and `ImageStore` interfaces (already defined in `conversation.go`). Test `ConversationManager` methods: `AddMessage`, `GetMessages`, `SetScripted`/`IsScripted`, `CheckRateLimit`, `SetWorldReady`/`GetWorldReady`, `SetHatched`, `ResetConversation` (with `t.TempDir()`).

### Phase 3: Interface Extraction for External Dependencies

Refactor `worldchannel.Client` to accept a `DiscordSession` interface instead of concrete `*discordgo.Session`. This enables testing `CreateChannel`, `GrantAccess`, `SendWelcomeMessage`, etc. with mock implementations. Similarly, extract a `GenerativeModel` interface for `imagegen`. These are refactoring tasks and should be separate tickets.

### Test Conventions to Follow

- Table-driven tests with `t.Run` and `t.Parallel()`
- Raw `testing.T` assertions (no testify)
- `t.TempDir()` for filesystem tests
- `t.Context()` for context
- Hand-rolled mocks (no gomock)
- Package-level tests (same package name)
