---
ticket: CRE-8-4
workflow: e3a5ec12
session: 83d49623
version: 1
timestamp: 2026-03-03T02:05:06Z
---

# Plan: Pure Function Tests for pkg/ Packages (v1)

## Goal
Add comprehensive unit tests for all 20 pure functions across the 4 `pkg/` packages (worldchannel, mayorchat, imagegen, markdown). These are purely additive test files — no production code changes.

## Acceptance Criteria
- [ ] 10 test files created, one per source file containing pure functions
- [ ] All 20 pure functions have at least one test function with table-driven subtests
- [ ] Tests follow existing codebase conventions exactly: `t.Parallel()`, table-driven, `t.Errorf("Func() = %v; want %v", got, want)`
- [ ] All tests pass: `cd pkg/<name> && go test ./...` for each package
- [ ] Unexported functions tested via same-package test files (not `_test` package suffix)

## File Inventory

| # | File | Type | ~Lines | Purpose |
|---|------|------|--------|---------|
| 1 | `pkg/worldchannel/sanitize_test.go` | New | 80 | Test SanitizeChannelName |
| 2 | `pkg/worldchannel/uniqueness_test.go` | New | 90 | Test FormatChannelTopic, ParseMayorName, round-trip |
| 3 | `pkg/worldchannel/onboarding_test.go` | New | 100 | Test extractJSON, splitConversation (unexported) |
| 4 | `pkg/mayorchat/message_test.go` | New | 120 | Test CountUserMessages, NthUserMessage, LastUserMessage, Truncate |
| 5 | `pkg/mayorchat/scripted_test.go` | New | 150 | Test IsMayorNameRefusal, ScriptedStage, ScriptedResponseForStage, ScriptedExtractWorldInfo |
| 6 | `pkg/mayorchat/stream_test.go` | New | 130 | Test ParseWorldReady, StripWorldReadyMarker |
| 7 | `pkg/mayorchat/prompt_test.go` | New | 50 | Test BuildSystemPrompt |
| 8 | `pkg/mayorchat/cover_test.go` | New | 60 | Test BuildCoverArtPrompt, MimeToExt |
| 9 | `pkg/imagegen/client_test.go` | New | 60 | Test DetectMIMEType |
| 10 | `pkg/markdown/renderer_test.go` | New | 80 | Test MarkdownBytesToHTML |

## Implementation Steps

### Step 1: pkg/worldchannel/sanitize_test.go

Create `package worldchannel` test file (same package for consistency, even though SanitizeChannelName is exported).

`TestSanitizeChannelName` — table-driven cases:
- `"My Cool World"` → `"my-cool-world"` (basic conversion)
- `"  spaces  "` → `"spaces"` (trim)
- `"a--b---c"` → `"a-b-c"` (collapse multiple hyphens)
- `"Hello!@#$World"` → `"helloworld"` (strip special chars)
- `""` → `"world"` (empty fallback)
- `"!!!"` → `"world"` (all-special fallback)
- `"---hello---"` → `"hello"` (trim leading/trailing hyphens)
- String of 120 `a`s → 100 chars (max length)
- `"a" + 99 hyphens + "b"` → verify trailing hyphens trimmed after truncation

### Step 2: pkg/worldchannel/uniqueness_test.go

Create `package worldchannel` test file.

`TestFormatChannelTopic` — table-driven:
- `("Mayor Name", "A cool world")` → `"Mayor: Mayor Name | A cool world"`
- `("", "")` → `"Mayor:  | "` (empty inputs)
- Long summary (>1024 total) → truncated to 1024

`TestParseMayorName` — table-driven:
- `"Mayor: Bob | Some world"` → `"Bob"`
- `"Mayor: Bob"` → `"Bob"` (no pipe)
- `"Not a topic"` → `""` (no prefix)
- `""` → `""` (empty)
- `"Mayor: "` → `""` (prefix only, rest is empty)
- `"Mayor: Bob | "` → `"Bob"` (pipe with empty summary)

`TestFormatChannelTopicParseMayorNameRoundTrip` — verify `ParseMayorName(FormatChannelTopic(name, summary)) == name` for a few names.

