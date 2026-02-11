# Harness Template Cleanup & Datastar Expression Helpers

## Overview

The harness templates use raw Datastar expression strings inline (e.g., `"$overlay_expanded = true; $unread_count = 0"`). The datastarui reference project has well-designed expression builder utilities (`SignalManager`, `DatastarExpression`, `DataClass`) that make expressions more readable and eliminate magic string signal references. This plan ports those utilities (adapted for flat signals), extracts shared template components, adds FOUC prevention, and fixes several bugs identified in the architecture evaluation.

**Key constraint**: We keep the single EventBus and monolithic server.go. This is intentional — we want a single monolithic UI where the backend controls frontend state via signal patching and HTML fragment patching.

## Current State Analysis

- Templates in `harness/views/world/`, `harness/views/chat/`, `harness/views/lobby/` use raw Datastar expression strings
- No shared expression builder utilities exist
- Duplicate markup exists (chat input bar, load checkpoint button)
- FOUC (Flash of Unstyled Content) occurs on elements with `data-show` that should start hidden
- `evt.preventDefault()` is handled via string concatenation instead of Datastar's `__prevent` modifier
- `game_server.go` has no guard against negative refcount

### Key Discoveries:

- `context/datastarui/utils/` contains well-designed expression utilities we can port
- The datastarui utilities use namespaced signals (`$componentID.property`) but we need flat signals (`$property`)
- `overlay.templ` passes unused `cp` and `user` params to `OverlayTopBar`
- `world.templ` is missing `<meta name="viewport">` tag

## Desired End State

After implementation:

- All Datastar expressions in templates use builder utilities from `dsutil` package
- Shared components (`ChatInputBar`, `LoadCheckpointButton`) eliminate duplicate markup
- FOUC is prevented on initially-hidden elements
- `layout.Head` component is shared between lobby and world pages
- Negative refcount bug in `game_server.go` is fixed
- All templates compile and build cleanly

### Verification:

```bash
cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint
```

## What We're NOT Doing

- Splitting server.go into feature modules (monolithic is intentional)
- Replacing the EventBus with per-handler patterns
- Adding typed event structs for the EventBus
- Adding component ID namespacing to signals (flat signals are correct for our single-page-at-a-time model)

______________________________________________________________________

## Phase 1: Port Datastar Expression Utilities

Create `harness/views/dsutil/` package, adapted from `context/datastarui/utils/` for **flat signals** (no component ID prefix).

### Changes Required:

#### 1. `harness/views/dsutil/signals.go`

**File**: `harness/views/dsutil/signals.go` (NEW)
**Changes**: Adapted from `context/datastarui/utils/signals.go`. Key difference: flat signal references (`$property`) instead of namespaced (`$componentID.property`).

```go
package dsutil

type SignalManager struct {
    Signals     any
    DataSignals string // JSON string for data-signals attribute
}

// Signals creates a SignalManager with flat (non-namespaced) signal references.
func Signals(signalsStruct any) *SignalManager { ... }

// Signal returns "$property"
func (sm *SignalManager) Signal(property string) string

// Toggle, Set, SetString, Equals, NotEquals, DataClass, Conditional,
// ConditionalAction — same API as datastarui version, but with flat $ refs
```

Methods to port:

- `Signal(property)` → `"$property"` (not `"$id.property"`)
- `Toggle(property)` → `"$prop = !$prop"`
- `Set(property, value)` → `"$prop = value"`
- `SetString(property, value)` → `"$prop = 'value'"`
- `Equals(property, value)` → `"$prop === 'value'"`
- `NotEquals(property, value)` → `"$prop !== 'value'"`
- `DataClass(classConditions)` → `"{'class': condition, ...}"`
- `Conditional(property, trueValue, falseValue)`
- `ConditionalAction(condition, property, value)`

#### 2. `harness/views/dsutil/expressions.go`

**File**: `harness/views/dsutil/expressions.go` (NEW)
**Changes**: Copy `context/datastarui/utils/expressions.go` as-is — the `DatastarExpression` builder and `BuildConditional` helper are generic (no namespacing). Skip `FocusCapture` (not needed).

#### 3. `harness/views/dsutil/data_class.go`

**File**: `harness/views/dsutil/data_class.go` (NEW)
**Changes**: Copy `context/datastarui/utils/data_class.go` as-is — the `DataClass` builder is generic.

#### 4. `harness/views/dsutil/sse.go`

**File**: `harness/views/dsutil/sse.go` (NEW)
**Changes**: New helper for SSE connection expressions.

