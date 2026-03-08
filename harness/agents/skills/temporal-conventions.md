---
name: temporal-conventions
description: Temporal namespace swarm, dev server setup, CLI commands, future workflow integration
tags: [temporal, workflow, activity, worker, task-queue]
last_verified: 2026-03-08
---

# Temporal Conventions

## Status

Temporal is deployed as infrastructure but the Go SDK is **not yet integrated** into the harness codebase. The `go.mod` has no `go.temporal.io` dependency. Agent orchestration currently uses JSONL subprocess protocol (`internal/swarmorch/types.go`), with Temporal integration planned for Phase 3.

## Infrastructure (Running)

- **Service**: `temporal-dev.service` (systemd), SQLite-backed dev server
- **Ports**: 7233 (gRPC), 8233 (UI dashboard)
- **Namespace**: `swarm`
- **Data**: `/var/lib/temporal/temporal.db`
- **Binary**: `/home/deploy/.nix-profile/bin/temporal` (Nix-managed, v1.5.1)
- **Env var**: `CM_SWARM_TEMPORAL=true` in `.env`
- **Dependency**: `creative-mode.service` Requires+After `temporal-dev.service`

## CLI Commands

```bash
temporal workflow list --namespace swarm
temporal schedule list --namespace swarm
temporal workflow show --namespace swarm -w <workflow-id>
```

## Planned Workflow Rules (Phase 3)

When Temporal Go SDK is integrated:
- Workflows are deterministic — no I/O, no randomness, no time.Now()
- Use activities for all side effects (DB, HTTP, file I/O, subprocess)
- Activities can be retried; workflows replay from event history
- Use signals for external input to running workflows
- Use queries for reading workflow state without side effects
- Task queue name must match between workflow starter and worker
