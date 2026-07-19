#!/usr/bin/env bash
set -Eeuo pipefail

case "${1:-}" in
  backend)
    go -C backend run ./cmd/magi-server
    ;;
  frontend)
    npm -C frontend run dev
    ;;
  debug)
    trap 'kill 0' EXIT
    go -C backend run ./cmd/magi-server &
    npm -C frontend run dev &
    wait
    ;;
  server)
    go -C backend run ./cmd/magi-server
    ;;
  *)
    echo "Usage: dev.sh {backend|frontend|debug|server}"
    exit 1
    ;;
esac
