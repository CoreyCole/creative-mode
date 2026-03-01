---
name: swarm-setup
description: One-time setup for agent swarm — creates Linear labels, verifies CLI auth. Run once before using other swarm primitives.
allowed-tools: Bash
---

# Swarm Setup

Idempotent setup for the agent swarm system. Safe to run multiple times.

## Steps

### 1. Verify Linear CLI Auth

```bash
linear-cli config doctor
```

If auth fails, run `linear-cli config auth` and follow prompts.

### 2. Check Existing Labels

```bash
linear-cli l list --output json --compact
```

### 3. Create Missing Labels

For each label below, check if it already exists. Only create if missing.

| Label | Color |
|-------|-------|
| `swarm:research` | `#3B82F6` |
| `swarm:code` | `#10B981` |
| `swarm:verification` | `#EAB308` |
| `swarm:project` | `#8B5CF6` |
| `swarm:plan` | `#F97316` |
| `swarm:orchestration` | `#EF4444` |
| `type:bug` | `#DC2626` |
| `type:feature` | `#2563EB` |
| `type:refactor` | `#7C3AED` |
| `type:prototype` | `#059669` |

Create command: `linear-cli l create "{name}" --color "{color}"`

### 4. Verify Workflow States

```bash
linear-cli st list -t CM --output json
```

Confirm these states exist: Triage, Backlog, Todo, In Progress, In Review, Done.

### 5. Report Summary

Print a summary of labels created vs. already existing.

## Dry-Run Support

When `--dry-run` is passed:
- Print `[DRY-RUN] Would create label: {name} ({color})` for each missing label
- Do not make any API calls to create labels
- Still run verification checks (auth, workflow states)
