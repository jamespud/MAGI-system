#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$PROJECT_ROOT/.env" ]; then
  set -a
  source "$PROJECT_ROOT/.env"
  set +a
fi

MYSQL_HOST="${DB_HOST:-127.0.0.1}"
MYSQL_PORT="${DB_PORT:-3307}"
MYSQL_USER="${DB_USER:-magi}"
MYSQL_PASS="${DB_PASS:-magi123}"
MYSQL_DB="${DB_NAME:-magi}"
MYSQL_CMD="mysql -h $MYSQL_HOST -P $MYSQL_PORT -u $MYSQL_USER -p$MYSQL_PASS $MYSQL_DB"

case "${1:-}" in
  reset)
    echo "Dropping all tables in $MYSQL_DB..."
    $MYSQL_CMD -N -e \
      "SELECT CONCAT('DROP TABLE IF EXISTS \`', table_name, '\`;') FROM information_schema.tables WHERE table_schema = '$MYSQL_DB' AND table_type = 'BASE TABLE';" \
      | $MYSQL_CMD
    echo "Done. Tables will be recreated by AutoMigrate on next startup."
    ;;
  *)
    echo "Usage: db.sh {reset}"
    echo "  reset   Drop all tables in the MAGI database (AutoMigrate recreates on restart)"
    exit 1
    ;;
esac
