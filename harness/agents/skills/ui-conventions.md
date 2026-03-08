---
name: ui-conventions
description: templ components, Datastar colon syntax, fat-morph SSE, signals, PatchElementTempl
tags: [templ, datastar, sse, ui, signals, html, components]
last_verified: 2026-03-08
---

# UI Conventions

## templ Components

- `.templ` extension, compiled with `templ generate`
- Composition via `{ children... }`
- Dynamic URLs: `templ.SafeURL(href)`
- JSON in attributes: `templ.JSONString(data)`

## Datastar Attribute Syntax (v1.0.0-RC.6)

**All plugin suffixes use colon syntax** — NOT dashes:
- `data-on:click` (NOT `data-on-click`)
- `data-bind:chat_text` (NOT `data-bind-chat_text`)
- `data-init` for SSE on load (NOT `data-on-load`)

## Key Attributes

| Attribute | Purpose |
|-----------|---------|
| `data-signals` | Initialize reactive signals (JSON) |
| `data-text` | Bind text content to expression |
| `data-show` | Conditional visibility |
| `data-class` | Conditional CSS classes (object syntax) |
| `data-bind:field` | Two-way input binding |
| `data-on:click` | Click handler |
| `data-init` | Run when element first processed |

## Signal Best Practices

Only use signals for:
1. **User input binding** — `data-bind:chat_text`
2. **Simple UI toggles** — `data-show="$expanded"`

Server state belongs in PatchElementTempl, not signals.

## SSE Pattern

```go
sse := datastar.NewSSE(w, r)
// Render + patch HTML
sse.PatchElementTempl(views.Component(data), datastar.WithSelectorID("id"))
// Only for clearing inputs
sse.MarshalAndPatchSignals(map[string]any{"chat_text": ""})
```

## Actions in Templates

```html
<button data-on:click="@post('/api/chat')">Send</button>
<div data-init="@get('/world/abc/events')"></div>
```

`@post`, `@get` automatically include all signals in the request.

## Templ Helpers (datastar-go)

```go
// Generate data-on:click/data-init expressions in .templ files
datastar.GetSSE("/path")     // GET SSE request
datastar.PostSSE("/path")    // POST SSE request
datastar.PutSSE("/path")     // PUT SSE request
datastar.DeleteSSE("/path")  // DELETE SSE request

// Usage:
<button data-on:click={ datastar.PostSSE("/api/chat") }>Send</button>
<div data-init={ datastar.GetSSE("/world/abc/events") }></div>
```
