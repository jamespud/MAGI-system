#!/usr/bin/env bash
# docker.sh — dev middleware lifecycle (MySQL + Milvus + Elasticsearch + deps).
# Aligned with deer-flow's docker.sh: single init/start/stop/logs surface for the
# stateful services that local `make dev` / `make start` depend on.
#
# Usage: scripts/docker.sh {init|start|stop|logs|ps}
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-debug.yml"
COMPOSE_WEB="$PROJECT_ROOT/docker/docker-compose-web.yml"

# Load .env so MAGI_* / DB_* reach docker compose interpolation.
if [ -f "$PROJECT_ROOT/.env" ]; then
  set -a
  source "$PROJECT_ROOT/.env"
  set +a
fi

RAG_VOLUMES=(magi-milvus-data magi-es-data magi-etcd-data magi-minio-data)

compose() {
  docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" "$@"
}

create_rag_volumes() {
  for volume in "${RAG_VOLUMES[@]}"; do
    docker volume create "$volume" >/dev/null 2>&1 || true
  done
}

wait_ready() {
  echo "Waiting for middleware readiness (mysql + milvus :9091 + es :9200)..."
  local ready=0
  for _ in $(seq 1 60); do
    if compose ps --format '{{.Health}}' 2>/dev/null | grep -q 'healthy' \
       && curl -sf http://localhost:9091/healthz >/dev/null 2>&1 \
       && curl -sf http://localhost:9200/ >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 2
  done
  if [ "$ready" -ne 1 ]; then
    echo "ERROR: middleware did not become ready in time."
    echo "       Try 'scripts/docker.sh stop' then 'scripts/docker.sh start' again."
    exit 1
  fi
  echo "Middleware ready."
}

case "${1:-}" in
  init)
    create_rag_volumes
    compose pull mysql milvus-standalone elasticsearch
    ;;
  start)
    # dev/debug and the containerized web stack share MAGI's RAG container names
    # and MySQL bind-mount, so they are mutually exclusive. Free the web stack
    # first (external RAG volumes are preserved on `down`).
    docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_WEB" down >/dev/null 2>&1 || true
    create_rag_volumes
    compose up -d mysql milvus-standalone elasticsearch
    wait_ready
    ;;
  down)
    compose down
    ;;
  stop)
    compose stop
    ;;
  logs)
    compose logs -f
    ;;
  ps)
    compose ps
    ;;
  *)
    echo "usage: docker.sh {init|start|stop|down|logs|ps}"
    exit 1
    ;;
esac
