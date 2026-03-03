---
ticket: CRE-8-4
workflow: e3a5ec12
session: 3aa1795c
timestamp: 2026-03-03T00:50:11Z
---

# Research: Pure Function Tests for pkg/ Packages

## Questions
1. Which pure functions exist across the 4 pkg/ packages (worldchannel, mayorchat, imagegen, markdown)?
2. What are the existing test conventions in the codebase?
3. What test files are needed and what should each test cover?
4. Are there any dependencies or constraints to be aware of?

## Findings

### 1. Existing Test Conventions

No test files exist in `pkg/`. The codebase follows these conventions (from `harness/internal/swarm/classify_test.go`, `harness/internal/swarm/transcript/pricing_test.go`, `harness/internal/swarmorch/hooks_test.go`):

- **`t.Parallel()`** at both top-level test function and inside `t.Run` subtests
- **Table-driven tests** with `[]struct{}` slices, `tt` variable, `t.Run(tt.name, ...)`
- **Standard library assertions only** — `t.Errorf`/`t.Fatalf`, no testify
- **Error format**: `t.Errorf("FuncName() = %v; want %v", got, want)` (semicolon separator)
- **Lowercase descriptive names**: `"research footer"`, `"sonnet basic"`, `"unknown model defaults to sonnet"`

### 2. Package Architecture

Each package is a separate Go module with its own `go.mod`:

| Package | Module Path | External Deps |
|---------|-------------|---------------|
| `pkg/worldchannel/` | `github.com/coreycole/creative-mode/pkg/worldchannel` | `discordgo` |
| `pkg/mayorchat/` | `github.com/coreycole/creative-mode/pkg/mayorchat` | `anthropic-sdk-go` |
| `pkg/imagegen/` | `github.com/coreycole/creative-mode/pkg/imagegen` | `google.golang.org/genai` |
| `pkg/markdown/` | `github.com/coreycole/creative-mode/pkg/markdown` | `chroma/v2`, `gomarkdown` |

### 3. Pure Functions Inventory

**20 pure functions total** across 4 packages:

#### pkg/worldchannel/ (5 functions, 2 unexported)

| Function | File:Line | Exported | Key Logic |
|----------|-----------|----------|-----------|
| `SanitizeChannelName(name string) string` | `sanitize.go:13` | Yes | Lowercase, spaces→hyphens, strip non-alphanumeric, collapse multiple hyphens, trim, max 100 chars, fallback "world" |
| `FormatChannelTopic(mayorName, summary string) string` | `uniqueness.go:11` | Yes | Format "Mayor: {name} \| {summary}", truncate to 1024 |
| `ParseMayorName(topic string) string` | `uniqueness.go:21` | Yes | Extract mayor name from topic format |
| `extractJSON(content, marker string) string` | `onboarding.go:168` | No | Extract JSON from `marker + ```json\n...\n``` ` format |
| `splitConversation(messages []OnboardingMessage) [][]OnboardingMessage` | `onboarding.go:187` | No | Split messages into chunks fitting Discord's 2000-char limit |

#### pkg/mayorchat/ (13 functions)

| Function | File:Line | Key Logic |
|----------|-----------|-----------|
| `CountUserMessages(messages []Message) int` | `message.go:21` | Count messages with Role=="user" |
| `NthUserMessage(messages []Message, n int) string` | `message.go:32` | Return nth user message (0-indexed), trimmed |
| `LastUserMessage(messages []Message) string` | `message.go:46` | Iterate backward, return last user message, trimmed |
| `Truncate(s string, maxLen int) string` | `message.go:56` | Truncate to maxLen bytes, append "..." |
| `IsMayorNameRefusal(input string) bool` | `scripted.go:35` | Check against 15 refusal strings (empty + 14 others) |
| `ScriptedStage(messages []Message) int` | `scripted.go:54` | `CountUserMessages - 1`, clamped ≥0 |
| `ScriptedResponseForStage(stage int, messages []Message) string` | `scripted.go:64` | Return templated response; stage 2 subs world name, stage 3 subs mayor+world |
| `ScriptedExtractWorldInfo(messages []Message, stage int) (string, string, string)` | `scripted.go:83` | Extract mayorName/worldName/worldSummary; refusal→"Mayor" |
| `ParseWorldReady(content string) *WorldReadyInfo` | `stream.go:31` | Parse multi-format WORLD_READY marker (2/3/4/6/7-field) |
| `StripWorldReadyMarker(content string) string` | `stream.go:87` | Remove WORLD_READY marker and everything after |
| `BuildSystemPrompt(username string, detectTemplateType bool) string` | `prompt.go:9` | Build system prompt with username substitution |
| `BuildCoverArtPrompt(worldName, summary string) string` | `cover.go:10` | Build image generation prompt |
| `MimeToExt(mime string) string` | `cover.go:43` | Map MIME type to file extension |

#### pkg/imagegen/ (1 function)

| Function | File:Line | Key Logic |
|----------|-----------|-----------|
| `DetectMIMEType(data []byte) string` | `client.go:98` | Check magic bytes: 0xFF 0xD8→JPEG, "RIFF"→WebP, default→PNG |

#### pkg/markdown/ (1 method)

