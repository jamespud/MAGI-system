#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose-web.yml"

OUTPUT_DIR="${MAGI_BACKUP_DIR:-$PROJECT_ROOT/backups}"
RETAIN="${MAGI_BACKUP_RETAIN:-14}"
DRY_RUN=false

usage() {
  cat <<'USAGE'
Usage: backup.sh [options]

Create a consistent backup bundle for the Docker web stack:
  - MySQL logical dump (single transaction)
  - Milvus, Elasticsearch, etcd, and MinIO data volumes
  - SHA256SUMS and a portable tar.gz bundle

The magi-server container is paused during backup so no new case, memory,
knowledge, checkpoint, or RAG writes occur. MySQL and RAG middleware stay up.

Options:
  --output DIR   Output directory (default: backups/ or MAGI_BACKUP_DIR)
  --retain N     Keep the N newest magi-backup-*.tar.gz bundles (default: 14)
  --dry-run      Print the plan without stopping containers or reading data
  -h, --help     Show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      [[ $# -ge 2 ]] || { echo "--output requires a directory" >&2; exit 2; }
      OUTPUT_DIR="$2"; shift 2 ;;
    --retain)
      [[ $# -ge 2 ]] || { echo "--retain requires a number" >&2; exit 2; }
      RETAIN="$2"; shift 2 ;;
    --dry-run)
      DRY_RUN=true; shift ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ "$RETAIN" =~ ^[1-9][0-9]*$ ]] || { echo "retain must be a positive integer" >&2; exit 2; }
[[ "$RETAIN" -lt 1000 ]] || { echo "retain must be smaller than 1000" >&2; exit 2; }

if [[ -f "$PROJECT_ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$PROJECT_ROOT/.env"
  set +a
fi

DB_NAME="${DB_NAME:-magi}"
[[ "$DB_NAME" =~ ^[A-Za-z0-9_-]+$ ]] || { echo "DB_NAME contains unsupported characters" >&2; exit 2; }

mkdir -p "$OUTPUT_DIR"
OUTPUT_DIR="$(cd "$OUTPUT_DIR" && pwd)"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
STAGING_DIR="$(mktemp -d "$OUTPUT_DIR/.magi-backup-$TIMESTAMP.XXXXXX")"
BUNDLE="$OUTPUT_DIR/magi-backup-$TIMESTAMP.tar.gz"
SERVER_WAS_RUNNING=false
RAG_VOLUMES=(magi-milvus-data magi-es-data magi-etcd-data magi-minio-data)

compose() {
  docker compose --project-directory "$PROJECT_ROOT" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  rm -rf "$STAGING_DIR"
}

restore_server_if_needed() {
  if [[ "$SERVER_WAS_RUNNING" == true ]]; then
    compose start magi-server >/dev/null 2>&1 || true
  fi
}
trap 'restore_server_if_needed; cleanup' EXIT

if compose ps --status running --services 2>/dev/null | grep -qx 'magi-server'; then
  SERVER_WAS_RUNNING=true
fi

mkdir -p "$STAGING_DIR/mysql" "$STAGING_DIR/rag"

echo "MAGI backup"
echo "  bundle:       $BUNDLE"
echo "  database:     $DB_NAME"
echo "  RAG volumes:  ${RAG_VOLUMES[*]}"
echo "  app pause:    $SERVER_WAS_RUNNING"

if [[ "$DRY_RUN" == true ]]; then
  echo "Dry run only; no data was read and no containers were changed."
  exit 0
fi

if [[ "$SERVER_WAS_RUNNING" == true ]]; then
  compose stop magi-server >/dev/null
fi

echo "Dumping MySQL..."
compose exec -T mysql sh -c \
  'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --events --set-gtid-purged=OFF "$0"' \
  "$DB_NAME" | gzip -c > "$STAGING_DIR/mysql/mysql.sql.gz"
[[ -s "$STAGING_DIR/mysql/mysql.sql.gz" ]] || { echo "MySQL dump is empty" >&2; exit 1; }

for volume in "${RAG_VOLUMES[@]}"; do
  short="${volume#magi-}"
  short="${short%-data}"
  if ! docker volume inspect "$volume" >/dev/null 2>&1; then
    echo "Skipping missing volume: $volume"
    continue
  fi
  echo "Archiving $volume..."
  docker run --rm \
    -v "$volume:/source:ro" \
    -v "$STAGING_DIR:/backup" \
    alpine:3.21 sh -c 'cd /source && tar -czf "/backup/rag/$0.tar.gz" .' "$short"
done

{
  echo "MAGI_BACKUP_FORMAT=1"
  echo "CREATED_AT_UTC=$TIMESTAMP"
  echo "DB_NAME=$DB_NAME"
  echo "GIT_COMMIT=$(git -C "$PROJECT_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
  echo "MYSQL_DUMP=mysql/mysql.sql.gz"
  for volume in "${RAG_VOLUMES[@]}"; do
    short="${volume#magi-}"
    short="${short%-data}"
    echo "RAG_VOLUME_$short=$volume"
  done
} > "$STAGING_DIR/manifest.txt"

(
  cd "$STAGING_DIR"
  find mysql rag -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)

echo "Creating bundle..."
tar -czf "$BUNDLE" -C "$STAGING_DIR" manifest.txt SHA256SUMS mysql rag

echo "Pruning old bundles (keeping $RETAIN)..."
mapfile -t bundles < <(find "$OUTPUT_DIR" -maxdepth 1 -type f -name 'magi-backup-*.tar.gz' | sort -r)
if (( ${#bundles[@]} > RETAIN )); then
  rm -f "${bundles[@]:RETAIN}"
fi

restore_server_if_needed
SERVER_WAS_RUNNING=false

echo "Backup complete: $BUNDLE"
sha256sum "$BUNDLE"