```go
package dsutil

// GetSSENoCancel generates a @get() expression with requestCancellation disabled.
func GetSSENoCancel(url string) string {
    return fmt.Sprintf("@get('%s',{requestCancellation: 'disabled'})", url)
}
```

### Success Criteria:

#### Automated Verification:

- [ ] Build succeeds: `cd /Users/coreycole/cdev/creative-mode/harness && go build ./...`
- [ ] Lint passes: `cd /Users/coreycole/cdev/creative-mode/harness && just lint`
- [ ] `dsutil` package compiles with no errors

______________________________________________________________________

## Phase 2: Template Refactor

### Changes Required:

#### 1. Create Expression Handlers

**File**: `harness/views/world/expressions.go` (NEW)
**Changes**: Overlay-specific expression builders using `dsutil`.

```go
type OverlayExpr struct {
    s *dsutil.SignalManager
}

var overlaySignals = dsutil.Signals(OverlaySignals{})
var OE = NewOverlayExpr(overlaySignals)

func (o *OverlayExpr) Expand() string     // "$overlay_expanded = true; $unread_count = 0"
func (o *OverlayExpr) Minimize() string    // "$overlay_expanded = false"
func (o *OverlayExpr) ToggleTree() string  // "$show_checkpoint_tree = !$show_checkpoint_tree"
func (o *OverlayExpr) BuildStatusDataClass() string  // data-class for build status
```

**File**: `harness/views/chat/expressions.go` (NEW)
**Changes**: Chat tab expression builders.

```go
type ChatExpr struct {
    s *dsutil.SignalManager
}

var signals = dsutil.Signals(struct {
    ActiveTab string `json:"active_tab"`
}{})
var CE = NewChatExpr(signals)

func (c *ChatExpr) SelectTab(tab string) string       // "$active_tab = 'tab'"
func (c *ChatExpr) SelectLineageTab() string           // set tab + call loadLineage()
func (c *ChatExpr) TabActiveClass(tab string) string   // data-class for active tab
```

#### 2. Extract Shared Components

**File**: `harness/views/chat/chat_input.templ` (NEW)
**Changes**: Extract duplicated chat input markup into reusable component.

```go
templ ChatInputBar() {
    <input placeholder="Type a message..." data-bind-chat_text/>
    <button data-on-click={ datastar.PostSSE("/api/chat") }
        data-indicator-fetching
        data-attr-disabled="$fetching">Send</button>
}
```

**File**: `harness/views/shared/load_checkpoint.templ` (NEW)
**Changes**: Extract duplicated loadCheckpoint button into reusable component.

```go
templ LoadCheckpointButton(worldID, cpID, label, class string) {
    <button class={ class }
        data-world-id={ worldID }
        data-cp-id={ cpID }
        data-on-click="loadCheckpoint(evt.target.dataset.worldId, evt.target.dataset.cpId)">
        { label }
    </button>
}
```

#### 3. Remove Unused Parameters from OverlayTopBar

**File**: `harness/views/world/overlay.templ`
**Changes**: Change `OverlayTopBar(w sqlc.World, cp sqlc.Checkpoint, user *sqlc.User)` to `OverlayTopBar(w sqlc.World)` — `cp` and `user` are never referenced in the template body. Update call site.

#### 4. Apply Expression Helpers to Templates

**File**: `harness/views/world/overlay.templ`
**Changes**: Replace raw Datastar expression strings with `OverlayExpr` calls:

- `data-on-click="$overlay_expanded = true; $unread_count = 0"` → `data-on-click={ OE.Expand() }`
- `data-on-click="$show_checkpoint_tree = !$show_checkpoint_tree"` → `data-on-click={ OE.ToggleTree() }`
- `data-on-click="$overlay_expanded = false"` → `data-on-click={ OE.Minimize() }`
- Build status `data-class` → `data-class={ OE.BuildStatusDataClass() }`

**File**: `harness/views/chat/chat.templ`
**Changes**: Replace raw expression strings with `ChatExpr` calls:

- Tab buttons: `data-on-click="$active_tab = 'global'"` → `data-on-click={ CE.SelectTab("global") }`
- Tab classes: `data-class="{'tab-active': $active_tab === 'global'}"` → `data-class={ CE.TabActiveClass("global") }`
- Lineage tab: combined expression → `data-on-click={ CE.SelectLineageTab() }`

**File**: `harness/views/world/world.templ`
**Changes**: Use `dsutil.GetSSENoCancel` and `layout.Head`:

