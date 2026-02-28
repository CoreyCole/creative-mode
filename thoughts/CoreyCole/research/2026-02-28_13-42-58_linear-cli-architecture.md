---
date: 2026-02-28T13:42:58-08:00
researcher: CoreyCole
git_commit: 055cdfdc9a74da5dfdcc3be8ffe65d38343e5a33
branch: main
repository: creative-mode
topic: "Linear CLI Architecture for Creative Mode Work Tracking"
tags: [research, codebase, linear-cli, project-management, rust, cli]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
---

# Research: Linear CLI Architecture for Creative Mode Work Tracking

**Date**: 2026-02-28T13:42:58-08:00
**Researcher**: CoreyCole
**Git Commit**: 055cdfdc9a74da5dfdcc3be8ffe65d38343e5a33
**Branch**: main
**Repository**: creative-mode

## Research Question
Understand the linear-cli architecture so we can use it to track work on creative-mode.

## Summary

linear-cli is a comprehensive Rust CLI (v0.3.14) for Linear.app with 50+ commands across 38 command modules, 38 agent skills, and first-class support for AI agent integration. It's designed to replace Linear MCP tools with 10-50x better token efficiency. The architecture is a flat module structure: clap-derived command parsing → shared LinearClient (GraphQL over reqwest) → output pipeline (JSON/table/NDJSON with filtering, sorting, field selection). Authentication supports API keys and OAuth 2.0 with PKCE.

## Detailed Findings

### Architecture Overview

```
linear-cli (single Rust binary)
├── main.rs          -- CLI parsing (clap derive), global state, command dispatch
├── api.rs           -- LinearClient: GraphQL HTTP, auth resolution, ID resolvers
├── commands/        -- 38 modules (one per domain: issues, git, sprint, etc.)
├── output.rs        -- JSON/table/NDJSON formatting, filtering, sorting, field selection
├── pagination.rs    -- Cursor-based GraphQL pagination (batch + streaming)
├── cache.rs         -- File-based JSON cache with per-key TTL
├── retry.rs         -- Exponential backoff with jitter
├── oauth.rs         -- OAuth 2.0 PKCE flow
├── config.rs        -- TOML config with workspace profiles
├── error.rs         -- Typed errors with exit codes (0-4)
├── types.rs         -- Typed Rust structs for Linear entities
├── vcs.rs           -- Git/Jujutsu branch management
├── dates.rs         -- Natural language date parsing (+3d, eow, tomorrow)
├── text.rs          -- String utilities, markdown stripping
├── json_path.rs     -- Dot-path JSON traversal
├── input.rs         -- Stdin piping support
├── priority.rs      -- Priority display formatting
└── keyring.rs       -- OS keyring (optional feature)
```

### Entry Point & Command Dispatch

