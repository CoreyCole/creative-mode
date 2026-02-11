---
date: 2026-02-10T20:36:08Z
reviewer: Claude (Staff Eng Review)
git_commit: 0c284dbf012af933bf1cb19527bb16640070348b
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-10-creative-mode-implementation.md
status: complete
type: plan_review
---

# Plan Review: Creative Mode Implementation

### Summary

This is an impressively thorough plan with strong architecture decisions (fork model, JSONL logging, hooks-based observability, iframe isolation). However, it has several critical technical inaccuracies in dependency versions, a platform portability issue that will break the core fork mechanism on macOS, and glosses over WebTransport's TLS certificate requirements which add significant operational complexity.

### Critical Issues (Must Address Before Implementation)

These issues could cause significant problems if not resolved:

1. **`cp -al` Does Not Work on macOS**
   - Problem: The fork mechanism relies on `cp -al` to hardlink the `target/` directory (lines 269, 598-603). On macOS, `cp -al` is not supported - macOS's `cp` does not have an `-l` flag for creating hardlinks, and macOS does not support hardlinking directories at all.
   - Risk: The entire fork-and-build strategy breaks on macOS. Since the plan says "Linux/Mac" shared server (line 47) and the developer appears to be on macOS (Darwin 23.4.0), this will fail immediately during Phase 3.
   - Suggestion: Use `rsync --link-dest` which works cross-platform, or use a platform detection in the fork logic:
     ```bash
     # Linux: cp -al source/target/ dest/target/
     # macOS: rsync -a --link-dest=source/target/ source/target/ dest/target/
     # Or: use cp -c (copy-on-write clone) on APFS which is even better than hardlinks
     ```
     On APFS (modern macOS), `cp -c` creates copy-on-write clones that are instant and share blocks until modified - this is actually superior to hardlinks for this use case.

2. **Bevy Version Is Outdated**
   - Problem: Plan specifies Bevy 0.17 (lines 14, 966), but Bevy 0.18.0 was released January 13, 2026. Starting a new project on an old version means missing improvements and facing eventual migration.
   - Risk: Lightyear version compatibility is tightly coupled to Bevy version. Using wrong Lightyear version will cause compilation failures.
   - Suggestion: Update to Bevy 0.18.0 + Lightyear 0.26.0, or explicitly document why 0.17 is chosen and use Lightyear 0.25.x (not 0.23 as stated).

3. **Lightyear Version Mismatch**
   - Problem: Plan specifies Lightyear 0.23 (line 973) for use with Bevy 0.17. However, Lightyear 0.23 targets an older Bevy version (pre-0.17). The correct pairing is Lightyear 0.25.x for Bevy 0.17, or Lightyear 0.26.0 for Bevy 0.18.
   - Risk: Compilation will fail with incompatible trait implementations and missing types. This is not a "try it and see" issue - wrong Lightyear/Bevy pairings simply do not compile.
   - Suggestion: Verify the exact version pairing from the Lightyear compatibility table:
     - Bevy 0.17 → Lightyear 0.25.x
     - Bevy 0.18 → Lightyear 0.26.0

4. **WebTransport Requires TLS Certificates (Not Mentioned)**
   - Problem: The plan describes WebTransport connections from browser WASM clients to game servers (lines 1021, 1023) but never mentions TLS certificate requirements. WebTransport in browsers *requires* TLS - it cannot work over plain TCP/UDP.
   - Risk: WebTransport will silently fail to connect from the browser. This isn't a "nice to have" - it's a hard requirement of the WebTransport spec. Self-signed certificates for WebTransport have a max validity of 14 days per the spec, requiring automated rotation.
   - Suggestion: Either:
     - (a) Use WebSocket transport instead for local development (simpler, no TLS needed for `localhost`), with WebTransport as a future enhancement for production
     - (b) Add a certificate generation step to the setup process and document the 14-day rotation requirement
     - (c) Use Lightyear's built-in certificate handling (recent versions of aeronet/Lightyear handle self-signed cert generation)
     - Note: The plan mentions "WebSocket fallback" in passing (line 1023) but doesn't actually configure it or explain when it's used. Make this the primary transport for the initial implementation.

5. **Lightyear Now Uses Aeronet Transport Layer**
   - Problem: The plan's Lightyear protocol code (lines 986-1001) uses an older API style. Recent Lightyear versions (0.24+) use the `aeronet` crate as the underlying transport layer. The transport configuration, server setup, and client connection code will look substantially different from what's shown.
   - Risk: The template code in Phase 2 will need significant rework. The protocol registration, transport configuration, and server/client plugin setup have all changed.
   - Suggestion: Reference Lightyear's current examples (especially the `simple_box` or `interest_management` examples) for the correct API. The plan's Phase 2 code snippets should be treated as pseudocode, not copy-pasteable implementations.

