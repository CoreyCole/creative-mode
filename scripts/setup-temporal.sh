#!/usr/bin/env bash
# Setup Temporal server for swarm orchestration.
# Idempotent — safe to re-run.
set -euo pipefail

DATA_DIR="${DATA_DIR:-$(cd "$(dirname "$0")/.." && pwd)/data}"
TEMPORAL_DB="$DATA_DIR/temporal.db"
TEMPORAL_PORT="${TEMPORAL_PORT:-7233}"
TEMPORAL_UI_PORT="${TEMPORAL_UI_PORT:-8233}"
TEMPORAL_NAMESPACE="swarm"

echo "==> Installing Temporal CLI..."
if command -v temporal &>/dev/null; then
    echo "    temporal CLI already installed: $(temporal --version)"
else
    curl -sSf https://temporal.download/cli.sh | sh
    # The installer puts it in ~/.temporalio/bin
    export PATH="$HOME/.temporalio/bin:$PATH"
    echo "    installed: $(temporal --version)"
fi

# Ensure temporal is on PATH for systemd.
TEMPORAL_BIN="$(command -v temporal)"
echo "    binary: $TEMPORAL_BIN"

echo "==> Creating data directory..."
mkdir -p "$DATA_DIR"

echo "==> Creating systemd service..."
cat > /tmp/temporal.service <<EOF
[Unit]
Description=Temporal Server (SQLite)
After=network.target

[Service]
Type=simple
ExecStart=$TEMPORAL_BIN server start-dev \
    --ip 127.0.0.1 \
    --port $TEMPORAL_PORT \
    --ui-port $TEMPORAL_UI_PORT \
    --db-filename $TEMPORAL_DB \
    --namespace $TEMPORAL_NAMESPACE \
    --log-format json
Restart=on-failure
RestartSec=5
Environment=PATH=$HOME/.temporalio/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=multi-user.target
EOF

sudo mv /tmp/temporal.service /etc/systemd/system/temporal.service
sudo systemctl daemon-reload
sudo systemctl enable temporal

echo "==> Starting Temporal server..."
sudo systemctl start temporal || true

# Wait for server to be ready.
echo "==> Waiting for Temporal server..."
for i in $(seq 1 30); do
    if temporal operator namespace describe "$TEMPORAL_NAMESPACE" --address "127.0.0.1:$TEMPORAL_PORT" &>/dev/null 2>&1; then
        echo "    namespace '$TEMPORAL_NAMESPACE' ready"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "    WARNING: timeout waiting for namespace (server may still be starting)"
    fi
    sleep 1
done

echo "==> Temporal setup complete"
echo "    gRPC:  127.0.0.1:$TEMPORAL_PORT"
echo "    UI:    http://127.0.0.1:$TEMPORAL_UI_PORT"
echo "    DB:    $TEMPORAL_DB"
echo "    NS:    $TEMPORAL_NAMESPACE"
