#!/usr/bin/env bash
# serve.sh — unified MAGI local launcher (aligned with deer-flow's serve.sh).
#
# Modes / actions:
#   --dev       Hot-reload: middleware + vite dev (:5173) + backend go run (:8080)
#   --prod      Production-ish local: middleware + built frontend via nginx (:80) + go binary
#   --start     Alias for --prod
#   --stop      Stop local services (nginx proxy + backend)
#   --restart   --stop then start with the given mode
#   --daemon    (accepted; runs foreground for MAGI, keep simple)
#
# Usage:
#   scripts/serve.sh [--dev|--prod|--start] [--stop|--restart]
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Load .env so MAGI_* reach the process (single source for secrets).
if [ -f "$ROOT/.env" ]; then
  set -a
  source "$ROOT/.env"
  set +a
fi

ACTION="start"
MODE="dev"
DAEMON=false
for arg in "$@"; do
  case "$arg" in
    --dev) MODE=dev ;;
    --prod|--start) MODE=prod ;;
    --stop) ACTION=stop ;;
    --restart) ACTION=restart ;;
    --daemon) DAEMON=true ;;
  esac
done

stop_local() {
  docker rm -f magi-dev-nginx >/dev/null 2>&1 || true
  pkill -f 'cmd/magi-server' >/dev/null 2>&1 || true
  pkill -f 'vite' >/dev/null 2>&1 || true
  bash "$ROOT/scripts/docker.sh" down >/dev/null 2>&1 || true
}

case "$ACTION" in
  stop)
    stop_local
    echo "Local MAGI services stopped."
    exit 0
    ;;
  restart)
    stop_local
    ;;
esac

# Ensure dev middleware (MySQL + Milvus + ES) is running.
bash "$ROOT/scripts/docker.sh" start

if [ "$MODE" = "dev" ]; then
  # Backend listen port follows MAGI_HTTP_PORT (default 8080); the vite dev
  # proxy targets the same address so changing the backend port keeps working.
  export VITE_PROXY_TARGET="http://localhost:${MAGI_HTTP_PORT:-8080}"
  echo "Starting dev stack: middleware + vite (:5173) + backend go run ($VITE_PROXY_TARGET)"
  export VITE_DEV_SERVER_PORT="5173"
  ( npm -C "$ROOT/frontend" run dev ) &
  trap 'pkill -f "vite" >/dev/null 2>&1 || true' EXIT
  go -C "$ROOT/backend" run ./cmd/magi-server
else
  echo "Starting prod stack: middleware + nginx (:80) + go binary"
  npm -C "$ROOT/frontend" run build
  if [ ! -x "$ROOT/bin/magi" ]; then
    bash "$ROOT/scripts/build.sh" backend
  fi
  # nginx:1.27-alpine renders *.template files under /etc/nginx/templates via
  # its entrypoint envsubst, so MAGI_HTTP_PORT drives the proxy target here too.
  docker run -d --name magi-dev-nginx \
    --network host \
    -e MAGI_HTTP_PORT="${MAGI_HTTP_PORT:-8080}" \
    -v "$ROOT/docker/nginx/debug.conf.template:/etc/nginx/templates/default.conf.template:ro" \
    -v "$ROOT/frontend/dist:/usr/share/nginx/html:ro" \
    nginx:1.27-alpine
  exec "$ROOT/bin/magi"
fi
