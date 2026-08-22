#!/usr/bin/env bash
# deploy.sh — containerized full stack (web) lifecycle.
# Aligned with deer-flow's deploy.sh: `up` builds + starts, `down` tears down.
#
# Usage: scripts/deploy.sh {up|down|stop|ps}
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-web.yml"
COMPOSE_DEBUG="$PROJECT_ROOT/docker/docker-compose-debug.yml"

if [ -f "$PROJECT_ROOT/.env" ]; then
  set -a
  source "$PROJECT_ROOT/.env"
  set +a
fi

case "${1:-up}" in
  up)
    # The dev/debug stack shares MAGI's RAG container names and MySQL bind-mount
    # with the web stack, so they cannot coexist. Tear the dev middleware down
    # first (external RAG volumes are preserved by compose down).
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_DEBUG" down >/dev/null 2>&1 || true
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" up -d --build
    ;;
  down)
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" down
    ;;
  ps)
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" ps
    ;;
  stop)
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" stop
    ;;
  *)
    echo "usage: deploy.sh {up|down|stop|ps}"
    exit 1
    ;;
esac