6. **tmux `send-keys` Prompt Injection Vulnerability**
   - Problem: The tmux session sends user prompts directly via `send-keys` (line 1303-1304):
     ```go
     cmd := fmt.Sprintf("claude --dangerously-skip-permissions %q", prompt)
     exec.Command("tmux", "send-keys", "-t", s.Name, cmd, "Enter").Run()
     ```
     Even with `%q` quoting, this is fragile. The `--dangerously-skip-permissions` flag means claude code can execute arbitrary shell commands. A malicious prompt could potentially escape the quoting.
   - Risk: Command injection via crafted prompts. Since claude runs with `--dangerously-skip-permissions`, a successful injection gives full shell access.
   - Suggestion: Use claude code's `--print` / stdin mode or write the prompt to a file and pass it via `--input-file`. Avoid passing user-controlled strings through tmux send-keys entirely. Also consider using `claude code --allowedTools` to restrict which tools claude can use rather than `--dangerously-skip-permissions`.

### Concerns (Should Address)

These warrant attention but aren't blockers:

1. **SQLite Concurrent Access Without WAL Mode**
   - Observation: Multiple goroutines will write to SQLite simultaneously (world creation, checkpoint updates, user position tracking, prompt history, session management). The plan doesn't mention enabling WAL mode or setting a busy timeout.
   - Suggestion: Enable WAL mode and set a busy timeout in the database initialization:
     ```go
     db.Exec("PRAGMA journal_mode=WAL")
     db.Exec("PRAGMA busy_timeout=5000")
     ```
     Without this, concurrent writes will frequently hit "database is locked" errors.

2. **No Graceful Shutdown**
   - Observation: The plan doesn't address what happens when the harness server is stopped (Ctrl+C, deploy, crash). Running game servers, tmux sessions, and in-progress builds will be orphaned.
   - Suggestion: Add a shutdown handler that: (a) signals game servers to stop, (b) kills tmux sessions, (c) marks in-progress checkpoints as "interrupted", (d) closes SSE connections gracefully.

3. **Shared CARGO_HOME Race Conditions**
   - Observation: The plan suggests using a shared `CARGO_HOME` (line 602) for all builds. If two checkpoints build simultaneously, cargo may experience lock contention on the registry or package cache.
   - Suggestion: Use `CARGO_HOME` for the shared registry/cache but ensure that concurrent `cargo build` invocations don't conflict. Cargo uses file locks internally, so this may work, but it should be tested under concurrent load. Alternatively, use `sccache` which is designed for this.

4. **No Input Sanitization on World/Checkpoint Names**
   - Observation: World names and prompts become directory paths and tmux session names. The plan doesn't validate these inputs.
   - Suggestion: Use the UUID-based IDs (which are safe) for all filesystem and tmux operations. Sanitize display names. The tmux session naming (`cm-{worldID}-{cpID}`) looks safe if IDs are UUIDs, but verify.

5. **README Says "Hot-Reloads" but Plan Says "No Hot Module Reloading"**
   - Observation: README line 21 says "the browser hot-reloads" but the plan (line 40) explicitly excludes hot module reloading. The actual mechanism is a full iframe reload.
   - Suggestion: Clarify README wording. "Hot-reload" implies preserving state, which doesn't happen here. Say "the browser reloads with your changes" instead.

6. **No Build Timeout**
   - Observation: The build pipeline (Phase 3) has no timeout. A pathological Rust compilation (or claude generating code that triggers exponential compile times via type system abuse) could hang indefinitely.
   - Suggestion: Add a build timeout (e.g., 5 minutes for incremental, 15 minutes for initial). Kill the build process and mark the checkpoint as "failed" if exceeded.

7. **No Rate Limiting on Prompts**
   - Observation: There's no limit on how many prompts a user can submit. Each prompt forks the project directory (1-2GB with target/), spawns a tmux session, runs claude code (API costs), and triggers a build.
   - Suggestion: Add at minimum: (a) one active build per user, (b) a cooldown between prompts, (c) a maximum number of checkpoints per world. This prevents both accidental spam and intentional abuse.

8. **MEMORY.md Per World, Not Per Checkpoint**
   - Observation: The plan says MEMORY.md is per-world (line 161), but each checkpoint gets its own copy of the project directory. If User A forks from checkpoint 3 and User B forks from checkpoint 3, they get the same MEMORY.md. But if User A's fork modifies MEMORY.md, User B's fork won't see those changes.
   - Suggestion: This is actually fine for the fork model (each fork is independent), but the plan's Phase 4 section on "MEMORY.md management" (line 1354) implies a single shared file. Clarify that MEMORY.md diverges at fork points, just like the code.

