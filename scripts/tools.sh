#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

case "${1:-}" in
  fmt)
    go -C "$PROJECT_ROOT/backend" fmt ./...
    ;;
  lint)
    golangci-lint -C "$PROJECT_ROOT/backend" run
    ;;
  vet)
    go -C "$PROJECT_ROOT/backend" vet ./...
    ;;
  tidy)
    go -C "$PROJECT_ROOT/backend" mod tidy
    ;;
  test)
    go -C "$PROJECT_ROOT/backend" test ./... -cover
    ;;
  *)
    echo "Usage: tools.sh {fmt|lint|vet|tidy|test}"
    exit 1
    ;;
esac
