#!/bin/sh
# Render the scrape target port (busybox in the prometheus image has no
# envsubst, so substitute with sed), then exec the real prometheus binary.
set -eu
sed "s|\${MAGI_HTTP_PORT}|${MAGI_HTTP_PORT:-8080}|g" \
    /etc/prometheus/prometheus.yml.template > /etc/prometheus/prometheus.yml
exec /bin/prometheus "$@"
