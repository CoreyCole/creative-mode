---
name: temporal-conventions
description: Temporal Go SDK integration patterns, workflow/activity structure, swarm task queue
tags: [temporal, workflow, activity, worker, task-queue]
last_verified: 2026-03-09
---

# Temporal Conventions

## Status

Temporal Go SDK is fully integrated. The harness uses `go.temporal.io/sdk` for swarm task orchestration. Agent subprocess management still uses the JSONL protocol (`internal/swarmorch/types.go`), but workflow lifecycle, fan-out, and span tracking are all Temporal-driven.

## Infrastructure

- **Service**: `temporal-dev.service` (systemd), SQLite-backed dev server
- **Ports**: 7233 (gRPC), 8233 (UI dashboard)
- **Namespace**: `swarm`
- **Data**: `/var/lib/temporal/temporal.db`
- **Binary**: `/home/deploy/.nix-profile/bin/temporal` (Nix-managed, v1.5.1)
- **Env var**: `CM_SWARM_TEMPORAL=true` in `.env` (required)
- **Dependency**: `creative-mode.service` Requires+After `temporal-dev.service`

## CLI Commands

```bash
temporal workflow list --namespace swarm
temporal schedule list --namespace swarm
temporal workflow show --namespace swarm -w <workflow-id>
```

## Workflows

Two workflow types registered on task queue `swarm-agents`:

- **`ResearchWorkflow`** — Decomposes a request into sub-questions, fans out parallel research agents, synthesizes findings into a document.
- **`CodeChangePlanWorkflow`** — Runs research first, then classifies domains, fans out specialist planners, synthesizes a project plan.

Both are defined in `harness/internal/swarmorch/workflows.go`.

## Activity Struct

All activities are methods on `SwarmActivities` (in `activities.go`):

```go
type SwarmActivities struct {
    db        *db.DB
    eventBus  *events.EventBus
    repoRoot  string
    agentsDir string
    runner    AgentRunner
    config    SwarmConfig
    logger    *slog.Logger
}
```

Agent activities delegate to `runAgent()` which spawns a JS subprocess, manages the JSONL protocol, creates spans, and reads the output file.

Infrastructure activities handle DB updates, span management, event publishing, and file writes.

## Key Patterns

### Deterministic Workflows

Workflows must be deterministic (Temporal replays them from event history):
- Use `workflow.SideEffect()` for UUIDs and timestamps
- No direct I/O, HTTP, or DB calls in workflow code
- All side effects go through activities

### Activity Options

```go
func agentActivityOpts() workflow.ActivityOptions {
    return workflow.ActivityOptions{
        StartToCloseTimeout: 20 * time.Minute,
        HeartbeatTimeout:    60 * time.Second,
    }
}
```

### Disconnected Context for Cleanup

Cleanup activities (fail spans, update status) use `workflow.NewDisconnectedCtx(ctx)` so they run even if the workflow is cancelled.

### Fan-Out Pattern

Specialist planners use `workflow.Go()` goroutines with a shared `workflow.Channel` for fan-out/fan-in:

```go
planCh := workflow.NewChannel(ctx)
for _, spec := range planners {
    workflow.Go(ctx, func(gCtx workflow.Context) {
        // run activity
        planCh.Send(gCtx, output)
    })
}
for range planners {
    planCh.Receive(ctx, &output)
}
```

## Task Queue

All workflows and activities register on `swarm-agents`. The worker is started in `SwarmManager.Start()` (`manager.go`).
