#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$PROJECT_ROOT/.env.local" ]; then
  set -a
  source "$PROJECT_ROOT/.env.local"
  set +a
fi

MIGRATIONS_DIR="file://$PROJECT_ROOT/docker/atlas/migrations"
DB_URL="mysql://${DB_USER:-magi}:${DB_PASS:-magi123}@${DB_HOST:-127.0.0.1}:${DB_PORT:-3307}/${DB_NAME:-magi}"

case "${1:-}" in
  migrate)
    atlas migrate apply --dir "$MIGRATIONS_DIR" --url "$DB_URL"
    ;;
  seed)
    echo "TODO: seed not implemented"
    ;;
  reset)
    echo "TODO: reset-db not implemented"
    ;;
  *)
    echo "Usage: db.sh {migrate|seed|reset}"
    exit 1
    ;;
esac
