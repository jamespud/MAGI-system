#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

build_backend() {
  echo "=== Building backend binary ==="
  go -C "$PROJECT_ROOT/backend" build -o "$PROJECT_ROOT/bin/magi" ./cmd/magi-server
  echo "=== Building backend Docker image ==="
  docker build -f "$PROJECT_ROOT/backend/Dockerfile" -t magi-server "$PROJECT_ROOT/backend"
}

build_frontend() {
  echo "=== Building frontend ==="
  npm -C "$PROJECT_ROOT/frontend" run build
  echo "=== Building frontend Docker image ==="
  docker build -f "$PROJECT_ROOT/frontend/Dockerfile" -t magi-web "$PROJECT_ROOT/frontend"
}

case "${1:-}" in
  backend)
    build_backend
    ;;
  frontend)
    build_frontend
    ;;
  all)
    build_backend
    build_frontend
    echo ""
    echo "=== Build complete: bin/magi + frontend/dist/ + 2 Docker images ==="
    ;;
  clean)
    rm -rf "$PROJECT_ROOT/bin" "$PROJECT_ROOT/frontend/dist"
    echo "Cleaned: bin/ frontend/dist/"
    echo "(Docker images not removed — use docker rmi manually if needed)"
    ;;
  *)
    echo "Usage: build.sh {backend|frontend|all|clean}"
    exit 1
    ;;
esac
