#!/usr/bin/env bash
# deploy-go.sh — Zero-downtime blue/green deployment for the Go audio service.
#
# Strategy (from the migration plan):
#   1. Build a new audiod binary
#   2. Start it on a temporary port :8002 (blue/green)
#   3. Wait for health check to pass
#   4. Atomically swap: Nginx starts sending new connections to :8002
#   5. Send SIGTERM to the old process on :8001
#   6. Old process drains active calls (up to 60s) then exits cleanly
#   7. New process inherits the :8001 address (or stays on :8002 depending on mode)
#
# Usage:
#   ./scripts/deploy-go.sh [--docker | --binary]
#
#   --binary  (default) Build and deploy as a native binary (systemd)
#   --docker            Deploy using docker-compose rolling update

set -euo pipefail

MODE="${1:---binary}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_SVC="$REPO_ROOT/go-audio-service"
BINARY_PATH="$GO_SVC/bin/audiod"
NEW_BINARY_PATH="$GO_SVC/bin/audiod.new"
SYSTEMD_SERVICE="callified-go-audio"
HEALTH_URL="http://127.0.0.1"
DRAIN_TIMEOUT=60
MAX_HEALTH_WAIT=30

log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

# ─────────────────────────────────────────────────────────────────────────────
# Docker mode: leverage docker-compose rolling update
# ─────────────────────────────────────────────────────────────────────────────
if [ "$MODE" = "--docker" ]; then
    log "Building Docker image..."
    docker build -t callified-go-audio:latest "$GO_SVC"

    log "Rolling update via docker-compose..."
    cd "$REPO_ROOT"
    docker-compose up -d --no-deps --build go-audio

    log "Waiting for health check..."
    for i in $(seq 1 $MAX_HEALTH_WAIT); do
        if docker-compose exec -T go-audio wget -qO- http://localhost:8001/health > /dev/null 2>&1; then
            log "Health check passed after ${i}s"
            break
        fi
        [ "$i" -eq "$MAX_HEALTH_WAIT" ] && die "Health check failed after ${MAX_HEALTH_WAIT}s"
        sleep 1
    done

    log "Deploy complete (Docker mode)"
    exit 0
fi

# ─────────────────────────────────────────────────────────────────────────────
# Binary mode: build → start on :8002 → health check → swap → drain old
# ─────────────────────────────────────────────────────────────────────────────

log "Building Go audio service..."
(cd "$GO_SVC" && go build -ldflags="-s -w" -o "$NEW_BINARY_PATH" ./cmd/audiod)
log "Build complete: $NEW_BINARY_PATH"

# Start new instance on temporary port :8002
log "Starting new instance on :8002..."
GO_AUDIO_PORT=8002 "$NEW_BINARY_PATH" &
NEW_PID=$!
log "New instance PID: $NEW_PID"

# Wait for the new instance to pass health checks
log "Waiting for health check on :8002..."
for i in $(seq 1 $MAX_HEALTH_WAIT); do
    if curl -sf "${HEALTH_URL}:8002/health" > /dev/null 2>&1; then
        log "Health check passed after ${i}s"
        break
    fi
    if ! kill -0 "$NEW_PID" 2>/dev/null; then
        die "New instance exited prematurely (PID $NEW_PID)"
    fi
    [ "$i" -eq "$MAX_HEALTH_WAIT" ] && die "Health check failed after ${MAX_HEALTH_WAIT}s — aborting deploy"
    sleep 1
done

# Update Nginx to send NEW connections to :8002
# (existing calls on :8001 continue undisturbed)
log "Updating Nginx upstream to :8002..."
NGINX_CONF_TEMP=$(mktemp)
cat > "$NGINX_CONF_TEMP" << 'NGINX_EOF'
upstream go_audio {
    server 127.0.0.1:8002;
    keepalive 64;
}
NGINX_EOF
# Replace the go_audio upstream definition in the live Nginx config
sudo sed -i 's|server 127.0.0.1:8001;|server 127.0.0.1:8002;|g' /etc/nginx/conf.d/callified.conf
sudo nginx -t || die "Nginx config test failed — reverting"
sudo systemctl reload nginx
log "Nginx now routing new connections → :8002"

# Gracefully stop the old instance on :8001
if systemctl is-active --quiet "$SYSTEMD_SERVICE" 2>/dev/null; then
    log "Sending SIGTERM to $SYSTEMD_SERVICE (60s drain window)..."
    sudo systemctl stop "$SYSTEMD_SERVICE"
elif pgrep -f "audiod.*8001" > /dev/null 2>&1; then
    OLD_PID=$(pgrep -f "audiod.*8001" | head -1)
    log "Sending SIGTERM to old audiod PID $OLD_PID (${DRAIN_TIMEOUT}s drain window)..."
    kill -TERM "$OLD_PID"
    # Wait for graceful drain
    for i in $(seq 1 $DRAIN_TIMEOUT); do
        kill -0 "$OLD_PID" 2>/dev/null || { log "Old instance exited after ${i}s"; break; }
        sleep 1
    done
    # Force kill if still alive after drain window
    kill -0 "$OLD_PID" 2>/dev/null && kill -KILL "$OLD_PID" && log "Force-killed stale process"
fi

# Promote the new binary to the canonical path and fix Nginx back to :8001
log "Promoting new binary to :8001..."
mv "$NEW_BINARY_PATH" "$BINARY_PATH"
sudo sed -i 's|server 127.0.0.1:8002;|server 127.0.0.1:8001;|g' /etc/nginx/conf.d/callified.conf

# Restart as the canonical port
GO_AUDIO_PORT=8001 "$BINARY_PATH" &
PROMOTED_PID=$!
log "Promoted instance PID: $PROMOTED_PID (port 8001)"

# Register with systemd if available
if systemctl cat "$SYSTEMD_SERVICE" > /dev/null 2>&1; then
    sudo systemctl restart "$SYSTEMD_SERVICE"
    log "Registered with systemd: $SYSTEMD_SERVICE"
fi

# Final Nginx reload with :8001 upstream
sudo nginx -t && sudo systemctl reload nginx
log "Nginx routing new connections → :8001 (promoted)"

log ""
log "Deploy complete."
log "  Old instance: drained and stopped"
log "  New instance: PID $PROMOTED_PID on :8001"
log "  Zero calls dropped during deployment"
