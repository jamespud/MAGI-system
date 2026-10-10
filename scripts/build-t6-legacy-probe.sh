#!/usr/bin/env bash
# Build the actual old repository code; no replacement fencing or mock SQL.
set -Eeuo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${1:?output directory required}"
mkdir -p "$target"
target="$(cd "$target" && pwd)"
legacy_commit=ee935d079da44f4a5ec8dd5ce3549a06cec3faab
git -C "$root" archive "$legacy_commit" backend > "$target/legacy.tar"
tar -xf "$target/legacy.tar" -C "$target"
mkdir -p "$target/backend/cmd/t6-legacy-probe"
cp "$root/backend/bootstrap/testdata/t6_legacy_probe.go" "$target/backend/cmd/t6-legacy-probe/main.go"
(
 cd "$target/backend"
 go build -buildvcs=false -o "$target/legacy-probe" ./cmd/t6-legacy-probe
)
printf '%s\n' "$target/legacy-probe"
