#!/usr/bin/env bash
# T6 offline writer cutover. Every failed stage leaves ingress frozen. There is
# deliberately no EXIT handler that restarts an old writer.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DRY_RUN=false
if [[ "${1:-}" == --dry-run ]]; then DRY_RUN=true; shift; fi
[[ $# == 0 ]] || { echo "usage: decision-cutover.sh [--dry-run]" >&2; exit 2; }

plan() {
  cat <<'PLAN'
1. Freeze all Decision ingress, Recurring, Benchmark and A2A launchers.
2. BLOCK admission when S29 exists; drain accepted work with a bounded deadline.
3. Stop every old instance; backup while the Compose writer remains stopped.
4. Install/verify S29 on an already verified T1-T4 schema. No silent repair.
5. Configure separate new/legacy identities; lock/revoke old grants and kill sessions.
6. Verify schema, zero RUNNING jobs, legacy exclusion and the new writer identity.
7. ENABLE the exact blocked epoch, start only the immutable new image, reopen ingress.
Any failure keeps ingress frozen and the writer stopped; repair and rerun.
PLAN
}
if "$DRY_RUN"; then plan; exit 0; fi

: "${MAGI_DECISION_ADMIN:?Path to decision-admin built from the target commit}"
: "${MAGI_ADMIN_DSN:?Private migrator/control-plane DSN}"
: "${MAGI_WRITER_DSN:?Private new writer DSN for verification}"
: "${MAGI_WRITER_DB_USER:?Separate runtime writer user}"
: "${MAGI_WRITER_DB_PASSWORD:?Separate runtime writer password}"
: "${MAGI_WRITER_IMAGE:?Immutable new image digest}"
: "${MAGI_NEW_WRITER_ACCOUNT:?New user@host, provisioned with per-table DML}"
: "${MAGI_LEGACY_WRITER_ACCOUNT:?Old user@host}"
: "${MAGI_CUTOVER_FREEZE:?Executable that freezes all external launchers and ingress}"
: "${MAGI_CUTOVER_STOP_ALL:?Executable that stops every inventoried old deployment}"
: "${MAGI_CUTOVER_OPEN:?Executable that reopens only verified new ingress}"
[[ "$MAGI_WRITER_IMAGE" =~ @sha256:[0-9a-f]{64}$ || "$MAGI_WRITER_IMAGE" =~ ^sha256:[0-9a-f]{64}$ ]] || {
 echo "Writer image must be pinned by digest" >&2; exit 2;
}
for hook in "$MAGI_CUTOVER_FREEZE" "$MAGI_CUTOVER_STOP_ALL" "$MAGI_CUTOVER_OPEN"; do
 [[ -x "$hook" ]] || { echo "Cutover hook must be an executable path" >&2; exit 2; }
done
compose() { docker compose --project-directory "$PROJECT_ROOT" -f "$PROJECT_ROOT/docker/docker-compose-web.yml" "$@"; }
admin() { "$MAGI_DECISION_ADMIN" "$@"; }
# Failure containment never starts a service or opens ingress.
contain_failure() {
 compose stop magi-server web >/dev/null 2>&1 || true
 "$MAGI_CUTOVER_STOP_ALL" >/dev/null 2>&1 || true
 admin block >/dev/null 2>&1 || true
 echo "Cutover failed; ingress remains frozen. Inspect status and repair offline." >&2
}
trap contain_failure ERR
"$MAGI_CUTOVER_FREEZE"
# Absence of S29 is safe only because the old launchers have been frozen by the
# explicit inventory hook. An existing malformed gate is never ignored.
if admin status >/dev/null 2>&1; then admin block >/dev/null; fi
deadline=$((SECONDS + ${MAGI_CUTOVER_DRAIN_SECONDS:-300}))
while true; do
 running_count="$(admin running-count)"
 [[ "$running_count" =~ ^[0-9]+$ ]] || { echo "Invalid drain observation" >&2; false; }
 if [[ "$running_count" == 0 ]]; then break; fi
 [[ $SECONDS -lt $deadline ]] || { echo "Drain timed out; cancel explicitly before retrying" >&2; false; }
 sleep 1
done
"$MAGI_CUTOVER_STOP_ALL"
compose stop magi-server web
"$SCRIPT_DIR/backup.sh" --output "${MAGI_BACKUP_DIR:-$PROJECT_ROOT/backups}"
admin install-contract
admin block >/dev/null
admin configure --writer "$MAGI_NEW_WRITER_ACCOUNT" --legacy "$MAGI_LEGACY_WRITER_ACCOUNT"
admin exclude-legacy
admin verify-cutover
admin verify-identity
# Read the epoch using a new BLOCK after all preparation, avoiding stale plans.
cutover_epoch="$(admin block)"
admin enable --epoch "$cutover_epoch"
admin verify-writer
compose up -d --no-build magi-server web
# Verify health before opening external traffic; failure invokes containment.
compose up -d --no-build --wait magi-server web
"$MAGI_CUTOVER_OPEN"
trap - ERR
