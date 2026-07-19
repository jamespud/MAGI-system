#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_BASE="$PROJECT_ROOT/docker/docker-compose.yml"
COMPOSE_DEV="$PROJECT_ROOT/docker/docker-compose.dev.yml"
COMPOSE_FILES=(-f "$COMPOSE_BASE" -f "$COMPOSE_DEV")

case "${1:-}" in
  up)
    docker compose "${COMPOSE_FILES[@]}" up -d
    ;;
  down)
    docker compose "${COMPOSE_FILES[@]}" down
    ;;
  logs)
    docker compose "${COMPOSE_FILES[@]}" logs -f
    ;;
  restart)
    docker compose "${COMPOSE_FILES[@]}" down
    docker compose "${COMPOSE_FILES[@]}" up -d
    ;;
  *)
    echo "Usage: docker.sh {up|down|logs|restart}"
    exit 1
    ;;
esac
