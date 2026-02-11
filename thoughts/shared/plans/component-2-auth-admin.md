# Component 2: Auth + Admin Approval System

## Overview

Implement GitHub OAuth authentication, session management, role-based access control, and admin user approval. Users sign in with GitHub. The first user is auto-promoted to admin. Subsequent users start as `pending` until an admin approves them.

**Dependencies**: Component 1 (harness server + DB layer must exist)
**Depends on this**: Components 6, 7

## Directory Layout

```
harness/internal/auth/
├── auth.go              # OAuth flow handlers (login, callback, logout)
└── middleware.go         # Session auth middleware, role checks
```

## Implementation Details

### GitHub OAuth Flow (`harness/internal/auth/auth.go`)

```go
package auth

type Config struct {
    GitHubClientID     string
    GitHubClientSecret string
    BaseURL            string // e.g., "http://localhost:8080"
}

type Handler struct {
    db     *db.DB
    config *Config
}

func NewHandler(database *db.DB, config *Config) *Handler {
    return &Handler{db: database, config: config}
}
```

**Three endpoints:**

#### 1. `GET /auth/github/login`
- Generate a cryptographically random `state` token (16 bytes, hex-encoded)
- Store `state` in a short-lived cookie (5 minutes, `HttpOnly`, `SameSite=Lax`)
- Redirect to GitHub OAuth authorize URL:
  ```
  https://github.com/login/oauth/authorize?
    client_id={GITHUB_CLIENT_ID}&
    redirect_uri={HARNESS_URL}/auth/github/callback&
    scope=read:user&
    state={random_state}
  ```

#### 2. `GET /auth/github/callback`
- Validate `state` parameter matches the cookie (CSRF protection)
- Exchange `code` for access token: `POST https://github.com/login/oauth/access_token`
  - Send: `client_id`, `client_secret`, `code`
  - Receive: `access_token`
- Fetch user info: `GET https://api.github.com/user` with `Authorization: Bearer {token}`
  - Extract: `id` (github_id), `login` (username), `avatar_url`
- **Do NOT store the GitHub access token** — use it once, discard it
- Determine role:
  - Call `db.CountUsers()` — if 0, this is the first user → role = `"admin"`
  - Otherwise → role = `"pending"`
- Upsert user in DB: `db.UpsertUser(uuid, githubID, username, avatarURL, role)`
  - If user already exists (by github_id), update username/avatar but **do not change role**
- Create session:
  - Generate 32 bytes from `crypto/rand`, hex-encode → 64 char session ID
  - `db.CreateSession(sessionID, userID, time.Now().Add(7*24*time.Hour))`
- Set session cookie:
  - Name: `session`
  - Value: session ID
  - `HttpOnly: true`
  - `SameSite: Lax`
  - `Secure: true` (disable for localhost: check if BaseURL contains "localhost")
  - `Path: /`
  - `MaxAge: 7 * 24 * 3600` (7 days)
- Redirect to `/`

#### 3. `POST /auth/logout`
- Read session cookie
- `db.DeleteSession(sessionID)`
- Clear the session cookie (set MaxAge=-1)
- Redirect to `/auth/github/login`

### Session Middleware (`harness/internal/auth/middleware.go`)

Three middleware functions:

#### `SessionMiddleware(db)`
Applied to all routes that require authentication:
1. Read `session` cookie from request
2. `db.GetSession(sessionID)` — returns nil if not found or expired
3. If invalid/missing: redirect to `/auth/github/login`
4. Load user: `db.GetUserByID(session.UserID)`
5. Update last seen: `db.UpdateLastSeen(user.ID)`
6. Set user in Echo context: `c.Set("user", user)`
7. Call `next(c)`

#### `ApprovedMiddleware()`
Applied after `SessionMiddleware` for routes that require approved users:
1. Get user from context: `c.Get("user").(*User)`
2. If `user.Role == "pending"`: redirect to `/auth/pending`
3. Otherwise: call `next(c)`

#### `AdminMiddleware()`
Applied after `SessionMiddleware` for admin-only routes:
1. Get user from context: `c.Get("user").(*User)`
2. If `user.Role != "admin"`: return 403 Forbidden
3. Otherwise: call `next(c)`

### Route Registration

