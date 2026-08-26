#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

compose_out="$tmp_dir/compose.yml"
MAGI_A2A_ENABLED=true \
MAGI_AUTH_ENABLED=true \
MAGI_AUTH_API_KEYS='1:admin:test:render-only-random-secret' \
MAGI_A2A_PUBLIC_URL='https://magi.example.com' \
docker compose -f "$repo_root/docker/docker-compose-web.yml" config >"$compose_out"

rg -q 'MAGI_A2A_ENABLED.*true' "$compose_out"
rg -q 'MAGI_AUTH_ENABLED.*true' "$compose_out"
rg -q 'MAGI_AUTH_API_KEYS.*1:admin:test:render-only-random-secret' "$compose_out"
rg -q 'MAGI_A2A_MAX_STREAMS_PER_USER_PER_REPLICA' "$compose_out"

helm_out="$tmp_dir/helm.yml"
helm template magi "$repo_root/deploy/magi" \
  --set-string secret.values.dbDSN=render-only \
  --set-string secret.values.modelApiKey=render-only \
  --set-string secret.values.authAPIKeys='1:admin:test:render-only-random-secret' \
  --set-string configuration.authEnabled=true \
  --set-string configuration.a2a.enabled=true \
  --set-string configuration.a2a.publicURL=https://magi.example.com \
  --set-string configuration.a2a.maxRequestBytes=131072 >"$helm_out"

rg -q '/.well-known/agent-card.json' "$helm_out"
rg -q 'location /a2a/' "$helm_out"
rg -q 'client_max_body_size 131072' "$helm_out"
rg -q 'MAGI_AUTH_ENABLED' "$helm_out"
rg -q 'MAGI_AUTH_API_KEYS' "$helm_out"
rg -q 'MAGI_A2A_MAX_STREAMS_PER_USER_PER_REPLICA' "$helm_out"

if helm template magi "$repo_root/deploy/magi" \
  --set-string secret.values.dbDSN=render-only \
  --set-string secret.values.modelApiKey=render-only \
  --set-string configuration.a2a.enabled=true \
  --set-string configuration.authEnabled=false >"$tmp_dir/unsafe-auth.yml" 2>&1; then
  echo "unsafe Helm render accepted A2A with auth disabled" >&2
  exit 1
fi

if helm template magi "$repo_root/deploy/magi" \
  --set-string secret.values.dbDSN=render-only \
  --set-string secret.values.modelApiKey=render-only \
  --set-string configuration.a2a.enabled=true \
  --set-string configuration.authEnabled=true \
  --set-string secret.values.authAPIKeys= >"$tmp_dir/unsafe-key.yml" 2>&1; then
  echo "unsafe Helm render accepted an empty chart-created auth secret" >&2
  exit 1
fi

(
  cd "$repo_root/backend"
  GOCACHE=/tmp/magi-a2a-gocache go test ./bootstrap \
    -run ExampleConfigContainsNoActiveBootstrapCredential -count=1
)
