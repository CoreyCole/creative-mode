#!/usr/bin/env bash
# Create Linear labels for swarm workflow tracking.
# Idempotent — re-running won't create duplicates (linear-cli errors on existing labels).
set -euo pipefail

CLI="${LINEAR_CLI:-linear-cli}"

echo "Creating type labels..."
$CLI labels create "type:research" --type issue -c "#5B8DB8" || true
$CLI labels create "type:code-change" --type issue -c "#8B6CB0" || true
$CLI labels create "type:project" --type issue -c "#D4920A" || true

echo "Creating swarm stage labels..."
$CLI labels create "swarm:research" --type issue -c "#F2C94C" || true
$CLI labels create "swarm:planning" --type issue -c "#F2994A" || true
$CLI labels create "swarm:implementing" --type issue -c "#27AE60" || true
$CLI labels create "swarm:verifying" --type issue -c "#EB5757" || true

echo "Done. Verify with: $CLI labels list --type issue"