These routes should be registered on the Echo instance in `main.go` or via a registration function:

```go
// Auth routes (no auth middleware)
e.GET("/auth/github/login", authHandler.HandleLogin)
e.GET("/auth/github/callback", authHandler.HandleCallback)
e.POST("/auth/logout", authHandler.HandleLogout)

// Authenticated but possibly pending
authed := e.Group("", auth.SessionMiddleware(database))
authed.GET("/auth/pending", handlePendingApproval)

// Approved users only
approved := e.Group("", auth.SessionMiddleware(database), auth.ApprovedMiddleware())
// ... other components register on this group

// Admin only
admin := e.Group("/admin", auth.SessionMiddleware(database), auth.AdminMiddleware())
admin.GET("/users", handleAdminUsers)
admin.POST("/users/:userID/approve", handleApproveUser)
admin.POST("/users/:userID/reject", handleRejectUser)
```

### Admin Endpoints

#### `GET /admin/users`
- Query `db.ListUsers()` — all users with their roles
- Render admin user management page (templ template from Component 6, but logic here)
- For now, return JSON list of users

#### `POST /admin/users/:userID/approve`
- `db.UpdateUserRole(userID, "user")`
- Return 200 OK

#### `POST /admin/users/:userID/reject`
- `db.DeleteUser(userID)` — also delete associated sessions
- Return 200 OK

### Pending Approval Page

#### `GET /auth/pending`
- Render a page showing the user's avatar + username
- Message: "Your request to join has been submitted. An admin will approve your access."
- Can poll `/auth/pending/status` to auto-redirect when approved (or use SSE in Component 6)

## Security Measures

- **Session tokens**: 32 bytes from `crypto/rand`, hex-encoded (64 chars). Cryptographically unpredictable.
- **Cookie flags**: `HttpOnly` (no JavaScript access), `SameSite=Lax` (CSRF protection), `Secure` (HTTPS only, disabled for localhost)
- **OAuth state parameter**: Random token in short-lived cookie, validated on callback (prevents CSRF on OAuth flow)
- **No stored access tokens**: GitHub token used once to fetch user info, then discarded
- **Session expiry**: 7 days, checked on every request in middleware
- **Expired session cleanup**: `db.DeleteExpiredSessions()` can be called periodically (e.g., on startup and every hour)

## Interface Contract

This component provides to other components:

1. **`SessionMiddleware(db)`** — extracts authenticated user and sets in Echo context as `c.Get("user")`
2. **`ApprovedMiddleware()`** — rejects pending users
3. **`AdminMiddleware()`** — rejects non-admin users
4. **User in Echo context** — other handlers access via `c.Get("user").(*db.User)`
5. **Route groups** — `approved` and `admin` groups for other components to register on

Other components should:
- Use `c.Get("user").(*db.User)` to access the authenticated user
- Register routes on the `approved` group for user-facing endpoints
- Register routes on the `admin` group for admin endpoints

## Environment Variables

Required:
```
GITHUB_CLIENT_ID          # GitHub OAuth App client ID
GITHUB_CLIENT_SECRET      # GitHub OAuth App client secret
HARNESS_URL=http://localhost:8080  # Base URL for OAuth redirect URI
```

To set up: Create a GitHub OAuth App at https://github.com/settings/developers with:
- Homepage URL: `http://localhost:8080`
- Authorization callback URL: `http://localhost:8080/auth/github/callback`

## Success Criteria

### Automated Verification
- [ ] `cd harness && go build ./...` compiles with auth package
- [ ] `cd harness && go test ./internal/auth/...` passes
- [ ] Session tokens are 64 hex chars (32 bytes)
- [ ] OAuth state cookie is set and validated

### Manual Verification
- [ ] `curl localhost:8080/` redirects to `/auth/github/login` (no session)
- [ ] GitHub OAuth login flow completes and redirects back with session cookie
- [ ] First user auto-promoted to `admin` role
- [ ] Second user starts as `pending`, sees "waiting for approval" page
- [ ] Admin can approve pending users at `/admin/users`
- [ ] Approved user can access lobby and worlds
- [ ] Session cookie is `HttpOnly`, `SameSite=Lax`
- [ ] Logout clears session from DB and cookie
- [ ] Expired sessions are rejected