| Function | File:Line | Key Logic |
|----------|-----------|-----------|
| `(*Renderer).MarkdownBytesToHTML(md []byte) string` | `renderer.go:40` | Render markdown to styled HTML. Requires `NewRenderer()` setup. |

### 4. Test Files Needed

Based on the project plan (Ticket #4) and the analysis:

#### `pkg/worldchannel/sanitize_test.go`
- `TestSanitizeChannelName`: basic conversion, whitespace trim, hyphen collapsing, special chars, empty→"world", max 100 chars, leading/trailing hyphens

#### `pkg/worldchannel/uniqueness_test.go`
- `TestFormatChannelTopic`: normal case, empty inputs, >1024 truncation
- `TestParseMayorName`: normal, no pipe, no prefix, empty, prefix-only
- Round-trip: `ParseMayorName(FormatChannelTopic(name, summary)) == name`

#### `pkg/worldchannel/onboarding_test.go`
- `TestExtractJSON`: marker match, wrong marker, missing code block, missing closing fence
- `TestSplitConversation`: empty, single message, messages exceeding 2000 char budget

#### `pkg/mayorchat/message_test.go`
- `TestCountUserMessages`: empty, all user, mixed roles
- `TestNthUserMessage`: normal, out of range, whitespace trimming
- `TestLastUserMessage`: empty, assistant-only, normal
- `TestTruncate`: short string, exact length, over length, empty

#### `pkg/mayorchat/scripted_test.go`
- `TestIsMayorNameRefusal`: all 15 refusal strings + case insensitivity + valid names
- `TestScriptedStage`: 0, 1, 2, 3+ user messages
- `TestScriptedResponseForStage`: stages 0-3, verify substitutions
- `TestScriptedExtractWorldInfo`: stage<3 (empty), stage≥3 normal, stage≥3 refusal→"Mayor"

#### `pkg/mayorchat/stream_test.go`
- `TestParseWorldReady`: 2/3/4/6/7-field formats, no marker, empty fields→nil, trailing whitespace
- `TestStripWorldReadyMarker`: with marker, without, marker at start

#### `pkg/mayorchat/prompt_test.go`
- `TestBuildSystemPrompt`: verify username substitution, detectTemplateType true/false toggle

#### `pkg/mayorchat/cover_test.go`
- `TestBuildCoverArtPrompt`: verify world name quoted in output
- `TestMimeToExt`: all 4 MIME types + unknown default

#### `pkg/imagegen/client_test.go`
- `TestDetectMIMEType`: JPEG magic, RIFF/WebP magic, PNG default, empty, nil, short data

#### `pkg/markdown/renderer_test.go`
- `TestMarkdownBytesToHTML`: headings (class check), code blocks (wrapper div), links (styled), lists, tables, checkbox stripping

### 5. Constraints and Considerations

#### Module isolation
Each package is a separate Go module. Tests run independently per package: `cd pkg/worldchannel && go test ./...`. No cross-package test dependencies needed.

#### Unexported function testing
`extractJSON` and `splitConversation` in `worldchannel/onboarding.go` are unexported. Tests must be in the `worldchannel` package (not `worldchannel_test`) to access them.

#### Markdown renderer setup
`MarkdownBytesToHTML` is a method on `*Renderer`. Each test needs `NewRenderer()` in setup, which loads the `github-dark` chroma style. This is deterministic and has no external I/O — safe for unit tests.

#### The `Truncate` function operates on byte length
`Truncate` uses `len(s)` which is byte count, not rune count. Tests should include multi-byte UTF-8 strings to document this behavior (even though it's intentional for the use case).

#### No `go test` in check.sh yet
`scripts/check.sh` does NOT currently run `go test` — that's Ticket #5. The tests in this ticket are verified by running `go test ./...` directly in each pkg/ directory.

#### `IsBillingOrOverloadError` excluded
This function in `mayorchat/stream.go` depends on `*anthropic.Error` which requires constructing SDK error values. It's technically pure but tightly coupled to SDK internals. Not worth testing here.

## Architecture Notes

- The pkg/ packages are designed as reusable libraries with separate go.mod files
- Each has clear external dependencies (discordgo, anthropic SDK, genai, chroma/gomarkdown)
- Pure functions are well-separated from I/O-dependent methods
- The project plan calls for ~10 test files; the analysis confirms this is accurate

## Risks and Considerations

- **No breaking risk**: These are purely additive test files with no production code changes
- **Module deps**: Running tests requires modules to be resolved. Each pkg/ already has go.sum files.
- **Markdown rendering assertions**: HTML output from `MarkdownBytesToHTML` is complex. Use `strings.Contains` assertions rather than exact match to avoid brittle tests.
- **`splitConversation` uses `json.Marshal`**: The chunk size depends on JSON encoding overhead. Tests should use realistic `OnboardingMessage` structs.

## Recommendations

1. **Create test files in the same package** (not `_test` package) to access unexported functions in `worldchannel`
2. **Use table-driven tests everywhere** matching existing conventions
3. **Start with highest-value tests**: `ParseWorldReady`, `SanitizeChannelName`, `IsMayorNameRefusal`, `DetectMIMEType`
4. **Use `strings.Contains` for markdown output assertions** rather than exact HTML match
5. **Run `go test ./...` in each pkg/ directory** as verification
6. **Total: 10 test files, ~20 test functions covering 20 pure functions**
