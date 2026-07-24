#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-mysql.yml"
NGINX_DEBUG_CONF="$PROJECT_ROOT/docker/nginx/debug.conf"
FRONTEND_DIST="$PROJECT_ROOT/frontend/dist"

# Whether `debug` started MySQL itself (so it should stop it on exit to release
# the shared docker/data/mysql lock for `make web-up`).
DEBUG_STARTED_MYSQL=false

db_up() {
  if docker compose -f "$COMPOSE_FILE" ps --format '{{.Health}}' 2>/dev/null | grep -q 'healthy'; then
    echo "MySQL middleware already healthy."
    return 0
  fi
  echo "--- Starting MySQL middleware ---"
  docker compose -f "$COMPOSE_FILE" up -d
  echo "Waiting for MySQL to become healthy..."
  for _ in $(seq 1 30); do
    if docker compose -f "$COMPOSE_FILE" ps --format '{{.Health}}' 2>/dev/null | grep -q 'healthy'; then
      echo "MySQL healthy."
      DEBUG_STARTED_MYSQL=true
      return 0
    fi
    sleep 2
  done
  echo "ERROR: MySQL did not become healthy in time."
  exit 1
}

db_down() {
  echo "--- Stopping MySQL middleware ---"
  docker compose -f "$COMPOSE_FILE" down
}

case "${1:-}" in
  backend)
    go -C "$PROJECT_ROOT/backend" run ./cmd/magi-server
    ;;

  frontend)
    npm -C "$PROJECT_ROOT/frontend" run dev
    ;;

  debug)
    # 1. MySQL
    db_up

    # 2. Build frontend (so nginx has something to serve)
    echo "--- Building frontend ---"
    npm -C "$PROJECT_ROOT/frontend" run build

    # 3. Nginx container (proxies /api -> host:8080, serves frontend dist)
    echo "--- Starting nginx debug container ---"
    docker rm -f magi-nginx-debug 2>/dev/null || true
    docker run -d --name magi-nginx-debug \
      -p 80:80 \
      -v "$NGINX_DEBUG_CONF:/etc/nginx/conf.d/default.conf:ro" \
      -v "$FRONTEND_DIST:/usr/share/nginx/html:ro" \
      --add-host=host.docker.internal:host-gateway \
      nginx:1.27-alpine

    cleanup() {
      trap - EXIT
      echo ""
      echo "--- Stopping nginx debug container ---"
      docker rm -f magi-nginx-debug 2>/dev/null || true
      if [ "$DEBUG_STARTED_MYSQL" = "true" ]; then
        db_down
      fi
    }
    trap cleanup EXIT

    # 4. Backend (go run, :8080)
    echo "--- Starting backend (go run :8080) ---"
    echo ""
    echo "  Frontend : http://localhost"
    echo "  API      : http://localhost/api/v1/cases"
    echo "  Health   : http://localhost/health"
    echo ""
    echo "  Press Ctrl+C to stop."
    echo ""
    go -C "$PROJECT_ROOT/backend" run ./cmd/magi-server
    ;;

  *)
    echo "Usage: dev.sh {backend|frontend|debug}"
    echo "  backend    Run Go backend (go run :8080)"
    echo "  frontend   Run Vite dev server (:5173)"
    echo "  debug      Full local dev: MySQL + backend (go run) + nginx (:80)"
    exit 1
    ;;
esac
