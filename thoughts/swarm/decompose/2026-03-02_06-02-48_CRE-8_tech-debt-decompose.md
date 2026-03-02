---
ticket: CRE-8
workflow: e8c72519
session: 917eb5d4
timestamp: 2026-03-02T06:02:48Z
---

# Research Decomposition: Tech Debt Audit

## Initial Research Summary
The initial research phase conducted a comprehensive tech debt audit across the Go harness, Rust/Bevy templates, infrastructure, and documentation. It identified 17 actionable items organized into 4 priority tiers. The biggest issues are: oversized Go files (manager.go at 1981 lines), zero test coverage for critical `pkg/` packages (mayorchat, worldchannel), duplicated Discord auth logic between harness and site, scattered magic numbers in Rust templates, and missing CI/CD and security hardening. The codebase has good hygiene (no TODOs, consistent error wrapping, proper concurrency), but structural debt has accumulated in file organization, test coverage, and operational tooling.

## Research Topics

| # | Topic | Description |
|---|-------|-------------|
| 1 | Go file splitting: dependency analysis | Map internal dependencies of the 4 largest Go files to identify safe split boundaries and interface extraction points |
| 2 | Testing strategy for untested packages | Analyze pkg/ packages for testability, identify mocking requirements, and design test infrastructure |
| 3 | Code deduplication: shared package design | Compare duplicated implementations (Discord auth, SSE helpers, BridgePlugin) and design shared abstractions |
| 4 | Rust/Bevy template cleanup audit | Catalog magic numbers, analyze 3D client module structure, and propose constant/module conventions |
| 5 | Infrastructure, CI/CD, and security hardening | Investigate database improvements, CI setup, Docker hardening, and map remaining security hardening items |

## Topic Details

### 1. Go file splitting: dependency analysis
**Why**: The top 4 Go files (manager.go 1981 lines, create.go 1046, server.go 914, project.go 851) are the highest-risk refactoring targets. Splitting them incorrectly could break the tightly-coupled orchestrator. The initial research identified file sizes and mixed concerns but didn't map the actual dependency graph between methods, which is essential for safe splitting.
**Scope**: For each of the 4 files: (a) list every exported and unexported method, (b) map which methods call which others, (c) identify shared state (struct fields accessed by method groups), (d) propose natural module boundaries, (e) identify interfaces that would need extraction to break coupling. Focus especially on `swarmorch/manager.go` since the research flagged it as highest-risk.
**Expected output**: Dependency graphs for each file, proposed split plans with specific method-to-file assignments, and a list of interfaces that must be extracted first.

### 2. Testing strategy for untested packages
**Why**: Critical business logic in `pkg/mayorchat` (9 files, conversation management), `pkg/worldchannel` (7 files, Discord channel creation), `pkg/markdown`, and `pkg/imagegen` has zero test coverage. The initial research noted these gaps but didn't analyze what mocking infrastructure or interface abstractions would be needed. Adding tests to packages with external dependencies (Discord API, SQLite, Gemini API) requires design decisions about how to mock.
**Scope**: For each untested package: (a) read all source files to understand the public API surface, (b) identify external dependencies that need mocking (HTTP clients, database, Discord API), (c) survey existing test patterns in the codebase (especially `harness/internal/swarm/` and `harness/internal/swarmorch/` which have tests), (d) propose interface abstractions needed for testability, (e) prioritize which functions/methods are most critical to test first.
**Expected output**: Per-package test plan with priority-ordered test targets, required interface abstractions, proposed test helper utilities, and estimated complexity per package.

### 3. Code deduplication: shared package design
**Why**: Three categories of duplication were identified: Discord auth (~150 lines overlapping between harness and site), SSE subscription boilerplate (3 handlers), and BridgePlugin (2d and boardgame templates). The initial research flagged these but didn't compare the implementations side-by-side to understand exact overlaps vs. intentional divergences. Extracting shared code incorrectly could break either harness or site.
**Scope**: (a) Read both Discord auth implementations and diff them — identify identical code, similar-but-different code, and unique code in each. (b) Read all 3 SSE subscription handlers to map the boilerplate pattern and identify what varies. (c) Read both BridgePlugin implementations to compare message types, JS interop patterns, and Bevy plugin structure. (d) For each category, propose a shared abstraction design that accommodates both consumers without over-engineering.
**Expected output**: Side-by-side comparison tables for each duplication category, proposed shared package APIs (function signatures, struct designs), and migration plan showing what changes in each consumer.

### 4. Rust/Bevy template cleanup audit
**Why**: Magic numbers are scattered across 2D camera/interaction code, 3D client main.rs, and boardgame board.rs. The 3D client main.rs (622 lines) mixes camera, input, mesh sync, and scene setup. The initial research listed examples but didn't create a complete inventory, which is needed for the project plan to produce accurate task estimates and avoid missing items during implementation.
**Scope**: (a) Exhaustive catalog of all magic numbers in templates/2d/, templates/3d/, and templates/boardgame/ with proposed constant names. (b) Analyze 3D client main.rs structure — list all systems, resources, and plugins, map their dependencies, and propose module split. (c) Review `#[allow(dead_code)]` annotations in boardgame template to determine if code is actually dead or just conditionally used. (d) Propose naming conventions for constants (e.g., `CAMERA_MIN_ZOOM`, `DRAG_THRESHOLD_PX`).
**Expected output**: Complete magic number inventory with proposed constant names, 3D client module split plan, dead code audit results, and Rust coding conventions document for the templates.

### 5. Infrastructure, CI/CD, and security hardening
**Why**: The codebase has no CI/CD pipeline, Docker runs as root without health checks, database indexes are missing, and 7+ security hardening items from a prior research document remain unimplemented. These are operational and security concerns that span the entire project. The initial research identified these at a surface level but didn't investigate implementation details (e.g., which GitHub Actions setup fits the Nix+Rust+Go+WASM build, what the security headers middleware should look like).
**Scope**: (a) Read the existing security hardening research document (`thoughts/CoreyCole/research/2026-02-13_*_vps-deployment-security-hardening.md`) and map current state of each item. (b) Investigate `just check` to understand what it validates and how to integrate it as a CI step. (c) Research GitHub Actions setup for Nix+Rust+Go+WASM builds — caching strategies, runner requirements, estimated build times. (d) Investigate specific database index additions (verify column types and query patterns). (e) Assess Docker hardening options (non-root user, health checks, resource limits) against the existing docker-compose.
**Expected output**: Security hardening status matrix (done/not-done/blocked for each item), GitHub Actions workflow draft, database migration for missing indexes, Docker hardening recommendations, and implementation priority order.

## Rationale
These 5 topics were chosen to cover the full scope of the tech debt audit while keeping each topic independently researchable. The split reflects natural technical boundaries: Go server structure (topics 1, 2, 3), Rust client code (topic 4), and operational/infrastructure concerns (topic 5). Topics 1-3 focus on the Go codebase but from different angles — file organization (1), test infrastructure (2), and code sharing (3) — so they don't depend on each other's findings. Topic 4 is entirely Rust-side. Topic 5 spans infrastructure concerns that are distinct from application code. Together, these 5 topics will produce the detailed findings needed for the project plan to create accurate, well-scoped child tickets.