### Step 3: pkg/worldchannel/onboarding_test.go

Create `package worldchannel` test file (MUST be same package — `extractJSON` and `splitConversation` are unexported).

`TestExtractJSON` — table-driven:
- Correct format with marker + ````json\n...\n` `` ` → extracted JSON
- Wrong marker → `""`
- Missing opening code block → `""`
- Missing closing fence → `""`
- Empty content → `""`

`TestSplitConversation` — table-driven:
- `nil` → `nil` (empty)
- Single short message → `[[msg]]` (one chunk)
- Messages exceeding budget → verify multiple chunks, each fits within `discordMaxMessageLen - overhead`

### Step 4: pkg/mayorchat/message_test.go

Create `package mayorchat` test file.

`TestCountUserMessages`:
- Empty slice → 0
- All user messages → count
- Mixed roles → only count "user"
- All assistant → 0

`TestNthUserMessage`:
- Normal (n=0, n=1) → correct message
- Out of range → `""`
- Whitespace trimming → `"  hello  "` → `"hello"`

`TestLastUserMessage`:
- Empty → `""`
- All assistant → `""`
- Normal → last user message content, trimmed

`TestTruncate`:
- Short string → unchanged
- Exact length → unchanged
- Over length → truncated + "..."
- Empty → `""`
- Multi-byte UTF-8 string → truncates on byte boundary (documents behavior)

### Step 5: pkg/mayorchat/scripted_test.go

Create `package mayorchat` test file.

`TestIsMayorNameRefusal`:
- Empty string → `true`
- All 14 refusal strings → `true` each (at least spot-check "mayor", "idk", "skip", "default")
- Case insensitive: `"MAYOR"`, `"Mayor"` → `true`
- Valid name: `"Bob"` → `false`
- Sentence: `"I want the mayor to be Bob"` → `false`

`TestScriptedStage`:
- 0 user messages → 0 (clamped)
- 1 user message → 0
- 2 user messages → 1
- 4 user messages → 3

`TestScriptedResponseForStage`:
- Stage 0 → ScriptedResponses[0] verbatim
- Stage 1 → ScriptedResponses[1] verbatim
- Stage 2 → contains world name from 3rd user message (`NthUserMessage(msgs, 2)`)
- Stage 3 → contains both mayor name (last user msg) and world name
- Stage >= len(ScriptedResponses) → clamped to last stage

`TestScriptedExtractWorldInfo`:
- Stage < 3 → all empty strings
- Stage 3 with valid names → correct mayor, world, summary
- Stage 3 with refusal name → mayorName = "Mayor"
- Summary truncated to 100 chars

### Step 6: pkg/mayorchat/stream_test.go

Create `package mayorchat` test file.

`TestParseWorldReady`:
- 3-field: `"WORLD_READY|Bob|Duskhollow|A dark world"` → correct fields
- 4-field: `"WORLD_READY|Bob|Duskhollow|3d|A dark world"` → correct with template
- 6-field: `"WORLD_READY|Bob|Duskhollow|tree spirit|warm|🌳|A cozy world"` → creature/vibe/emoji
- 7-field: `"WORLD_READY|Bob|Duskhollow|2d|tree spirit|warm|🌳|A cozy world"` → all fields
- 2-field: `"WORLD_READY|Bob|Duskhollow"` → mayor + world only
- No marker → `nil`
- 1-field (just mayor, no world) → `nil` (default case)
- Empty mayor name → `nil` (validation check)
- Empty world name → `nil`
- Text before marker: `"Some chat WORLD_READY|Bob|Duskhollow|summary"` → still parses
- Trailing whitespace stripped

`TestStripWorldReadyMarker`:
- `"Some text WORLD_READY|..."` → `"Some text"`
- `"WORLD_READY|..."` → `""` (marker at start)
- `"No marker here"` → unchanged
- `""` → `""`

### Step 7: pkg/mayorchat/prompt_test.go

Create `package mayorchat` test file.