`main()` spawns an 8MB stack thread (clap's derive macro generates a large enum), creates a single-threaded tokio runtime, and calls `async_main()`. Global state is stored in `static OnceLock<T>` variables set during init:

1. Parse `Cli` struct via clap
2. Construct `OutputOptions` (format, JSON options, filters, pagination, cache, dry_run)
3. Construct `AgentOptions` (quiet, id_only, dry_run, yes)
4. Set global state (quiet mode, yes mode, display options, retry config)
5. `run_command()` matches the `Commands` enum → delegates to each module's `handle()`

### API Client Layer

`LinearClient` wraps reqwest with:
- **Auth**: `Arc<RwLock<AuthState>>` supporting transparent OAuth token refresh with double-checked locking
- **Queries**: Wrapped with retry (exponential backoff + jitter, retryable: 429, timeouts, 502/503/504)
- **Mutations**: Never retried (prevents duplicate side effects)
- **ID resolution**: 3-tier strategy: cache → filtered GraphQL query → paginated fallback

Auth priority: `LINEAR_API_KEY` env var > OS keyring > OAuth tokens > config file API key.

### Command Module Pattern

Every command module follows this pattern:
1. `#[derive(Subcommand)]` enum with clap `#[arg]` attributes
2. `pub async fn handle(cmd, output, agent_opts)` that matches variants
3. Private async functions that: create LinearClient → resolve names to UUIDs → build GraphQL query → execute → format output
4. Dual output: JSON path (via `print_json_owned()`) or table path (via `tabled` crate)

All GraphQL queries are inline string literals (no code generation).

### Output Pipeline

`print_json_owned()` processes output through:
1. `apply_filters()` — field=value, field!=value, field~=value (case-insensitive, dot-path)
2. `apply_sort()` — by field with asc/desc, null-last semantics
3. `select_fields()` — project to specific dot-path fields
4. Template rendering (`{{field}}` substitution) or JSON/NDJSON serialization

### Agent-Friendly Design

| Flag | Purpose |
|------|---------|
| `--output json` | Machine-readable JSON |
| `--compact` | No pretty-printing |
| `--fields a,b,c` | Limit JSON to specific fields |
| `--id-only` | Only output resource ID (for chaining) |
| `--quiet` | Suppress decorative output |
| `--dry-run` | Preview without changes |
| `--yes` | Auto-confirm prompts |
| `--fail-on-empty` | Non-zero exit on empty results |
| `--format tpl` | Template output (`{{field}}`) |
| `-` (stdin) | Pipe IDs/descriptions between commands |

Exit codes: 0=Success, 1=General, 2=NotFound, 3=Auth, 4=RateLimited.

### Skills System (38 Skills)

Skills are Markdown files (`SKILL.md`) with YAML frontmatter that teach AI agents how to use linear-cli. Installed via `npx skills add Finesssee/linear-cli`. Categories:

| Category | Skills | Examples |
|----------|--------|----------|
| Issues (6) | list, create, update, workflow, comments, done | `linear-cli i create "Bug" -t CM -p 1` |
| Git (2) | git, pr | `linear-cli i start LIN-123 --checkout` |
| Planning (7) | projects, project-updates, milestones, roadmaps, initiatives, cycles, sprint | `linear-cli sp burndown -t CM` |
| Organization (6) | teams, labels, statuses, relations, templates, views | `linear-cli rel add LIN-1 blocks LIN-2` |
| Operations (6) | bulk, import, export, triage, favorites, attachments | `linear-cli b assign LIN-1 LIN-2 -a me` |
| Tracking (5) | metrics, history, time, watch, webhooks | `linear-cli mt -t CM` |
| Advanced (6) | api, search, notifications, documents, uploads, config | `linear-cli s issues "auth bug"` |

### Skills Most Relevant for Creative Mode

1. **`linear-projects`** — Create/manage the creative-mode project container
2. **`linear-create` + `linear-update` + `linear-list`** — Day-to-day issue tracking
3. **`linear-milestones`** — Break project into phases (Alpha, Beta, Launch)
4. **`linear-sprint` + `linear-cycles`** — Sprint planning with burndown/velocity
5. **`linear-relations`** — Model task dependencies (blocks/parent-child)
6. **`linear-workflow` + `linear-done`** — Connect git branches to issues
7. **`linear-project-updates`** — Post health status (onTrack/atRisk/offTrack)
8. **`linear-metrics`** — Velocity and progress tracking

### Configuration

Config stored at `~/.config/linear-cli/config.toml`. Supports multiple workspace profiles:

```bash
linear-cli config set-key lin_api_xxx          # Set API key
linear-cli config workspace-add creative       # Add workspace profile
linear-cli config workspace-switch creative    # Switch to it
linear-cli --profile creative i list           # Per-invocation override
```

### Testing Strategy

233 tests total (167 unit + 66 integration), all inline in source files (`#[cfg(test)]`):
- Type deserialization (full + minimal variants for all 13 models)
- Output pipeline (filter parsing, sorting, field selection, templates)
- Error handling and exit code mapping
- Retry/resilience with exponential backoff bounds
- Utility functions (dates, text, JSON paths, cache)
- Command-level formatting (history entries, notifications, branch names)
- CLI help text and alias integration tests

No mocking of the Linear API — tests operate on pure data transformations.

## Code References

- `context/linear-cli/src/main.rs:875` — Entry point, 8MB stack thread + tokio runtime
- `context/linear-cli/src/main.rs:275-760` — Commands enum (35 subcommands with aliases)
- `context/linear-cli/src/main.rs:1034-1190` — Command dispatch
- `context/linear-cli/src/api.rs:507-734` — LinearClient (auth, query, mutate)
- `context/linear-cli/src/api.rs:33-97` — Generic ID resolver
- `context/linear-cli/src/commands/issues.rs:27` — IssueCommands (18 variants)
- `context/linear-cli/src/commands/git.rs:32` — GitCommands (checkout, branch, PR)
- `context/linear-cli/src/commands/sprint.rs:12` — SprintCommands (burndown, velocity)
- `context/linear-cli/src/output.rs:147` — Output pipeline (filter→sort→select→render)
- `context/linear-cli/src/pagination.rs:31` — Cursor-based pagination
- `context/linear-cli/src/oauth.rs:55` — OAuth 2.0 PKCE authorization
- `context/linear-cli/src/config.rs:29-37` — Config/Workspace structs
- `context/linear-cli/src/error.rs:28-34` — CliError with ErrorKind
- `context/linear-cli/src/retry.rs:66-99` — Exponential backoff with jitter

## Architecture Insights

### Key Design Decisions

1. **Dynamic JSON over typed structs**: Commands use `serde_json::Value` for API responses rather than typed models. `types.rs` exists but is marked `#[allow(dead_code)]` — gradual adoption in progress. This gives flexibility with Linear's evolving API.

2. **No GraphQL code generation**: All queries are inline strings. Trades type safety for simplicity and fast iteration.

3. **Agent-first output**: JSON output with `--compact`, `--fields`, `--id-only`, and stdin piping are first-class features, not afterthoughts. The `--quiet` flag auto-enables for JSON/NDJSON.

4. **Single-threaded async**: Uses `tokio::runtime::Builder::new_current_thread()` — sufficient for CLI that does sequential API calls with occasional concurrent batch fetching.

5. **Three-tier ID resolution**: Cache → filtered query → paginated fallback. Balances speed (cache hit) with correctness (API query) and completeness (full pagination).

6. **Atomic file operations**: Both config and cache use temp-file + rename for crash safety, with 0o600 permissions on Unix.

### Patterns for Creative Mode Integration

**Recommended setup flow:**
```bash
# 1. Install
cargo install linear-cli

# 2. Authenticate
linear-cli config set-key lin_api_xxx
# or: linear-cli auth oauth

# 3. Set default team
linear-cli config set default_team CM

# 4. Install agent skills for Claude Code
npx skills add Finesssee/linear-cli

# 5. Verify
linear-cli doctor
```

**Recommended workflow for tracking creative-mode work:**
```bash
# Create project
linear-cli p create "Creative Mode" -t CM

# Create issues for work items
linear-cli i create "Implement mayor chat" -t CM -p 2 -l feature

# Start working (assigns, sets In Progress, creates branch)
linear-cli i start CM-123 --checkout

# When done
linear-cli done
linear-cli g pr CM-123

# Sprint tracking
linear-cli sp status -t CM
linear-cli sp burndown -t CM
linear-cli sp velocity -t CM
```

## Open Questions

1. **Linear workspace setup**: What team key should creative-mode use? (e.g., `CM`)
2. **Label taxonomy**: What labels make sense? (feature, bug, infra, harness, template, mayor, site)
3. **Sprint cadence**: Weekly or biweekly sprints?
4. **Agent integration**: Should the president/mayor agents also interact with Linear for automated status updates?
5. **Webhook integration**: Could Linear webhooks trigger harness actions (e.g., auto-deploy on issue status change)?
