#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

case "${1:-}" in
  backend)
    go -C "$PROJECT_ROOT/backend" build -o "$PROJECT_ROOT/bin/magi" ./cmd/magi-server
    ;;
  frontend)
    npm -C "$PROJECT_ROOT/frontend" run build
    ;;
  all)
    echo "=== Building backend ==="
    go -C "$PROJECT_ROOT/backend" build -o "$PROJECT_ROOT/bin/magi" ./cmd/magi-server
    echo "=== Building frontend ==="
    npm -C "$PROJECT_ROOT/frontend" run build
    echo "=== Build complete: bin/magi + frontend/dist/ ==="
    ;;
  clean)
    rm -rf "$PROJECT_ROOT/bin" "$PROJECT_ROOT/frontend/dist"
    echo "Cleaned: bin/ frontend/dist/"
    ;;
  *)
    echo "Usage: build.sh {backend|frontend|all|clean}"
    exit 1
    ;;
esac