9. **Claude Code Hook Payload Format Not Verified**
   - Observation: The hook scripts (lines 424-507) parse Claude Code's hook event JSON with `jq`, extracting fields like `.tool_name`, `.input.file_path`, `.input.command`, `.message`. These field names are assumed but not verified against Claude Code's actual hook event schema.
   - Suggestion: Test hook scripts against actual Claude Code hook payloads before finalizing. The field names may differ between hook event types (PreToolUse vs PostToolUse vs Stop vs Notification).

10. **No Health Check for Game Servers**
    - Observation: The plan mentions health-checking game servers (line 1249) but doesn't define how. Game servers are headless Bevy apps - they don't expose an HTTP health endpoint by default.
    - Suggestion: Either: (a) check if the process is still running (simplest), (b) have the game server write periodic heartbeats to its JSONL log, or (c) add a simple TCP health check port.

### Questions (Need Clarification)

1. Is this intended to run only on the developer's local macOS machine, or also on Linux servers? The `cp -al` issue makes this critical to resolve.
2. What is the expected number of concurrent worlds/users? This affects whether SQLite is sufficient and how aggressive the cleanup strategy needs to be.
3. Should users be able to delete worlds/checkpoints? The schema and API don't include deletion endpoints.
4. What happens if claude code's API rate limit is hit mid-session? Is there retry/backoff handling?
5. For the GitHub OAuth flow - is the intent to allow any GitHub user, or should there be an allowlist? The plan has no access control beyond "has a GitHub account."
6. The plan mentions `Trunk.toml` with `wasm_bindgen = "0.2.100"` - has this exact version been verified to exist and work with the chosen Bevy version?

### Suggestions (Nice to Have)

1. **Use APFS clonefile on macOS**: Instead of hardlinks, use `cp -c` which creates instant copy-on-write clones on APFS. This is semantically cleaner than hardlinks and perfectly suited for the fork model.

2. **Add a simple `just verify` command**: A single command that checks all prerequisites are installed (Go, Rust, wasm32 target, tmux, trunk, templ, sqlite3) and reports what's missing. This is more useful than a setup script that might partially fail.

3. **Consider `wasm-opt` size**: Bevy WASM builds can be 15-30MB+ even optimized. Consider adding wasm-opt with `-Oz` flag and brotli compression in the static file server. This significantly improves load times.

4. **Add a "playground" mode without GitHub OAuth**: For local development and demos, requiring GitHub OAuth adds friction. Consider an optional bypass (e.g., `HARNESS_DEV_MODE=true` auto-creates a dev user).

5. **Structured logging from game servers**: The plan wraps game server stdout as JSONL, but Bevy's default logging is unstructured `tracing` output. Consider configuring Bevy's `LogPlugin` with a JSON formatter so the game server output is already structured.

6. **Consider using `claude --output-format stream-json` or similar**: Instead of relying solely on hooks for observability, newer Claude Code versions may support structured output modes that give richer event data.

### What's Good

- **Fork model is excellent**: The tree-based checkpoint system is well-designed. Creating new branches instead of mutating in place elegantly handles concurrent users, rollback, and comparison.
- **JSONL logging throughout**: Consistent structured logging across all components (harness, claude hooks, build, game server) is a strong operational foundation. The ability to `tail -f | jq` any log is great for debugging.
- **Hook-based observability over tmux polling**: Using Claude Code hooks instead of scraping tmux output is the right architectural choice. It's event-driven, structured, and doesn't require fragile terminal parsing.
- **iframe isolation for game builds**: Using iframes for WASM isolation solves real problems (memory cleanup, build isolation, input scoping) without any custom teardown logic.
- **Thorough scope definition**: The "What We're NOT Doing" section (lines 35-41) and explicit out-of-scope items prevent scope creep.
- **Build cache preservation**: The hardlink strategy for incremental compilation (once the macOS portability is fixed) will keep build times manageable.
- **Claude Code CLAUDE.md and MEMORY.md**: Providing game-specific context files ensures claude code sessions have the right context without requiring the user to repeat instructions.

### Recommended Next Steps

1. **Fix the `cp -al` portability issue** - This is the #1 blocker. Decide on `rsync --link-dest` or `cp -c` (APFS clone) for macOS, or restrict to Linux.
2. **Update Bevy to 0.18 + Lightyear to 0.26.0** - Starting on the latest versions avoids an immediate migration tax.
3. **Prototype WebTransport/WebSocket in a standalone test** - Before building the full template, get a minimal Bevy WASM client connecting to a Lightyear server via WebSocket. This validates the most uncertain technical risk in the plan.
4. **Decide on prompt delivery mechanism** - Replace tmux `send-keys` with a file-based or stdin-based approach to avoid injection risks.
5. **Add WAL mode + busy timeout to SQLite initialization** - One-line fix that prevents a class of concurrency bugs.
6. **Verify Claude Code hook event payload schemas** - Run claude code with hooks and inspect the actual JSON payloads before writing the hook scripts.