`TestBuildSystemPrompt`:
- With username "Alice", detectTemplateType=false → contains "Alice", does NOT contain "template_type"
- With username "Bob", detectTemplateType=true → contains "Bob", contains "template_type" and "3d"/"2d"/"boardgame"
- Verify WORLD_READY marker format differs between the two modes (6-field vs 7-field)

### Step 8: pkg/mayorchat/cover_test.go

Create `package mayorchat` test file.

`TestBuildCoverArtPrompt`:
- Normal inputs → contains quoted world name and summary text
- Verify "Cover art" prefix and "No text or logos" suffix

`TestMimeToExt`:
- `"image/jpeg"` → `".jpg"`
- `"image/webp"` → `".webp"`
- `"image/gif"` → `".gif"`
- `"image/png"` → `".png"` (explicit known type)
- `"image/bmp"` → `".png"` (unknown defaults to png)
- `""` → `".png"`

### Step 9: pkg/imagegen/client_test.go

Create `package imagegen` test file.

`TestDetectMIMEType`:
- JPEG magic bytes `{0xFF, 0xD8, 0x00}` → `"image/jpeg"`
- WebP/RIFF magic `{0x52, 0x49, 0x46, 0x46, 0x00}` → `"image/webp"`
- PNG (any data not matching above) → `"image/png"` (default)
- Empty slice → `"image/png"`
- Single byte `{0xFF}` → `"image/png"` (too short for JPEG)
- `nil` → `"image/png"`

### Step 10: pkg/markdown/renderer_test.go

Create `package markdown` test file. Use `strings.Contains` for assertions — HTML output includes chroma classes, wrapper divs, and styling that makes exact matching brittle.

`TestMarkdownBytesToHTML`:
- Heading `# Hello` → contains `myh myh-1` class
- Code block → contains `<div class="my-4 rounded-lg` wrapper
- Link `[text](url)` → contains `class="font-medium text-primary`
- Unordered list → contains `class="list-disc`
- Ordered list → contains `class="list-decimal`
- Table → contains `<div class="table-wrapper">`
- Checkbox stripping: `[ ] unchecked` → text without `[ ]` prefix
- Empty input → empty or minimal output

Each test creates its own `NewRenderer()` — deterministic, no external I/O.

## Verification Checks

### Compilation
1. `cd /home/deploy/creative-mode/pkg/worldchannel && go test -run=^$ ./...` — Compiles worldchannel tests
2. `cd /home/deploy/creative-mode/pkg/mayorchat && go test -run=^$ ./...` — Compiles mayorchat tests
3. `cd /home/deploy/creative-mode/pkg/imagegen && go test -run=^$ ./...` — Compiles imagegen tests
4. `cd /home/deploy/creative-mode/pkg/markdown && go test -run=^$ ./...` — Compiles markdown tests

### Unit Tests
5. `cd /home/deploy/creative-mode/pkg/worldchannel && go test -v ./...` — Run worldchannel tests
6. `cd /home/deploy/creative-mode/pkg/mayorchat && go test -v ./...` — Run mayorchat tests
7. `cd /home/deploy/creative-mode/pkg/imagegen && go test -v ./...` — Run imagegen tests
8. `cd /home/deploy/creative-mode/pkg/markdown && go test -v ./...` — Run markdown tests

### Full Build
9. `just check` — Full project compilation (Go + Rust + WASM)

## Risks
- **Module dependency resolution**: Each `pkg/` has its own `go.mod`/`go.sum`. Running `go test` will resolve deps. If any are stale, `go mod tidy` may be needed per package. Mitigation: check `go.sum` exists for each before writing tests.
- **Markdown HTML output fragility**: HTML output from gomarkdown + chroma is complex. Mitigation: use `strings.Contains` assertions, not exact match.
- **splitConversation chunk size**: Depends on `json.Marshal` overhead. Mitigation: use realistic message structs and verify against `discordMaxMessageLen` constant.
- **Truncate byte vs rune semantics**: `Truncate` uses `len(s)` (bytes). Multi-byte UTF-8 tests document this intentional behavior but may look surprising. Mitigation: add comment in test clarifying byte-length semantics.
