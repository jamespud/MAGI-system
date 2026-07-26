#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-debug.yml"

case "${1:-}" in
  up)
    docker compose -f "$COMPOSE_FILE" up -d mysql
    ;;
  down)
    docker compose -f "$COMPOSE_FILE" rm -sf mysql
    ;;
  logs)
    docker compose -f "$COMPOSE_FILE" logs -f mysql
    ;;
  restart)
    docker compose -f "$COMPOSE_FILE" rm -sf mysql
    docker compose -f "$COMPOSE_FILE" up -d mysql
    ;;
  *)
    echo "Usage: docker.sh {up|down|logs|restart}"
    exit 1
    ;;
esac
