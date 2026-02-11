#!/bin/bash

# Clean shutdown: forward SIGTERM to server, then exit
trap 'kill $PID 2>/dev/null; wait $PID; exit 0' SIGTERM SIGINT

echo "=== Creative Mode Harness — Dev Container ==="
echo ""
echo "  Browse:  http://localhost:8080"
echo "  Rebuild: POST /dev/rebuild"
echo "  CSS:     POST /dev/reload-static"
echo ""

# Initial build
echo "Building..."
go build -o /tmp/harness . || exit 1

# Run in a restart loop.
# The /dev/rebuild endpoint builds a new binary, then sends SIGTERM.
# Graceful shutdown exits 0, and this loop restarts with the new binary.
while true; do
    /tmp/harness &
    PID=$!
    wait $PID
    EXIT_CODE=$?
    # If binary was removed or non-zero exit (not graceful), stop
    [ ! -f /tmp/harness ] && exit 1
    echo "Restarting... (exit code: $EXIT_CODE)"
done
