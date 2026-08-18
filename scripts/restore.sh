#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-web.yml"

BUNDLE=""
CONFIRM=false
DRY_RUN=false
SKIP_RAG=false
RAG_VOLUMES=(magi-milvus-data magi-es-data magi-etcd-data magi-minio-data)

usage() {
  cat <<'USAGE'
Usage: restore.sh --bundle FILE --confirm [options]

Restore a bundle created by scripts/backup.sh.

This is destructive:
  - stops magi-server and web
  - drops and recreates the configured MySQL database, then restores the dump
  - replaces Milvus/ES/etcd/MinIO volume contents unless --skip-rag is set
  - restarts the full Docker web stack

Options:
  --bundle FILE  Backup bundle to restore (required)
  --confirm      Explicitly authorize the destructive restore
  --skip-rag     Restore only MySQL; keep current RAG volumes
  --dry-run      Inspect and verify the bundle without changing containers
  -h, --help     Show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle)
      [[ $# -ge 2 ]] || { echo "--bundle requires a file" >&2; exit 2; }
      BUNDLE="$2"; shift 2 ;;
    --confirm)
      CONFIRM=true; shift ;;
    --skip-rag)
      SKIP_RAG=true; shift ;;
    --dry-run)
      DRY_RUN=true; shift ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "$BUNDLE" ]] || { usage >&2; exit 2; }
BUNDLE="$(cd "$(dirname "$BUNDLE")" && pwd)/$(basename "$BUNDLE")"
[[ -f "$BUNDLE" ]] || { echo "bundle not found: $BUNDLE" >&2; exit 2; }

if [[ -f "$PROJECT_ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$PROJECT_ROOT/.env"
  set +a
fi

DB_NAME="${DB_NAME:-magi}"
[[ "$DB_NAME" =~ ^[A-Za-z0-9_-]+$ ]] || { echo "DB_NAME contains unsupported characters" >&2; exit 2; }

compose() {
  docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" "$@"
}

STAGING_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGING_DIR"' EXIT

echo "Inspecting bundle: $BUNDLE"
mapfile -t entries < <(tar -tzf "$BUNDLE")
for entry in "${entries[@]}"; do
  case "$entry" in
    manifest.txt|SHA256SUMS|mysql/*|rag/*) ;;
    *)
      echo "refusing unsafe or unexpected archive entry: $entry" >&2
      exit 1
      ;;
  esac
done

tar -xzf "$BUNDLE" -C "$STAGING_DIR"
[[ -f "$STAGING_DIR/manifest.txt" ]] || { echo "manifest.txt missing" >&2; exit 1; }
[[ -f "$STAGING_DIR/SHA256SUMS" ]] || { echo "SHA256SUMS missing" >&2; exit 1; }
grep -qx 'MAGI_BACKUP_FORMAT=1' "$STAGING_DIR/manifest.txt" || {
  echo "unsupported backup format" >&2
  exit 1
}
[[ -f "$STAGING_DIR/mysql/mysql.sql.gz" ]] || { echo "mysql/mysql.sql.gz missing" >&2; exit 1; }
(
  cd "$STAGING_DIR"
  sha256sum --check SHA256SUMS
)

echo "Bundle verified."
if [[ "$DRY_RUN" == true ]]; then
  echo "Dry run only; no data was restored and no containers were changed."
  exit 0
fi

[[ "$CONFIRM" == true ]] || {
  echo "restore is destructive; rerun with --confirm" >&2
  exit 2
}

echo "Stopping application..."
compose stop magi-server web >/dev/null

echo "Recreating and restoring MySQL database: $DB_NAME"
compose exec -T mysql sh -c \
  'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "DROP DATABASE IF EXISTS \`$0\`; CREATE DATABASE \`$0\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"' \
  "$DB_NAME"
gzip -dc "$STAGING_DIR/mysql/mysql.sql.gz" | compose exec -T mysql sh -c \
  'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$0"' "$DB_NAME"

if [[ "$SKIP_RAG" != true ]]; then
  echo "Stopping RAG middleware..."
  compose stop milvus-standalone elasticsearch etcd minio >/dev/null
  for volume in "${RAG_VOLUMES[@]}"; do
    short="${volume#magi-}"
    short="${short%-data}"
    archive="$STAGING_DIR/rag/$short.tar.gz"
    if [[ ! -f "$archive" ]]; then
      echo "RAG archive missing for $volume; refusing partial restore" >&2
      exit 1
    fi
    docker volume inspect "$volume" >/dev/null 2>&1 || docker volume create "$volume" >/dev/null
    echo "Restoring $volume..."
    docker run --rm \
      -v "$volume:/target" \
      -v "$STAGING_DIR:/backup:ro" \
      alpine:3.21 sh -c \
      'find /target -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar -xzf "/backup/rag/$0.tar.gz" -C /target' "$short"
  done
fi

echo "Starting full stack..."
compose up -d >/dev/null

echo "Restore complete."
compose ps
echo "Verify readiness: curl http://localhost/ready"