- `fmt.Sprintf("@get('/world/%s/events',{requestCancellation: 'disabled'})", w.ID)` → `dsutil.GetSSENoCancel(...)`
- Use `layout.Head(w.Name)` instead of inline `<head>` block

#### 5. Share `<head>` Between Layouts

**File**: `harness/views/layout/layout.templ`
**Changes**: Extract a `Head` component and `Base` template:

```go
templ Head(title string) {
    <head>
        <meta charset="UTF-8"/>
        <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
        <title>{ title } — Creative Mode</title>
        <script type="module" defer src="/static/datastar.js"></script>
        <link rel="stylesheet" href="/static/styles.css"/>
    </head>
}
```

#### 6. Add FOUC Prevention

Add `style="display: none;"` to elements that start hidden:

- `overlay.templ` — `.overlay-minimized` (overlay_expanded defaults to true)
- `overlay.templ` — `.badge` (unread_count defaults to 0)
- `checkpoint_tree.templ` — `.checkpoint-tree` (show_checkpoint_tree defaults to false)
- `chat.templ` — `#lineage-view` (active_tab defaults to 'global')

#### 7. Fix `evt.preventDefault()` String Concatenation

**File**: `harness/views/lobby/lobby.templ`
**Changes**: Replace `data-on-click={ datastar.PostSSE("/world/create") + "; evt.preventDefault()" }` with `data-on-click__prevent={ datastar.PostSSE("/world/create") }`.

### Success Criteria:

#### Automated Verification:

- [ ] Template generation succeeds: `cd /Users/coreycole/cdev/creative-mode/harness && just generate`
- [ ] Build succeeds: `cd /Users/coreycole/cdev/creative-mode/harness && go build ./...`
- [ ] Lint passes: `cd /Users/coreycole/cdev/creative-mode/harness && just lint`

#### Manual Verification:

- [ ] Lobby page: chat sends messages, "Create World" form submits without page reload
- [ ] World page: overlay expands/minimizes, tree toggles, tabs switch, chat works
- [ ] No FOUC (hidden elements don't flash on page load)
- [ ] Build status classes update correctly during a build cycle
- [ ] Checkpoint load buttons work in both tree view and build notifications

______________________________________________________________________

## Phase 3: Bug Fix — Negative Refcount Guard

### Changes Required:

**File**: `harness/internal/world/game_server.go`
**Changes**: Add guard to prevent decrementing below zero:

```go
func (m *GameServerManager) Disconnect(worldID, cpID string) {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.refCount[key] <= 0 {
        return
    }
    m.refCount[key]--
    if m.refCount[key] == 0 {
        go m.stopAfterDelay(key, gameServerGracePeriod)
    }
}
```

### Success Criteria:

#### Automated Verification:

- [ ] Build succeeds: `cd /Users/coreycole/cdev/creative-mode/harness && go build ./...`

______________________________________________________________________

## Files Modified Summary

| File | Change |
|------|--------|
| `views/dsutil/signals.go` | **NEW** — flat SignalManager |
| `views/dsutil/expressions.go` | **NEW** — expression builder |
| `views/dsutil/data_class.go` | **NEW** — DataClass builder |
| `views/dsutil/sse.go` | **NEW** — GetSSENoCancel helper |
| `views/world/expressions.go` | **NEW** — OverlayExpr handlers |
| `views/chat/expressions.go` | **NEW** — ChatExpr handlers |
| `views/chat/chat_input.templ` | **NEW** — extracted ChatInputBar |
| `views/shared/load_checkpoint.templ` | **NEW** — extracted LoadCheckpointButton |
| `views/layout/layout.templ` | Extract `Head` component |
| `views/world/world.templ` | Use `layout.Head`, `dsutil.GetSSENoCancel` |
| `views/world/overlay.templ` | Use `OverlayExpr`, remove unused params, FOUC fix |
| `views/world/checkpoint_tree.templ` | Use `LoadCheckpointButton`, FOUC fix |
| `views/chat/chat.templ` | Use `ChatExpr`, `ChatInputBar`, `LoadCheckpointButton`, FOUC fix |
| `views/lobby/lobby.templ` | Use `ChatInputBar`, fix `__prevent` |
| `internal/world/game_server.go` | Fix negative refcount guard |

## Verification

```bash
cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint
```

## References

- Reference implementation: `context/datastarui/utils/` (signals.go, expressions.go, data_class.go)
- Architecture evaluation: `thoughts/CoreyCole/research/` (if applicable)
