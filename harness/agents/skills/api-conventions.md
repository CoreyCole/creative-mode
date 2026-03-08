---
name: api-conventions
description: Echo routes, auth middleware chain, SSE patterns, request handling, Datastar ReadSignals
tags: [echo, routes, auth, middleware, sse, api, datastar]
last_verified: 2026-03-08
---

# API Conventions

## Auth Middleware Chain

```
SessionMiddleware(db) -> sets c.Get("user") as *sqlc.User
  ApprovedMiddleware() -> rejects role="pending" users
    AdminMiddleware()  -> requires role="admin"
```

Extract user: `user, ok := c.Get("user").(*sqlc.User)`

## Hook Secret Auth

`hookSecretMiddleware()` (no args) validates `X-Hook-Secret` header against `CM_HOOK_SECRET` env var. Applied as inline middleware on individual routes, NOT as a group.

## Mayor API Auth

`mayorAuthMiddleware` validates `X-Mayor-Secret` header against per-world secrets in DB, sets `c.Get("mayor_world")`.

## Route Patterns

```go
// Public
e.GET("/auth/discord/login", s.handleDiscordLogin)

// Hook-authenticated (inline middleware, not groups)
e.POST("/api/claude-event", s.handleClaudeEvent, hookSecretMiddleware())
e.POST("/api/world-hatched", s.handleWorldHatched, hookSecretMiddleware())

// Session-only (authed)
authed := e.Group("", auth.SessionMiddleware(s.DB))

// Approved users (authed + approved)
approved := authed.Group("", auth.ApprovedMiddleware())
w := approved.Group("/world")
w.GET("/:worldID", s.handleWorldView)

// Admin only (authed + admin, NOT off approved)
adminGroup := authed.Group("/admin", auth.AdminMiddleware())
adminGroup.GET("", s.handleAdmin)
```

## Reading Signals from Requests

```go
type ChatSignals struct {
    ChatText string `json:"chatText"`
}
var signals ChatSignals
if err := datastar.ReadSignals(r, &signals); err != nil { ... }
```

## SSE Response Pattern

```go
sse := datastar.NewSSE(w, r)
sse.PatchElementTempl(views.Component(data), datastar.WithSelectorID("target"))
sse.MarshalAndPatchSignals(map[string]any{"field": ""}) // clear input
```
