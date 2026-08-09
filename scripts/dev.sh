#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-debug.yml"
RAG_COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-debug.yml"
NGINX_DEBUG_CONF="$PROJECT_ROOT/docker/nginx/debug.conf"
FRONTEND_DIST="$PROJECT_ROOT/frontend/dist"

# Containers (MySQL + Milvus + ES) are NOT stopped on exit - left running for
# fast re-runs. Stop manually with `make db-down` / `make rag_down`.
DEBUG_STARTED_MYSQL=false

db_up() {
  if docker compose -f "$COMPOSE_FILE" ps --format '{{.Health}}' 2>/dev/null | grep -q 'healthy'; then
    echo "MySQL middleware already healthy."
    return 0
  fi
  echo "--- Starting MySQL middleware ---"
  docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" up -d mysql
  echo "Waiting for MySQL to become healthy..."
  for _ in $(seq 1 30); do
    if docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" ps --format '{{.Health}}' 2>/dev/null | grep -q 'healthy'; then
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
  docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" down
}

# rag_up starts Milvus + Elasticsearch (and deps etcd/minio) and waits for
# readiness. Real backends are required (no fake) when milvus.address /
# elasticsearch.addresses are set in conf/magi.yaml.
rag_up() {
  echo "--- Creating RAG external volumes (idempotent) ---"
  docker volume create magi-milvus-data 2>/dev/null || true
  docker volume create magi-es-data 2>/dev/null || true
  docker volume create magi-etcd-data 2>/dev/null || true
  docker volume create magi-minio-data 2>/dev/null || true
  echo "--- Starting RAG middleware (Milvus + Elasticsearch) ---"
  docker compose --project-directory "$PROJECT_ROOT" -f "$RAG_COMPOSE_FILE" up -d milvus-standalone elasticsearch
  echo "Waiting for Milvus (9091/healthz)..."
  for _ in $(seq 1 60); do
    if curl -sf http://localhost:9091/healthz >/dev/null 2>&1; then
      echo "Milvus ready."
      break
    fi
    sleep 2
  done
  echo "Waiting for Elasticsearch (9200)..."
  for _ in $(seq 1 60); do
    if curl -sf "http://localhost:9200/_cluster/health?wait_for_status=yellow&timeout=2s" >/dev/null 2>&1; then
      echo "Elasticsearch ready."
      break
    fi
    sleep 2
  done
  if ! curl -sf http://localhost:9091/healthz >/dev/null 2>&1; then
    echo "ERROR: Milvus not ready at :9091. Run 'make rag_down' then retry."
    exit 1
  fi
  if ! curl -sf http://localhost:9200/ >/dev/null 2>&1; then
    echo "ERROR: Elasticsearch not ready at :9200. Run 'make rag_down' then retry."
    exit 1
  fi
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

    # 2. RAG middleware (Milvus + ES) - real backends, no fakes
    rag_up

    # 3. Build frontend (so nginx has something to serve)
    echo "--- Building frontend ---"
    npm -C "$PROJECT_ROOT/frontend" run build

    # 4. Nginx container (proxies /api -> host:8080, serves frontend dist).
    # --network host: the nginx container shares the host network so it can
    # reach the go-run backend on 127.0.0.1:8080 directly. The host firewall
    # blocks docker-bridge -> host:8080, so bridged networking + host-gateway
    # does not work here. debug.conf proxies to 127.0.0.1:8080.
    echo "--- Starting nginx debug container ---"
    docker rm -f magi-dev-nginx 2>/dev/null || true
    docker run -d --name magi-dev-nginx \
      --network host \
      -v "$NGINX_DEBUG_CONF:/etc/nginx/conf.d/default.conf:ro" \
      -v "$FRONTEND_DIST:/usr/share/nginx/html:ro" \
      nginx:1.27-alpine

    # On exit: stop ONLY the nginx debug proxy. MySQL + Milvus + ES are left
    # running for fast re-runs (stop manually via make db-down / make rag_down).
    cleanup() {
      trap - EXIT
      echo ""
      echo "--- Stopping nginx debug container (MySQL/Milvus/ES left running) ---"
      docker rm -f magi-dev-nginx 2>/dev/null || true
    }
    trap cleanup EXIT

    # 5. Backend (go run, :8080)
    echo "--- Starting backend (go run :8080) ---"
    echo ""
    echo "  Frontend : http://localhost"
    echo "  API      : http://localhost/api/v1/cases"
    echo "  Health   : http://localhost/health"
    echo ""
    echo "  Press Ctrl+C to stop (containers keep running)."
    echo ""
    go -C "$PROJECT_ROOT/backend" run ./cmd/magi-server
    ;;

  *)
    echo "Usage: dev.sh {backend|frontend|debug}"
    echo "  backend    Run Go backend (go run :8080)"
    echo "  frontend   Run Vite dev server (:5173)"
    echo "  debug      Full local dev: MySQL + Milvus + ES + backend (go run) + nginx (:80)"
    exit 1
    ;;
esac
