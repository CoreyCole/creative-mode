---
name: swarm-setup
description: One-time setup for agent swarm — creates Linear labels, verifies CLI auth. Run once before using other swarm primitives.
allowed-tools: Bash
---

# Swarm Setup

Idempotent setup for the agent swarm system. Safe to run multiple times.

## Prerequisites

- `LINEAR_API_KEY` environment variable must be set
- `curl` and `jq` must be available

## Steps

### 1. Verify Linear API Auth

```bash
curl -s -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ viewer { id name } }"}' \
  https://api.linear.app/graphql | jq .
```

If auth fails, check that `LINEAR_API_KEY` is set correctly.

### 2. Check Existing Labels

```bash
curl -s -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ issueLabels { nodes { id name color } } }"}' \
  https://api.linear.app/graphql | jq '.data.issueLabels.nodes'
```

### 3. Create Missing Labels

For each label below, check if it already exists in the output above. Only create if missing.

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

Create command:
```bash
curl -s -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { issueLabelCreate(input: { name: \"{name}\", color: \"{color}\" }) { success } }"}' \
  https://api.linear.app/graphql
```

### 4. Verify Workflow States

```bash
curl -s -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ workflowStates(filter: { team: { key: { eq: \"CM\" } } }) { nodes { id name type } } }"}' \
  https://api.linear.app/graphql | jq '.data.workflowStates.nodes'
```

Confirm these states exist: Triage, Backlog, Todo, In Progress, In Review, Done.

### 5. Report Summary

Print a summary of labels created vs. already existing.

## Dry-Run Support

When `--dry-run` is passed:
- Print `[DRY-RUN] Would create label: {name} ({color})` for each missing label
- Do not make any API calls to create labels
- Still run verification checks (auth, workflow states)
